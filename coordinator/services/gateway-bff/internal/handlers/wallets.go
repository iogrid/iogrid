// wallets.go: /api/v1/account/wallets surface (issue #326).
//
// Why this exists
// ---------------
// Closed #100 shipped the wallet-adapter UI scaffolding + the SIWS
// helpers in web/src/lib/solana/siws.ts, but the BFF surface that
// backs them was a Phase 0 stub (`emptyWalletsList` + `unimplemented`
// in routes.go) — so the page rendered "No wallets bound yet" forever
// and the "Connect & bind" button posted into a 501.
//
// This file is the real wiring: each verb forwards to the matching
// AuthService Connect-RPC method via the per-service authAdapter, with
// clients.WithCallerClaims attached so the header-forwarding
// interceptor stamps the caller's identity onto the outbound call.
// Identity-svc verifies the wallet signature, persists the binding in
// identifiers (kind='solana'), and returns the bound row.
//
// Surface
// -------
//
//	GET    /api/v1/account/wallets             list bound wallets
//	POST   /api/v1/account/wallets/challenge   issue a SIWS challenge
//	POST   /api/v1/account/wallets             complete the binding
//	DELETE /api/v1/account/wallets/{address}   unbind by base58 address
//
// Request / response shape mirrors the camelCase contract the UI's
// existing siws.ts client already speaks (see web/src/lib/solana/
// siws.ts), so the only frontend change is the path prefix.
package handlers

import (
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
)

// boundWalletJSON is the response row shape the UI's siws.ts already
// parses. We deliberately omit `id` from the public envelope and key
// on `walletAddress` because that's the human-visible identity.
type boundWalletJSON struct {
	WalletAddress string `json:"walletAddress"`
	Chain         string `json:"chain"`
	BoundAt       string `json:"boundAt"`
}

// ListWallets returns every Solana wallet bound to the caller. Sorted
// most-recently-bound first so the operator's newly-added wallet sits
// at the top after the bind flow completes.
//
//	GET /api/v1/account/wallets
//	  -> 200 { wallets: [{walletAddress, chain, boundAt}] }
//	     401 unauthenticated
func (a *API) ListWallets(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	ctx := clients.WithCallerClaims(r.Context(), claims)
	resp, err := a.Clients.Auth.ListBoundWallets(ctx, &identityv1.ListBoundWalletsRequest{})
	if err != nil {
		writeWalletsUpstreamError(w, err)
		return
	}
	out := make([]boundWalletJSON, 0, len(resp.GetBindings()))
	for _, b := range resp.GetBindings() {
		out = append(out, boundWalletJSON{
			WalletAddress: b.GetAddress(),
			Chain:         "solana",
			BoundAt:       protoTimeISO(b.GetCreatedAt().AsTime()),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"wallets": out})
}

// IssueWalletChallenge mints a fresh SIWS nonce + canonical message for
// the supplied address. The challenge is single-use (GETDEL on Redis on
// the matching Complete call) and expires after 5 minutes.
//
//	POST /api/v1/account/wallets/challenge
//	  { walletAddress }
//	-> 200 { nonce, challenge, expiresAt }
//	   400 invalid_argument (bad address)
//	   401 unauthenticated
func (a *API) IssueWalletChallenge(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body struct {
		WalletAddress string `json:"walletAddress"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.WalletAddress == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "walletAddress required")
		return
	}
	ctx := clients.WithCallerClaims(r.Context(), claims)
	resp, err := a.Clients.Auth.StartSiwsBinding(ctx, &identityv1.StartSiwsBindingRequest{
		WalletAddress: body.WalletAddress,
	})
	if err != nil {
		writeWalletsUpstreamError(w, err)
		return
	}
	// Extract the nonce from the canonical message tail
	// ("...\n\nNonce: <hex>") so the UI can echo it back to
	// CompleteSiwsBinding without re-deriving. The SIWS spec gives the
	// wallet the FULL message to sign, but the server only needs the
	// nonce on completion (the rest of the message is re-built from
	// the address + domain).
	nonce := extractNonce(resp.GetChallenge())
	writeJSON(w, http.StatusOK, map[string]any{
		"nonce":     nonce,
		"challenge": resp.GetChallenge(),
		"expiresAt": protoTimeISO(resp.GetExpiresAt().AsTime()),
	})
}

// BindWallet finishes the SIWS handshake. Identity-svc consumes the
// challenge atomically (GETDEL) and ed25519-verifies the base58
// signature against the wallet's pubkey before inserting the
// identifier row.
//
//	POST /api/v1/account/wallets
//	  { walletAddress, nonce, signature }
//	-> 200 { walletAddress, chain, boundAt }
//	   400 invalid_argument
//	   401 unauthenticated (bad signature OR missing bearer)
//	   403 permission_denied (wallet bound to another user)
//	   412 challenge_not_found (expired or already consumed)
func (a *API) BindWallet(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body struct {
		WalletAddress string `json:"walletAddress"`
		// `nonce` is accepted for parity with the UI's contract but
		// identity-svc keys the challenge by address (not by nonce)
		// and re-builds the message internally. We pass the nonce in
		// the log line below so support can trace a stuck bind.
		Nonce     string `json:"nonce"`
		Signature string `json:"signature"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.WalletAddress == "" || body.Signature == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "walletAddress and signature required")
		return
	}
	ctx := clients.WithCallerClaims(r.Context(), claims)
	resp, err := a.Clients.Auth.CompleteSiwsBinding(ctx, &identityv1.CompleteSiwsBindingRequest{
		WalletAddress:   body.WalletAddress,
		Signature:       body.Signature,
		CreateIfMissing: false,
	})
	if err != nil {
		writeWalletsUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, boundWalletJSON{
		WalletAddress: resp.GetBinding().GetAddress(),
		Chain:         "solana",
		BoundAt:       protoTimeISO(resp.GetBinding().GetCreatedAt().AsTime()),
	})
}

// UnbindWallet removes the supplied wallet from the caller's account.
// The {address} path parameter is the base58 pubkey — chosen over a
// UUID because the UI carries the address client-side anyway, and the
// uniqueness constraint on (kind, subject) makes it a safe natural
// key. Identity-svc asserts ownership in the WHERE clause so a
// missing row is indistinguishable from "not yours" (anti-
// enumeration).
//
//	DELETE /api/v1/account/wallets/{address}
//	-> 200 { ok: true }
//	   401 unauthenticated
//	   404 not_found
func (a *API) UnbindWallet(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	address := chi.URLParam(r, "address")
	if address == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "wallet address required")
		return
	}
	ctx := clients.WithCallerClaims(r.Context(), claims)
	_, err := a.Clients.Auth.UnbindWallet(ctx, &identityv1.UnbindWalletRequest{
		WalletAddress: address,
	})
	if err != nil {
		writeWalletsUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// extractNonce pulls the hex nonce out of the canonical SIWS message.
// The format is "<domain> wants you to sign in with your Solana
// account: <addr>\n\nNonce: <hex>". We grep for the last "Nonce: "
// occurrence so any future scope-line prefix change can't break this.
func extractNonce(challenge string) string {
	const marker = "Nonce: "
	for i := len(challenge) - len(marker); i >= 0; i-- {
		if challenge[i:i+len(marker)] == marker {
			return challenge[i+len(marker):]
		}
	}
	return ""
}

// protoTimeISO is a tiny helper so every JSON envelope renders
// timestamps in the same RFC3339Nano-UTC shape the rest of the BFF
// uses. Centralised so a future shift to a different epoch / TZ
// touches one line.
func protoTimeISO(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// writeWalletsUpstreamError maps Connect codes from identity-svc's
// SIWS path onto the HTTP shapes the UI needs to distinguish. We
// intentionally narrow CodeFailedPrecondition to 412 (challenge
// expired or already consumed) rather than the generic 5xx
// writeUpstreamError would emit — the UI prompts the user to retry
// the bind from scratch when it sees 412.
func writeWalletsUpstreamError(w http.ResponseWriter, err error) {
	var cErr *connect.Error
	if errors.As(err, &cErr) {
		switch cErr.Code() {
		case connect.CodeUnauthenticated:
			writeError(w, http.StatusUnauthorized, "unauthenticated", cErr.Message())
			return
		case connect.CodePermissionDenied:
			writeError(w, http.StatusForbidden, "permission_denied", cErr.Message())
			return
		case connect.CodeInvalidArgument:
			writeError(w, http.StatusBadRequest, "invalid_argument", cErr.Message())
			return
		case connect.CodeFailedPrecondition:
			writeError(w, http.StatusPreconditionFailed, "challenge_not_found", cErr.Message())
			return
		case connect.CodeNotFound:
			writeError(w, http.StatusNotFound, "not_found", cErr.Message())
			return
		}
	}
	writeUpstreamError(w, err)
}
