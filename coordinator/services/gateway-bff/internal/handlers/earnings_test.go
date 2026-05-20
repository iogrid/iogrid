package handlers

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
)

// mockBillingEarnings is the test double for clients.BillingEarningsClient.
// Each func field defaults to a benign response when unset so tests can
// stub only the call they exercise.
type mockBillingEarnings struct {
	getSummary func(context.Context, *billingv1.GetEarningsSummaryRequest) (*billingv1.GetEarningsSummaryResponse, error)
	getMethod  func(context.Context, *billingv1.GetPayoutMethodRequest) (*billingv1.GetPayoutMethodResponse, error)
	setMethod  func(context.Context, *billingv1.SetPayoutMethodRequest) (*billingv1.SetPayoutMethodResponse, error)
}

func (m *mockBillingEarnings) GetEarningsSummary(ctx context.Context, req *billingv1.GetEarningsSummaryRequest) (*billingv1.GetEarningsSummaryResponse, error) {
	if m.getSummary == nil {
		return &billingv1.GetEarningsSummaryResponse{}, nil
	}
	return m.getSummary(ctx, req)
}
func (m *mockBillingEarnings) GetPayoutMethod(ctx context.Context, req *billingv1.GetPayoutMethodRequest) (*billingv1.GetPayoutMethodResponse, error) {
	if m.getMethod == nil {
		return &billingv1.GetPayoutMethodResponse{}, nil
	}
	return m.getMethod(ctx, req)
}
func (m *mockBillingEarnings) SetPayoutMethod(ctx context.Context, req *billingv1.SetPayoutMethodRequest) (*billingv1.SetPayoutMethodResponse, error) {
	if m.setMethod == nil {
		return &billingv1.SetPayoutMethodResponse{}, nil
	}
	return m.setMethod(ctx, req)
}

// TestGetProviderEarningsSummary_NoProvider verifies that when the caller
// owns zero paired providers the handler returns a typed-zero envelope
// with currency "GRID" — NOT a 404 (the bug from #324) and NOT a 200
// keyed on user_id (the bug from #305).
func TestGetProviderEarningsSummary_NoProvider(t *testing.T) {
	set := &clients.Set{
		ProvidersRegistration: staticRegistration(), // zero providers owned
		BillingEarnings: &mockBillingEarnings{
			getSummary: func(_ context.Context, _ *billingv1.GetEarningsSummaryRequest) (*billingv1.GetEarningsSummaryResponse, error) {
				t.Fatal("billing-svc should not be called when caller owns no provider")
				return nil, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/earnings/summary", nil))
	w := httptest.NewRecorder()
	api.GetProviderEarningsSummary(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp billingv1.GetEarningsSummaryResponse
	mustReadJSON(t, w.Body, &resp)
	if resp.Summary == nil || resp.Summary.TotalEarned == nil {
		t.Fatalf("summary missing TotalEarned: %#v", resp.Summary)
	}
	if resp.Summary.TotalEarned.Currency != "GRID" {
		t.Fatalf("default currency = %q, want GRID", resp.Summary.TotalEarned.Currency)
	}
	if resp.Summary.TotalEarned.Micros != 0 {
		t.Fatalf("micros = %d, want 0", resp.Summary.TotalEarned.Micros)
	}
}

// TestGetProviderEarningsSummary_OwnedProvider verifies the handler
// forwards the resolved provider_id to billing-svc and passes the
// response through verbatim.
func TestGetProviderEarningsSummary_OwnedProvider(t *testing.T) {
	const pid = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	called := false
	set := &clients.Set{
		ProvidersRegistration: staticRegistration(&providersv1.Provider{
			Id:     &commonv1.UUID{Value: pid},
			Status: providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE,
		}),
		BillingEarnings: &mockBillingEarnings{
			getSummary: func(_ context.Context, req *billingv1.GetEarningsSummaryRequest) (*billingv1.GetEarningsSummaryResponse, error) {
				called = true
				if got := req.GetProviderId().GetValue(); got != pid {
					t.Fatalf("provider_id = %q, want %q", got, pid)
				}
				return &billingv1.GetEarningsSummaryResponse{
					Summary: &billingv1.EarningsSummary{
						ProviderId:        &commonv1.UUID{Value: pid},
						TotalEarned:       &commonv1.Money{Currency: "GRID", Micros: 1_500_000},
						LifetimeWorkloads: 3,
					},
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/earnings/summary", nil))
	w := httptest.NewRecorder()
	api.GetProviderEarningsSummary(w, r)
	if !called {
		t.Fatal("billing-svc was not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp billingv1.GetEarningsSummaryResponse
	mustReadJSON(t, w.Body, &resp)
	if resp.Summary.LifetimeWorkloads != 3 {
		t.Fatalf("lifetimeWorkloads = %d, want 3", resp.Summary.LifetimeWorkloads)
	}
}

// TestGetProviderPayoutMethod_DefaultsToUnspecified verifies the
// no-election path: billing-svc returns an UNSPECIFIED placeholder for
// users who haven't picked yet, and the BFF forwards it unchanged.
func TestGetProviderPayoutMethod_DefaultsToUnspecified(t *testing.T) {
	set := &clients.Set{
		BillingEarnings: &mockBillingEarnings{
			getMethod: func(_ context.Context, req *billingv1.GetPayoutMethodRequest) (*billingv1.GetPayoutMethodResponse, error) {
				if got := req.GetUserId().GetValue(); got != fakeUserID {
					t.Fatalf("user_id = %q, want %q", got, fakeUserID)
				}
				return &billingv1.GetPayoutMethodResponse{
					Method: &billingv1.PayoutMethod{
						UserId: &commonv1.UUID{Value: fakeUserID},
						Kind:   billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_UNSPECIFIED,
					},
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/payout-method", nil))
	w := httptest.NewRecorder()
	api.GetProviderPayoutMethod(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestSetProviderPayoutMethod_RoundTrip verifies that the JSON body
// shape decodes correctly, the kind is parsed via parsePayoutMethodKind
// (both bare and prefixed forms), and the upstream call receives the
// right user_id from the auth context.
func TestSetProviderPayoutMethod_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		body string
		want billingv1.PayoutMethodKind
	}{
		{"bare cash", `{"kind":"CASH_USDC","destination_address":"5x...solana..."}`, billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CASH_USDC},
		{"prefixed charity", `{"kind":"PAYOUT_METHOD_KIND_CHARITY","charity_id":"eff"}`, billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CHARITY},
		{"empty defaults to unspecified", `{"kind":""}`, billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_UNSPECIFIED},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var sawKind billingv1.PayoutMethodKind
			set := &clients.Set{
				BillingEarnings: &mockBillingEarnings{
					setMethod: func(_ context.Context, req *billingv1.SetPayoutMethodRequest) (*billingv1.SetPayoutMethodResponse, error) {
						sawKind = req.GetKind()
						if got := req.GetUserId().GetValue(); got != fakeUserID {
							t.Fatalf("user_id = %q, want %q", got, fakeUserID)
						}
						return &billingv1.SetPayoutMethodResponse{
							Method: &billingv1.PayoutMethod{
								UserId: &commonv1.UUID{Value: fakeUserID},
								Kind:   req.GetKind(),
							},
						}, nil
					},
				},
			}
			api := newAPI(t, set)
			req := httptest.NewRequest(http.MethodPut, "/api/v1/provide/payout-method", bytes.NewReader([]byte(tc.body)))
			req.Header.Set("Content-Type", "application/json")
			r := withAuth(req)
			w := httptest.NewRecorder()
			api.SetProviderPayoutMethod(w, r)
			if w.Code != http.StatusOK {
				t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
			}
			if sawKind != tc.want {
				t.Fatalf("upstream kind = %v, want %v", sawKind, tc.want)
			}
		})
	}
}

// TestSetProviderPayoutMethod_UnknownKind verifies the BFF rejects an
// unknown kind with 400 instead of forwarding it to billing-svc (which
// would itself reject, but the round-trip wastes a call).
func TestSetProviderPayoutMethod_UnknownKind(t *testing.T) {
	set := &clients.Set{
		BillingEarnings: &mockBillingEarnings{
			setMethod: func(_ context.Context, _ *billingv1.SetPayoutMethodRequest) (*billingv1.SetPayoutMethodResponse, error) {
				t.Fatal("billing-svc should not be called on unknown kind")
				return nil, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodPut, "/api/v1/provide/payout-method",
		strings.NewReader(`{"kind":"NOT_A_REAL_KIND"}`)))
	w := httptest.NewRecorder()
	api.SetProviderPayoutMethod(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// TestEarningsSurface_RequiresAuth locks the auth gate on all three
// routes (a missing Bearer must return 401 without ever hitting the
// upstream client).
func TestEarningsSurface_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{
		BillingEarnings: &mockBillingEarnings{
			getSummary: func(_ context.Context, _ *billingv1.GetEarningsSummaryRequest) (*billingv1.GetEarningsSummaryResponse, error) {
				t.Fatal("should not reach upstream when unauthenticated")
				return nil, nil
			},
		},
	})
	for name, fn := range map[string]http.HandlerFunc{
		"summary":    api.GetProviderEarningsSummary,
		"get-method": api.GetProviderPayoutMethod,
		"set-method": api.SetProviderPayoutMethod,
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/x", nil)
			w := httptest.NewRecorder()
			fn(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s: want 401, got %d", name, w.Code)
			}
		})
	}
}
