package handlers

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

// GetNotificationPrefs + SaveNotificationPrefs are the gateway-bff side
// of the /account/notifications surface (Refs #631). They forward to
// identity-svc's chi-router routes (GET/PUT /v1/me/notification-prefs)
// via the service-token shim that authenticates every gateway-bff →
// identity-svc call — the same pattern SetMyPreferredLandingRole uses.
//
//	GET  /api/v1/account/notifications  → 200 { "prefs": {...} | null }
//	POST /api/v1/account/notifications  { "prefs": {...} } → 204
//
// We forward the JSON body verbatim rather than round-tripping it
// through a protobuf message, so the stdlib-json / protobuf-es
// serialization mismatch (snake_case vs camelCase, enum-as-int) never
// applies — the payload is a free-form preferences object that both the
// web client and identity-svc treat as opaque JSON.

// GetNotificationPrefs forwards GET /api/v1/account/notifications to
// identity-svc's GET /v1/me/notification-prefs.
func (a *API) GetNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	a.forwardNotificationPrefs(w, r, http.MethodGet, nil)
}

// SaveNotificationPrefs forwards POST /api/v1/account/notifications to
// identity-svc's PUT /v1/me/notification-prefs. The browser POSTs (our
// REST client only does GET/POST); identity-svc models the write as an
// idempotent PUT, so we translate the verb here.
func (a *API) SaveNotificationPrefs(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	a.forwardNotificationPrefs(w, r, http.MethodPut, body)
}

// forwardNotificationPrefs is the shared service-token-shim forward used
// by both the read + write halves above.
func (a *API) forwardNotificationPrefs(w http.ResponseWriter, r *http.Request, method string, body []byte) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}

	fwd := a.Clients.IdentityRaw
	if fwd.BaseURL == "" || fwd.ServiceToken == "" || fwd.HTTPClient == nil {
		writeError(w, http.StatusServiceUnavailable, "identity_svc_unavailable",
			"identity-svc forwarder not configured")
		return
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	upstream, err := http.NewRequestWithContext(
		r.Context(),
		method,
		strings.TrimRight(fwd.BaseURL, "/")+"/v1/me/notification-prefs",
		reqBody,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	upstream.Header.Set("Authorization", "Bearer "+fwd.ServiceToken)
	upstream.Header.Set("X-Iogrid-User-Id", claims.UserID().String())
	upstream.Header.Set("Content-Type", "application/json")

	resp, err := fwd.HTTPClient.Do(upstream)
	if err != nil {
		writeError(w, http.StatusBadGateway, "identity_svc_unreachable", err.Error())
		return
	}
	defer resp.Body.Close()

	// Pass through verbatim — identity-svc's response + error envelopes
	// are the same shape gateway-bff uses, so no re-wrapping is needed.
	respBody, _ := io.ReadAll(resp.Body)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	if len(respBody) > 0 {
		_, _ = w.Write(respBody)
	}
}
