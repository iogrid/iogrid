package store

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/payment"
)

// ── In-memory escrow store (tests + dev mode) ───────────────────────

// MemoryEscrowStore implements payment.EscrowStore in memory.
type MemoryEscrowStore struct {
	mu       sync.Mutex
	escrows  map[uuid.UUID]*payment.Escrow
	nonces   map[string]time.Time // key = wallet|nonce
	clock    func() time.Time
}

// NewMemoryEscrowStore constructs an in-memory escrow store. `clock` is
// optional (defaults to time.Now).
func NewMemoryEscrowStore() *MemoryEscrowStore {
	return &MemoryEscrowStore{
		escrows: make(map[uuid.UUID]*payment.Escrow),
		nonces:  make(map[string]time.Time),
		clock:   time.Now,
	}
}

// CheckAndRecordNonce implements payment.EscrowStore.
func (m *MemoryEscrowStore) CheckAndRecordNonce(ctx context.Context, wallet, nonce string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock()
	k := wallet + "|" + nonce
	if seenAt, ok := m.nonces[k]; ok && now.Sub(seenAt) < payment.NonceTTL {
		return true, nil
	}
	m.nonces[k] = now
	return false, nil
}

// CleanupNonces implements payment.EscrowStore.
func (m *MemoryEscrowStore) CleanupNonces(ctx context.Context) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cutoff := m.clock().Add(-payment.NonceTTL)
	n := 0
	for k, t := range m.nonces {
		if t.Before(cutoff) {
			delete(m.nonces, k)
			n++
		}
	}
	return n, nil
}

// CreateEscrow implements payment.EscrowStore.
func (m *MemoryEscrowStore) CreateEscrow(ctx context.Context, e *payment.Escrow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.escrows[e.SessionID]; ok {
		return errors.New("escrow already exists for session")
	}
	clone := *e
	m.escrows[e.SessionID] = &clone
	return nil
}

// GetEscrow implements payment.EscrowStore.
func (m *MemoryEscrowStore) GetEscrow(ctx context.Context, sessionID uuid.UUID) (*payment.Escrow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.escrows[sessionID]
	if !ok {
		return nil, errors.New("escrow not found")
	}
	clone := *e
	return &clone, nil
}

// AddConsumption implements payment.EscrowStore.
func (m *MemoryEscrowStore) AddConsumption(ctx context.Context, sessionID uuid.UUID, delta uint64) (*payment.Escrow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.escrows[sessionID]
	if !ok {
		return nil, errors.New("escrow not found")
	}
	newConsumed := e.ConsumedAtomic + delta
	if newConsumed > e.EscrowedAtomic {
		return nil, payment.ErrEscrowExhausted
	}
	e.ConsumedAtomic = newConsumed
	e.LastHeartbeatAt = m.clock()
	clone := *e
	return &clone, nil
}

// SettleEscrow implements payment.EscrowStore.
func (m *MemoryEscrowStore) SettleEscrow(ctx context.Context, sessionID uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.escrows[sessionID]
	if !ok {
		return errors.New("escrow not found")
	}
	t := m.clock()
	e.SettledAt = &t
	return nil
}

// ── Postgres-backed escrow store ────────────────────────────────────

// PostgresEscrowStore is the Postgres impl of payment.EscrowStore. Uses
// the same pgx pool as the session store.
type PostgresEscrowStore struct {
	pool *pgxpool.Pool
}

// NewPostgresEscrowStore wraps the pgx pool to expose escrow operations.
func NewPostgresEscrowStore(pool *pgxpool.Pool) *PostgresEscrowStore {
	return &PostgresEscrowStore{pool: pool}
}

// CheckAndRecordNonce implements payment.EscrowStore.
func (s *PostgresEscrowStore) CheckAndRecordNonce(ctx context.Context, wallet, nonce string) (bool, error) {
	// First evict our own nonce row if it's older than NonceTTL — this
	// makes the unique-violation path below correspond to a TRUE replay.
	if _, err := s.pool.Exec(ctx,
		`DELETE FROM vpn_payment_nonces WHERE wallet_address = $1 AND nonce = $2 AND seen_at < NOW() - $3::interval`,
		wallet, nonce, payment.NonceTTL); err != nil {
		return false, err
	}
	// Try to insert. ON CONFLICT we return seen=true.
	tag, err := s.pool.Exec(ctx,
		`INSERT INTO vpn_payment_nonces (wallet_address, nonce) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		wallet, nonce)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 0 {
		// Row already there + still fresh → replay.
		return true, nil
	}
	return false, nil
}

// CleanupNonces implements payment.EscrowStore.
func (s *PostgresEscrowStore) CleanupNonces(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM vpn_payment_nonces WHERE seen_at < NOW() - $1::interval`,
		payment.NonceTTL)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// CreateEscrow implements payment.EscrowStore.
func (s *PostgresEscrowStore) CreateEscrow(ctx context.Context, e *payment.Escrow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO vpn_session_escrow (
			session_id, customer_id, wallet_address,
			escrowed_grid_atomic, consumed_grid_atomic,
			max_grid_per_min_atomic, nonce, started_at, last_heartbeat_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.SessionID, e.CustomerID, e.WalletAddress,
		int64(e.EscrowedAtomic), int64(e.ConsumedAtomic),
		int64(e.MaxGRIDPerMinAtomic), e.Nonce,
		e.StartedAt, e.LastHeartbeatAt,
	)
	return err
}

// GetEscrow implements payment.EscrowStore.
func (s *PostgresEscrowStore) GetEscrow(ctx context.Context, sessionID uuid.UUID) (*payment.Escrow, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT session_id, customer_id, wallet_address,
		       escrowed_grid_atomic, consumed_grid_atomic,
		       max_grid_per_min_atomic, nonce,
		       started_at, last_heartbeat_at, settled_at
		  FROM vpn_session_escrow WHERE session_id = $1`,
		sessionID)
	e := &payment.Escrow{}
	var esc, cons, mx int64
	var settledAt *time.Time
	if err := row.Scan(
		&e.SessionID, &e.CustomerID, &e.WalletAddress,
		&esc, &cons, &mx, &e.Nonce,
		&e.StartedAt, &e.LastHeartbeatAt, &settledAt,
	); err != nil {
		return nil, err
	}
	e.EscrowedAtomic = uint64(esc)
	e.ConsumedAtomic = uint64(cons)
	e.MaxGRIDPerMinAtomic = uint64(mx)
	e.SettledAt = settledAt
	return e, nil
}

// AddConsumption implements payment.EscrowStore.
//
// Uses a single UPDATE … WHERE … RETURNING with a CASE that yanks the
// consumption only when it would not exceed the escrow — so the check +
// write happens in one round trip atomically. RowsAffected==0 means the
// row exists but the deduction would exceed; the caller maps that to
// ErrEscrowExhausted.
func (s *PostgresEscrowStore) AddConsumption(ctx context.Context, sessionID uuid.UUID, delta uint64) (*payment.Escrow, error) {
	row := s.pool.QueryRow(ctx, `
		UPDATE vpn_session_escrow
		   SET consumed_grid_atomic = consumed_grid_atomic + $2,
		       last_heartbeat_at    = NOW()
		 WHERE session_id = $1
		   AND consumed_grid_atomic + $2 <= escrowed_grid_atomic
		 RETURNING session_id, customer_id, wallet_address,
		           escrowed_grid_atomic, consumed_grid_atomic,
		           max_grid_per_min_atomic, nonce,
		           started_at, last_heartbeat_at, settled_at`,
		sessionID, int64(delta))
	e := &payment.Escrow{}
	var esc, cons, mx int64
	var settledAt *time.Time
	if err := row.Scan(
		&e.SessionID, &e.CustomerID, &e.WalletAddress,
		&esc, &cons, &mx, &e.Nonce,
		&e.StartedAt, &e.LastHeartbeatAt, &settledAt,
	); err != nil {
		// pgx.ErrNoRows OR the deduction would exceed; distinguish by
		// querying the actual escrow vs an indicator row.
		var exists bool
		if err2 := s.pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM vpn_session_escrow WHERE session_id = $1)`,
			sessionID).Scan(&exists); err2 != nil {
			return nil, err
		}
		if exists {
			return nil, payment.ErrEscrowExhausted
		}
		return nil, err
	}
	e.EscrowedAtomic = uint64(esc)
	e.ConsumedAtomic = uint64(cons)
	e.MaxGRIDPerMinAtomic = uint64(mx)
	e.SettledAt = settledAt
	return e, nil
}

// SettleEscrow implements payment.EscrowStore.
func (s *PostgresEscrowStore) SettleEscrow(ctx context.Context, sessionID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE vpn_session_escrow SET settled_at = COALESCE(settled_at, NOW()) WHERE session_id = $1`,
		sessionID)
	return err
}
