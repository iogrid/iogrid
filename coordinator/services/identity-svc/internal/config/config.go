// Package config loads identity-svc's runtime configuration from environment
// variables. Every secret / external-endpoint is overridable via env so the
// same binary boots in dev (docker-compose), CI (dockertest), and prod
// (k8s sealed-secrets) without recompilation.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config is the parsed environment for identity-svc. Field-level `env`
// tags document defaults; missing required vars produce a startup error
// (fail-fast — no silent fallbacks).
type Config struct {
	// --- HTTP server ----------------------------------------------------
	ListenAddr string `env:"LISTEN_ADDR" envDefault:":8080"`
	BaseURL    string `env:"BASE_URL"    envDefault:"http://localhost:8080"`

	// --- Storage --------------------------------------------------------
	DatabaseURL string `env:"DATABASE_URL"`
	RedisURL    string `env:"REDIS_URL" envDefault:"redis://localhost:6379/0"`

	// --- Google OAuth ---------------------------------------------------
	GoogleClientID     string `env:"GOOGLE_CLIENT_ID"`
	GoogleClientSecret string `env:"GOOGLE_CLIENT_SECRET"`
	GoogleRedirectURL  string `env:"GOOGLE_REDIRECT_URL" envDefault:"http://localhost:8080/v1/auth/google/callback"`

	// --- JWT ------------------------------------------------------------
	JWTPrivateKeyPath string        `env:"JWT_PRIVATE_KEY_PATH"`
	JWTPublicKeyPath  string        `env:"JWT_PUBLIC_KEY_PATH"`
	JWTIssuer         string        `env:"JWT_ISSUER"          envDefault:"https://api.iogrid.org/identity"`
	JWTAudience       []string      `env:"JWT_AUDIENCE"        envSeparator:"," envDefault:"gateway-bff,proxy-gateway"`
	JWTKeyID          string        `env:"JWT_KEY_ID"          envDefault:"primary"`
	AccessTokenTTL    time.Duration `env:"ACCESS_TOKEN_TTL"    envDefault:"15m"`
	RefreshTokenTTL   time.Duration `env:"REFRESH_TOKEN_TTL"   envDefault:"720h"` // 30 days
	StepUpTTL         time.Duration `env:"STEP_UP_TTL"         envDefault:"5m"`

	// JWTKeypairAutogen enables an ephemeral keypair-generate-at-boot
	// path for dev / e2e. When true the binary mints a fresh RSA-2048
	// keypair, writes both PEM files under JWTAutogenDir, logs a loud
	// warning, and overrides JWT{Private,Public}KeyPath. Tokens do not
	// survive pod restart; verifiers that cached the previous public
	// key reject them. NEVER set in prod.
	JWTKeypairAutogen bool   `env:"JWT_KEYPAIR_AUTOGEN" envDefault:"false"`
	JWTAutogenDir     string `env:"JWT_AUTOGEN_DIR"     envDefault:"/tmp/jwt-keys"`

	// --- SMTP (Stalwart) ------------------------------------------------
	SMTPHost     string `env:"SMTP_HOST"     envDefault:"mail.openova.io"`
	SMTPPort     int    `env:"SMTP_PORT"     envDefault:"587"`
	SMTPFrom     string `env:"SMTP_FROM"     envDefault:"no-reply@iogrid.org"`
	SMTPFromName string `env:"SMTP_FROM_NAME" envDefault:"iogrid"`
	SMTPUsername string `env:"SMTP_USERNAME" envDefault:""`
	SMTPPassword string `env:"SMTP_PASSWORD" envDefault:""`
	SMTPStartTLS bool   `env:"SMTP_STARTTLS" envDefault:"true"`

	// --- Magic-link / rate limits --------------------------------------
	MagicLinkTTL              time.Duration `env:"MAGIC_LINK_TTL"               envDefault:"10m"`
	MagicLinkPerEmailPerHour  int           `env:"MAGIC_LINK_PER_EMAIL_PER_HOUR" envDefault:"3"`
	MagicLinkPerIPPerHour     int           `env:"MAGIC_LINK_PER_IP_PER_HOUR"    envDefault:"10"`

	// --- Behaviour toggles ---------------------------------------------
	// AllowedReturnHosts is the allow-list of hostnames that may appear
	// in OAuth `return_to` and magic-link redirect URLs. Defends against
	// open-redirect.
	AllowedReturnHosts []string `env:"ALLOWED_RETURN_HOSTS" envSeparator:"," envDefault:"localhost,127.0.0.1,iogrid.org,app.iogrid.org"`

	// --- Sign in with Apple (#582) -------------------------------------
	//
	// APPLE_SUB_SALT is the per-deployment salt mixed into SHA-256 when
	// hashing the Apple `sub` claim into the `users.apple_sub_hash`
	// lookup column. The salt MUST be present in production; the
	// service will start without it (Apple sign-in returns "not
	// configured" until set) so dev / unit-test runs that don't
	// exercise the iOS path still come up.
	//
	// AppleAudience overrides the default bundle id (io.iogrid.app) the
	// validator requires on the `aud` claim. Production deployments
	// should never set this; it's here so a future TestFlight-only
	// build with a different bundle id can be tested in isolation.
	AppleSubSalt   string `env:"APPLE_SUB_SALT"   envDefault:""`
	AppleAudience  string `env:"APPLE_AUDIENCE"   envDefault:"io.iogrid.app"`
	AppleJWKSURL   string `env:"APPLE_JWKS_URL"   envDefault:"https://appleid.apple.com/auth/keys"`
	AppleIssuer    string `env:"APPLE_ISSUER"     envDefault:"https://appleid.apple.com"`
	AppleJWKSCacheTTL time.Duration `env:"APPLE_JWKS_CACHE_TTL" envDefault:"24h"`
}

// Load parses environment variables into Config and returns the populated
// struct. Caller is responsible for surfacing the error.
func Load() (Config, error) {
	var c Config
	if err := env.Parse(&c); err != nil {
		return Config{}, fmt.Errorf("identity-svc: parse env: %w", err)
	}
	return c, nil
}

// SMTPAddress returns the host:port form used by net/smtp.
func (c Config) SMTPAddress() string {
	return fmt.Sprintf("%s:%d", c.SMTPHost, c.SMTPPort)
}
