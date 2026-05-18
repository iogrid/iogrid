// Package config centralises the environment-driven configuration for
// the gateway-bff microservice.
//
// Every value is loaded once at process boot via Load() and passed by
// value/pointer to the things that need it. No package-level globals.
//
// All addresses are full URLs (scheme included) so downstream Connect
// clients can wire them directly. In production this will look like
// "http://identity-svc.iogrid.svc.cluster.local:8080".
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config bundles every knob the gateway-bff binary reads from env.
type Config struct {
	// Network -------------------------------------------------------------

	// ListenAddr is the TCP listen for the public HTTP server. Defaults
	// to :8080 if unset; respected by the shared server bootstrap as
	// well.
	ListenAddr string `env:"LISTEN_ADDR" envDefault:":8080"`

	// Downstream services ------------------------------------------------

	IdentitySvcURL   string `env:"IDENTITY_SVC_URL"   envDefault:"http://identity-svc:8080"`
	ProvidersSvcURL  string `env:"PROVIDERS_SVC_URL"  envDefault:"http://providers-svc:8080"`
	WorkloadsSvcURL  string `env:"WORKLOADS_SVC_URL"  envDefault:"http://workloads-svc:8080"`
	AntiAbuseSvcURL  string `env:"ANTIABUSE_SVC_URL"  envDefault:"http://antiabuse-svc:8080"`
	BillingSvcURL    string `env:"BILLING_SVC_URL"    envDefault:"http://billing-svc:8080"`

	// Downstream timeouts ------------------------------------------------

	DownstreamTimeout time.Duration `env:"DOWNSTREAM_TIMEOUT" envDefault:"10s"`
	DownstreamRetries int           `env:"DOWNSTREAM_RETRIES" envDefault:"2"`

	// JWT -----------------------------------------------------------------

	// JWKSURL is the public key endpoint exposed by identity-svc. The
	// gateway-bff caches its keys and rotates them on TTL expiry.
	JWKSURL string `env:"JWKS_URL" envDefault:"http://identity-svc:8080/v1/.well-known/jwks.json"`
	// JWTIssuer is the expected `iss` claim. Tokens with a different
	// issuer are rejected outright.
	JWTIssuer string `env:"JWT_ISSUER" envDefault:"https://api.iogrid.org/identity"`
	// JWTAudience is the expected entry in the `aud` claim. We accept
	// tokens whose audience contains this string.
	JWTAudience string `env:"JWT_AUDIENCE" envDefault:"gateway-bff"`
	// JWKSRefreshInterval is how often we re-poll the JWKS endpoint.
	JWKSRefreshInterval time.Duration `env:"JWKS_REFRESH_INTERVAL" envDefault:"15m"`

	// CORS ----------------------------------------------------------------

	// CORSAllowedOrigins lists the exact (scheme+host) values we allow.
	// Defaults to the iogrid production web app.
	CORSAllowedOrigins []string `env:"CORS_ALLOWED_ORIGINS" envSeparator:"," envDefault:"https://app.iogrid.org,https://iogrid.org"`

	// Rate limiting -------------------------------------------------------

	// AuthedRatePerSec is the per-user request budget. 60 by default.
	AuthedRatePerSec int `env:"AUTHED_RATE_PER_SEC" envDefault:"60"`
	// AuthedBurst is the bucket size for the per-user limiter.
	AuthedBurst int `env:"AUTHED_BURST" envDefault:"120"`
	// AnonymousRatePerSec is the per-IP request budget for unauth requests.
	AnonymousRatePerSec int `env:"ANONYMOUS_RATE_PER_SEC" envDefault:"10"`
	// AnonymousBurst is the bucket size for the per-IP limiter.
	AnonymousBurst int `env:"ANONYMOUS_BURST" envDefault:"20"`

	// SSE -----------------------------------------------------------------

	// SSEKeepAliveInterval is how often the BFF emits a keep-alive comment
	// on long-lived SSE connections to keep proxies from idle-closing.
	SSEKeepAliveInterval time.Duration `env:"SSE_KEEPALIVE_INTERVAL" envDefault:"15s"`
}

// Load parses environment variables into a Config. Returns an error if
// any required value is malformed.
func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if cfg.AuthedRatePerSec <= 0 {
		return nil, fmt.Errorf("config: AUTHED_RATE_PER_SEC must be > 0")
	}
	if cfg.AnonymousRatePerSec <= 0 {
		return nil, fmt.Errorf("config: ANONYMOUS_RATE_PER_SEC must be > 0")
	}
	return cfg, nil
}
