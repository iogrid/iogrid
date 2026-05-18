// Package auth provides the JWT validation middleware for gateway-bff.
//
// The middleware extracts a Bearer token from the Authorization header,
// validates it against the RSA public key(s) fetched from identity-svc's
// JWKS endpoint, and stuffs the parsed claims into the request context.
//
// Handlers downstream of the middleware retrieve the active user via
// FromContext(). Routes that should reject unauthenticated callers wrap
// themselves in RequireAuth; routes that gate by role wrap in RequireRole.
//
// Key handling: by design we cache the JWKS set in memory and refresh it
// every JWKSRefreshInterval. The middleware is robust to JWKS being
// momentarily unreachable — it serves the last known good key set until
// it is replaced.
package auth

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// contextKey is unexported so callers must use FromContext().
type contextKey struct{}

// Claims is the structured payload we extract from each access token.
// Mirrors identity-svc's tokens.AccessClaims so the wire-format is
// uniform across services.
type Claims struct {
	jwt.RegisteredClaims
	Identifiers  []string `json:"identifiers,omitempty"`
	PrimaryEmail string   `json:"primary_email,omitempty"`
	Roles        []string `json:"roles,omitempty"`
	StepUp       bool     `json:"step_up,omitempty"`
}

// UserID returns the parsed user UUID. Returns the nil UUID if missing
// or malformed.
func (c *Claims) UserID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	id, err := uuid.Parse(c.Subject)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// SessionID returns the JWT `jti` claim which we use to carry the
// session id for revocation lookups.
func (c *Claims) SessionID() string {
	if c == nil {
		return ""
	}
	return c.ID
}

// HasRole reports whether the active token includes the given role.
// Roles are case-sensitive and match the proto UserRole enum names
// (e.g. "USER_ROLE_ADMIN"). Aliases ("admin", "staff", "provider",
// "customer") are also accepted to remain forward-compatible with
// short identity-svc claims.
func (c *Claims) HasRole(role string) bool {
	if c == nil {
		return false
	}
	role = strings.ToUpper(role)
	for _, r := range c.Roles {
		if strings.ToUpper(r) == role {
			return true
		}
		// Identity-svc may shorten "USER_ROLE_ADMIN" → "ADMIN".
		short := strings.TrimPrefix(strings.ToUpper(r), "USER_ROLE_")
		if short == strings.TrimPrefix(role, "USER_ROLE_") {
			return true
		}
	}
	return false
}

// IsAdmin is the common shorthand the /admin/* routes use.
func (c *Claims) IsAdmin() bool { return c.HasRole("ADMIN") || c.HasRole("USER_ROLE_ADMIN") }

// FromContext fetches the Claims wired by Middleware. Returns (nil, false)
// when the caller is unauthenticated.
func FromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(contextKey{}).(*Claims)
	return c, ok
}

// withClaims is the internal context-wrap helper.
func withClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, contextKey{}, c)
}

// NewContextForTesting wires Claims into ctx as if Middleware had run.
// Exposed for in-tree test fixtures only — production code MUST go
// through Middleware so the JWT signature is actually validated.
func NewContextForTesting(ctx context.Context, c *Claims) context.Context {
	return withClaims(ctx, c)
}

// --- key set --------------------------------------------------------------

// KeyResolver returns the verification key for a given JWT `kid` header.
// Implementations are expected to cache and refresh as appropriate.
type KeyResolver interface {
	Resolve(kid string) (*rsa.PublicKey, error)
}

// StaticKeyResolver always returns the same key regardless of kid. Used
// in tests + in environments where identity-svc has a single signing key
// mounted from a sealed Secret.
type StaticKeyResolver struct {
	Key *rsa.PublicKey
}

// Resolve implements KeyResolver.
func (s *StaticKeyResolver) Resolve(_ string) (*rsa.PublicKey, error) {
	if s == nil || s.Key == nil {
		return nil, errors.New("static key resolver: nil key")
	}
	return s.Key, nil
}

// JWKSResolver fetches & caches a JWK set from a remote URL. It re-polls
// on the configured interval and on a miss for an unknown kid (so newly
// rotated keys are picked up without waiting a full TTL).
type JWKSResolver struct {
	URL             string
	HTTPClient      *http.Client
	RefreshInterval time.Duration
	Logger          *slog.Logger

	mu      sync.RWMutex
	keys    map[string]*rsa.PublicKey
	lastFetch time.Time
}

// NewJWKSResolver constructs a JWKSResolver. Logger is optional; defaults
// to slog.Default(). HTTPClient defaults to a 5s-timeout client.
func NewJWKSResolver(url string, refresh time.Duration, hc *http.Client, logger *slog.Logger) *JWKSResolver {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Second}
	}
	if logger == nil {
		logger = slog.Default()
	}
	if refresh <= 0 {
		refresh = 15 * time.Minute
	}
	return &JWKSResolver{
		URL:             url,
		HTTPClient:      hc,
		RefreshInterval: refresh,
		Logger:          logger,
		keys:            map[string]*rsa.PublicKey{},
	}
}

// jwk is a single key entry in a JSON Web Key Set per RFC 7517.
type jwk struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	// Some servers serve PEM directly under a non-standard key for
	// easier debugging. We accept it as a fallback.
	X5c []string `json:"x5c,omitempty"`
	Pem string   `json:"pem,omitempty"`
}

type jwks struct {
	Keys []jwk `json:"keys"`
}

// Resolve implements KeyResolver. Returns the cached key if present, or
// re-fetches the JWKS to pick up new rotations.
func (j *JWKSResolver) Resolve(kid string) (*rsa.PublicKey, error) {
	j.mu.RLock()
	k, ok := j.keys[kid]
	stale := time.Since(j.lastFetch) > j.RefreshInterval
	j.mu.RUnlock()
	if ok && !stale {
		return k, nil
	}
	if err := j.Refresh(); err != nil {
		// If we can't refresh but we DO have a cached key, use it. This
		// keeps the gateway serving traffic during a momentary
		// identity-svc outage rather than failing every JWT.
		if ok {
			return k, nil
		}
		return nil, fmt.Errorf("jwks resolve: %w", err)
	}
	j.mu.RLock()
	defer j.mu.RUnlock()
	if got, ok := j.keys[kid]; ok {
		return got, nil
	}
	// As a last resort: if the kid is empty and we have exactly one key,
	// return that. Single-key deployments don't bother stamping kid.
	if kid == "" && len(j.keys) == 1 {
		for _, v := range j.keys {
			return v, nil
		}
	}
	return nil, fmt.Errorf("jwks resolve: unknown kid %q", kid)
}

// Refresh pulls the JWKS endpoint and replaces the cached key set.
func (j *JWKSResolver) Refresh() error {
	if j.URL == "" {
		return errors.New("jwks: empty URL")
	}
	req, err := http.NewRequest(http.MethodGet, j.URL, nil)
	if err != nil {
		return err
	}
	resp, err := j.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks: status %d", resp.StatusCode)
	}
	var ks jwks
	if err := json.NewDecoder(resp.Body).Decode(&ks); err != nil {
		return fmt.Errorf("jwks decode: %w", err)
	}
	out := make(map[string]*rsa.PublicKey, len(ks.Keys))
	for _, k := range ks.Keys {
		pub, err := jwkToRSA(k)
		if err != nil {
			j.Logger.Warn("jwks: skip key", slog.String("kid", k.Kid), slog.String("error", err.Error()))
			continue
		}
		out[k.Kid] = pub
	}
	if len(out) == 0 {
		return errors.New("jwks: empty key set")
	}
	j.mu.Lock()
	j.keys = out
	j.lastFetch = time.Now()
	j.mu.Unlock()
	return nil
}

// jwkToRSA converts a JWK entry to an *rsa.PublicKey. Supports both
// modulus+exponent (RFC 7518 §6.3.1) and x5c (the first cert) shapes.
// As a non-standard convenience we also accept a "pem" field with a
// raw PEM-encoded public key — identity-svc emits this for easier ops.
func jwkToRSA(k jwk) (*rsa.PublicKey, error) {
	if k.Pem != "" {
		block, _ := pem.Decode([]byte(k.Pem))
		if block == nil {
			return nil, errors.New("invalid PEM block")
		}
		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse PKIX: %w", err)
		}
		rsapub, ok := pub.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("not an RSA public key")
		}
		return rsapub, nil
	}
	if k.Kty != "RSA" {
		return nil, fmt.Errorf("unsupported kty %q", k.Kty)
	}
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		// Some implementations pad — fall through to std encoding.
		nBytes, err = base64.URLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, fmt.Errorf("decode n: %w", err)
		}
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		eBytes, err = base64.URLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, fmt.Errorf("decode e: %w", err)
		}
	}
	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 | int(b)
	}
	if e == 0 {
		return nil, errors.New("zero exponent")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}

// --- middleware -----------------------------------------------------------

// Verifier is the subset of behaviour the middleware needs. Splitting
// this from the resolver lets tests inject a stub directly.
type Verifier interface {
	Verify(token string) (*Claims, error)
}

// JWTVerifier combines a KeyResolver with issuer/audience checks.
type JWTVerifier struct {
	Resolver KeyResolver
	Issuer   string
	Audience string
}

// Verify implements Verifier.
func (v *JWTVerifier) Verify(token string) (*Claims, error) {
	c := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, c, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, fmt.Errorf("unexpected alg %q", t.Method.Alg())
		}
		kid, _ := t.Header["kid"].(string)
		return v.Resolver.Resolve(kid)
	}, jwt.WithIssuer(v.Issuer))
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("token invalid")
	}
	if v.Audience != "" {
		ok := false
		for _, a := range c.Audience {
			if a == v.Audience {
				ok = true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("token audience missing %q", v.Audience)
		}
	}
	return c, nil
}

// Middleware parses Bearer tokens and (when valid) stuffs the claims
// into the request context. It does NOT reject anonymous requests on
// its own — that's RequireAuth's job. This split lets routes opt into
// optional auth (e.g. /api/v1/me returns 401 explicitly with an error
// envelope; rate limiters can still see the IP).
func Middleware(v Verifier, logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tok := bearer(r)
			if tok == "" {
				next.ServeHTTP(w, r)
				return
			}
			claims, err := v.Verify(tok)
			if err != nil {
				logger.Debug("jwt rejected", slog.String("error", err.Error()))
				next.ServeHTTP(w, r)
				return
			}
			next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), claims)))
		})
	}
}

// RequireAuth rejects requests that do not have a valid Claims in
// context. Apply it after Middleware on routes that must be
// authenticated.
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			writeAuthErr(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole composes RequireAuth and a role check. Used to gate the
// /admin/* routes.
func RequireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := FromContext(r.Context())
			if !ok {
				writeAuthErr(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
				return
			}
			if !c.HasRole(role) {
				writeAuthErr(w, http.StatusForbidden, "forbidden", "missing required role "+role)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// bearer extracts the token portion of "Bearer <token>". Returns "" if
// the header is missing or malformed.
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// writeAuthErr writes the canonical auth-failure envelope.
func writeAuthErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"code":"` + code + `","message":"` + msg + `"}`))
}
