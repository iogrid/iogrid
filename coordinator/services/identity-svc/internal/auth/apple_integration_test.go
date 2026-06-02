//go:build integration
// +build integration

// Apple sign-in integration tests — full Postgres + JWKS roundtrip
// against a fake Apple JWKS server. Mirrors the magic-link / Google
// integration_test.go pattern so the same `go test -tags=integration`
// invocation runs both suites.

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// newTestServiceWithApple wires the standard test service plus the
// AppleValidator pointed at a fake JWKS server.
func newTestServiceWithApple(t *testing.T) (*Service, *fakeJWKSServer, func()) {
	t.Helper()
	pool, cleanup := pgFixture(t)
	svc, _ := newTestService(t, pool)

	f := newFakeJWKSServer(t)
	cache := NewJWKSCache(f.url(), 24*time.Hour, http.DefaultClient)
	svc.Apple = NewAppleValidator(cache)
	svc.AppleSubSalt = []byte("integration-test-salt")
	return svc, f, cleanup
}

func TestApple_NewUser_CreatesUserAndBundle(t *testing.T) {
	svc, f, cleanup := newTestServiceWithApple(t)
	defer cleanup()

	claims := freshClaims("apple-sub-int-1", AppleAudience, AppleIssuer, "n-int-1", time.Now().Add(5*time.Minute))
	claims.Email = "alice.apple@example.com"
	tok := f.sign(t, "", claims)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	res, err := svc.CompleteAppleSignIn(context.Background(), tok, "n-int-1", "Alice Apple", req)
	if err != nil {
		t.Fatalf("CompleteAppleSignIn: %v", err)
	}
	if !res.NewUser {
		t.Errorf("expected NewUser=true")
	}
	if res.Bundle == nil || res.Bundle.AccessToken == "" || res.Bundle.RefreshToken == "" {
		t.Fatalf("missing bundle tokens")
	}
	if res.Bundle.User.PrimaryEmail != "alice.apple@example.com" {
		t.Errorf("PrimaryEmail: %s", res.Bundle.User.PrimaryEmail)
	}
	if res.Bundle.User.DisplayName != "Alice Apple" {
		t.Errorf("DisplayName: %s", res.Bundle.User.DisplayName)
	}
	if !res.NonceValidated {
		t.Errorf("expected NonceValidated=true")
	}
	if res.WalletAddress != "" {
		t.Errorf("expected empty wallet on new user, got %q", res.WalletAddress)
	}
}

func TestApple_ExistingUser_ReturnsSameUser(t *testing.T) {
	svc, f, cleanup := newTestServiceWithApple(t)
	defer cleanup()

	claims := freshClaims("apple-sub-int-2", AppleAudience, AppleIssuer, "", time.Now().Add(5*time.Minute))
	claims.Email = "bob.apple@example.com"

	tok1 := f.sign(t, "", claims)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	res1, err := svc.CompleteAppleSignIn(context.Background(), tok1, "", "Bob", req)
	if err != nil {
		t.Fatalf("first sign-in: %v", err)
	}
	if !res1.NewUser {
		t.Errorf("first sign-in should be NewUser=true")
	}

	// Second sign-in with a brand-new token (different jti but same sub).
	tok2 := f.sign(t, "", claims)
	res2, err := svc.CompleteAppleSignIn(context.Background(), tok2, "", "", req)
	if err != nil {
		t.Fatalf("second sign-in: %v", err)
	}
	if res2.NewUser {
		t.Errorf("second sign-in should be NewUser=false")
	}
	if res1.Bundle.User.ID != res2.Bundle.User.ID {
		t.Errorf("user id changed across sign-ins: %s → %s", res1.Bundle.User.ID, res2.Bundle.User.ID)
	}
}

func TestApple_InvalidToken_Rejects(t *testing.T) {
	svc, _, cleanup := newTestServiceWithApple(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	_, err := svc.CompleteAppleSignIn(context.Background(), "not-a-jwt", "", "", req)
	if err == nil {
		t.Fatal("expected error for garbage token")
	}
}

func TestApple_NotConfigured_ReturnsError(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	svc, _ := newTestService(t, pool)
	// svc.Apple is nil — flow must reject rather than panic.
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	_, err := svc.CompleteAppleSignIn(context.Background(), "anything", "", "", req)
	if err == nil {
		t.Fatal("expected 'not configured' error")
	}
}

// TestApple_WrongAudience_Rejects ensures the bundle id check actually
// fires inside the service layer (it's also covered by the validator
// unit test but we want the e2e wiring to confirm).
func TestApple_WrongAudience_Rejects(t *testing.T) {
	svc, f, cleanup := newTestServiceWithApple(t)
	defer cleanup()

	claims := freshClaims("apple-sub-int-3", "io.someone.app", AppleIssuer, "", time.Now().Add(5*time.Minute))
	tok := f.sign(t, "", claims)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	_, err := svc.CompleteAppleSignIn(context.Background(), tok, "", "", req)
	if err == nil {
		t.Fatal("expected aud-mismatch error")
	}
}

// Reference the jwt import so the build tag still resolves correctly
// if test gets pruned.
var _ = jwt.SigningMethodRS256
