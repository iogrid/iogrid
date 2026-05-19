package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

// OffRampProxy is the thin BFF surface that forwards /api/v1/offramp/*
// to billing-svc's /v1/offramp/* endpoints.
//
// The BFF does NOT implement off-ramp logic — billing-svc owns the
// provider registry, persistence, and webhook validation. The BFF
// merely:
//
//   - enforces auth on /start + /status routes
//   - leaves /webhook/{provider_name} unauthed (partners can't carry a
//     bearer token; the partner's signature is the auth)
//   - normalises the JSON envelope shape between the two services
type OffRampProxy struct {
	BaseURL string // e.g. http://billing-svc:8080
	HTTP    *http.Client
}

// NewOffRampProxy constructs a proxy. httpClient is shared with the
// rest of the BFF for connection pooling.
func NewOffRampProxy(baseURL string, httpClient *http.Client) *OffRampProxy {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &OffRampProxy{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    httpClient,
	}
}

// startOffRampBody is the JSON the browser POSTs. We accept either
// snake_case or camelCase keys (front-end uses camelCase by convention).
type startOffRampBody struct {
	ProviderName  string `json:"provider_name"`
	WalletAddress string `json:"wallet_address"`
	GridAmount    uint64 `json:"grid_amount"`
	FiatCurrency  string `json:"fiat_currency"`
	ReturnURL     string `json:"return_url"`
}

// StartOffRamp handles POST /api/v1/offramp/start.
func (a *API) StartOffRamp(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	if a.OffRamp == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "off-ramp proxy not configured")
		return
	}
	var body startOffRampBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(body.ProviderName) == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider_name required")
		return
	}

	upstream := map[string]any{
		"user_id":        claims.UserID().String(),
		"provider_name":  body.ProviderName,
		"wallet_address": body.WalletAddress,
		"grid_amount":    body.GridAmount,
		"fiat_currency":  body.FiatCurrency,
		"return_url":     body.ReturnURL,
	}
	a.OffRamp.proxyJSON(w, r, http.MethodPost, "/v1/offramp/start", upstream)
}

// GetOffRampStatus handles GET /api/v1/offramp/status/{request_id}.
func (a *API) GetOffRampStatus(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	if a.OffRamp == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "off-ramp proxy not configured")
		return
	}
	id := chi.URLParam(r, "requestID")
	if id == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "request_id required")
		return
	}
	a.OffRamp.proxyJSON(w, r, http.MethodGet, "/v1/offramp/status/"+id, nil)
}

// ListOffRampProviders handles GET /api/v1/offramp/providers.
func (a *API) ListOffRampProviders(w http.ResponseWriter, r *http.Request) {
	if a.OffRamp == nil {
		writeJSON(w, http.StatusOK, map[string]any{"providers": []any{}})
		return
	}
	a.OffRamp.proxyJSON(w, r, http.MethodGet, "/v1/offramp/providers", nil)
}

// HandleOffRampWebhook handles POST /api/v1/webhooks/offramp/{provider_name}.
//
// This route is INTENTIONALLY UNAUTHED — partners post directly with
// their signature header. The BFF forwards the raw body + the signature
// header so billing-svc's adapter can verify it.
func (a *API) HandleOffRampWebhook(w http.ResponseWriter, r *http.Request) {
	if a.OffRamp == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "off-ramp proxy not configured")
		return
	}
	provider := chi.URLParam(r, "providerName")
	if provider == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "provider_name required")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "read body")
		return
	}
	upstreamURL := a.OffRamp.BaseURL + "/v1/offramp/webhook/" + provider
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// Forward whichever signature header the partner sent. We pass every
	// `*-Signature*` header so adapter-specific names (Moonpay-Signature-V2,
	// Cash-Signature, X-Webhook-Signature) all reach billing-svc.
	for key, vals := range r.Header {
		if strings.Contains(strings.ToLower(key), "signature") {
			for _, v := range vals {
				req.Header.Add(key, v)
			}
		}
	}
	resp, err := a.OffRamp.HTTP.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	defer resp.Body.Close()
	copyResponse(w, resp)
}

// proxyJSON forwards a request to billing-svc and copies the upstream
// JSON response verbatim. Used for /offramp/start, /status, /providers.
func (p *OffRampProxy) proxyJSON(w http.ResponseWriter, r *http.Request, method, path string, body any) {
	upstreamURL := p.BaseURL + path
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "encode_error", err.Error())
			return
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, upstreamURL, bodyReader)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := p.HTTP.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_error", err.Error())
		return
	}
	defer resp.Body.Close()
	copyResponse(w, resp)
}

// copyResponse pipes an upstream response into our ResponseWriter.
func copyResponse(w http.ResponseWriter, resp *http.Response) {
	for k, vs := range resp.Header {
		// Skip hop-by-hop headers; chi sets Content-Type itself.
		if strings.EqualFold(k, "Connection") {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
