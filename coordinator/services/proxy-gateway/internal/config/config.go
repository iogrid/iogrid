// Package config centralises the env-var contract for proxy-gateway.
//
// Defaults are picked so the binary boots in local-dev mode without any
// env vars set — TLS is disabled (plain TCP), upstream calls fall back
// to in-memory stubs, Redis falls back to in-memory session store, NATS
// falls back to slog-only emission.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved runtime configuration. Build via Load().
type Config struct {
	// ListenAddr is the SOCKS5/HTTP-CONNECT bind address. The TLS
	// frontend listens here when TLS_CERT_PATH/TLS_KEY_PATH are set;
	// otherwise plain TCP (for local dev / behind a TLS-terminating LB).
	// Default :443. Tests use :0.
	ListenAddr string

	// HealthAddr is the HTTP bind for /healthz, /readyz, /metrics.
	// Default :8080.
	HealthAddr string

	// TLSCertPath / TLSKeyPath are filesystem paths to the mounted
	// TLS certificate + key. When empty, the proxy listener runs
	// plain TCP. In production these come from a k8s Secret mount.
	TLSCertPath string
	TLSKeyPath  string

	// WorkloadsSvcURL is the base URL of workloads-svc (Connect-RPC
	// over HTTP/2). Empty enables the in-memory stub dispatcher.
	WorkloadsSvcURL string
	// AntiabuseSvcURL is the base URL of antiabuse-svc. Empty enables
	// allow-everything stub (test only).
	AntiabuseSvcURL string
	// AntiabuseFailOpen flips the proxy from fail-closed to fail-open
	// when antiabuse-svc is unreachable (RPC error / timeout / empty
	// verdict). Default false — docs/LEGAL.md mandates fail-closed so
	// a control-plane outage can't silently disable the legal-defence
	// kill switch. Operators MAY flip this to true via env during a
	// declared antiabuse-svc incident to keep the data plane flowing.
	// See issue #360.
	AntiabuseFailOpen bool
	// BillingSvcURL is the base URL of billing-svc. Empty enables the
	// in-memory ApiKey stub.
	BillingSvcURL string

	// RedisURL is the Redis connection URL used for sticky session
	// state. Empty disables Redis (in-memory fallback).
	RedisURL string
	// SessionTTL is how long a sticky session entry survives without
	// activity. Default 30 minutes (per docs/ARCHITECTURE.md).
	SessionTTL time.Duration

	// NATSURL is the JetStream-enabled NATS URL used for audit + billing
	// event emission. Empty falls back to slog-only emission.
	NATSURL string

	// MeterBytesEvery is the byte interval at which the relay loop emits
	// a billing event. Default 1 MiB (per task spec).
	MeterBytesEvery uint64

	// MaxFailoverAttempts caps how many providers a single connection
	// will try before giving up. Default 3 (per task spec).
	MaxFailoverAttempts int

	// IdleTimeout is how long a relay can be idle before being torn
	// down. Default 5 minutes.
	IdleTimeout time.Duration

	// DialTimeout is how long workloads-svc has to respond with an
	// assignment AND the daemon endpoint to accept our TCP connect.
	// Default 10 seconds.
	DialTimeout time.Duration

	// AllowPorts is the set of TCP destination ports the proxy will
	// forward. Empty means "all ports allowed except the explicit
	// docs/LEGAL.md outbound port blocklist". Comma-separated digit
	// list to override.
	AllowPorts []int

	// BlockPorts overrides the docs/LEGAL.md outbound port blocklist
	// (SMTP 25/465/587/2525, IRC 6667/6697, Tor 9001/9030). When set,
	// only the supplied list is enforced.
	BlockPorts []int
}

// Defaults returns the canonical defaults.
func Defaults() Config {
	return Config{
		ListenAddr:          ":443",
		HealthAddr:          ":8080",
		SessionTTL:          30 * time.Minute,
		MeterBytesEvery:     1 << 20, // 1 MiB
		MaxFailoverAttempts: 3,
		IdleTimeout:         5 * time.Minute,
		DialTimeout:         10 * time.Second,
		// docs/LEGAL.md mandatory outbound port restrictions.
		BlockPorts: []int{25, 465, 587, 2525, 6667, 6697, 9001, 9030},
	}
}

// Load reads the environment and returns a populated Config.
func Load() Config {
	c := Defaults()
	c.ListenAddr = getenv("LISTEN_ADDR", c.ListenAddr)
	c.HealthAddr = getenv("HEALTH_ADDR", c.HealthAddr)
	c.TLSCertPath = os.Getenv("TLS_CERT_PATH")
	c.TLSKeyPath = os.Getenv("TLS_KEY_PATH")
	c.WorkloadsSvcURL = os.Getenv("WORKLOADS_SVC_URL")
	c.AntiabuseSvcURL = os.Getenv("ANTIABUSE_SVC_URL")
	c.AntiabuseFailOpen = boolEnv("ANTIABUSE_FAIL_OPEN", c.AntiabuseFailOpen)
	c.BillingSvcURL = os.Getenv("BILLING_SVC_URL")
	c.RedisURL = os.Getenv("REDIS_URL")
	c.SessionTTL = durationEnv("SESSION_TTL", c.SessionTTL)
	c.NATSURL = os.Getenv("NATS_URL")
	c.MeterBytesEvery = uint64(intEnv("METER_BYTES_EVERY", int(c.MeterBytesEvery)))
	c.MaxFailoverAttempts = intEnv("MAX_FAILOVER_ATTEMPTS", c.MaxFailoverAttempts)
	c.IdleTimeout = durationEnv("IDLE_TIMEOUT", c.IdleTimeout)
	c.DialTimeout = durationEnv("DIAL_TIMEOUT", c.DialTimeout)
	if v := os.Getenv("ALLOW_PORTS"); v != "" {
		c.AllowPorts = intCSV(v)
	}
	if v := os.Getenv("BLOCK_PORTS"); v != "" {
		c.BlockPorts = intCSV(v)
	}
	return c
}

// TLSEnabled reports whether the proxy listener should terminate TLS.
func (c Config) TLSEnabled() bool {
	return c.TLSCertPath != "" && c.TLSKeyPath != ""
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "t", "true", "yes", "y", "on":
		return true
	case "0", "f", "false", "no", "n", "off":
		return false
	}
	return fallback
}

func intCSV(s string) []int {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil && n > 0 && n < 65536 {
			out = append(out, n)
		}
	}
	return out
}
