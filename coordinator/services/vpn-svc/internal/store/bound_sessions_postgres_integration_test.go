//go:build integration

// Postgres integration tests for the #788 daemon-restart recovery query:
// Postgres.ListBoundSessions.
//
// The Memory-impl mirror lives in assigned_session_cutoff_test.go
// (TestMemory_ListBoundSessions_*). This file pins the real SQL on Postgres
// so we catch column/predicate drift the in-memory version can't surface —
// specifically that ListBoundSessions OMITS the two ListAssignedSessions
// exclusions (the `provider_wg_public_key = ''` already-keyed filter and the
// `created_at > now() - AssignedSessionMaxAge` cutoff) while keeping the
// non-terminated + non-empty-customer-key predicates.
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
// Refs #788.

package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
)

// seedBoundSessionRow inserts a session that has already advertised both a
// customer key and (optionally) a provider key, with an explicit CreatedAt
// so the test can place it inside or outside the AssignedSessionMaxAge
// window. Returns the session ID.
func seedBoundSessionRow(t *testing.T, ctx context.Context, p *Postgres, providerID uuid.UUID, customerKey, providerKey string, createdAt time.Time) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := p.CreateSession(ctx, &Session{
		ID:                  id,
		CustomerID:          uuid.New(),
		Region:              "us-east-1",
		PrimaryProvider:     providerID,
		CurrentProvider:     providerID,
		State:               pb.VpnSessionState_VPN_SESSION_STATE_ACTIVE,
		CreatedAt:           createdAt,
		LastActivityAt:      time.Now().UTC(),
		CustomerWgPublicKey: customerKey,
	}); err != nil {
		t.Fatalf("seed bound session: %v", err)
	}
	if providerKey != "" {
		if err := p.BindProviderToSession(ctx, id, providerKey); err != nil {
			t.Fatalf("bind provider on seed: %v", err)
		}
	}
	return id
}

// TestPostgresListBoundSessions_ReturnsLiveBoundIncludingOldAndKeyed is the
// core #788 path: ListBoundSessions returns EVERY still-live session bound to
// the provider with a customer key — including already-provider-keyed ones
// AND ones older than AssignedSessionMaxAge, which ListAssignedSessions hides.
// Those are exactly the peers a daemon restart drops from its in-memory map.
func TestPostgresListBoundSessions_ReturnsLiveBoundIncludingOldAndKeyed(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	prov := uuid.New()
	other := uuid.New()
	seedProviderRow(t, ctx, p, prov)
	seedProviderRow(t, ctx, p, other)

	now := time.Now().UTC()

	// (1) Bound + provider-keyed + an hour old: hidden by /assigned-sessions,
	//     MUST appear here (the stranded customer).
	boundOld := seedBoundSessionRow(t, ctx, p, prov,
		"Y3VzdG9tZXJfa2V5X29sZA==", "cHJvdmlkZXJfa2V5",
		now.Add(-1*time.Hour))
	// (2) Fresh, customer-keyed, not yet provider-keyed: live.
	freshBound := seedBoundSessionRow(t, ctx, p, prov,
		"Y3VzdG9tZXJfa2V5X2ZyZXNo", "",
		now.Add(-2*time.Minute))
	// (3) No customer key at all: nothing to upsert → excluded.
	noKey := seedSessionRow(t, ctx, p, prov, uuid.New())
	// (4) Other provider's bound session: excluded.
	otherProv := seedBoundSessionRow(t, ctx, p, other,
		"b3RoZXJfY3VzdG9tZXJfa2V5", "b3RoZXJfcHJvdmlkZXI=",
		now.Add(-3*time.Minute))
	// (5) Terminated bound session on prov: excluded.
	term := seedBoundSessionRow(t, ctx, p, prov,
		"dGVybWluYXRlZF9rZXk=", "cHJvdmlkZXJfa2V5",
		now.Add(-10*time.Minute))
	if err := p.TerminateSession(ctx, term, "client_disconnect"); err != nil {
		t.Fatalf("terminate: %v", err)
	}

	got, err := p.ListBoundSessions(ctx, prov)
	if err != nil {
		t.Fatalf("ListBoundSessions: %v", err)
	}
	ids := map[uuid.UUID]*Session{}
	for _, s := range got {
		ids[s.ID] = s
	}
	if len(got) != 2 {
		t.Fatalf("want exactly {boundOld, freshBound}, got %d: %v", len(got), keysOf(ids))
	}
	if s, ok := ids[boundOld]; !ok {
		t.Errorf("boundOld MUST be returned (live, bound, >15min) — it is the customer a restart strands")
	} else {
		// The customer key must round-trip so the binder can upsert the peer.
		if s.CustomerWgPublicKey != "Y3VzdG9tZXJfa2V5X29sZA==" {
			t.Errorf("boundOld customer key=%q, want round-tripped value", s.CustomerWgPublicKey)
		}
		if s.ProviderWgPublicKey != "cHJvdmlkZXJfa2V5" {
			t.Errorf("boundOld provider key=%q, want round-tripped value (proves we DON'T filter on it)", s.ProviderWgPublicKey)
		}
	}
	if _, ok := ids[freshBound]; !ok {
		t.Errorf("freshBound must be returned")
	}
	if _, ok := ids[noKey]; ok {
		t.Errorf("noKey must be excluded (nothing to upsert)")
	}
	if _, ok := ids[otherProv]; ok {
		t.Errorf("other provider's session must be excluded")
	}
	if _, ok := ids[term]; ok {
		t.Errorf("terminated session must be excluded")
	}
}

// TestPostgresListBoundSessions_SimulatedRestartReDerivesPeers proves the
// recovery contract end to end against real SQL: every live bound peer is
// re-derivable from ListBoundSessions even when ALL of them are past the
// bring-up cutoff (so /assigned-sessions returns nothing). A brand-new
// daemon view (the post-restart binder, holding no in-memory peers) can thus
// rebuild its full peer set.
func TestPostgresListBoundSessions_SimulatedRestartReDerivesPeers(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	prov := uuid.New()
	seedProviderRow(t, ctx, p, prov)
	now := time.Now().UTC()

	want := map[string]bool{}
	for i := 0; i < 3; i++ {
		key := uuid.NewString()
		seedBoundSessionRow(t, ctx, p, prov, key, "cHJvdmlkZXJfa2V5",
			now.Add(-AssignedSessionMaxAge-time.Duration(i+1)*time.Minute))
		want[key] = true
	}

	// Precondition: bind-poll hides every one (bound + past cutoff) — the
	// reason a naive restart strands them.
	assigned, err := p.ListAssignedSessions(ctx, prov)
	if err != nil {
		t.Fatalf("ListAssignedSessions: %v", err)
	}
	if len(assigned) != 0 {
		t.Fatalf("precondition: bind-poll should hide bound+old sessions, got %d", len(assigned))
	}

	bound, err := p.ListBoundSessions(ctx, prov)
	if err != nil {
		t.Fatalf("ListBoundSessions: %v", err)
	}
	if len(bound) != len(want) {
		t.Fatalf("recovery query must re-derive all %d live peers, got %d", len(want), len(bound))
	}
	for _, s := range bound {
		if !want[s.CustomerWgPublicKey] {
			t.Errorf("unexpected/duplicate recovered key %q", s.CustomerWgPublicKey)
		}
		delete(want, s.CustomerWgPublicKey)
	}
	if len(want) != 0 {
		t.Errorf("missing peers from recovery: %v", want)
	}
}

func keysOf(m map[uuid.UUID]*Session) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
