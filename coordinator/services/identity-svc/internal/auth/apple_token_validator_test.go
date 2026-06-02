package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeJWKSServer wraps a single RSA keypair, serves it as a JWKS doc,
// and signs arbitrary claims with the private half so tests can mint
// fake Apple identity tokens locally — no network.
type fakeJWKSServer struct {
	priv    *rsa.PrivateKey
	pub     *rsa.PublicKey
	kid     string
	srv     *httptest.Server
	hits    atomic.Int64
	respond func(w http.ResponseWriter) // overridable
}

func newFakeJWKSServer(t *testing.T) *fakeJWKSServer {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeJWKSServer{priv: priv, pub: &priv.PublicKey, kid: "test-kid-1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		f.hits.Add(1)
		if f.respond != nil {
			f.respond(w)
			return
		}
		f.writeJWKS(w, f.kid, f.pub)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeJWKSServer) url() string {
	return f.srv.URL + "/keys"
}

func (f *fakeJWKSServer) writeJWKS(w http.ResponseWriter, kid string, pub *rsa.PublicKey) {
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"keys": []map[string]string{
			{"kty": "RSA", "kid": kid, "use": "sig", "alg": "RS256", "n": n, "e": e},
		},
	})
}

// sign creates a JWT signed by the fake server's key with the given claims.
// kid header defaults to the server's current kid; pass "" to use it.
func (f *fakeJWKSServer) sign(t *testing.T, kid string, claims jwt.Claims) string {
	t.Helper()
	if kid == "" {
		kid = f.kid
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(f.priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// freshClaims builds AppleClaims with all the must-have fields populated.
func freshClaims(sub, aud, iss, nonce string, exp time.Time) AppleClaims {
	return AppleClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    iss,
			Subject:   sub,
			Audience:  jwt.ClaimStrings{aud},
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		Nonce: nonce,
		Email: "user@example.com",
	}
}

func newTestValidator(t *testing.T, f *fakeJWKSServer) *AppleValidator {
	t.Helper()
	cache := NewJWKSCache(f.url(), 24*time.Hour, http.DefaultClient)
	v := NewAppleValidator(cache)
	v.Issuer = AppleIssuer
	v.Audience = AppleAudience
	return v
}

func TestApple_Validate_Valid(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	tok := f.sign(t, "", freshClaims("apple-sub-1", AppleAudience, AppleIssuer, "n1", time.Now().Add(5*time.Minute)))
	claims, err := v.Validate(context.Background(), tok, "n1")
	if err != nil {
		t.Fatalf("expected ok, got %v", err)
	}
	if claims.Subject != "apple-sub-1" {
		t.Errorf("subject: %s", claims.Subject)
	}
	if claims.Email != "user@example.com" {
		t.Errorf("email: %s", claims.Email)
	}
}

func TestApple_Validate_ExpiredToken(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	tok := f.sign(t, "", freshClaims("apple-sub-2", AppleAudience, AppleIssuer, "", time.Now().Add(-5*time.Minute)))
	_, err := v.Validate(context.Background(), tok, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAppleTokenInvalid) {
		t.Errorf("err: %v", err)
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("want 'expired' in error, got %v", err)
	}
}

func TestApple_Validate_WrongAudience(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	tok := f.sign(t, "", freshClaims("apple-sub-3", "io.someoneelse.app", AppleIssuer, "", time.Now().Add(5*time.Minute)))
	_, err := v.Validate(context.Background(), tok, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAppleTokenInvalid) {
		t.Errorf("err: %v", err)
	}
	if !strings.Contains(err.Error(), "aud") {
		t.Errorf("want 'aud' in error, got %v", err)
	}
}

func TestApple_Validate_WrongIssuer(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	tok := f.sign(t, "", freshClaims("apple-sub-4", AppleAudience, "https://evil.example", "", time.Now().Add(5*time.Minute)))
	_, err := v.Validate(context.Background(), tok, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAppleTokenInvalid) {
		t.Errorf("err: %v", err)
	}
	if !strings.Contains(err.Error(), "iss") {
		t.Errorf("want 'iss' in error, got %v", err)
	}
}

func TestApple_Validate_MissingNonce_RequestSuppliedOne(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	// Token has no nonce, but client claims it supplied one — must reject.
	claims := freshClaims("apple-sub-5", AppleAudience, AppleIssuer, "", time.Now().Add(5*time.Minute))
	tok := f.sign(t, "", claims)
	_, err := v.Validate(context.Background(), tok, "client-nonce")
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, ErrAppleTokenInvalid) {
		t.Errorf("err: %v", err)
	}
	if !strings.Contains(err.Error(), "nonce") {
		t.Errorf("want 'nonce' in error, got %v", err)
	}
}

func TestApple_Validate_NonceMismatch(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	tok := f.sign(t, "", freshClaims("apple-sub-6", AppleAudience, AppleIssuer, "server-nonce", time.Now().Add(5*time.Minute)))
	_, err := v.Validate(context.Background(), tok, "client-nonce")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "nonce mismatch") {
		t.Errorf("want 'nonce mismatch', got %v", err)
	}
}

func TestApple_Validate_SkipsNonceWhenClientEmpty(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	// Token has a nonce; client passed "" — we skip nonce check.
	tok := f.sign(t, "", freshClaims("apple-sub-7", AppleAudience, AppleIssuer, "ignored", time.Now().Add(5*time.Minute)))
	if _, err := v.Validate(context.Background(), tok, ""); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
}

func TestApple_Validate_UnknownKid(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	tok := f.sign(t, "no-such-kid", freshClaims("apple-sub-8", AppleAudience, AppleIssuer, "", time.Now().Add(5*time.Minute)))
	_, err := v.Validate(context.Background(), tok, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "kid") && !strings.Contains(err.Error(), "no key") {
		t.Errorf("expected kid/no-key error, got %v", err)
	}
}

func TestJWKSCache_CachesAcrossCalls(t *testing.T) {
	f := newFakeJWKSServer(t)
	cache := NewJWKSCache(f.url(), 24*time.Hour, http.DefaultClient)
	for i := 0; i < 5; i++ {
		if _, err := cache.GetKey(context.Background(), f.kid); err != nil {
			t.Fatalf("get key: %v", err)
		}
	}
	if got := f.hits.Load(); got != 1 {
		t.Errorf("expected 1 JWKS fetch (cached), got %d", got)
	}
}

func TestJWKSCache_RefreshOnCacheMiss(t *testing.T) {
	f := newFakeJWKSServer(t)
	cache := NewJWKSCache(f.url(), 24*time.Hour, http.DefaultClient)
	// First call fetches the doc.
	if _, err := cache.GetKey(context.Background(), f.kid); err != nil {
		t.Fatal(err)
	}
	// Now rotate the server-side keypair + kid. New token has a kid the
	// cache hasn't seen; GetKey should eagerly refresh.
	f.kid = "test-kid-2"
	_, err := cache.GetKey(context.Background(), "test-kid-2")
	if err != nil {
		t.Fatalf("expected refresh-on-miss to succeed, got %v", err)
	}
	if got := f.hits.Load(); got < 2 {
		t.Errorf("expected ≥2 fetches after rotation, got %d", got)
	}
}

func TestJWKSCache_StaleServeOnRefreshFailure(t *testing.T) {
	f := newFakeJWKSServer(t)
	cache := NewJWKSCache(f.url(), 1*time.Nanosecond, http.DefaultClient) // immediate expiry
	if _, err := cache.GetKey(context.Background(), f.kid); err != nil {
		t.Fatal(err)
	}
	// Make the server return 500s.
	f.respond = func(w http.ResponseWriter) { w.WriteHeader(500); _, _ = io.WriteString(w, "boom") }
	// We still have the kid cached locally → expect stale-but-live fallback.
	if _, err := cache.GetKey(context.Background(), f.kid); err != nil {
		t.Errorf("expected stale fallback, got %v", err)
	}
}

func TestHashAppleSub_Deterministic(t *testing.T) {
	salt := []byte("test-salt")
	a := hashAppleSub("apple-sub-xyz", salt)
	b := hashAppleSub("apple-sub-xyz", salt)
	if fmt.Sprintf("%x", a) != fmt.Sprintf("%x", b) {
		t.Fatal("hash must be deterministic")
	}
	if len(a) != 32 {
		t.Errorf("hash length: %d", len(a))
	}
	// Different sub → different hash.
	c := hashAppleSub("apple-sub-other", salt)
	if fmt.Sprintf("%x", a) == fmt.Sprintf("%x", c) {
		t.Fatal("different sub should produce different hash")
	}
	// Different salt → different hash.
	d := hashAppleSub("apple-sub-xyz", []byte("other-salt"))
	if fmt.Sprintf("%x", a) == fmt.Sprintf("%x", d) {
		t.Fatal("different salt should produce different hash")
	}
}
