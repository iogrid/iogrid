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
//
// The settled on-chain $GRID half (grid_build_settlement +
// grid_settlement, #748) is ADDED on top: those ledgers carry the real
// provider_share that actually moved on-chain (devnet), keyed by
// provider_id, and were previously surfaced NOWHERE on the dashboard —
// so a provider who earned 4.675 $GRID across real iOS builds still saw
// "0 $GRID" because usage_event.cost_cents summed to ~0. See #758.
type EarningsTotals struct {
	LifetimeMicros      int64
	Last30DMicros       int64
	Last7DMicros        int64
	PendingPayoutMicros int64
	LifetimeWorkloads   int64
	Currency            string
	// SettledGridMicros is the lifetime on-chain $GRID provider_share
	// (build + session settlements) for this provider, in micros. Folded
	// into LifetimeMicros/PendingPayoutMicros above; exposed separately so
	// callers/tests can assert the on-chain contribution in isolation.
	SettledGridMicros int64
	// SettledBuilds is the count of settled iOS-build settlement rows
	// (grid_build_settlement) attributed to this provider — the "builds"
	// number the dashboard renders. A settled row == a build that ran +
	// paid out on-chain.
	SettledBuilds int64
}

// atomicGridPerMicro converts atomic $GRID (9 decimals, 1e9 atomic ==
// 1 $GRID) to Money micros (1e6 micros == 1 unit): micros = atomic / 1000.
// $GRID amounts on the settlement ledgers are whole-lamport multiples
// well above 1000 atomic (MinSettlementAtomic == 1_000_000), so the
// integer division loses no economically-meaningful precision.
const atomicGridPerMicro = 1_000 // 1e9 atomic / 1e6 micros

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
// On top of usage_event we ADD the real on-chain $GRID provider_share
// from the settlement ledgers (grid_build_settlement + grid_settlement,
// #748/#758). usage_event.cost_cents is the legacy Phase-0 metering basis
// (and was ~0 in prod because VPN sessions logged 0 bytes and build
// metering only recorded a few cents); the settlement ledgers are where
// the $GRID that ACTUALLY moved on-chain lives. Folding them in is what
// makes a provider who earned 4.675 $GRID across real builds finally see
// a non-zero headline. SettledGridMicros only counts settled_at IS NOT
// NULL rows so the figure is "$GRID confirmed on-chain", not pending dust.
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

	// Fold in the on-chain settled $GRID provider_share (#758). Best-effort:
	// if the settlement ledgers are absent (e.g. a fresh test DB without the
	// grid migrations) we keep the usage_event-only totals rather than fail
	// the whole earnings card.
	settledAtomic, builds, build30, build7, serr := s.sumSettledGrid(ctx, providerID, now)
	if serr == nil {
		t.SettledGridMicros = settledAtomic / atomicGridPerMicro
		t.SettledBuilds = builds
		t.LifetimeMicros += t.SettledGridMicros
		t.Last30DMicros += build30 / atomicGridPerMicro
		t.Last7DMicros += build7 / atomicGridPerMicro
		// Any real on-chain $GRID makes the currency unambiguously GRID,
		// even if usage_event happened to default to "USD".
		if settledAtomic > 0 {
			t.Currency = "GRID"
		}
		// Each settled build is a real, paid workload — surface it in the
		// workload count so the "N workloads" hint isn't "No workloads yet"
		// for a provider that actually ran builds.
		t.LifetimeWorkloads += builds
	}

	// Pending = lifetime credited (the off-ramp cron is gated on #274).
	t.PendingPayoutMicros = t.LifetimeMicros
	return t, nil
}

// sumSettledGrid returns this provider's settled on-chain $GRID
// provider_share, in atomic units, across both settlement ledgers, plus
// the settled-build count and the 30d/7d build-settlement windows (atomic).
// "Settled" == settled_at IS NOT NULL == confirmed on-chain. Session
// settlements (grid_settlement) are included for completeness; in prod
// today only build settlements have rows, but a VPN provider's settled
// bandwidth $GRID belongs on the same card.
//
// The window split is computed off grid_build_settlement.settled_at only
// (the session ledger has no per-row recorded_at distinct from settled_at
// that maps cleanly onto the chart windows; its lifetime share is still
// counted in the build+session lifetime sum below).
func (s *Store) sumSettledGrid(ctx context.Context, providerID uuid.UUID, now time.Time) (lifetimeAtomic, builds, win30Atomic, win7Atomic int64, err error) {
	const q = `
WITH bs AS (
    SELECT provider_share, settled_at
      FROM grid_build_settlement
     WHERE provider_id = $1 AND settled_at IS NOT NULL
), ss AS (
    SELECT provider_share
      FROM grid_settlement
     WHERE provider_id = $1 AND settled_at IS NOT NULL
)
SELECT
    COALESCE((SELECT SUM(provider_share) FROM bs), 0)
      + COALESCE((SELECT SUM(provider_share) FROM ss), 0)            AS lifetime_atomic,
    COALESCE((SELECT COUNT(*) FROM bs), 0)                           AS builds,
    COALESCE((SELECT SUM(provider_share) FROM bs WHERE settled_at >= $2), 0) AS win30_atomic,
    COALESCE((SELECT SUM(provider_share) FROM bs WHERE settled_at >= $3), 0) AS win7_atomic`
	err = s.pool.QueryRow(ctx, q, providerID,
		now.Add(-30*24*time.Hour), now.Add(-7*24*time.Hour),
	).Scan(&lifetimeAtomic, &builds, &win30Atomic, &win7Atomic)
	return lifetimeAtomic, builds, win30Atomic, win7Atomic, err
}
