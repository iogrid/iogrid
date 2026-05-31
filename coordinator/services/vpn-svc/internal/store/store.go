package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
)

// Store defines the interface for VPN session and ICE candidate persistence.
type Store interface {
	// --- VPN Sessions ---

	// CreateSession creates a new VPN session.
	CreateSession(ctx context.Context, session *Session) error

	// GetSession retrieves a session by ID.
	GetSession(ctx context.Context, sessionID uuid.UUID) (*Session, error)

	// UpdateSessionState updates the session state.
	UpdateSessionState(ctx context.Context, sessionID uuid.UUID, state pb.VpnSessionState) error

	// UpdateSessionMetrics updates bytes/roaming/failover counts.
	UpdateSessionMetrics(ctx context.Context, sessionID uuid.UUID, bytesIn, bytesOut uint64, roamingEvents, failoverCount int64) error

	// TerminateSession marks a session as terminated.
	TerminateSession(ctx context.Context, sessionID uuid.UUID, exitReason string) error

	// ListActiveSessionsByRegion lists all active sessions in a region.
	ListActiveSessionsByRegion(ctx context.Context, region string) ([]*Session, error)

	// ListSessionsByCustomer lists all sessions for a customer (active + terminated).
	ListSessionsByCustomer(ctx context.Context, customerID uuid.UUID) ([]*Session, error)

	// --- ICE Candidates ---

	// RegisterCandidates registers ICE candidates for a provider.
	RegisterCandidates(ctx context.Context, providerID uuid.UUID, candidates []*pb.IceCandidate) error

	// GetProviderCandidates retrieves all non-expired candidates for a provider.
	GetProviderCandidates(ctx context.Context, providerID uuid.UUID) ([]*pb.IceCandidate, error)

	// ConfirmWorkingCandidate marks a candidate as the preferred one for a session.
	ConfirmWorkingCandidate(ctx context.Context, sessionID uuid.UUID, candidate *pb.IceCandidate) error

	// CleanupExpiredCandidates deletes expired ICE candidates.
	CleanupExpiredCandidates(ctx context.Context) error

	// --- Provider Health & Failover ---

	// GetProvidersInRegion retrieves all healthy providers in a region for failover.
	GetProvidersInRegion(ctx context.Context, region string) ([]*ProviderInfo, error)

	// SelectProviderForSession picks a provider for a new session (round-robin or health-based).
	SelectProviderForSession(ctx context.Context, region string) (uuid.UUID, error)

	// SelectAlternateProvider picks a provider in the same region but excluding the listed IDs.
	// Used by failover to avoid re-selecting the provider that just failed.
	SelectAlternateProvider(ctx context.Context, region string, exclude []uuid.UUID) (uuid.UUID, error)

	// UpdateProviderHealth updates provider status (healthy/degraded/offline).
	UpdateProviderHealth(ctx context.Context, providerID uuid.UUID, status string, lastSeen time.Time) error

	// TriggerFailover switches a session to an alternate provider.
	TriggerFailover(ctx context.Context, sessionID uuid.UUID, currentProvider, altProvider uuid.UUID) error
}

// Session represents a VPN session in the ledger.
type Session struct {
	ID              uuid.UUID
	CustomerID      uuid.UUID
	Region          string
	PrimaryProvider uuid.UUID
	CurrentProvider uuid.UUID
	State           pb.VpnSessionState
	BytesIn         uint64
	BytesOut        uint64
	RoamingEvents   int64
	FailoverCount   int64
	IceCandidates   int32
	IceTimeMs       int32
	WgEstablishMs   int32
	CreatedAt       time.Time
	TerminatedAt    *time.Time
	LastActivityAt  time.Time
	ExitReason      string
}

// ProviderInfo represents a provider for regional failover selection.
type ProviderInfo struct {
	ID          uuid.UUID
	Region      string
	Status      string // "healthy", "degraded", "offline"
	LastSeenAt  time.Time
	SessionCount int32
}
