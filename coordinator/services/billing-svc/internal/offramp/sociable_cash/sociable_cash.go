// Package sociable_cash is the contract-only stub for the Sociable Cash
// off-ramp adapter. Real implementation lives at
// github.com/sociable-cloud/cash/offramp; this package is the iogrid
// half of the cross-org integration.
//
// # Loose coupling, not shared code
//
// Per founder direction 2026-05-19 (issue #167), iogrid and Sociable
// Cash are independent products with separate tokens, separate legal
// entities, and separate codebases. The integration is REST-shaped:
// iogrid redirects providers to cash.sociable.cloud/off-ramp, the Cash
// team handles KYC + swap + fiat settlement, and Cash POSTs back a
// signed webhook when the off-ramp completes.
//
// This file documents the contract surface so:
//
//   - iogrid's web UI can list "Sociable Cash" as an off-ramp option
//     even before the Cash team has finished implementing their end.
//   - The webhook receiver shape is agreed on cross-org BEFORE either
//     side codes.
//   - The Cash team can stand up the real adapter by replacing this
//     file's body with their actual swap-+-off-ramp logic (the
//     interface stays identical).
//
// # The documented redirect contract
//
//	https://cash.sociable.cloud/off-ramp
//	  ?from=GRID
//	  &amount=<lamports>
//	  &signer=<provider wallet pubkey>
//	  &return_url=<our return url>
//	  &ref=<our request_id>
//	  &currency=<USD|EUR|PHP|...>      (optional; defaults to USD)
//
// The redirect is unsigned today — Cash's KYC pipeline re-asks the user
// for the swap amount before executing on chain, so URL-tampering is
// detected client-side. When Cash adds signed redirects they should
// extend this struct with a `Secret` field and HMAC the query string;
// the offramp.Provider interface is already designed for that.
//
// # The documented webhook contract
//
// Sociable Cash POSTs:
//
//	POST /api/v1/webhooks/offramp/sociable-cash
//	Headers:
//	  Cash-Signature: <hex HMAC-SHA256 of body using CASH_WEBHOOK_SECRET>
//	Body:
//	  {
//	    "offramp_id":   "<cash internal id>",
//	    "ref":          "<echoed from redirect ?ref>",
//	    "provider_id":  "<echoed from redirect ?signer or our user id>",
//	    "status":       "pending|swapping|off-ramping|completed|failed",
//	    "grid_amount":  "<decimal $GRID string, 9 dp>",
//	    "fiat_amount":  "<decimal string in major units, e.g. '150.00'>",
//	    "fiat_currency": "USD",
//	    "txn_signature": "<solana sig of the GRID→USDC swap, if any>",
//	    "completed_at": "<RFC3339 timestamp when status=completed|failed>"
//	  }
//
// CASH_WEBHOOK_SECRET is provisioned out-of-band per environment and
// rotated by either team filing a coordinated PR; the value is shared
// in 1Password under "iogrid ⇄ Cash off-ramp webhook secret".
//
// # Open contract gaps (tracked in issue #167)
//
//   - Quote API: Cash has not yet published a quote endpoint for
//     pre-redirect price preview. Until then iogrid renders "Estimated
//     fiat: ~$X.XX" client-side from Pyth $GRID/USD * 0.97 (3% slippage
//     buffer).
//   - Multi-rail routing: when Cash adds GCash / M-Pesa rails, the
//     fiat_currency string will gain values like "PHP-GCASH" so the
//     billing-svc rail-aware reporting works.
//   - Refunds: today a failed off-ramp leaves the $GRID in the provider's
//     wallet (Cash never custodied it). When Cash adds atomic custody
//     they'll add a "refunded" status that we'll map to StatusFailed
//     with a non-nil completion timestamp.
package sociable_cash

import (
	"crypto/hmac"
	"crypto/sha256"
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

// ProviderName is the canonical id used by the registry, URL paths
// (/api/v1/webhooks/offramp/sociable-cash), and the database
// (offramp_requests.provider_name).
const ProviderName = "sociable-cash"

const (
	defaultBaseURL = "https://cash.sociable.cloud"
	gridDecimals   = 9
)

// Config captures the env-var surface for the Sociable Cash adapter.
// Even in stub mode we expect a webhook secret because the routes layer
// rejects unsigned webhooks unconditionally.
type Config struct {
	WebhookSecret string
	BaseURL       string
}

// Adapter implements offramp.Provider against the documented Sociable
// Cash contract. The redirect-URL builder is a real implementation —
// it returns the contract URL exactly as documented — but the webhook
// parser is a stub-friendly implementation that the Cash team will
// flesh out in their real adapter port.
type Adapter struct {
	cfg Config
}

// New constructs a Sociable Cash adapter. WebhookSecret may be empty in
// development (signature verification then always returns false).
func New(cfg Config) (*Adapter, error) {
	if strings.TrimSpace(cfg.BaseURL) == "" {
		cfg.BaseURL = defaultBaseURL
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Adapter{cfg: cfg}, nil
}

// Name implements offramp.Provider.
func (a *Adapter) Name() string { return ProviderName }

// BuildRedirectURL returns the contract URL documented in this
// package's comment block:
//
//	https://cash.sociable.cloud/off-ramp?from=GRID&amount=<lamports>
//	  &signer=<wallet>&return_url=<our return>&ref=<request_id>
//	  [&currency=<USD|EUR|...>]
func (a *Adapter) BuildRedirectURL(req offramp.OffRampRequest) (string, error) {
	if req.RequestID == "" {
		return "", errors.New("sociable_cash: RequestID required")
	}
	if req.WalletAddress == "" {
		return "", errors.New("sociable_cash: WalletAddress required")
	}
	if req.GridAmount == 0 {
		return "", errors.New("sociable_cash: GridAmount must be > 0")
	}
	q := url.Values{}
	q.Set("from", "GRID")
	q.Set("amount", strconv.FormatUint(req.GridAmount, 10))
	q.Set("signer", req.WalletAddress)
	q.Set("ref", req.RequestID)
	if req.ReturnURL != "" {
		q.Set("return_url", req.ReturnURL)
	}
	if fc := strings.ToUpper(strings.TrimSpace(req.FiatCurrency)); fc != "" {
		q.Set("currency", fc)
	}
	return a.cfg.BaseURL + "/off-ramp?" + q.Encode(), nil
}

// VerifyWebhookSignature implements offramp.Provider.
//
// Stub: when CASH_WEBHOOK_SECRET is empty we ALWAYS return false so
// the routes layer rejects the webhook with 401. Operators must
// provision the secret before enabling Sociable Cash in production.
//
// Signature scheme (per the contract comment at the top of this file):
// hex HMAC-SHA256 of the raw request body using CASH_WEBHOOK_SECRET.
func (a *Adapter) VerifyWebhookSignature(payload []byte, signature string) bool {
	if a.cfg.WebhookSecret == "" || len(payload) == 0 || signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(a.cfg.WebhookSecret))
	mac.Write(payload)
	got := mac.Sum(nil)
	want, err := hex.DecodeString(strings.TrimSpace(signature))
	if err != nil {
		return false
	}
	return hmac.Equal(got, want)
}

// ParseWebhook decodes the documented Sociable Cash payload into an
// OffRampStatus.
//
// Implementation note for the Cash team: when you port the real
// adapter, this is the only function whose body needs to change. The
// canonical OffRampStatus shape is part of the iogrid API contract;
// any new fields you need to expose should be added to
// offramp.OffRampStatus first.
func (a *Adapter) ParseWebhook(payload []byte) (*offramp.OffRampStatus, error) {
	var msg struct {
		OffRampID    string `json:"offramp_id"`
		Ref          string `json:"ref"`
		ProviderID   string `json:"provider_id"`
		Status       string `json:"status"`
		GridAmount   string `json:"grid_amount"`
		FiatAmount   string `json:"fiat_amount"`
		FiatCurrency string `json:"fiat_currency"`
		TxnSignature string `json:"txn_signature"`
		CompletedAt  string `json:"completed_at"`
	}
	if err := json.Unmarshal(payload, &msg); err != nil {
		return nil, fmt.Errorf("sociable_cash: decode webhook: %w", err)
	}
	if msg.Ref == "" {
		return nil, errors.New("sociable_cash: webhook missing ref")
	}
	status := normaliseStatus(msg.Status)

	var completedAt *time.Time
	if msg.CompletedAt != "" {
		if t, err := time.Parse(time.RFC3339, msg.CompletedAt); err == nil {
			completedAt = &t
		}
	}

	return &offramp.OffRampStatus{
		RequestID:     msg.Ref,
		ProviderID:    msg.ProviderID,
		Status:        status,
		GridAmount:    decimalGridToLamports(msg.GridAmount),
		FiatAmount:    msg.FiatAmount,
		FiatCurrency:  strings.ToUpper(strings.TrimSpace(msg.FiatCurrency)),
		CompletedAt:   completedAt,
		TxnSignature:  msg.TxnSignature,
		ProviderRefID: msg.OffRampID,
	}, nil
}

// normaliseStatus folds the wire status string into our canonical
// offramp.Status* constants. Unknown values default to StatusPending so
// the system fails open (the next webhook re-delivery can correct).
func normaliseStatus(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case offramp.StatusPending, "":
		return offramp.StatusPending
	case offramp.StatusSwapping, "swap":
		return offramp.StatusSwapping
	case offramp.StatusOffRamping, "settling", "offramping":
		return offramp.StatusOffRamping
	case offramp.StatusCompleted, "complete", "settled":
		return offramp.StatusCompleted
	case offramp.StatusFailed, "fail", "error":
		return offramp.StatusFailed
	default:
		return offramp.StatusPending
	}
}

// decimalGridToLamports converts a decimal $GRID string (e.g. "1.5") to
// raw lamports. Returns 0 on parse failure — caller treats a zero as a
// "Cash hasn't told us the amount yet" signal.
func decimalGridToLamports(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	parts := strings.SplitN(s, ".", 2)
	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0
	}
	out := whole
	for i := 0; i < gridDecimals; i++ {
		out *= 10
	}
	if len(parts) == 2 {
		frac := parts[1]
		if len(frac) > gridDecimals {
			frac = frac[:gridDecimals]
		}
		for len(frac) < gridDecimals {
			frac += "0"
		}
		f, err := strconv.ParseUint(frac, 10, 64)
		if err == nil {
			out += f
		}
	}
	return out
}
