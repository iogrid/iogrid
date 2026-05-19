package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
)

// claimsForUser builds an *auth.Claims for a specific user id so the
// per-test handle-collision case can simulate a *second* user trying to
// claim the same handle. Mirrors the withAuth helper in handlers_test.go.
func claimsForUser(userID string) *auth.Claims {
	c := &auth.Claims{}
	c.RegisteredClaims = jwt.RegisteredClaims{Subject: userID}
	return c
}

// onboardCustomerReq builds a POST /api/v1/onboard/customer request.
func onboardCustomerReq(body map[string]any) *http.Request {
	b, _ := json.Marshal(body)
	return httptest.NewRequest(http.MethodPost, "/api/v1/onboard/customer", bytes.NewReader(b))
}

func TestOnboardCustomer_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{}).WithCustomerOnboardStore(NewMemoryCustomerOnboardStore())
	w := httptest.NewRecorder()
	api.OnboardCustomer(w, onboardCustomerReq(map[string]any{
		"handle":       "vcard-prod",
		"display_name": "vCard Production",
	}))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestOnboardCustomer_RejectsMalformedHandle(t *testing.T) {
	cases := []string{
		"",           // empty
		"a",          // too short (1 char)
		"ab",         // too short (2 chars)
		"-leading",   // starts with -
		"1vcard",     // starts with digit
		"VCardProd",  // uppercase
		"vcard prod", // space
		"vcard_prod", // underscore
		"vcard-",     // trailing -
		strings.Repeat("a", 33), // too long
	}
	for _, h := range cases {
		t.Run(h, func(t *testing.T) {
			api := newAPI(t, &clients.Set{}).WithCustomerOnboardStore(NewMemoryCustomerOnboardStore())
			r := withAuth(onboardCustomerReq(map[string]any{
				"handle":       h,
				"display_name": "vCard",
			}))
			w := httptest.NewRecorder()
			api.OnboardCustomer(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("handle %q: want 400, got %d (body=%s)", h, w.Code, w.Body.String())
			}
		})
	}
}

func TestOnboardCustomer_RejectsReservedHandle(t *testing.T) {
	for _, h := range []string{"admin", "iogrid", "api", "billing"} {
		t.Run(h, func(t *testing.T) {
			api := newAPI(t, &clients.Set{}).WithCustomerOnboardStore(NewMemoryCustomerOnboardStore())
			r := withAuth(onboardCustomerReq(map[string]any{
				"handle":       h,
				"display_name": "Reserved",
			}))
			w := httptest.NewRecorder()
			api.OnboardCustomer(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("reserved %q: want 400, got %d", h, w.Code)
			}
			if !strings.Contains(w.Body.String(), "reserved") {
				t.Fatalf("response must explain reservation: %s", w.Body.String())
			}
		})
	}
}

func TestOnboardCustomer_RejectsMalformedBillingEmail(t *testing.T) {
	api := newAPI(t, &clients.Set{}).WithCustomerOnboardStore(NewMemoryCustomerOnboardStore())
	r := withAuth(onboardCustomerReq(map[string]any{
		"handle":        "vcard-prod",
		"display_name":  "vCard",
		"billing_email": "not-an-email",
	}))
	w := httptest.NewRecorder()
	api.OnboardCustomer(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

func TestOnboardCustomer_HappyPathReturnsKeyOnce(t *testing.T) {
	api := newAPI(t, &clients.Set{}).WithCustomerOnboardStore(NewMemoryCustomerOnboardStore())
	r := withAuth(onboardCustomerReq(map[string]any{
		"handle":                  "vcard-prod",
		"display_name":            "Dynolabs vCard — Production",
		"billing_email":           "billing@dynolabs.io",
		"initial_api_key_label":   "linkedin-enrich-cronjob",
	}))
	w := httptest.NewRecorder()
	api.OnboardCustomer(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp CustomerOnboardResponse
	mustReadJSON(t, w.Body, &resp)

	if resp.Handle != "vcard-prod" {
		t.Errorf("handle: %q", resp.Handle)
	}
	if resp.DisplayName != "Dynolabs vCard — Production" {
		t.Errorf("display_name: %q", resp.DisplayName)
	}
	if resp.BillingEmail != "billing@dynolabs.io" {
		t.Errorf("billing_email: %q", resp.BillingEmail)
	}
	if resp.WorkspaceID == "" {
		t.Error("workspace_id empty")
	}
	if resp.APIKey.Plaintext == "" {
		t.Error("api_key.plaintext must be returned on first signup")
	}
	if resp.APIKey.Label != "linkedin-enrich-cronjob" {
		t.Errorf("api_key.label: %q", resp.APIKey.Label)
	}
	if resp.ProxyEndpoint != "proxy.iogrid.org:443" {
		t.Errorf("proxy_endpoint: %q", resp.ProxyEndpoint)
	}
	if !strings.Contains(resp.OnboardingGuide, "phase0") {
		t.Errorf("onboarding_guide should point at Phase 0 docs: %q", resp.OnboardingGuide)
	}
}

func TestOnboardCustomer_HandleCollisionReturns409(t *testing.T) {
	store := NewMemoryCustomerOnboardStore()
	api := newAPI(t, &clients.Set{}).WithCustomerOnboardStore(store)

	// First user claims the handle.
	r1 := withAuth(onboardCustomerReq(map[string]any{
		"handle":       "vcard-prod",
		"display_name": "vCard",
	}))
	w1 := httptest.NewRecorder()
	api.OnboardCustomer(w1, r1)
	if w1.Code != http.StatusCreated {
		t.Fatalf("first signup: want 201, got %d", w1.Code)
	}

	// Second DIFFERENT user tries the same handle → 409.
	r2 := httptest.NewRequest(http.MethodPost, "/api/v1/onboard/customer", bytes.NewReader([]byte(`{"handle":"vcard-prod","display_name":"Other"}`)))
	r2 = r2.WithContext(withClaimsForTest(r2.Context(), claimsForUser("99999999-9999-9999-9999-999999999999")))
	w2 := httptest.NewRecorder()
	api.OnboardCustomer(w2, r2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("collision: want 409, got %d (body=%s)", w2.Code, w2.Body.String())
	}
}

func TestOnboardCustomer_NoStoreReturns503(t *testing.T) {
	api := newAPI(t, &clients.Set{}) // no WithCustomerOnboardStore
	r := withAuth(onboardCustomerReq(map[string]any{
		"handle":       "vcard-prod",
		"display_name": "vCard",
	}))
	w := httptest.NewRecorder()
	api.OnboardCustomer(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
}

func TestMemoryCustomerOnboardStore_IdempotentReserve(t *testing.T) {
	s := NewMemoryCustomerOnboardStore()
	id1, err := s.Reserve("vcard-prod", "user-1")
	if err != nil {
		t.Fatalf("first reserve: %v", err)
	}
	id2, err := s.Reserve("vcard-prod", "user-1")
	if err != nil {
		t.Fatalf("idempotent reserve: %v", err)
	}
	if id1 != id2 {
		t.Fatalf("ids should match: %s vs %s", id1, id2)
	}
}

func TestMemoryCustomerOnboardStore_LookupByHandle(t *testing.T) {
	s := NewMemoryCustomerOnboardStore()
	id, _ := s.Reserve("vcard-prod", "user-1")
	got, ok := s.LookupByHandle("vcard-prod")
	if !ok || got != id {
		t.Fatalf("lookup mismatch: ok=%v got=%s id=%s", ok, got, id)
	}
	if _, ok := s.LookupByHandle("nonexistent"); ok {
		t.Fatal("lookup should miss")
	}
}
