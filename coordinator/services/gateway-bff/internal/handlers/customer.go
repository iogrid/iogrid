package handlers

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/sse"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
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
