// Package moonpay implements the offramp.Provider interface against
// MoonPay's "sell" (off-ramp) flow.
//
// References:
//
//   - Redirect URL spec   — https://dev.moonpay.com/docs/ramps-sdk-sell-overview
//   - URL signing scheme  — https://dev.moonpay.com/docs/ramps-sdk-url-signing
//   - Webhook payloads    — https://dev.moonpay.com/docs/ramps-webhooks-overview
//
// Env vars consumed (see internal/config/config.go):
//
//   - MOONPAY_API_KEY        — pub key, embedded in the redirect URL as ?apiKey=
//   - MOONPAY_WEBHOOK_SECRET — used for HMAC-SHA256 on URL params + webhook bodies
//   - MOONPAY_BASE_URL       — defaults to https://sell.moonpay.com
//
// We sign every redirect URL because MoonPay rejects unsigned URLs in
// "strict" mode (which we set on the partner dashboard). The signature
// is computed exactly the way MoonPay's official SDK does it: HMAC-SHA256
// over the query string (sans signature param), base64-encoded.
package moonpay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp"
)

// ProviderName is the canonical id used in env vars, URL paths, and
// the registry. Lower kebab-case per offramp.Provider.Name() contract.
const ProviderName = "moonpay"

const (
	defaultBaseURL = "https://sell.moonpay.com"

	// gridDecimals is the SPL token decimal count for $GRID. Token-2022
	// standard. Mirrors coordinator/services/billing-svc/internal/solana
	// constants.
	gridDecimals = 9
)

// Config captures the env-var surface for MoonPay.
type Config struct {
	APIKey        string
	WebhookSecret string
	BaseURL       string
}

// Adapter is the offramp.Provider implementation for MoonPay.
type Adapter struct {
	cfg Config
	now func() time.Time
}

// New constructs an Adapter from the resolved config. Returns an error
// when the API key or webhook secret is empty (we refuse to construct a
// half-configured adapter — operators should not register it without
// MOONPAY_API_KEY + MOONPAY_WEBHOOK_SECRET).
func New(cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("moonpay: MOONPAY_API_KEY required")
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		return nil, errors.New("moonpay: MOONPAY_WEBHOOK_SECRET required")
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Adapter{cfg: cfg, now: time.Now}, nil
}

// Name implements offramp.Provider.
func (a *Adapter) Name() string { return ProviderName }

// BuildRedirectURL builds the MoonPay "sell" widget URL with the
// provider's wallet + amount + return URL + HMAC signature.
//
// The exact MoonPay redirect spec:
//
//	https://sell.moonpay.com/?apiKey=<key>
//	  &defaultBaseCurrencyCode=<solana-grid-or-similar>
//	  &baseCurrencyAmount=<decimal-grid>
//	  &quoteCurrencyCode=<usd|eur|...>
//	  &refundWalletAddress=<solana-pubkey>
//	  &redirectURL=<our return url>
//	  &externalCustomerId=<provider_id>
//	  &externalTransactionId=<our request_id>
//	  &signature=<HMAC-SHA256-base64 of the query string>
func (a *Adapter) BuildRedirectURL(req offramp.OffRampRequest) (string, error) {
	if req.RequestID == "" {
		return "", errors.New("moonpay: RequestID required")
	}
	if req.WalletAddress == "" {
		return "", errors.New("moonpay: WalletAddress required")
	}
	if req.GridAmount == 0 {
		return "", errors.New("moonpay: GridAmount must be > 0")
	}

	fiat := strings.ToLower(strings.TrimSpace(req.FiatCurrency))
	if fiat == "" {
		fiat = "usd"
	}

	// Convert lamports → human $GRID with 9 decimals. We render at most
	// 9 significant fractional digits, trimmed of trailing zeros so the
	// MoonPay signature stays canonical across replays.
	gridStr := lamportsToDecimal(req.GridAmount, gridDecimals)

	// Build query in a deterministic order so the signature matches the
	// query string we send. We construct via a slice of key/value pairs
	// (NOT url.Values.Encode which sorts keys) to preserve our chosen
	// order; the signature input is the literal query-string we emit.
	pairs := [][2]string{
		{"apiKey", a.cfg.APIKey},
		{"defaultBaseCurrencyCode", "grid"},
		{"baseCurrencyAmount", gridStr},
		{"quoteCurrencyCode", fiat},
		{"refundWalletAddress", req.WalletAddress},
		{"externalCustomerId", req.ProviderID},
		{"externalTransactionId", req.RequestID},
	}
	if req.ReturnURL != "" {
		pairs = append(pairs, [2]string{"redirectURL", req.ReturnURL})
	}

	qs := encodePairs(pairs)
	sig := signQueryString(qs, a.cfg.WebhookSecret)
	qs += "&signature=" + url.QueryEscape(sig)

	return a.cfg.BaseURL + "/?" + qs, nil
}

// VerifyWebhookSignature implements offramp.Provider.
//
// MoonPay sends the signature in the `Moonpay-Signature-V2` header as
// `t=<unix>,s=<hex-hmac-sha256>`. The signed payload is
// `<timestamp>.<raw-body>`. We accept either the parsed header value
// (with t=,s=) or a bare hex sig — the routes layer strips the prefix
// before calling us in some test contexts.
func (a *Adapter) VerifyWebhookSignature(payload []byte, signature string) bool {
	if len(payload) == 0 || signature == "" {
		return false
	}

	ts, sig := parseMoonPaySignature(signature)
	if sig == "" {
		// Bare hex — accept as-is; the signed payload is just the body.
		return hmacMatchHex(a.cfg.WebhookSecret, payload, signature)
	}

	signed := []byte(ts + "." + string(payload))
	return hmacMatchHex(a.cfg.WebhookSecret, signed, sig)
}

// ParseWebhook decodes MoonPay's transaction webhook payload into our
// canonical OffRampStatus.
//
// MoonPay sends one of:
//
//	{ "type":"transaction_updated", "data": { ... } }
//	{ "type":"transaction_failed",  "data": { ... } }
//
// The `data` object has fields: id (MoonPay's txn id), externalTransactionId
// (echoed from our redirect), externalCustomerId, status (pending/completed/...),
// baseCurrencyAmount, quoteCurrencyAmount, quoteCurrencyCode,
// cryptoTransactionId (Solana sig), updatedAt.
func (a *Adapter) ParseWebhook(payload []byte) (*offramp.OffRampStatus, error) {
	var env struct {
		Type string `json:"type"`
		Data struct {
			ID                    string  `json:"id"`
			ExternalTransactionID string  `json:"externalTransactionId"`
			ExternalCustomerID    string  `json:"externalCustomerId"`
			Status                string  `json:"status"`
			BaseCurrencyAmount    float64 `json:"baseCurrencyAmount"`
			QuoteCurrencyAmount   float64 `json:"quoteCurrencyAmount"`
			QuoteCurrencyCode     string  `json:"quoteCurrencyCode"`
			CryptoTransactionID   string  `json:"cryptoTransactionId"`
			UpdatedAt             string  `json:"updatedAt"`
		} `json:"data"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return nil, fmt.Errorf("moonpay: decode webhook: %w", err)
	}
	if env.Data.ExternalTransactionID == "" {
		return nil, errors.New("moonpay: webhook missing externalTransactionId")
	}
	status := mapMoonPayStatus(env.Data.Status)

	var completedAt *time.Time
	if status == offramp.StatusCompleted || status == offramp.StatusFailed {
		if t, err := time.Parse(time.RFC3339, env.Data.UpdatedAt); err == nil {
			completedAt = &t
		} else {
			now := a.now().UTC()
			completedAt = &now
		}
	}

	fiat := strings.ToUpper(strings.TrimSpace(env.Data.QuoteCurrencyCode))
	fiatAmount := ""
	if env.Data.QuoteCurrencyAmount != 0 {
		fiatAmount = strconv.FormatFloat(env.Data.QuoteCurrencyAmount, 'f', 2, 64)
	}

	return &offramp.OffRampStatus{
		RequestID:     env.Data.ExternalTransactionID,
		ProviderID:    env.Data.ExternalCustomerID,
		Status:        status,
		GridAmount:    decimalToLamports(env.Data.BaseCurrencyAmount, gridDecimals),
		FiatAmount:    fiatAmount,
		FiatCurrency:  fiat,
		CompletedAt:   completedAt,
		TxnSignature:  env.Data.CryptoTransactionID,
		ProviderRefID: env.Data.ID,
	}, nil
}

// --- helpers -------------------------------------------------------------

// mapMoonPayStatus translates MoonPay's transaction.status to our
// offramp.Status* values.
func mapMoonPayStatus(s string) string {
	switch strings.ToLower(s) {
	case "completed":
		return offramp.StatusCompleted
	case "failed":
		return offramp.StatusFailed
	case "waitingforswap", "waitingauthorization":
		return offramp.StatusSwapping
	case "waitingforpayout", "pending":
		return offramp.StatusOffRamping
	default:
		return offramp.StatusPending
	}
}

// lamportsToDecimal converts an integer lamport count to a decimal
// string with `decimals` fractional places, trailing zeros trimmed.
//
//	lamportsToDecimal(1_500_000_000, 9) → "1.5"
//	lamportsToDecimal(1_000_000_000, 9) → "1"
//	lamportsToDecimal(1, 9)             → "0.000000001"
func lamportsToDecimal(n uint64, decimals int) string {
	if decimals <= 0 {
		return strconv.FormatUint(n, 10)
	}
	s := strconv.FormatUint(n, 10)
	if len(s) <= decimals {
		s = strings.Repeat("0", decimals+1-len(s)) + s
	}
	cut := len(s) - decimals
	whole, frac := s[:cut], s[cut:]
	frac = strings.TrimRight(frac, "0")
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}

// decimalToLamports is the inverse of lamportsToDecimal — at best
// effort precision (float64 → uint64 rounding). MoonPay sends amounts
// as floats so we can't avoid this.
func decimalToLamports(v float64, decimals int) uint64 {
	if v <= 0 {
		return 0
	}
	mul := 1.0
	for i := 0; i < decimals; i++ {
		mul *= 10
	}
	return uint64(v*mul + 0.5)
}

// encodePairs renders the slice in iteration order using
// url.QueryEscape per value, joined by '&'.
func encodePairs(pairs [][2]string) string {
	var sb strings.Builder
	for i, kv := range pairs {
		if i > 0 {
			sb.WriteByte('&')
		}
		sb.WriteString(url.QueryEscape(kv[0]))
		sb.WriteByte('=')
		sb.WriteString(url.QueryEscape(kv[1]))
	}
	return sb.String()
}

// signQueryString computes the HMAC-SHA256 over the query string,
// base64-encoded — MoonPay's documented redirect-URL signature scheme.
func signQueryString(qs, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("?" + qs))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyRedirectSignature is exported for tests + cross-checks: given
// the raw query-string and the base64 signature, returns true if the
// signature is valid under cfg.WebhookSecret. This is the inverse of
// signQueryString.
func (a *Adapter) VerifyRedirectSignature(qs, signature string) bool {
	want := signQueryString(qs, a.cfg.WebhookSecret)
	return hmac.Equal([]byte(want), []byte(signature))
}

// parseMoonPaySignature pulls `t=<ts>,s=<hex>` out of the
// Moonpay-Signature-V2 header. Returns empty strings if either
// component is missing — the caller falls back to treating the
// signature as a bare hex value.
func parseMoonPaySignature(h string) (ts, sig string) {
	parts := strings.Split(h, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "t="):
			ts = strings.TrimPrefix(p, "t=")
		case strings.HasPrefix(p, "s="):
			sig = strings.TrimPrefix(p, "s=")
		}
	}
	return ts, sig
}

// hmacMatchHex compares an HMAC-SHA256 of payload (using secret) to the
// expected hex string in constant time.
func hmacMatchHex(secret string, payload []byte, hexSig string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	got := mac.Sum(nil)
	want, err := hex.DecodeString(strings.TrimSpace(hexSig))
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}
