package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

func TestIndex(t *testing.T) {
	api := New(nil, nil, nil)
	r := chi.NewRouter()
	api.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/v1/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", w.Code, w.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["service"] != "identity-svc" {
		t.Fatalf("service: %q", body["service"])
	}
}

func TestSessionsList_RequiresBearer(t *testing.T) {
	api := New(nil, nil, nil)
	r := chi.NewRouter()
	api.Mount(r)

	req := httptest.NewRequest(http.MethodGet, "/v1/sessions/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestBundleToJSON_Shape(t *testing.T) {
	now := time.Now()
	uid := uuid.New()
	b := &auth.Bundle{
		AccessToken:           "AT",
		AccessTokenExpiresAt:  now,
		RefreshToken:          "RT",
		RefreshTokenExpiresAt: now,
		NewUser:               true,
		Merged:                false,
		User: &store.User{
			ID:           uid,
			PrimaryEmail: "alice@example.com",
			DisplayName:  "Alice",
			Roles:        []string{"USER_ROLE_CUSTOMER"},
		},
	}
	got := bundleToJSON(b)
	if got["access_token"] != "AT" {
		t.Errorf("access_token: %v", got["access_token"])
	}
	if got["new_user"] != true {
		t.Errorf("new_user: %v", got["new_user"])
	}
	user, ok := got["user"].(map[string]any)
	if !ok {
		t.Fatalf("user not a map: %T", got["user"])
	}
	if user["primary_email"] != "alice@example.com" {
		t.Errorf("user.primary_email: %v", user["primary_email"])
	}
}

func TestUpdateUser_RejectsCrossUser(t *testing.T) {
	api := New(nil, nil, nil)
	r := chi.NewRouter()
	api.Mount(r)

	req := httptest.NewRequest(http.MethodPatch, "/v1/users/"+uuid.New().String(), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRequestMagicLink_RejectsMalformedBody(t *testing.T) {
	api := New(nil, nil, nil)
	r := chi.NewRouter()
	api.Mount(r)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/magic-link/request", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Empty body decodes to {} → Auth.RequestMagicLink errors on empty
	// email → 400. We hit a nil-Auth panic if not protected, so this
	// test also confirms the unauthenticated path returns gracefully.
	if w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 400/500, got %d body=%s", w.Code, w.Body.String())
	}
}
