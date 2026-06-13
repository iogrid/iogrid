//go:build integration

// Field-complete round-trip test for customer_wallet_bindings (#726).
//
// This is the wallet $GRID settlement pays to (build-gateway resolves it via
// identity-svc, #720/#723), so a silently-dropped field here is a money bug.
// The #726 audit verified the store CLEAN by inspection; this locks it in
// against the in-memory-green/Postgres-broken class going forward: a future
// column added to CustomerWalletBinding but not the SQL fails the suite.
//
// Fixture: shares newAuthFixture (session_magiclink_roundtrip_integration_
// test.go) — prefer an external DATABASE_URL (local podman dev), else a
// one-shot dockertest Postgres so it RUNS in identity-svc-integration.yml.
// Wipe + re-apply the embedded identity migrations per run; destructive by
// design.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

func TestCustomerWalletBinding_RoundTrip(t *testing.T) {
	st, cleanup := newAuthFixture(t)
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
