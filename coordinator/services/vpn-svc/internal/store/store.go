package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
)

// AssignedSessionMaxAge is the bring-up window for ListAssignedSessions:
// a session the daemon hasn't been able to bind within this window is an
// abandoned connect attempt (the mobile app's attempt lives ~30s), and
// returning it forever floods the binder log + grows the poll unbounded
// (#730). Generous so slow clients / retries still bind.
const AssignedSessionMaxAge = 15 * time.Minute

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

	// InvalidateSessionsOnProviderKeyChange compares newKey against the
	// provider's currently-stored wg_public_key and, when they differ
	// (and both are non-empty), terminates every still-active session
	// bound to that provider so the affected clients re-fetch the new
	// server key on their next Connect (G1 server-key recurrence, #762).
	//
	// Why this exists: load_or_generate_wg_private_key (daemon) mints a
	// FRESH server static key whenever /var/lib/iogridd has no wg.key —
	// a re-provisioned host, wiped state-dir, or volume-less container
	// silently gets a brand-new server pubkey. Clients that already
	// installed an NE tunnel toward that provider keep the OLD baked
	// server pubkey, so every handshake-init they send is MAC1-rejected
	// ("did not decapsulate") forever, because MAC1 is keyed on the
	// responder (server) static pubkey. Terminating their session forces
	// a clean reconnect: the mobile bring-up returns the NEW peer_public_key
	// (lookupProvider reads vpn_providers.wg_public_key) and #760's client
	// self-heal rebuilds the NE config against it.
	//
	// MUST be called BEFORE RegisterProvider persists the new key, so the
	// comparison still sees the prior value. Returns the number of sessions
	// terminated and whether a key change was detected. A no-op (returns
	// 0,false) when: the provider is new (no prior key), newKey is empty
	// (legacy daemon — preserves the cached key), or the key is unchanged.
	InvalidateSessionsOnProviderKeyChange(ctx context.Context, providerID uuid.UUID, newKey string) (terminated int, changed bool, err error)

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
	// ~5s to find new customers to allocate peer slots for. Sessions
	// older than AssignedSessionMaxAge are excluded: a session that
	// hasn't been bound within the bring-up window is an abandoned
	// connect attempt, and without the cutoff the daemon polls it
	// forever (#730 — 9 day-old zombies, every 5s, unbounded growth).
	ListAssignedSessions(ctx context.Context, providerID uuid.UUID) ([]*Session, error)

	// ListBoundSessions returns EVERY non-terminated session bound to
	// this provider whose customer_wg_public_key is set — WITHOUT the
	// ListAssignedSessions exclusions (no already-keyed filter, no
	// AssignedSessionMaxAge cutoff). It is the daemon's restart-recovery
	// query (#788): the binder calls it on startup + on a slow reconcile
	// tick to re-derive the live WG peer set after its in-memory boringtun
	// peer map was wiped by a restart.
	//
	// Why this is separate from ListAssignedSessions: a daemon restart
	// drops every per-customer peer Tunn out of boringtun's map. The
	// normal bind-poll deliberately HIDES already-bound + >15-min-old
	// sessions (#730), so a still-live customer who bound an hour ago is
	// invisible to it — and every WG handshake-init that customer sends
	// after the restart is dropped ("did not decapsulate against any
	// known peer") forever. Re-upserting the customers this query returns
	// repopulates the map. Unlike the #762 key-rotation case, the right
	// remedy here is RE-BIND, not terminate: the session is still valid,
	// only the daemon's volatile state was lost.
	//
	// Only the customer key + inner-IP are needed to upsert the peer, so
	// rows with an empty customer_wg_public_key (customer hasn't advertised
	// a key yet) are excluded — there is nothing to upsert for them, and
	// they are still covered by the live ListAssignedSessions bring-up path.
	ListBoundSessions(ctx context.Context, providerID uuid.UUID) ([]*Session, error)

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

	// --- Mobile session bring-up (#588 / #605) ---

	// AllocateInnerIP atomically allocates a per-session tunnel-inner
	// IPv4 address in the 10.66.X.Y/16 space where X derives from the
	// provider UUID's first byte and Y is drawn from a monotonic
	// counter on the vpn_provider_inner_ip_alloc table. Idempotent
	// on (providerID, sessionID): a second call with the same args
	// returns the previously-allocated IP rather than burning a new Y.
	// Returns the dotted-quad string (e.g. "10.66.42.3").
	AllocateInnerIP(ctx context.Context, providerID, sessionID uuid.UUID) (string, error)

	// PersistSessionPeerConfig writes the resolved peer endpoint +
	// public key onto the session row so a subsequent GetSession can
	// surface it to the mobile client without re-running peer
	// selection. Called by the mobile session handler after it picks
	// a provider via the geo-nearest picker.
	PersistSessionPeerConfig(ctx context.Context, sessionID uuid.UUID, peerPubKey, peerEndpoint string) error
}

// RegionSummary is one row of the customer region picker.
type RegionSummary struct {
	Region           string
	HealthyProviders int32
	TotalProviders   int32
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

	// ── Mobile session fields (#588 / #605) ──────────────────────
	//
	// The mobile PacketTunnelProvider needs a single-round-trip
	// session bring-up (POST /v1/vpn/sessions/mobile) that returns
	// the complete WG peer config. The four fields below carry that
	// payload; all are zero/empty for the legacy daemon-driven
	// session flow (which uses ICE candidates + bind-provider RPC
	// instead).

	// ClientPublicKey is the customer's WireGuard public key, base64
	// encoded. Sent by the mobile app in the start-session request;
	// kept here so the provider daemon can pre-allocate the peer
	// slot when it next polls /assigned-sessions.
	ClientPublicKey string

	// InnerIP is the per-session tunnel-inner IPv4 address the
	// mobile client uses inside the VPN namespace. Allocated by
	// vpn-svc atomically (AllocateInnerIP) at session-create time
	// in the 10.66.X.Y/16 range where X derives from the provider
	// UUID's first byte and Y comes from an atomic counter
	// (vpn_provider_inner_ip_alloc table). Empty for non-mobile
	// sessions.
	InnerIP string

	// ExpiresAt is the wall-clock deadline after which the mobile
	// client must call POST /sessions/{id}/heartbeat to renew its
	// $GRID payment authorization. Nil for non-$GRID sessions
	// (legacy daemon flow or paid-tier customers).
	ExpiresAt *time.Time

	// PaymentAuthorization is the opaque $GRID payment-authorization
	// blob the mobile client supplies at start-session time. Track 5
	// (#596) validates it against on-chain balance + signs an escrow
	// receipt; vpn-svc stores it as opaque bytes for now so the
	// handler contract is forward-compatible while #596 finishes.
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
