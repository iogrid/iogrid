package config

import (
	"testing"
	"time"
)

func TestLoad_DefaultsApply(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/iogrid_test")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ListenAddr != ":8080" {
		t.Errorf("ListenAddr default: %q", c.ListenAddr)
	}
	if c.SMTPPort != 587 {
		t.Errorf("SMTPPort default: %d", c.SMTPPort)
	}
	if c.MagicLinkTTL != 10*time.Minute {
		t.Errorf("MagicLinkTTL default: %v", c.MagicLinkTTL)
	}
	if len(c.JWTAudience) != 2 {
		t.Errorf("JWTAudience default: %v", c.JWTAudience)
	}
}

func TestLoad_OverrideViaEnv(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("MAGIC_LINK_TTL", "2m")
	t.Setenv("DATABASE_URL", "postgres://x/y")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.ListenAddr != ":9090" {
		t.Errorf("ListenAddr: %q", c.ListenAddr)
	}
	if c.MagicLinkTTL != 2*time.Minute {
		t.Errorf("MagicLinkTTL: %v", c.MagicLinkTTL)
	}
}

func TestSMTPAddress(t *testing.T) {
	c := Config{SMTPHost: "mail.example.org", SMTPPort: 2525}
	if c.SMTPAddress() != "mail.example.org:2525" {
		t.Errorf("SMTPAddress: %q", c.SMTPAddress())
	}
}
