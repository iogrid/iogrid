package config

import (
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("LISTEN_ADDR", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Fatalf("default LISTEN_ADDR mismatch: %q", cfg.ListenAddr)
	}
	if cfg.AuthedRatePerSec != 60 {
		t.Fatalf("default AuthedRatePerSec = %d", cfg.AuthedRatePerSec)
	}
	if len(cfg.CORSAllowedOrigins) != 2 {
		t.Fatalf("CORS origins: %v", cfg.CORSAllowedOrigins)
	}
}

func TestLoad_OverrideViaEnv(t *testing.T) {
	t.Setenv("AUTHED_RATE_PER_SEC", "120")
	t.Setenv("CORS_ALLOWED_ORIGINS", "https://x.test,https://y.test")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuthedRatePerSec != 120 {
		t.Fatalf("override failed: %d", cfg.AuthedRatePerSec)
	}
	if len(cfg.CORSAllowedOrigins) != 2 || cfg.CORSAllowedOrigins[0] != "https://x.test" {
		t.Fatalf("cors override failed: %v", cfg.CORSAllowedOrigins)
	}
}

func TestLoad_RejectsZeroRate(t *testing.T) {
	t.Setenv("AUTHED_RATE_PER_SEC", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected rejection")
	}
}
