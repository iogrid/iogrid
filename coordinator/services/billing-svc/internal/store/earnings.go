// earnings.go: store layer for the /provide/earnings headline-card
// surface (#324). Two concerns:
//
//   - PayoutMethod election (per user): see payout_methods table from
//     migration 0005_payout_methods.sql.
//   - Derived earnings totals (lifetime, last_30d, last_7d, pending,
//     workload count): aggregated off the existing usage_event rows.
//     Phase 0 has no real metering, so every aggregation collapses to
//     zero — which is the contract the empty-state UI #312/#315 expects.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ── PayoutMethod ────────────────────────────────────────────────────

// PayoutMethod mirrors the proto PayoutMethod message. Stored as one
// row per user_id in the payout_methods table.
type PayoutMethod struct {
	UserID             uuid.UUID
	Kind               string // 'UNSPECIFIED' | 'CASH_USDC' | 'FREE_VPN' | 'CHARITY'
	DestinationAddress string
	CharityID          string
	UpdatedAt          time.Time
}

// GetPayoutMethod returns the saved election for a user. When no row
// exists we synthesise an UNSPECIFIED record so callers don't need to
// special-case ErrNotFound — the /provide/earnings page treats
// UNSPECIFIED as "hold $GRID (default)" anyway.
func (s *Store) GetPayoutMethod(ctx context.Context, userID uuid.UUID) (*PayoutMethod, error) {
	const q = `
SELECT user_id, kind, destination_address, charity_id, updated_at
  FROM payout_methods
 WHERE user_id = $1`
	var m PayoutMethod
	err := s.pool.QueryRow(ctx, q, userID).Scan(
		&m.UserID, &m.Kind, &m.DestinationAddress, &m.CharityID, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return &PayoutMethod{
			UserID:    userID,
			Kind:      "UNSPECIFIED",
			UpdatedAt: time.Time{},
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// UpsertPayoutMethod persists the election. Last write wins on user_id.
func (s *Store) UpsertPayoutMethod(ctx context.Context, m PayoutMethod) error {
	const q = `
INSERT INTO payout_methods (user_id, kind, destination_address, charity_id, updated_at)
VALUES ($1, $2, $3, $4, now())
ON CONFLICT (user_id) DO UPDATE SET
    kind                = EXCLUDED.kind,
    destination_address = EXCLUDED.destination_address,
    charity_id          = EXCLUDED.charity_id,
    updated_at          = now()`
	_, err := s.pool.Exec(ctx, q, m.UserID, m.Kind, m.DestinationAddress, m.CharityID)
	return err
}

// ── EarningsSummary aggregation ────────────────────────────────────

// EarningsTotals captures the five numeric rollups the headline cards
// render. cost_cents in the underlying usage_event table mirrors the
// $GRID ledger micros for Phase-0 native-currency earnings (#312); we
// surface micros directly so the proto-side Money.micros lines up.
type EarningsTotals struct {
	LifetimeMicros      int64
	Last30DMicros       int64
	Last7DMicros        int64
	PendingPayoutMicros int64
	LifetimeWorkloads   int64
	Currency            string
}

// SumProviderEarnings rolls up the headline numbers for a provider.
// All aggregations run against usage_event, which is the source of
// truth for what's been credited. Currency defaults to "GRID" when no
// rows exist — see #312/#315.
//
// Pending-payout is intentionally derived as "lifetime credited" in
// Phase 0: the off-ramp pipeline that actually moves $GRID → USD / VPN
// burn / charity forward (#274 founder mint) hasn't shipped yet, so
// every credited workload is by definition "pending payout". Once the
// solana payout/burn loop settles rows it will subtract its own
// settled-cents tally here. Until then we don't pretend a fictional
// settlement happened.
//
// SQL deliberately uses COALESCE so a zero-row provider returns
// zero-totals (not NULL) and we never raise ErrNoRows for an
// authenticated but unmetered Phase-0 provider.
func (s *Store) SumProviderEarnings(ctx context.Context, providerID uuid.UUID, now time.Time) (EarningsTotals, error) {
	const lifetimeQ = `
SELECT COALESCE(SUM(cost_cents), 0)::bigint AS total,
       COALESCE(COUNT(DISTINCT workload_id), 0)::bigint AS workloads,
       COALESCE(MAX(currency), 'GRID') AS currency
  FROM usage_event
 WHERE provider_id = $1`
	const windowQ = `
SELECT COALESCE(SUM(cost_cents), 0)::bigint
  FROM usage_event
 WHERE provider_id = $1 AND recorded_at >= $2`

	var t EarningsTotals
	if err := s.pool.QueryRow(ctx, lifetimeQ, providerID).Scan(
		&t.LifetimeMicros, &t.LifetimeWorkloads, &t.Currency,
	); err != nil {
		return EarningsTotals{}, err
	}
	if err := s.pool.QueryRow(ctx, windowQ, providerID, now.Add(-30*24*time.Hour)).Scan(&t.Last30DMicros); err != nil {
		return EarningsTotals{}, err
	}
	if err := s.pool.QueryRow(ctx, windowQ, providerID, now.Add(-7*24*time.Hour)).Scan(&t.Last7DMicros); err != nil {
		return EarningsTotals{}, err
	}
	// Pending = lifetime credited (the off-ramp cron is gated on #274).
	t.PendingPayoutMicros = t.LifetimeMicros
	return t, nil
}
