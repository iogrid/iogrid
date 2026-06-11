//go:build integration

// Field-complete round-trip tests for the billing-svc MONEY tables (#726).
//
// The in-memory-green/Postgres-broken bug class (#709/#725/#732) is a write
// or read path that silently drops a struct field. billing-svc's store had
// ZERO tests — on the tables that move actual money. Each test here writes
// a struct with EVERY field populated (pointers included), reads it back
// through the public getter the handlers use, and asserts every field
// survives. A future column added to the struct but not the SQL fails here.
//
// Fixture pattern mirrors internal/grid/store_postgres_integration_test.go:
// DATABASE_URL (default localhost:55433/billing_svc_test), skip-if-
// unreachable, wipe + re-apply embedded migrations per run.
package store

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	// stdlib registers the "pgx" sql.Driver used by shareddb.MigrateUpFS.
	_ "github.com/jackc/pgx/v5/stdlib"

	shareddb "github.com/iogrid/iogrid/coordinator/shared/db"
)

const defaultMoneyDSN = "postgres://postgres:postgres@localhost:55433/billing_svc_test?sslmode=disable"

func moneyDSN() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultMoneyDSN
}

func newMoneyFixture(t *testing.T) (*Store, func()) {
	t.Helper()
	dsn := moneyDSN()

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

	wipeCtx, wipeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer wipeCancel()
	dropStmts := []string{
		`DROP TABLE IF EXISTS grid_build_settlement CASCADE`,
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
	if err := shareddb.MigrateUpFS(context.Background(), dsn, Migrations, "migrations"); err != nil {
		pool.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	return New(pool), func() { pool.Close() }
}

// assertFieldsEqual compares two structs of the same type field-by-field,
// using time.Equal semantics for time.Time / *time.Time (Postgres returns
// the same instant in a different location, which breaks DeepEqual).
func assertFieldsEqual(t *testing.T, want, got any) {
	t.Helper()
	wv, gv := reflect.ValueOf(want), reflect.ValueOf(got)
	if wv.Type() != gv.Type() {
		t.Fatalf("type mismatch: %v vs %v", wv.Type(), gv.Type())
	}
	for i := 0; i < wv.NumField(); i++ {
		name := wv.Type().Field(i).Name
		wf, gf := wv.Field(i).Interface(), gv.Field(i).Interface()
		switch w := wf.(type) {
		case time.Time:
			g := gf.(time.Time)
			if !w.Equal(g) {
				t.Errorf("field %s: want %v, got %v", name, w, g)
			}
		case *time.Time:
			g := gf.(*time.Time)
			switch {
			case w == nil && g == nil:
			case w == nil || g == nil:
				t.Errorf("field %s: want %v, got %v", name, w, g)
			case !w.Equal(*g):
				t.Errorf("field %s: want %v, got %v", name, *w, *g)
			}
		default:
			if !reflect.DeepEqual(wf, gf) {
				t.Errorf("field %s: want %#v, got %#v", name, wf, gf)
			}
		}
	}
}

func ptr[T any](v T) *T { return &v }

func microNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func TestMoneyRoundTrip_Subscription(t *testing.T) {
	st, cleanup := newMoneyFixture(t)
	defer cleanup()
	ctx := context.Background()

	now := microNow()
	want := Subscription{
		ID:                   uuid.New(),
		WorkspaceID:          uuid.New(),
		Tier:                 "pro",
		Status:               "active",
		StripeCustomerID:     "cus_rt1",
		StripeSubscriptionID: "sub_rt1",
		CurrentPeriodStart:   ptr(now.Add(-24 * time.Hour)),
		CurrentPeriodEnd:     ptr(now.Add(24 * time.Hour)),
		CreatedAt:            now,
		UpdatedAt:            now,
		CanceledAt:           ptr(now.Add(time.Hour)),
	}
	if err := st.UpsertSubscription(ctx, want); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	got, err := st.GetSubscriptionByWorkspace(ctx, want.WorkspaceID)
	if err != nil {
		t.Fatalf("GetSubscriptionByWorkspace: %v", err)
	}
	// updated_at is stamped at write time by the store (upsert semantics) —
	// assert it survived as a sane timestamp, then compare the rest exactly.
	if got.UpdatedAt.Before(want.UpdatedAt) || got.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt not stamped sanely: %v", got.UpdatedAt)
	}
	got.UpdatedAt = want.UpdatedAt
	assertFieldsEqual(t, want, *got)
}

func TestMoneyRoundTrip_Invoice(t *testing.T) {
	st, cleanup := newMoneyFixture(t)
	defer cleanup()
	ctx := context.Background()

	now := microNow()
	want := Invoice{
		ID:               uuid.New(),
		WorkspaceID:      uuid.New(),
		StripeInvoiceID:  "in_rt1",
		PeriodStart:      now.Add(-30 * 24 * time.Hour),
		PeriodEnd:        now,
		SubtotalCents:    12345,
		TaxCents:         678,
		TotalCents:       13023,
		Currency:         "usd",
		Status:           "paid",
		HostedInvoiceURL: ptr("https://invoice.example/rt1"),
		IssuedAt:         ptr(now.Add(-time.Hour)),
		PaidAt:           ptr(now),
	}
	if err := st.UpsertInvoice(ctx, want); err != nil {
		t.Fatalf("UpsertInvoice: %v", err)
	}
	list, err := st.ListInvoicesByWorkspace(ctx, want.WorkspaceID, 10, 0)
	if err != nil {
		t.Fatalf("ListInvoicesByWorkspace: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 invoice, got %d", len(list))
	}
	assertFieldsEqual(t, want, list[0])
}

func TestMoneyRoundTrip_PayoutAccount(t *testing.T) {
	st, cleanup := newMoneyFixture(t)
	defer cleanup()
	ctx := context.Background()

	now := microNow()
	want := PayoutAccount{
		ID:                     uuid.New(),
		UserID:                 uuid.New(),
		StripeConnectAccountID: "acct_rt1",
		Status:                 "active",
		CountryCode:            ptr("DE"),
		DefaultCurrency:        ptr("eur"),
		OnboardedAt:            ptr(now.Add(-48 * time.Hour)),
		LastPayoutAt:           ptr(now.Add(-time.Hour)),
		CreatedAt:              now,
	}
	if err := st.UpsertPayoutAccount(ctx, want); err != nil {
		t.Fatalf("UpsertPayoutAccount: %v", err)
	}
	got, err := st.GetPayoutAccountByUser(ctx, want.UserID)
	if err != nil {
		t.Fatalf("GetPayoutAccountByUser: %v", err)
	}
	assertFieldsEqual(t, want, *got)
}

func TestMoneyRoundTrip_Payout(t *testing.T) {
	st, cleanup := newMoneyFixture(t)
	defer cleanup()
	ctx := context.Background()

	now := microNow()
	acct := PayoutAccount{
		ID:                     uuid.New(),
		UserID:                 uuid.New(),
		StripeConnectAccountID: "acct_rt2",
		Status:                 "active",
		CreatedAt:              now,
	}
	if err := st.UpsertPayoutAccount(ctx, acct); err != nil {
		t.Fatalf("UpsertPayoutAccount: %v", err)
	}
	want := Payout{
		ID:              uuid.New(),
		UserID:          acct.UserID,
		PayoutAccountID: acct.ID,
		AmountCents:     50000,
		Currency:        "usd",
		Status:          "paid",
		StripePayoutID:  ptr("po_rt1"),
		PeriodStart:     now.Add(-7 * 24 * time.Hour),
		PeriodEnd:       now,
		InitiatedAt:     now,
		SettledAt:       ptr(now.Add(time.Hour)),
		FailureReason:   ptr("none"),
	}
	if err := st.InsertPayout(ctx, want); err != nil {
		t.Fatalf("InsertPayout: %v", err)
	}
	list, err := st.ListPayoutsByUser(ctx, want.UserID,
		now.Add(-30*24*time.Hour), now.Add(30*24*time.Hour), 10, 0)
	if err != nil {
		t.Fatalf("ListPayoutsByUser: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 payout, got %d", len(list))
	}
	assertFieldsEqual(t, want, list[0])
}

func TestMoneyRoundTrip_ApiKey(t *testing.T) {
	st, cleanup := newMoneyFixture(t)
	defer cleanup()
	ctx := context.Background()

	now := microNow()
	want := ApiKey{
		ID:                uuid.New(),
		WorkspaceID:       uuid.New(),
		Label:             "round-trip",
		KeyHash:           "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		LastFour:          "4444",
		Tier:              "pro",
		AllowedCategories: "proxy,compute",
		GeoTarget:         "eu",
		KYCVerified:       true,
		CreatedAt:         now,
		LastUsedAt:        ptr(now.Add(-time.Minute)),
		RevokedAt:         ptr(now.Add(time.Hour)),
	}
	if err := st.InsertApiKey(ctx, want); err != nil {
		t.Fatalf("InsertApiKey: %v", err)
	}
	got, err := st.GetApiKey(ctx, want.ID)
	if err != nil {
		t.Fatalf("GetApiKey: %v", err)
	}
	assertFieldsEqual(t, want, *got)
}

func TestMoneyRoundTrip_OffRampRequest(t *testing.T) {
	st, cleanup := newMoneyFixture(t)
	defer cleanup()
	ctx := context.Background()

	now := microNow()
	want := OffRampRequest{
		ID:            uuid.New(),
		UserID:        uuid.New(),
		ProviderName:  "transak",
		ProviderRefID: ptr("ref-rt1"),
		WalletAddress: "7g1sQWalletRt1",
		GridAmount:    1_000_000,
		FiatAmount:    ptr("42.50"),
		FiatCurrency:  "EUR",
		Status:        "completed",
		RedirectURL:   "https://offramp.example/rt1",
		ReturnURL:     ptr("https://iogrid.org/account"),
		TxnSignature:  ptr("5sigRt1"),
		ErrorMessage:  ptr(""),
		CreatedAt:     now,
		UpdatedAt:     now,
		CompletedAt:   ptr(now.Add(time.Minute)),
	}
	if err := st.InsertOffRampRequest(ctx, want); err != nil {
		t.Fatalf("InsertOffRampRequest: %v", err)
	}
	got, err := st.GetOffRampRequest(ctx, want.ID)
	if err != nil {
		t.Fatalf("GetOffRampRequest: %v", err)
	}
	assertFieldsEqual(t, want, *got)
}
