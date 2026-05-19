//go:build integration

// Integration tests for the audit-retention pruner. Requires a reachable
// Postgres (default postgres://postgres:postgres@localhost:5432/antiabuse_audit?sslmode=disable).
//
// Run with:
//
//	docker run --rm -d -p 5432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=antiabuse_audit postgres:16
//	go test -tags=integration ./internal/audit/...
//
// The test exercises the full flow:
//
//	1. EnsureIndex creates the schema
//	2. Seed two rows: one fresh, one 100 days old
//	3. RunOnce with retentionDays=90
//	4. Assert the old row was deleted, the fresh row survived
package audit

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const defaultDSN = "postgres://postgres:postgres@localhost:5432/antiabuse_audit?sslmode=disable"

func dsn() string {
	if v := os.Getenv("AUDIT_POSTGRES_DSN"); v != "" {
		return v
	}
	return defaultDSN
}

func TestPruner_Integration_DeletesStaleRows(t *testing.T) {
	db, err := sql.Open("pgx", dsn())
	if err != nil {
		t.Skipf("postgres unreachable, skipping: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("postgres ping failed, skipping: %v", err)
	}

	// Clean slate per run.
	_, _ = db.Exec(`DROP TABLE IF EXISTS antiabuse_audit`)

	p := NewPruner(PrunerOptions{
		RetentionDays: 90,
		DB:            db,
	})
	if err := p.EnsureIndex(context.Background()); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	// Seed: one fresh row, one 100 days old.
	fresh := time.Now().UTC()
	stale := time.Now().UTC().Add(-100 * 24 * time.Hour)
	for _, c := range []struct {
		when time.Time
		tag  string
	}{
		{fresh, "fresh"},
		{stale, "stale"},
	} {
		_, err := db.Exec(`INSERT INTO antiabuse_audit
			(created_at, check_type, decision, payload)
			VALUES ($1, $2, $3, $4::jsonb)`,
			c.when, "check_url", "FILTER_DECISION_BLOCK", `{"tag":"`+c.tag+`"}`)
		if err != nil {
			t.Fatalf("insert %s: %v", c.tag, err)
		}
	}

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM antiabuse_audit`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 1 {
		t.Errorf("rows remaining = %d, want 1", total)
	}

	var tag string
	if err := db.QueryRow(`SELECT payload->>'tag' FROM antiabuse_audit LIMIT 1`).Scan(&tag); err != nil {
		t.Fatalf("scan tag: %v", err)
	}
	if tag != "fresh" {
		t.Errorf("surviving tag = %q, want fresh", tag)
	}

	_, deleted, lastErr := p.Status()
	if lastErr != nil {
		t.Errorf("Status err = %v", lastErr)
	}
	if deleted != 1 {
		t.Errorf("Status deleted = %d, want 1", deleted)
	}
}

func TestPruner_Integration_HonorsBatchLimit(t *testing.T) {
	db, err := sql.Open("pgx", dsn())
	if err != nil {
		t.Skipf("postgres unreachable, skipping: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("postgres ping failed, skipping: %v", err)
	}

	_, _ = db.Exec(`DROP TABLE IF EXISTS antiabuse_audit`)

	p := NewPruner(PrunerOptions{
		RetentionDays: 90,
		DB:            db,
		Batch:         50, // tiny batch — exercises the multi-pass loop
	})
	if err := p.EnsureIndex(context.Background()); err != nil {
		t.Fatalf("EnsureIndex: %v", err)
	}

	// Insert 200 stale rows.
	stale := time.Now().UTC().Add(-100 * 24 * time.Hour)
	for i := 0; i < 200; i++ {
		_, err := db.Exec(`INSERT INTO antiabuse_audit
			(created_at, check_type, decision, payload)
			VALUES ($1, $2, $3, $4::jsonb)`,
			stale, "check_url", "FILTER_DECISION_ALLOW", `{}`)
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM antiabuse_audit`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != 0 {
		t.Errorf("rows remaining = %d, want 0", total)
	}
}
