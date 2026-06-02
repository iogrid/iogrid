// Package auth — Apple JWKS cache.
//
// Apple publishes its OAuth/OIDC public keys at
// https://appleid.apple.com/auth/keys. The set is small (3-5 keys at any
// given time), rotates infrequently (~months), and is the linchpin we
// use to verify every Apple ID token submitted by the mobile app.
//
// Cache strategy:
//
//   - TTL-based with a 24h soft refresh: any request after the soft
//     expiry triggers a refresh, but if the refresh fails we keep
//     serving the cached set rather than reject sign-ins. Apple
//     rotates keys overlap-style so a brief network blip never strands
//     valid tokens.
//   - On a `kid` cache miss the cache eagerly refreshes once before
//     reporting "key not found" — this handles the case where Apple
//     publishes a new key id and starts signing tokens with it
//     immediately, ahead of our TTL.
//   - The transport is `http.Client` with a 10s timeout; tests inject
//     a fake `Doer` that serves canned key sets so the validator suite
//     never hits the network.
package auth

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"
)

// AppleJWKSURL is the canonical URL for Apple's OIDC JWKS document.
// Overridable in tests by constructing JWKSCache with a custom URL.
const AppleJWKSURL = "https://appleid.apple.com/auth/keys"

// DefaultJWKSCacheTTL is how long a fetched JWKS document is considered
// fresh before the next access triggers a background refresh.
const DefaultJWKSCacheTTL = 24 * time.Hour

// Doer is the minimal subset of *http.Client we need so tests can
// swap in a fake without taking a transport dependency.
type Doer interface {
	Do(req *http.Request) (*http.Response, error)
}

// JWKSCache caches a JWKS document keyed by `kid`. It's safe for
// concurrent use; lookups take an RLock, refreshes upgrade to a write
// lock for the swap.
type JWKSCache struct {
	URL    string
	TTL    time.Duration
	Client Doer

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey
	expiresAt time.Time
}

// NewJWKSCache constructs a cache against the given URL with the given
// TTL. Passing a zero TTL defaults to 24h; passing a nil client uses
// http.DefaultClient with a 10s per-request timeout.
func NewJWKSCache(url string, ttl time.Duration, client Doer) *JWKSCache {
	if ttl <= 0 {
		ttl = DefaultJWKSCacheTTL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &JWKSCache{URL: url, TTL: ttl, Client: client}
}

// GetKey returns the RSA public key for the given kid. On a miss it
// refreshes the JWKS once and retries. Caller must treat a non-nil
// error as "verification failed" — never serve a token whose kid we
// can't resolve, even if a stale cache is present.
func (c *JWKSCache) GetKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	if k := c.lookup(kid); k != nil && !c.expired() {
		return k, nil
	}
	if err := c.refresh(ctx); err != nil {
		// Stale-but-present fallback: if we already had a key for the
		// kid before the refresh, keep using it. A flaky Apple call
		// must not lock every iOS user out.
		if k := c.lookup(kid); k != nil {
			return k, nil
		}
		return nil, fmt.Errorf("apple jwks refresh: %w", err)
	}
	if k := c.lookup(kid); k != nil {
		return k, nil
	}
	return nil, fmt.Errorf("apple jwks: no key with kid=%q", kid)
}

func (c *JWKSCache) lookup(kid string) *rsa.PublicKey {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.keys[kid]
}

func (c *JWKSCache) expired() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().After(c.expiresAt)
}

// refresh fetches a fresh JWKS document and atomically swaps the cache.
// A failure here leaves the existing cache untouched.
func (c *JWKSCache) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return fmt.Errorf("build jwks request: %w", err)
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("fetch jwks: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MB cap
	if err != nil {
		return fmt.Errorf("read jwks body: %w", err)
	}
	doc := &jwksDoc{}
	if err := json.Unmarshal(body, doc); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kty != "RSA" {
			continue
		}
		pub, err := k.toRSA()
		if err != nil {
			// One bad key shouldn't poison the rest.
			continue
		}
		keys[k.Kid] = pub
	}
	c.mu.Lock()
	c.keys = keys
	c.expiresAt = time.Now().Add(c.TTL)
	c.mu.Unlock()
	return nil
}

// jwksDoc is the JSON shape Apple returns.
type jwksDoc struct {
	Keys []jwksKey `json:"keys"`
}

// jwksKey is one entry in the JWKS doc. Apple includes:
//
//	{ "kty":"RSA","kid":"...","use":"sig","alg":"RS256","n":"...","e":"AQAB" }
type jwksKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (k *jwksKey) toRSA() (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode N: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode E: %w", err)
	}
	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}
	if n.Sign() <= 0 || e <= 0 {
		return nil, fmt.Errorf("invalid RSA key components")
	}
	return &rsa.PublicKey{N: n, E: e}, nil
}
