package store

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
)

// Memory is an in-memory implementation of Store (for development/testing).
type Memory struct {
	mu         sync.RWMutex
	sessions   map[uuid.UUID]*Session
	candidates map[uuid.UUID][]*pb.IceCandidate // provider_id -> candidates
	providers  map[uuid.UUID]*ProviderInfo      // provider_id -> info

	// innerIPAlloc tracks the next free Y in 10.66.X.Y for each
	// provider (#605). Keyed by (providerID, sessionID) for
	// idempotency — a second AllocateInnerIP call with the same
	// args returns the previously-allocated value instead of
	// burning a new Y.
	innerIPNext  map[uuid.UUID]uint8 // provider_id → next Y counter (2..253; 0/1/254/255 reserved)
	innerIPAlloc map[string]string   // "provider:session" → "10.66.X.Y"
}

// NewMemory creates a new in-memory store.
func NewMemory() Store {
	return &Memory{
		sessions:     make(map[uuid.UUID]*Session),
		candidates:   make(map[uuid.UUID][]*pb.IceCandidate),
		providers:    make(map[uuid.UUID]*ProviderInfo),
		innerIPNext:  make(map[uuid.UUID]uint8),
		innerIPAlloc: make(map[string]string),
	}
}

// CreateSession implements Store.
func (m *Memory) CreateSession(ctx context.Context, session *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sessions[session.ID]; exists {
		return fmt.Errorf("session %s already exists", session.ID)
	}
	if session.CreatedAt.IsZero() {
		// Parity with the Postgres schema's `created_at DEFAULT now()` —
		// without this a zero CreatedAt reads as ancient and the #730
		// bring-up cutoff would hide the session from the daemon poll.
		session.CreatedAt = time.Now()
	}
	m.sessions[session.ID] = session
	return nil
}

// GetSession implements Store.
func (m *Memory) GetSession(ctx context.Context, sessionID uuid.UUID) (*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, exists := m.sessions[sessionID]
	if !exists {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	return session, nil
}

// UpdateSessionState implements Store.
func (m *Memory) UpdateSessionState(ctx context.Context, sessionID uuid.UUID, state pb.VpnSessionState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.State = state
	session.LastActivityAt = time.Now()
	return nil
}

// UpdateSessionMetrics implements Store.
func (m *Memory) UpdateSessionMetrics(ctx context.Context, sessionID uuid.UUID, bytesIn, bytesOut uint64, roamingEvents, failoverCount int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.BytesIn = bytesIn
	session.BytesOut = bytesOut
	session.RoamingEvents = roamingEvents
	session.FailoverCount = failoverCount
	session.LastActivityAt = time.Now()
	return nil
}

// TerminateSession implements Store.
func (m *Memory) TerminateSession(ctx context.Context, sessionID uuid.UUID, exitReason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}
	now := time.Now()
	session.TerminatedAt = &now
	session.ExitReason = exitReason
	session.State = pb.VpnSessionState_VPN_SESSION_STATE_TERMINATING
	return nil
}

// ListActiveSessionsByRegion implements Store.
func (m *Memory) ListActiveSessionsByRegion(ctx context.Context, region string) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Session
	for _, session := range m.sessions {
		if session.Region == region && session.TerminatedAt == nil {
			result = append(result, session)
		}
	}
	return result, nil
}

// ListSessionsByCustomer implements Store.
func (m *Memory) ListSessionsByCustomer(ctx context.Context, customerID uuid.UUID) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Session
	for _, session := range m.sessions {
		if session.CustomerID == customerID {
			result = append(result, session)
		}
	}
	return result, nil
}

// RegisterCandidates implements Store.
func (m *Memory) RegisterCandidates(ctx context.Context, providerID uuid.UUID, candidates []*pb.IceCandidate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Replace previous candidates with new ones
	m.candidates[providerID] = candidates
	return nil
}

// GetProviderCandidates implements Store.
func (m *Memory) GetProviderCandidates(ctx context.Context, providerID uuid.UUID) ([]*pb.IceCandidate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	candidates, exists := m.candidates[providerID]
	if !exists {
		return []*pb.IceCandidate{}, nil
	}
	// Filter out expired candidates (in-memory doesn't expire, just return all)
	return candidates, nil
}

// ConfirmWorkingCandidate implements Store.
func (m *Memory) ConfirmWorkingCandidate(ctx context.Context, sessionID uuid.UUID, candidate *pb.IceCandidate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if candidate != nil {
		candidate.IsPreferred = true
		session.IceTimeMs = int32(time.Since(time.UnixMilli(candidate.DiscoveredAtUnixMs)).Milliseconds())
	}
	return nil
}

// CleanupExpiredCandidates implements Store.
func (m *Memory) CleanupExpiredCandidates(ctx context.Context) error {
	// In-memory store doesn't actually expire candidates
	return nil
}

// CleanupStaleSessions implements Store.
func (m *Memory) CleanupStaleSessions(ctx context.Context, staleAfter time.Duration) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := time.Now().Add(-staleAfter)
	cleaned := 0
	for _, session := range m.sessions {
		if session.TerminatedAt != nil {
			continue
		}
		if session.LastActivityAt.Before(cutoff) {
			now := time.Now()
			session.TerminatedAt = &now
			session.ExitReason = "stale_heartbeat"
			session.State = pb.VpnSessionState_VPN_SESSION_STATE_TERMINATING
			cleaned++
		}
	}
	return cleaned, nil
}

// SeedProvider is a test helper that injects a provider into the in-memory store.
// Production code paths register providers via dedicated registration RPCs (TBD).
func (m *Memory) SeedProvider(id uuid.UUID, region, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.providers[id] = &ProviderInfo{
		ID:         id,
		Region:     region,
		Status:     status,
		LastSeenAt: time.Now(),
	}
}

// GetProvidersInRegion implements Store.
func (m *Memory) GetProvidersInRegion(ctx context.Context, region string) ([]*ProviderInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*ProviderInfo
	for _, provider := range m.providers {
		if provider.Region == region && provider.Status != "offline" {
			result = append(result, provider)
		}
	}
	return result, nil
}

// SelectProviderForSession implements Store.
// Round-robin selection among healthy providers in region.
func (m *Memory) SelectProviderForSession(ctx context.Context, region string) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var healthy []*ProviderInfo
	for _, provider := range m.providers {
		if provider.Region == region && provider.Status == "healthy" {
			healthy = append(healthy, provider)
		}
	}
	if len(healthy) == 0 {
		return uuid.Nil, fmt.Errorf("no healthy providers in region %s", region)
	}
	// Simple round-robin: pick the one with fewest sessions
	selected := healthy[0]
	for _, p := range healthy[1:] {
		if p.SessionCount < selected.SessionCount {
			selected = p
		}
	}
	selected.SessionCount++
	selected.LastSeenAt = time.Now()
	return selected.ID, nil
}

// RegisterProvider implements Store. Idempotent — re-registering an
// existing provider preserves its SessionCount and replaces the rest.
// WgPublicKey is only overwritten when the new value is non-empty so a
// legacy daemon doesn't blank out a previously-registered key.
func (m *Memory) RegisterProvider(ctx context.Context, p *ProviderInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.providers[p.ID]; ok {
		existing.Region = p.Region
		existing.Status = p.Status
		existing.LastSeenAt = p.LastSeenAt
		if p.WgPublicKey != "" {
			existing.WgPublicKey = p.WgPublicKey
		}
		return nil
	}
	// Copy so the caller mutating their input doesn't poke the store.
	clone := *p
	m.providers[p.ID] = &clone
	return nil
}

// InvalidateSessionsOnProviderKeyChange implements Store (#762). Mirrors the
// Postgres CTE: when the daemon re-registers with a server pubkey that differs
// from the one we already hold, every still-active session bound to that
// provider is terminated so the affected clients reconnect against the new key
// (load_or_generate_wg_private_key can mint a fresh server identity on an empty
// state-dir — see the interface doc). Must be called BEFORE RegisterProvider
// persists the new key.
func (m *Memory) InvalidateSessionsOnProviderKeyChange(ctx context.Context, providerID uuid.UUID, newKey string) (int, bool, error) {
	if strings.TrimSpace(newKey) == "" {
		return 0, false, nil // legacy daemon; nothing to compare
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.providers[providerID]
	if !ok || existing.WgPublicKey == "" {
		// First register (or a provider that never published a key):
		// no prior identity to diverge from.
		return 0, false, nil
	}
	if existing.WgPublicKey == newKey {
		return 0, false, nil // unchanged
	}
	// Key rotated — terminate every live session pinned to this provider.
	now := time.Now()
	terminated := 0
	for _, s := range m.sessions {
		if s.TerminatedAt != nil {
			continue
		}
		if s.CurrentProvider != providerID && s.PrimaryProvider != providerID {
			continue
		}
		t := now
		s.TerminatedAt = &t
		s.ExitReason = "provider_key_rotated"
		s.State = pb.VpnSessionState_VPN_SESSION_STATE_TERMINATING
		s.LastActivityAt = now
		terminated++
	}
	return terminated, true, nil
}

// SelectTopProvidersInRegion implements Store.
func (m *Memory) SelectTopProvidersInRegion(ctx context.Context, region string, limit int) ([]*ProviderProbe, error) {
	if limit <= 0 {
		limit = 3
	}
	m.mu.RLock()
	// Snapshot healthy providers in region.
	candidates := make([]*ProviderInfo, 0)
	for _, p := range m.providers {
		if p.Region == region && p.Status == "healthy" {
			candidates = append(candidates, p)
		}
	}
	// Snapshot candidate sets so we can release the read lock before
	// returning (callers may iterate at their own pace).
	candSnap := make(map[uuid.UUID][]*pb.IceCandidate, len(candidates))
	for _, p := range candidates {
		if cs, ok := m.candidates[p.ID]; ok {
			candSnap[p.ID] = cs
		}
	}
	m.mu.RUnlock()

	// Sort by ascending session_count (least-loaded first) — same
	// heuristic as SelectProviderForSession.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].SessionCount < candidates[j].SessionCount
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	out := make([]*ProviderProbe, 0, len(candidates))
	hourAgoMs := time.Now().Add(-1 * time.Hour).UnixMilli()
	for _, p := range candidates {
		probe := &ProviderProbe{
			ProviderID:  p.ID,
			WgPublicKey: p.WgPublicKey,
			Candidates:  candSnap[p.ID],
		}
		// Median RTT over the last hour of candidate-discovery samples.
		latencies := make([]uint32, 0, len(probe.Candidates))
		for _, c := range probe.Candidates {
			if c.LatencyMs > 0 && c.DiscoveredAtUnixMs >= hourAgoMs {
				latencies = append(latencies, c.LatencyMs)
			}
		}
		probe.MedianRttMs = medianUint32(latencies)
		out = append(out, probe)
	}
	return out, nil
}

// SelectProviderAcrossRegions implements Store.
//
// Memory store ignores clientIPHint (geo lookup is a postgres-side
// concern); least-loaded across regions is sufficient for tests +
// dev mode.
func (m *Memory) SelectProviderAcrossRegions(ctx context.Context, clientIPHint string) (uuid.UUID, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var selected *ProviderInfo
	for _, p := range m.providers {
		if p.Status != "healthy" {
			continue
		}
		if selected == nil || p.SessionCount < selected.SessionCount {
			selected = p
		}
	}
	if selected == nil {
		return uuid.Nil, "", fmt.Errorf("no healthy providers across regions")
	}
	selected.SessionCount++
	selected.LastSeenAt = time.Now()
	return selected.ID, selected.Region, nil
}

// medianUint32 returns the median of the slice (0 if empty). For an
// even-length slice it averages the two middle values, matching the
// SQL definition used in the Postgres path.
func medianUint32(xs []uint32) uint32 {
	n := len(xs)
	if n == 0 {
		return 0
	}
	sorted := make([]uint32, n)
	copy(sorted, xs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// SelectAlternateProvider implements Store.
func (m *Memory) SelectAlternateProvider(ctx context.Context, region string, exclude []uuid.UUID) (uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	excludeSet := make(map[uuid.UUID]bool, len(exclude))
	for _, id := range exclude {
		excludeSet[id] = true
	}
	var selected *ProviderInfo
	for _, provider := range m.providers {
		if provider.Region != region || provider.Status != "healthy" {
			continue
		}
		if excludeSet[provider.ID] {
			continue
		}
		if selected == nil || provider.SessionCount < selected.SessionCount {
			selected = provider
		}
	}
	if selected == nil {
		return uuid.Nil, fmt.Errorf("no alternate healthy provider in region %s (excluded %d)", region, len(exclude))
	}
	selected.SessionCount++
	selected.LastSeenAt = time.Now()
	return selected.ID, nil
}

// UpdateProviderHealth implements Store.
func (m *Memory) UpdateProviderHealth(ctx context.Context, providerID uuid.UUID, status string, lastSeen time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	provider, exists := m.providers[providerID]
	if !exists {
		return fmt.Errorf("provider %s not found", providerID)
	}
	provider.Status = status
	provider.LastSeenAt = lastSeen
	return nil
}

// BindProviderToSession implements Store.
func (m *Memory) BindProviderToSession(ctx context.Context, sessionID uuid.UUID, providerWgPubKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.ProviderWgPublicKey = providerWgPubKey
	session.LastActivityAt = time.Now()
	return nil
}

// BindCustomerWgKey implements Store.
func (m *Memory) BindCustomerWgKey(ctx context.Context, sessionID uuid.UUID, customerWgPubKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.CustomerWgPublicKey = customerWgPubKey
	session.LastActivityAt = time.Now()
	return nil
}

// TerminateAllForCustomer implements Store.
func (m *Memory) TerminateAllForCustomer(ctx context.Context, customerID uuid.UUID, exitReason string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	n := 0
	for _, s := range m.sessions {
		if s.CustomerID == customerID && s.TerminatedAt == nil {
			t := now
			s.TerminatedAt = &t
			s.ExitReason = exitReason
			s.LastActivityAt = now
			n++
		}
	}
	return n, nil
}

// ListUnbilledTerminatedSessions implements Store.
func (m *Memory) ListUnbilledTerminatedSessions(ctx context.Context, limit int) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Session, 0)
	for _, s := range m.sessions {
		if s.TerminatedAt != nil && s.BilledAt == nil {
			out = append(out, s)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// SumCustomerBytesThisMonth implements Store.
func (m *Memory) SumCustomerBytesThisMonth(ctx context.Context, customerID uuid.UUID) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var total uint64
	for _, s := range m.sessions {
		if s.CustomerID == customerID && !s.CreatedAt.Before(monthStart) {
			total += s.BytesIn + s.BytesOut
		}
	}
	return total, nil
}

// MarkSessionBilled implements Store.
func (m *Memory) MarkSessionBilled(ctx context.Context, sessionID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if session.BilledAt != nil {
		return nil
	}
	now := time.Now()
	session.BilledAt = &now
	return nil
}

// ListRegions implements Store.
func (m *Memory) ListRegions(ctx context.Context) ([]*RegionSummary, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	byRegion := make(map[string]*RegionSummary)
	for _, p := range m.providers {
		s, ok := byRegion[p.Region]
		if !ok {
			s = &RegionSummary{Region: p.Region}
			byRegion[p.Region] = s
		}
		s.TotalProviders++
		if p.Status == "healthy" {
			s.HealthyProviders++
		}
	}
	out := make([]*RegionSummary, 0, len(byRegion))
	for _, s := range byRegion {
		out = append(out, s)
	}
	return out, nil
}

// ListAssignedSessions implements Store.
func (m *Memory) ListAssignedSessions(ctx context.Context, providerID uuid.UUID) ([]*Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []*Session
	cutoff := time.Now().Add(-AssignedSessionMaxAge)
	for _, session := range m.sessions {
		if session.TerminatedAt != nil {
			continue
		}
		if session.CurrentProvider != providerID {
			continue
		}
		if session.ProviderWgPublicKey != "" {
			continue // already bound
		}
		if session.CreatedAt.Before(cutoff) {
			continue // abandoned bring-up — never bound, don't poll forever (#730)
		}
		result = append(result, session)
	}
	return result, nil
}

// TriggerFailover implements Store.
func (m *Memory) TriggerFailover(ctx context.Context, sessionID uuid.UUID, currentProvider, altProvider uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, exists := m.sessions[sessionID]
	if !exists {
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.CurrentProvider = altProvider
	session.FailoverCount++
	session.State = pb.VpnSessionState_VPN_SESSION_STATE_FAILING_OVER
	session.LastActivityAt = time.Now()
	return nil
}

// AllocateInnerIP implements Store. The in-memory variant is the
// authoritative implementation for unit tests and dev mode; the
// Postgres variant (postgres.go) uses an INSERT … ON CONFLICT DO
// UPDATE … RETURNING against vpn_provider_inner_ip_alloc to get the
// same atomicity across replicas. (#605)
func (m *Memory) AllocateInnerIP(ctx context.Context, providerID, sessionID uuid.UUID) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := providerID.String() + ":" + sessionID.String()
	if existing, ok := m.innerIPAlloc[key]; ok {
		return existing, nil
	}
	// X is the provider UUID's first byte, clamped to [2, 253] so we
	// avoid 10.66.0.0/24 (reserved for tooling) and 10.66.255.0/24
	// (broadcast space). Provider UUIDs are uniformly distributed so
	// the X distribution across providers is also uniform.
	x := providerID[0]
	if x < 2 {
		x = 2
	}
	if x > 253 {
		x = 253
	}
	// Y is monotonic per provider; allocate from a counter that starts
	// at 2 (we reserve .0 broadcast, .1 gateway).
	y := m.innerIPNext[providerID]
	if y < 2 {
		y = 2
	}
	if y >= 254 {
		// Provider's /24 is exhausted. In production this signals
		// "rotate to next provider"; in memory we just error.
		return "", fmt.Errorf("inner-IP space exhausted for provider %s", providerID)
	}
	ip := fmt.Sprintf("10.66.%d.%d", x, y)
	m.innerIPNext[providerID] = y + 1
	m.innerIPAlloc[key] = ip
	return ip, nil
}

// PersistSessionPeerConfig implements Store. Writes the resolved peer
// public key + endpoint onto the session row. Idempotent — repeated
// calls overwrite with the latest values (mobile flow may re-resolve
// the peer on $GRID re-authorization).
func (m *Memory) PersistSessionPeerConfig(ctx context.Context, sessionID uuid.UUID, peerPubKey, peerEndpoint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	session.ProviderWgPublicKey = peerPubKey
	// Endpoint isn't a top-level field on Session; the mobile flow
	// uses ProviderWgPublicKey for the WG handshake + reads the
	// endpoint from the provider record. Keep peerEndpoint param in
	// the API for forward-compat (Postgres impl will persist it on
	// a new vpn_sessions.peer_endpoint column). For in-memory we
	// drop it — tests assert on ProviderWgPublicKey only.
	_ = peerEndpoint
	return nil
}
