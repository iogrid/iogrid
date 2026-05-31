// Package auth — Connect implementation backed by billing-svc.ValidateApiKey.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
)

const cacheTTL = 60 * time.Second

// Connect is a Validator backed by billing-svc's ValidateApiKey RPC.
// Positive results are cached locally for cacheTTL (60s) to keep the
// hot path off the upstream on every SOCKS5 USERPASS handshake.
type Connect struct {
	client billingv1connect.ApiKeyServiceClient

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	customer Customer
	expiry   time.Time
}

// NewConnect creates a Connect Validator dialling billing-svc at baseURL.
// When httpClient is nil a default 3-second-timeout client is used.
func NewConnect(baseURL string, httpClient connect.HTTPClient) *Connect {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}
	return &Connect{
		client: billingv1connect.NewApiKeyServiceClient(httpClient, baseURL),
		cache:  make(map[string]cacheEntry),
	}
}

// Validate resolves the API key against billing-svc, with a 60s local
// cache on positive results. Unknown or revoked keys return ErrInvalidKey;
// suspended workspaces return ErrSuspended. Any RPC error is fail-closed
// (ErrInvalidKey) so a transient billing-svc outage never lets traffic
// through on an unverified key.
func (c *Connect) Validate(ctx context.Context, apiKey string) (*Customer, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, ErrInvalidKey
	}

	if cust, ok := c.lookup(apiKey); ok {
		return cust, nil
	}

	resp, err := c.client.ValidateApiKey(ctx, connect.NewRequest(&billingv1.ValidateApiKeyRequest{
		ApiKey: apiKey,
	}))
	if err != nil {
		return nil, fmt.Errorf("%w: billing-svc RPC: %w", ErrInvalidKey, err)
	}
	r := resp.Msg

	if !r.GetValid() {
		return nil, ErrInvalidKey
	}
	if r.GetSuspended() {
		return nil, ErrSuspended
	}

	cust := Customer{
		WorkspaceID:       uuidValue(r.GetWorkspaceId()),
		CustomerID:        uuidValue(r.GetCustomerId()),
		Tier:              tierString(r.GetTier()),
		AllowedCategories: r.GetAllowedCategories(),
		GeoTarget:         r.GetGeoTarget(),
		KYCVerified:       r.GetKycVerified(),
		ResolvedAt:        time.Now(),
	}
	c.store(apiKey, cust)
	return &cust, nil
}

func (c *Connect) lookup(apiKey string) (*Customer, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[apiKey]
	if !ok || time.Now().After(e.expiry) {
		delete(c.cache, apiKey)
		return nil, false
	}
	out := e.customer
	return &out, true
}

func (c *Connect) store(apiKey string, cust Customer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[apiKey] = cacheEntry{customer: cust, expiry: time.Now().Add(cacheTTL)}
}

// uuidValue safely dereferences a *commonv1.UUID, returning "" for nil.
func uuidValue(u interface{ GetValue() string }) string {
	if u == nil {
		return ""
	}
	return u.GetValue()
}

// tierString maps a SubscriptionTier proto enum to the lowercase label
// the rest of proxy-gateway uses in audit logs and rate-limit lookups.
func tierString(t billingv1.SubscriptionTier) string {
	switch t {
	case billingv1.SubscriptionTier_SUBSCRIPTION_TIER_PAYG:
		return "payg"
	case billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER:
		return "starter"
	case billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH:
		return "growth"
	case billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE:
		return "enterprise"
	default:
		return "free"
	}
}
