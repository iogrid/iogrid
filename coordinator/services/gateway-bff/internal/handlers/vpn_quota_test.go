package handlers

import (
	"testing"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
)

// Regression test for #443 — locks the marketing-vs-billing pricing
// match in code so a future refactor that accidentally inlines a
// number can't silently break the public commitment.
//
// Public commitment (per docs/BUSINESS-STRATEGY.md §3.3 + the /vpn
// landing's FAQ): Free 2 GB / month; paid tiers are unlimited.
//
// PR #448 made vpnQuotaForTier the single source of truth. This test
// pins the four published tiers against their expected byte caps.
func TestVPNQuotaForTier_PinsPublicCommitment(t *testing.T) {
	const twoGiB uint64 = 2 * 1024 * 1024 * 1024 // public free-tier cap
	const unlimited uint64 = 0                   // sentinel — UI renders "unlimited"

	cases := []struct {
		name string
		tier billingv1.SubscriptionTier
		want uint64
	}{
		{"FREE = 2 GiB", billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED, twoGiB},
		{"PAYG = 2 GiB", billingv1.SubscriptionTier_SUBSCRIPTION_TIER_PAYG, twoGiB},
		{"STARTER = unlimited", billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER, unlimited},
		{"GROWTH = unlimited", billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH, unlimited},
		{"ENTERPRISE = unlimited", billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE, unlimited},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := vpnQuotaForTier(c.tier)
			if got != c.want {
				t.Fatalf("vpnQuotaForTier(%s) = %d bytes, want %d bytes", c.tier, got, c.want)
			}
		})
	}
}

// The free-tier cap is the one that customers SEE if /customer/billing
// shows "X / Y GB". Pin the exact byte value so a future drift in the
// helper (e.g. someone using 1000^3 instead of 1024^3) fails CI rather
// than shipping the wrong public number.
func TestVPNQuotaForTier_FreeIsExactly2GiB(t *testing.T) {
	got := vpnQuotaForTier(billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED)
	if got != 2*1024*1024*1024 {
		t.Fatalf("FREE tier quota drifted from 2 GiB: got %d bytes (%.2f GiB)",
			got, float64(got)/(1024*1024*1024))
	}
}
