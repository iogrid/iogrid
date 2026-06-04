package store

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
)

func TestMemoryStore_SessionLifecycle(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()

	sessionID := uuid.New()
	customerID := uuid.New()
	primaryProvider := uuid.New()

	session := &Session{
		ID:              sessionID,
		CustomerID:      customerID,
		Region:          "us-east-1",
		PrimaryProvider: primaryProvider,
		CurrentProvider: primaryProvider,
		State:           pb.VpnSessionState_VPN_SESSION_STATE_CREATING,
		CreatedAt:       time.Now(),
		LastActivityAt:  time.Now(),
	}

	// Create
	if err := st.CreateSession(ctx, session); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Get
	got, err := st.GetSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomerID != customerID {
		t.Errorf("expected customer %s, got %s", customerID, got.CustomerID)
	}

	// Update state
	if err := st.UpdateSessionState(ctx, sessionID, pb.VpnSessionState_VPN_SESSION_STATE_ACTIVE); err != nil {
		t.Fatalf("UpdateSessionState failed: %v", err)
	}

	got, _ = st.GetSession(ctx, sessionID)
	if got.State != pb.VpnSessionState_VPN_SESSION_STATE_ACTIVE {
		t.Errorf("expected state ACTIVE, got %v", got.State)
	}

	// Terminate
	if err := st.TerminateSession(ctx, sessionID, "user_initiated"); err != nil {
		t.Fatalf("TerminateSession failed: %v", err)
	}

	got, _ = st.GetSession(ctx, sessionID)
	if got.TerminatedAt == nil {
		t.Error("expected TerminatedAt to be set")
	}
	if got.ExitReason != "user_initiated" {
		t.Errorf("expected exit reason 'user_initiated', got %q", got.ExitReason)
	}
}

func TestMemoryStore_SelectProvider_NoneAvailable(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()

	_, err := st.SelectProviderForSession(ctx, "us-east-1")
	if err == nil {
		t.Error("expected error when no providers available, got nil")
	}
}

// TestMemoryStore_UnregisteredProviderNeverSelectable pins the safety
// invariant the daemon-side #694 fix depends on. A daemon with no real
// egress data plane (LoggingSink fallback — no CAP_NET_ADMIN/iptables)
// now WITHHOLDS RegisterProvider so vpn-svc can't route a session to a
// provider that would black-hole it. That fix is only sound if /register
// is the ONLY way into the selection pool: /health and /candidates must
// NOT create-on-missing. If a future change makes either of them upsert a
// provider row, this test fails and the #694 black-hole silently returns.
func TestMemoryStore_UnregisteredProviderNeverSelectable(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()

	ghost := uuid.New() // a daemon that withheld /register (no egress)

	// /health on an unregistered provider must error, never create a row.
	if err := st.UpdateProviderHealth(ctx, ghost, "healthy", time.Now()); err == nil {
		t.Error("UpdateProviderHealth created a provider from /health alone — reopens #694")
	}

	// /candidates must not make the provider selectable either (it writes
	// a separate map, keyed by providerID, with no provider-pool row).
	if err := st.RegisterCandidates(ctx, ghost, []*pb.IceCandidate{{}}); err != nil {
		t.Fatalf("RegisterCandidates failed: %v", err)
	}

	// With no RegisterProvider call, the provider is not selectable.
	if _, err := st.SelectProviderForSession(ctx, "us-east-1"); err == nil {
		t.Error("selected a provider that only sent /health + /candidates (never /register) — #694 black-hole")
	}
}

func TestMemoryStore_SelectProvider_RoundRobin(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	// Register 3 healthy providers in us-east-1
	providerA := uuid.New()
	providerB := uuid.New()
	providerC := uuid.New()
	st.providers[providerA] = &ProviderInfo{ID: providerA, Region: "us-east-1", Status: "healthy", SessionCount: 0}
	st.providers[providerB] = &ProviderInfo{ID: providerB, Region: "us-east-1", Status: "healthy", SessionCount: 0}
	st.providers[providerC] = &ProviderInfo{ID: providerC, Region: "us-east-1", Status: "healthy", SessionCount: 0}

	// One in different region — should NOT be selected
	providerD := uuid.New()
	st.providers[providerD] = &ProviderInfo{ID: providerD, Region: "us-west-2", Status: "healthy", SessionCount: 0}

	// First 3 selections should hit each provider once (round-robin by session_count)
	selections := make(map[uuid.UUID]int)
	for i := 0; i < 6; i++ {
		picked, err := st.SelectProviderForSession(ctx, "us-east-1")
		if err != nil {
			t.Fatalf("SelectProviderForSession failed at iteration %d: %v", i, err)
		}
		if picked == providerD {
			t.Errorf("picked us-west-2 provider for us-east-1 request")
		}
		selections[picked]++
	}

	// Each of A, B, C should have been picked 2 times (6 / 3)
	for _, p := range []uuid.UUID{providerA, providerB, providerC} {
		if selections[p] != 2 {
			t.Errorf("expected provider %s picked 2 times, got %d", p, selections[p])
		}
	}
}

func TestMemoryStore_SelectProvider_SkipsOffline(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	offlineProv := uuid.New()
	healthyProv := uuid.New()
	st.providers[offlineProv] = &ProviderInfo{ID: offlineProv, Region: "us-east-1", Status: "offline"}
	st.providers[healthyProv] = &ProviderInfo{ID: healthyProv, Region: "us-east-1", Status: "healthy"}

	picked, err := st.SelectProviderForSession(ctx, "us-east-1")
	if err != nil {
		t.Fatalf("SelectProviderForSession failed: %v", err)
	}
	if picked != healthyProv {
		t.Errorf("expected healthy provider, got offline provider")
	}
}

func TestMemoryStore_TriggerFailover(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()

	sessionID := uuid.New()
	primaryProv := uuid.New()
	altProv := uuid.New()

	session := &Session{
		ID:              sessionID,
		CustomerID:      uuid.New(),
		Region:          "us-east-1",
		PrimaryProvider: primaryProv,
		CurrentProvider: primaryProv,
		State:           pb.VpnSessionState_VPN_SESSION_STATE_ACTIVE,
		CreatedAt:       time.Now(),
		LastActivityAt:  time.Now(),
	}
	_ = st.CreateSession(ctx, session)

	// Trigger failover
	if err := st.TriggerFailover(ctx, sessionID, primaryProv, altProv); err != nil {
		t.Fatalf("TriggerFailover failed: %v", err)
	}

	got, _ := st.GetSession(ctx, sessionID)
	if got.CurrentProvider != altProv {
		t.Errorf("expected current provider %s, got %s", altProv, got.CurrentProvider)
	}
	if got.FailoverCount != 1 {
		t.Errorf("expected FailoverCount=1, got %d", got.FailoverCount)
	}
	if got.State != pb.VpnSessionState_VPN_SESSION_STATE_FAILING_OVER {
		t.Errorf("expected state FAILING_OVER, got %v", got.State)
	}
}

func TestMemoryStore_UpdateProviderHealth(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	providerID := uuid.New()
	st.providers[providerID] = &ProviderInfo{ID: providerID, Region: "us-east-1", Status: "healthy"}

	now := time.Now()
	if err := st.UpdateProviderHealth(ctx, providerID, "degraded", now); err != nil {
		t.Fatalf("UpdateProviderHealth failed: %v", err)
	}

	if st.providers[providerID].Status != "degraded" {
		t.Errorf("expected status 'degraded', got %q", st.providers[providerID].Status)
	}
	if !st.providers[providerID].LastSeenAt.Equal(now) {
		t.Errorf("expected LastSeenAt updated")
	}
}

func TestMemoryStore_GetProvidersInRegion_SkipsOffline(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	healthyA := uuid.New()
	degradedB := uuid.New()
	offlineC := uuid.New()
	wrongRegion := uuid.New()
	st.providers[healthyA] = &ProviderInfo{ID: healthyA, Region: "eu-west-1", Status: "healthy"}
	st.providers[degradedB] = &ProviderInfo{ID: degradedB, Region: "eu-west-1", Status: "degraded"}
	st.providers[offlineC] = &ProviderInfo{ID: offlineC, Region: "eu-west-1", Status: "offline"}
	st.providers[wrongRegion] = &ProviderInfo{ID: wrongRegion, Region: "us-east-1", Status: "healthy"}

	providers, err := st.GetProvidersInRegion(ctx, "eu-west-1")
	if err != nil {
		t.Fatalf("GetProvidersInRegion failed: %v", err)
	}

	if len(providers) != 2 {
		t.Errorf("expected 2 providers (healthy + degraded), got %d", len(providers))
	}

	// Make sure offline + wrong-region are not in the result
	for _, p := range providers {
		if p.Status == "offline" {
			t.Error("offline provider should not be returned")
		}
		if p.Region != "eu-west-1" {
			t.Errorf("wrong-region provider returned: %v", p.Region)
		}
	}
}

func TestMemoryStore_TerminateAllForCustomer(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()

	customer := uuid.New()
	other := uuid.New()
	prov := uuid.New()
	_ = st.RegisterProvider(ctx, &ProviderInfo{ID: prov, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now()})

	// Two active sessions for our customer + one already-terminated + one for another customer.
	mkActive := func(custID uuid.UUID) uuid.UUID {
		id := uuid.New()
		_ = st.CreateSession(ctx, &Session{
			ID: id, CustomerID: custID, Region: "us-east-1",
			PrimaryProvider: prov, CurrentProvider: prov,
			CreatedAt: time.Now(), LastActivityAt: time.Now(),
		})
		return id
	}
	s1 := mkActive(customer)
	s2 := mkActive(customer)
	sTerm := mkActive(customer)
	_ = st.TerminateSession(ctx, sTerm, "earlier")
	sOther := mkActive(other)

	n, err := st.TerminateAllForCustomer(ctx, customer, "user_logout")
	if err != nil {
		t.Fatalf("TerminateAllForCustomer: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 sessions terminated, got %d", n)
	}
	for _, id := range []uuid.UUID{s1, s2} {
		got, _ := st.GetSession(ctx, id)
		if got.TerminatedAt == nil {
			t.Errorf("session %s should be terminated", id)
		}
		if got.ExitReason != "user_logout" {
			t.Errorf("session %s exit reason = %q, want user_logout", id, got.ExitReason)
		}
	}
	// Other customer's session must be untouched.
	got, _ := st.GetSession(ctx, sOther)
	if got.TerminatedAt != nil {
		t.Errorf("other customer's session should NOT be terminated")
	}
	// Second call must be a no-op (returns 0 since nothing active).
	n, err = st.TerminateAllForCustomer(ctx, customer, "user_logout")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if n != 0 {
		t.Errorf("second call should yield 0 terminations, got %d", n)
	}
}

// TestMemoryStore_SelectTopProvidersInRegion exercises the #570 mobile-app
// probe path: it must return up to `limit` least-loaded HEALTHY providers
// in the region, each populated with wg_public_key + candidate set + median
// RTT computed from candidates discovered within the last hour.
func TestMemoryStore_SelectTopProvidersInRegion(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	pA, pB, pC, pDegraded, pWrongRegion := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	now := time.Now()
	st.providers[pA] = &ProviderInfo{ID: pA, Region: "us-east-1", Status: "healthy", SessionCount: 0, WgPublicKey: "keyA", LastSeenAt: now}
	st.providers[pB] = &ProviderInfo{ID: pB, Region: "us-east-1", Status: "healthy", SessionCount: 2, WgPublicKey: "keyB", LastSeenAt: now}
	st.providers[pC] = &ProviderInfo{ID: pC, Region: "us-east-1", Status: "healthy", SessionCount: 1, WgPublicKey: "keyC", LastSeenAt: now}
	st.providers[pDegraded] = &ProviderInfo{ID: pDegraded, Region: "us-east-1", Status: "degraded", SessionCount: 0, WgPublicKey: "keyDegraded", LastSeenAt: now}
	st.providers[pWrongRegion] = &ProviderInfo{ID: pWrongRegion, Region: "eu-west-1", Status: "healthy", SessionCount: 0, WgPublicKey: "keyEU", LastSeenAt: now}

	// Seed candidates with varying latencies (median of A's three samples
	// is 60, B has no samples → median 0).
	freshMs := now.UnixMilli()
	st.candidates[pA] = []*pb.IceCandidate{
		{Foundation: "1", Transport: "udp", CandidateType: "host", ConnectionAddress: "10.0.0.1", ConnectionPort: 51820, LatencyMs: 20, DiscoveredAtUnixMs: freshMs},
		{Foundation: "2", Transport: "udp", CandidateType: "srflx", ConnectionAddress: "1.2.3.4", ConnectionPort: 51820, LatencyMs: 60, DiscoveredAtUnixMs: freshMs},
		{Foundation: "3", Transport: "udp", CandidateType: "relay", ConnectionAddress: "5.6.7.8", ConnectionPort: 51820, LatencyMs: 200, DiscoveredAtUnixMs: freshMs},
	}
	st.candidates[pC] = []*pb.IceCandidate{
		{Foundation: "1", Transport: "udp", CandidateType: "host", ConnectionAddress: "10.0.0.5", ConnectionPort: 51820, LatencyMs: 40, DiscoveredAtUnixMs: freshMs},
	}
	// pB has no candidates registered.

	probes, err := st.SelectTopProvidersInRegion(ctx, "us-east-1", 3)
	if err != nil {
		t.Fatalf("SelectTopProvidersInRegion: %v", err)
	}
	if len(probes) != 3 {
		t.Fatalf("expected 3 probes (healthy in region), got %d", len(probes))
	}
	// Ordering: pA (load 0), pC (load 1), pB (load 2).
	wantOrder := []uuid.UUID{pA, pC, pB}
	for i, want := range wantOrder {
		if probes[i].ProviderID != want {
			t.Errorf("probe[%d].ProviderID = %s, want %s", i, probes[i].ProviderID, want)
		}
	}
	// pA's wg_public_key + median RTT.
	if probes[0].WgPublicKey != "keyA" {
		t.Errorf("probes[0].WgPublicKey = %q, want keyA", probes[0].WgPublicKey)
	}
	if probes[0].MedianRttMs != 60 {
		t.Errorf("probes[0].MedianRttMs = %d, want 60 (median of 20,60,200)", probes[0].MedianRttMs)
	}
	if len(probes[0].Candidates) != 3 {
		t.Errorf("probes[0] candidate_set len = %d, want 3", len(probes[0].Candidates))
	}
	// pB has no samples → median 0.
	if probes[2].MedianRttMs != 0 {
		t.Errorf("probes[2].MedianRttMs = %d, want 0 (no samples)", probes[2].MedianRttMs)
	}

	// Limit caps the result.
	probes2, err := st.SelectTopProvidersInRegion(ctx, "us-east-1", 1)
	if err != nil {
		t.Fatalf("SelectTopProvidersInRegion(limit=1): %v", err)
	}
	if len(probes2) != 1 || probes2[0].ProviderID != pA {
		t.Errorf("limit=1 should return [pA], got %d entries", len(probes2))
	}

	// Stale samples (older than 1h) must not count toward median.
	st.candidates[pA] = []*pb.IceCandidate{
		{Foundation: "1", Transport: "udp", CandidateType: "host", ConnectionAddress: "10.0.0.1", ConnectionPort: 51820, LatencyMs: 999, DiscoveredAtUnixMs: now.Add(-2 * time.Hour).UnixMilli()},
	}
	probes3, _ := st.SelectTopProvidersInRegion(ctx, "us-east-1", 1)
	if probes3[0].MedianRttMs != 0 {
		t.Errorf("stale samples should be excluded; got median %d", probes3[0].MedianRttMs)
	}
}

// TestMemoryStore_SelectProviderAcrossRegions verifies region=auto picks
// the least-loaded healthy provider across ALL regions (#570) and skips
// non-healthy + degraded rows.
func TestMemoryStore_SelectProviderAcrossRegions(t *testing.T) {
	ctx := context.Background()
	st := NewMemory().(*Memory)

	pUSEast := uuid.New()
	pEUWest := uuid.New()
	pAsia := uuid.New()
	pOffline := uuid.New()
	now := time.Now()
	st.providers[pUSEast] = &ProviderInfo{ID: pUSEast, Region: "us-east-1", Status: "healthy", SessionCount: 5, LastSeenAt: now}
	st.providers[pEUWest] = &ProviderInfo{ID: pEUWest, Region: "eu-west-1", Status: "healthy", SessionCount: 1, LastSeenAt: now}
	st.providers[pAsia] = &ProviderInfo{ID: pAsia, Region: "ap-south-1", Status: "healthy", SessionCount: 3, LastSeenAt: now}
	st.providers[pOffline] = &ProviderInfo{ID: pOffline, Region: "us-west-2", Status: "offline", SessionCount: 0, LastSeenAt: now}

	id, region, err := st.SelectProviderAcrossRegions(ctx, "203.0.113.10")
	if err != nil {
		t.Fatalf("SelectProviderAcrossRegions: %v", err)
	}
	if id != pEUWest {
		t.Errorf("got %s, want pEUWest (least-loaded healthy)", id)
	}
	if region != "eu-west-1" {
		t.Errorf("region = %q, want eu-west-1", region)
	}
	// Session count must increment.
	if st.providers[pEUWest].SessionCount != 2 {
		t.Errorf("session_count should be incremented, got %d", st.providers[pEUWest].SessionCount)
	}

	// Empty store → error.
	empty := NewMemory()
	if _, _, err := empty.SelectProviderAcrossRegions(ctx, ""); err == nil {
		t.Error("expected error on empty store, got nil")
	}
}

// TestMemoryStore_RegisterProviderWgPublicKey verifies that the
// wg_public_key is captured on register and preserved across a re-
// registration that omits it (legacy daemon path).
func TestMemoryStore_RegisterProviderWgPublicKey(t *testing.T) {
	ctx := context.Background()
	st := NewMemory()
	id := uuid.New()

	if err := st.RegisterProvider(ctx, &ProviderInfo{ID: id, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(), WgPublicKey: "originalkey"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	probes, _ := st.SelectTopProvidersInRegion(ctx, "us-east-1", 1)
	if len(probes) != 1 || probes[0].WgPublicKey != "originalkey" {
		t.Fatalf("expected key 'originalkey' after first register")
	}

	// Re-register without wg_public_key — must preserve previous.
	if err := st.RegisterProvider(ctx, &ProviderInfo{ID: id, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now()}); err != nil {
		t.Fatalf("legacy re-register: %v", err)
	}
	probes2, _ := st.SelectTopProvidersInRegion(ctx, "us-east-1", 1)
	if probes2[0].WgPublicKey != "originalkey" {
		t.Errorf("legacy re-register blanked wg key: got %q", probes2[0].WgPublicKey)
	}

	// Re-register WITH a new key — must overwrite.
	if err := st.RegisterProvider(ctx, &ProviderInfo{ID: id, Region: "us-east-1", Status: "healthy", LastSeenAt: time.Now(), WgPublicKey: "rotated"}); err != nil {
		t.Fatalf("rotate register: %v", err)
	}
	probes3, _ := st.SelectTopProvidersInRegion(ctx, "us-east-1", 1)
	if probes3[0].WgPublicKey != "rotated" {
		t.Errorf("rotate register did not overwrite: got %q", probes3[0].WgPublicKey)
	}
}
