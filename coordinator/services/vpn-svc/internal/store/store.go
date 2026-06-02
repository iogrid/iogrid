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

	// TerminateAllForCustomer marks every active session owned by the
	// customer as terminated. Used by /logout to cascade key-revoke (#549)
	// — without this a compromised key keeps serving traffic until the
	// stale-cleanup tick fires (5 min). Returns the count of sessions
	// terminated by this call (0 if none were active).
	TerminateAllForCustomer(ctx context.Context, customerID uuid.UUID, exitReason string) (int, error)

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

	// SelectTopProvidersInRegion returns up to `limit` providers in the
	// given region ordered by ascending session_count (least-loaded first),
	// joined with their fresh ICE candidates (expires_at > now()) and the
	// median candidate-discovery RTT over the last hour. Powers the
	// mobile-app top-providers endpoint (#570) — the iOS client probes
	// each one independently before committing to a session.
	SelectTopProvidersInRegion(ctx context.Context, region string, limit int) ([]*ProviderProbe, error)

	// SelectProviderAcrossRegions picks the best-scoring healthy provider
	// across ALL regions for a `region=auto` session request (#570). The
	// scoring is (session_count asc, region-affinity asc) — region
	// affinity is derived from the caller's IP via a simple country/AS
	// hint (`clientIPHint`) supplied by the handler from the
	// X-Forwarded-For header. When clientIPHint is empty (e.g. dev mode
	// behind no proxy), the scorer degenerates to "least-loaded across
	// all regions", which is still correct — just less locality-aware.
	// Returns the chosen provider's UUID + the region it lives in.
	SelectProviderAcrossRegions(ctx context.Context, clientIPHint string) (uuid.UUID, string, error)

	// --- Earnings batch (#547) ---

	// ListUnbilledTerminatedSessions returns sessions where
	// terminated_at IS NOT NULL AND billed_at IS NULL — the input
	// queue for the periodic billing batcher. Bounded by `limit` to
	// keep a single batch tick small.
	ListUnbilledTerminatedSessions(ctx context.Context, limit int) ([]*Session, error)

	// MarkSessionBilled stamps billed_at = now() once the batcher has
	// successfully published the usage event. Idempotent: a second call
	// is a no-op (billed_at column is not re-overwritten).
	MarkSessionBilled(ctx context.Context, sessionID uuid.UUID) error

	// --- Quota enforcement (#548) ---

	// SumCustomerBytesThisMonth returns total bytes_in + bytes_out
	// across every session this customer created since the start of the
	// current calendar month (UTC). The free-tier quota check on
	// RequestSession compares this against FreeTierQuotaBytes.
	SumCustomerBytesThisMonth(ctx context.Context, customerID uuid.UUID) (uint64, error)

	// --- Track 3 (#588): mobile-PacketTunnelProvider session config ---

	// AllocateInnerIP increments the per-provider suffix counter and
	// returns the next free 10.66.X.Y/32 host address for this session.
	// The 10.66.X.Y space is keyed by the provider's last-octet pair
	// (deterministic from provider UUID — see memory.go), so two
	// providers serving the same customer-side mesh don't collide.
	//
	// Returns the dotted-quad host (e.g. "10.66.42.2"). Wraparound at
	// .254 raises an error — at that point we need a /23 expansion,
	// which is a future-#596 problem.
	AllocateInnerIP(ctx context.Context, providerID uuid.UUID) (string, error)

	// PersistSessionPeerConfig stamps the Track-3-only fields onto an
	// existing session row in one go. Called after CreateSession +
	// AllocateInnerIP so the inner_ip / expires_at / client_public_key
	// / payment_authorization land atomically. Memory store ignores
	// payment_authorization opaque payload identity.
	PersistSessionPeerConfig(ctx context.Context, sessionID uuid.UUID,
		clientPubKey, innerIP string, expiresAt time.Time,
		paymentAuth []byte) error
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
	// BilledAt is stamped by the earnings batcher (#547) once the
	// session's bytes have been forwarded to billing-svc. Nil means
	// the session is either still active OR terminated-but-unbilled
	// (pending in the next batch tick).
	BilledAt *time.Time

	// --- Track 3 (#588): mobile PacketTunnelProvider session config ---
	// ClientPublicKey is the customer's WG public key (base64). On the
	// mobile flow this is generated in-app at startup and sent with
	// POST /v1/vpn/sessions. Distinct from CustomerWgPublicKey above
	// (which is the legacy daemon-bind path) so the mobile + daemon
	// flows can co-exist while we migrate.
	ClientPublicKey string
	// InnerIP is the customer-side IPv4 we allocated for this session.
	// Format: dotted-quad (we stash a /32 in the DB INET column but
	// surface the host address in Go for convenience). Empty when no
	// allocation has been done yet (e.g. legacy sessions).
	InnerIP string
	// ExpiresAt is the wall-clock TTL on the session. The mobile app
	// requests 24h; the cleanup loop will terminate any session that
	// outlives this timestamp regardless of heartbeat liveness. Zero
	// = no expiry set (legacy sessions).
	ExpiresAt time.Time
	// PaymentAuthorization is the raw JSON payload Track 5 (#596) will
	// validate. vpn-svc only persists it for forward-compat; do not
	// rely on its contents in Track 3 code paths. Empty bytes = no
	// authorization sent (free-tier flow).
	PaymentAuthorization []byte
}

// ProviderInfo represents a provider for regional failover selection.
type ProviderInfo struct {
	ID           uuid.UUID
	Region       string
	Status       string // "healthy", "degraded", "offline"
	LastSeenAt   time.Time
	SessionCount int32
	// WgPublicKey is the daemon's static WireGuard public key (base64).
	// Populated at /v1/vpn/providers/{id}/register time when the daemon
	// includes it in the body. The mobile-app top-providers endpoint
	// (#570) returns it so the client can latency-probe BEFORE creating
	// a session. Empty string is valid (legacy daemons that pre-date the
	// schema bump).
	WgPublicKey string
}

// ProviderProbe is one entry of the mobile-app top-providers response —
// the wire shape exposed by GET /v1/vpn/regions/{region}/providers?limit=N
// (#570). Mirrors the proto ProviderProbe but kept in the store layer so
// the SQL path can SELECT the median_rtt_ms aggregate without round-
// tripping through proto. Candidates are the same shape the existing
// GetProviderCandidates returns (fresh = expires_at > now()).
type ProviderProbe struct {
	ProviderID  uuid.UUID
	WgPublicKey string
	Candidates  []*pb.IceCandidate
	MedianRttMs uint32
}
