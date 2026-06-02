// Package grid implements the $GRID session-burn meter + settlement
// arithmetic for billing-svc.
//
// Refs iogrid/iogrid#597 (Track 5 / EPIC #581).
//
// The flow:
//
//   1. vpn-svc terminates a session and POSTs to /v1/grid/session-end with
//      the (session_id, customer_wallet, escrowed, consumed, bytes_in,
//      bytes_out) tuple.
//   2. SessionMeter.Settle() computes:
//          refund         = max(0, escrowed - consumed)
//          provider_share = consumed * ProviderSharePct / 100
//          iogrid_share   = consumed - provider_share
//      … and writes a grid_settlement row with status=PENDING and
//      tx_signature=NULL.
//   3. The settlement-worker cron (#598) selects unsettled rows GROUPed by
//      provider_wallet and submits one batched SPL TransferChecked per
//      wallet per tick.
//   4. On a successful on-chain confirm, the worker stamps settled_at +
//      tx_signature.
//
// Pure arithmetic + a Store interface — easy to unit-test without Postgres.
package grid

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ProviderSharePct is the fraction of consumed $GRID that goes to the
// residential provider. Locked at 85% per EPIC #581 LOCKED MODEL.
const ProviderSharePct = 85

// IogridSharePct is the residual 15% commission. Sum of shares == 100 by
// definition; computed as `consumed - provider_share` to avoid rounding
// drift at the GB boundary.
const IogridSharePct = 100 - ProviderSharePct

// MinSettlementAtomic is the smallest consumed amount we bother queuing
// for an on-chain transfer. Below this, both shares fall into the dust
// bucket and we'd lose the entire share to Solana txn fees. The threshold
// is 1_000_000 atomic units (= 0.001 GRID = 1 GB worth) — anything below
// represents <1GB of traffic and we fold it into the next session's
// settlement.
const MinSettlementAtomic = 1_000_000

// Settlement is one row of grid_settlement. Mirrors the Postgres schema in
// migrations/0006_grid_settlement.sql.
type Settlement struct {
	ID              uuid.UUID
	SessionID       uuid.UUID
	CustomerWallet  string
	ProviderWallet  string // empty if vpn-svc didn't supply yet — worker re-resolves before submit
	ProviderID      uuid.UUID
	BytesIn         uint64
	BytesOut        uint64
	EscrowedAtomic  uint64
	ConsumedAtomic  uint64
	RefundAtomic    uint64
	ProviderShare   uint64
	IogridShare     uint64
	CreatedAt       time.Time
	SettledAt       *time.Time
	TxSignature     string
	SettleAttempts  int
	LastError       string
}

// ComputeShares is the pure-function arithmetic. Used by both the meter
// (which writes the row) and the worker (which double-checks before
// submitting). Always returns provider_share + iogrid_share == consumed.
//
// Rounding mode: iogrid_share is the residual (consumed - provider_share)
// so the integer split is exact.
func ComputeShares(consumedAtomic uint64) (providerShare, iogridShare uint64) {
	providerShare = consumedAtomic * ProviderSharePct / 100
	iogridShare = consumedAtomic - providerShare
	return
}

// ComputeRefund returns max(0, escrowed - consumed). Implemented to be
// explicit about the wraparound case (consumed > escrowed should never
// happen given the AddConsumption gate in vpn-svc, but we defend in depth).
func ComputeRefund(escrowed, consumed uint64) uint64 {
	if escrowed <= consumed {
		return 0
	}
	return escrowed - consumed
}

// Input is what /v1/grid/session-end accepts on the wire.
type Input struct {
	SessionID      uuid.UUID `json:"session_id"`
	CustomerID     uuid.UUID `json:"customer_id"`
	CustomerWallet string    `json:"wallet_address"`
	ProviderWallet string    `json:"provider_wallet,omitempty"`
	ProviderID     uuid.UUID `json:"provider_id,omitempty"`
	BytesIn        uint64    `json:"bytes_in"`
	BytesOut       uint64    `json:"bytes_out"`
	EscrowedAtomic uint64    `json:"escrowed_atomic"`
	ConsumedAtomic uint64    `json:"consumed_atomic"`
	StartedAt      time.Time `json:"started_at"`
	EndedAt        time.Time `json:"ended_at"`
}

// Store is the persistence boundary. Postgres impl lives in
// internal/grid/store.go.
type Store interface {
	InsertSettlement(ctx context.Context, s *Settlement) error
	GetSettlementBySession(ctx context.Context, sessionID uuid.UUID) (*Settlement, error)
}

// MetricsRecorder is a tiny abstraction so the meter doesn't import the
// prometheus client directly — keeps the unit tests dependency-free.
type MetricsRecorder interface {
	RecordConsumed(atomic uint64)
	RecordProviderPayoutQueued(atomic uint64)
	RecordIogridCommission(atomic uint64)
}

// SessionMeter is the entry point — called from the /v1/grid/session-end
// handler. Settle is idempotent: if a row already exists for the
// session_id we re-fetch and return it unchanged.
type SessionMeter struct {
	St      Store
	Metrics MetricsRecorder
}

// ErrNoConsumption is returned when a session ended without burning any
// $GRID (e.g. immediate disconnect). The caller may discard the input
// rather than persist a zero-everything row.
var ErrNoConsumption = errors.New("grid: session ended with zero consumption")

// Settle creates (or fetches) the grid_settlement row for the given input.
// The on-chain transfer is queued separately by the settlement-worker.
func (m *SessionMeter) Settle(ctx context.Context, in Input) (*Settlement, error) {
	if in.SessionID == uuid.Nil {
		return nil, errors.New("grid: session_id required")
	}
	if in.CustomerWallet == "" {
		return nil, errors.New("grid: customer wallet required")
	}
	if in.ConsumedAtomic == 0 {
		// Still record the row so we have an audit entry, but skip the
		// on-chain queue. (provider+iogrid shares are zero.)
	}
	existing, err := m.St.GetSettlementBySession(ctx, in.SessionID)
	if err == nil && existing != nil {
		return existing, nil // idempotent re-call
	}
	providerShare, iogridShare := ComputeShares(in.ConsumedAtomic)
	refund := ComputeRefund(in.EscrowedAtomic, in.ConsumedAtomic)
	row := &Settlement{
		ID:             uuid.New(),
		SessionID:      in.SessionID,
		CustomerWallet: in.CustomerWallet,
		ProviderWallet: in.ProviderWallet,
		ProviderID:     in.ProviderID,
		BytesIn:        in.BytesIn,
		BytesOut:       in.BytesOut,
		EscrowedAtomic: in.EscrowedAtomic,
		ConsumedAtomic: in.ConsumedAtomic,
		RefundAtomic:   refund,
		ProviderShare:  providerShare,
		IogridShare:    iogridShare,
		CreatedAt:      time.Now().UTC(),
	}
	if err := m.St.InsertSettlement(ctx, row); err != nil {
		return nil, fmt.Errorf("grid: insert settlement: %w", err)
	}
	if m.Metrics != nil {
		m.Metrics.RecordConsumed(in.ConsumedAtomic)
		if providerShare >= MinSettlementAtomic {
			m.Metrics.RecordProviderPayoutQueued(providerShare)
		}
		m.Metrics.RecordIogridCommission(iogridShare)
	}
	return row, nil
}
