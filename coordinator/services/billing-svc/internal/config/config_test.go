package config

import (
	"testing"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	_, err := Load()
	if err == nil {
		t.Fatalf("expected error when DATABASE_URL unset")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/billing")
	t.Setenv("BURN_PERCENTAGE", "")
	t.Setenv("JUPITER_API_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BurnPercentage != defaultBurnPercentage {
		t.Errorf("BurnPercentage default: got %f want %f",
			cfg.BurnPercentage, defaultBurnPercentage)
	}
	if cfg.JupiterAPIURL != defaultJupiterAPIURL {
		t.Errorf("JupiterAPIURL default: got %q", cfg.JupiterAPIURL)
	}
	if cfg.IncineratorAddress != defaultIncineratorAddress {
		t.Errorf("IncineratorAddress default: got %q", cfg.IncineratorAddress)
	}
}

func TestSolanaEnabled(t *testing.T) {
	cfg := &Config{GRIDTokenMint: "", SolanaHotWalletPath: ""}
	if cfg.SolanaEnabled() {
		t.Errorf("expected disabled when both empty")
	}
	cfg.GRIDTokenMint = "MintAddrXYZ"
	if cfg.SolanaEnabled() {
		t.Errorf("expected disabled when wallet path empty")
	}
	cfg.SolanaHotWalletPath = "/tmp/keypair.json"
	if !cfg.SolanaEnabled() {
		t.Errorf("expected enabled when both set")
	}
}

func TestStripeEnabled(t *testing.T) {
	cfg := &Config{}
	if cfg.StripeEnabled() {
		t.Errorf("expected disabled when key empty")
	}
	cfg.StripeSecretKey = "sk_test_123"
	if !cfg.StripeEnabled() {
		t.Errorf("expected enabled when key set")
	}
}

func TestLoadPriceIDs(t *testing.T) {
	t.Setenv("STRIPE_PRICE_PAYG", "price_payg")
	t.Setenv("STRIPE_PRICE_GROWTH", "price_growth")
	t.Setenv("STRIPE_PRICE_STARTER", "")
	t.Setenv("STRIPE_PRICE_ENTERPRISE", "")
	m := loadPriceIDs()
	if m["PAYG"] != "price_payg" {
		t.Errorf("PAYG not set")
	}
	if m["GROWTH"] != "price_growth" {
		t.Errorf("GROWTH not set")
	}
	if _, ok := m["STARTER"]; ok {
		t.Errorf("STARTER should be omitted when empty")
	}
}
