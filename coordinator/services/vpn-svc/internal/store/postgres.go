package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
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
	query := `
		INSERT INTO vpn_sessions (
			id, customer_id, region, primary_provider_id, current_provider_id,
			state, created_at, last_activity_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8
		)
	`
	_, err := p.pool.Exec(ctx, query,
		session.ID,
		session.CustomerID,
		session.Region,
		session.PrimaryProvider,
		session.CurrentProvider,
		session.State.String(),
		session.CreatedAt,
		session.LastActivityAt,
	)
	return err
}

// GetSession implements Store.
func (p *Postgres) GetSession(ctx context.Context, sessionID uuid.UUID) (*Session, error) {
	query := `
		SELECT id, customer_id, region, primary_provider_id, current_provider_id,
		       state, bytes_in, bytes_out, roaming_events, failover_count,
		       ice_candidate_count, ice_time_ms, wg_establish_time_ms,
		       created_at, terminated_at, last_activity_at, exit_reason
		FROM vpn_sessions
		WHERE id = $1
	`
	row := p.pool.QueryRow(ctx, query, sessionID)
	session := &Session{}
	var stateStr string
	var terminatedAt *time.Time
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
		&session.ExitReason,
	)
	if err != nil {
		return nil, fmt.Errorf("query session: %w", err)
	}
	session.TerminatedAt = terminatedAt
	// Parse state from string
	stateVal, ok := pb.VpnSessionState_value[stateStr]
	if !ok {
		stateVal = int32(pb.VpnSessionState_VPN_SESSION_STATE_UNSPECIFIED)
	}
	session.State = pb.VpnSessionState(stateVal)
	return session, nil
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
		       created_at, terminated_at, last_activity_at, exit_reason
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
		       created_at, terminated_at, last_activity_at, exit_reason
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
		_, err := p.pool.Exec(ctx, query,
			providerID,
			cand.Foundation,
			cand.Component,
			cand.Transport,
			cand.Priority,
			cand.ConnectionAddress,
			cand.ConnectionPort,
			cand.CandidateType,
			cand.RelatedAddress,
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
		       connection_address, connection_port, candidate_type,
		       related_address, related_port, latency_ms, discovered_at
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
		err := rows.Scan(
			&cand.Foundation,
			&cand.Component,
			&cand.Transport,
			&cand.Priority,
			&cand.ConnectionAddress,
			&cand.ConnectionPort,
			&cand.CandidateType,
			&cand.RelatedAddress,
			&cand.RelatedPort,
			&cand.LatencyMs,
			&discoveredAt,
		)
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
		SELECT id, region, status, last_seen_at, session_count
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
		if err := rows.Scan(&provider.ID, &provider.Region, &provider.Status, &provider.LastSeenAt, &provider.SessionCount); err != nil {
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
func (p *Postgres) RegisterProvider(ctx context.Context, info *ProviderInfo) error {
	query := `
		INSERT INTO vpn_providers (id, region, status, last_seen_at, session_count)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE
		SET region = EXCLUDED.region,
		    status = EXCLUDED.status,
		    last_seen_at = EXCLUDED.last_seen_at
	`
	_, err := p.pool.Exec(ctx, query, info.ID, info.Region, info.Status, info.LastSeenAt, info.SessionCount)
	return err
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
	_, err := p.pool.Exec(ctx, query, altProvider, pb.VpnSessionState_FAILING_OVER.String(), sessionID)
	return err
}

// Helper to scan a session row
func (p *Postgres) scanSession(row interface {
	Scan(dest ...interface{}) error
}) (*Session, error) {
	session := &Session{}
	var stateStr string
	var terminatedAt *time.Time
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
		&session.ExitReason,
	)
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}
	session.TerminatedAt = terminatedAt
	stateVal, ok := pb.VpnSessionState_value[stateStr]
	if !ok {
		stateVal = int32(pb.VpnSessionState_VPN_SESSION_STATE_UNSPECIFIED)
	}
	session.State = pb.VpnSessionState(stateVal)
	return session, nil
}
