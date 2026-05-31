package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// fakeBillingSvc implements billingv1connect.ApiKeyServiceHandler for tests.
type fakeBillingSvc struct {
	billingv1connect.UnimplementedApiKeyServiceHandler

	resp *billingv1.ValidateApiKeyResponse
	err  error
	hits int
}

func (f *fakeBillingSvc) ValidateApiKey(
	_ context.Context,
	req *connect.Request[billingv1.ValidateApiKeyRequest],
) (*connect.Response[billingv1.ValidateApiKeyResponse], error) {
	f.hits++
	if f.err != nil {
		return nil, f.err
	}
	return connect.NewResponse(f.resp), nil
}

func startFakeBilling(svc *fakeBillingSvc) (*Connect, func()) {
	mux := http.NewServeMux()
	path, handler := billingv1connect.NewApiKeyServiceHandler(svc)
	mux.Handle(path, handler)
	ts := httptest.NewServer(mux)
	v := NewConnect(ts.URL, ts.Client())
	return v, ts.Close
}

func TestConnect_ValidKey(t *testing.T) {
	svc := &fakeBillingSvc{
		resp: &billingv1.ValidateApiKeyResponse{
			Valid:       true,
			WorkspaceId: &commonv1.UUID{Value: "ws-uuid-1"},
			CustomerId:  &commonv1.UUID{Value: "cust-uuid-1"},
			Tier:        billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER,
			GeoTarget:   "eu-west",
		},
	}
	v, stop := startFakeBilling(svc)
	defer stop()

	cust, err := v.Validate(context.Background(), "sk_live_abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cust.WorkspaceID != "ws-uuid-1" {
		t.Fatalf("WorkspaceID = %q", cust.WorkspaceID)
	}
	if cust.Tier != "starter" {
		t.Fatalf("Tier = %q", cust.Tier)
	}
	if cust.GeoTarget != "eu-west" {
		t.Fatalf("GeoTarget = %q", cust.GeoTarget)
	}
}

func TestConnect_InvalidKey(t *testing.T) {
	svc := &fakeBillingSvc{resp: &billingv1.ValidateApiKeyResponse{Valid: false}}
	v, stop := startFakeBilling(svc)
	defer stop()

	_, err := v.Validate(context.Background(), "bad-key")
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got %v", err)
	}
}

func TestConnect_SuspendedWorkspace(t *testing.T) {
	svc := &fakeBillingSvc{resp: &billingv1.ValidateApiKeyResponse{
		Valid:     true,
		Suspended: true,
	}}
	v, stop := startFakeBilling(svc)
	defer stop()

	_, err := v.Validate(context.Background(), "sk_live_suspended")
	if !errors.Is(err, ErrSuspended) {
		t.Fatalf("expected ErrSuspended, got %v", err)
	}
}

func TestConnect_EmptyKeyRejected(t *testing.T) {
	svc := &fakeBillingSvc{resp: &billingv1.ValidateApiKeyResponse{Valid: true}}
	v, stop := startFakeBilling(svc)
	defer stop()

	_, err := v.Validate(context.Background(), "")
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey for empty key, got %v", err)
	}
	if svc.hits != 0 {
		t.Fatalf("expected 0 upstream hits for empty key, got %d", svc.hits)
	}
}

func TestConnect_CacheHit(t *testing.T) {
	svc := &fakeBillingSvc{
		resp: &billingv1.ValidateApiKeyResponse{
			Valid:       true,
			WorkspaceId: &commonv1.UUID{Value: "ws-cached"},
		},
	}
	v, stop := startFakeBilling(svc)
	defer stop()

	// First call populates the cache.
	if _, err := v.Validate(context.Background(), "sk_cached"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// Second call must be served from cache — upstream hit count stays at 1.
	if _, err := v.Validate(context.Background(), "sk_cached"); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if svc.hits != 1 {
		t.Fatalf("expected 1 upstream hit (cache served 2nd), got %d", svc.hits)
	}
}

func TestConnect_CacheExpiry(t *testing.T) {
	svc := &fakeBillingSvc{
		resp: &billingv1.ValidateApiKeyResponse{
			Valid:       true,
			WorkspaceId: &commonv1.UUID{Value: "ws-expiry"},
		},
	}
	v, stop := startFakeBilling(svc)
	defer stop()

	// Populate cache and then forcibly expire the entry.
	if _, err := v.Validate(context.Background(), "sk_expiry"); err != nil {
		t.Fatalf("first call: %v", err)
	}
	v.mu.Lock()
	e := v.cache["sk_expiry"]
	e.expiry = time.Now().Add(-time.Second) // backdate expiry
	v.cache["sk_expiry"] = e
	v.mu.Unlock()

	// Second call must go upstream because the cache entry is expired.
	if _, err := v.Validate(context.Background(), "sk_expiry"); err != nil {
		t.Fatalf("second call after expiry: %v", err)
	}
	if svc.hits != 2 {
		t.Fatalf("expected 2 upstream hits (expiry evicted cache), got %d", svc.hits)
	}
}

func TestConnect_RPCError_FailClosed(t *testing.T) {
	svc := &fakeBillingSvc{err: connect.NewError(connect.CodeUnavailable, errors.New("billing down"))}
	v, stop := startFakeBilling(svc)
	defer stop()

	_, err := v.Validate(context.Background(), "sk_rpc_fail")
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("expected fail-closed ErrInvalidKey on RPC error, got %v", err)
	}
}

func TestTierString(t *testing.T) {
	cases := []struct {
		in   billingv1.SubscriptionTier
		want string
	}{
		{billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED, "free"},
		{billingv1.SubscriptionTier_SUBSCRIPTION_TIER_PAYG, "payg"},
		{billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER, "starter"},
		{billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH, "growth"},
		{billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE, "enterprise"},
	}
	for _, c := range cases {
		got := tierString(c.in)
		if got != c.want {
			t.Fatalf("tierString(%v) = %q; want %q", c.in, got, c.want)
		}
	}
}
