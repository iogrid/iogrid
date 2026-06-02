package grid

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/google/uuid"
)

// SettlementCron is the long-running tick loop used by the
// settlement-worker binary. It selects unsettled rows GROUPed by
// provider_wallet and submits one SPL TransferChecked per (wallet, tick).
//
// Refs iogrid/iogrid#598.
//
// Design notes:
//
//   - Defaults to a 5-minute tick (LOCKED MODEL).
//   - Pre-flight: query treasury $GRID balance; abort the tick if it's
//     below the total to be paid out (manual top-up required).
//   - Per wallet: sum provider_share atomic units, submit a single
//     TransferChecked, then mark all rows settled with that signature.
//   - On 3 consecutive failed ticks: emit a stuck alert via the
//     Alerter callback (prometheus + chepherd hook).
type SettlementCron struct {
	Store      *PostgresStore
	Solana     SolanaTransferer
	Metrics    SettlementMetrics
	Logger     *slog.Logger
	Tick       time.Duration
	BatchLimit int
	Alerter    AlertCallback
	// failuresInARow is shared across ticks; reset to 0 on the first
	// success. Internal.
	failuresInARow int
}

// SolanaTransferer is the narrow slice of solana.Service the cron uses —
// keeps unit tests simple (no Solana RPC needed).
type SolanaTransferer interface {
	Enabled() bool
	WalletAddress() string
	// GRIDAtomicTreasuryBalance returns the treasury ATA balance.
	GRIDAtomicTreasuryBalance(ctx context.Context) (uint64, error)
	// TransferGRID transfers `amount` atomic units to the recipient wallet.
	TransferGRID(ctx context.Context, destWallet common.PublicKey, amount uint64) (string, error)
}

// SettlementMetrics is a narrow recorder for the cron's per-tick
// outcomes — implemented by PromMetrics.
type SettlementMetrics interface {
	SettledOK(n int)
	SettledFailed(n int)
}

// AlertCallback fires when the cron has failed `n` consecutive ticks.
// Production wires this to chepherd.alert_human.
type AlertCallback func(ctx context.Context, body string)

// Run blocks until ctx is canceled. Runs RunOnce on a Tick interval.
func (c *SettlementCron) Run(ctx context.Context) error {
	tick := c.Tick
	if tick == 0 {
		tick = 5 * time.Minute
	}
	// Run once immediately so a freshly-deployed pod doesn't sit idle for
	// 5 min before the first settlement.
	if err := c.RunOnce(ctx); err != nil {
		c.logErr("initial tick failed", err)
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := c.RunOnce(ctx); err != nil {
				c.logErr("tick failed", err)
			}
		}
	}
}

// RunOnce processes one tick. Exposed for tests + an /admin/settle-now
// route. Returns the first error encountered (subsequent wallets in the
// same tick still get attempted).
func (c *SettlementCron) RunOnce(ctx context.Context) error {
	if c.Solana == nil || !c.Solana.Enabled() {
		c.logInfo("solana stub mode — skipping settlement tick")
		return nil
	}
	limit := c.BatchLimit
	if limit <= 0 {
		limit = 500
	}
	groups, err := c.Store.ListUnsettledByWallet(ctx, limit)
	if err != nil {
		return fmt.Errorf("list unsettled: %w", err)
	}
	if len(groups) == 0 {
		c.logInfo("no unsettled rows")
		c.failuresInARow = 0
		return nil
	}
	// Pre-flight: treasury balance >= sum of all provider shares?
	var grandTotal uint64
	for _, rows := range groups {
		for _, r := range rows {
			grandTotal += r.ProviderShare
		}
	}
	bal, err := c.Solana.GRIDAtomicTreasuryBalance(ctx)
	if err != nil {
		c.bumpFailure(ctx, "treasury balance fetch failed: "+err.Error())
		return fmt.Errorf("treasury balance: %w", err)
	}
	if bal < grandTotal {
		c.bumpFailure(ctx, fmt.Sprintf(
			"treasury balance %d < required %d — needs manual top-up", bal, grandTotal))
		return errors.New("treasury balance insufficient")
	}

	var firstErr error
	totalOK := 0
	totalFail := 0
	for wallet, rows := range groups {
		var sum uint64
		ids := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			sum += r.ProviderShare
			ids = append(ids, r.ID)
		}
		if sum == 0 {
			continue
		}
		recipient := common.PublicKeyFromString(wallet)
		sig, err := c.Solana.TransferGRID(ctx, recipient, sum)
		if err != nil {
			c.logErr("transfer failed",
				fmt.Errorf("wallet=%s sum=%d: %w", wallet, sum, err))
			if firstErr == nil {
				firstErr = err
			}
			_ = c.Store.MarkAttemptFailed(ctx, ids, err.Error())
			totalFail += len(ids)
			continue
		}
		if err := c.Store.MarkSettled(ctx, ids, sig); err != nil {
			c.logErr("mark settled failed", err)
			if firstErr == nil {
				firstErr = err
			}
			totalFail += len(ids)
			continue
		}
		totalOK += len(ids)
		c.logInfo("settlement batch confirmed",
			"wallet", wallet, "rows", len(ids), "sum_atomic", sum, "sig", sig)
	}
	if c.Metrics != nil {
		if totalOK > 0 {
			c.Metrics.SettledOK(totalOK)
		}
		if totalFail > 0 {
			c.Metrics.SettledFailed(totalFail)
		}
	}
	if firstErr != nil {
		c.bumpFailure(ctx, "settlement tick had failures: "+firstErr.Error())
		return firstErr
	}
	c.failuresInARow = 0
	return nil
}

func (c *SettlementCron) bumpFailure(ctx context.Context, body string) {
	c.failuresInARow++
	c.logErr("settlement tick failed", errors.New(body))
	if c.failuresInARow >= 3 && c.Alerter != nil {
		c.Alerter(ctx, fmt.Sprintf("settlement-worker: %d consecutive failures — %s",
			c.failuresInARow, body))
	}
}

func (c *SettlementCron) logInfo(msg string, kv ...any) {
	if c.Logger == nil {
		return
	}
	c.Logger.Info(msg, slogKV(kv)...)
}

func (c *SettlementCron) logErr(msg string, err error) {
	if c.Logger == nil {
		return
	}
	c.Logger.Error(msg, slog.String("error", err.Error()))
}

func slogKV(kv []any) []any {
	out := make([]any, 0, len(kv))
	for i := 0; i+1 < len(kv); i += 2 {
		k, _ := kv[i].(string)
		out = append(out, slog.Any(k, kv[i+1]))
	}
	return out
}
