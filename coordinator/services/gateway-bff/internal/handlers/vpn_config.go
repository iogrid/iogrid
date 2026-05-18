package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// VPNGatewayURL is the in-cluster base URL of the vpn-gateway service.
// Set at process boot from config; defaults to the k8s DNS name.
//
// We keep this as a per-API field on a small VPNGatewayProxy struct so
// tests can inject httptest.Server.URL without touching the global API
// surface. (The rest of the BFF talks to downstream services via the
// typed clients in internal/clients; vpn-gateway exposes only HTTP/JSON
// so a typed Connect client would be over-engineered.)

// VPNGatewayProxy is the HTTP transport for the vpn-gateway control
// plane. Constructed at boot in main(), assigned to the API instance,
// and used by GetVPNConfigForPlatform.
type VPNGatewayProxy struct {
	BaseURL string
	Client  *http.Client
}

// NewVPNGatewayProxy returns a proxy with a sensible HTTP client
// (15s timeout). Used by main() when VPN_GATEWAY_URL is set.
func NewVPNGatewayProxy(baseURL string) *VPNGatewayProxy {
	return &VPNGatewayProxy{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Client:  &http.Client{Timeout: 15 * time.Second},
	}
}

// GetVPNConfigForPlatform proxies the call to vpn-gateway's
// /v1/config/render endpoint and streams the artefact back to the
// browser.
//
//	GET /api/v1/vpn/config-for-platform?customer_id=<UUID>&platform=ios
//	-> 200 [.mobileconfig payload]  Content-Disposition: attachment
//	   401 unauthenticated
//	   400 bad_request
//	   502 upstream_error
//
// Supported platforms: ios, android, mac, windows, linux.
//
// We deliberately do NOT generate the customer's WG private key
// server-side here. The BFF assumes the client supplied it on the way
// in (or the customer pre-registered their pubkey at signup time);
// vpn-gateway accepts an empty private-key field and renders a
// commented-out placeholder the customer fills in.
func (a *API) GetVPNConfigForPlatform(w http.ResponseWriter, r *http.Request) {
	if a.VPNGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "vpn_gateway_unavailable", "vpn-gateway not configured")
		return
	}
	customerID := strings.TrimSpace(r.URL.Query().Get("customer_id"))
	platform := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("platform")))
	if customerID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "customer_id required")
		return
	}
	if platform == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "platform required (ios|android|mac|windows|linux)")
		return
	}
	switch platform {
	case "ios", "android", "mac", "macos", "windows", "win", "linux":
	default:
		writeError(w, http.StatusBadRequest, "bad_request", "unsupported platform: "+platform)
		return
	}

	body, err := json.Marshal(map[string]string{
		"customer_id": customerID,
		"platform":    platform,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	endpoint, err := url.JoinPath(a.VPNGateway.BaseURL, "/v1/config/render")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.VPNGateway.Client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", "vpn-gateway: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Forward the vpn-gateway's JSON error envelope verbatim with the
		// same status. Read body, then write — we can't stream because
		// we need to set headers before WriteHeader.
		b, _ := io.ReadAll(resp.Body)
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(b)
		return
	}
	// Pass through Content-Disposition + Content-Type so the browser
	// triggers the right download UI.
	if v := resp.Header.Get("Content-Type"); v != "" {
		w.Header().Set("Content-Type", v)
	}
	if v := resp.Header.Get("Content-Disposition"); v != "" {
		w.Header().Set("Content-Disposition", v)
	}
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, resp.Body); err != nil {
		// At this point the headers are already on the wire; only useful
		// thing we can do is log via the API logger.
		a.Logger.Warn(fmt.Sprintf("vpn config-for-platform copy: %s", err.Error()))
	}
}
