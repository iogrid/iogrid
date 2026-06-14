package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

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
	// #801: marshal with protojson (canonical proto3-JSON: camelCase +
	// enum-as-string) rather than stdlib encoding/json. stdlib emits the
	// protoc-gen-go struct tags — snake_case fields and the enum as its
	// numeric tag (`"kind":2`) — which forced /account/identifiers' web
	// panel to hand-decode the wire shape (#372). That coupling is the
	// proto3-JSON enum-as-int masking class #630/#633/#758 converged away
	// from. EmitUnpopulated keeps proto3-default fields present so the web
	// can distinguish "empty" from "absent". Identifiers now arrive as
	// {"kind":"IDENTIFIER_KIND_MAGIC_LINK","verifiedEmail":"…"} — the shape
	// the web protobuf-es mapping expects.
	writeProtoJSON(w, http.StatusOK, resp)
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

// EnsureMyIdentifier idempotently registers an identifier on the caller's
// own account — the web's NextAuth signIn event calls this so magic-link
// sign-ins finally materialize as identifier rows (#685: users signed in
// via magic link saw "No identifiers bound" because nothing in that path
// ever told identity-svc about the email).
//
//	POST /api/v1/me/identifiers  {kind:"IDENTIFIER_KIND_MAGIC_LINK", verified_email:"a@b.c"}
//	  -> 200 {identifier, created}
func (a *API) EnsureMyIdentifier(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body struct {
		Kind          string `json:"kind"`
		VerifiedEmail string `json:"verified_email"`
		Subject       string `json:"subject"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	kind := identityv1.IdentifierKind(identityv1.IdentifierKind_value[body.Kind])
	if kind == identityv1.IdentifierKind_IDENTIFIER_KIND_UNSPECIFIED {
		writeError(w, http.StatusBadRequest, "bad_request", "kind required (e.g. IDENTIFIER_KIND_MAGIC_LINK)")
		return
	}
	resp, err := a.Clients.Identity.EnsureIdentifier(r.Context(), &identityv1.EnsureIdentifierRequest{
		UserId:        &commonv1.UUID{Value: claims.UserID().String()},
		Kind:          kind,
		VerifiedEmail: body.VerifiedEmail,
		Subject:       body.Subject,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// RemoveMyIdentifier unbinds a single identifier from the caller's own
// account. The identifier id is the path parameter; the user id comes
// from the bearer token so a caller cannot scrub another user's
// identifiers by guessing UUIDs.
//
//	DELETE /api/v1/me/identifiers/{id}  ->  200 {remaining:[...]}
//	                                       401 unauthenticated
//	                                       404 not_found (not bound to caller)
//	                                       409 last_identifier (would orphan)
func (a *API) RemoveMyIdentifier(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	identifierID := chi.URLParam(r, "id")
	if identifierID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "identifier id required")
		return
	}
	resp, err := a.Clients.Identity.RemoveIdentifier(r.Context(), &identityv1.RemoveIdentifierRequest{
		UserId:       &commonv1.UUID{Value: claims.UserID().String()},
		IdentifierId: &commonv1.UUID{Value: identifierID},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// DeleteMyAccount soft-deletes the caller's user record + revokes every
// session. Requires the body to carry a step_up_token minted within the
// last 5 minutes via /api/v1/account/step-up/*; the dummy "confirmation
// phrase" the panel collects exists only to defend against accidental
// clicks — the cryptographic guard is the step-up token.
//
//	DELETE /api/v1/me  { step_up_token, reason? }
//	-> 200 {deleted_at, sessions_revoked}
//	   401 unauthenticated
//	   403 step_up_required (no/expired step_up_token)
func (a *API) DeleteMyAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body struct {
		StepUpToken string `json:"step_up_token"`
		Reason      string `json:"reason"`
	}
	// Body is optional — we still call identity-svc which will reject
	// the request when step_up_token is missing. Keeping the decode
	// best-effort means the surface still returns the upstream's
	// canonical error envelope.
	_ = decodeJSON(r, &body)
	resp, err := a.Clients.Identity.DeleteAccount(r.Context(), &identityv1.DeleteAccountRequest{
		UserId:      &commonv1.UUID{Value: claims.UserID().String()},
		StepUpToken: body.StepUpToken,
		Reason:      body.Reason,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
