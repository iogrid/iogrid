// Package offramp defines a provider-agnostic contract for fiat
// off-ramp partners that take a provider's $GRID balance and convert it
// to bank-deposit-able fiat.
//
// The Provider interface is intentionally narrow: build a signed
// redirect URL, verify a partner's webhook signature, parse a partner's
// webhook payload. Anything else (KYC, off-ramp execution, partner
// account management) lives behind the redirect URL on the partner's
// side — iogrid never custodies fiat.
//
// # Architecture (see ../README.md and docs/OFFRAMP_PROVIDERS.md)
//
//	┌───────────────────────────────────────────────────────────────────┐
//	│ Provider clicks "Withdraw" in /provide/earnings                  │
//	│                                                                   │
//	│ web -> gateway-bff /api/v1/offramp/start                          │
//	│        -> billing-svc StartOffRamp(provider_name, …)              │
//	│             1. Persists offramp_requests row (status=pending)     │
//	│             2. Resolves Provider via registry.GetProvider(name)   │
//	│             3. Provider.BuildRedirectURL(req) -> signed URL       │
//	│        <- { redirect_url, request_id }                            │
//	│   browser redirects to partner; partner handles KYC + swap        │
//	│                                                                   │
//	│ Partner posts back:                                               │
//	│   POST /api/v1/webhooks/offramp/{provider_name}                   │
//	│        -> billing-svc HandleWebhook(provider_name, body, sig)     │
//	│             1. Provider.VerifyWebhookSignature(body, sig)         │
//	│             2. Provider.ParseWebhook(body) -> OffRampStatus       │
//	│             3. Updates offramp_requests row                       │
//	│             4. Emits NATS event for telemetry-svc                 │
//	└───────────────────────────────────────────────────────────────────┘
//
// Today's catalogue:
//
//   - MoonPay (default real implementation, see moonpay/)
//   - Sociable Cash (documented contract stub, see sociable_cash/) —
//     real implementation lives in the sociable-cloud/cash repo; we
//     keep the contract surface here so the rest of iogrid is unaware
//     of the cross-org coupling.
//   - Coinbase (placeholder — wired post-Wormhole-NTT bridge to Base).
package offramp

import (
	"errors"
	"time"
)

// Provider is the interface every off-ramp partner adapter implements.
//
// All methods are safe to call concurrently. Implementations MUST be
// idempotent: a webhook redelivery for the same partner request id must
// map to the same OffRampStatus.
type Provider interface {
	// Name returns the canonical, lower-kebab-case provider identifier
	// used in env vars (OFFRAMP_PROVIDERS), URL paths
	// (/api/v1/webhooks/offramp/<name>), database rows
	// (offramp_requests.provider_name), and the gRPC StartOffRamp.provider_name
	// field. Must be stable across releases. Examples: "moonpay",
	// "sociable-cash", "coinbase".
	Name() string

	// BuildRedirectURL constructs a signed URL the browser redirects to
	// when the provider clicks "Withdraw". The URL embeds the request id
	// so the partner's webhook can echo it back. Implementations MUST
	// sign sensitive query params (amount, wallet) so the partner cannot
	// be tricked by a manipulated URL.
	BuildRedirectURL(req OffRampRequest) (string, error)

	// VerifyWebhookSignature checks the partner's signature header
	// against the raw request body. Returns false on any mismatch — the
	// caller should respond 401.
	VerifyWebhookSignature(payload []byte, signature string) bool

	// ParseWebhook decodes the partner's payload into our canonical
	// OffRampStatus. Implementations should preserve ProviderRefID so
	// the partner's transaction can be cross-referenced later (refunds,
	// dispute handling).
	ParseWebhook(payload []byte) (*OffRampStatus, error)
}

// OffRampRequest is the input to Provider.BuildRedirectURL.
type OffRampRequest struct {
	// RequestID is iogrid's UUID for this off-ramp attempt. Embedded in
	// the redirect URL as ?ref=<request_id> so the partner can echo it
	// back in the webhook.
	RequestID string

	// ProviderID is the iogrid provider's user id (the wallet owner).
	ProviderID string

	// WalletAddress is the provider's Solana wallet — the source of
	// the $GRID being off-ramped. Some partners require this in the
	// redirect URL for the swap-then-send flow.
	WalletAddress string

	// GridAmount is the raw lamport count of $GRID to off-ramp.
	// $GRID has 9 decimals (Token-2022 std), so 1 $GRID = 1_000_000_000
	// lamports. Use this scale uniformly across the system.
	GridAmount uint64

	// ReturnURL is where the partner should send the browser after
	// the off-ramp completes / cancels. Typically
	// https://app.iogrid.org/provide/earnings.
	ReturnURL string

	// FiatCurrency is the ISO-4217 currency code the provider wants to
	// receive: "USD", "EUR", "PHP", etc. Partners that don't support
	// the requested currency should return a 4xx redirect with an
	// explanatory query param.
	FiatCurrency string
}

// OffRampStatus is the canonical, partner-agnostic representation of
// an off-ramp request's state. ParseWebhook returns this; the routes
// layer persists it to offramp_requests.
type OffRampStatus struct {
	// RequestID echoes OffRampRequest.RequestID — partners are required
	// to round-trip our ref id in their webhook.
	RequestID string

	// ProviderID echoes OffRampRequest.ProviderID. Optional in webhook
	// payloads; the routes layer looks it up from offramp_requests if
	// the partner omits it.
	ProviderID string

	// Status is one of the constants below. Lowercase, hyphenated.
	Status string

	// GridAmount is the actual $GRID consumed by the swap (in
	// lamports). Partners may report a slightly different number than
	// what we sent (slippage) — we record both.
	GridAmount uint64

	// FiatAmount is a decimal string in major units of FiatCurrency
	// ("150.00" for $150.00 USD).
	FiatAmount string

	// FiatCurrency is ISO-4217.
	FiatCurrency string

	// CompletedAt is set when Status == StatusCompleted or StatusFailed.
	CompletedAt *time.Time

	// TxnSignature is the on-chain Solana signature for the $GRID→USDC
	// swap (or the underlying transfer). Used for forensic audit.
	TxnSignature string

	// ProviderRefID is the partner's internal id for this transaction
	// (MoonPay's transactionId, Sociable Cash's transferId, etc.).
	ProviderRefID string
}

// Status constants — keep in sync with offramp_requests.status enum
// in migrations/0004_offramp_requests.sql.
const (
	StatusPending    = "pending"     // request created, redirect issued
	StatusSwapping   = "swapping"    // partner is running the $GRID→USDC swap
	StatusOffRamping = "off-ramping" // partner is settling fiat to the user's bank
	StatusCompleted  = "completed"   // fiat hit the user's account
	StatusFailed     = "failed"      // any terminal failure
)

// ErrUnknownProvider is returned by the registry when GetProvider is
// called with a name that isn't registered.
var ErrUnknownProvider = errors.New("offramp: unknown provider")

// ErrInvalidSignature is returned by VerifyWebhookSignature wrappers
// when the routes layer wants to bubble a typed error rather than a
// plain bool.
var ErrInvalidSignature = errors.New("offramp: invalid webhook signature")
