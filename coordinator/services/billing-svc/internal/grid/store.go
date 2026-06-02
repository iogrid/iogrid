package grid

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound mirrors store.ErrNotFound but local to this package so
// callers don't import billing-svc/internal/store circularly.
var ErrNotFound = errors.New("grid: not found")

// PostgresStore is the Postgres implementation of Store.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore wraps the pgx pool.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{pool: pool} }

// InsertSettlement implements Store.
func (p *PostgresStore) InsertSettlement(ctx context.Context, s *Settlement) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO grid_settlement (
			id, session_id, customer_wallet, provider_wallet, provider_id,
			bytes_in, bytes_out,
			escrowed_atomic, consumed_atomic, refund_atomic,
			provider_share, iogrid_share,
			created_at, settled_at, tx_signature, settle_attempts, last_error
		) VALUES ($1, $2, $3, NULLIF($4,''), NULLIF($5,'00000000-0000-0000-0000-000000000000')::uuid,
			$6, $7, $8, $9, $10, $11, $12, $13, $14, NULLIF($15,''), $16, NULLIF($17,''))
		ON CONFLICT (session_id) DO NOTHING`,
		s.ID, s.SessionID, s.CustomerWallet, s.ProviderWallet, s.ProviderID.String(),
		int64(s.BytesIn), int64(s.BytesOut),
		int64(s.EscrowedAtomic), int64(s.ConsumedAtomic), int64(s.RefundAtomic),
		int64(s.ProviderShare), int64(s.IogridShare),
		s.CreatedAt, s.SettledAt, s.TxSignature, s.SettleAttempts, s.LastError,
	)
	return err
}

// GetSettlementBySession implements Store.
func (p *PostgresStore) GetSettlementBySession(ctx context.Context, sessionID uuid.UUID) (*Settlement, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, session_id, customer_wallet, COALESCE(provider_wallet,''),
		       COALESCE(provider_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       bytes_in, bytes_out,
		       escrowed_atomic, consumed_atomic, refund_atomic,
		       provider_share, iogrid_share,
		       created_at, settled_at, COALESCE(tx_signature,''),
		       settle_attempts, COALESCE(last_error,'')
		  FROM grid_settlement WHERE session_id = $1`,
		sessionID)
	s := &Settlement{}
	var bi, bo, esc, cons, refund, ps, ic int64
	var settledAt *time.Time
	if err := row.Scan(
		&s.ID, &s.SessionID, &s.CustomerWallet, &s.ProviderWallet, &s.ProviderID,
		&bi, &bo, &esc, &cons, &refund, &ps, &ic,
		&s.CreatedAt, &settledAt, &s.TxSignature, &s.SettleAttempts, &s.LastError,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	s.BytesIn = uint64(bi)
	s.BytesOut = uint64(bo)
	s.EscrowedAtomic = uint64(esc)
	s.ConsumedAtomic = uint64(cons)
	s.RefundAtomic = uint64(refund)
	s.ProviderShare = uint64(ps)
	s.IogridShare = uint64(ic)
	s.SettledAt = settledAt
	return s, nil
}

// ListUnsettledByWallet returns up to `limit` unsettled rows grouped by
// provider_wallet — the input shape the settlement-worker batches over.
// Each map entry is (wallet → []rows); the worker submits one batched
// TransferChecked per wallet.
func (p *PostgresStore) ListUnsettledByWallet(ctx context.Context, limit int) (map[string][]*Settlement, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := p.pool.Query(ctx, `
		SELECT id, session_id, customer_wallet, COALESCE(provider_wallet,''),
		       COALESCE(provider_id, '00000000-0000-0000-0000-000000000000'::uuid),
		       bytes_in, bytes_out,
		       escrowed_atomic, consumed_atomic, refund_atomic,
		       provider_share, iogrid_share,
		       created_at, settled_at, COALESCE(tx_signature,''),
		       settle_attempts, COALESCE(last_error,'')
		  FROM grid_settlement
		 WHERE settled_at IS NULL
		   AND provider_wallet IS NOT NULL
		   AND provider_share >= $2
		 ORDER BY created_at ASC
		 LIMIT $1`,
		limit, int64(MinSettlementAtomic))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string][]*Settlement)
	for rows.Next() {
		s := &Settlement{}
		var bi, bo, esc, cons, refund, ps, ic int64
		var settledAt *time.Time
		if err := rows.Scan(
			&s.ID, &s.SessionID, &s.CustomerWallet, &s.ProviderWallet, &s.ProviderID,
			&bi, &bo, &esc, &cons, &refund, &ps, &ic,
			&s.CreatedAt, &settledAt, &s.TxSignature, &s.SettleAttempts, &s.LastError,
		); err != nil {
			return nil, err
		}
		s.BytesIn = uint64(bi)
		s.BytesOut = uint64(bo)
		s.EscrowedAtomic = uint64(esc)
		s.ConsumedAtomic = uint64(cons)
		s.RefundAtomic = uint64(refund)
		s.ProviderShare = uint64(ps)
		s.IogridShare = uint64(ic)
		s.SettledAt = settledAt
		out[s.ProviderWallet] = append(out[s.ProviderWallet], s)
	}
	return out, rows.Err()
}

// MarkSettled stamps settled_at + tx_signature on the rows by id. Called
// after a successful on-chain confirm.
func (p *PostgresStore) MarkSettled(ctx context.Context, ids []uuid.UUID, txSig string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
		UPDATE grid_settlement
		   SET settled_at = COALESCE(settled_at, NOW()),
		       tx_signature = COALESCE(NULLIF($2,''), tx_signature)
		 WHERE id = ANY($1::uuid[])
		   AND settled_at IS NULL`,
		ids, txSig)
	return err
}

// MarkAttemptFailed bumps settle_attempts + records last_error on the rows.
// Called by the worker on a settle failure so dashboards can show
// retry-count + the most-recent error.
func (p *PostgresStore) MarkAttemptFailed(ctx context.Context, ids []uuid.UUID, errMsg string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
		UPDATE grid_settlement
		   SET settle_attempts = settle_attempts + 1,
		       last_error      = $2
		 WHERE id = ANY($1::uuid[])
		   AND settled_at IS NULL`,
		ids, errMsg)
	return err
}

// SumGraceOverageOwedByCustomer returns the total prepaid-overage arrears
// (atomic $GRID) the customer wallet has accrued but not yet cleared — the
// "amount owed" surfaced on /customer/billing (#632).
//
// For each unsettled session the overage is max(0, consumed - escrowed):
// the slice of consumption that ran past what the customer pre-funded into
// escrow. Settled rows are excluded because their refund/settlement already
// reconciled. The sum is the arrears the next top-up must clear before
// further consumption (founder-ruled prepaid + capped-grace model).
func (p *PostgresStore) SumGraceOverageOwedByCustomer(ctx context.Context, customerWallet string) (uint64, error) {
	if customerWallet == "" {
		return 0, nil
	}
	row := p.pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(GREATEST(consumed_atomic - escrowed_atomic, 0)), 0)
		  FROM grid_settlement
		 WHERE customer_wallet = $1
		   AND settled_at IS NULL`,
		customerWallet)
	var owed int64
	if err := row.Scan(&owed); err != nil {
		return 0, err
	}
	if owed < 0 {
		owed = 0
	}
	return uint64(owed), nil
}

// FaucetClaim is one row of grid_devnet_faucet_log.
type FaucetClaim struct {
	ID            uuid.UUID
	WalletAddress string
	MintedAtomic  uint64
	TxSignature   string
	ClaimedAt     time.Time
}

// InsertFaucetClaim records a successful devnet faucet drop. The handler
// queries LastFaucetClaim before minting to enforce the 1/hour rate limit.
func (p *PostgresStore) InsertFaucetClaim(ctx context.Context, c *FaucetClaim) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	if c.ClaimedAt.IsZero() {
		c.ClaimedAt = time.Now().UTC()
	}
	_, err := p.pool.Exec(ctx, `
		INSERT INTO grid_devnet_faucet_log (id, wallet_address, minted_atomic, tx_signature, claimed_at)
		VALUES ($1, $2, $3, NULLIF($4,''), $5)`,
		c.ID, c.WalletAddress, int64(c.MintedAtomic), c.TxSignature, c.ClaimedAt)
	return err
}

// LastFaucetClaim returns the most recent claim for the wallet, or
// ErrNotFound if none.
func (p *PostgresStore) LastFaucetClaim(ctx context.Context, wallet string) (*FaucetClaim, error) {
	row := p.pool.QueryRow(ctx, `
		SELECT id, wallet_address, minted_atomic, COALESCE(tx_signature,''), claimed_at
		  FROM grid_devnet_faucet_log
		 WHERE wallet_address = $1
		 ORDER BY claimed_at DESC LIMIT 1`,
		wallet)
	c := &FaucetClaim{}
	var minted int64
	if err := row.Scan(&c.ID, &c.WalletAddress, &minted, &c.TxSignature, &c.ClaimedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	c.MintedAtomic = uint64(minted)
	return c, nil
}
