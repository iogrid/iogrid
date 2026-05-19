package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

func freshSigner(t *testing.T) *tokens.Signer {
	t.Helper()
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	return tokens.NewSignerFromKeys(priv, "k1", "https://t.iogrid.org", []string{"x"}, 15*time.Minute)
}

func TestVerifyBearer_NoHeader_PassesThroughUnauthed(t *testing.T) {
	signer := freshSigner(t)
	called := false
	h := VerifyBearer(signer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if _, ok := AuthedUser(r.Context()); ok {
			t.Errorf("expected no authed user")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatalf("handler not called")
	}
}

func TestVerifyBearer_ValidToken_SetsUser(t *testing.T) {
	signer := freshSigner(t)
	uid := uuid.New()
	tok, _, _ := signer.IssueAccessToken(uid, uuid.New(), "x@x.x", nil, nil, false, nil)

	called := false
	h := VerifyBearer(signer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotID, ok := AuthedUser(r.Context())
		if !ok || gotID != uid {
			t.Errorf("authed user mismatch: got %v ok=%v", gotID, ok)
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !called {
		t.Fatalf("handler not called")
	}
}

func TestRequireBearer_RejectsAnonymous(t *testing.T) {
	h := RequireBearer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("handler should not have been called")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestRequireStepUp_RequiresFlag(t *testing.T) {
	h := RequireStepUp(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("handler should not have been called without step_up")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}
