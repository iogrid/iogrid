package abuse

import (
	"context"
	"errors"
	"testing"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
)

func TestStaticFilter(t *testing.T) {
	want := Verdict{Decision: DecisionBlock, Reason: "csam"}
	f := &StaticFilter{Verdict: want}
	got, err := f.Check(context.Background(), CheckInput{Host: "x"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Decision != want.Decision || got.Reason != want.Reason {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestStaticFilter_PropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	f := &StaticFilter{Err: sentinel}
	_, err := f.Check(context.Background(), CheckInput{})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v", err)
	}
}

func TestCategorySlug(t *testing.T) {
	cases := []struct {
		in   string
		want antiabusev1.CategorySlug
	}{
		{"e_commerce", antiabusev1.CategorySlug_CATEGORY_SLUG_E_COMMERCE},
		{"ECommerce", antiabusev1.CategorySlug_CATEGORY_SLUG_E_COMMERCE},
		{"seo", antiabusev1.CategorySlug_CATEGORY_SLUG_SEO},
		{"adult_content", antiabusev1.CategorySlug_CATEGORY_SLUG_ADULT_CONTENT},
		{"unknown_thing", antiabusev1.CategorySlug_CATEGORY_SLUG_UNSPECIFIED},
		{"", antiabusev1.CategorySlug_CATEGORY_SLUG_UNSPECIFIED},
	}
	for _, c := range cases {
		if got := categorySlug(c.in); got != c.want {
			t.Fatalf("categorySlug(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsRateLimitReason(t *testing.T) {
	for _, r := range []string{"rate_limited", "rate_limit", "rate_limit_customer", "rate_limit_provider"} {
		if !isRateLimitReason(r) {
			t.Errorf("isRateLimitReason(%q) = false; want true", r)
		}
	}
	if isRateLimitReason("anything_else") {
		t.Errorf("isRateLimitReason(anything_else) = true; want false")
	}
}

func TestNewConnectFilter_NilClient(t *testing.T) {
	// Building with empty URL should still succeed; we just won't dial.
	f := NewConnectFilter("http://localhost:0", nil)
	if f == nil || f.Client == nil {
		t.Fatal("expected non-nil filter and client")
	}
}

// TestConnectFilter_NilClient_ReturnsDecisionError — issue #360: when
// the filter cannot reach antiabuse-svc the Verdict MUST be
// DecisionError (not DecisionBlock), so proxy.go can route through
// the configurable fail-open / fail-closed policy.
func TestConnectFilter_NilClient_ReturnsDecisionError(t *testing.T) {
	cf := &ConnectFilter{}
	v, err := cf.Check(context.Background(), CheckInput{Host: "any.example", Port: 443})
	if err == nil {
		t.Fatal("expected error from nil-client path")
	}
	if v.Decision != DecisionError {
		t.Fatalf("Decision = %v, want DecisionError", v.Decision)
	}
	if v.Reason != "antiabuse_unavailable" {
		t.Fatalf("Reason = %q, want antiabuse_unavailable", v.Reason)
	}
}
