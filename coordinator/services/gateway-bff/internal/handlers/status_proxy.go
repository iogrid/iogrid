package handlers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// Status proxy (#674): the public iogrid.org/status dashboard needs the
// telemetry-svc posture + uptime feeds, but telemetry-svc is cluster-
// internal. These handlers re-publish exactly two read-only GET feeds,
// unauthenticated by design (a status page must work while sign-in is
// down). No request body, headers, or query params are forwarded — the
// upstream paths are pinned so this can never become an open proxy.

// statusProxyClient keeps the timeout tighter than the default
// downstream budget: a status page that hangs is worse than one that
// errors fast (the island falls back to "status feed unavailable").
var statusProxyClient = &http.Client{Timeout: 5 * time.Second}

// StatusPosture proxies GET /status/posture → telemetry-svc.
//
//	GET /status/posture  ->  200 {schema_version, generated_at, overall,
//	                              services[], incidents_active[], incidents_recent[]}
func (a *API) StatusPosture(w http.ResponseWriter, r *http.Request) {
	a.proxyStatusFeed(w, r, "/status/posture")
}

// StatusUptime proxies GET /status/uptime → telemetry-svc.
//
//	GET /status/uptime  ->  200 (uptime ledger JSON)
func (a *API) StatusUptime(w http.ResponseWriter, r *http.Request) {
	a.proxyStatusFeed(w, r, "/status/uptime")
}

func (a *API) proxyStatusFeed(w http.ResponseWriter, r *http.Request, path string) {
	base := strings.TrimRight(a.TelemetrySvcURL, "/")
	if base == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "status feed not configured")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "status feed request build failed")
		return
	}
	resp, err := statusProxyClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_unavailable", "status feed unreachable")
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	// Short shared cache: the posture generator is cheap but the edge
	// shouldn't hammer telemetry-svc when the page is hot.
	w.Header().Set("Cache-Control", "public, max-age=15")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
