// Package handlers wires the auth.Service onto HTTP routes. Endpoints
// follow the AuthService / IdentityService proto contracts so a future
// Connect-Go bridge is a thin shim over these JSON handlers.
package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/auth"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// authedUserID extracts the bearer-token user from the request context.
// Returns (zero, false) when no valid Authorization header was present.
func authedUserID(r *http.Request) (uuid.UUID, bool) {
	return authmw.AuthedUser(r.Context())
}

// API is the wired HTTP surface; one per process.
type API struct {
	Auth   *auth.Service
	Store  *store.Store
	Logger *slog.Logger
}

// New constructs an API.
func New(svc *auth.Service, st *store.Store, logger *slog.Logger) *API {
	if logger == nil {
		logger = slog.Default()
	}
	return &API{Auth: svc, Store: st, Logger: logger}
}

// Mount attaches all routes onto the supplied chi router under /v1.
func (a *API) Mount(r chi.Router) {
	r.Route("/v1", func(r chi.Router) {
		r.Get("/", a.index)

		r.Route("/auth", func(r chi.Router) {
			r.Post("/google/start", a.startGoogle)
			r.Post("/google/complete", a.completeGoogle)
			// GET form supports redirecting straight from Google's
			// callback into the browser, which is how real apps wire
			// the flow.
			r.Get("/google/callback", a.completeGoogleGET)

			r.Post("/magic-link/request", a.requestMagicLink)
			r.Post("/magic-link/complete", a.completeMagicLink)
			r.Get("/magic-link/complete", a.completeMagicLinkGET)

			r.Post("/refresh", a.refresh)
			r.Post("/sign-out", a.signOut)

			r.Post("/step-up/request", a.requestStepUp)
			r.Post("/step-up/complete", a.completeStepUp)
		})

		r.Route("/sessions", func(r chi.Router) {
			r.Get("/", a.listSessions)
			r.Delete("/{id}", a.revokeSession)
		})

		r.Route("/users", func(r chi.Router) {
			r.Get("/{id}", a.getUser)
			r.Patch("/{id}", a.updateUser)
		})
	})
}

// --- shared HTTP helpers -------------------------------------------------

// errResp is the JSON envelope returned for non-2xx responses.
type errResp struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errResp{Code: code, Message: msg})
}

func bundleToJSON(b *auth.Bundle) map[string]any {
	if b == nil {
		return nil
	}
	user := map[string]any{}
	if b.User != nil {
		user = userToJSON(b.User)
	}
	return map[string]any{
		"access_token":             b.AccessToken,
		"access_token_expires_at":  b.AccessTokenExpiresAt.UTC().Format(time.RFC3339Nano),
		"refresh_token":            b.RefreshToken,
		"refresh_token_expires_at": b.RefreshTokenExpiresAt.UTC().Format(time.RFC3339Nano),
		"user":                     user,
		"new_user":                 b.NewUser,
		"merged":                   b.Merged,
	}
}

func userToJSON(u *store.User) map[string]any {
	out := map[string]any{
		"id":            u.ID.String(),
		"primary_email": u.PrimaryEmail,
		"display_name":  u.DisplayName,
		"picture_url":   u.PictureURL,
		"roles":         u.Roles,
		"created_at":    u.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":    u.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	if u.LastLoginAt != nil {
		out["last_login_at"] = u.LastLoginAt.UTC().Format(time.RFC3339Nano)
	}
	if u.DeletedAt != nil {
		out["deleted_at"] = u.DeletedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

// --- index --------------------------------------------------------------

func (a *API) index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "identity-svc",
		"status":  "ok",
	})
}

// --- Google -------------------------------------------------------------

type startGoogleReq struct {
	ReturnTo string `json:"return_to"`
}

func (a *API) startGoogle(w http.ResponseWriter, r *http.Request) {
	var req startGoogleReq
	if r.Body != http.NoBody {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	url, state, err := a.Auth.StartGoogle(r.Context(), req.ReturnTo)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"authorize_url": url,
		"state":         state,
	})
}

type completeGoogleReq struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

func (a *API) completeGoogle(w http.ResponseWriter, r *http.Request) {
	var req completeGoogleReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	bundle, err := a.Auth.CompleteGoogle(r.Context(), req.Code, req.State, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bundleToJSON(bundle))
}

// completeGoogleGET handles the redirect-from-Google callback. Same logic
// as the POST form; reads `code` + `state` from the query string.
func (a *API) completeGoogleGET(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	bundle, err := a.Auth.CompleteGoogle(r.Context(), code, state, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bundleToJSON(bundle))
}

// --- magic-link --------------------------------------------------------

type requestMagicLinkReq struct {
	Email    string `json:"email"`
	ReturnTo string `json:"return_to"`
}

func (a *API) requestMagicLink(w http.ResponseWriter, r *http.Request) {
	var req requestMagicLinkReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	resp, err := a.Auth.RequestMagicLink(r.Context(), req.Email, req.ReturnTo, sourceIP(r), store.IntentSignIn)
	if err != nil {
		if strings.Contains(err.Error(), "too many") {
			writeError(w, http.StatusTooManyRequests, "rate_limited", err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"accepted":   resp.Accepted,
		"expires_in": int(resp.ExpiresIn.Seconds()),
	})
}

type completeMagicLinkReq struct {
	Token string `json:"token"`
}

func (a *API) completeMagicLink(w http.ResponseWriter, r *http.Request) {
	var req completeMagicLinkReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	bundle, err := a.Auth.CompleteMagicLink(r.Context(), req.Token, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bundleToJSON(bundle))
}

// completeMagicLinkGET handles the click-from-email flow — token is in
// the URL query string. Real browser deployments redirect to the
// management-plane after success; we keep it simple and return JSON.
func (a *API) completeMagicLinkGET(w http.ResponseWriter, r *http.Request) {
	bundle, err := a.Auth.CompleteMagicLink(r.Context(), r.URL.Query().Get("token"), r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bundleToJSON(bundle))
}

// --- session lifecycle -------------------------------------------------

type refreshReq struct {
	RefreshToken string `json:"refresh_token"`
}

func (a *API) refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	bundle, err := a.Auth.Refresh(r.Context(), req.RefreshToken, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, bundleToJSON(bundle))
}

type signOutReq struct {
	RefreshToken string `json:"refresh_token"`
}

func (a *API) signOut(w http.ResponseWriter, r *http.Request) {
	var req signOutReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	if err := a.Auth.SignOut(r.Context(), req.RefreshToken); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *API) listSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUserID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	sessions, err := a.Auth.ListSessions(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		entry := map[string]any{
			"id":           s.ID.String(),
			"user_agent":   s.UserAgent,
			"created_at":   s.CreatedAt.UTC().Format(time.RFC3339Nano),
			"last_used_at": s.LastUsedAt.UTC().Format(time.RFC3339Nano),
			"expires_at":   s.ExpiresAt.UTC().Format(time.RFC3339Nano),
		}
		if s.IP != nil {
			entry["ip_address"] = s.IP.String()
		}
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
}

func (a *API) revokeSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUserID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad session id")
		return
	}
	if err := a.Auth.RevokeSession(r.Context(), userID, id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// --- step-up -----------------------------------------------------------

type requestStepUpReq struct {
	Reason string `json:"reason"`
}

func (a *API) requestStepUp(w http.ResponseWriter, r *http.Request) {
	userID, ok := authedUserID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	if err := a.Auth.RequestStepUp(r.Context(), userID, sourceIP(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"magic_link_sent": true})
}

type completeStepUpReq struct {
	MagicLinkToken string `json:"magic_link_token"`
}

func (a *API) completeStepUp(w http.ResponseWriter, r *http.Request) {
	var req completeStepUpReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	bundle, err := a.Auth.CompleteMagicLink(r.Context(), req.MagicLinkToken, r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error())
		return
	}
	// Note: completeStepUpTx (inside CompleteMagicLink) flips
	// step_up_until on the freshest session; the access token here is
	// the one we just rotated.
	if bundle == nil {
		// Pure step-up redemption — return a 204.
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	writeJSON(w, http.StatusOK, bundleToJSON(bundle))
}

// --- users -------------------------------------------------------------

func (a *API) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad user id")
		return
	}
	user, err := a.Store.GetUser(r.Context(), nil, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	identifiers, err := a.Store.ListIdentifiersForUser(r.Context(), nil, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	idJSON := make([]map[string]any, 0, len(identifiers))
	for _, i := range identifiers {
		idJSON = append(idJSON, map[string]any{
			"id":             i.ID.String(),
			"kind":           string(i.Kind),
			"subject":        i.Subject,
			"email":          i.Email,
			"verified":       i.Verified,
			"hosted_domain":  i.HostedDomain,
			"created_at":     i.CreatedAt.UTC().Format(time.RFC3339Nano),
			"last_used_at":   i.LastUsedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        userToJSON(user),
		"identifiers": idJSON,
	})
}

type updateUserReq struct {
	DisplayName  string `json:"display_name"`
	PrimaryEmail string `json:"primary_email"`
	PictureURL   string `json:"picture_url"`
}

func (a *API) updateUser(w http.ResponseWriter, r *http.Request) {
	authedID, ok := authedUserID(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad user id")
		return
	}
	if id != authedID {
		writeError(w, http.StatusForbidden, "permission_denied", "cannot edit another user")
		return
	}
	var req updateUserReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	user, err := a.Store.UpdateUserProfile(r.Context(), nil, id, req.DisplayName, req.PrimaryEmail, req.PictureURL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, userToJSON(user))
}

// --- helpers -----------------------------------------------------------

// sourceIP returns the best-effort source IP for rate-limit keying.
// Prefers X-Real-IP (set by the shared middleware.RealIP wrapper) and
// falls back to RemoteAddr.
func sourceIP(r *http.Request) string {
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	return strings.SplitN(r.RemoteAddr, ":", 2)[0]
}
