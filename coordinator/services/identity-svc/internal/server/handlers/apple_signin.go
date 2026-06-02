// Apple Sign-in JSON handler — POST /v1/identity/apple-signin.
//
// Wired separately from the Google + magic-link tree because the mobile
// iOS app is the ONLY caller and the bundle id / nonce semantics are
// specific to Sign-in-with-Apple. Sits behind the bearer middleware in
// the SAME chi.Group as the rest of identity-svc, but the route itself
// is public (a fresh-launch iOS client has no bearer to present).
//
// Response shape mirrors the proto AppleSignInResponse:
//
//	{
//	  "bundle": { ... AuthBundle ... },
//	  "new_user": true,
//	  "wallet_address": "",
//	  "nonce_validated": true
//	}
//
// Errors:
//
//   * 400 invalid_argument — missing identity_token in request body
//   * 401 unauthenticated — token failed validation (signature, iss,
//     aud, nonce, exp). Body: { code: "unauthenticated", message: "..." }
//   * 500 internal — DB / bundle issuance failure
//
// The iOS client surfaces 4xx as the "Apple sign-in failed, try again"
// error UI; 5xx falls into the same bucket with a "try again later" hint.

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/auth"
)

// MountAppleJSON attaches the /identity/apple-signin route. Called from
// routes.go inside the shared /v1 group so the path resolves to
// /v1/identity/apple-signin.
func (a *API) MountAppleJSON(r chi.Router) {
	r.Route("/identity", func(r chi.Router) {
		r.Post("/apple-signin", a.appleSignIn)
	})
}

// appleSignInReq is the JSON shape POST'd by the mobile app. Fields
// mirror proto AppleSignInRequest snake_case.
type appleSignInReq struct {
	IdentityToken string `json:"identity_token"`
	Nonce         string `json:"nonce"`
	AppleUser     string `json:"apple_user"`
	FullName      string `json:"full_name"`
}

func (a *API) appleSignIn(w http.ResponseWriter, r *http.Request) {
	var req appleSignInReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad request body")
		return
	}
	if req.IdentityToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "identity_token is required")
		return
	}
	if a.Auth == nil {
		writeError(w, http.StatusInternalServerError, "internal", "auth service not wired")
		return
	}
	res, err := a.Auth.CompleteAppleSignIn(r.Context(), req.IdentityToken, req.Nonce, req.FullName, r)
	if err != nil {
		if errors.Is(err, auth.ErrAppleTokenInvalid) {
			writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
			return
		}
		// "not configured" + DB / bundle failures.
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"bundle":          bundleToJSON(res.Bundle),
		"new_user":        res.NewUser,
		"wallet_address":  res.WalletAddress,
		"nonce_validated": res.NonceValidated,
	})
}
