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

// 401 when no bearer is on the context (Refs #631).
func TestGetNotificationPrefs_RequiresAuth(t *testing.T) {
	api := &API{Clients: &clients.Set{}}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/notifications", nil)
	w := httptest.NewRecorder()
	api.GetNotificationPrefs(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestSaveNotificationPrefs_RequiresAuth(t *testing.T) {
	api := &API{Clients: &clients.Set{}}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/notifications",
		strings.NewReader(`{"prefs":{}}`))
	w := httptest.NewRecorder()
	api.SaveNotificationPrefs(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// 503 when the identity-svc forwarder isn't configured.
func TestGetNotificationPrefs_UnconfiguredForwarder(t *testing.T) {
	api := &API{Clients: &clients.Set{}} // IdentityRaw zero-value → 503
	c := &auth.Claims{}
	c.Subject = uuid.NewString()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/notifications", nil)
	req = req.WithContext(auth.NewContextForTesting(req.Context(), c))
	w := httptest.NewRecorder()
	api.GetNotificationPrefs(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

// GET forwards verbatim to identity-svc's GET /v1/me/notification-prefs
// with the service-token + caller header, passing the upstream body +
// status through.
func TestGetNotificationPrefs_ForwardsAndPassesThrough(t *testing.T) {
	const svcToken = "shared-service-token"
	const upstreamBody = `{"prefs":{"security_alerts":{"email":true,"in_app":true}}}`
	callerID := uuid.New()

	var gotMethod, gotPath, gotAuth, gotUser string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotUser = r.Header.Get("X-Iogrid-User-Id")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(upstreamBody))
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
	req := httptest.NewRequest(http.MethodGet, "/api/v1/account/notifications", nil)
	req = req.WithContext(auth.NewContextForTesting(req.Context(), c))
	w := httptest.NewRecorder()
	api.GetNotificationPrefs(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if gotMethod != http.MethodGet {
		t.Errorf("upstream method = %q, want GET", gotMethod)
	}
	if gotPath != "/v1/me/notification-prefs" {
		t.Errorf("upstream path = %q, want /v1/me/notification-prefs", gotPath)
	}
	if want := "Bearer " + svcToken; gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
	if gotUser != callerID.String() {
		t.Errorf("X-Iogrid-User-Id = %q, want %q", gotUser, callerID.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != upstreamBody {
		t.Errorf("body = %q, want %q", got, upstreamBody)
	}
}

// The browser POSTs (the REST client only does GET/POST); the BFF must
// translate that to identity-svc's idempotent PUT and forward the body.
func TestSaveNotificationPrefs_TranslatesPostToPut(t *testing.T) {
	const reqBody = `{"prefs":{"product_updates":{"email":false,"in_app":true}}}`
	callerID := uuid.New()

	var gotMethod, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusNoContent)
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
	c.Subject = callerID.String()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/account/notifications",
		strings.NewReader(reqBody))
	req = req.WithContext(auth.NewContextForTesting(req.Context(), c))
	w := httptest.NewRecorder()
	api.SaveNotificationPrefs(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 passthrough, got %d body=%s", w.Code, w.Body.String())
	}
	if gotMethod != http.MethodPut {
		t.Errorf("upstream method = %q, want PUT", gotMethod)
	}
	if gotBody != reqBody {
		t.Errorf("upstream body = %q, want %q", gotBody, reqBody)
	}
}
