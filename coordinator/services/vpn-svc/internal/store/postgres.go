package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is a Postgres-backed implementation of Store.
type Postgres struct {
	pool *pgxpool.Pool
}

// NewPostgres creates a new Postgres store.
func NewPostgres(pool *pgxpool.Pool) Store {
	return &Postgres{pool: pool}
}

// CreateSession implements Store.
func (p *Postgres) CreateSession(ctx context.Context, session *Session) error {
	// #701: persist the peer-config columns AT CREATE time. The mobile
	// flow (#698) sets CustomerWgPublicKey + InnerIP on the session at
	// creation so the daemon's binder — which polls /assigned-sessions and
	// upserts the customer as a WG peer — can read them. The previous
	// INSERT wrote only the 8 identity/state columns and SILENTLY DROPPED
	// customer_wg_public_key / client_public_key / inner_ip / expires_at.
	// The in-memory store kept the whole struct (so unit tests passed), but
	// on Postgres /assigned-sessions returned an empty customer key → the
	// provider never added the device peer → the mobile WG handshake got no
	// response → "Resolving peer" failed for every real (Postgres) session.
	query := `
		INSERT INTO vpn_sessions (
			id, customer_id, region, primary_provider_id, current_provider_id,
			state, created_at, last_activity_at,
			customer_wg_public_key, client_public_key, inner_ip, expires_at,
			payment_authorization
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13
		)
	`
	// inner_ip is INET — an empty string is not a valid INET literal, so
	// pass NULL for the legacy (non-mobile) sessions that don't set it.
	var innerIP any
	if session.InnerIP != "" {
		innerIP = session.InnerIP
	}
	// payment_authorization is JSONB — empty bytes are not valid JSON, so
	// pass NULL for sessions without an escrow body (#726 audit: this
	// column was silently dropped by the INSERT, the #709 bug class).
	var paymentAuth any
	if len(session.PaymentAuthorization) > 0 {
		paymentAuth = session.PaymentAuthorization
	}
	_, err := p.pool.Exec(ctx, query,
		session.ID,
		session.CustomerID,
		session.Region,
		session.PrimaryProvider,
		session.CurrentProvider,
		session.State.String(),
		session.CreatedAt,
		session.LastActivityAt,
		session.CustomerWgPublicKey,
		session.ClientPublicKey,
		innerIP,
		session.ExpiresAt,
		paymentAuth,
	)
	return err
}

// GetSession implements Store.
func (p *Postgres) GetSession(ctx context.Context, sessionID uuid.UUID) (*Session, error) {
	query := `
		SELECT id, customer_id, region, primary_provider_id, current_provider_id,
		       state, bytes_in, bytes_out, roaming_events, failover_count,
		       ice_candidate_count, ice_time_ms, wg_establish_time_ms,
		       created_at, terminated_at, last_activity_at, exit_reason,
		       COALESCE(provider_wg_public_key, ''), COALESCE(customer_wg_public_key, ''),
		       billed_at, COALESCE(host(inner_ip), ''), payment_authorization
		FROM vpn_sessions
		WHERE id = $1
	`
	row := p.pool.QueryRow(ctx, query, sessionID)
	return p.scanSession(row)
}

// UpdateSessionState implements Store.
func (p *Postgres) UpdateSessionState(ctx context.Context, sessionID uuid.UUID, state pb.VpnSessionState) error {
	query := `
		UPDATE vpn_sessions
		SET state = $1, last_activity_at = NOW()
		WHERE id = $2
	`
	_, err := p.pool.Exec(ctx, query, state.String(), sessionID)
	return err
}

// UpdateSessionMetrics implements Store.
func (p *Postgres) UpdateSessionMetrics(ctx context.Context, sessionID uuid.UUID, bytesIn, bytesOut uint64, roamingEvents, failoverCount int64) error {
	query := `
		UPDATE vpn_sessions
		SET bytes_in = $1, bytes_out = $2, roaming_events = $3, failover_count = $4,
		    last_activity_at = NOW()
		WHERE id = $5
	`
	_, err := p.pool.Exec(ctx, query, bytesIn, bytesOut, roamingEvents, failoverCount, sessionID)
	return err
}

// TerminateSession implements Store.
func (p *Postgres) TerminateSession(ctx context.Context, sessionID uuid.UUID, exitReason string) error {
	query := `
		UPDATE vpn_sessions
		SET terminated_at = NOW(), exit_reason = $1, state = 'TERMINATING'
		WHERE id = $2
	`
	_, err := p.pool.Exec(ctx, query, exitReason, sessionID)
	return err
}

// ListActiveSessionsByRegion implements Store.
func (p *Postgres) ListActiveSessionsByRegion(ctx context.Context, region string) ([]*Session, error) {
	query := `
		SELECT id, customer_id, region, primary_provider_id, current_provider_id,
		       state, bytes_in, bytes_out, roaming_events, failover_count,
		       ice_candidate_count, ice_time_ms, wg_establish_time_ms,
		       created_at, terminated_at, last_activity_at, exit_reason,
		       COALESCE(provider_wg_public_key, ''), COALESCE(customer_wg_public_key, ''),
		       billed_at, COALESCE(host(inner_ip), ''), payment_authorization
		FROM vpn_sessions
		WHERE region = $1 AND terminated_at IS NULL
		ORDER BY created_at DESC
	`
	rows, err := p.pool.Query(ctx, query, region)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session, err := p.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// ListSessionsByCustomer implements Store.
func (p *Postgres) ListSessionsByCustomer(ctx context.Context, customerID uuid.UUID) ([]*Session, error) {
	query := `
		SELECT id, customer_id, region, primary_provider_id, current_provider_id,
		       state, bytes_in, bytes_out, roaming_events, failover_count,
		       ice_candidate_count, ice_time_ms, wg_establish_time_ms,
		       created_at, terminated_at, last_activity_at, exit_reason,
		       COALESCE(provider_wg_public_key, ''), COALESCE(customer_wg_public_key, ''),
		       billed_at, COALESCE(host(inner_ip), ''), payment_authorization
		FROM vpn_sessions
		WHERE customer_id = $1
		ORDER BY created_at DESC
	`
	rows, err := p.pool.Query(ctx, query, customerID)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session, err := p.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// RegisterCandidates implements Store.
func (p *Postgres) RegisterCandidates(ctx context.Context, providerID uuid.UUID, candidates []*pb.IceCandidate) error {
	// Delete old candidates first
	_, err := p.pool.Exec(ctx, `DELETE FROM ice_candidates WHERE provider_id = $1`, providerID)
	if err != nil {
		return fmt.Errorf("delete old candidates: %w", err)
	}

	// Insert new candidates
	for _, cand := range candidates {
		query := `
			INSERT INTO ice_candidates (
				provider_id, foundation, component, transport, priority,
				connection_address, connection_port, candidate_type,
				related_address, related_port, latency_ms,
				discovered_at, expires_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW() + INTERVAL '5 minutes'
			)
		`
		// related_address is `inet` in Postgres which rejects empty
		// string; convert "" → NULL so host candidates (which have no
		// reflexive/relay related-address) insert cleanly.
		var relatedAddr interface{}
		if cand.RelatedAddress != "" {
			relatedAddr = cand.RelatedAddress
		}
		_, err := p.pool.Exec(ctx, query,
			providerID,
			cand.Foundation,
			cand.Component,
			cand.Transport,
			cand.Priority,
			cand.ConnectionAddress,
			cand.ConnectionPort,
			cand.CandidateType,
			relatedAddr,
			cand.RelatedPort,
			cand.LatencyMs,
		)
		if err != nil {
			return fmt.Errorf("insert candidate: %w", err)
		}
	}
	return nil
}

// GetProviderCandidates implements Store.
func (p *Postgres) GetProviderCandidates(ctx context.Context, providerID uuid.UUID) ([]*pb.IceCandidate, error) {
	query := `
		SELECT foundation, component, transport, priority,
		       connection_address::text, connection_port, candidate_type,
		       related_address::text, related_port, latency_ms, discovered_at
		FROM ice_candidates
		WHERE provider_id = $1 AND expires_at > NOW()
		ORDER BY priority ASC
	`
	rows, err := p.pool.Query(ctx, query, providerID)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var candidates []*pb.IceCandidate
	for rows.Next() {
		cand := &pb.IceCandidate{}
		var discoveredAt time.Time
		var relatedAddr sql.NullString
		err := rows.Scan(
			&cand.Foundation,
			&cand.Component,
			&cand.Transport,
			&cand.Priority,
			&cand.ConnectionAddress,
			&cand.ConnectionPort,
			&cand.CandidateType,
			&relatedAddr,
			&cand.RelatedPort,
			&cand.LatencyMs,
			&discoveredAt,
		)
		cand.RelatedAddress = relatedAddr.String
		if err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		cand.DiscoveredAtUnixMs = discoveredAt.UnixMilli()
		candidates = append(candidates, cand)
	}
	return candidates, rows.Err()
}

// ConfirmWorkingCandidate implements Store.
func (p *Postgres) ConfirmWorkingCandidate(ctx context.Context, sessionID uuid.UUID, candidate *pb.IceCandidate) error {
	// Reject empty addresses up front — Postgres rejects "" for the
	// inet column and explodes the connection. A malformed SDK request
	// must surface as 4xx, not a 500.
	if candidate == nil || candidate.GetConnectionAddress() == "" || candidate.GetConnectionPort() == 0 {
		return fmt.Errorf("confirm candidate: chosen_candidate.connection_address + connection_port required")
	}
	query := `
		UPDATE ice_candidates
		SET session_id = $1, is_preferred = TRUE
		WHERE provider_id = (SELECT current_provider_id FROM vpn_sessions WHERE id = $1)
		  AND connection_address = $2 AND connection_port = $3
	`
	_, err := p.pool.Exec(ctx, query, sessionID, candidate.ConnectionAddress, candidate.ConnectionPort)
	return err
}

// CleanupExpiredCandidates implements Store.
func (p *Postgres) CleanupExpiredCandidates(ctx context.Context) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM ice_candidates WHERE expires_at < NOW()`)
	return err
}

// GetProvidersInRegion implements Store.
func (p *Postgres) GetProvidersInRegion(ctx context.Context, region string) ([]*ProviderInfo, error) {
	query := `
		SELECT id, region, status, last_seen_at, session_count, COALESCE(wg_public_key, '')
		FROM vpn_providers
		WHERE region = $1 AND status != 'offline'
		ORDER BY session_count ASC
	`
	rows, err := p.pool.Query(ctx, query, region)
	if err != nil {
		return nil, fmt.Errorf("query providers: %w", err)
	}
	defer rows.Close()

	var providers []*ProviderInfo
	for rows.Next() {
		provider := &ProviderInfo{}
		if err := rows.Scan(&provider.ID, &provider.Region, &provider.Status, &provider.LastSeenAt, &provider.SessionCount, &provider.WgPublicKey); err != nil {
			return nil, fmt.Errorf("scan provider: %w", err)
		}
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}

// SelectProviderForSession implements Store.
func (p *Postgres) SelectProviderForSession(ctx context.Context, region string) (uuid.UUID, error) {
	query := `
		SELECT id FROM vpn_providers
		WHERE region = $1 AND status = 'healthy'
		ORDER BY session_count ASC
		LIMIT 1
	`
	var providerID uuid.UUID
	err := p.pool.QueryRow(ctx, query, region).Scan(&providerID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("select provider: %w", err)
	}
	// Increment session count
	updateQuery := `
		UPDATE vpn_providers
		SET session_count = session_count + 1, last_seen_at = NOW()
		WHERE id = $1
	`
	_, _ = p.pool.Exec(ctx, updateQuery, providerID)
	return providerID, nil
}

// RegisterProvider implements Store. Idempotent UPSERT — on conflict
// the existing row's region / status / last_seen_at are overwritten,
// session_count is left untouched so we don't yank sessions away.
// wg_public_key is COALESCEd on update: a daemon that re-registers
// without a key (e.g. legacy build) won't blank out a previously
// captured key.
func (p *Postgres) RegisterProvider(ctx context.Context, info *ProviderInfo) error {
	query := `
		INSERT INTO vpn_providers (id, region, status, last_seen_at, session_count, wg_public_key)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
		ON CONFLICT (id) DO UPDATE
		SET region = EXCLUDED.region,
		    status = EXCLUDED.status,
		    last_seen_at = EXCLUDED.last_seen_at,
		    wg_public_key = COALESCE(EXCLUDED.wg_public_key, vpn_providers.wg_public_key)
	`
	_, err := p.pool.Exec(ctx, query, info.ID, info.Region, info.Status, info.LastSeenAt, info.SessionCount, info.WgPublicKey)
	return err
}

// InvalidateSessionsOnProviderKeyChange implements Store (#762).
//
// Single round-trip CTE: it reads the provider's prior wg_public_key and,
// only when newKey is non-empty AND a non-empty prior key exists AND they
// differ, terminates every active session whose current OR primary provider
// is this one. Terminated rows get exit_reason='provider_key_rotated' so the
// reconnect telemetry is attributable and the earnings batcher still bills the
// bytes used so far (terminate, not delete). Doing the read+terminate in one
// statement makes the check race-free against a concurrent register from the
// same daemon.
//
// `changed` is true whenever a prior non-empty key existed and differed from
// newKey, regardless of how many sessions were live — so the caller logs the
// rotation even if zero clients were currently connected.
func (p *Postgres) InvalidateSessionsOnProviderKeyChange(ctx context.Context, providerID uuid.UUID, newKey string) (int, bool, error) {
	if strings.TrimSpace(newKey) == "" {
		// Legacy daemon: no key to compare. RegisterProvider's COALESCE
		// preserves the cached key; nothing to invalidate.
		return 0, false, nil
	}
	// prior CTE: the stored key (NULL if the provider row doesn't exist yet
	//            or never had a key — first register).
	// changed   = prior key is present, non-empty, and != newKey.
	// terminated rows are returned so we can count them.
	query := `
		WITH prior AS (
		    SELECT NULLIF(wg_public_key, '') AS key
		    FROM vpn_providers
		    WHERE id = $1
		),
		changed AS (
		    SELECT (SELECT key FROM prior) IS NOT NULL
		       AND (SELECT key FROM prior) <> $2 AS did_change
		),
		terminated AS (
		    UPDATE vpn_sessions
		       SET terminated_at = NOW(),
		           exit_reason   = 'provider_key_rotated',
		           state         = 'TERMINATING',
		           last_activity_at = NOW()
		     WHERE terminated_at IS NULL
		       AND (current_provider_id = $1 OR primary_provider_id = $1)
		       AND (SELECT did_change FROM changed)
		    RETURNING id
		)
		SELECT (SELECT did_change FROM changed),
		       (SELECT COUNT(*) FROM terminated)
	`
	var changed bool
	var terminated int
	if err := p.pool.QueryRow(ctx, query, providerID, newKey).Scan(&changed, &terminated); err != nil {
		return 0, false, fmt.Errorf("invalidate sessions on provider key change: %w", err)
	}
	return terminated, changed, nil
}

// SelectTopProvidersInRegion implements Store.
//
// One round-trip: a CTE selects the top-N least-loaded healthy providers
// + their wg_public_key + the median latency from ice_candidates rows
// discovered within the last hour. The candidate set is then fetched in
// a second query (one per provider) — for N=3 that's at most 4 queries
// total, which is fine for the rare mobile-app probe call.
func (p *Postgres) SelectTopProvidersInRegion(ctx context.Context, region string, limit int) ([]*ProviderProbe, error) {
	if limit <= 0 {
		limit = 3
	}
	// percentile_cont(0.5) is Postgres' median (continuous). NULL-safe:
	// providers with zero fresh latency samples return NULL → 0 via COALESCE.
	query := `
		SELECT vp.id,
		       COALESCE(vp.wg_public_key, ''),
		       COALESCE(
		         (SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY ic.latency_ms)::int
		            FROM ice_candidates ic
		           WHERE ic.provider_id = vp.id
		             AND ic.latency_ms IS NOT NULL
		             AND ic.latency_ms > 0
		             AND ic.discovered_at > NOW() - INTERVAL '1 hour'),
		         0
		       ) AS median_rtt_ms
		FROM vpn_providers vp
		WHERE vp.region = $1
		  AND vp.status = 'healthy'
		  AND vp.last_seen_at > NOW() - INTERVAL '5 minutes'
		ORDER BY vp.session_count ASC
		LIMIT $2
	`
	rows, err := p.pool.Query(ctx, query, region, limit)
	if err != nil {
		return nil, fmt.Errorf("query top providers: %w", err)
	}
	type row struct {
		id          uuid.UUID
		wgKey       string
		medianRttMs int32
	}
	var rs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.wgKey, &r.medianRttMs); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan top provider: %w", err)
		}
		rs = append(rs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]*ProviderProbe, 0, len(rs))
	for _, r := range rs {
		cands, err := p.GetProviderCandidates(ctx, r.id)
		if err != nil {
			// Skip providers whose candidate fetch fails; logging is
			// the handler's job (we don't have a logger here).
			cands = nil
		}
		var rtt uint32
		if r.medianRttMs > 0 {
			rtt = uint32(r.medianRttMs)
		}
		out = append(out, &ProviderProbe{
			ProviderID:  r.id,
			WgPublicKey: r.wgKey,
			Candidates:  cands,
			MedianRttMs: rtt,
		})
	}
	return out, nil
}

// SelectProviderAcrossRegions implements Store.
//
// Picks the best healthy provider across ALL regions for region=auto
// session requests (#570). Today the scoring is purely
// least-loaded-first (ORDER BY session_count ASC, last_seen_at DESC) —
// geo-affinity from clientIPHint is a hint logged for future tuning but
// not yet used at SQL level (the country-from-IP table lives in
// providers-svc, not vpn-svc, so adding it would mean a cross-svc RPC on
// the session-create hot path — explicit deferral). Once #573 lands a
// geo helper into a shared lib the ORDER BY can be extended.
func (p *Postgres) SelectProviderAcrossRegions(ctx context.Context, clientIPHint string) (uuid.UUID, string, error) {
	query := `
		SELECT id, region FROM vpn_providers
		WHERE status = 'healthy'
		  AND last_seen_at > NOW() - INTERVAL '5 minutes'
		ORDER BY session_count ASC, last_seen_at DESC
		LIMIT 1
	`
	var providerID uuid.UUID
	var region string
	err := p.pool.QueryRow(ctx, query).Scan(&providerID, &region)
	if err != nil {
		return uuid.Nil, "", fmt.Errorf("select provider across regions: %w", err)
	}
	// Increment session count + bump last_seen_at (same bookkeeping as
	// SelectProviderForSession).
	updateQuery := `
		UPDATE vpn_providers
		SET session_count = session_count + 1, last_seen_at = NOW()
		WHERE id = $1
	`
	_, _ = p.pool.Exec(ctx, updateQuery, providerID)
	_ = clientIPHint // reserved for future geo-affinity scoring (#573)
	return providerID, region, nil
}

// CleanupStaleSessions implements Store.
func (p *Postgres) CleanupStaleSessions(ctx context.Context, staleAfter time.Duration) (int, error) {
	query := `
		UPDATE vpn_sessions
		SET terminated_at = NOW(), exit_reason = 'stale_heartbeat', state = 'TERMINATING'
		WHERE terminated_at IS NULL
		  AND last_activity_at < NOW() - $1::interval
	`
	tag, err := p.pool.Exec(ctx, query, staleAfter)
	if err != nil {
		return 0, fmt.Errorf("cleanup stale sessions: %w", err)
	}
	return int(tag.RowsAffected()), nil
}

// SelectAlternateProvider implements Store.
func (p *Postgres) SelectAlternateProvider(ctx context.Context, region string, exclude []uuid.UUID) (uuid.UUID, error) {
	// Build a NOT IN clause; pgx supports = ANY($N::uuid[]) cleanly
	query := `
		SELECT id FROM vpn_providers
		WHERE region = $1 AND status = 'healthy' AND id <> ALL($2::uuid[])
		ORDER BY session_count ASC
		LIMIT 1
	`
	excludeStrs := make([]uuid.UUID, len(exclude))
	copy(excludeStrs, exclude)

	var providerID uuid.UUID
	err := p.pool.QueryRow(ctx, query, region, excludeStrs).Scan(&providerID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("select alternate provider: %w", err)
	}
	updateQuery := `
		UPDATE vpn_providers
		SET session_count = session_count + 1, last_seen_at = NOW()
		WHERE id = $1
	`
	_, _ = p.pool.Exec(ctx, updateQuery, providerID)
	return providerID, nil
}

// UpdateProviderHealth implements Store.
func (p *Postgres) UpdateProviderHealth(ctx context.Context, providerID uuid.UUID, status string, lastSeen time.Time) error {
	query := `
		UPDATE vpn_providers
		SET status = $1, last_seen_at = $2
		WHERE id = $3
	`
	_, err := p.pool.Exec(ctx, query, status, lastSeen, providerID)
	return err
}

// TriggerFailover implements Store.
func (p *Postgres) TriggerFailover(ctx context.Context, sessionID uuid.UUID, currentProvider, altProvider uuid.UUID) error {
	query := `
		UPDATE vpn_sessions
		SET current_provider_id = $1, failover_count = failover_count + 1,
		    state = $2, last_activity_at = NOW()
		WHERE id = $3
	`
	_, err := p.pool.Exec(ctx, query, altProvider, pb.VpnSessionState_VPN_SESSION_STATE_FAILING_OVER.String(), sessionID)
	return err
}

// BindProviderToSession implements Store.
func (p *Postgres) BindProviderToSession(ctx context.Context, sessionID uuid.UUID, providerWgPubKey string) error {
	// Note: provider_wg_public_key + customer_wg_public_key columns added
	// in migration 00004_session_wg_keys.sql (alters vpn_sessions table).
	query := `
		UPDATE vpn_sessions
		SET provider_wg_public_key = $1, last_activity_at = NOW()
		WHERE id = $2
	`
	_, err := p.pool.Exec(ctx, query, providerWgPubKey, sessionID)
	return err
}

// BindCustomerWgKey implements Store.
func (p *Postgres) BindCustomerWgKey(ctx context.Context, sessionID uuid.UUID, customerWgPubKey string) error {
	query := `
		UPDATE vpn_sessions
		SET customer_wg_public_key = $1, last_activity_at = NOW()
		WHERE id = $2
	`
	_, err := p.pool.Exec(ctx, query, customerWgPubKey, sessionID)
	return err
}

// ListAssignedSessions implements Store.
func (p *Postgres) ListAssignedSessions(ctx context.Context, providerID uuid.UUID) ([]*Session, error) {
	query := `
		SELECT id, customer_id, region, primary_provider_id, current_provider_id,
		       state, bytes_in, bytes_out, roaming_events, failover_count,
		       ice_candidate_count, ice_time_ms, wg_establish_time_ms,
		       created_at, terminated_at, last_activity_at, exit_reason,
		       COALESCE(provider_wg_public_key, ''), COALESCE(customer_wg_public_key, ''),
		       billed_at, COALESCE(host(inner_ip), ''), payment_authorization
		FROM vpn_sessions
		WHERE current_provider_id = $1
		  AND terminated_at IS NULL
		  AND (provider_wg_public_key IS NULL OR provider_wg_public_key = '')
		  AND created_at > now() - make_interval(secs => $2)
		ORDER BY created_at ASC
	`
	rows, err := p.pool.Query(ctx, query, providerID, AssignedSessionMaxAge.Seconds())
	if err != nil {
		return nil, fmt.Errorf("query assigned sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session, err := p.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// ListBoundSessions implements Store (#788 daemon-restart recovery).
//
// Deliberately OMITS the two ListAssignedSessions exclusions:
//   - no `provider_wg_public_key IS NULL` filter (we WANT the already-bound
//     ones — those are exactly the live peers a restart lost), and
//   - no `created_at > now() - AssignedSessionMaxAge` cutoff (a session
//     bound an hour ago is still live and must be re-derived).
//
// It keeps the non-empty-customer-key predicate: a row with no customer
// key has nothing to upsert as a WG peer (the binder skips empty-key rows
// anyway), and the live ListAssignedSessions bring-up path still covers it.
// Stale-session cleanup (CleanupStaleSessions) terminates truly-dead
// sessions on its own tick, so `terminated_at IS NULL` keeps this set
// bounded by the genuinely-live population.
func (p *Postgres) ListBoundSessions(ctx context.Context, providerID uuid.UUID) ([]*Session, error) {
	query := `
		SELECT id, customer_id, region, primary_provider_id, current_provider_id,
		       state, bytes_in, bytes_out, roaming_events, failover_count,
		       ice_candidate_count, ice_time_ms, wg_establish_time_ms,
		       created_at, terminated_at, last_activity_at, exit_reason,
		       COALESCE(provider_wg_public_key, ''), COALESCE(customer_wg_public_key, ''),
		       billed_at, COALESCE(host(inner_ip), ''), payment_authorization
		FROM vpn_sessions
		WHERE current_provider_id = $1
		  AND terminated_at IS NULL
		  AND customer_wg_public_key IS NOT NULL
		  AND customer_wg_public_key <> ''
		ORDER BY created_at ASC
	`
	rows, err := p.pool.Query(ctx, query, providerID)
	if err != nil {
		return nil, fmt.Errorf("query bound sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		session, err := p.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, session)
	}
	return sessions, rows.Err()
}

// ListRegions implements Store.
func (p *Postgres) ListRegions(ctx context.Context) ([]*RegionSummary, error) {
	query := `
		SELECT region,
		       COUNT(*) FILTER (WHERE status = 'healthy') AS healthy,
		       COUNT(*) AS total
		FROM vpn_providers
		GROUP BY region
		HAVING COUNT(*) FILTER (WHERE status = 'healthy') > 0
		ORDER BY healthy DESC
	`
	rows, err := p.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query regions: %w", err)
	}
	defer rows.Close()
	var out []*RegionSummary
	for rows.Next() {
		s := &RegionSummary{}
		if err := rows.Scan(&s.Region, &s.HealthyProviders, &s.TotalProviders); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// TerminateAllForCustomer implements Store.
func (p *Postgres) TerminateAllForCustomer(ctx context.Context, customerID uuid.UUID, exitReason string) (int, error) {
	res, err := p.pool.Exec(ctx,
		`UPDATE vpn_sessions
		    SET terminated_at = NOW(), exit_reason = $1, state = 'TERMINATING', last_activity_at = NOW()
		  WHERE customer_id = $2 AND terminated_at IS NULL`,
		exitReason, customerID)
	if err != nil {
		return 0, fmt.Errorf("terminate all for customer: %w", err)
	}
	return int(res.RowsAffected()), nil
}

// ListUnbilledTerminatedSessions implements Store.
func (p *Postgres) ListUnbilledTerminatedSessions(ctx context.Context, limit int) ([]*Session, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT id, customer_id, region, primary_provider_id, current_provider_id,
		       state, bytes_in, bytes_out, roaming_events, failover_count,
		       ice_candidate_count, ice_time_ms, wg_establish_time_ms,
		       created_at, terminated_at, last_activity_at, exit_reason,
		       COALESCE(provider_wg_public_key, ''), COALESCE(customer_wg_public_key, ''),
		       billed_at, COALESCE(host(inner_ip), ''), payment_authorization
		FROM vpn_sessions
		WHERE terminated_at IS NOT NULL AND billed_at IS NULL
		ORDER BY terminated_at ASC
		LIMIT $1
	`
	rows, err := p.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query unbilled sessions: %w", err)
	}
	defer rows.Close()
	var sessions []*Session
	for rows.Next() {
		s, err := p.scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// SumCustomerBytesThisMonth implements Store.
func (p *Postgres) SumCustomerBytesThisMonth(ctx context.Context, customerID uuid.UUID) (uint64, error) {
	var total int64
	err := p.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(bytes_in + bytes_out)::bigint, 0)
		   FROM vpn_sessions
		  WHERE customer_id = $1
		    AND created_at >= date_trunc('month', NOW() AT TIME ZONE 'UTC')`,
		customerID).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("sum bytes: %w", err)
	}
	if total < 0 {
		return 0, nil
	}
	return uint64(total), nil
}

// MarkSessionBilled implements Store.
func (p *Postgres) MarkSessionBilled(ctx context.Context, sessionID uuid.UUID) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE vpn_sessions SET billed_at = NOW() WHERE id = $1 AND billed_at IS NULL`,
		sessionID)
	return err
}

// Helper to scan a session row
func (p *Postgres) scanSession(row interface {
	Scan(dest ...interface{}) error
}) (*Session, error) {
	session := &Session{}
	var stateStr string
	var terminatedAt *time.Time
	var exitReason sql.NullString
	err := row.Scan(
		&session.ID,
		&session.CustomerID,
		&session.Region,
		&session.PrimaryProvider,
		&session.CurrentProvider,
		&stateStr,
		&session.BytesIn,
		&session.BytesOut,
		&session.RoamingEvents,
		&session.FailoverCount,
		&session.IceCandidates,
		&session.IceTimeMs,
		&session.WgEstablishMs,
		&session.CreatedAt,
		&terminatedAt,
		&session.LastActivityAt,
		&exitReason,
		&session.ProviderWgPublicKey,
		&session.CustomerWgPublicKey,
		&session.BilledAt,
		&session.InnerIP,              // #701: the daemon's #695 return-routing needs it
		&session.PaymentAuthorization, // #726 audit: was silently dropped
	)
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	session.TerminatedAt = terminatedAt
	session.ExitReason = exitReason.String
	stateVal, ok := pb.VpnSessionState_value[stateStr]
	if !ok {
		// Legacy rows: enum values were stored un-prefixed ("ACTIVE",
		// "FAILING_OVER") before the buf-lint rename added the
		// VPN_SESSION_STATE_ prefix (bbc8fd0 follow-up). New writes
		// store the prefixed .String(); accept both so existing
		// sessions keep their state across the deploy.
		stateVal, ok = pb.VpnSessionState_value["VPN_SESSION_STATE_"+stateStr]
		if !ok {
			stateVal = int32(pb.VpnSessionState_VPN_SESSION_STATE_UNSPECIFIED)
		}
	}
	session.State = pb.VpnSessionState(stateVal)
	return session, nil
}

// AllocateInnerIP implements Store. Uses INSERT…ON CONFLICT DO UPDATE
// …RETURNING against vpn_provider_inner_ip_alloc to atomically claim
// the next Y suffix for the provider, then composes the dotted-quad
// using X = first byte of providerID (clamped 2..253). Idempotent
// at the (provider, session) level via a follow-up SELECT against
// vpn_sessions.inner_ip: if a row already exists with the same
// (provider, session) pair, return the previously-allocated IP
// instead of burning a new suffix.
//
// Migration 0008_session_peer_config.sql defines the table:
//
//	CREATE TABLE vpn_provider_inner_ip_alloc (
//	    provider_id UUID PRIMARY KEY,
//	    next_suffix INT NOT NULL DEFAULT 1,
//	    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
//	);
//
// Refs #605.
func (p *Postgres) AllocateInnerIP(ctx context.Context, providerID, sessionID uuid.UUID) (string, error) {
	// 1. Idempotency check — return existing if already allocated.
	var existing sql.NullString
	const existingQuery = `
		SELECT host(inner_ip) FROM vpn_sessions
		WHERE id = $1 AND inner_ip IS NOT NULL
	`
	err := p.pool.QueryRow(ctx, existingQuery, sessionID).Scan(&existing)
	if err == nil && existing.Valid && existing.String != "" {
		return existing.String, nil
	}

	// 2. Atomic suffix bump.
	const allocQuery = `
		INSERT INTO vpn_provider_inner_ip_alloc (provider_id, next_suffix, updated_at)
		VALUES ($1, 2, NOW())
		ON CONFLICT (provider_id) DO UPDATE
		SET next_suffix = vpn_provider_inner_ip_alloc.next_suffix + 1,
		    updated_at = NOW()
		RETURNING next_suffix
	`
	var nextY int
	if err := p.pool.QueryRow(ctx, allocQuery, providerID).Scan(&nextY); err != nil {
		return "", fmt.Errorf("inner-ip alloc: %w", err)
	}
	if nextY >= 254 {
		// Per-provider /24 exhausted; signal upstream.
		return "", fmt.Errorf("inner-ip space exhausted for provider %s", providerID)
	}

	// 3. Compose dotted-quad from providerID[0] (clamped to safe range)
	// and the allocated Y.
	x := providerID[0]
	if x < 2 {
		x = 2
	}
	if x > 253 {
		x = 253
	}
	return fmt.Sprintf("10.66.%d.%d", x, nextY), nil
}

// PersistSessionPeerConfig implements Store. Writes the resolved peer
// public key + endpoint onto the session row via an UPDATE against
// vpn_sessions. Refs #605.
//
// Note: peer_endpoint isn't a column on vpn_sessions yet (it would
// be added in a follow-up migration if we decide to surface it via
// GetSession). For now we store the WG public key only — the
// endpoint is re-derived on demand from the provider's freshest ICE
// candidate, which is already what the mobile handler does on its
// own.
func (p *Postgres) PersistSessionPeerConfig(ctx context.Context, sessionID uuid.UUID, peerPubKey, peerEndpoint string) error {
	const query = `
		UPDATE vpn_sessions
		SET provider_wg_public_key = $1
		WHERE id = $2
	`
	tag, err := p.pool.Exec(ctx, query, peerPubKey, sessionID)
	if err != nil {
		return fmt.Errorf("persist peer config: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("session %s not found", sessionID)
	}
	// peerEndpoint reserved for future per-session pinning; the
	// mobile flow derives it from the provider's ICE candidates so
	// dropping it here is forward-compatible.
	_ = peerEndpoint
	return nil
}
