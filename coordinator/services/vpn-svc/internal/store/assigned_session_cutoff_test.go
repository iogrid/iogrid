package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// #730: a session the daemon couldn't bind within AssignedSessionMaxAge is
// an abandoned connect attempt — ListAssignedSessions must stop returning
// it, or the binder polls it forever (observed live: 9 day-old zombies
// polled every 5s). Fresh unbound sessions must still be returned.
func TestMemory_ListAssignedSessions_ExcludesStaleBringUps(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	provider := uuid.New()

	fresh := &Session{
		ID:              uuid.New(),
		CustomerID:      uuid.New(),
		Region:          "us-east-1",
		PrimaryProvider: provider,
		CurrentProvider: provider,
		CreatedAt:       time.Now().Add(-1 * time.Minute),
	}
	stale := &Session{
		ID:              uuid.New(),
		CustomerID:      uuid.New(),
		Region:          "us-east-1",
		PrimaryProvider: provider,
		CurrentProvider: provider,
		CreatedAt:       time.Now().Add(-AssignedSessionMaxAge - time.Minute),
	}
	for _, s := range []*Session{fresh, stale} {
		if err := m.CreateSession(ctx, s); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	assigned, err := m.ListAssignedSessions(ctx, provider)
	if err != nil {
		t.Fatalf("ListAssignedSessions: %v", err)
	}
	if len(assigned) != 1 {
		t.Fatalf("want exactly the fresh session, got %d sessions", len(assigned))
	}
	if assigned[0].ID != fresh.ID {
		t.Fatalf("want fresh session %s, got %s", fresh.ID, assigned[0].ID)
	}
}

// #788: the restart-recovery query must return EVERY live bound session for
// the provider — including ones already-keyed AND older than
// AssignedSessionMaxAge, which ListAssignedSessions deliberately hides. This
// is the exact set a daemon restart loses from its in-memory peer map. Rows
// with no customer key, terminated rows, and other providers' rows are
// excluded.
func TestMemory_ListBoundSessions_IncludesBoundAndOldButLive(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	provider := uuid.New()
	other := uuid.New()

	// Bound an hour ago — ListAssignedSessions hides this (already-keyed
	// AND past the 15-min cutoff), but it is exactly the stranded customer.
	boundOld := &Session{
		ID:                  uuid.New(),
		CustomerID:          uuid.New(),
		Region:              "us-east-1",
		PrimaryProvider:     provider,
		CurrentProvider:     provider,
		CreatedAt:           time.Now().Add(-1 * time.Hour),
		CustomerWgPublicKey: "Y3VzdG9tZXJfa2V5X29sZA==",
		ProviderWgPublicKey: "cHJvdmlkZXJfa2V5", // already bound
	}
	// Fresh, just bound a key, not yet provider-keyed — also live.
	freshBound := &Session{
		ID:                  uuid.New(),
		CustomerID:          uuid.New(),
		Region:              "us-east-1",
		PrimaryProvider:     provider,
		CurrentProvider:     provider,
		CreatedAt:           time.Now().Add(-2 * time.Minute),
		CustomerWgPublicKey: "Y3VzdG9tZXJfa2V5X2ZyZXNo",
	}
	// No customer key yet — nothing to upsert, must be excluded.
	noKey := &Session{
		ID:              uuid.New(),
		CustomerID:      uuid.New(),
		Region:          "us-east-1",
		PrimaryProvider: provider,
		CurrentProvider: provider,
		CreatedAt:       time.Now().Add(-5 * time.Minute),
	}
	// Different provider — excluded.
	otherProv := &Session{
		ID:                  uuid.New(),
		CustomerID:          uuid.New(),
		Region:              "us-east-1",
		PrimaryProvider:     other,
		CurrentProvider:     other,
		CreatedAt:           time.Now().Add(-3 * time.Minute),
		CustomerWgPublicKey: "b3RoZXJfY3VzdG9tZXJfa2V5",
	}
	for _, s := range []*Session{boundOld, freshBound, noKey, otherProv} {
		if err := m.CreateSession(ctx, s); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	// Terminated bound session — excluded.
	term := &Session{
		ID:                  uuid.New(),
		CustomerID:          uuid.New(),
		Region:              "us-east-1",
		PrimaryProvider:     provider,
		CurrentProvider:     provider,
		CreatedAt:           time.Now().Add(-10 * time.Minute),
		CustomerWgPublicKey: "dGVybWluYXRlZF9rZXk=",
	}
	if err := m.CreateSession(ctx, term); err != nil {
		t.Fatalf("CreateSession term: %v", err)
	}
	if err := m.TerminateSession(ctx, term.ID, "client_disconnect"); err != nil {
		t.Fatalf("TerminateSession: %v", err)
	}

	bound, err := m.ListBoundSessions(ctx, provider)
	if err != nil {
		t.Fatalf("ListBoundSessions: %v", err)
	}
	got := map[uuid.UUID]bool{}
	for _, s := range bound {
		got[s.ID] = true
	}
	if len(bound) != 2 {
		t.Fatalf("want exactly {boundOld, freshBound}, got %d sessions: %v", len(bound), got)
	}
	if !got[boundOld.ID] {
		t.Errorf("boundOld (live, bound, >15min) MUST be returned — it is the stranded customer the restart lost")
	}
	if !got[freshBound.ID] {
		t.Errorf("freshBound (live, keyed) must be returned")
	}
	if got[noKey.ID] {
		t.Errorf("noKey session has nothing to upsert and must be excluded")
	}
	if got[otherProv.ID] {
		t.Errorf("other provider's session must be excluded")
	}
	if got[term.ID] {
		t.Errorf("terminated session must be excluded")
	}
}

// TestMemory_ListBoundSessions_SimulatedRestartReDerivesPeers models the
// actual failure: a daemon binds customers, then restarts. The "fresh
// daemon view" (a brand-new binder with an empty peer set) must be able to
// re-derive every live customer peer purely from ListBoundSessions, with no
// dependence on the 15-min bring-up window. We simulate the restart by
// advancing the sessions' CreatedAt well past AssignedSessionMaxAge (so the
// normal bind-poll would return nothing) and asserting the recovery query
// still yields them.
func TestMemory_ListBoundSessions_SimulatedRestartReDerivesPeers(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	provider := uuid.New()

	var ids []uuid.UUID
	for i := 0; i < 3; i++ {
		s := &Session{
			ID:              uuid.New(),
			CustomerID:      uuid.New(),
			Region:          "us-east-1",
			PrimaryProvider: provider,
			CurrentProvider: provider,
			// Older than the bring-up window — these are the long-lived
			// connections a restart would otherwise strand.
			CreatedAt:           time.Now().Add(-AssignedSessionMaxAge - time.Duration(i+1)*time.Minute),
			CustomerWgPublicKey: uuid.NewString(), // stand-in unique key per peer
			ProviderWgPublicKey: "cHJvdmlkZXJfa2V5",
		}
		if err := m.CreateSession(ctx, s); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		ids = append(ids, s.ID)
	}

	// Pre-restart sanity: the normal bind-poll returns NOTHING (all bound
	// + past the cutoff) — which is exactly why a restart strands them.
	assigned, err := m.ListAssignedSessions(ctx, provider)
	if err != nil {
		t.Fatalf("ListAssignedSessions: %v", err)
	}
	if len(assigned) != 0 {
		t.Fatalf("precondition: bind-poll should hide these bound+old sessions, got %d", len(assigned))
	}

	// Post-restart recovery: every live peer is re-derivable.
	bound, err := m.ListBoundSessions(ctx, provider)
	if err != nil {
		t.Fatalf("ListBoundSessions: %v", err)
	}
	if len(bound) != len(ids) {
		t.Fatalf("recovery query must re-derive all %d live peers, got %d", len(ids), len(bound))
	}
	keys := map[string]bool{}
	for _, s := range bound {
		if s.CustomerWgPublicKey == "" {
			t.Fatalf("recovered session %s has no customer key — nothing to upsert", s.ID)
		}
		keys[s.CustomerWgPublicKey] = true
	}
	if len(keys) != len(ids) {
		t.Fatalf("expected %d distinct customer keys to upsert, got %d", len(ids), len(keys))
	}
}
