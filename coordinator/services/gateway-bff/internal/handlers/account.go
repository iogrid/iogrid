package handlers

import (
	"net/http"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
)

// GetMe returns the currently-authenticated user + their bound
// identifiers. Calls identity-svc.GetUser.
//
//	GET /api/v1/me  ->  200 {user, identifiers}
//	                    401 if unauthenticated.
func (a *API) GetMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	resp, err := a.Clients.Identity.GetUser(r.Context(), &identityv1.GetUserRequest{
		Id: &commonv1.UUID{Value: claims.UserID().String()},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// StartGoogleSignIn kicks off the Google OAuth flow. The body is
// forwarded straight to identity-svc's matching RPC.
//
//	POST /api/v1/account/sign-in/google
//	  { return_to, code_challenge }
//	-> 200 { authorize_url, state }
func (a *API) StartGoogleSignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ReturnTo      string `json:"return_to"`
		CodeChallenge string `json:"code_challenge"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resp, err := a.Clients.Auth.StartGoogleSignIn(r.Context(), &identityv1.StartGoogleSignInRequest{
		ReturnTo:      body.ReturnTo,
		CodeChallenge: body.CodeChallenge,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// CompleteGoogleSignIn finishes Google OAuth and returns the auth bundle.
//
//	POST /api/v1/account/sign-in/google/complete
//	  { code, state, code_verifier }
//	-> 200 { bundle, new_user, merged }
func (a *API) CompleteGoogleSignIn(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code         string `json:"code"`
		State        string `json:"state"`
		CodeVerifier string `json:"code_verifier"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resp, err := a.Clients.Auth.CompleteGoogleSignIn(r.Context(), &identityv1.CompleteGoogleSignInRequest{
		Code:         body.Code,
		State:        body.State,
		CodeVerifier: body.CodeVerifier,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// RequestMagicLink emails a single-use sign-in link to the supplied
// address. We always respond accepted=true to prevent account
// enumeration (matches identity-svc's contract).
//
//	POST /api/v1/account/sign-in/magic
//	  { email, return_to }
//	-> 200 { expires_in, accepted }
func (a *API) RequestMagicLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Email    string `json:"email"`
		ReturnTo string `json:"return_to"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.Email == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "email required")
		return
	}
	resp, err := a.Clients.Auth.RequestMagicLink(r.Context(), &identityv1.RequestMagicLinkRequest{
		Email:    body.Email,
		ReturnTo: body.ReturnTo,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// CompleteMagicLink redeems a magic-link token and returns the auth
// bundle.
//
//	POST /api/v1/account/sign-in/magic/complete
//	  { token }
//	-> 200 { bundle, new_user, merged }
func (a *API) CompleteMagicLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resp, err := a.Clients.Auth.CompleteMagicLink(r.Context(), &identityv1.CompleteMagicLinkRequest{
		Token: body.Token,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// SignOut revokes the supplied refresh token.
//
//	POST /api/v1/account/sign-out
//	  { refresh_token }
//	-> 204
func (a *API) SignOut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	_, err := a.Clients.Auth.SignOut(r.Context(), &identityv1.SignOutRequest{RefreshToken: body.RefreshToken})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListSessions returns every active session for the current user.
//
//	GET /api/v1/account/sessions  ->  200 [{...}]
func (a *API) ListSessions(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	resp, err := a.Clients.Auth.ListSessions(r.Context(), &identityv1.ListSessionsRequest{})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
