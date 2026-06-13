package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

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
	body := w.Body.Bytes()
	// The web protobuf-es client reads proto3-JSON (camelCase). Assert the
	// raw wire shape carries camelCase keys — a stdlib encoding (snake_case
	// "total_earned") would render every card undefined ⇒ "0 $GRID" (#633).
	assertCamelEarningsKeys(t, body)
	var resp billingv1.GetEarningsSummaryResponse
	mustReadProtoJSON(t, body, &resp)
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

// assertCamelEarningsKeys verifies the JSON the BFF emits uses the
// proto3-JSON camelCase field names the web protobuf-es client expects.
// This is the load-bearing assertion for #633: with stdlib encoding/json
// the keys were snake_case ("summary.total_earned", "lifetime_workloads")
// and the web read undefined everywhere.
func assertCamelEarningsKeys(t *testing.T, body []byte) {
	t.Helper()
	var env struct {
		Summary map[string]json.RawMessage `json:"summary"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v: %s", err, body)
	}
	if env.Summary == nil {
		t.Fatalf("no summary in body: %s", body)
	}
	for _, camel := range []string{"totalEarned", "last30d", "last7d", "pendingPayout", "lifetimeWorkloads"} {
		if _, ok := env.Summary[camel]; !ok {
			t.Fatalf("summary missing camelCase key %q (got keys %v) — stdlib snake_case regression (#633): %s",
				camel, keysOf(env.Summary), body)
		}
	}
	for _, snake := range []string{"total_earned", "pending_payout", "lifetime_workloads"} {
		if _, ok := env.Summary[snake]; ok {
			t.Fatalf("summary leaked snake_case key %q — web protobuf-es can't read it (#633): %s", snake, body)
		}
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// mustReadProtoJSON deserialises a proto3-JSON body into a protobuf message
// using protojson — the correct decoder for the camelCase wire shape the
// BFF now emits. (mustReadJSON, the stdlib path, would silently read zero
// from camelCase keys against the snake_case protoc-gen-go tags.)
func mustReadProtoJSON(t *testing.T, body []byte, out proto.Message) {
	t.Helper()
	if err := protojson.Unmarshal(body, out); err != nil {
		t.Fatalf("protojson unmarshal: %v: %s", err, body)
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
	body := w.Body.Bytes()
	assertCamelEarningsKeys(t, body)
	// Decode the way the web does (proto3-JSON / camelCase). This is the
	// regression guard for #633: a credited provider (1.5 $GRID, 3
	// workloads) MUST survive the round-trip, not collapse to zero.
	var resp billingv1.GetEarningsSummaryResponse
	mustReadProtoJSON(t, body, &resp)
	if resp.Summary.GetLifetimeWorkloads() != 3 {
		t.Fatalf("lifetimeWorkloads = %d, want 3 (body=%s)", resp.Summary.GetLifetimeWorkloads(), body)
	}
	if got := resp.Summary.GetTotalEarned().GetMicros(); got != 1_500_000 {
		t.Fatalf("totalEarned.micros = %d, want 1500000 (body=%s)", got, body)
	}
	if got := resp.Summary.GetTotalEarned().GetCurrency(); got != "GRID" {
		t.Fatalf("totalEarned.currency = %q, want GRID", got)
	}
}

// TestGetAdminProviderEarnings_AnyProvider verifies the operator
// surface (#758): an ADMIN caller can read ANY provider's earnings by
// path UUID — NOT gated on ownership — and the on-chain settled $GRID +
// settled-build count survive the proto3-JSON round-trip. This is the
// regression guard for "the founder can't SEE Hatice's grids": the
// provider whose builds settled (808ce330) is owned by a DIFFERENT
// account than the operator, so the owner-scoped /provide path returns
// the operator's own zero — this admin path returns the real number.
func TestGetAdminProviderEarnings_AnyProvider(t *testing.T) {
	const pid = "808ce330-79c1-4390-8cc6-87c5ce5a94d8"
	called := false
	set := &clients.Set{
		// Deliberately NO ProvidersRegistration: the admin handler must
		// not consult ownership at all. If it tried, it would nil-panic
		// or 403 — either way the test fails, proving the path is
		// ownership-independent.
		BillingEarnings: &mockBillingEarnings{
			getSummary: func(_ context.Context, req *billingv1.GetEarningsSummaryRequest) (*billingv1.GetEarningsSummaryResponse, error) {
				called = true
				if got := req.GetProviderId().GetValue(); got != pid {
					t.Fatalf("provider_id = %q, want %q", got, pid)
				}
				return &billingv1.GetEarningsSummaryResponse{
					Summary: &billingv1.EarningsSummary{
						ProviderId:    &commonv1.UUID{Value: pid},
						TotalEarned:   &commonv1.Money{Currency: "GRID", Micros: 11_050_000},
						SettledGrid:   &commonv1.Money{Currency: "GRID", Micros: 11_050_000},
						SettledBuilds: 14,
					},
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/providers/"+pid+"/earnings", nil)
	// chi normally injects the {id} URL param from the route pattern; in
	// a direct handler unit-test we wire it by hand via a RouteContext.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", pid)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	r := withAuth(req, "ADMIN")
	w := httptest.NewRecorder()
	api.GetAdminProviderEarnings(w, r)
	if !called {
		t.Fatal("billing-svc was not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.Bytes()
	assertCamelEarningsKeys(t, body)
	var resp billingv1.GetEarningsSummaryResponse
	mustReadProtoJSON(t, body, &resp)
	if got := resp.Summary.GetSettledGrid().GetMicros(); got != 11_050_000 {
		t.Fatalf("settledGrid.micros = %d, want 11050000 (body=%s)", got, body)
	}
	if got := resp.Summary.GetSettledBuilds(); got != 14 {
		t.Fatalf("settledBuilds = %d, want 14 (body=%s)", got, body)
	}
}

// TestGetAdminProviderEarnings_RequiresAdmin locks the gate: a
// non-admin authenticated caller (no ADMIN role) must get 403 and the
// billing client must never be reached. RequireRole("ADMIN") gates the
// router, but mustAdmin re-checks inside the handler (defence-in-depth)
// — this test exercises that inner check directly.
func TestGetAdminProviderEarnings_RequiresAdmin(t *testing.T) {
	set := &clients.Set{
		BillingEarnings: &mockBillingEarnings{
			getSummary: func(_ context.Context, _ *billingv1.GetEarningsSummaryRequest) (*billingv1.GetEarningsSummaryResponse, error) {
				t.Fatal("billing-svc must not be called for a non-admin caller")
				return nil, nil
			},
		},
	}
	api := newAPI(t, set)
	const pid = "808ce330-79c1-4390-8cc6-87c5ce5a94d8"
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/providers/"+pid+"/earnings", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", pid)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	// withAuth with NO roles → authenticated but not ADMIN.
	r := withAuth(req)
	w := httptest.NewRecorder()
	api.GetAdminProviderEarnings(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
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
