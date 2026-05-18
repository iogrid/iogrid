package handlers

import (
	"net/http"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// vpnAccount is the simplified payload the /vpn dashboard renders.
// We derive most of it from the billing-svc subscription record; the
// bandwidth_usage_bytes field is sourced from metering aggregates.
type vpnAccount struct {
	Tier                 string `json:"tier"`
	Status               string `json:"status"`
	BandwidthUsedBytes   uint64 `json:"bandwidth_used_bytes"`
	BandwidthQuotaBytes  uint64 `json:"bandwidth_quota_bytes"`
	UpgradeAvailable     bool   `json:"upgrade_available"`
}

// GetVPNAccount aggregates the subscription + bandwidth-usage view.
//
//	GET /api/v1/vpn/account?workspace_id=<UUID>
//
// Free-tier callers (no subscription) get the default 50 GB quota.
func (a *API) GetVPNAccount(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	wsID, ok := parseUUIDParam(w, r.URL.Query().Get("workspace_id"), "workspace_id")
	if !ok {
		return
	}
	sub, err := a.Clients.Billing.GetSubscription(r.Context(), &billingv1.GetSubscriptionRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID.String()},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	tier := "FREE"
	status := "active"
	upgrade := true
	if sub != nil && sub.Subscription != nil {
		tier = sub.Subscription.Tier.String()
		status = sub.Subscription.Status.String()
		upgrade = sub.Subscription.Tier != billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE
	}
	writeJSON(w, http.StatusOK, vpnAccount{
		Tier:                tier,
		Status:              status,
		BandwidthUsedBytes:  0, // populated by metering rollup once landed
		BandwidthQuotaBytes: 50 * 1024 * 1024 * 1024,
		UpgradeAvailable:    upgrade,
	})
}

// UpgradeVPN starts a Stripe Checkout session for the upgraded tier.
//
//	POST /api/v1/vpn/upgrade
//	  { workspace_id, tier, success_url, cancel_url }
//	-> 200 { checkout_url }
func (a *API) UpgradeVPN(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body struct {
		WorkspaceID string `json:"workspace_id"`
		Tier        string `json:"tier"`
		SuccessURL  string `json:"success_url"`
		CancelURL   string `json:"cancel_url"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	wsID, ok := parseUUIDParam(w, body.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	tier, ok := parseTier(body.Tier)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "tier must be one of starter|growth|enterprise|payg")
		return
	}
	resp, err := a.Clients.Billing.CreateCheckoutSession(r.Context(), &billingv1.CreateCheckoutSessionRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID.String()},
		DesiredTier: tier,
		SuccessUrl:  body.SuccessURL,
		CancelUrl:   body.CancelURL,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// parseTier maps the public-facing string to the proto enum. We accept
// case-insensitive short aliases the marketing site uses (starter,
// growth, ...).
func parseTier(s string) (billingv1.SubscriptionTier, bool) {
	switch s {
	case "starter", "STARTER", "SUBSCRIPTION_TIER_STARTER":
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER, true
	case "growth", "GROWTH", "SUBSCRIPTION_TIER_GROWTH":
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH, true
	case "enterprise", "ENTERPRISE", "SUBSCRIPTION_TIER_ENTERPRISE":
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE, true
	case "payg", "PAYG", "SUBSCRIPTION_TIER_PAYG":
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_PAYG, true
	default:
		return billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED, false
	}
}
