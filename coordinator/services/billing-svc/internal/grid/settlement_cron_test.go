package grid

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/google/uuid"
)

// stubTransferer is the test-only SolanaTransferer.
type stubTransferer struct {
	mu          sync.Mutex
	balance     uint64
	balErr      error
	transferErr error
	calls       []transferCall
	nextSig     string
}

type transferCall struct {
	dest   string
	amount uint64
}

func (s *stubTransferer) Enabled() bool          { return true }
func (s *stubTransferer) WalletAddress() string  { return "Tre1asury" }
func (s *stubTransferer) GRIDAtomicTreasuryBalance(ctx context.Context) (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.balance, s.balErr
}
func (s *stubTransferer) TransferGRID(ctx context.Context, dest common.PublicKey, amount uint64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, transferCall{dest: dest.ToBase58(), amount: amount})
	if s.transferErr != nil {
		return "", s.transferErr
	}
	sig := s.nextSig
	if sig == "" {
		sig = "sig-" + dest.ToBase58()
	}
	return sig, nil
}

// stubMetrics counts ok/fail.
type stubMetrics struct {
	mu   sync.Mutex
	ok   int
	fail int
}

func (s *stubMetrics) SettledOK(n int)     { s.mu.Lock(); s.ok += n; s.mu.Unlock() }
func (s *stubMetrics) SettledFailed(n int) { s.mu.Lock(); s.fail += n; s.mu.Unlock() }

// #748: the cron must drain iOS-BUILD settlements too. Before this it only
// drained grid_settlement (VPN sessions), so build provider-shares never
// reached the chain and providers' wallets stayed empty. Asserts
// buildBatchesByWallet aggregates correctly and drainBatches transfers the
// share + marks the rows settled with the tx signature.
func TestDrainBatches_BuildSettlements(t *testing.T) {
	const wallet = "3TuRAZPs7YUpcigdjoZsyQ7f7iLf6ZGbjcpsMfwhxbmT" // valid base58 so it round-trips
	stub := &stubTransferer{balance: 1_000_000_000, nextSig: "buildsig"}
	groups := map[string][]*BuildSettlement{
		wallet: {{ID: uuid.New(), ProviderWallet: wallet, ProviderShare: 425_000_000}},
	}
	batches := buildBatchesByWallet(groups)
	if len(batches) != 1 || batches[wallet].sum != 425_000_000 || len(batches[wallet].ids) != 1 {
		t.Fatalf("buildBatchesByWallet wrong: %+v", batches)
	}
	var markedIDs []uuid.UUID
	var markedSig string
	c := &SettlementCron{Solana: stub}
	budget := stub.balance
	ok, fail, skip, err := c.drainBatches(context.Background(), batches, &budget,
		func(_ context.Context, ids []uuid.UUID, sig string) error {
			markedIDs, markedSig = ids, sig
			return nil
		}, nil)
	if err != nil || ok != 1 || fail != 0 || skip != 0 {
		t.Fatalf("drainBatches: ok=%d fail=%d skip=%d err=%v", ok, fail, skip, err)
	}
	if budget != stub.balance-425_000_000 {
		t.Fatalf("budget not decremented: got %d want %d", budget, stub.balance-425_000_000)
	}
	if len(stub.calls) != 1 || stub.calls[0].amount != 425_000_000 || stub.calls[0].dest != wallet {
		t.Fatalf("transfer wrong: %+v", stub.calls)
	}
	if len(markedIDs) != 1 || markedSig != "buildsig" {
		t.Fatalf("mark wrong: ids=%v sig=%q", markedIDs, markedSig)
	}
}

// TestDrainBatches_SkipsOversizedPaysAffordable is the #818 regression test:
// a candidate set with ONE unaffordable wallet plus several affordable ones
// must pay the affordable wallets and SKIP (not fail, not abort) the
// unaffordable one. Before the fix, RunOnce aborted the whole tick on
// `treasury balance < grand total`, dead-locking ALL payouts for ~5 days on a
// single oversized synthetic row.
func TestDrainBatches_SkipsOversizedPaysAffordable(t *testing.T) {
	// Three valid base58 wallets so the recipients round-trip.
	const (
		small = "3TuRAZPs7YUpcigdjoZsyQ7f7iLf6ZGbjcpsMfwhxbmT"
		mid   = "5iaigRahctvgthUhUG5gaJTjfQXn9NQW7juAzWDQbE3Y"
		big   = "8Z8A7A6A9iLP32HpEwb33qMKceRwKp9BPxEmQ2Ya6ttf"
	)
	stub := &stubTransferer{balance: 10_000_000_000} // 10 GRID
	groups := map[string][]*BuildSettlement{
		small: {{ID: uuid.New(), ProviderWallet: small, ProviderShare: 1_000_000_000}}, // 1 GRID — affordable
		mid:   {{ID: uuid.New(), ProviderWallet: mid, ProviderShare: 2_000_000_000}},   // 2 GRID — affordable
		big:   {{ID: uuid.New(), ProviderWallet: big, ProviderShare: 29_000_000_000}},  // 29 GRID — UNAFFORDABLE
	}
	batches := buildBatchesByWallet(groups)

	var alerts []string
	c := &SettlementCron{
		Solana: stub,
		Alerter: func(_ context.Context, body string) {
			alerts = append(alerts, body)
		},
	}
	settled := map[string]bool{}
	budget := stub.balance
	ok, fail, skip, err := c.drainBatches(context.Background(), batches, &budget,
		func(_ context.Context, ids []uuid.UUID, sig string) error {
			_ = ids
			settled[sig] = true
			return nil
		}, nil)

	if err != nil {
		t.Fatalf("drainBatches returned error (should skip, not fail): %v", err)
	}
	if ok != 2 {
		t.Fatalf("expected 2 affordable rows settled, got %d", ok)
	}
	if fail != 0 {
		t.Fatalf("expected 0 failed rows, got %d", fail)
	}
	if skip != 1 {
		t.Fatalf("expected 1 skipped (oversized) wallet, got %d", skip)
	}
	// The oversized wallet must NOT have been transferred.
	for _, call := range stub.calls {
		if call.dest == big {
			t.Fatalf("oversized wallet %s was transferred — must be skipped", big)
		}
	}
	if len(stub.calls) != 2 {
		t.Fatalf("expected exactly 2 transfers (the affordable wallets), got %d", len(stub.calls))
	}
	// Budget started at 10 GRID, paid 1 + 2 = 3 GRID → 7 GRID remains.
	if budget != 7_000_000_000 {
		t.Fatalf("budget after affordable payouts: got %d want 7_000_000_000", budget)
	}
	// The skip must have raised exactly one scoped alert naming the wallet.
	if len(alerts) != 1 {
		t.Fatalf("expected 1 skip alert, got %d: %v", len(alerts), alerts)
	}
}

// stubStore is a tiny in-memory grid store implementing what the cron uses.
// The cron only calls ListUnsettledByWallet + MarkSettled + MarkAttemptFailed,
// so we plug a fake at the *PostgresStore field… but cron expects
// *PostgresStore concretely. To unit-test without Postgres we instead
// build the cron with reflection-friendly shim: extract the methods we
// need as function pointers. For this test we exercise the cron's
// orchestration via a wrapper that delegates to in-memory fakes.

type cronStore struct {
	mu sync.Mutex
	groups map[string][]*Settlement
	settled map[uuid.UUID]string
	failed  map[uuid.UUID]string
}

func newCronStore() *cronStore {
	return &cronStore{
		groups:  map[string][]*Settlement{},
		settled: map[uuid.UUID]string{},
		failed:  map[uuid.UUID]string{},
	}
}

// We use a small fork of the SettlementCron tuned to a Store interface for
// testing. Keeps the test free of postgres.
type testCron struct {
	St      *cronStore
	Solana  SolanaTransferer
	Metrics SettlementMetrics
	failuresInARow int
	Alerter AlertCallback
}

func (c *testCron) RunOnce(ctx context.Context) error {
	if !c.Solana.Enabled() {
		return nil
	}
	c.St.mu.Lock()
	groups := make(map[string][]*Settlement, len(c.St.groups))
	for k, v := range c.St.groups {
		// only unsettled
		rows := make([]*Settlement, 0)
		for _, r := range v {
			if _, ok := c.St.settled[r.ID]; ok {
				continue
			}
			rows = append(rows, r)
		}
		if len(rows) > 0 {
			groups[k] = rows
		}
	}
	c.St.mu.Unlock()
	if len(groups) == 0 {
		c.failuresInARow = 0
		return nil
	}
	budget, err := c.Solana.GRIDAtomicTreasuryBalance(ctx)
	if err != nil {
		c.failuresInARow++
		return err
	}
	var firstErr error
	okN := 0
	failN := 0
	skipN := 0
	for wallet, rows := range groups {
		var sum uint64
		ids := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			sum += r.ProviderShare
			ids = append(ids, r.ID)
		}
		// #818: per-wallet affordability. Skip (don't fail/abort) any batch
		// that exceeds the remaining budget; the rest keep settling.
		if sum > budget {
			skipN++
			if c.Alerter != nil {
				c.Alerter(ctx, "skipped oversized wallet "+wallet)
			}
			continue
		}
		sig, err := c.Solana.TransferGRID(ctx, common.PublicKeyFromString(wallet), sum)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			c.St.mu.Lock()
			for _, id := range ids {
				c.St.failed[id] = err.Error()
			}
			c.St.mu.Unlock()
			failN += len(ids)
			continue
		}
		budget -= sum
		c.St.mu.Lock()
		for _, id := range ids {
			c.St.settled[id] = sig
		}
		c.St.mu.Unlock()
		okN += len(ids)
	}
	if c.Metrics != nil {
		if okN > 0 {
			c.Metrics.SettledOK(okN)
		}
		if failN > 0 {
			c.Metrics.SettledFailed(failN)
		}
	}
	_ = skipN // a skip is not a failure — it does not bump the counter
	if firstErr != nil {
		c.failuresInARow++
		if c.failuresInARow >= 3 && c.Alerter != nil {
			c.Alerter(ctx, firstErr.Error())
		}
		return firstErr
	}
	c.failuresInARow = 0
	return nil
}

func TestSettlementCron_BatchesByWallet(t *testing.T) {
	t.Parallel()
	st := newCronStore()
	wallet := "PrvW1"
	r1 := &Settlement{ID: uuid.New(), ProviderShare: 500_000}
	r2 := &Settlement{ID: uuid.New(), ProviderShare: 300_000}
	st.groups[wallet] = []*Settlement{r1, r2}

	xfer := &stubTransferer{balance: 10_000_000}
	metrics := &stubMetrics{}
	cron := &testCron{St: st, Solana: xfer, Metrics: metrics}

	if err := cron.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(xfer.calls) != 1 {
		t.Fatalf("expected 1 transfer, got %d", len(xfer.calls))
	}
	if xfer.calls[0].amount != 800_000 {
		t.Fatalf("batched sum: got %d want 800_000", xfer.calls[0].amount)
	}
	if _, ok := st.settled[r1.ID]; !ok {
		t.Fatal("r1 not settled")
	}
	if _, ok := st.settled[r2.ID]; !ok {
		t.Fatal("r2 not settled")
	}
	if metrics.ok != 2 {
		t.Fatalf("metrics.ok: %d", metrics.ok)
	}
}

// TestSettlementCron_InsufficientTreasury (post-#818): an unaffordable wallet
// is SKIPPED, not aborted. RunOnce returns nil (the tick made no error — it
// just couldn't afford this one wallet), the row stays unsettled for a future
// tick, and the consecutive-failure counter does NOT increment. This is the
// behaviour change that unwedges the dead-locked worker.
func TestSettlementCron_InsufficientTreasury(t *testing.T) {
	t.Parallel()
	st := newCronStore()
	r := &Settlement{ID: uuid.New(), ProviderShare: 1_000_000}
	st.groups["Pw"] = []*Settlement{r}
	xfer := &stubTransferer{balance: 100} // not enough for this wallet
	cron := &testCron{St: st, Solana: xfer, Metrics: &stubMetrics{}}
	if err := cron.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce should skip (not error) an unaffordable wallet: %v", err)
	}
	if _, ok := st.settled[r.ID]; ok {
		t.Fatal("unaffordable row should not be settled")
	}
	if cron.failuresInARow != 0 {
		t.Fatalf("a skip must not bump the failure counter, got %d", cron.failuresInARow)
	}
	if len(xfer.calls) != 0 {
		t.Fatalf("no transfer should be attempted for an unaffordable wallet, got %d", len(xfer.calls))
	}
}

// TestSettlementCron_SkipOversizedPaysRest (post-#818): with one unaffordable
// wallet plus an affordable one, the affordable one settles and the oversized
// one is skipped — forward progress is never blocked by a single oversized row.
func TestSettlementCron_SkipOversizedPaysRest(t *testing.T) {
	t.Parallel()
	st := newCronStore()
	affordable := &Settlement{ID: uuid.New(), ProviderShare: 1_000_000}
	oversized := &Settlement{ID: uuid.New(), ProviderShare: 50_000_000}
	st.groups["Affordable"] = []*Settlement{affordable}
	st.groups["Oversized"] = []*Settlement{oversized}
	xfer := &stubTransferer{balance: 5_000_000} // covers affordable, not oversized
	cron := &testCron{St: st, Solana: xfer, Metrics: &stubMetrics{}}
	if err := cron.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if _, ok := st.settled[affordable.ID]; !ok {
		t.Fatal("affordable row must settle even when another wallet is oversized")
	}
	if _, ok := st.settled[oversized.ID]; ok {
		t.Fatal("oversized row must be skipped, not settled")
	}
	if cron.failuresInARow != 0 {
		t.Fatalf("a partial tick with skips is not a failure, got %d", cron.failuresInARow)
	}
}

func TestSettlementCron_AlertsAfter3Failures(t *testing.T) {
	t.Parallel()
	st := newCronStore()
	r := &Settlement{ID: uuid.New(), ProviderShare: 1_000_000}
	st.groups["Pw"] = []*Settlement{r}
	xfer := &stubTransferer{balance: 100_000_000, transferErr: errors.New("rpc dead")}
	alertCalls := 0
	cron := &testCron{
		St:     st,
		Solana: xfer,
		Metrics: &stubMetrics{},
		Alerter: func(ctx context.Context, body string) {
			alertCalls++
			_ = body
		},
	}
	// First failure — no alert yet.
	_ = cron.RunOnce(context.Background())
	if alertCalls != 0 {
		t.Fatalf("alert fired too early: %d", alertCalls)
	}
	// Need to reset the failed-row marker so the second/third tick will
	// actually see it as unsettled again — clear failed map but row is
	// still in groups and not in settled.
	st.failed = map[uuid.UUID]string{}
	// Second failure — still no alert.
	_ = cron.RunOnce(context.Background())
	st.failed = map[uuid.UUID]string{}
	if alertCalls != 0 {
		t.Fatalf("alert fired on 2nd: %d", alertCalls)
	}
	// Third failure — alert fires.
	_ = cron.RunOnce(context.Background())
	if alertCalls < 1 {
		t.Fatalf("expected alert on 3rd failure, got %d", alertCalls)
	}
}

func TestSettlementCron_NoUnsettledIsNoop(t *testing.T) {
	t.Parallel()
	st := newCronStore()
	xfer := &stubTransferer{balance: 1_000_000_000}
	cron := &testCron{St: st, Solana: xfer, Metrics: &stubMetrics{}}
	if err := cron.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(xfer.calls) != 0 {
		t.Fatalf("no transfers expected, got %d", len(xfer.calls))
	}
}

// also exercise the real SettlementCron's error paths via a hand-rolled
// mini smoke (uses the real Postgres impl shape via the embedded
// SolanaTransferer interface — but with nil Store it should bail with a
// solid panic-or-error, which we don't actually want to invoke here).
// The PostgresStore path itself is covered by integration tests in CI's
// devnet pipeline (#598 DoD line: "Devnet integration test").

var _ = time.Now // anchor time import for future timestamp assertions
