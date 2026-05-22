package handlers

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

// SetMyPreferredLandingRole is the gateway-bff side of the EPIC #422
// /welcome picker: forwards the JSON payload to identity-svc's
// chi-router route (PUT /v1/me/preferred-landing-role) via the
// service-token shim that authenticates every gateway-bff →
// identity-svc call.
//
//	PUT /api/v1/me/preferred-landing-role
//	  { "role": "provider" | "customer" | "vpn" | "" }
//	→ 204 No Content (identity-svc passthrough)
//	→ 400 invalid_argument on bad role
//	→ 401 unauthenticated when no bearer
//	→ 503 identity_svc_unavailable when forwarder not configured
//
// The forwarder lives on a.Clients.IdentityRaw — configured at boot from
// IDENTITY_SVC_URL + IOGRID_SERVICE_TOKEN env vars. When either is
// empty the route returns 503 so dev environments without the shim
// fail loudly instead of silently 401-ing on every request.
//
// Refs #422 (EPIC), #449 (identity-svc seam), #445 (welcome page).
func (a *API) SetMyPreferredLandingRole(w http.ResponseWriter, r *http.Request) {
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

	// Buffer the body — small JSON object; safe to read fully.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}

	upstream, err := http.NewRequestWithContext(
		r.Context(),
		http.MethodPut,
		strings.TrimRight(fwd.BaseURL, "/")+"/v1/me/preferred-landing-role",
		bytes.NewReader(body),
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

	// Pass through verbatim — identity-svc's error envelope is the same
	// {code, message} shape gateway-bff uses, so we don't need to
	// re-wrap. Forwarding the upstream status keeps the contract
	// transparent.
	respBody, _ := io.ReadAll(resp.Body)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	if len(respBody) > 0 {
		_, _ = w.Write(respBody)
	}
}
