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

// vpnQuotaForTier returns the bandwidth quota (bytes) for a given
// subscription tier. The free tier is capped at 2 GiB per month to
// match the public commitment on /vpn (#443). Paid tiers report 0 to
// signal "unlimited" — the UI renders 0 as a dash rather than "0 GB"
// so customers don't misread it as zero allowance.
//
// Single source of truth: every caller (gateway-bff, /customer/billing
// panel, daemon slow-lane gate) reads this table instead of inlining
// the numbers. Future tier table changes ship in this one place.
func vpnQuotaForTier(tier billingv1.SubscriptionTier) uint64 {
	switch tier {
	case billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER,
		billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH,
		billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE:
		return 0 // unlimited
	default:
		// FREE / PAYG / UNSPECIFIED — public 2 GiB / month cap.
		return 2 * 1024 * 1024 * 1024
	}
}

// GetVPNAccount aggregates the subscription + bandwidth-usage view.
//
//	GET /api/v1/vpn/account?workspace_id=<UUID>
//
// Free-tier callers (no subscription) get the 2 GiB free-tier cap that
// matches the public marketing commitment on /vpn (#443). Paid tiers
// (STARTER / GROWTH / ENTERPRISE) report quota=0 to signal "unlimited".
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
	subTier := billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED
	if sub != nil && sub.Subscription != nil {
		subTier = sub.Subscription.Tier
		tier = subTier.String()
		status = sub.Subscription.Status.String()
		upgrade = subTier != billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE
	}
	writeJSON(w, http.StatusOK, vpnAccount{
		Tier:                tier,
		Status:              status,
		BandwidthUsedBytes:  0, // populated by metering rollup once landed
		BandwidthQuotaBytes: vpnQuotaForTier(subTier),
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
