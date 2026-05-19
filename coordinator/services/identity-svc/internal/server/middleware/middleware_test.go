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

// --- BFF service-token bypass (#232) -------------------------------------
//
// These cover the Phase 0 stop-gap in VerifyBearer that lets the Next.js
// BFF assert a user identity on behalf of an authenticated browser
// session that does not yet hold a real identity-svc JWT. End state:
// the bypass is removed once the NextAuth→identity-svc token exchange
// ships.

func TestVerifyBearer_ServiceToken_SetsUserFromHeader(t *testing.T) {
	signer := freshSigner(t)
	t.Setenv("IOGRID_SERVICE_TOKEN", "shh-secret")
	uid := uuid.New()

	var seen uuid.UUID
	var sawClaims bool
	h := VerifyBearer(signer)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		id, ok := AuthedUser(r.Context())
		if !ok {
			t.Fatalf("expected authed user via service-token path")
		}
		seen = id
		_, sawClaims = AuthedClaims(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer shh-secret")
	req.Header.Set("X-Iogrid-User-Id", uid.String())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if seen != uid {
		t.Fatalf("user id mismatch: got %s want %s", seen, uid)
	}
	// Step-up must NOT be claimable via the service-token path.
	if sawClaims {
		t.Fatalf("service-token path must not synthesize JWT claims")
	}
}

func TestVerifyBearer_ServiceToken_BadHeaderRejected(t *testing.T) {
	signer := freshSigner(t)
	t.Setenv("IOGRID_SERVICE_TOKEN", "shh-secret")

	h := VerifyBearer(signer)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, ok := AuthedUser(r.Context()); ok {
			t.Errorf("malformed X-Iogrid-User-Id must not authenticate")
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer shh-secret")
	req.Header.Set("X-Iogrid-User-Id", "not-a-uuid")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
}

func TestVerifyBearer_ServiceToken_DisabledWhenEnvUnset(t *testing.T) {
	signer := freshSigner(t)
	t.Setenv("IOGRID_SERVICE_TOKEN", "")
	uid := uuid.New()

	h := VerifyBearer(signer)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, ok := AuthedUser(r.Context()); ok {
			t.Errorf("service-token bypass must be disabled when env unset")
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer anything")
	req.Header.Set("X-Iogrid-User-Id", uid.String())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
}

func TestVerifyBearer_ServiceToken_WrongSecretFallsThroughToJWT(t *testing.T) {
	signer := freshSigner(t)
	t.Setenv("IOGRID_SERVICE_TOKEN", "shh-secret")
	uid := uuid.New()

	h := VerifyBearer(signer)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// Wrong secret — falls through to signer.Verify which rejects
		// the opaque token. Final state: no authed user.
		if _, ok := AuthedUser(r.Context()); ok {
			t.Errorf("wrong secret must not authenticate")
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-secret")
	req.Header.Set("X-Iogrid-User-Id", uid.String())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
}
