package config

import (
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	c := Defaults()
	if c.ListenAddr != ":443" {
		t.Fatalf("ListenAddr = %q", c.ListenAddr)
	}
	if c.SessionTTL != 30*time.Minute {
		t.Fatalf("SessionTTL = %v", c.SessionTTL)
	}
	if c.MeterBytesEvery != 1<<20 {
		t.Fatalf("MeterBytesEvery = %d", c.MeterBytesEvery)
	}
	if c.MaxFailoverAttempts != 3 {
		t.Fatalf("MaxFailoverAttempts = %d", c.MaxFailoverAttempts)
	}
	// docs/LEGAL.md mandated outbound port blocks must be present.
	wantBlocked := []int{25, 465, 587, 2525, 6667, 6697, 9001, 9030}
	for _, p := range wantBlocked {
		found := false
		for _, x := range c.BlockPorts {
			if x == p {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("port %d missing from default block list", p)
		}
	}
}

func TestLoad_Env(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":1443")
	t.Setenv("WORKLOADS_SVC_URL", "http://workloads.example")
	t.Setenv("ANTIABUSE_SVC_URL", "http://aa.example")
	t.Setenv("BILLING_SVC_URL", "http://billing.example")
	t.Setenv("NATS_URL", "nats://x:4222")
	t.Setenv("REDIS_URL", "redis://x:6379/0")
	t.Setenv("SESSION_TTL", "15m")
	t.Setenv("METER_BYTES_EVERY", "524288")
	t.Setenv("MAX_FAILOVER_ATTEMPTS", "5")
	t.Setenv("ALLOW_PORTS", "80,443,8080")
	t.Setenv("BLOCK_PORTS", "25,123")
	t.Setenv("TLS_CERT_PATH", "/tls/tls.crt")
	t.Setenv("TLS_KEY_PATH", "/tls/tls.key")

	c := Load()
	if c.ListenAddr != ":1443" {
		t.Fatalf("ListenAddr = %q", c.ListenAddr)
	}
	if c.WorkloadsSvcURL != "http://workloads.example" {
		t.Fatalf("WorkloadsSvcURL = %q", c.WorkloadsSvcURL)
	}
	if c.SessionTTL != 15*time.Minute {
		t.Fatalf("SessionTTL = %v", c.SessionTTL)
	}
	if c.MeterBytesEvery != 524288 {
		t.Fatalf("MeterBytesEvery = %d", c.MeterBytesEvery)
	}
	if c.MaxFailoverAttempts != 5 {
		t.Fatalf("MaxFailoverAttempts = %d", c.MaxFailoverAttempts)
	}
	if len(c.AllowPorts) != 3 || c.AllowPorts[0] != 80 {
		t.Fatalf("AllowPorts = %v", c.AllowPorts)
	}
	if !c.TLSEnabled() {
		t.Fatalf("TLSEnabled() should be true")
	}
}

func TestTLSDisabledWithoutBothPaths(t *testing.T) {
	t.Setenv("TLS_CERT_PATH", "/tls/tls.crt")
	t.Setenv("TLS_KEY_PATH", "")
	c := Load()
	if c.TLSEnabled() {
		t.Fatal("TLSEnabled() should require BOTH cert and key")
	}
}

// TestLoad_AntiabuseFailOpen — issue #360: the env knob must default
// OFF (fail-closed kill switch) and accept the standard truthy
// spellings.
func TestLoad_AntiabuseFailOpen(t *testing.T) {
	// Default OFF.
	c := Load()
	if c.AntiabuseFailOpen {
		t.Fatal("AntiabuseFailOpen default = true; want false (fail-closed)")
	}
	for _, v := range []string{"1", "true", "TRUE", "on", "yes", "y"} {
		t.Setenv("ANTIABUSE_FAIL_OPEN", v)
		c := Load()
		if !c.AntiabuseFailOpen {
			t.Errorf("ANTIABUSE_FAIL_OPEN=%q parsed false", v)
		}
	}
	for _, v := range []string{"0", "false", "off", "no", "n", "bogus"} {
		t.Setenv("ANTIABUSE_FAIL_OPEN", v)
		c := Load()
		if c.AntiabuseFailOpen {
			t.Errorf("ANTIABUSE_FAIL_OPEN=%q parsed true", v)
		}
	}
}
