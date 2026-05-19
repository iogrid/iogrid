package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	for _, k := range []string{
		"LISTEN_ADDR", "PHISHTANK_API_KEY", "GSB_API_KEY", "PHOTODNA_API_KEY",
		"REDIS_URL", "NATS_URL", "HIGH_VALUE_TARGETS",
		"DEFAULT_CUSTOMER_RPS", "PREMIUM_CUSTOMER_RPS", "HIGH_VALUE_PROVIDER_RPS",
		"PHISHTANK_REFRESH", "OPENPHISH_REFRESH",
	} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	c := Load()
	if c.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", c.ListenAddr)
	}
	if c.DefaultCustomerRPS != 100 {
		t.Errorf("DefaultCustomerRPS = %d, want 100", c.DefaultCustomerRPS)
	}
	if c.PremiumCustomerRPS != 1000 {
		t.Errorf("PremiumCustomerRPS = %d, want 1000", c.PremiumCustomerRPS)
	}
	if c.HighValueProviderRPS != 10 {
		t.Errorf("HighValueProviderRPS = %d, want 10", c.HighValueProviderRPS)
	}
	if c.PhishTankRefresh != 24*time.Hour {
		t.Errorf("PhishTankRefresh = %v, want 24h", c.PhishTankRefresh)
	}
	if c.OpenPhishRefresh != 6*time.Hour {
		t.Errorf("OpenPhishRefresh = %v, want 6h", c.OpenPhishRefresh)
	}
	want := []string{"linkedin.com", "facebook.com", "twitter.com", "google.com", "instagram.com"}
	if len(c.HighValueTargets) != len(want) {
		t.Errorf("HighValueTargets = %v, want %v", c.HighValueTargets, want)
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("DEFAULT_CUSTOMER_RPS", "250")
	t.Setenv("HIGH_VALUE_TARGETS", "alpha.com,beta.com")
	t.Setenv("PHISHTANK_REFRESH", "12h")
	c := Load()
	if c.ListenAddr != ":9090" {
		t.Errorf("ListenAddr override failed: %q", c.ListenAddr)
	}
	if c.DefaultCustomerRPS != 250 {
		t.Errorf("DefaultCustomerRPS override failed: %d", c.DefaultCustomerRPS)
	}
	if len(c.HighValueTargets) != 2 || c.HighValueTargets[0] != "alpha.com" {
		t.Errorf("HighValueTargets override failed: %v", c.HighValueTargets)
	}
	if c.PhishTankRefresh != 12*time.Hour {
		t.Errorf("PhishTankRefresh override failed: %v", c.PhishTankRefresh)
	}
}

func TestLoad_BlockDomains_ParsedFromCSV(t *testing.T) {
	t.Setenv("BLOCK_DOMAINS", "malware.test, KNOWN-bad.test ,*.evil.example")
	c := Load()
	want := []string{"malware.test", "known-bad.test", "*.evil.example"}
	if len(c.BlockDomains) != len(want) {
		t.Fatalf("BlockDomains = %v, want %v", c.BlockDomains, want)
	}
	for i, w := range want {
		if c.BlockDomains[i] != w {
			t.Errorf("BlockDomains[%d] = %q, want %q", i, c.BlockDomains[i], w)
		}
	}
}

func TestLoad_BlockDomains_EmptyByDefault(t *testing.T) {
	t.Setenv("BLOCK_DOMAINS", "")
	c := Load()
	if len(c.BlockDomains) != 0 {
		t.Errorf("BlockDomains default = %v, want empty", c.BlockDomains)
	}
}

func TestLoad_InvalidNumbersFallBack(t *testing.T) {
	t.Setenv("DEFAULT_CUSTOMER_RPS", "garbage")
	t.Setenv("PHISHTANK_REFRESH", "notADuration")
	c := Load()
	if c.DefaultCustomerRPS != 100 {
		t.Errorf("DefaultCustomerRPS = %d, want fallback 100", c.DefaultCustomerRPS)
	}
	if c.PhishTankRefresh != 24*time.Hour {
		t.Errorf("PhishTankRefresh = %v, want fallback 24h", c.PhishTankRefresh)
	}
}
