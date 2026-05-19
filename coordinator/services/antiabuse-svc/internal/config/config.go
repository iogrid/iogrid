// Package config centralises the env-var contract for antiabuse-svc.
//
// Each filter backend (PhishTank, OpenPhish, Google Safe Browsing,
// NCMEC PhotoDNA) is conditional on its API key being set. When a key is
// absent the corresponding filter is loaded in "disabled" mode — it logs
// a startup warning and short-circuits to ALLOW. This makes local
// development frictionless and keeps unit tests offline.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved runtime configuration. Build via Load().
type Config struct {
	// ListenAddr is the HTTP bind address (default :8080).
	ListenAddr string

	// PhishTankAPIKey is the optional PhishTank registered-app key. The
	// public feed URL embeds this key. When empty the backend uses the
	// unauthenticated URL and accepts the lower (~hourly) rate limit.
	PhishTankAPIKey string
	// PhishTankRefresh controls cache refresh cadence (default 24h).
	PhishTankRefresh time.Duration

	// OpenPhishRefresh controls cache refresh cadence (default 6h).
	OpenPhishRefresh time.Duration

	// GSBAPIKey is the Google Safe Browsing v4 lookup API key. Required
	// to query the threatMatches:find endpoint.
	GSBAPIKey string

	// PhotoDNAAPIKey is the NCMEC PhotoDNA API key. Without it the
	// backend is in stub mode (logs warning, returns no-match).
	PhotoDNAAPIKey string

	// RedisURL is the Redis connection URL used for rate limiting.
	// Empty disables Redis-based rate limiting (in-memory fallback).
	RedisURL string

	// NATSURL is the JetStream-enabled NATS URL used for audit log
	// emission. Empty falls back to slog-only audit emission.
	NATSURL string

	// AuditPostgresDSN, when set, enables the relational audit mirror
	// that the retention pruner enforces. The pgx stdlib driver is
	// linked when this is non-empty. Empty disables the mirror
	// (JetStream-only — retention is enforced by stream MaxAge).
	AuditPostgresDSN string

	// AuditRetentionDays overrides the docs/LEGAL.md-mandated 90 days
	// for self-hosted deployments that need a different value
	// (regulator-imposed, customer contract, etc). Min 30 enforced at
	// startup; values below 30 are clamped to 90.
	AuditRetentionDays int

	// PhotoDNABloomRefresh controls the cadence at which the
	// in-memory NCMEC hash-bloom is rebuilt from the published
	// hash list (default 168h = 7 days). Test deployments override
	// this to validate the refresh path without waiting a week.
	PhotoDNABloomRefresh time.Duration

	// HighValueTargets is the comma-separated list of destinations that
	// get tighter per-provider rate limits (default: LinkedIn,
	// Facebook, Twitter, Google, Instagram).
	HighValueTargets []string

	// BlockDomains is a comma-separated list of glob patterns that the
	// domain classifier matches in addition to the hardcoded .gov /
	// .mil / banking / adult lists. Matches return ClassBlocked which
	// the handler maps to FILTER_DECISION_BLOCK with
	// reason="destination_blocked".
	//
	// Patterns use Go's filepath.Match glob syntax — `*.malware.test`,
	// `known-bad.test`, `evil-*.example`. Patterns are normalised to
	// lower-case at load time; matching is case-insensitive.
	//
	// Intended for staging / e2e harnesses to pre-seed a known-bad
	// fixture without standing up the reputation-feed pipeline. In
	// production the same list lives in Redis / the DB-backed loader.
	BlockDomains []string

	// DefaultCustomerRPS is the per-customer aggregate cap (default 100).
	DefaultCustomerRPS int
	// PremiumCustomerRPS is the per-premium-customer cap (default 1000).
	PremiumCustomerRPS int
	// HighValueProviderRPS is the per-provider RPS to any high-value
	// destination (default 10).
	HighValueProviderRPS int
}

// Load reads the environment and returns a populated Config. All values
// have safe defaults so the binary boots without any env vars set
// (filters become stubs / no-ops).
func Load() Config {
	return Config{
		ListenAddr:           getenv("LISTEN_ADDR", ":8080"),
		PhishTankAPIKey:      os.Getenv("PHISHTANK_API_KEY"),
		PhishTankRefresh:     durationEnv("PHISHTANK_REFRESH", 24*time.Hour),
		OpenPhishRefresh:     durationEnv("OPENPHISH_REFRESH", 6*time.Hour),
		GSBAPIKey:            os.Getenv("GSB_API_KEY"),
		PhotoDNAAPIKey:       os.Getenv("PHOTODNA_API_KEY"),
		RedisURL:             os.Getenv("REDIS_URL"),
		NATSURL:              os.Getenv("NATS_URL"),
		AuditPostgresDSN:     os.Getenv("AUDIT_POSTGRES_DSN"),
		AuditRetentionDays:   clampRetention(intEnv("AUDIT_RETENTION_DAYS", 90)),
		PhotoDNABloomRefresh: durationEnv("PHOTODNA_BLOOM_REFRESH", 7*24*time.Hour),
		HighValueTargets:     csv(getenv("HIGH_VALUE_TARGETS",
			"linkedin.com,facebook.com,twitter.com,google.com,instagram.com")),
		BlockDomains:         csv(os.Getenv("BLOCK_DOMAINS")),
		DefaultCustomerRPS:   intEnv("DEFAULT_CUSTOMER_RPS", 100),
		PremiumCustomerRPS:   intEnv("PREMIUM_CUSTOMER_RPS", 1000),
		HighValueProviderRPS: intEnv("HIGH_VALUE_PROVIDER_RPS", 10),
	}
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

// clampRetention enforces the docs/LEGAL.md floor (90 days). Self-hosted
// deployments that need a longer retention can specify any value above
// the floor; values below it are clamped up so the legal-shield argument
// in docs/LEGAL.md never silently breaks.
func clampRetention(n int) int {
	if n < 90 {
		return 90
	}
	return n
}

func csv(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
