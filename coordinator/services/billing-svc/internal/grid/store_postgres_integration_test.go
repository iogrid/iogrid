//go:build integration

// Postgres integration tests for the Track 5 grid_settlement store
// methods: InsertSettlement, GetSettlementBySession, ListUnsettledByWallet,
// MarkSettled, MarkAttemptFailed (+ idempotency via the session_id
// UNIQUE constraint).
//
// The unit suite in session_meter_test.go + settlement_cron_test.go
// covers the pure arithmetic and the cron orchestration against
// in-memory fakes. This file pins the Postgres path on a real database
// so we catch SQL-level drift (column names, ON CONFLICT semantics,
// `=ANY($1::uuid[])` array-cast behaviour, NULL handling on
// `provider_wallet` + `settled_at`) that the in-memory mirror can't
// surface — exactly the gap PR #608 just closed for vpn-svc's
// AllocateInnerIP + PersistSessionPeerConfig.
//
// Run with:
//
//	docker run --rm -d -p 55433:5432 \
//	    -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=billing_svc_test \
//	    postgres:16
//	DATABASE_URL=postgres://postgres:postgres@localhost:55433/billing_svc_test?sslmode=disable \
//	    go test -tags=integration ./internal/grid/...
//
// Port 55433 deliberately avoids clashing with #608's vpn-svc fixture
// on 55432 so both suites can co-tenant on a single dev box.
//
// If DATABASE_URL is unset and the default localhost:55433 DSN is
// unreachable the tests skip cleanly — same pattern as
// inner_ip_postgres_integration_test.go (#608) and
// coordinator/services/antiabuse-svc/internal/audit/retention_integration_test.go.
//
// Refs #597, #598.

package grid

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	// stdlib registers the "pgx" sql.Driver name that the shared
	// db.MigrateUpFS helper (via goose) uses to open its migration
	// sql.DB handle.
	_ "github.com/jackc/pgx/v5/stdlib"

	shareddb "github.com/iogrid/iogrid/coordinator/shared/db"
	billingstore "github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

const defaultPostgresDSN = "postgres://postgres:postgres@localhost:55433/billing_svc_test?sslmode=disable"

func postgresDSN() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultPostgresDSN
}

// newPostgresFixture brings up a pgxpool against the configured DATABASE_URL,
// drops every table the billing-svc migrations create (plus goose's marker
// table) and re-applies the embedded migrations so each test starts from
// a clean baseline. Skips cleanly if the database is unreachable (CI
// without docker, dev box without a local Postgres, etc.).
func newPostgresFixture(t *testing.T) (*pgxpool.Pool, *PostgresStore, func()) {
	t.Helper()
	dsn := postgresDSN()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("postgres pool create failed (DATABASE_URL=%q): %v", dsn, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres ping failed (DATABASE_URL=%q): %v", dsn, err)
	}

	// Wipe + re-migrate. We drop the goose marker table too so
	// MigrateUpFS re-runs from scratch every test run — these tests are
	// destructive by design, so DATABASE_URL must point at a throwaway
	// database.
	wipeCtx, wipeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer wipeCancel()
	dropStmts := []string{
		`DROP TABLE IF EXISTS grid_devnet_faucet_log CASCADE`,
		`DROP TABLE IF EXISTS grid_settlement CASCADE`,
		`DROP TABLE IF EXISTS offramp_request CASCADE`,
		`DROP TABLE IF EXISTS api_key CASCADE`,
		`DROP TABLE IF EXISTS payout_methods CASCADE`,
		`DROP TABLE IF EXISTS tax_report CASCADE`,
		`DROP TABLE IF EXISTS solana_burn CASCADE`,
		`DROP TABLE IF EXISTS solana_payout CASCADE`,
		`DROP TABLE IF EXISTS usage_aggregate_daily CASCADE`,
		`DROP TABLE IF EXISTS usage_event CASCADE`,
		`DROP TABLE IF EXISTS payout CASCADE`,
		`DROP TABLE IF EXISTS payout_account CASCADE`,
		`DROP TABLE IF EXISTS invoice CASCADE`,
		`DROP TABLE IF EXISTS subscription CASCADE`,
		`DROP TABLE IF EXISTS goose_db_version CASCADE`,
	}
	for _, s := range dropStmts {
		if _, err := pool.Exec(wipeCtx, s); err != nil {
			pool.Close()
			t.Fatalf("wipe (%s): %v", s, err)
		}
	}

	if err := shareddb.MigrateUpFS(context.Background(), dsn, billingstore.Migrations, "migrations"); err != nil {
		pool.Close()
		t.Fatalf("apply migrations: %v", err)
	}

	store := NewPostgresStore(pool)
	cleanup := func() { pool.Close() }
	return pool, store, cleanup
}

// newSettlement returns a Settlement seeded with deterministic non-zero
// values and the 85/15 split applied via ComputeShares. Tests can override
// fields before calling InsertSettlement.
func newSettlement(consumed, escrowed uint64) *Settlement {
	provShare, iogridShare := ComputeShares(consumed)
	refund := ComputeRefund(escrowed, consumed)
	return &Settlement{
		ID:             uuid.New(),
		SessionID:      uuid.New(),
		CustomerWallet: "CustWalletAddrBase58XXXXXXXXXXXXXXXXXXXXXXX",
		ProviderWallet: "PrvWalletAddrBase58XXXXXXXXXXXXXXXXXXXXXXXX",
		ProviderID:     uuid.New(),
		BytesIn:        500_000_000,
		BytesOut:       500_000_000,
		EscrowedAtomic: escrowed,
		ConsumedAtomic: consumed,
		RefundAtomic:   refund,
		ProviderShare:  provShare,
		IogridShare:    iogridShare,
		CreatedAt:      time.Now().UTC(),
	}
}

// TestPostgresInsertSettlement_WritesAllSplitFields verifies that
// InsertSettlement persists every column and that the 85/15 split round-trips
// at the SQL level (provider_share + iogrid_share == consumed_atomic).
func TestPostgresInsertSettlement_WritesAllSplitFields(t *testing.T) {
	_, store, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	// Consumed = 10 GB worth (10_000_000 atomic = 0.01 GRID).
	// Expected provider_share = 8_500_000, iogrid_share = 1_500_000.
	const consumed uint64 = 10_000_000
	const escrowed uint64 = 12_000_000
	s := newSettlement(consumed, escrowed)

	if err := store.InsertSettlement(ctx, s); err != nil {
		t.Fatalf("InsertSettlement: %v", err)
	}

	// Round-trip via the public reader.
	got, err := store.GetSettlementBySession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSettlementBySession: %v", err)
	}

	if got.ConsumedAtomic != consumed {
		t.Errorf("consumed_atomic = %d, want %d", got.ConsumedAtomic, consumed)
	}
	if got.EscrowedAtomic != escrowed {
		t.Errorf("escrowed_atomic = %d, want %d", got.EscrowedAtomic, escrowed)
	}
	if got.RefundAtomic != escrowed-consumed {
		t.Errorf("refund_atomic = %d, want %d", got.RefundAtomic, escrowed-consumed)
	}
	if got.ProviderShare != 8_500_000 {
		t.Errorf("provider_share = %d, want 8_500_000 (85%%)", got.ProviderShare)
	}
	if got.IogridShare != 1_500_000 {
		t.Errorf("iogrid_share = %d, want 1_500_000 (15%%)", got.IogridShare)
	}
	// Sum-must-equal-consumed invariant — same property the cron relies
	// on when batching across rows.
	if got.ProviderShare+got.IogridShare != got.ConsumedAtomic {
		t.Errorf("share split must sum to consumed: %d + %d != %d",
			got.ProviderShare, got.IogridShare, got.ConsumedAtomic)
	}
	if got.CustomerWallet != s.CustomerWallet {
		t.Errorf("customer_wallet = %q, want %q", got.CustomerWallet, s.CustomerWallet)
	}
	if got.ProviderWallet != s.ProviderWallet {
		t.Errorf("provider_wallet = %q, want %q", got.ProviderWallet, s.ProviderWallet)
	}
	if got.SettledAt != nil {
		t.Errorf("freshly-inserted row must have NULL settled_at, got %v", got.SettledAt)
	}
	if got.TxSignature != "" {
		t.Errorf("freshly-inserted row must have empty tx_signature, got %q", got.TxSignature)
	}
}

// TestPostgresInsertSettlement_SplitMathAtGBBoundary verifies the 85/15
// split for a single 1GB session (1_000_000 atomic, the MinSettlementAtomic
// threshold). Catches integer-rounding bugs at the floor of the settlement
// queue.
func TestPostgresInsertSettlement_SplitMathAtGBBoundary(t *testing.T) {
	_, store, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	// Exactly 1 GB worth.
	s := newSettlement(MinSettlementAtomic, MinSettlementAtomic)
	if err := store.InsertSettlement(ctx, s); err != nil {
		t.Fatalf("InsertSettlement: %v", err)
	}

	got, err := store.GetSettlementBySession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSettlementBySession: %v", err)
	}
	// 1_000_000 * 85 / 100 = 850_000 ; 1_000_000 - 850_000 = 150_000.
	if got.ProviderShare != 850_000 {
		t.Errorf("provider_share = %d, want 850_000", got.ProviderShare)
	}
	if got.IogridShare != 150_000 {
		t.Errorf("iogrid_share = %d, want 150_000", got.IogridShare)
	}
	if got.RefundAtomic != 0 {
		t.Errorf("refund_atomic = %d, want 0 (escrowed == consumed)", got.RefundAtomic)
	}
}

// TestPostgresListUnsettledByWallet_ReturnsInsertedSkipsSettled exercises
// the worker's query path: a fresh row is visible; once MarkSettled stamps
// settled_at, the subsequent list call must exclude it. Also verifies the
// MinSettlementAtomic floor — dust rows below 1_000_000 atomic are filtered.
func TestPostgresListUnsettledByWallet_ReturnsInsertedSkipsSettled(t *testing.T) {
	_, store, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	// Two rows for the same wallet, one for a different wallet, plus
	// one dust row that must be filtered by the MinSettlementAtomic
	// floor in ListUnsettledByWallet.
	const wallet1 = "PrvWalletAlpha1111111111111111111111111111"
	const wallet2 = "PrvWalletBeta22222222222222222222222222222"

	a := newSettlement(10_000_000, 10_000_000)
	a.ProviderWallet = wallet1
	b := newSettlement(2_000_000, 2_000_000)
	b.ProviderWallet = wallet1
	c := newSettlement(5_000_000, 5_000_000)
	c.ProviderWallet = wallet2
	dust := newSettlement(100, 100) // provider_share=85 < MinSettlementAtomic
	dust.ProviderWallet = wallet1

	for _, s := range []*Settlement{a, b, c, dust} {
		if err := store.InsertSettlement(ctx, s); err != nil {
			t.Fatalf("seed %s: %v", s.SessionID, err)
		}
	}

	groups, err := store.ListUnsettledByWallet(ctx, 100)
	if err != nil {
		t.Fatalf("ListUnsettledByWallet: %v", err)
	}
	if len(groups[wallet1]) != 2 {
		t.Errorf("wallet1 group should have 2 rows (dust filtered), got %d", len(groups[wallet1]))
	}
	if len(groups[wallet2]) != 1 {
		t.Errorf("wallet2 group should have 1 row, got %d", len(groups[wallet2]))
	}
	// The dust row must NOT appear in either group.
	for w, rows := range groups {
		for _, r := range rows {
			if r.SessionID == dust.SessionID {
				t.Errorf("dust row leaked into group %q", w)
			}
		}
	}

	// Settle the wallet1 batch and re-list — they should disappear.
	ids := []uuid.UUID{a.ID, b.ID}
	if err := store.MarkSettled(ctx, ids, "TxSigForWallet1Batch11111111111111111"); err != nil {
		t.Fatalf("MarkSettled: %v", err)
	}
	groups2, err := store.ListUnsettledByWallet(ctx, 100)
	if err != nil {
		t.Fatalf("ListUnsettledByWallet after settle: %v", err)
	}
	if got := len(groups2[wallet1]); got != 0 {
		t.Errorf("wallet1 group must be empty after settle, got %d rows", got)
	}
	if got := len(groups2[wallet2]); got != 1 {
		t.Errorf("wallet2 group still expected 1 row, got %d", got)
	}
}

// TestPostgresMarkSettled_StampsTxSignatureAndSettledAt verifies the
// columns that MarkSettled writes. The cron uses these to expose
// audit-trail info to the providers UI ("Last paid on <date>, tx
// <sig>") and to gate the next tick.
func TestPostgresMarkSettled_StampsTxSignatureAndSettledAt(t *testing.T) {
	pool, store, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	s := newSettlement(10_000_000, 10_000_000)
	if err := store.InsertSettlement(ctx, s); err != nil {
		t.Fatalf("InsertSettlement: %v", err)
	}

	const txSig = "Sig1111111111111111111111111111111111111111Z"
	before := time.Now().UTC().Add(-1 * time.Second)
	if err := store.MarkSettled(ctx, []uuid.UUID{s.ID}, txSig); err != nil {
		t.Fatalf("MarkSettled: %v", err)
	}
	after := time.Now().UTC().Add(1 * time.Second)

	// Read directly so we can assert on the raw timestamp window.
	var (
		gotSig    string
		settledAt *time.Time
	)
	row := pool.QueryRow(ctx, `SELECT COALESCE(tx_signature,''), settled_at FROM grid_settlement WHERE id=$1`, s.ID)
	if err := row.Scan(&gotSig, &settledAt); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if gotSig != txSig {
		t.Errorf("tx_signature = %q, want %q", gotSig, txSig)
	}
	if settledAt == nil {
		t.Fatal("settled_at must not be NULL after MarkSettled")
	}
	if settledAt.Before(before) || settledAt.After(after) {
		t.Errorf("settled_at %v out of expected window [%v, %v]", *settledAt, before, after)
	}

	// And the public reader must surface the same view.
	got, err := store.GetSettlementBySession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSettlementBySession: %v", err)
	}
	if got.TxSignature != txSig {
		t.Errorf("public reader tx_signature = %q, want %q", got.TxSignature, txSig)
	}
	if got.SettledAt == nil {
		t.Fatal("public reader settled_at must not be NULL after MarkSettled")
	}
}

// TestPostgresMarkSettled_IdempotentOnSecondCall verifies the cron-safe
// property of MarkSettled: the `AND settled_at IS NULL` predicate
// preserves the FIRST tx_signature even if a later tick re-stamps the
// same row with a different sig.
func TestPostgresMarkSettled_IdempotentOnSecondCall(t *testing.T) {
	pool, store, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	s := newSettlement(10_000_000, 10_000_000)
	if err := store.InsertSettlement(ctx, s); err != nil {
		t.Fatalf("InsertSettlement: %v", err)
	}

	const firstSig = "FirstSig1111111111111111111111111111111111Z"
	const secondSig = "SecondSig222222222222222222222222222222222Z"
	if err := store.MarkSettled(ctx, []uuid.UUID{s.ID}, firstSig); err != nil {
		t.Fatalf("MarkSettled #1: %v", err)
	}
	if err := store.MarkSettled(ctx, []uuid.UUID{s.ID}, secondSig); err != nil {
		t.Fatalf("MarkSettled #2: %v", err)
	}

	var gotSig string
	if err := pool.QueryRow(ctx, `SELECT tx_signature FROM grid_settlement WHERE id=$1`, s.ID).Scan(&gotSig); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if gotSig != firstSig {
		t.Errorf("tx_signature must remain %q after re-settle, got %q", firstSig, gotSig)
	}
}

// TestPostgresInsertSettlement_IdempotentOnSessionID verifies that
// re-inserting with the same session_id is silently absorbed by the
// `ON CONFLICT (session_id) DO NOTHING` clause — NOT a duplicate-key
// error and NOT a silent overwrite. The original row's columns must
// survive intact so the worker doesn't lose audit data.
//
// This is the exact-by-design path SessionMeter.Settle relies on
// (idempotent re-call from a retried POST /v1/grid/session-end).
func TestPostgresInsertSettlement_IdempotentOnSessionID(t *testing.T) {
	pool, store, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	original := newSettlement(10_000_000, 10_000_000)
	if err := store.InsertSettlement(ctx, original); err != nil {
		t.Fatalf("InsertSettlement original: %v", err)
	}

	// Replay with the same session_id but different shares. Must NOT
	// surface a unique-violation error AND must NOT overwrite the
	// original row.
	replay := &Settlement{
		ID:             uuid.New(), // fresh PK so the DO NOTHING is the only collision
		SessionID:      original.SessionID,
		CustomerWallet: "DifferentCustomerWallet0000000000000000000",
		ProviderWallet: "DifferentProviderWallet0000000000000000000",
		ProviderID:     uuid.New(),
		BytesIn:        1,
		BytesOut:       1,
		EscrowedAtomic: 1,
		ConsumedAtomic: 1,
		RefundAtomic:   0,
		ProviderShare:  0,
		IogridShare:    1,
		CreatedAt:      time.Now().UTC(),
	}
	if err := store.InsertSettlement(ctx, replay); err != nil {
		t.Fatalf("InsertSettlement replay should be silently absorbed, got: %v", err)
	}

	// Count must still be 1.
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM grid_settlement WHERE session_id=$1`,
		original.SessionID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected exactly 1 row per session_id, got %d", n)
	}

	// And the surviving row must be the ORIGINAL — not the replay.
	got, err := store.GetSettlementBySession(ctx, original.SessionID)
	if err != nil {
		t.Fatalf("GetSettlementBySession: %v", err)
	}
	if got.CustomerWallet != original.CustomerWallet {
		t.Errorf("DO NOTHING leaked into overwrite: customer_wallet = %q, want %q",
			got.CustomerWallet, original.CustomerWallet)
	}
	if got.ConsumedAtomic != original.ConsumedAtomic {
		t.Errorf("DO NOTHING leaked into overwrite: consumed_atomic = %d, want %d",
			got.ConsumedAtomic, original.ConsumedAtomic)
	}
	if got.ProviderShare != original.ProviderShare {
		t.Errorf("DO NOTHING leaked into overwrite: provider_share = %d, want %d",
			got.ProviderShare, original.ProviderShare)
	}
}

// TestPostgresInsertSettlement_RejectsNegativeShares verifies the
// grid_settlement_atomic_nonneg CHECK constraint in 0006 rejects
// pathological inputs at the SQL level. We can't easily craft a negative
// uint64 via the Go API, so we INSERT directly via raw SQL using a
// negative literal and assert the constraint fires.
func TestPostgresInsertSettlement_RejectsNegativeShares(t *testing.T) {
	pool, _, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	id := uuid.New()
	sessID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO grid_settlement (
			id, session_id, customer_wallet, provider_wallet, provider_id,
			bytes_in, bytes_out,
			escrowed_atomic, consumed_atomic, refund_atomic,
			provider_share, iogrid_share,
			created_at, settled_at, tx_signature, settle_attempts, last_error
		) VALUES ($1, $2, 'CW', NULL, NULL,
			0, 0,
			1000, 1000, 0,
			-1, 1001,   -- provider_share negative — must violate CHECK
			NOW(), NULL, NULL, 0, NULL)`,
		id, sessID)
	if err == nil {
		t.Fatal("expected CHECK constraint violation for negative provider_share, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "atomic_nonneg") &&
		!strings.Contains(strings.ToLower(err.Error()), "check") {
		t.Errorf("error should mention the atomic_nonneg CHECK constraint, got: %v", err)
	}
}

// TestPostgresMarkAttemptFailed_BumpsCounterAndStampsError verifies the
// failure-bookkeeping path the cron uses after a failed Solana submit.
func TestPostgresMarkAttemptFailed_BumpsCounterAndStampsError(t *testing.T) {
	_, store, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	s := newSettlement(10_000_000, 10_000_000)
	if err := store.InsertSettlement(ctx, s); err != nil {
		t.Fatalf("InsertSettlement: %v", err)
	}

	if err := store.MarkAttemptFailed(ctx, []uuid.UUID{s.ID}, "rpc node unreachable"); err != nil {
		t.Fatalf("MarkAttemptFailed #1: %v", err)
	}
	if err := store.MarkAttemptFailed(ctx, []uuid.UUID{s.ID}, "tx simulation failed"); err != nil {
		t.Fatalf("MarkAttemptFailed #2: %v", err)
	}

	got, err := store.GetSettlementBySession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSettlementBySession: %v", err)
	}
	if got.SettleAttempts != 2 {
		t.Errorf("settle_attempts = %d, want 2", got.SettleAttempts)
	}
	if got.LastError != "tx simulation failed" {
		t.Errorf("last_error = %q, want %q", got.LastError, "tx simulation failed")
	}
	if got.SettledAt != nil {
		t.Error("settled_at must remain NULL after MarkAttemptFailed")
	}
}

// TestPostgresGetSettlementBySession_NotFound returns the package's
// ErrNotFound — the handler relies on this to surface a NotFound to
// idempotent retry callers vs. wrapping the underlying pgx error.
func TestPostgresGetSettlementBySession_NotFound(t *testing.T) {
	_, store, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	_, err := store.GetSettlementBySession(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected ErrNotFound for unknown session_id, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
	// Defensive: must not leak pgx.ErrNoRows.
	if errors.Is(err, pgx.ErrNoRows) {
		t.Error("public reader must wrap pgx.ErrNoRows as ErrNotFound, not leak it")
	}
}

// TestPostgresInsertSettlement_HandlesNullableProviderWallet verifies
// the schema's `provider_wallet VARCHAR(64)` nullable column behaves
// correctly when the meter writes an empty wallet (vpn-svc may not have
// the binding yet at session-end time — the worker re-resolves before
// submit).
func TestPostgresInsertSettlement_HandlesNullableProviderWallet(t *testing.T) {
	pool, store, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	s := newSettlement(10_000_000, 10_000_000)
	s.ProviderWallet = "" // empty → must land as NULL in Postgres
	if err := store.InsertSettlement(ctx, s); err != nil {
		t.Fatalf("InsertSettlement: %v", err)
	}

	// Raw read confirms the NULL.
	var pw *string
	if err := pool.QueryRow(ctx, `SELECT provider_wallet FROM grid_settlement WHERE id=$1`, s.ID).Scan(&pw); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if pw != nil {
		t.Errorf("provider_wallet should be NULL when empty string passed, got %q", *pw)
	}

	// And the row must NOT appear in the worker's batch — the
	// `provider_wallet IS NOT NULL` predicate excludes it until the
	// late-bind path stamps a real wallet.
	groups, err := store.ListUnsettledByWallet(ctx, 100)
	if err != nil {
		t.Fatalf("ListUnsettledByWallet: %v", err)
	}
	for w, rows := range groups {
		for _, r := range rows {
			if r.SessionID == s.SessionID {
				t.Errorf("row with NULL provider_wallet leaked into batch group %q", w)
			}
		}
	}

	// Public reader still returns the row (audit path), with empty wallet.
	got, err := store.GetSettlementBySession(ctx, s.SessionID)
	if err != nil {
		t.Fatalf("GetSettlementBySession: %v", err)
	}
	if got.ProviderWallet != "" {
		t.Errorf("provider_wallet round-trip should be empty, got %q", got.ProviderWallet)
	}
}

// newBuildSettlement returns a BuildSettlement seeded with deterministic
// non-zero values + the 85/15 split applied. Mirrors newSettlement for the
// iOS-build ledger (grid_build_settlement). Tests override fields (notably
// ProviderID) before InsertBuildSettlement. See #758.
func newBuildSettlement(consumed, escrowed uint64) *BuildSettlement {
	provShare, iogridShare := ComputeShares(consumed)
	refund := ComputeRefund(escrowed, consumed)
	return &BuildSettlement{
		ID:             uuid.New(),
		BuildID:        uuid.New(),
		AttemptID:      uuid.New(),
		CustomerWallet: "CustWalletAddrBase58XXXXXXXXXXXXXXXXXXXXXXX",
		ProviderWallet: "PrvWalletAddrBase58XXXXXXXXXXXXXXXXXXXXXXXX",
		ProviderID:     uuid.New(),
		EscrowedAtomic: escrowed,
		ConsumedAtomic: consumed,
		RefundAtomic:   refund,
		ProviderShare:  provShare,
		IogridShare:    iogridShare,
	}
}

// TestSumProviderEarnings_FoldsSettledOnChainGrid is the #758 regression:
// before the fix, SumProviderEarnings summed only usage_event.cost_cents, so
// a provider whose entire earnings were real on-chain build settlements (the
// prod reality — Hatice's Mac earned 4.675 $GRID across 4 settled builds while
// usage_event.cost_cents summed to ~0) saw "0 $GRID" on the dashboard. This
// test inserts settled build-settlement rows for a provider with ZERO
// usage_event rows and asserts the on-chain provider_share surfaces as
// non-zero TotalEarned (in micros), with the settled-build count + the
// SettledGrid micros populated and currency flipped to GRID.
func TestSumProviderEarnings_FoldsSettledOnChainGrid(t *testing.T) {
	pool, gridStore, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	providerID := uuid.New()
	// Two settled builds: 0.85 GRID + 2.55 GRID provider_share (consumed
	// 1.0 + 3.0 GRID atomic). Settled NOW so they land in the 30d/7d windows.
	b1 := newBuildSettlement(1_000_000_000, 1_200_000_000) // provShare 850_000_000
	b1.ProviderID = providerID
	b2 := newBuildSettlement(3_000_000_000, 3_000_000_000) // provShare 2_550_000_000
	b2.ProviderID = providerID
	for _, b := range []*BuildSettlement{b1, b2} {
		if err := gridStore.InsertBuildSettlement(ctx, b); err != nil {
			t.Fatalf("InsertBuildSettlement: %v", err)
		}
	}
	// Mark both settled (stamps settled_at) — only settled rows count.
	if err := gridStore.MarkBuildSettled(ctx, []uuid.UUID{b1.ID, b2.ID}, "TestTxSigBase58XXXXXXXXXXXXXXXXXXXXXXXXXXXXX"); err != nil {
		t.Fatalf("MarkBuildSettled: %v", err)
	}

	// A DIFFERENT provider's settled build must NOT leak into our totals.
	other := newBuildSettlement(5_000_000_000, 5_000_000_000)
	if err := gridStore.InsertBuildSettlement(ctx, other); err != nil {
		t.Fatalf("InsertBuildSettlement(other): %v", err)
	}
	if err := gridStore.MarkBuildSettled(ctx, []uuid.UUID{other.ID}, "OtherSigXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"); err != nil {
		t.Fatalf("MarkBuildSettled(other): %v", err)
	}

	// SumProviderEarnings lives in the store package; it shares this pool.
	bs := billingstore.New(pool)
	tot, err := bs.SumProviderEarnings(ctx, providerID, time.Now().UTC())
	if err != nil {
		t.Fatalf("SumProviderEarnings: %v", err)
	}

	// provider_share atomic = 850_000_000 + 2_550_000_000 = 3_400_000_000.
	// micros = atomic / 1000 = 3_400_000.
	const wantMicros int64 = 3_400_000
	if tot.SettledGridMicros != wantMicros {
		t.Errorf("SettledGridMicros = %d, want %d", tot.SettledGridMicros, wantMicros)
	}
	if tot.LifetimeMicros != wantMicros {
		t.Errorf("LifetimeMicros = %d, want %d (no usage_event rows, so == settled)", tot.LifetimeMicros, wantMicros)
	}
	if tot.PendingPayoutMicros != wantMicros {
		t.Errorf("PendingPayoutMicros = %d, want %d", tot.PendingPayoutMicros, wantMicros)
	}
	if tot.Last30DMicros != wantMicros {
		t.Errorf("Last30DMicros = %d, want %d (settled now)", tot.Last30DMicros, wantMicros)
	}
	if tot.SettledBuilds != 2 {
		t.Errorf("SettledBuilds = %d, want 2", tot.SettledBuilds)
	}
	if tot.LifetimeWorkloads != 2 {
		t.Errorf("LifetimeWorkloads = %d, want 2 (each settled build is a workload)", tot.LifetimeWorkloads)
	}
	if tot.Currency != "GRID" {
		t.Errorf("Currency = %q, want GRID (real on-chain $GRID present)", tot.Currency)
	}
}

// TestSumProviderEarnings_OnlyCountsSettled asserts an UNSETTLED build
// settlement (settled_at IS NULL — queued but not yet on-chain) does NOT
// inflate the headline. The dashboard figure must be "confirmed on-chain",
// not "pending". See #758.
func TestSumProviderEarnings_OnlyCountsSettled(t *testing.T) {
	pool, gridStore, cleanup := newPostgresFixture(t)
	defer cleanup()
	ctx := context.Background()

	providerID := uuid.New()
	b := newBuildSettlement(2_000_000_000, 2_000_000_000)
	b.ProviderID = providerID
	if err := gridStore.InsertBuildSettlement(ctx, b); err != nil {
		t.Fatalf("InsertBuildSettlement: %v", err)
	}
	// Deliberately NOT marked settled.

	bs := billingstore.New(pool)
	tot, err := bs.SumProviderEarnings(ctx, providerID, time.Now().UTC())
	if err != nil {
		t.Fatalf("SumProviderEarnings: %v", err)
	}
	if tot.SettledGridMicros != 0 {
		t.Errorf("SettledGridMicros = %d, want 0 (row not settled)", tot.SettledGridMicros)
	}
	if tot.SettledBuilds != 0 {
		t.Errorf("SettledBuilds = %d, want 0 (row not settled)", tot.SettledBuilds)
	}
	if tot.LifetimeMicros != 0 {
		t.Errorf("LifetimeMicros = %d, want 0", tot.LifetimeMicros)
	}
}
