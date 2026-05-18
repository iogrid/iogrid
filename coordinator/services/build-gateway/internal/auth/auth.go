// Package auth implements API-key extraction and validation for the
// build-gateway.
//
// The wire format mirrors the proxy-gateway: customers send
//
//	Authorization: Bearer <api_key>
//
// or, for tooling that can't customize the Authorization header (Xcode
// fastlane plugins, occasionally),
//
//	X-Iogrid-Api-Key: <api_key>
//
// Either is accepted; we never look at both.
//
// Validation talks to billing-svc (the canonical owner of customer API
// keys per docs/TECH.md). The Validator interface is the seam — production
// wires a Connect-Go client to billing-svc.ValidateApiKey, tests wire the
// in-memory StaticValidator.
package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Identity is the result of a successful API-key validation. The HTTP
// handler stashes this on the request context so downstream code can scope
// every action to the customer's workspace.
type Identity struct {
	// WorkspaceID is the canonical UUID the build belongs to. Required.
	WorkspaceID string
	// UserID is the user that issued the key. May be empty for
	// service-tier keys.
	UserID string
	// Plan is the customer's subscription tier ("free", "pro",
	// "enterprise"). Webhook delivery and longer artifact retention are
	// gated on plan.
	Plan string
}

// ErrInvalidKey indicates the credential was malformed, expired, or
// unknown. We deliberately never differentiate to the caller — every
// failure looks like 401, no oracle.
var ErrInvalidKey = errors.New("invalid api key")

// Validator resolves a raw API key to an Identity.
type Validator interface {
	// Validate is called once per request. Implementations SHOULD cache
	// validations for a short window (a few seconds) to absorb bursts of
	// the same key without hammering billing-svc.
	Validate(ctx context.Context, apiKey string) (Identity, error)
}

// StaticValidator is a Validator backed by a process-local map. Useful in
// tests and for the early stub deployment (no billing-svc dependency yet).
type StaticValidator struct {
	mu   sync.RWMutex
	keys map[string]Identity
}

// NewStaticValidator returns an empty StaticValidator.
func NewStaticValidator() *StaticValidator {
	return &StaticValidator{keys: make(map[string]Identity)}
}

// Add registers a single key. Safe to call concurrently with Validate.
func (v *StaticValidator) Add(key string, id Identity) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.keys[key] = id
}

// Validate implements Validator.
func (v *StaticValidator) Validate(_ context.Context, key string) (Identity, error) {
	if key == "" {
		return Identity{}, ErrInvalidKey
	}
	v.mu.RLock()
	defer v.mu.RUnlock()
	id, ok := v.keys[key]
	if !ok {
		return Identity{}, ErrInvalidKey
	}
	return id, nil
}

// CachingValidator wraps another Validator with a tiny TTL cache so a burst
// of requests reusing the same key only hits the backing validator once.
//
// The cache is intentionally short (default 5s) — long enough to deflect
// bursts, short enough that key revocations land within seconds.
type CachingValidator struct {
	inner Validator
	ttl   time.Duration
	now   func() time.Time

	mu    sync.RWMutex
	cache map[string]cachedID
}

type cachedID struct {
	id      Identity
	expires time.Time
}

// NewCachingValidator wraps inner with a TTL cache. ttl <= 0 falls back to
// 5 seconds.
func NewCachingValidator(inner Validator, ttl time.Duration) *CachingValidator {
	if ttl <= 0 {
		ttl = 5 * time.Second
	}
	return &CachingValidator{
		inner: inner,
		ttl:   ttl,
		now:   time.Now,
		cache: make(map[string]cachedID),
	}
}

// Validate implements Validator.
func (v *CachingValidator) Validate(ctx context.Context, key string) (Identity, error) {
	v.mu.RLock()
	if entry, ok := v.cache[key]; ok && entry.expires.After(v.now()) {
		v.mu.RUnlock()
		return entry.id, nil
	}
	v.mu.RUnlock()

	id, err := v.inner.Validate(ctx, key)
	if err != nil {
		return Identity{}, err
	}
	v.mu.Lock()
	v.cache[key] = cachedID{id: id, expires: v.now().Add(v.ttl)}
	v.mu.Unlock()
	return id, nil
}

// ctxKey is the unexported type used to stash Identity on request context.
type ctxKey struct{}

// WithIdentity returns a derived context carrying id.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// IdentityFrom retrieves the Identity stashed by Middleware. The boolean
// is false when no identity has been set (the request is unauthenticated).
func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

// ExtractAPIKey reads the customer-supplied API key off either the
// Authorization Bearer header or X-Iogrid-Api-Key. Returns "" when neither
// is present.
func ExtractAPIKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			return strings.TrimSpace(h[len("bearer "):])
		}
	}
	if h := r.Header.Get("X-Iogrid-Api-Key"); h != "" {
		return strings.TrimSpace(h)
	}
	return ""
}

// Middleware wraps next so every request must carry a valid API key.
// Successful requests get Identity stashed on the context; failures return
// 401 with a stable JSON envelope.
//
// The /internal/* subtree (provider artifact upload) is exempted — those
// paths use a different credential (dispatch JWT) which is validated
// elsewhere.
func Middleware(v Validator, writeErr func(http.ResponseWriter, int, string, string)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := ExtractAPIKey(r)
			if key == "" {
				writeErr(w, http.StatusUnauthorized, "missing_api_key", "Authorization header or X-Iogrid-Api-Key required")
				return
			}
			id, err := v.Validate(r.Context(), key)
			if err != nil {
				writeErr(w, http.StatusUnauthorized, "invalid_api_key", "API key rejected")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithIdentity(r.Context(), id)))
		})
	}
}
