package store

import (
	"context"
	"fmt"
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
}

// NewMemory creates a new in-memory store.
func NewMemory() Store {
	return &Memory{
		sessions:   make(map[uuid.UUID]*Session),
		candidates: make(map[uuid.UUID][]*pb.IceCandidate),
		providers:  make(map[uuid.UUID]*ProviderInfo),
	}
}

// CreateSession implements Store.
func (m *Memory) CreateSession(ctx context.Context, session *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.sessions[session.ID]; exists {
		return fmt.Errorf("session %s already exists", session.ID)
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
	session.State = pb.VpnSessionState_TERMINATING
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
			session.State = pb.VpnSessionState_TERMINATING
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
func (m *Memory) RegisterProvider(ctx context.Context, p *ProviderInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.providers[p.ID]; ok {
		existing.Region = p.Region
		existing.Status = p.Status
		existing.LastSeenAt = p.LastSeenAt
		return nil
	}
	// Copy so the caller mutating their input doesn't poke the store.
	clone := *p
	m.providers[p.ID] = &clone
	return nil
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
	session.State = pb.VpnSessionState_FAILING_OVER
	session.LastActivityAt = time.Now()
	return nil
}
