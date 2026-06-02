package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// WalletProvider matches the Postgres wallet_provider enum declared in
// 0005_wallet_binding.sql. Both providers store SPL $GRID on Solana; the
// discriminator records which client app the user paired through so the
// mobile app can deeplink back to the same wallet for top-up / signing.
type WalletProvider string

const (
	WalletProviderPhantom WalletProvider = "phantom"
	WalletProviderPing    WalletProvider = "ping"
)

// IsValidWalletProvider returns true when s matches one of the enum values.
func IsValidWalletProvider(s string) bool {
	switch WalletProvider(s) {
	case WalletProviderPhantom, WalletProviderPing:
		return true
	}
	return false
}

// CustomerWalletBinding is one row in customer_wallet_bindings — the
// consumer-side wallet surface for the mobile VPN app. One per user.
type CustomerWalletBinding struct {
	UserID              uuid.UUID
	WalletAddress       string
	WalletProvider      WalletProvider
	BoundAt             time.Time
	LastBalanceAt       *time.Time
	LastBalanceLamports *int64
}

// UpsertCustomerWalletBinding writes or replaces the user's bound wallet.
// A user holds at most one binding; calling this for an already-bound
// user replaces the prior row (matches the Settings → Wallet "Switch"
// UX in the wireframes-v2).
func (s *Store) UpsertCustomerWalletBinding(ctx context.Context, q Querier, b *CustomerWalletBinding) error {
	if q == nil {
		q = s.Pool
	}
	row := q.QueryRow(ctx, `
		INSERT INTO customer_wallet_bindings (user_id, wallet_address, wallet_provider, bound_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (user_id) DO UPDATE
		   SET wallet_address  = EXCLUDED.wallet_address,
		       wallet_provider = EXCLUDED.wallet_provider,
		       bound_at        = now(),
		       last_balance_at = NULL,
		       last_balance_lamports = NULL
		RETURNING bound_at`,
		b.UserID, b.WalletAddress, b.WalletProvider)
	return row.Scan(&b.BoundAt)
}

// GetCustomerWalletBinding loads the user's bound wallet. Returns
// ErrNotFound when the user has not bound a wallet yet (cleared by
// DeleteCustomerWalletBinding or just never set).
func (s *Store) GetCustomerWalletBinding(ctx context.Context, q Querier, userID uuid.UUID) (*CustomerWalletBinding, error) {
	if q == nil {
		q = s.Pool
	}
	b := &CustomerWalletBinding{}
	err := q.QueryRow(ctx, `
		SELECT user_id, wallet_address, wallet_provider, bound_at,
		       last_balance_at, last_balance_lamports
		  FROM customer_wallet_bindings
		 WHERE user_id = $1`, userID).
		Scan(&b.UserID, &b.WalletAddress, &b.WalletProvider, &b.BoundAt,
			&b.LastBalanceAt, &b.LastBalanceLamports)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// DeleteCustomerWalletBinding clears the user's bound wallet. Returns
// ErrNotFound when there was nothing to delete so the unbind handler can
// distinguish "already unbound" from a deeper error.
func (s *Store) DeleteCustomerWalletBinding(ctx context.Context, q Querier, userID uuid.UUID) error {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `DELETE FROM customer_wallet_bindings WHERE user_id = $1`, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateCustomerWalletBalanceCache records the most recent observed
// $GRID balance for the bound wallet. Used by mobile / server-side
// pollers to share a recent value without re-hitting the Solana RPC.
func (s *Store) UpdateCustomerWalletBalanceCache(ctx context.Context, q Querier, userID uuid.UUID, lamports int64) error {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `
		UPDATE customer_wallet_bindings
		   SET last_balance_at       = now(),
		       last_balance_lamports = $2
		 WHERE user_id = $1`, userID, lamports)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
