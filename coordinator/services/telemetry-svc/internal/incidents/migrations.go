// Package incidents implements the operator-curated incident timeline,
// the email-subscription registry, and the daily uptime sample ledger
// behind the public status page at status.iogrid.org.
//
// Storage is split into two implementations behind the [Store]
// interface:
//
//   - [InMemory] — keeps everything in process memory. Used by unit
//     tests, the local-dev binary (no DATABASE_URL), and as a
//     deliberate fallback when Postgres is unavailable so the public
//     /status page never goes dark just because the DB is down.
//   - [Postgres] — pgx-backed implementation against the schema in
//     ./migrations/0001_init.sql. Used in prod.
//
// All public read endpoints (/status, /status/posture, /status/uptime)
// are intentionally world-readable; mutating endpoints
// (/status/incidents, /status/subscribe) gate behind either a
// shared-secret admin token (incident management) or a public
// rate-limited path (subscribe).
package incidents

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrations is the embedded SQL bundle.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Apply runs every pending Up migration against the supplied URL.
// Idempotent across pod restarts.
func Apply(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return fmt.Errorf("incidents: DATABASE_URL is empty")
	}
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("incidents: open db for migrations: %w", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("incidents: goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		return fmt.Errorf("incidents: goose up: %w", err)
	}
	return nil
}

// Ensure the pgx stdlib driver is linked so sql.Open("pgx") works.
var _ = stdlib.GetDefaultDriver
