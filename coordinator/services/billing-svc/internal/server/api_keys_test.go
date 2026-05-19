package server

import (
	"strings"
	"testing"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
)

// TestMintPlaintextKey_ShapeAndHashStability locks the format of the
// generated key string + verifies that hashKey(plaintext) matches the
// keyHash returned by mintPlaintextKey for the same plaintext.
func TestMintPlaintextKey_ShapeAndHashStability(t *testing.T) {
	plaintext, keyHash, lastFour, err := mintPlaintextKey()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !strings.HasPrefix(plaintext, "iog_") {
		t.Fatalf("expected iog_ prefix, got %q", plaintext)
	}
	// 4 prefix + 64 hex chars = 68
	if len(plaintext) != 68 {
		t.Fatalf("expected length 68, got %d", len(plaintext))
	}
	if lastFour != plaintext[len(plaintext)-4:] {
		t.Fatalf("lastFour mismatch")
	}
	if got := hashKey(plaintext); got != keyHash {
		t.Fatalf("hashKey unstable: mint=%q rehash=%q", keyHash, got)
	}
}

// TestMintPlaintextKey_Uniqueness rolls 1000 keys and ensures no
// collisions on plaintext OR hash.
func TestMintPlaintextKey_Uniqueness(t *testing.T) {
	const N = 1000
	seen := make(map[string]struct{}, N)
	hashes := make(map[string]struct{}, N)
	for i := 0; i < N; i++ {
		p, h, _, err := mintPlaintextKey()
		if err != nil {
			t.Fatalf("mint #%d: %v", i, err)
		}
		if _, dup := seen[p]; dup {
			t.Fatalf("plaintext collision at i=%d: %s", i, p)
		}
		if _, dup := hashes[h]; dup {
			t.Fatalf("hash collision at i=%d", i)
		}
		seen[p] = struct{}{}
		hashes[h] = struct{}{}
	}
}

func TestTierFromString(t *testing.T) {
	cases := map[string]billingv1.SubscriptionTier{
		"":           billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED,
		"PAYG":       billingv1.SubscriptionTier_SUBSCRIPTION_TIER_PAYG,
		"starter":    billingv1.SubscriptionTier_SUBSCRIPTION_TIER_STARTER,
		" Growth ":   billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH,
		"ENTERPRISE": billingv1.SubscriptionTier_SUBSCRIPTION_TIER_ENTERPRISE,
		"bogus":      billingv1.SubscriptionTier_SUBSCRIPTION_TIER_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := tierFromString(in); got != want {
			t.Errorf("tierFromString(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	cases := map[string][]string{
		"":                 nil,
		"scrape":           {"scrape"},
		"scrape,bandwidth": {"scrape", "bandwidth"},
		"  a , b,, c ":     {"a", "b", "c"},
	}
	for in, want := range cases {
		got := splitCSV(in)
		if len(got) != len(want) {
			t.Errorf("splitCSV(%q) len=%d want %d", in, len(got), len(want))
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("splitCSV(%q)[%d]=%q want %q", in, i, got[i], want[i])
			}
		}
	}
}
