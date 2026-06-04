// Package server — register-on-first-use decorator around the
// billing-svc APIKeyValidator (#690 D1).
//
// The mobile app generates its 16-digit account number CLIENT-SIDE
// (mobile/ios/src/lib/account.ts) under the Mullvad model (#569: the
// account number IS the credential) — but nothing ever registered it,
// so every fresh install's first connect 401'd (and, before #690 D2,
// silently). This decorator heals on touch: a Validate miss whose key
// LOOKS like a consumer account number triggers one
// RegisterConsumerAccount round-trip, then a single re-Validate.
// Non-consumer-shaped keys (iog_… workspace keys) pass through
// untouched, as do billing-svc outages (fail closed, same as the
// wrapped validator).
package server

import (
	"context"
	"net/http"
	"regexp"
	"time"

	"connectrpc.com/connect"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
)

var consumerNumberRe = regexp.MustCompile(`^[0-9]{16}$`)

// ConsumerRegisteringValidator decorates an APIKeyValidator with
// register-on-first-use for 16-digit consumer account numbers.
type ConsumerRegisteringValidator struct {
	inner  APIKeyValidator
	client billingv1connect.ApiKeyServiceClient
}

// NewConsumerRegisteringValidator wraps inner; baseURL dials billing-svc
// (same endpoint the BillingValidator speaks to). httpClient nil → 3s
// timeout default.
func NewConsumerRegisteringValidator(inner APIKeyValidator, baseURL string, httpClient connect.HTTPClient) *ConsumerRegisteringValidator {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 3 * time.Second}
	}
	return &ConsumerRegisteringValidator{
		inner:  inner,
		client: billingv1connect.NewApiKeyServiceClient(httpClient, baseURL),
	}
}

// Validate implements APIKeyValidator.
func (v *ConsumerRegisteringValidator) Validate(ctx context.Context, apiKey string) (string, string, string, error) {
	ws, cust, tier, err := v.inner.Validate(ctx, apiKey)
	if err == nil {
		return ws, cust, tier, nil
	}
	if !consumerNumberRe.MatchString(apiKey) {
		return "", "", "", err
	}
	// Register-on-first-use (idempotent server-side; a lost race still
	// resolves to the same identity).
	if _, rerr := v.client.RegisterConsumerAccount(ctx, connect.NewRequest(&billingv1.RegisterConsumerAccountRequest{
		AccountNumber: apiKey,
	})); rerr != nil {
		// Registration unavailable/declined → the original auth error
		// stands (fail closed; no partial trust).
		return "", "", "", err
	}
	return v.inner.Validate(ctx, apiKey)
}
