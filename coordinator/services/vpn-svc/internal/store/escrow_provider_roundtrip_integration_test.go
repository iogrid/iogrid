//go:build integration

// Field-complete round-trip tests for the two remaining vpn-svc stores the
// #726 audit verified CLEAN by inspection but left without a regression
// guard: the $GRID payment escrow (vpn_session_escrow) and the provider
// registry (vpn_providers).
//
// The bug class (#709 / #725 / #732): the in-memory impl keeps the whole
// struct, so unit tests are green, while the Postgres INSERT/SELECT silently
// drops a column — the data is lost only in prod. These tests write a row
// with EVERY field populated, read it back through the real getter the
// handlers use against a REAL Postgres, and assert each field survives. A
// future column added to payment.Escrow / store.ProviderInfo but not to the
// SQL fails here.
//
// Fixture: shares newPostgresFixture / seedProviderRow from
// inner_ip_postgres_integration_test.go (same package) — skip-if-
// unreachable, wipe + re-apply the embedded vpn migrations per run. Point
// DATABASE_URL at a throwaway postgres:16 (testcontainers/podman); the
// fixture is destructive by design.
package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/payment"
)

// TestPostgresEscrow_RoundTrip locks every field of the $GRID escrow row.
// The escrow is the money record vpn-svc settles against (#596/#598) — a
// dropped column here (e.g. wallet_address, max_grid_per_min_atomic, nonce)
// is a settlement/security bug.
func TestPostgresEscrow_RoundTrip(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	es := NewPostgresEscrowStore(pool)
	ctx := context.Background()

	// vpn_session_escrow.session_id REFERENCES vpn_sessions(id) — seed both
	// the provider and a session so the FK is satisfied.
	providerID := uuid.New()
	seedProviderRow(t, ctx, p, providerID)
	sessionID := uuid.New()
	if err := p.CreateSession(ctx, &Session{
		ID:              sessionID,
		CustomerID:      uuid.New(),
		Region:          "us-east-1",
		PrimaryProvider: providerID,
		CurrentProvider: providerID,
		State:           pb.VpnSessionState_VPN_SESSION_STATE_CREATING,
		CreatedAt:       time.Now().UTC(),
		LastActivityAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	started := time.Now().UTC().Add(-10 * time.Minute).Truncate(time.Microsecond)
	heartbeat := time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Microsecond)
	want := &payment.Escrow{
		SessionID:           sessionID,
		CustomerID:          uuid.New(),
		WalletAddress:       "7gWxQ3iogridDevnetEscrowWalletXXXXXXXXXXXXXX",
		EscrowedAtomic:      5_000_000_000, // 5 $GRID (9 decimals)
		ConsumedAtomic:      1_234_567_890,
		MaxGRIDPerMinAtomic: 100_000_000,
		Nonce:               "nonce-roundtrip-0001",
		StartedAt:           started,
		LastHeartbeatAt:     heartbeat,
		// SettledAt is set via SettleEscrow, asserted in its own leg below.
	}
	if err := es.CreateEscrow(ctx, want); err != nil {
		t.Fatalf("CreateEscrow: %v", err)
	}

	got, err := es.GetEscrow(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetEscrow: %v", err)
	}
	if got.SessionID != want.SessionID {
		t.Errorf("SessionID = %v, want %v", got.SessionID, want.SessionID)
	}
	if got.CustomerID != want.CustomerID {
		t.Errorf("CustomerID = %v, want %v", got.CustomerID, want.CustomerID)
	}
	if got.WalletAddress != want.WalletAddress {
		t.Errorf("WalletAddress = %q, want %q", got.WalletAddress, want.WalletAddress)
	}
	if got.EscrowedAtomic != want.EscrowedAtomic {
		t.Errorf("EscrowedAtomic = %d, want %d", got.EscrowedAtomic, want.EscrowedAtomic)
	}
	if got.ConsumedAtomic != want.ConsumedAtomic {
		t.Errorf("ConsumedAtomic = %d, want %d", got.ConsumedAtomic, want.ConsumedAtomic)
	}
	if got.MaxGRIDPerMinAtomic != want.MaxGRIDPerMinAtomic {
		t.Errorf("MaxGRIDPerMinAtomic = %d, want %d", got.MaxGRIDPerMinAtomic, want.MaxGRIDPerMinAtomic)
	}
	if got.Nonce != want.Nonce {
		t.Errorf("Nonce = %q, want %q", got.Nonce, want.Nonce)
	}
	if !got.StartedAt.Equal(want.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", got.StartedAt, want.StartedAt)
	}
	if !got.LastHeartbeatAt.Equal(want.LastHeartbeatAt) {
		t.Errorf("LastHeartbeatAt = %v, want %v", got.LastHeartbeatAt, want.LastHeartbeatAt)
	}
	if got.SettledAt != nil {
		t.Errorf("fresh escrow SettledAt = %v, want nil", got.SettledAt)
	}

	// AddConsumption must round-trip the bumped consumed counter (the
	// per-heartbeat decrement path, #596).
	const delta uint64 = 500_000_000
	afterAdd, err := es.AddConsumption(ctx, sessionID, delta)
	if err != nil {
		t.Fatalf("AddConsumption: %v", err)
	}
	if afterAdd.ConsumedAtomic != want.ConsumedAtomic+delta {
		t.Errorf("ConsumedAtomic after add = %d, want %d", afterAdd.ConsumedAtomic, want.ConsumedAtomic+delta)
	}
	reread, err := es.GetEscrow(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetEscrow after add: %v", err)
	}
	if reread.ConsumedAtomic != want.ConsumedAtomic+delta {
		t.Errorf("persisted ConsumedAtomic = %d, want %d", reread.ConsumedAtomic, want.ConsumedAtomic+delta)
	}

	// SettleEscrow must stamp settled_at and have it survive read-back (the
	// field billing-svc keys the settlement webhook off, #597).
	if err := es.SettleEscrow(ctx, sessionID); err != nil {
		t.Fatalf("SettleEscrow: %v", err)
	}
	settled, err := es.GetEscrow(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetEscrow after settle: %v", err)
	}
	if settled.SettledAt == nil {
		t.Fatal("SettledAt nil after SettleEscrow — the #597 settlement webhook keys off it")
	}
}

// TestPostgresProvider_RoundTrip locks every field of a vpn_providers row
// through RegisterProvider → GetProvidersInRegion. WgPublicKey is the field
// the #570 mobile top-providers probe reads and the field #709's sibling
// (#710) hardened — a silent drop here regresses the latency-probe UX.
func TestPostgresProvider_RoundTrip(t *testing.T) {
	pool, cleanup := newPostgresFixture(t)
	defer cleanup()
	p := &Postgres{pool: pool}
	ctx := context.Background()

	lastSeen := time.Now().UTC().Add(-30 * time.Second).Truncate(time.Microsecond)
	want := &ProviderInfo{
		ID:           uuid.New(),
		Region:       "eu-central-1",
		Status:       "healthy",
		LastSeenAt:   lastSeen,
		SessionCount: 7,
		WgPublicKey:  "l2bXKoVtjk8dg5tCImHH1qz3LasZr1pkm47I30tuLgE=",
	}
	if err := p.RegisterProvider(ctx, want); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	list, err := p.GetProvidersInRegion(ctx, want.Region)
	if err != nil {
		t.Fatalf("GetProvidersInRegion: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("GetProvidersInRegion len = %d, want 1", len(list))
	}
	got := list[0]
	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.Region != want.Region {
		t.Errorf("Region = %q, want %q", got.Region, want.Region)
	}
	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}
	if !got.LastSeenAt.Equal(want.LastSeenAt) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, want.LastSeenAt)
	}
	if got.SessionCount != want.SessionCount {
		t.Errorf("SessionCount = %d, want %d", got.SessionCount, want.SessionCount)
	}
	if got.WgPublicKey != want.WgPublicKey {
		t.Errorf("WgPublicKey = %q, want %q — the #570 mobile probe reads it", got.WgPublicKey, want.WgPublicKey)
	}

	// Empty WgPublicKey is a valid legacy-daemon case (NULLIF stores NULL,
	// COALESCE reads ''). A re-register without a key must NOT blank an
	// already-captured key (the COALESCE-on-update guard documented on
	// RegisterProvider).
	reReg := *want
	reReg.WgPublicKey = "" // legacy re-register
	if err := p.RegisterProvider(ctx, &reReg); err != nil {
		t.Fatalf("re-RegisterProvider (legacy, no key): %v", err)
	}
	list2, err := p.GetProvidersInRegion(ctx, want.Region)
	if err != nil {
		t.Fatalf("GetProvidersInRegion after re-register: %v", err)
	}
	if len(list2) != 1 {
		t.Fatalf("after re-register len = %d, want 1", len(list2))
	}
	if list2[0].WgPublicKey != want.WgPublicKey {
		t.Errorf("a legacy re-register blanked the WG key: got %q, want %q (COALESCE guard)",
			list2[0].WgPublicKey, want.WgPublicKey)
	}
}
