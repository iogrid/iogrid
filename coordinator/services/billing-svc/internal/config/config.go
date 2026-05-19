// Package config centralises the env-var contract for billing-svc.
//
// All external integrations (Stripe, Solana RPC, Jupiter, NATS, Postgres)
// are configured here so a single struct documents the deployment surface.
// Defaults are conservative: missing required values cause Load() to return
// an error rather than silently degrading.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the resolved runtime configuration for billing-svc.
type Config struct {
	// HTTP listen address (chi router). Defaults to :8080.
	ListenAddr string

	// Postgres connection URL for billing-svc's logical database.
	DatabaseURL string

	// NATS JetStream URL — the BILLING stream's metering subject is
	// consumed from here. Empty disables the metering consumer.
	NATSURL string

	// Stripe (customer subscriptions + Connect provider payouts).
	StripeSecretKey       string
	StripeWebhookSecret   string
	StripeConnectClientID string

	// Per-tier Stripe Price IDs. The wire enum maps to these at
	// CreateCheckoutSession time. Empty entries cause that tier to be
	// rejected with an explicit error.
	StripePriceIDs map[string]string

	// Public URL of the management plane; used to construct success /
	// cancel return URLs that Stripe Checkout redirects to.
	WebBaseURL string

	// Solana hot-wallet + Jupiter swap config. When GRIDTokenMint is
	// empty the Solana subsystem boots in stub mode (logs SKIP).
	SolanaRPCURL        string
	SolanaHotWalletPath string
	GRIDTokenMint       string
	// GRIDTokenProgram is one of "token-2022" (default), "token" (legacy SPL).
	// $GRID is minted under Token-2022 in production; the legacy program is
	// supported for dev / devnet fixtures.
	GRIDTokenProgram   string
	JupiterAPIURL      string
	BurnPercentage     float64
	IncineratorAddress string
	// BurnViaIncinerator — when true, the daily burn is implemented as a
	// transfer-to-incinerator instead of a real SPL BurnChecked. Default
	// false (real burn). Useful when the hot wallet is not the mint's
	// burn authority (legacy SPL Token mints without burn delegation).
	BurnViaIncinerator bool

	// SquadsMultisigPubkey — when set, billing-svc routes writes through a
	// Squads Protocol vault rather than single-sig (Phase 2). Empty in
	// Phase 0/1.
	SquadsMultisigPubkey string

	// Cron schedule for the daily payout loop. Defaults to 00:05 UTC.
	DailyPayoutCron string
	// DailyPayoutEnabled — when true, the in-process daily cron is started
	// (rather than relying on an external k8s CronJob). Default false.
	DailyPayoutEnabled bool

	// --- Off-ramp adapters (issue #167 / #169 / #170) --------------------
	//
	// OffRampProviders is the comma-separated list of provider names to
	// register at boot. Order matters — the web UI displays providers in
	// this order, so put the default real implementation first.
	//   "moonpay,sociable-cash,coinbase"
	// Empty disables the /api/v1/offramp/* routes (they return 503).
	OffRampProviders []string

	// MoonPay — default real off-ramp implementation. See
	// internal/offramp/moonpay.
	MoonPayAPIKey        string
	MoonPayWebhookSecret string
	MoonPayBaseURL       string

	// Sociable Cash — documented contract stub; the real adapter is
	// implemented in the sociable-cloud/cash repo and replaces this
	// package's body without changing the offramp.Provider interface.
	CashWebhookSecret string
	CashBaseURL       string
}

// Defaults applied when env vars are empty.
const (
	defaultListenAddr         = ":8080"
	defaultJupiterAPIURL      = "https://quote-api.jup.ag/v6"
	defaultBurnPercentage     = 2.0
	defaultIncineratorAddress = "1nc1nerator11111111111111111111111111111111"
	defaultDailyPayoutCron    = "5 0 * * *"
	defaultSolanaRPCURL       = "https://mainnet.helius-rpc.com"
)

// Load reads env vars and produces a validated Config. Returns a non-nil
// error if any REQUIRED value is missing.
func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:            getenv("LISTEN_ADDR", defaultListenAddr),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		NATSURL:               os.Getenv("NATS_URL"),
		StripeSecretKey:       os.Getenv("STRIPE_SECRET_KEY"),
		StripeWebhookSecret:   os.Getenv("STRIPE_WEBHOOK_SECRET"),
		StripeConnectClientID: os.Getenv("STRIPE_CONNECT_CLIENT_ID"),
		StripePriceIDs:        loadPriceIDs(),
		WebBaseURL:            getenv("WEB_BASE_URL", "https://app.iogrid.org"),
		SolanaRPCURL:          getenv("SOLANA_RPC_URL", defaultSolanaRPCURL),
		SolanaHotWalletPath:   os.Getenv("SOLANA_HOT_WALLET_KEYPAIR_PATH"),
		GRIDTokenMint:         os.Getenv("GRID_TOKEN_MINT_ADDRESS"),
		GRIDTokenProgram:      getenv("GRID_TOKEN_PROGRAM", "token-2022"),
		JupiterAPIURL:         getenv("JUPITER_API_URL", defaultJupiterAPIURL),
		BurnPercentage:        getenvFloat("BURN_PERCENTAGE", defaultBurnPercentage),
		IncineratorAddress:    getenv("INCINERATOR_ADDRESS", defaultIncineratorAddress),
		BurnViaIncinerator:    getenvBool("BURN_VIA_INCINERATOR", false),
		SquadsMultisigPubkey:  os.Getenv("SQUADS_MULTISIG_PUBKEY"),
		DailyPayoutCron:       getenv("DAILY_PAYOUT_CRON", defaultDailyPayoutCron),
		DailyPayoutEnabled:    getenvBool("DAILY_PAYOUT_ENABLED", false),

		OffRampProviders:     splitCSV(os.Getenv("OFFRAMP_PROVIDERS")),
		MoonPayAPIKey:        os.Getenv("MOONPAY_API_KEY"),
		MoonPayWebhookSecret: os.Getenv("MOONPAY_WEBHOOK_SECRET"),
		MoonPayBaseURL:       os.Getenv("MOONPAY_BASE_URL"),
		CashWebhookSecret:    os.Getenv("CASH_WEBHOOK_SECRET"),
		CashBaseURL:          os.Getenv("CASH_BASE_URL"),
	}

	// Required for any real Stripe operation. Skipped silently if the
	// service is started in "metering-only" mode (DATABASE_URL still
	// required because the BILLING stream needs an aggregate table).
	if cfg.DatabaseURL == "" {
		return nil, errors.New("DATABASE_URL is required")
	}

	if cfg.BurnPercentage < 0 || cfg.BurnPercentage > 100 {
		return nil, fmt.Errorf("BURN_PERCENTAGE must be in [0,100], got %.2f", cfg.BurnPercentage)
	}
	return cfg, nil
}

// SolanaEnabled is true when GRID_TOKEN_MINT_ADDRESS is set (post-TGE) AND
// a hot-wallet keypair path is configured. In stub mode the Solana
// subsystem logs SKIP and returns no-op responses; the daily cron still
// runs to surface log evidence in observability.
func (c *Config) SolanaEnabled() bool {
	return strings.TrimSpace(c.GRIDTokenMint) != "" &&
		strings.TrimSpace(c.SolanaHotWalletPath) != ""
}

// StripeEnabled is true when STRIPE_SECRET_KEY is configured. In test
// environments the Stripe key is left empty and routes return 503.
func (c *Config) StripeEnabled() bool {
	return strings.TrimSpace(c.StripeSecretKey) != ""
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// getenvBool parses a permissive "1|true|yes" → true.
func getenvBool(key string, def bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	}
	return def
}

// loadPriceIDs reads STRIPE_PRICE_PAYG / STRIPE_PRICE_STARTER /
// STRIPE_PRICE_GROWTH / STRIPE_PRICE_ENTERPRISE — one env var per tier.
func loadPriceIDs() map[string]string {
	out := map[string]string{}
	for _, tier := range []string{"PAYG", "STARTER", "GROWTH", "ENTERPRISE"} {
		if v := os.Getenv("STRIPE_PRICE_" + tier); v != "" {
			out[tier] = v
		}
	}
	return out
}

// splitCSV splits a comma-separated env value into trimmed,
// case-folded entries. "moonpay, Sociable-Cash" → ["moonpay","sociable-cash"].
func splitCSV(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
