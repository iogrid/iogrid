package grid

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
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
	// Drain BOTH ledgers: VPN-session settlements (grid_settlement) AND
	// iOS-build settlements (grid_build_settlement). Before #748 only the
	// session table was drained, so build provider-shares never reached the
	// chain and providers' wallets stayed empty.
	sessionGroups, err := c.Store.ListUnsettledByWallet(ctx, limit)
	if err != nil {
		return fmt.Errorf("list unsettled sessions: %w", err)
	}
	buildGroups, err := c.Store.ListUnsettledBuildsByWallet(ctx, limit)
	if err != nil {
		return fmt.Errorf("list unsettled builds: %w", err)
	}
	sessionBatches := sessionBatchesByWallet(sessionGroups)
	buildBatches := buildBatchesByWallet(buildGroups)
	if len(sessionBatches) == 0 && len(buildBatches) == 0 {
		c.logInfo("no unsettled rows")
		c.failuresInARow = 0
		return nil
	}
	// Read the treasury balance ONCE per tick. We no longer abort the whole
	// tick when the balance is below the GRAND total — that all-or-nothing
	// pre-flight let a single oversized (often synthetic/self-pay) wallet
	// dead-lock every affordable payout for days (#818). Instead drainBatches
	// pays each wallet that fits the REMAINING balance and skips+alerts the
	// ones that don't, so forward progress is never blocked by one row.
	bal, err := c.Solana.GRIDAtomicTreasuryBalance(ctx)
	if err != nil {
		c.bumpFailure(ctx, "treasury balance fetch failed: "+err.Error())
		return fmt.Errorf("treasury balance: %w", err)
	}

	// budget is the running treasury balance the two drains share: session
	// payouts first, then build payouts, each decrementing what's left.
	budget := bal
	sOK, sFail, sSkip, sErr := c.drainBatches(ctx, sessionBatches, &budget, c.Store.MarkSettled, c.Store.MarkAttemptFailed)
	// Build settlements have no settle_attempts/last_error columns, so failures
	// simply retry next tick (markFailed = nil).
	bOK, bFail, bSkip, bErr := c.drainBatches(ctx, buildBatches, &budget, c.Store.MarkBuildSettled, nil)
	totalOK := sOK + bOK
	totalFail := sFail + bFail
	totalSkip := sSkip + bSkip
	firstErr := sErr
	if firstErr == nil {
		firstErr = bErr
	}
	if c.Metrics != nil {
		if totalOK > 0 {
			c.Metrics.SettledOK(totalOK)
		}
		if totalFail > 0 {
			c.Metrics.SettledFailed(totalFail)
		}
	}
	// A wallet we deliberately SKIPPED for insufficient funds is NOT a tick
	// failure — it raised its own scoped alert inside drainBatches. Only a
	// genuine transfer/mark error (firstErr) counts toward the consecutive-
	// failure counter. This is what unwedges the worker: an underfunded
	// oversized row no longer increments "1430 consecutive failures" forever.
	if firstErr != nil {
		c.bumpFailure(ctx, "settlement tick had failures: "+firstErr.Error())
		return firstErr
	}
	if totalSkip > 0 {
		c.logInfo("settlement tick made progress with skips",
			"settled_rows", totalOK, "skipped_wallets", totalSkip)
	}
	c.failuresInARow = 0
	return nil
}

// walletBatch is the per-wallet aggregate the cron transfers in one tx.
type walletBatch struct {
	ids []uuid.UUID
	sum uint64
}

// sessionBatchesByWallet aggregates session settlements into per-wallet
// (ids, sum) batches, dropping zero-share groups.
func sessionBatchesByWallet(groups map[string][]*Settlement) map[string]walletBatch {
	out := make(map[string]walletBatch, len(groups))
	for wallet, rows := range groups {
		var b walletBatch
		for _, r := range rows {
			b.sum += r.ProviderShare
			b.ids = append(b.ids, r.ID)
		}
		if b.sum > 0 {
			out[wallet] = b
		}
	}
	return out
}

// buildBatchesByWallet is the build-settlement analogue (#748).
func buildBatchesByWallet(groups map[string][]*BuildSettlement) map[string]walletBatch {
	out := make(map[string]walletBatch, len(groups))
	for wallet, rows := range groups {
		var b walletBatch
		for _, r := range rows {
			b.sum += r.ProviderShare
			b.ids = append(b.ids, r.ID)
		}
		if b.sum > 0 {
			out[wallet] = b
		}
	}
	return out
}

// drainBatches submits one TransferChecked per wallet and marks the rows
// settled (or failed). markFailed may be nil (build-settlement rows have no
// attempt-tracking columns — they simply retry on the next tick).
//
// budget is the REMAINING treasury balance (atomic $GRID). A wallet whose
// batch exceeds the remaining budget is SKIPPED (not failed): its rows stay
// unsettled for a future tick, it raises a scoped Alerter notification, and
// the rest of the wallets keep settling. This is the #818 fix — one oversized
// row no longer aborts the whole tick. budget is decremented by each settled
// batch and never goes negative.
func (c *SettlementCron) drainBatches(
	ctx context.Context,
	batches map[string]walletBatch,
	budget *uint64,
	markSettled func(context.Context, []uuid.UUID, string) error,
	markFailed func(context.Context, []uuid.UUID, string) error,
) (okN int, failN int, skipN int, firstErr error) {
	// Settle smallest-first so a single oversized wallet can never starve the
	// affordable backlog: we spend the budget on the cheapest payouts first.
	for _, wallet := range walletsBySumAsc(batches) {
		b := batches[wallet]
		if b.sum == 0 {
			continue
		}
		// Affordability: skip (don't fail) any batch that exceeds what's left.
		if b.sum > *budget {
			skipN++
			c.logErr("settlement batch skipped — insufficient treasury",
				fmt.Errorf("wallet=%s sum=%d > remaining_balance=%d — needs devnet top-up", wallet, b.sum, *budget))
			if c.Alerter != nil {
				c.Alerter(ctx, fmt.Sprintf(
					"settlement-worker: skipped wallet %s — payout %d > remaining treasury %d; needs top-up (other wallets still settling)",
					wallet, b.sum, *budget))
			}
			continue
		}
		recipient := common.PublicKeyFromString(wallet)
		sig, err := c.Solana.TransferGRID(ctx, recipient, b.sum)
		if err != nil {
			c.logErr("transfer failed",
				fmt.Errorf("wallet=%s sum=%d: %w", wallet, b.sum, err))
			if firstErr == nil {
				firstErr = err
			}
			if markFailed != nil {
				_ = markFailed(ctx, b.ids, err.Error())
			}
			failN += len(b.ids)
			continue
		}
		if err := markSettled(ctx, b.ids, sig); err != nil {
			c.logErr("mark settled failed", err)
			if firstErr == nil {
				firstErr = err
			}
			failN += len(b.ids)
			continue
		}
		*budget -= b.sum
		okN += len(b.ids)
		c.logInfo("settlement batch confirmed",
			"wallet", wallet, "rows", len(b.ids), "sum_atomic", b.sum, "sig", sig, "remaining_balance", *budget)
	}
	return okN, failN, skipN, firstErr
}

// walletsBySumAsc returns the wallet keys ordered by ascending batch sum so
// the budget is spent on the cheapest payouts first — maximising the number
// of providers paid when the treasury can't cover everyone.
func walletsBySumAsc(batches map[string]walletBatch) []string {
	wallets := make([]string, 0, len(batches))
	for w := range batches {
		wallets = append(wallets, w)
	}
	sort.SliceStable(wallets, func(i, j int) bool {
		return batches[wallets[i]].sum < batches[wallets[j]].sum
	})
	return wallets
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
