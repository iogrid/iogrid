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

	// CleanupStaleSessions marks sessions terminated if they haven't
	// received a /refresh heartbeat in `staleAfter`. Customer SDKs
	// heartbeat every 30s by default, so a 5-minute threshold means
	// ~10 missed ticks before cleanup — generous enough that a
	// transient network hiccup on the bastion doesn't yank the session.
	// Returns the number of sessions cleaned up.
	CleanupStaleSessions(ctx context.Context, staleAfter time.Duration) (int, error)

	// --- Provider Health & Failover ---

	// RegisterProvider inserts (or refreshes) a provider row. Used at
	// daemon pairing time to seed the failover table; the in-cluster
	// pairing flow calls this once with a `healthy` row before the
	// daemon starts its `health` reporter (VPN-7, #511). Idempotent —
	// re-registering an existing provider id replaces region / status
	// / last_seen_at with the supplied values. SessionCount is reset
	// to zero only on first insert; subsequent calls leave it alone.
	RegisterProvider(ctx context.Context, p *ProviderInfo) error

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

	// --- WG Peer Binding (#536) ---

	// BindProviderToSession records the provider's WG public key for
	// a session. Called by the provider daemon after it allocates a
	// peer slot for the customer. Customer SDK then reads this via
	// GetSession to know which key to authenticate against.
	BindProviderToSession(ctx context.Context, sessionID uuid.UUID, providerWgPubKey string) error

	// BindCustomerWgKey records the customer's WG public key for a
	// session. Called by the customer SDK before connect so the daemon
	// can pre-allocate the peer ahead of the WG handshake.
	BindCustomerWgKey(ctx context.Context, sessionID uuid.UUID, customerWgPubKey string) error

	// ListAssignedSessions returns sessions assigned to a provider that
	// don't yet have ProviderWgPublicKey set. Daemon polls this every
	// ~5s to find new customers to allocate peer slots for.
	ListAssignedSessions(ctx context.Context, providerID uuid.UUID) ([]*Session, error)

	// ListRegions aggregates provider counts per region for the customer
	// region picker. Returns one entry per known region with healthy +
	// total counts. Empty regions are filtered out.
	ListRegions(ctx context.Context) ([]*RegionSummary, error)
}

// RegionSummary is one row of the customer region picker.
type RegionSummary struct {
	Region           string
	HealthyProviders int32
	TotalProviders   int32
}

// Session represents a VPN session in the ledger.
type Session struct {
	ID                  uuid.UUID
	CustomerID          uuid.UUID
	Region              string
	PrimaryProvider     uuid.UUID
	CurrentProvider     uuid.UUID
	State               pb.VpnSessionState
	BytesIn             uint64
	BytesOut            uint64
	RoamingEvents       int64
	FailoverCount       int64
	IceCandidates       int32
	IceTimeMs           int32
	WgEstablishMs       int32
	CreatedAt           time.Time
	TerminatedAt        *time.Time
	LastActivityAt      time.Time
	ExitReason          string
	// ProviderWgPublicKey is set by the provider daemon via BindProvider
	// once it has allocated a peer slot for this customer. The customer
	// SDK reads it via GetSession to know which key to authenticate against.
	ProviderWgPublicKey string
	// CustomerWgPublicKey is set by the customer SDK via ConfirmCandidate
	// so the provider daemon can pre-allocate the peer ahead of the WG
	// handshake landing.
	CustomerWgPublicKey string
}

// ProviderInfo represents a provider for regional failover selection.
type ProviderInfo struct {
	ID          uuid.UUID
	Region      string
	Status      string // "healthy", "degraded", "offline"
	LastSeenAt  time.Time
	SessionCount int32
}
