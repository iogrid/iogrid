// Package auth validates customer API keys against the billing-svc.
//
// The proxy-gateway accepts an API key on the customer-facing wire via:
//
//   - SOCKS5 RFC 1929 username/password sub-negotiation: username is the
//     workspace handle (free-form, e.g. "myco"), password is the API key.
//   - HTTP CONNECT Proxy-Authorization: Basic with the same shape.
//
// The single Validator interface is here so the rest of the proxy depends
// on a small contract that's trivially stubbed in tests. Implementations:
//
//   - Static — used for unit tests and local dev (no upstream calls).
//   - Connect — calls the (future) billing-svc.ApiKeyService.ValidateApiKey
//     RPC. When BILLING_SVC_URL is unset the Connect impl is not wired and
//     the binary falls back to Static seeded from env.
//
// The wire schema for ValidateApiKey is intentionally not in proto/ yet —
// billing-svc owns the source-of-truth definition. Until that lands, the
// proxy ships with an interface and an in-memory Static implementation
// so the SOCKS5/HTTP-CONNECT path is fully testable end-to-end.
package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"
)

// ErrInvalidKey is the canonical "this API key is bad / revoked" signal.
var ErrInvalidKey = errors.New("invalid api key")

// ErrSuspended is returned when the workspace exists but is paused
// (billing past_due, AUP violation, etc.).
var ErrSuspended = errors.New("workspace suspended")

// Customer is the resolved identity behind a validated API key.
type Customer struct {
	// WorkspaceID — the billing/quota anchor. UUID string.
	WorkspaceID string
	// CustomerID — convenience alias for the workspace owner; used as
	// the audit-log primary key.
	CustomerID string
	// Tier — "free" | "starter" | "growth" | "enterprise". Used by
	// antiabuse-svc to decide the per-customer rate-limit ceiling.
	Tier string
	// AllowedCategories — opt-in categories the customer's contract
	// permits. Filtered against the destination at dispatch time.
	AllowedCategories []string
	// GeoTarget — preferred provider region (e.g. "us-east", "eu-west").
	// Empty means "any".
	GeoTarget string
	// KYCVerified — set when manual KYC has been completed; gates
	// access to banking/government destinations.
	KYCVerified bool
	// ResolvedAt — when the validation last hit the upstream. The
	// proxy caches positive results for a short interval.
	ResolvedAt time.Time
}

// Validator authenticates an API key and returns the resolved Customer.
type Validator interface {
	Validate(ctx context.Context, apiKey string) (*Customer, error)
}

// Static is an in-memory Validator for tests and local dev.
type Static struct {
	mu      sync.RWMutex
	byKey   map[string]Customer
	hookErr error
}

// NewStatic seeds an in-memory validator. Entries can be added later
// via Set.
func NewStatic(entries map[string]Customer) *Static {
	s := &Static{byKey: make(map[string]Customer, len(entries))}
	for k, v := range entries {
		s.byKey[k] = v
	}
	return s
}

// Set adds or replaces a key/customer pair.
func (s *Static) Set(apiKey string, c Customer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byKey[apiKey] = c
}

// SetError forces every subsequent Validate to return err (test hook).
func (s *Static) SetError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hookErr = err
}

// Validate looks the key up. Empty key always returns ErrInvalidKey.
func (s *Static) Validate(_ context.Context, apiKey string) (*Customer, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, ErrInvalidKey
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.hookErr != nil {
		return nil, s.hookErr
	}
	c, ok := s.byKey[apiKey]
	if !ok {
		return nil, ErrInvalidKey
	}
	out := c
	out.ResolvedAt = time.Now()
	return &out, nil
}

// SplitUserPass decodes a SOCKS5 RFC 1929 user/pass pair OR an HTTP
// Basic credential into (workspaceHandle, apiKey). Either is accepted
// as the "key" — implementations can choose to honour the workspace
// handle as a sanity hint. Empty inputs return ("", "", false).
//
// The proxy treats the password slot as the authoritative API key; the
// username slot is propagated to audit logs as the customer-side
// workspace handle but never trusted alone.
func SplitUserPass(user, pass string) (workspaceHandle, apiKey string, ok bool) {
	user = strings.TrimSpace(user)
	pass = strings.TrimSpace(pass)
	if pass == "" && user == "" {
		return "", "", false
	}
	// Allow apiKey-in-username for clients that lack a dedicated
	// password slot (uncommon but matches some Bright Data SDKs).
	if pass == "" {
		return "", user, true
	}
	return user, pass, true
}
