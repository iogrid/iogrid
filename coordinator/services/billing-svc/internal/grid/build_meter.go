// build_meter.go implements the $GRID earnings meter for iOS-BUILD
// workloads — the provider-earnings half of G2/G3 (iOS builds through
// iogrid, paid in devnet $GRID).
//
// Mirrors session_meter.go (the VPN-bandwidth meter) but keyed on a
// (build_id, attempt_id) pair instead of a session_id. The economics are
// identical and intentionally share the same pure arithmetic
// (ComputeShares / ComputeRefund) so the provider's cut is the SAME 85%
// whether they shared bandwidth or ran a build:
//
//	refund         = max(0, escrowed - consumed)
//	provider_share = consumed * ProviderSharePct / 100   (85%)
//	iogrid_share   = consumed - provider_share           (15%)
//
// The flow:
//
//  1. build-gateway/workloads-svc finishes a build attempt and POSTs to
//     /v1/grid/build-end with the (build_id, attempt_id, customer_wallet,
//     provider_wallet/id, escrowed, consumed) tuple.
//  2. BuildMeter.Settle() computes the split and writes a build settlement
//     row with status=PENDING and tx_signature=NULL.
//  3. The same settlement-worker cron (#598) that drains session
//     settlements drains build settlements — one batched SPL
//     TransferChecked per provider wallet per tick.
//
// Devnet-only: the mint is the devnet $GRID (BaQvWwb1…WorR); mainnet is a
// founder go-live. Refs #700 (iOS builds EPIC), #581 (LOCKED economics).
package grid

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// BuildMemoPrefix is the canonical Ping memo schema for an iOS build pull,
// e.g. `iogrid.v1:build:ios:<spec>`. The mobile/customer side builds the
// full memo (see mobile/ios/src/lib/wallets/ping-pay.ts::buildBuildMemo);
// billing-svc only needs the prefix to recognise + route a build pull.
const BuildMemoPrefix = "iogrid.v1:build:ios:"

// BuildSettlement is one row of the build-earnings ledger. It mirrors
// Settlement (the VPN-session row) but carries the build identity instead
// of a session id, so the two ledgers can share the worker + the share
// arithmetic without conflating their primary keys.
type BuildSettlement struct {
	ID             uuid.UUID
	BuildID        uuid.UUID
	AttemptID      uuid.UUID
	CustomerWallet string
	ProviderWallet string // empty if not yet supplied — worker re-resolves
	ProviderID     uuid.UUID
	EscrowedAtomic uint64
	ConsumedAtomic uint64
	RefundAtomic   uint64
	ProviderShare  uint64
	IogridShare    uint64
	TxSignature    string
}

// BuildInput is what /v1/grid/build-end accepts on the wire.
type BuildInput struct {
	BuildID        uuid.UUID `json:"build_id"`
	AttemptID      uuid.UUID `json:"attempt_id"`
	CustomerID     uuid.UUID `json:"customer_id"`
	CustomerWallet string    `json:"wallet_address"`
	ProviderWallet string    `json:"provider_wallet,omitempty"`
	ProviderID     uuid.UUID `json:"provider_id,omitempty"`
	EscrowedAtomic uint64    `json:"escrowed_atomic"`
	ConsumedAtomic uint64    `json:"consumed_atomic"`
}

// BuildStore is the persistence boundary for build settlements. The
// Postgres impl lives alongside the session store in internal/grid/store.go.
type BuildStore interface {
	InsertBuildSettlement(ctx context.Context, s *BuildSettlement) error
	GetBuildSettlement(ctx context.Context, buildID, attemptID uuid.UUID) (*BuildSettlement, error)
}

// BuildMeter is the entry point — called from the /v1/grid/build-end
// handler. Settle is idempotent on (build_id, attempt_id): a re-POST of an
// already-settled attempt re-fetches and returns the existing row
// unchanged (so a build-gateway retry can't double-pay a provider).
type BuildMeter struct {
	St      BuildStore
	Metrics MetricsRecorder
}

// ErrNoBuildConsumption mirrors ErrNoConsumption: a build that burned no
// $GRID (e.g. rejected before it ran) yields no settlement row.
var ErrNoBuildConsumption = errors.New("grid: build ended with zero consumption")

// Settle creates (or fetches) the build settlement row for the input.
// Idempotent on (build_id, attempt_id). The on-chain transfer is queued
// separately by the settlement-worker.
func (m *BuildMeter) Settle(ctx context.Context, in BuildInput) (*BuildSettlement, error) {
	if in.BuildID == uuid.Nil {
		return nil, errors.New("grid: build_id required")
	}
	if in.AttemptID == uuid.Nil {
		return nil, errors.New("grid: attempt_id required")
	}
	if in.CustomerWallet == "" {
		return nil, errors.New("grid: customer wallet required")
	}
	if in.ConsumedAtomic == 0 {
		return nil, ErrNoBuildConsumption
	}

	// Idempotency: return the existing row if this attempt already settled.
	if existing, err := m.St.GetBuildSettlement(ctx, in.BuildID, in.AttemptID); err == nil && existing != nil {
		return existing, nil
	}

	// Self-pay guard (#818): a real customer→provider economy requires the
	// build SUBMITTER's wallet (customer_wallet) and the Mac PROVIDER owner's
	// wallet (provider_wallet) to be DISTINCT — the customer pays, the
	// provider earns. When they're the same address (the dogfood case, where
	// one identity both submits the build and owns the provider Mac), there is
	// no real transfer to make: paying that wallet would move treasury $GRID to
	// the very party who "spent" it, manufacturing fake earnings. We still
	// persist the row for audit + idempotency, but with provider_wallet cleared
	// so the settlement-worker (which only drains rows WHERE provider_wallet
	// <> '') treats it as a non-payable self-pay row instead of transferring.
	providerWallet := in.ProviderWallet
	if providerWallet != "" && providerWallet == in.CustomerWallet {
		providerWallet = ""
	}

	providerShare, iogridShare := ComputeShares(in.ConsumedAtomic)
	row := &BuildSettlement{
		ID:             uuid.New(),
		BuildID:        in.BuildID,
		AttemptID:      in.AttemptID,
		CustomerWallet: in.CustomerWallet,
		ProviderWallet: providerWallet,
		ProviderID:     in.ProviderID,
		EscrowedAtomic: in.EscrowedAtomic,
		ConsumedAtomic: in.ConsumedAtomic,
		RefundAtomic:   ComputeRefund(in.EscrowedAtomic, in.ConsumedAtomic),
		ProviderShare:  providerShare,
		IogridShare:    iogridShare,
	}
	if err := m.St.InsertBuildSettlement(ctx, row); err != nil {
		return nil, err
	}
	if m.Metrics != nil {
		m.Metrics.RecordConsumed(in.ConsumedAtomic)
		m.Metrics.RecordProviderPayoutQueued(providerShare)
		m.Metrics.RecordIogridCommission(iogridShare)
	}
	return row, nil
}
