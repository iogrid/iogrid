// earnings_test.go exercises the pure mapping helpers in earnings.go
// without a database. Integration tests that hit Postgres live in a
// separate file (gated on a billing-svc CI fixture).
package server

import (
	"testing"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
)

// TestPayoutMethodKindRoundTrip locks the proto enum ⇄ string mapping
// stored in payout_methods.kind. The string form is the single column
// in the DB; the proto form is what the EarningsService RPC speaks. A
// regression here silently breaks /provide/earnings payout selection.
func TestPayoutMethodKindRoundTrip(t *testing.T) {
	cases := []struct {
		k    billingv1.PayoutMethodKind
		s    string
		ok   bool
	}{
		{billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_UNSPECIFIED, "UNSPECIFIED", true},
		{billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CASH_USDC, "CASH_USDC", true},
		{billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_FREE_VPN, "FREE_VPN", true},
		{billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CHARITY, "CHARITY", true},
	}
	for _, c := range cases {
		got, ok := payoutMethodKindToString(c.k)
		if ok != c.ok || got != c.s {
			t.Fatalf("toString(%v) = (%q,%v), want (%q,%v)", c.k, got, ok, c.s, c.ok)
		}
		round := payoutMethodKindFromString(c.s)
		if round != c.k {
			t.Fatalf("fromString(%q) = %v, want %v", c.s, round, c.k)
		}
	}
	// Unknown strings collapse to UNSPECIFIED (so a future enum-member
	// row read by an older binary doesn't crash).
	if got := payoutMethodKindFromString("FUTURE_VARIANT"); got != billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_UNSPECIFIED {
		t.Fatalf("unknown string did not collapse to UNSPECIFIED: %v", got)
	}
}
