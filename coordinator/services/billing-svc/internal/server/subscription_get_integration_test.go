//go:build integration

// End-to-end proof for #802: the SubscriptionService.GetSubscription RPC
// — which gateway-bff's GetVPNAccount (/api/v1/vpn/account → web
// /customer/billing + /vpn) calls — was an Unimplemented stub, so every
// billing page view shipped a 501 to the browser console. These tests
// hit the REAL RPC handler against a throwaway Postgres and assert:
//
//   - the common NO-SUBSCRIPTION case returns an EMPTY (non-error)
//     response (subscription == nil) — NOT CodeUnimplemented, NOT
//     CodeNotFound — so the BFF maps it onto the public "FREE" tier view
//     and the page renders clean instead of a masked 501.
//   - a real subscription row round-trips through the RPC with every
//     field intact.
//
// Run with the same throwaway PG fixture as the grid integration suite:
//
//	DATABASE_URL=postgres://postgres:postgres@localhost:55433/billing_svc_test?sslmode=disable \
//	    go test -tags=integration -run TestGetSubscription ./internal/server/...
package server

import (
	"context"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	billingstore "github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
	shareddb "github.com/iogrid/iogrid/coordinator/shared/db"
)

const subDSN = "postgres://postgres:postgres@localhost:55433/billing_svc_test?sslmode=disable"

func newSubFixture(t *testing.T) (*billingstore.Store, func()) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = subDSN
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("no Postgres at %s: %v", dsn, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("Postgres unreachable: %v", err)
	}
	if err := shareddb.MigrateUpFS(ctx, dsn, billingstore.Migrations, "migrations"); err != nil {
		pool.Close()
		t.Fatalf("migrate: %v", err)
	}
	if _, err := pool.Exec(ctx, `TRUNCATE subscription`); err != nil {
		pool.Close()
		t.Fatalf("truncate subscription: %v", err)
	}
	return billingstore.New(pool), func() { pool.Close() }
}

// The free-tier / prepaid-$GRID case (no subscription row): the RPC must
// return a 200-equivalent EMPTY response, NOT an error. This is the
// exact 501→clean-200 fix — before #802 this path fell through to the
// embedded Unimplemented stub.
func TestGetSubscription_NoSubscriptionIsEmptyNotError(t *testing.T) {
	st, cleanup := newSubFixture(t)
	defer cleanup()

	h := NewSubscriptionHandler(st, nil)
	resp, err := h.GetSubscription(context.Background(), connect.NewRequest(&billingv1.GetSubscriptionRequest{
		WorkspaceId: &commonv1.UUID{Value: uuid.NewString()},
	}))
	if err != nil {
		t.Fatalf("GetSubscription on a workspace with no subscription returned an error (want empty 200): code=%v err=%v",
			connect.CodeOf(err), err)
	}
	if resp.Msg.GetSubscription() != nil {
		t.Errorf("subscription = %v, want nil (no row on file → free tier)", resp.Msg.GetSubscription())
	}
}

// A real subscription row must round-trip through the RPC intact, so the
// day a customer DOES subscribe the billing surface shows the true tier.
func TestGetSubscription_PopulatedRowRoundTrips(t *testing.T) {
	st, cleanup := newSubFixture(t)
	defer cleanup()

	wsID := uuid.New()
	start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	row := billingstore.Subscription{
		ID:                   uuid.New(),
		WorkspaceID:          wsID,
		Tier:                 "GROWTH",
		Status:               "active",
		StripeCustomerID:     "cus_INT123",
		StripeSubscriptionID: "sub_INT789",
		CurrentPeriodStart:   &start,
		CurrentPeriodEnd:     &end,
		CreatedAt:            time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC),
	}
	if err := st.UpsertSubscription(context.Background(), row); err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}

	h := NewSubscriptionHandler(st, nil)
	resp, err := h.GetSubscription(context.Background(), connect.NewRequest(&billingv1.GetSubscriptionRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID.String()},
	}))
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	sub := resp.Msg.GetSubscription()
	if sub == nil {
		t.Fatal("subscription = nil, want the seeded GROWTH row")
	}
	if sub.GetWorkspaceId().GetValue() != wsID.String() {
		t.Errorf("workspace_id = %q, want %q", sub.GetWorkspaceId().GetValue(), wsID.String())
	}
	if sub.GetTier() != billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH {
		t.Errorf("tier = %v, want GROWTH", sub.GetTier())
	}
	if sub.GetStatus() != billingv1.SubscriptionStatus_SUBSCRIPTION_STATUS_ACTIVE {
		t.Errorf("status = %v, want ACTIVE", sub.GetStatus())
	}
	if sub.GetStripeSubscriptionId() != "sub_INT789" {
		t.Errorf("stripe_subscription_id = %q, want sub_INT789", sub.GetStripeSubscriptionId())
	}
	if !sub.GetCurrentPeriod().GetStart().AsTime().Equal(start) {
		t.Errorf("current_period.start = %v, want %v", sub.GetCurrentPeriod().GetStart().AsTime(), start)
	}
}
