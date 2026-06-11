//go:build integration

// Field-complete round-trip test for customer_wallet_bindings (#726).
//
// This is the wallet $GRID settlement pays to (build-gateway resolves it via
// identity-svc, #720/#723), so a silently-dropped field here is a money bug.
// The #726 audit verified the store CLEAN by inspection; this locks it in
// against the in-memory-green/Postgres-broken class going forward: a future
// column added to CustomerWalletBinding but not the SQL fails the suite.
//
// Fixture mirrors billing-svc/internal/store's money_roundtrip test:
// DATABASE_URL (default a local throwaway Postgres), skip-if-unreachable,
// wipe + re-apply the embedded identity migrations per run.
package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	idb "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/db"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

const defaultWalletDSN = "postgres://postgres:postgres@localhost:55434/identity_test?sslmode=disable"

func walletDSN() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultWalletDSN
}

func newWalletFixture(t *testing.T) (*store.Store, *pgxpool.Pool, func()) {
	t.Helper()
	dsn := walletDSN()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("pg pool create failed (DATABASE_URL=%q): %v", dsn, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("pg ping failed (DATABASE_URL=%q): %v", dsn, err)
	}
	wipeCtx, wipeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer wipeCancel()
	// Drop everything the identity migrations create so Apply re-runs clean.
	if _, err := pool.Exec(wipeCtx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		pool.Close()
		t.Fatalf("wipe schema: %v", err)
	}
	if err := idb.Apply(context.Background(), dsn); err != nil {
		pool.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	return store.New(pool), pool, func() { pool.Close() }
}

func TestCustomerWalletBinding_RoundTrip(t *testing.T) {
	st, _, cleanup := newWalletFixture(t)
	defer cleanup()
	ctx := context.Background()

	// Seed the FK user (customer_wallet_bindings.user_id REFERENCES users).
	u := &store.User{
		ID:           uuid.New(),
		PrimaryEmail: "wallet-rt@example.com",
		DisplayName:  "RT",
		Roles:        []string{"customer"},
	}
	if err := st.CreateUser(ctx, nil, u); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	bind := &store.CustomerWalletBinding{
		UserID:         u.ID,
		WalletAddress:  "7gWxQ3iogridDevnetWalletXXXXXXXXXXXXXXXXXXXX",
		WalletProvider: store.WalletProviderPhantom,
	}
	if err := st.UpsertCustomerWalletBinding(ctx, nil, bind); err != nil {
		t.Fatalf("UpsertCustomerWalletBinding: %v", err)
	}

	got, err := st.GetCustomerWalletBinding(ctx, nil, u.ID)
	if err != nil {
		t.Fatalf("GetCustomerWalletBinding: %v", err)
	}
	if got.UserID != bind.UserID {
		t.Errorf("UserID = %v, want %v", got.UserID, bind.UserID)
	}
	if got.WalletAddress != bind.WalletAddress {
		t.Errorf("WalletAddress = %q, want %q", got.WalletAddress, bind.WalletAddress)
	}
	if got.WalletProvider != bind.WalletProvider {
		t.Errorf("WalletProvider = %q, want %q", got.WalletProvider, bind.WalletProvider)
	}
	if got.BoundAt.IsZero() {
		t.Error("BoundAt not stamped")
	}
	// Fresh binding: balance cache is NULL until UpdateCustomerWalletBalanceCache.
	if got.LastBalanceAt != nil || got.LastBalanceLamports != nil {
		t.Errorf("fresh binding has balance cache set: at=%v lamports=%v", got.LastBalanceAt, got.LastBalanceLamports)
	}

	// The balance-cache update path must survive its own round-trip.
	const lamports int64 = 123_456_789
	if err := st.UpdateCustomerWalletBalanceCache(ctx, nil, u.ID, lamports); err != nil {
		t.Fatalf("UpdateCustomerWalletBalanceCache: %v", err)
	}
	got2, err := st.GetCustomerWalletBinding(ctx, nil, u.ID)
	if err != nil {
		t.Fatalf("GetCustomerWalletBinding (after cache): %v", err)
	}
	if got2.LastBalanceLamports == nil || *got2.LastBalanceLamports != lamports {
		t.Errorf("LastBalanceLamports = %v, want %d", got2.LastBalanceLamports, lamports)
	}
	if got2.LastBalanceAt == nil {
		t.Error("LastBalanceAt nil after cache update")
	}

	// Upsert (switch wallet) must RESET the balance cache (ON CONFLICT clears it).
	bind.WalletAddress = "9hYz2NewWalletAfterSwitchYYYYYYYYYYYYYYYYYYYY"
	if err := st.UpsertCustomerWalletBinding(ctx, nil, bind); err != nil {
		t.Fatalf("Upsert (switch): %v", err)
	}
	got3, err := st.GetCustomerWalletBinding(ctx, nil, u.ID)
	if err != nil {
		t.Fatalf("Get (after switch): %v", err)
	}
	if got3.WalletAddress != bind.WalletAddress {
		t.Errorf("switched wallet = %q, want %q", got3.WalletAddress, bind.WalletAddress)
	}
	if got3.LastBalanceLamports != nil {
		t.Errorf("balance cache not reset on wallet switch: %v", got3.LastBalanceLamports)
	}
}
