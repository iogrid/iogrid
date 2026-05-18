package ratelimit

import (
	"context"
	"testing"
	"time"
)

func TestCheckCustomer_DefaultUnderLimit(t *testing.T) {
	l := New(Config{
		Window:              time.Second,
		DefaultCustomerRate: 3,
	}, nil)
	for i := 0; i < 3; i++ {
		d := l.CheckCustomer(context.Background(), "cust-1", TierDefault)
		if !d.Allowed {
			t.Fatalf("request %d should be ALLOW: %+v", i, d)
		}
	}
	d := l.CheckCustomer(context.Background(), "cust-1", TierDefault)
	if d.Allowed {
		t.Errorf("4th request should be BLOCKED")
	}
	if d.Reason != "customer_rate_limited" {
		t.Errorf("Reason = %q, want customer_rate_limited", d.Reason)
	}
}

func TestCheckCustomer_PremiumGetsHigherCap(t *testing.T) {
	l := New(Config{
		Window:              time.Second,
		DefaultCustomerRate: 2,
		PremiumCustomerRate: 5,
	}, nil)
	for i := 0; i < 5; i++ {
		d := l.CheckCustomer(context.Background(), "cust-prem", TierPremium)
		if !d.Allowed {
			t.Fatalf("premium request %d should be ALLOW", i)
		}
	}
	if d := l.CheckCustomer(context.Background(), "cust-prem", TierPremium); d.Allowed {
		t.Errorf("6th premium request should BLOCK")
	}
}

func TestCheckProviderDestination_HighValueOnly(t *testing.T) {
	l := New(Config{
		Window:                time.Second,
		HighValueProviderRate: 2,
		HighValueTargets:      []string{"linkedin.com"},
	}, nil)
	// Non-high-value destinations should always allow.
	for i := 0; i < 10; i++ {
		if d := l.CheckProviderDestination(context.Background(), "prov-1", "example.com"); !d.Allowed {
			t.Fatalf("non-HV request %d should ALLOW", i)
		}
	}
	// HV destinations cap at 2 RPS.
	for i := 0; i < 2; i++ {
		if d := l.CheckProviderDestination(context.Background(), "prov-1", "linkedin.com"); !d.Allowed {
			t.Fatalf("HV request %d should ALLOW", i)
		}
	}
	if d := l.CheckProviderDestination(context.Background(), "prov-1", "linkedin.com"); d.Allowed {
		t.Errorf("3rd HV request should BLOCK")
	}
}

func TestIsHighValue_SubdomainMatch(t *testing.T) {
	l := New(Config{HighValueTargets: []string{"linkedin.com"}}, nil)
	if !l.IsHighValue("ads.linkedin.com") {
		t.Errorf("ads.linkedin.com should match linkedin.com")
	}
	if l.IsHighValue("example.com") {
		t.Errorf("example.com should not match")
	}
}

func TestWindowExpiry_ReleasesSlots(t *testing.T) {
	l := New(Config{
		Window:              50 * time.Millisecond,
		DefaultCustomerRate: 1,
	}, nil)
	if d := l.CheckCustomer(context.Background(), "x", TierDefault); !d.Allowed {
		t.Fatal("1st should allow")
	}
	if d := l.CheckCustomer(context.Background(), "x", TierDefault); d.Allowed {
		t.Fatal("2nd should block")
	}
	time.Sleep(75 * time.Millisecond)
	if d := l.CheckCustomer(context.Background(), "x", TierDefault); !d.Allowed {
		t.Errorf("after window expiry, should allow again")
	}
}
