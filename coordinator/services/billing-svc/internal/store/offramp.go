package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// OffRampRequest mirrors a single row in offramp_request. Lifecycle is
// owned by billing-svc:
//
//	StartOffRamp        → INSERT row, status='pending'
//	HandleWebhook       → UPDATE row by (provider_name, provider_ref_id)
//	                       OR by id when the webhook can echo our ref
//
// All amount fields use the same units as the offramp.OffRampStatus
// canonical shape: grid_amount is uint64 lamports (9 decimals for $GRID),
// fiat_amount is a decimal string in major units of fiat_currency.
type OffRampRequest struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	ProviderName  string
	ProviderRefID *string
	WalletAddress string
	GridAmount    int64
	FiatAmount    *string
	FiatCurrency  string
	Status        string
	RedirectURL   string
	ReturnURL     *string
	TxnSignature  *string
	ErrorMessage  *string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CompletedAt   *time.Time
}

// InsertOffRampRequest persists a freshly-created off-ramp attempt.
func (s *Store) InsertOffRampRequest(ctx context.Context, r OffRampRequest) error {
	const q = `
INSERT INTO offramp_request (
    id, user_id, provider_name, provider_ref_id,
    wallet_address, grid_amount, fiat_amount, fiat_currency,
    status, redirect_url, return_url,
    txn_signature, error_message,
    created_at, updated_at, completed_at
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	if r.Status == "" {
		r.Status = "pending"
	}
	if r.FiatCurrency == "" {
		r.FiatCurrency = "USD"
	}
	now := time.Now().UTC()
	if r.CreatedAt.IsZero() {
		r.CreatedAt = now
	}
	if r.UpdatedAt.IsZero() {
		r.UpdatedAt = now
	}
	_, err := s.pool.Exec(ctx, q,
		r.ID, r.UserID, r.ProviderName, r.ProviderRefID,
		r.WalletAddress, r.GridAmount, r.FiatAmount, r.FiatCurrency,
		r.Status, r.RedirectURL, r.ReturnURL,
		r.TxnSignature, r.ErrorMessage,
		r.CreatedAt, r.UpdatedAt, r.CompletedAt,
	)
	return err
}

// GetOffRampRequest returns the row by id.
func (s *Store) GetOffRampRequest(ctx context.Context, id uuid.UUID) (*OffRampRequest, error) {
	const q = `
SELECT id, user_id, provider_name, provider_ref_id,
       wallet_address, grid_amount, fiat_amount, fiat_currency,
       status, redirect_url, return_url,
       txn_signature, error_message,
       created_at, updated_at, completed_at
  FROM offramp_request
 WHERE id = $1`
	var r OffRampRequest
	err := s.pool.QueryRow(ctx, q, id).Scan(
		&r.ID, &r.UserID, &r.ProviderName, &r.ProviderRefID,
		&r.WalletAddress, &r.GridAmount, &r.FiatAmount, &r.FiatCurrency,
		&r.Status, &r.RedirectURL, &r.ReturnURL,
		&r.TxnSignature, &r.ErrorMessage,
		&r.CreatedAt, &r.UpdatedAt, &r.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListOffRampRequestsByUser returns rows for the user, newest first.
func (s *Store) ListOffRampRequestsByUser(ctx context.Context, userID uuid.UUID, limit, offset int) ([]OffRampRequest, error) {
	const q = `
SELECT id, user_id, provider_name, provider_ref_id,
       wallet_address, grid_amount, fiat_amount, fiat_currency,
       status, redirect_url, return_url,
       txn_signature, error_message,
       created_at, updated_at, completed_at
  FROM offramp_request
 WHERE user_id = $1
 ORDER BY created_at DESC
 LIMIT $2 OFFSET $3`
	rows, err := s.pool.Query(ctx, q, userID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OffRampRequest
	for rows.Next() {
		var r OffRampRequest
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.ProviderName, &r.ProviderRefID,
			&r.WalletAddress, &r.GridAmount, &r.FiatAmount, &r.FiatCurrency,
			&r.Status, &r.RedirectURL, &r.ReturnURL,
			&r.TxnSignature, &r.ErrorMessage,
			&r.CreatedAt, &r.UpdatedAt, &r.CompletedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// OffRampStatusUpdate carries the partial update produced by a partner
// webhook. Nil pointers are not written (COALESCE in SQL).
type OffRampStatusUpdate struct {
	Status        string
	ProviderRefID *string
	FiatAmount    *string
	FiatCurrency  *string
	TxnSignature  *string
	ErrorMessage  *string
	CompletedAt   *time.Time
}

// UpdateOffRampRequestStatus moves a row through the lifecycle.
func (s *Store) UpdateOffRampRequestStatus(ctx context.Context, id uuid.UUID, u OffRampStatusUpdate) error {
	const q = `
UPDATE offramp_request
   SET status          = $2,
       provider_ref_id = COALESCE($3, provider_ref_id),
       fiat_amount     = COALESCE($4, fiat_amount),
       fiat_currency   = COALESCE($5, fiat_currency),
       txn_signature   = COALESCE($6, txn_signature),
       error_message   = COALESCE($7, error_message),
       completed_at    = COALESCE($8, completed_at),
       updated_at      = now()
 WHERE id = $1`
	tag, err := s.pool.Exec(ctx, q, id, u.Status,
		u.ProviderRefID, u.FiatAmount, u.FiatCurrency,
		u.TxnSignature, u.ErrorMessage, u.CompletedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
