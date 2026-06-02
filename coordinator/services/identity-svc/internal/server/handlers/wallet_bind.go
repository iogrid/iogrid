// wallet_bind.go — consumer-side wallet binding for the mobile VPN app
// (Track 2 of EPIC #581 / Closes #583 #584).
//
// Flow
// ----
//  1. Mobile client opens Phantom or Ping via deeplink, retrieves
//     a Solana wallet address + a signature over
//     "iogrid:bind:<nonce>:<unix_ts>".
//  2. Mobile POSTs the (address, provider, challenge, signature) to
//     `POST /v1/identity/wallet/bind` with its bearer token.
//  3. Server verifies the ed25519 signature against the address via
//     internal/wallet.VerifyBindSignature, then upserts the row in
//     customer_wallet_bindings (one wallet per user, switching means
//     overwriting the prior row).
//  4. Server returns the resolved binding so the mobile app can render
//     the wallet card immediately.
//
// PATCH /v1/identity/wallet/unbind clears the binding (Settings →
// Wallet → "Switch wallet" UX).
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
	walletpkg "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/wallet"
)

// MountWalletBind installs the /v1/identity/wallet routes on the
// supplied router (the caller is expected to already be scoped to /v1).
// Kept on the API struct so the existing routes.go wiring picks it up
// via the central MountV1 call.
func (a *API) MountWalletBind(r chi.Router) {
	r.Route("/identity/wallet", func(r chi.Router) {
		r.Post("/bind", a.bindWallet)
		r.Patch("/unbind", a.unbindCustomerWallet)
		r.Get("/", a.getBoundCustomerWallet)
	})
}

type bindWalletReq struct {
	// Base58-encoded Solana ed25519 pubkey returned by Phantom or Ping.
	WalletAddress string `json:"wallet_address"`
	// One of "phantom" / "ping" — recorded so the mobile app can deeplink
	// back to the same wallet for top-up / re-signing.
	WalletProvider string `json:"wallet_provider"`
	// The exact bytes the wallet signed:
	//   "iogrid:bind:<nonce>:<unix_seconds>"
	// Server verifies the timestamp window + signature; nonce is opaque.
	Challenge string `json:"challenge"`
	// Base58-encoded ed25519 signature of `Challenge` by the keypair
	// whose public component is `WalletAddress`.
	Signature string `json:"signature"`
}

type bindWalletJSON struct {
	UserID         string `json:"user_id"`
	WalletAddress  string `json:"wallet_address"`
	WalletProvider string `json:"wallet_provider"`
	BoundAt        string `json:"bound_at"`
}

type bindWalletResp struct {
	Binding bindWalletJSON `json:"binding"`
}

func (a *API) bindWallet(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUserID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	var req bindWalletReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	if !store.IsValidWalletProvider(req.WalletProvider) {
		writeError(w, http.StatusBadRequest, "invalid_argument",
			`wallet_provider must be "phantom" or "ping"`)
		return
	}
	if err := walletpkg.VerifyBindSignature(req.WalletAddress, req.Challenge, req.Signature, time.Now()); err != nil {
		switch {
		case errors.Is(err, walletpkg.ErrInvalidAddress):
			writeError(w, http.StatusBadRequest, "invalid_argument", "invalid wallet_address")
		case errors.Is(err, walletpkg.ErrMalformedChallenge):
			writeError(w, http.StatusBadRequest, "invalid_argument", "malformed challenge")
		case errors.Is(err, walletpkg.ErrExpiredChallenge):
			writeError(w, http.StatusBadRequest, "challenge_expired", "challenge timestamp outside acceptance window")
		case errors.Is(err, walletpkg.ErrInvalidSignature):
			writeError(w, http.StatusUnauthorized, "unauthenticated", "signature does not match wallet_address")
		default:
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
		}
		return
	}

	binding := &store.CustomerWalletBinding{
		UserID:         userID,
		WalletAddress:  req.WalletAddress,
		WalletProvider: store.WalletProvider(req.WalletProvider),
	}
	if err := a.Store.UpsertCustomerWalletBinding(r.Context(), nil, binding); err != nil {
		// Most likely: the wallet address is already bound by ANOTHER
		// user (the address_uk unique index trips). Mapped to 409 so the
		// client can render "this wallet is already linked to another
		// iogrid account; sign in with that account or switch wallets".
		writeError(w, http.StatusConflict, "already_bound", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bindWalletResp{
		Binding: bindWalletJSON{
			UserID:         binding.UserID.String(),
			WalletAddress:  binding.WalletAddress,
			WalletProvider: string(binding.WalletProvider),
			BoundAt:        binding.BoundAt.UTC().Format(time.RFC3339Nano),
		},
	})
}

func (a *API) unbindCustomerWallet(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUserID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	if err := a.Store.DeleteCustomerWalletBinding(r.Context(), nil, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Idempotent: already unbound is success.
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) getBoundCustomerWallet(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUserID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	b, err := a.Store.GetCustomerWalletBinding(r.Context(), nil, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// 200 with binding=null lets the client distinguish
			// "haven't onboarded yet" from "endpoint broken".
			writeJSON(w, http.StatusOK, map[string]any{"binding": nil})
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := bindWalletJSON{
		UserID:         b.UserID.String(),
		WalletAddress:  b.WalletAddress,
		WalletProvider: string(b.WalletProvider),
		BoundAt:        b.BoundAt.UTC().Format(time.RFC3339Nano),
	}
	resp := map[string]any{"binding": out}
	if b.LastBalanceAt != nil && b.LastBalanceLamports != nil {
		resp["last_balance"] = map[string]any{
			"observed_at":        b.LastBalanceAt.UTC().Format(time.RFC3339Nano),
			"amount_token_atoms": *b.LastBalanceLamports,
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
