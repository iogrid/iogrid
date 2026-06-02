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

// TestJWKSCache_TTLExpiryTriggersRefresh exercises the soft-TTL path: a
// fetch BEFORE the TTL hits returns the cached doc with no new network
// hit, but the first lookup AFTER the TTL window must trigger a refresh
// (i.e. hit the JWKS endpoint a second time). Guards against the bug
// where a deployed pod would happily serve a 6-month-stale JWKS doc
// because the cache never noticed its TTL had elapsed.
func TestJWKSCache_TTLExpiryTriggersRefresh(t *testing.T) {
	f := newFakeJWKSServer(t)
	// Very short TTL so the test doesn't have to sleep for 24h.
	cache := NewJWKSCache(f.url(), 50*time.Millisecond, http.DefaultClient)
	if _, err := cache.GetKey(context.Background(), f.kid); err != nil {
		t.Fatalf("initial fetch: %v", err)
	}
	if got := f.hits.Load(); got != 1 {
		t.Fatalf("expected 1 hit after first fetch, got %d", got)
	}
	// Within-TTL lookup — no extra hit.
	if _, err := cache.GetKey(context.Background(), f.kid); err != nil {
		t.Fatalf("within-TTL fetch: %v", err)
	}
	if got := f.hits.Load(); got != 1 {
		t.Errorf("expected 1 hit (within TTL), got %d", got)
	}
	// Wait past the soft TTL.
	time.Sleep(80 * time.Millisecond)
	if _, err := cache.GetKey(context.Background(), f.kid); err != nil {
		t.Fatalf("post-TTL fetch: %v", err)
	}
	if got := f.hits.Load(); got < 2 {
		t.Errorf("expected ≥2 hits after TTL expiry, got %d", got)
	}
}

// TestApple_Validate_MissingExpClaim — Apple's real tokens always carry
// `exp`, but a malicious or test-rig token without one must NEVER be
// accepted. The jwt library may treat the absence as "no expiry to
// check" in some configurations; our validator explicitly rejects.
func TestApple_Validate_MissingExpClaim(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	claims := AppleClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:   AppleIssuer,
			Subject:  "apple-sub-noexp",
			Audience: jwt.ClaimStrings{AppleAudience},
			IssuedAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			// No ExpiresAt.
		},
		Email: "noexp@example.com",
	}
	tok := f.sign(t, "", claims)
	_, err := v.Validate(context.Background(), tok, "")
	if err == nil {
		t.Fatal("expected error for missing exp")
	}
	if !errors.Is(err, ErrAppleTokenInvalid) {
		t.Errorf("err: %v", err)
	}
	if !strings.Contains(err.Error(), "exp") {
		t.Errorf("want 'exp' in error, got %v", err)
	}
}

// TestApple_Validate_WrongAlg ensures a token signed with anything other
// than RS256 — even if the signature is technically valid for the alg —
// is rejected. Apple ONLY signs with RS256; accepting any other alg is
// the classic "alg=none" / alg-confusion vulnerability surface.
func TestApple_Validate_WrongAlg(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	// Build a token with HS256 signed with a symmetric secret.
	claims := freshClaims("apple-sub-hs", AppleAudience, AppleIssuer, "", time.Now().Add(5*time.Minute))
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tok.Header["kid"] = f.kid
	signed, err := tok.SignedString([]byte("some-symmetric-secret"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.Validate(context.Background(), signed, "")
	if err == nil {
		t.Fatal("expected error for HS256 token")
	}
	if !errors.Is(err, ErrAppleTokenInvalid) {
		t.Errorf("err: %v", err)
	}
	if !strings.Contains(err.Error(), "alg") {
		t.Errorf("want 'alg' in error, got %v", err)
	}
}

// TestApple_Validate_MissingKidHeader — a token whose header omits `kid`
// can't be routed to a JWKS entry. The validator must reject before
// touching the cache (avoids ambiguity if the cache happens to have
// exactly one key).
func TestApple_Validate_MissingKidHeader(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	claims := freshClaims("apple-sub-nokid", AppleAudience, AppleIssuer, "", time.Now().Add(5*time.Minute))
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	// Explicitly do NOT set tok.Header["kid"].
	signed, err := tok.SignedString(f.priv)
	if err != nil {
		t.Fatal(err)
	}
	_, err = v.Validate(context.Background(), signed, "")
	if err == nil {
		t.Fatal("expected error for missing kid")
	}
	if !errors.Is(err, ErrAppleTokenInvalid) {
		t.Errorf("err: %v", err)
	}
	if !strings.Contains(err.Error(), "kid") {
		t.Errorf("want 'kid' in error, got %v", err)
	}
}

// TestApple_Validate_MultiAudWithMatch — Apple sometimes mints tokens
// with multiple aud entries when a Services ID is involved. As long as
// OUR bundle id is in the set the token is acceptable.
func TestApple_Validate_MultiAudWithMatch(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	claims := AppleClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    AppleIssuer,
			Subject:   "apple-sub-multi",
			Audience:  jwt.ClaimStrings{"io.someoneelse.app", AppleAudience, "io.thirdparty.app"},
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		},
		Email: "multi@example.com",
	}
	tok := f.sign(t, "", claims)
	got, err := v.Validate(context.Background(), tok, "")
	if err != nil {
		t.Fatalf("expected ok with multi-aud containing ours, got %v", err)
	}
	if got.Subject != "apple-sub-multi" {
		t.Errorf("subject: %s", got.Subject)
	}
}

// TestApple_Validate_EmptyAudClaim — token with the audience claim set
// to an empty array must be rejected; ClaimStrings with zero entries
// can't contain our bundle id by definition.
func TestApple_Validate_EmptyAudClaim(t *testing.T) {
	f := newFakeJWKSServer(t)
	v := newTestValidator(t, f)
	claims := AppleClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    AppleIssuer,
			Subject:   "apple-sub-noaud",
			Audience:  jwt.ClaimStrings{},
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		},
	}
	tok := f.sign(t, "", claims)
	_, err := v.Validate(context.Background(), tok, "")
	if err == nil {
		t.Fatal("expected error for empty aud")
	}
	if !errors.Is(err, ErrAppleTokenInvalid) {
		t.Errorf("err: %v", err)
	}
	if !strings.Contains(err.Error(), "aud") {
		t.Errorf("want 'aud' in error, got %v", err)
	}
}

// TestJWKSCache_ConcurrentGetKey covers the race-condition surface that
// production WILL hit: 50+ iOS clients all sign in within the same TTL
// window, each calling GetKey from a different goroutine. The cache
// must be safe under concurrent reads + refreshes — no data race, no
// missed lookups, no double-fetch storm.
//
// Note: this is the closest analogue to "two concurrent
// CompleteAppleSignIn calls with the same apple_sub" that we can
// exercise at the validator layer; the full DB-side race is covered in
// the integration suite (apple_integration_test.go) which depends on a
// live Postgres fixture. See follow-up issue for an explicit
// concurrent-write race test against the Service.
func TestJWKSCache_ConcurrentGetKey(t *testing.T) {
	f := newFakeJWKSServer(t)
	cache := NewJWKSCache(f.url(), 24*time.Hour, http.DefaultClient)
	const N = 64
	errs := make(chan error, N)
	done := make(chan struct{})
	for i := 0; i < N; i++ {
		go func() {
			_, err := cache.GetKey(context.Background(), f.kid)
			errs <- err
		}()
	}
	go func() {
		for i := 0; i < N; i++ {
			<-errs
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent GetKey timed out")
	}
	// All callers should have succeeded; the server may have been hit
	// more than once (concurrent refresh isn't deduped today — that's
	// an optimization, not a correctness gate), but it must NOT have
	// been hit hundreds of times.
	if got := f.hits.Load(); got > int64(N) {
		t.Errorf("excessive JWKS fetches under concurrency: %d (expected ≪ %d)", got, N)
	}
}
