package server

import (
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// MoonPay webhooks send their signature in the `Moonpay-Signature-V2`
// header; Sociable Cash sends a bare hex in `Cash-Signature`. The
// dispatch is provider-aware — we try the typed header first, fall back
// to a generic `X-Webhook-Signature`.
const (
	moonpaySignatureHeader = "Moonpay-Signature-V2"
	cashSignatureHeader    = "Cash-Signature"
	genericSignatureHeader = "X-Webhook-Signature"
)

// startOffRampReq mirrors gateway-bff's JSON body for POST /api/v1/offramp/start.
type startOffRampReq struct {
	UserID        string `json:"user_id"`
	ProviderName  string `json:"provider_name"`
	WalletAddress string `json:"wallet_address"`
	// GridAmount is a uint64 lamport count carried as a JSON number;
	// $GRID supply (1e18 lamports) fits within Number.MAX_SAFE_INTEGER
	// (2^53) for any realistic per-user balance.
	GridAmount   uint64 `json:"grid_amount"`
	FiatCurrency string `json:"fiat_currency"`
	ReturnURL    string `json:"return_url"`
}

// startOffRamp persists a pending row and returns the partner redirect URL.
//
//	POST /v1/offramp/start
//	  { user_id, provider_name, wallet_address, grid_amount, fiat_currency, return_url }
//	→ { request_id, redirect_url }
func (h *handlers) startOffRamp(w http.ResponseWriter, r *http.Request) {
	if h.deps.OffRamp == nil {
		writeErr(w, http.StatusServiceUnavailable, "offramp disabled")
		return
	}
	var req startOffRampReq
	if err := decodeJSON(r.Body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	uid, err := uuid.Parse(req.UserID)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	out, err := h.deps.OffRamp.StartOffRamp(r.Context(), offramp.StartOffRampInput{
		UserID:        uid,
		ProviderName:  req.ProviderName,
		WalletAddress: req.WalletAddress,
		GridAmount:    req.GridAmount,
		FiatCurrency:  req.FiatCurrency,
		ReturnURL:     req.ReturnURL,
	})
	if err != nil {
		if errors.Is(err, offramp.ErrUnknownProvider) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"request_id":   out.RequestID.String(),
		"redirect_url": out.RedirectURL,
	})
}

// getOffRampStatus returns the persisted row for a request id.
//
//	GET /v1/offramp/status/{request_id}
func (h *handlers) getOffRampStatus(w http.ResponseWriter, r *http.Request) {
	if h.deps.OffRamp == nil {
		writeErr(w, http.StatusServiceUnavailable, "offramp disabled")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "requestID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request id")
		return
	}
	row, err := h.deps.OffRamp.GetStatus(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "off-ramp request not found")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, offRampRowToJSON(row))
}

// listOffRampProviders enumerates registered adapters so the web UI
// knows which providers to show in the picker.
//
//	GET /v1/offramp/providers → { providers: [{ name, ... }] }
func (h *handlers) listOffRampProviders(w http.ResponseWriter, _ *http.Request) {
	if h.deps.OffRamp == nil {
		writeJSON(w, http.StatusOK, map[string]any{"providers": []any{}})
		return
	}
	providers := h.deps.OffRamp.Registry().ListAvailable()
	out := make([]map[string]string, 0, len(providers))
	for _, p := range providers {
		out = append(out, map[string]string{"name": p.Name()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": out})
}

// offRampWebhook dispatches a partner webhook to the right adapter's
// VerifyWebhookSignature + ParseWebhook + persist path.
//
//	POST /v1/offramp/webhook/{provider_name}
func (h *handlers) offRampWebhook(w http.ResponseWriter, r *http.Request) {
	if h.deps.OffRamp == nil {
		writeErr(w, http.StatusServiceUnavailable, "offramp disabled")
		return
	}
	providerName := chi.URLParam(r, "providerName")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body")
		return
	}
	sig := signatureForProvider(providerName, r.Header)
	_, err = h.deps.OffRamp.HandleWebhook(r.Context(), providerName, body, sig)
	if errors.Is(err, offramp.ErrUnknownProvider) {
		writeErr(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, offramp.ErrInvalidSignature) {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// signatureForProvider pulls the partner-specific signature header, with
// a generic fallback. Returning "" makes the adapter reject the webhook
// (every adapter requires a non-empty signature).
func signatureForProvider(name string, h http.Header) string {
	switch name {
	case "moonpay":
		if v := h.Get(moonpaySignatureHeader); v != "" {
			return v
		}
	case "sociable-cash":
		if v := h.Get(cashSignatureHeader); v != "" {
			return v
		}
	}
	return h.Get(genericSignatureHeader)
}

// offRampRowToJSON renders a store.OffRampRequest as the JSON envelope
// gateway-bff forwards to the browser. Fields use snake_case to match
// the rest of billing-svc's REST surface.
func offRampRowToJSON(r *store.OffRampRequest) map[string]any {
	out := map[string]any{
		"request_id":     r.ID.String(),
		"user_id":        r.UserID.String(),
		"provider_name":  r.ProviderName,
		"wallet_address": r.WalletAddress,
		"grid_amount":    r.GridAmount,
		"fiat_currency":  r.FiatCurrency,
		"status":         r.Status,
		"redirect_url":   r.RedirectURL,
		"created_at":     r.CreatedAt.Format(time.RFC3339),
		"updated_at":     r.UpdatedAt.Format(time.RFC3339),
	}
	if r.ProviderRefID != nil {
		out["provider_ref_id"] = *r.ProviderRefID
	}
	if r.FiatAmount != nil {
		out["fiat_amount"] = *r.FiatAmount
	}
	if r.ReturnURL != nil {
		out["return_url"] = *r.ReturnURL
	}
	if r.TxnSignature != nil {
		out["txn_signature"] = *r.TxnSignature
	}
	if r.ErrorMessage != nil {
		out["error_message"] = *r.ErrorMessage
	}
	if r.CompletedAt != nil {
		out["completed_at"] = r.CompletedAt.Format(time.RFC3339)
	}
	return out
}
