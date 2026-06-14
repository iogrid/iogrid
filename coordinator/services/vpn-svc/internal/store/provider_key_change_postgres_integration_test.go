//go:build integration

// Postgres integration tests for the #762 server-key-recurrence guard:
// Postgres.InvalidateSessionsOnProviderKeyChange.
//
// The Memory-impl mirror lives in memory_test.go
// (TestMemoryStore_InvalidateSessionsOnProviderKeyChange). This file pins
// the real CTE on Postgres so we catch SQL-level drift the in-memory
// version can't surface: the WITH-clause prior/changed/terminated chain,
// the NULLIF('') prior-key normalisation, and the
// `(current_provider_id = $1 OR primary_provider_id = $1)` predicate.
//
// Run with:
//
//	docker run --rm -d -p 5432:5432 \
//	    -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=vpn_svc_test \
//	    postgres:16
//	DATABASE_URL=postgres://postgres:postgres@localhost:5432/vpn_svc_test?sslmode=disable \
//	    go test -tags=integration ./internal/store/...
//
// Skips cleanly when the database is unreachable (shares newPostgresFixture
// with inner_ip_postgres_integration_test.go).
//
// Refs #762.

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seedProviderRowWithKey registers a provider carrying an explicit WG
// server pubkey so the key-change comparison has a prior value to diverge
// from. seedProviderRow (no key) is the legacy/empty-key case.
func seedProviderRowWithKey(t *testing.T, ctx context.Context, p *Postgres, providerID uuid.UUID, key string) {
	t.Helper()
	if err := p.RegisterProvider(ctx, &ProviderInfo{
		ID:          providerID,
		Region:      "us-east-1",
		Status:      "healthy",
		LastSeenAt:  time.Now().UTC(),
		WgPublicKey: key,
	}); err != nil {
		t.Fatalf("seed provider %s with key: %v", providerID, err)
	}
}

func sessionTerminated(t *testing.T, ctx context.Context, p *Postgres, id uuid.UUID) (bool, string) {
	t.Helper()
	got, err := p.GetSession(ctx, id)
	if err != nil {
		t.Fatalf("GetSession %s: %v", id, err)
	}
	return got.TerminatedAt != nil, got.ExitReason
}

// TestPostgresInvalidateSessionsOnProviderKeyChange_TerminatesOnRotation is
// the core #762 path: a changed server pubkey terminates every still-active
// session bound to the provider (whether via current_provider_id OR
// primary_provider_id) and leaves unrelated + already-terminated sessions
// alone.
func TestPostgresInvalidateSessionsOnProviderKeyChange_TerminatesOnRotation(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	provA := uuid.New()
	provB := uuid.New()
	seedProviderRowWithKey(t, ctx, p, provA, "OLDKEY")
	seedProviderRowWithKey(t, ctx, p, provB, "OTHERKEY")

	// Active session whose current_provider is provA.
	sCurrent := seedSessionRow(t, ctx, p, provA, uuid.New())
	// Active session that failed over to provB but whose primary is provA.
	sPrimaryOnly := uuid.New()
	if err := p.CreateSession(ctx, &Session{
		ID: sPrimaryOnly, CustomerID: uuid.New(), Region: "us-east-1",
		PrimaryProvider: provA, CurrentProvider: provB,
		CreatedAt: time.Now().UTC(), LastActivityAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed primary-only session: %v", err)
	}
	// Active session unrelated to provA.
	sOther := seedSessionRow(t, ctx, p, provB, uuid.New())
	// Already-terminated session on provA (must not be re-counted).
	sDone := seedSessionRow(t, ctx, p, provA, uuid.New())
	if err := p.TerminateSession(ctx, sDone, "client_disconnect"); err != nil {
		t.Fatalf("pre-terminate: %v", err)
	}

	n, changed, err := p.InvalidateSessionsOnProviderKeyChange(ctx, provA, "NEWKEY")
	if err != nil {
		t.Fatalf("InvalidateSessionsOnProviderKeyChange: %v", err)
	}
	if !changed {
		t.Fatalf("expected changed=true on a rotated key")
	}
	if n != 2 {
		t.Fatalf("expected 2 terminations (current + primary on provA), got %d", n)
	}

	if term, reason := sessionTerminated(t, ctx, p, sCurrent); !term || reason != "provider_key_rotated" {
		t.Errorf("current-provider session: terminated=%v reason=%q, want true/provider_key_rotated", term, reason)
	}
	if term, reason := sessionTerminated(t, ctx, p, sPrimaryOnly); !term || reason != "provider_key_rotated" {
		t.Errorf("primary-provider session: terminated=%v reason=%q, want true/provider_key_rotated", term, reason)
	}
	if term, _ := sessionTerminated(t, ctx, p, sOther); term {
		t.Errorf("unrelated-provider session must NOT be terminated")
	}
	// The pre-terminated session keeps its original exit reason (not clobbered).
	if _, reason := sessionTerminated(t, ctx, p, sDone); reason != "client_disconnect" {
		t.Errorf("already-terminated session exit reason changed to %q", reason)
	}
}

// TestPostgresInvalidateSessionsOnProviderKeyChange_NoopCases verifies every
// path that must leave sessions untouched: unchanged key, empty newKey
// (legacy daemon), and a provider with no prior key (first register).
func TestPostgresInvalidateSessionsOnProviderKeyChange_NoopCases(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	t.Run("unchanged key", func(t *testing.T) {
		prov := uuid.New()
		seedProviderRowWithKey(t, ctx, p, prov, "STABLE")
		s := seedSessionRow(t, ctx, p, prov, uuid.New())
		n, changed, err := p.InvalidateSessionsOnProviderKeyChange(ctx, prov, "STABLE")
		if err != nil {
			t.Fatalf("invalidate: %v", err)
		}
		if changed || n != 0 {
			t.Fatalf("unchanged key must be a no-op, got changed=%v n=%d", changed, n)
		}
		if term, _ := sessionTerminated(t, ctx, p, s); term {
			t.Errorf("session must survive an unchanged-key re-register")
		}
	})

	t.Run("empty newKey legacy daemon", func(t *testing.T) {
		prov := uuid.New()
		seedProviderRowWithKey(t, ctx, p, prov, "PRESENT")
		s := seedSessionRow(t, ctx, p, prov, uuid.New())
		n, changed, err := p.InvalidateSessionsOnProviderKeyChange(ctx, prov, "")
		if err != nil {
			t.Fatalf("invalidate: %v", err)
		}
		if changed || n != 0 {
			t.Fatalf("empty key must be a no-op, got changed=%v n=%d", changed, n)
		}
		if term, _ := sessionTerminated(t, ctx, p, s); term {
			t.Errorf("session must survive an empty-key (legacy) re-register")
		}
	})

	t.Run("provider with no prior key", func(t *testing.T) {
		prov := uuid.New()
		seedProviderRow(t, ctx, p, prov) // registered, but no wg key
		s := seedSessionRow(t, ctx, p, prov, uuid.New())
		n, changed, err := p.InvalidateSessionsOnProviderKeyChange(ctx, prov, "FIRSTKEY")
		if err != nil {
			t.Fatalf("invalidate: %v", err)
		}
		if changed || n != 0 {
			t.Fatalf("first key must not count as a change, got changed=%v n=%d", changed, n)
		}
		if term, _ := sessionTerminated(t, ctx, p, s); term {
			t.Errorf("session must survive a provider's first-key publish")
		}
	})

	t.Run("provider row absent entirely", func(t *testing.T) {
		// No provider row at all → prior key is NULL → no change.
		n, changed, err := p.InvalidateSessionsOnProviderKeyChange(ctx, uuid.New(), "ANY")
		if err != nil {
			t.Fatalf("invalidate: %v", err)
		}
		if changed || n != 0 {
			t.Fatalf("absent provider must be a no-op, got changed=%v n=%d", changed, n)
		}
	})
}
