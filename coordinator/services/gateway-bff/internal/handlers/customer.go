package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/sse"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
)

// CreateAPIKey issues a fresh customer API key. The plaintext is
// returned ONCE — clients must persist it client-side, the server will
// never reveal it again.
//
//	POST /api/v1/customer/api-keys
//	  { workspace_id, label }
//	-> 201 { id, workspace_id, label, prefix, created_at, plaintext }
func (a *API) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body struct {
		WorkspaceID string `json:"workspace_id"`
		Label       string `json:"label"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	wsID, ok := parseUUIDParam(w, body.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	k, err := a.APIKeyStore.Create(r.Context(), wsID, body.Label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, k)
}

// ListAPIKeys returns every key for a workspace (plaintexts stripped).
//
//	GET /api/v1/customer/api-keys?workspace_id=<UUID>
func (a *API) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	wsID, ok := parseUUIDParam(w, r.URL.Query().Get("workspace_id"), "workspace_id")
	if !ok {
		return
	}
	keys, err := a.APIKeyStore.List(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// DeleteAPIKey revokes a key by id.
//
//	DELETE /api/v1/customer/api-keys/{id}?workspace_id=<UUID>
func (a *API) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	id, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	wsID, ok := parseUUIDParam(w, r.URL.Query().Get("workspace_id"), "workspace_id")
	if !ok {
		return
	}
	if err := a.APIKeyStore.Delete(r.Context(), wsID, id); err != nil {
		if errors.Is(err, ErrAPIKeyNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "api key not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "store_error", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetCustomerUsage returns metering aggregates from billing-svc.
//
//	GET /api/v1/customer/usage?workspace_id=<UUID>&start=<ISO>&end=<ISO>
func (a *API) GetCustomerUsage(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	wsID, ok := parseUUIDParam(w, r.URL.Query().Get("workspace_id"), "workspace_id")
	if !ok {
		return
	}
	req := &billingv1.ListUsageRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID.String()},
		Page:        &commonv1.PageRequest{PageSize: 100},
	}
	if window := parseTimeWindow(r); window != nil {
		req.Window = window
	}
	resp, err := a.Clients.Billing.ListUsage(r.Context(), req)
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// SubmitWorkload forwards a customer workload submission.
//
//	POST /api/v1/customer/workloads
//	  { workload: { ...full Workload payload... } }
func (a *API) SubmitWorkload(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body struct {
		Workload *workloadsv1.Workload `json:"workload"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.Workload == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "workload required")
		return
	}
	// Stamp a workload id if the caller didn't.
	if body.Workload.Id == nil || body.Workload.Id.Value == "" {
		body.Workload.Id = &commonv1.UUID{Value: uuid.NewString()}
	}
	resp, err := a.Clients.Workloads.SubmitWorkload(r.Context(), &workloadsv1.SubmitWorkloadRequest{
		Workload: body.Workload,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// StreamWorkloadEvents pushes per-workload status updates as SSE.
//
//	GET /api/v1/customer/workloads/{id}/events  (SSE)
func (a *API) StreamWorkloadEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	id, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	sse.Handler(sse.ProducerFunc(func(ctx context.Context, lastEventID string, emit func(sse.Event) error) error {
		stream, err := a.Clients.Workloads.StreamWorkloadEvents(ctx, &workloadsv1.StreamWorkloadEventsRequest{
			Id: &commonv1.UUID{Value: id.String()},
		})
		if err != nil {
			return err
		}
		defer stream.Close()
		for stream.Receive() {
			ev := stream.Msg()
			if ev == nil {
				continue
			}
			if err := emit(sse.Event{
				Kind:     "workload_event",
				DataJSON: ev,
			}); err != nil {
				return err
			}
		}
		return stream.Err()
	}), 15*time.Second).ServeHTTP(w, r)
}

// ListCustomerVPNSessions proxies to vpn-svc /v1/vpn/customers/{customer_id}/sessions.
//
// Customer ID is taken from the authenticated session — NOT from any
// wire param — so a user can only see their own sessions even if they
// craft a URL with someone else's customer_id.
//
//	GET /api/v1/customer/vpn/sessions
//
// Refs #541.
func (a *API) ListCustomerVPNSessions(w http.ResponseWriter, r *http.Request) {
	authCtx, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	customerID := authCtx.UserID().String()
	if customerID == "" || customerID == "00000000-0000-0000-0000-000000000000" {
		writeError(w, http.StatusInternalServerError, "no_customer_id", "auth context missing user id")
		return
	}
	if a.VPNSvcBaseURL == "" {
		// vpn-svc URL not wired — return an empty list so the web UI
		// renders the "no sessions" empty state instead of an error toast.
		writeJSON(w, http.StatusOK, map[string]any{
			"customer_id": customerID,
			"sessions":    []any{},
			"count":       0,
		})
		return
	}
	url := a.VPNSvcBaseURL + "/v1/vpn/customers/" + customerID + "/sessions"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "request_build", err.Error())
		return
	}
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "vpn_svc_unreachable", err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// GetCustomerBalance returns the caller's prepaid $GRID balance + the
// grace-overage owed (founder-ruled prepaid + capped-grace model, #632).
//
//	GET /api/v1/customer/billing/balance
//	-> 200 { wallet, balance_atomic, balance_grid,
//	         grace_overage_owed_atomic, grace_overage_cap_atomic,
//	         available_atomic }
//	   409 no_wallet_bound  — user must bind a Solana wallet first
//	   503 unavailable      — billing-svc URL not wired / $GRID pre-TGE
//
// It resolves the caller's bound wallet (identity-svc) then forwards to
// billing-svc /v1/grid/balance, which owns the Solana RPC read + the
// unsettled-arrears query. Anti-fake-state (#417): on any upstream
// failure we propagate the error status rather than emitting a fake $0
// balance — the web surface shows an explicit banner.
func (a *API) GetCustomerBalance(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	if strings.TrimSpace(a.BillingSvcBaseURL) == "" {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"billing-svc not configured (BILLING_SVC_URL unset)")
		return
	}
	ctx := clients.WithCallerClaims(r.Context(), claims)
	wresp, err := a.Clients.Auth.ListBoundWallets(ctx, &identityv1.ListBoundWalletsRequest{})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	var wallet string
	for _, b := range wresp.GetBindings() {
		if addr := strings.TrimSpace(b.GetAddress()); addr != "" {
			wallet = addr
			break
		}
	}
	if wallet == "" {
		// No wallet bound — the user can't hold or top up $GRID yet. This
		// is an actionable state (bind a wallet), NOT an outage; surface it
		// distinctly so the panel can render a "connect wallet" CTA.
		writeError(w, http.StatusConflict, "no_wallet_bound",
			"bind a Solana wallet to view your prepaid $GRID balance")
		return
	}
	upstream := strings.TrimRight(a.BillingSvcBaseURL, "/") +
		"/v1/grid/balance?wallet=" + url.QueryEscape(wallet)
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "request_build", err.Error())
		return
	}
	client := a.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "billing_svc_unreachable", err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
