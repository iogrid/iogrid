package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestAppleSignIn_MissingToken_Returns400 verifies the route is mounted
// and rejects empty identity_token before reaching the auth service.
func TestAppleSignIn_MissingToken_Returns400(t *testing.T) {
	api := New(nil, nil, nil)
	r := chi.NewRouter()
	api.Mount(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/identity/apple-signin",
		strings.NewReader(`{"identity_token":""}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "identity_token is required") {
		t.Errorf("body=%s", w.Body.String())
	}
}

// TestAppleSignIn_MalformedJSON_Returns400 verifies broken JSON is rejected.
func TestAppleSignIn_MalformedJSON_Returns400(t *testing.T) {
	api := New(nil, nil, nil)
	r := chi.NewRouter()
	api.Mount(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/identity/apple-signin",
		strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestAppleSignIn_AuthNil_Returns500 covers the "Auth service not wired"
// guard so the handler never panics on nil dependencies.
func TestAppleSignIn_AuthNil_Returns500(t *testing.T) {
	api := New(nil, nil, nil)
	r := chi.NewRouter()
	api.Mount(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/identity/apple-signin",
		strings.NewReader(`{"identity_token":"some-fake-token"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", w.Code, w.Body.String())
	}
}
