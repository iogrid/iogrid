// Package server — billing-svc-backed APIKeyValidator for vpn-svc.
//
// Reuses the same ValidateApiKey Connect-RPC contract that proxy-gateway
// already speaks (see coordinator/services/proxy-gateway/internal/auth/
// billing.go). vpn-svc keeps a 60-second positive cache so the hot path
// (every POST /v1/vpn/sessions) doesn't round-trip to billing-svc.
//
// Refs #531.
package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"connectrpc.com/connect"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
)

const billingCacheTTL = 60 * time.Second

// errInvalidKey is returned for any non-valid key (revoked, unknown, expired,
// or RPC failure — we fail closed so a transient billing-svc outage never
// lets traffic through unverified). Don't differentiate to the caller —
// log on the server side; client just sees 401.
var errInvalidKey = errors.New("invalid api key")

// BillingValidator authenticates raw API keys against billing-svc.ValidateApiKey.
type BillingValidator struct {
	client billingv1connect.ApiKeyServiceClient

	mu    sync.Mutex
	cache map[string]billingCacheEntry
}

type billingCacheEntry struct {
	workspaceID string
	customerID  string
	tier        string
	expiry      time.Time
}

// NewBillingValidator dials billing-svc at baseURL (e.g. http://billing-svc.iogrid.svc.cluster.local:8080).
// httpClient nil → 3s timeout default.
func NewBillingValidator(baseURL string, httpClient connect.HTTPClient) *BillingValidator {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}
	return &BillingValidator{
		client: billingv1connect.NewApiKeyServiceClient(httpClient, baseURL),
		cache:  make(map[string]billingCacheEntry),
	}
}

// Validate implements APIKeyValidator. Returns the resolved
// workspace ID, customer ID, and subscription tier string (matches
// the proto enum string form — e.g. "SUBSCRIPTION_TIER_STARTER").
// vpn-svc uses tier for free-tier quota enforcement (#548).
func (v *BillingValidator) Validate(ctx context.Context, apiKey string) (string, string, string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", "", "", errInvalidKey
	}

	if ws, cust, tier, ok := v.lookup(apiKey); ok {
		return ws, cust, tier, nil
	}

	resp, err := v.client.ValidateApiKey(ctx, connect.NewRequest(&billingv1.ValidateApiKeyRequest{
		ApiKey: apiKey,
	}))
	if err != nil {
		return "", "", "", fmt.Errorf("%w: %v", errInvalidKey, err)
	}
	r := resp.Msg
	if !r.GetValid() || r.GetSuspended() {
		return "", "", "", errInvalidKey
	}

	wsID := ""
	if u := r.GetWorkspaceId(); u != nil {
		wsID = u.GetValue()
	}
	custID := ""
	if u := r.GetCustomerId(); u != nil {
		custID = u.GetValue()
	}
	tier := r.GetTier().String()
	v.store(apiKey, wsID, custID, tier)
	return wsID, custID, tier, nil
}

func (v *BillingValidator) lookup(apiKey string) (string, string, string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	e, ok := v.cache[apiKey]
	if !ok || time.Now().After(e.expiry) {
		delete(v.cache, apiKey)
		return "", "", "", false
	}
	return e.workspaceID, e.customerID, e.tier, true
}

func (v *BillingValidator) store(apiKey, wsID, custID, tier string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cache[apiKey] = billingCacheEntry{
		workspaceID: wsID,
		customerID:  custID,
		tier:        tier,
		expiry:      time.Now().Add(billingCacheTTL),
	}
}
