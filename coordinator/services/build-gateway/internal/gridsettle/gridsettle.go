// Package gridsettle settles iOS-build provider earnings in devnet $GRID.
//
// When a build reaches a terminal status the build-gateway computes the
// consumed amount (billable minutes × the per-minute rate) and POSTs it to
// billing-svc's /v1/grid/build-end (#712), which writes the 85/15 split
// settlement that the settlement-worker drains. This package owns the wire
// shape + the conversion; the build Service calls Settler.SettleBuild from
// its terminal-status hook.
//
// The customer wallet is NOT yet resolvable from a build (a build carries a
// WorkspaceID, not a wallet) — that binding + a build escrow are the
// remaining integration tracked in iogrid/iogrid#718. Until then a build
// with an empty wallet is a logged no-op (best-effort, mirrors dev-mode
// metering), so this can ship + be exercised ahead of the escrow work.
package gridsettle

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// GridDecimals — $GRID has 9 decimals, so 1 GRID == 1e9 atomic units.
const GridDecimals uint64 = 1_000_000_000

// DefaultRatePerMinuteAtomic is the devnet build price: 0.5 GRID / minute.
// (Placeholder until the pricing table lands; #718.)
const DefaultRatePerMinuteAtomic uint64 = GridDecimals / 2

// BillableToAtomic converts billable minutes to atomic $GRID at a per-minute
// rate. Saturates rather than overflowing on absurd inputs.
func BillableToAtomic(minutes int64, ratePerMinuteAtomic uint64) uint64 {
	if minutes <= 0 {
		return 0
	}
	m := uint64(minutes)
	// overflow guard: cap at a sane ceiling (1e6 minutes ≈ 1.9 yr).
	if m > 1_000_000 {
		m = 1_000_000
	}
	return m * ratePerMinuteAtomic
}

// BuildSettleInput mirrors billing-svc grid.BuildInput (#712).
type BuildSettleInput struct {
	BuildID        string `json:"build_id"`
	AttemptID      string `json:"attempt_id"`
	CustomerID     string `json:"customer_id,omitempty"`
	CustomerWallet string `json:"wallet_address"`
	ProviderWallet string `json:"provider_wallet,omitempty"`
	ProviderID     string `json:"provider_id,omitempty"`
	EscrowedAtomic uint64 `json:"escrowed_atomic"`
	ConsumedAtomic uint64 `json:"consumed_atomic"`
}

// Settler settles a finished build's provider earnings. Implementations must
// be safe to call from the build Service's terminal hook + idempotent on
// (BuildID, AttemptID) — billing-svc enforces that server-side (#712).
type Settler interface {
	SettleBuild(ctx context.Context, in BuildSettleInput) error
}

// Noop is the default when billing-svc isn't wired (dev / tests). It records
// nothing and returns nil so the terminal-status path never fails on it.
type Noop struct{}

// SettleBuild implements Settler.
func (Noop) SettleBuild(context.Context, BuildSettleInput) error { return nil }

// HTTPSettler POSTs to billing-svc /v1/grid/build-end.
type HTTPSettler struct {
	// BaseURL is billing-svc's base, e.g. http://billing-svc.iogrid:8080.
	BaseURL string
	// Client defaults to a 5s-timeout client when nil.
	Client *http.Client
}

// SettleBuild implements Settler. A build with no customer wallet is a
// logged no-op (the wallet binding is #718) rather than an error.
func (h *HTTPSettler) SettleBuild(ctx context.Context, in BuildSettleInput) error {
	if in.CustomerWallet == "" {
		// Nothing to settle to yet — the workspace→wallet binding (#718)
		// isn't resolved. Don't fail the build's status transition.
		return nil
	}
	cl := h.Client
	if cl == nil {
		cl = &http.Client{Timeout: 5 * time.Second}
	}
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/v1/grid/build-end", trimSlash(h.BaseURL))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return errors.New("billing-svc /v1/grid/build-end returned " + resp.Status)
	}
	return nil
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

// WalletResolver maps a user id to their bound $GRID wallet (#718). The
// build Service calls it at submission so a finished build can settle to the
// right provider/customer. Implementations return "" (not an error) when the
// user has no bound wallet, so submission never fails on settlement plumbing.
type WalletResolver interface {
	ResolveWallet(ctx context.Context, userID string) (string, error)
}

// NoopWalletResolver always returns "" (dev / no identity-svc wiring).
type NoopWalletResolver struct{}

// ResolveWallet implements WalletResolver.
func (NoopWalletResolver) ResolveWallet(context.Context, string) (string, error) { return "", nil }

// ProviderWalletResolver maps a provider id to the provider owner's bound
// $GRID payout wallet (#748). The build Service calls it at terminal status so
// a finished build's grid_build_settlement row carries a non-empty
// provider_wallet — the settlement-worker only drains rows WHERE
// provider_wallet <> ”, so without this the provider is never paid on-chain.
// Implementations return "" (not an error) when unresolvable so settlement
// degrades to a no-op rather than failing the build's status transition.
type ProviderWalletResolver interface {
	ResolveProviderWallet(ctx context.Context, providerID string) (string, error)
}

// NoopProviderWalletResolver always returns "" (dev / no providers-svc wiring).
type NoopProviderWalletResolver struct{}

// ResolveProviderWallet implements ProviderWalletResolver.
func (NoopProviderWalletResolver) ResolveProviderWallet(context.Context, string) (string, error) {
	return "", nil
}

// HTTPWalletResolver calls identity-svc's internal wallet endpoint (#718
// step 1): GET {IdentityURL}/internal/v1/users/{id}/wallet with the shared
// X-Internal-Token. A 404 (no binding) yields "" with no error.
type HTTPWalletResolver struct {
	// IdentityURL is identity-svc's base, e.g. http://identity-svc.iogrid:8080.
	IdentityURL string
	// Token is the shared secret matching identity-svc's IDENTITY_INTERNAL_TOKEN.
	Token string
	// Client defaults to a 5s-timeout client when nil.
	Client *http.Client
}

// ResolveWallet implements WalletResolver.
func (h *HTTPWalletResolver) ResolveWallet(ctx context.Context, userID string) (string, error) {
	if h.IdentityURL == "" || userID == "" {
		return "", nil
	}
	cl := h.Client
	if cl == nil {
		cl = &http.Client{Timeout: 5 * time.Second}
	}
	url := fmt.Sprintf("%s/internal/v1/users/%s/wallet", trimSlash(h.IdentityURL), userID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("X-Internal-Token", h.Token)
	resp, err := cl.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", nil // no bound wallet — settle becomes a no-op
	}
	if resp.StatusCode/100 != 2 {
		return "", errors.New("identity-svc wallet lookup returned " + resp.Status)
	}
	var body struct {
		WalletAddress string `json:"wallet_address"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	return body.WalletAddress, nil
}
