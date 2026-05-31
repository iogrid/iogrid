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
}

// NewMemory creates a new in-memory store.
func NewMemory() Store {
	return &Memory{
		sessions:   make(map[uuid.UUID]*Session),
		candidates: make(map[uuid.UUID][]*pb.IceCandidate),
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
