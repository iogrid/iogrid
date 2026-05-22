package handlers

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
)

// 401 when no bearer is on the context.
func TestSetMyPreferredLandingRole_RequiresAuth(t *testing.T) {
	api := &API{Clients: &clients.Set{}}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/preferred-landing-role",
		strings.NewReader(`{"role":"provider"}`))
	w := httptest.NewRecorder()
	api.SetMyPreferredLandingRole(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// 503 when the identity-svc forwarder isn't configured (dev/test env
// without IDENTITY_SVC_URL + IOGRID_SERVICE_TOKEN). Surfaces the
// missing-config state loudly instead of silently 401-ing.
func TestSetMyPreferredLandingRole_UnconfiguredForwarder(t *testing.T) {
	api := &API{Clients: &clients.Set{}} // IdentityRaw zero-value → 503
	c := &auth.Claims{}
	c.Subject = uuid.NewString()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/preferred-landing-role",
		strings.NewReader(`{"role":"customer"}`))
	req = req.WithContext(auth.NewContextForTesting(req.Context(), c))
	w := httptest.NewRecorder()
	api.SetMyPreferredLandingRole(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

// Happy-path forward: handler stamps the service-token + caller's
// userID on the outbound request, and passes through the upstream
// 204 status. Asserts the exact headers identity-svc's middleware
// reads.
func TestSetMyPreferredLandingRole_ForwardsWithServiceTokenAndUserHeader(t *testing.T) {
	const svcToken = "shared-service-token"
	callerID := uuid.New()

	var gotAuth, gotUserHeader, gotMethod, gotPath, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotUserHeader = r.Header.Get("X-Iogrid-User-Id")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	api := &API{Clients: &clients.Set{
		IdentityRaw: clients.IdentityRawForwarder{
			BaseURL:      upstream.URL,
			ServiceToken: svcToken,
			HTTPClient:   upstream.Client(),
		},
	}}
	c := &auth.Claims{}
	c.Subject = callerID.String()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/preferred-landing-role",
		strings.NewReader(`{"role":"vpn"}`))
	req = req.WithContext(auth.NewContextForTesting(req.Context(), c))
	w := httptest.NewRecorder()
	api.SetMyPreferredLandingRole(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 passthrough, got %d body=%s", w.Code, w.Body.String())
	}
	if gotMethod != http.MethodPut {
		t.Errorf("upstream method = %q, want PUT", gotMethod)
	}
	if gotPath != "/v1/me/preferred-landing-role" {
		t.Errorf("upstream path = %q, want /v1/me/preferred-landing-role", gotPath)
	}
	if want := "Bearer " + svcToken; gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
	if gotUserHeader != callerID.String() {
		t.Errorf("X-Iogrid-User-Id header = %q, want %q", gotUserHeader, callerID.String())
	}
	if gotBody != `{"role":"vpn"}` {
		t.Errorf("upstream body = %q, want %q", gotBody, `{"role":"vpn"}`)
	}
}

// Error from identity-svc (e.g. 400 invalid_argument) passes through
// verbatim — the gateway-bff doesn't repackage the upstream envelope.
func TestSetMyPreferredLandingRole_PassesThroughUpstreamErrors(t *testing.T) {
	const errBody = `{"code":"invalid_argument","message":"role must be one of …"}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(errBody))
	}))
	defer upstream.Close()

	api := &API{Clients: &clients.Set{
		IdentityRaw: clients.IdentityRawForwarder{
			BaseURL:      upstream.URL,
			ServiceToken: "tok",
			HTTPClient:   upstream.Client(),
		},
	}}
	c := &auth.Claims{}
	c.Subject = uuid.NewString()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/me/preferred-landing-role",
		strings.NewReader(`{"role":"bogus"}`))
	req = req.WithContext(auth.NewContextForTesting(req.Context(), c))
	w := httptest.NewRecorder()
	api.SetMyPreferredLandingRole(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 passthrough, got %d", w.Code)
	}
	if got := strings.TrimSpace(w.Body.String()); got != errBody {
		t.Fatalf("body = %q, want %q", got, errBody)
	}
}
