package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

func newTestKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func signToken(t *testing.T, priv *rsa.PrivateKey, kid, issuer string, audience []string, claims jwt.MapClaims) string {
	t.Helper()
	if _, ok := claims["iss"]; !ok {
		claims["iss"] = issuer
	}
	if _, ok := claims["aud"]; !ok {
		claims["aud"] = audience
	}
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Minute).Unix()
	}
	if _, ok := claims["iat"]; !ok {
		claims["iat"] = time.Now().Unix()
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	if kid != "" {
		tok.Header["kid"] = kid
	}
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStaticResolverAndVerify(t *testing.T) {
	priv := newTestKey(t)
	v := &JWTVerifier{Resolver: &StaticKeyResolver{Key: &priv.PublicKey}, Issuer: "iogrid", Audience: "gateway-bff"}
	uid := uuid.NewString()
	tok := signToken(t, priv, "", "iogrid", []string{"gateway-bff"}, jwt.MapClaims{"sub": uid, "roles": []string{"ADMIN"}})
	c, err := v.Verify(tok)
	if err != nil {
		t.Fatalf("verify failed: %v", err)
	}
	if c.UserID().String() != uid {
		t.Fatalf("user id mismatch: %s vs %s", c.UserID(), uid)
	}
	if !c.IsAdmin() {
		t.Fatal("expected admin")
	}
}

func TestVerify_RejectsBadAudience(t *testing.T) {
	priv := newTestKey(t)
	v := &JWTVerifier{Resolver: &StaticKeyResolver{Key: &priv.PublicKey}, Issuer: "iogrid", Audience: "gateway-bff"}
	tok := signToken(t, priv, "", "iogrid", []string{"someone-else"}, jwt.MapClaims{"sub": uuid.NewString()})
	if _, err := v.Verify(tok); err == nil {
		t.Fatal("expected audience rejection")
	}
}

func TestVerify_RejectsBadIssuer(t *testing.T) {
	priv := newTestKey(t)
	v := &JWTVerifier{Resolver: &StaticKeyResolver{Key: &priv.PublicKey}, Issuer: "iogrid", Audience: "gateway-bff"}
	tok := signToken(t, priv, "", "evil", []string{"gateway-bff"}, jwt.MapClaims{"sub": uuid.NewString()})
	if _, err := v.Verify(tok); err == nil {
		t.Fatal("expected issuer rejection")
	}
}

func TestJWKSResolver_FetchesAndCaches(t *testing.T) {
	priv := newTestKey(t)
	der, _ := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})

	fetches := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{
				{"kid": "k1", "kty": "RSA", "pem": string(pemBlock)},
			},
		})
	}))
	defer srv.Close()

	resolver := NewJWKSResolver(srv.URL, time.Minute, nil, nil)
	got, err := resolver.Resolve("k1")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.N.Cmp(priv.PublicKey.N) != 0 {
		t.Fatal("key mismatch")
	}
	// Subsequent call should hit cache, not server.
	if _, err := resolver.Resolve("k1"); err != nil {
		t.Fatalf("cached resolve: %v", err)
	}
	if fetches != 1 {
		t.Fatalf("want 1 fetch, got %d", fetches)
	}
}

func TestMiddleware_OptionalAuth(t *testing.T) {
	priv := newTestKey(t)
	v := &JWTVerifier{Resolver: &StaticKeyResolver{Key: &priv.PublicKey}, Issuer: "iogrid", Audience: "gateway-bff"}
	mw := Middleware(v, nil)

	calls := 0
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if _, ok := FromContext(r.Context()); !ok {
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(200)
	}))

	// Without header: handler still runs, no claims.
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("anon should hit handler: got %d", w.Code)
	}

	// With valid bearer: claims present.
	tok := signToken(t, priv, "", "iogrid", []string{"gateway-bff"}, jwt.MapClaims{"sub": uuid.NewString()})
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("Authorization", "Bearer "+tok)
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != 200 {
		t.Fatalf("authed should hit handler with claims: got %d", w2.Code)
	}
	if calls != 2 {
		t.Fatalf("want 2 calls, got %d", calls)
	}
}

func TestRequireAuth_Rejects(t *testing.T) {
	handler := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestRequireRole_Forbids(t *testing.T) {
	handler := RequireRole("ADMIN")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	c := &Claims{Roles: []string{"CUSTOMER"}}
	c.Subject = uuid.NewString()
	r := httptest.NewRequest("GET", "/", nil).WithContext(withClaims(httptest.NewRequest("GET", "/", nil).Context(), c))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

func TestClaims_HasRole_ShortAndLongForm(t *testing.T) {
	c := &Claims{Roles: []string{"USER_ROLE_ADMIN"}}
	if !c.HasRole("ADMIN") {
		t.Fatal("short alias should match")
	}
	if !c.HasRole("USER_ROLE_ADMIN") {
		t.Fatal("long form should match")
	}
	if c.HasRole("CUSTOMER") {
		t.Fatal("unrelated role should not match")
	}
}

// TestMiddleware_ServiceTokenShim asserts the issue #237 BFF
// service-token + X-Iogrid-User-Id header bypass materialises Claims
// without going through the JWT verifier, and only when the env-var
// is set + the token matches exactly.
func TestMiddleware_ServiceTokenShim(t *testing.T) {
	priv := newTestKey(t)
	v := &JWTVerifier{Resolver: &StaticKeyResolver{Key: &priv.PublicKey}, Issuer: "iogrid", Audience: "gateway-bff"}
	mw := Middleware(v, nil)

	uid := uuid.NewString()
	var gotUID, gotRole string
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := FromContext(r.Context())
		if !ok {
			w.WriteHeader(204)
			return
		}
		gotUID = c.UserID().String()
		if c.IsAdmin() {
			gotRole = "ADMIN"
		}
		w.WriteHeader(200)
	}))

	// Env unset: shim is dormant. Service-token bearer is treated as a
	// (bad) JWT and the handler runs anon.
	t.Setenv(ServiceTokenEnv, "")
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer service-secret-xyz")
	r.Header.Set(ServiceUserIDHeader, uid)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("env-off: want 204 anon, got %d", w.Code)
	}

	// Env set, token matches, valid UUID + roles header: Claims appears.
	t.Setenv(ServiceTokenEnv, "service-secret-xyz")
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("Authorization", "Bearer service-secret-xyz")
	r2.Header.Set(ServiceUserIDHeader, uid)
	r2.Header.Set(ServiceUserRolesHeader, "ADMIN, USER")
	r2.Header.Set(ServiceUserEmailHeader, "ops@iogrid.org")
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, r2)
	if w2.Code != 200 {
		t.Fatalf("authed shim: want 200, got %d", w2.Code)
	}
	if gotUID != uid {
		t.Fatalf("uid mismatch: got %s, want %s", gotUID, uid)
	}
	if gotRole != "ADMIN" {
		t.Fatal("expected ADMIN role materialised from header")
	}

	// Env set, token MISMATCH: falls through to JWT verifier (which
	// fails) → anon. The service header must NOT short-circuit anything.
	r3 := httptest.NewRequest("GET", "/", nil)
	r3.Header.Set("Authorization", "Bearer wrong-token")
	r3.Header.Set(ServiceUserIDHeader, uid)
	w3 := httptest.NewRecorder()
	handler.ServeHTTP(w3, r3)
	if w3.Code != 204 {
		t.Fatalf("mismatched token: want 204 anon, got %d", w3.Code)
	}

	// Env set, token matches, but X-Iogrid-User-Id MISSING: fail closed
	// (anon). The shim must never authenticate without a user id.
	r4 := httptest.NewRequest("GET", "/", nil)
	r4.Header.Set("Authorization", "Bearer service-secret-xyz")
	w4 := httptest.NewRecorder()
	handler.ServeHTTP(w4, r4)
	if w4.Code != 204 {
		t.Fatalf("missing uid header: want 204 anon, got %d", w4.Code)
	}

	// Env set, token matches, but X-Iogrid-User-Id is malformed:
	// fail closed (anon) — never trust a non-UUID id.
	r5 := httptest.NewRequest("GET", "/", nil)
	r5.Header.Set("Authorization", "Bearer service-secret-xyz")
	r5.Header.Set(ServiceUserIDHeader, "not-a-uuid")
	w5 := httptest.NewRecorder()
	handler.ServeHTTP(w5, r5)
	if w5.Code != 204 {
		t.Fatalf("malformed uid: want 204 anon, got %d", w5.Code)
	}
}
