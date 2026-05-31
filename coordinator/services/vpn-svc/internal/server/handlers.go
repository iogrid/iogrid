package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

// APIKeyValidator authenticates a raw customer API key against the upstream
// billing-svc and resolves to a workspace + tier. The proxy-gateway already
// implements this (internal/auth.Connect); vpn-svc reuses the same contract
// to avoid drift. nil = unauthenticated mode (dev / smoke).
type APIKeyValidator interface {
	Validate(ctx context.Context, apiKey string) (workspaceID string, customerID string, err error)
}

// RequestSession handles POST /v1/vpn/sessions
type RequestSession struct {
	st        store.Store
	logger    *slog.Logger
	validator APIKeyValidator // optional — if nil, requests are unauthenticated (dev mode)
}

func NewRequestSession(st store.Store, logger *slog.Logger) *RequestSession {
	return &RequestSession{st: st, logger: logger}
}

// WithValidator wires up API key validation. Call from server.Mount when
// VPN_SVC_BILLING_URL is set to enable per-key auth (#531).
func (h *RequestSession) WithValidator(v APIKeyValidator) *RequestSession {
	h.validator = v
	return h
}

// requestSessionReq is the wire body — superset of pb.RequestVpnSession with
// an api_key field. The proto's api_key_hash is treated as historic + ignored;
// new clients send raw api_key and vpn-svc forwards to billing-svc.
type requestSessionReq struct {
	CustomerID string `json:"customer_id"`
	Region     string `json:"region"`
	APIKey     string `json:"api_key"`
	APIKeyHash string `json:"api_key_hash"` // deprecated, ignored
}

func (h *RequestSession) Handle(w http.ResponseWriter, r *http.Request) {
	req := &requestSessionReq{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.CustomerID == "" || req.Region == "" {
		respondError(w, http.StatusBadRequest, "customer_id and region required")
		return
	}
	customerUUID, err := uuid.Parse(req.CustomerID)
	if err != nil {
		respondError(w, http.StatusBadRequest, "customer_id must be a UUID")
		return
	}

	// API key validation (#531). When validator wired, reject unauthenticated
	// requests; otherwise (dev mode) skip and log a WARN.
	if h.validator != nil {
		if req.APIKey == "" {
			respondError(w, http.StatusUnauthorized, "api_key required")
			return
		}
		wsID, custID, err := h.validator.Validate(r.Context(), req.APIKey)
		if err != nil {
			h.logger.Warn("api key rejected",
				slog.String("customer_id", req.CustomerID),
				slog.String("error", err.Error()))
			respondError(w, http.StatusUnauthorized, "invalid api_key")
			return
		}
		// Trust billing-svc's customer_id over the claimed one — prevents
		// a valid key from being used to spoof another customer's session.
		if custID != "" {
			customerUUID = uuid.MustParse(custID)
		}
		_ = wsID
	} else {
		h.logger.Warn("api key validation skipped (dev mode — set VPN_SVC_BILLING_URL to enable)")
	}

	// Create session in store
	sessionID := uuid.New()
	session := &store.Session{
		ID:         sessionID,
		CustomerID: customerUUID,
		Region:     req.Region,
		State:      pb.VpnSessionState_CREATING,
	}
	if err := h.st.CreateSession(r.Context(), session); err != nil {
		h.logger.Error("create session failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	SessionsCreated.Inc()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{
		"session_id": sessionID.String(),
		"status":     "CREATING",
	})
}

// GetSession handles GET /v1/vpn/sessions/{sessionID}
type GetSession struct {
	st     store.Store
	logger *slog.Logger
}

func NewGetSession(st store.Store, logger *slog.Logger) *GetSession {
	return &GetSession{st: st, logger: logger}
}

func (h *GetSession) Handle(w http.ResponseWriter, r *http.Request) {
	sessionID := uuid.MustParse(chi.URLParam(r, "sessionID"))
	session, err := h.st.GetSession(r.Context(), sessionID)
	if err != nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	// Build a wire-friendly response — Session.State is a proto enum
	// which serialises as an int by default; the SDK expects a string.
	// We surface the assigned provider's ICE candidates here as the
	// customer's initial-tunnel candidate list, plus the provider's
	// WG public key (populated when the daemon BindProvider call lands
	// — until then it's empty and the customer SDK should poll).
	candidates, _ := h.st.GetProviderCandidates(r.Context(), session.CurrentProvider)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id":              session.ID.String(),
		"customer_id":             session.CustomerID.String(),
		"region":                  session.Region,
		"primary_provider_id":     session.PrimaryProvider.String(),
		"current_provider_id":     session.CurrentProvider.String(),
		"state":                   session.State.String(),
		"bytes_in":                session.BytesIn,
		"bytes_out":               session.BytesOut,
		"created_at":              session.CreatedAt,
		"last_activity_at":        session.LastActivityAt,
		"provider_id":             session.CurrentProvider.String(),
		"provider_wg_public_key":  session.ProviderWgPublicKey,
		"customer_wg_public_key":  session.CustomerWgPublicKey,
		"ice_candidates":          candidates,
	})
}

// ConfirmCandidate handles PUT /v1/vpn/sessions/{sessionID}/confirm
type ConfirmCandidate struct {
	st     store.Store
	logger *slog.Logger
}

func NewConfirmCandidate(st store.Store, logger *slog.Logger) *ConfirmCandidate {
	return &ConfirmCandidate{st: st, logger: logger}
}

func (h *ConfirmCandidate) Handle(w http.ResponseWriter, r *http.Request) {
	sessionID := uuid.MustParse(chi.URLParam(r, "sessionID"))
	req := &pb.ConfirmWorkingCandidate{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if err := h.st.ConfirmWorkingCandidate(r.Context(), sessionID, req.ChosenCandidate); err != nil {
		h.logger.Error("confirm candidate failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to confirm candidate")
		return
	}

	if err := h.st.UpdateSessionState(r.Context(), sessionID, pb.VpnSessionState_ESTABLISHING); err != nil {
		h.logger.Error("update state failed", slog.String("error", err.Error()))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "confirmed"})
}

// RefreshSession handles POST /v1/vpn/sessions/{sessionID}/refresh
type RefreshSession struct {
	st     store.Store
	logger *slog.Logger
}

func NewRefreshSession(st store.Store, logger *slog.Logger) *RefreshSession {
	return &RefreshSession{st: st, logger: logger}
}

func (h *RefreshSession) Handle(w http.ResponseWriter, r *http.Request) {
	sessionID := uuid.MustParse(chi.URLParam(r, "sessionID"))
	req := &pb.RefreshVpnSession{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if err := h.st.UpdateSessionMetrics(r.Context(), sessionID, req.BytesIn, req.BytesOut, req.RoamingEvents, req.FailoverCount); err != nil {
		h.logger.Error("refresh failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to refresh session")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	SessionRefreshes.Inc()
	json.NewEncoder(w).Encode(map[string]string{"status": "refreshed"})
}

// TerminateSession handles POST /v1/vpn/sessions/{sessionID}/terminate
type TerminateSession struct {
	st     store.Store
	logger *slog.Logger
}

func NewTerminateSession(st store.Store, logger *slog.Logger) *TerminateSession {
	return &TerminateSession{st: st, logger: logger}
}

func (h *TerminateSession) Handle(w http.ResponseWriter, r *http.Request) {
	sessionID := uuid.MustParse(chi.URLParam(r, "sessionID"))
	req := &pb.TerminateVpnSession{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if err := h.st.TerminateSession(r.Context(), sessionID, req.Reason); err != nil {
		h.logger.Error("terminate failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to terminate session")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "terminated"})
}

// RegisterCandidates handles POST /v1/vpn/providers/{providerID}/candidates
type RegisterCandidates struct {
	st     store.Store
	logger *slog.Logger
}

func NewRegisterCandidates(st store.Store, logger *slog.Logger) *RegisterCandidates {
	return &RegisterCandidates{st: st, logger: logger}
}

func (h *RegisterCandidates) Handle(w http.ResponseWriter, r *http.Request) {
	providerID := uuid.MustParse(chi.URLParam(r, "providerID"))
	req := &pb.RegisterIceCandidates{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	if err := h.st.RegisterCandidates(r.Context(), providerID, req.Candidates); err != nil {
		h.logger.Error("register candidates failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to register candidates")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]int{"candidate_count": len(req.Candidates)})
}

// GetCandidates handles GET /v1/vpn/providers/{providerID}/candidates
type GetCandidates struct {
	st     store.Store
	logger *slog.Logger
}

func NewGetCandidates(st store.Store, logger *slog.Logger) *GetCandidates {
	return &GetCandidates{st: st, logger: logger}
}

func (h *GetCandidates) Handle(w http.ResponseWriter, r *http.Request) {
	providerID := uuid.MustParse(chi.URLParam(r, "providerID"))
	candidates, err := h.st.GetProviderCandidates(r.Context(), providerID)
	if err != nil {
		h.logger.Error("get candidates failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to get candidates")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"candidates": candidates,
		"count":      len(candidates),
	})
}

// TriggerFailover handles POST /v1/vpn/sessions/{sessionID}/failover
type TriggerFailover struct {
	st     store.Store
	logger *slog.Logger
}

func NewTriggerFailover(st store.Store, logger *slog.Logger) *TriggerFailover {
	return &TriggerFailover{st: st, logger: logger}
}

func (h *TriggerFailover) Handle(w http.ResponseWriter, r *http.Request) {
	sessionID := uuid.MustParse(chi.URLParam(r, "sessionID"))
	req := &pb.TriggerFailover{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Get current session to determine region
	session, err := h.st.GetSession(r.Context(), sessionID)
	if err != nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}

	// Capture old provider ID BEFORE failover. Memory store returns a live
	// pointer and TriggerFailover mutates session.CurrentProvider in-place,
	// so any later read of session.CurrentProvider would show the NEW value.
	failedProviderID := session.CurrentProvider
	region := session.Region

	// Edge case (#535): if the session has no CurrentProvider yet (i.e. it
	// was created but never confirmed), failover is meaningless — there's
	// nothing to fail over from. Reject with a clear error instead of
	// silently picking a "new" provider that's the same as the (nil) old.
	if failedProviderID == uuid.Nil {
		respondError(w, http.StatusConflict,
			"session has no current provider — confirm an ICE candidate first")
		return
	}

	// Select alternate provider in same region, EXCLUDING the failed provider
	// (so we don't immediately retry the one that just died)
	exclude := []uuid.UUID{failedProviderID}
	for _, idStr := range req.ExcludeProviderIds {
		if id, err := uuid.Parse(idStr); err == nil {
			exclude = append(exclude, id)
		}
	}
	altProviderID, err := h.st.SelectAlternateProvider(r.Context(), region, exclude)
	if err != nil {
		FailoversTriggered.WithLabelValues(region, "no_alternate").Inc()
		h.logger.Error("no alternate provider available",
			slog.String("region", region),
			slog.String("error", err.Error()))
		respondError(w, http.StatusServiceUnavailable, "no alternate provider available")
		return
	}
	FailoversTriggered.WithLabelValues(region, "success").Inc()

	// Trigger failover in store (updates session.CurrentProvider, increments FailoverCount, sets FAILING_OVER state)
	if err := h.st.TriggerFailover(r.Context(), sessionID, failedProviderID, altProviderID); err != nil {
		h.logger.Error("failover failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to trigger failover")
		return
	}

	// Mark previous provider as degraded (if reason indicates unreachability)
	if req.FailureReason != "" {
		_ = h.st.UpdateProviderHealth(r.Context(), failedProviderID, "degraded", time.Now())
	}

	// Fetch alt provider's ICE candidates
	altCandidates, _ := h.st.GetProviderCandidates(r.Context(), altProviderID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "failover_complete",
		"session_id":      sessionID.String(),
		"old_provider_id": failedProviderID.String(),
		"new_provider_id": altProviderID.String(),
		"ice_candidates":  altCandidates,
	})
}

// ---------------------------------------------------------------
// VPN-7 (#511): provider daemon health probes + graceful offline.
// ---------------------------------------------------------------

// healthReport is the body the daemon POSTs on
// /v1/vpn/providers/{id}/health. Field tags match the Rust
// `iogrid_routing::HealthReport` serde shape so the round-trip is
// type-checked at the daemon boundary, not just at the JSON parser.
type healthReport struct {
	ProviderID    string `json:"provider_id"`
	Status        string `json:"status"`
	AtUnixMs      int64  `json:"at_unix_ms"`
	VpnListenAddr string `json:"vpn_listen_addr"`
}

// offlineReport is the body for /v1/vpn/providers/{id}/offline.
type offlineReport struct {
	ProviderID string `json:"provider_id"`
	AtUnixMs   int64  `json:"at_unix_ms"`
	Reason     string `json:"reason"`
}

// UpdateHealth handles POST /v1/vpn/providers/{providerID}/health.
//
// Daemons POST this every ~15s; vpn-svc updates the provider row's
// Status + LastSeenAt. The failover store (VPN-4) consults
// LastSeenAt for staleness detection, so a daemon that crashes mid-
// loop will be inferred-offline by the SelectProviderForSession path
// even without an explicit offline POST.
type UpdateHealth struct {
	st     store.Store
	logger *slog.Logger
}

// NewUpdateHealth builds a new UpdateHealth handler.
func NewUpdateHealth(st store.Store, logger *slog.Logger) *UpdateHealth {
	return &UpdateHealth{st: st, logger: logger}
}

// Handle implements http.Handler for UpdateHealth.
func (h *UpdateHealth) Handle(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(chi.URLParam(r, "providerID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid provider id")
		return
	}
	req := &healthReport{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	// Only accept the three known wire states. Anything else is a
	// client bug — fail fast rather than write a junk Status row.
	switch req.Status {
	case "healthy", "degraded":
		// ok
	default:
		respondError(w, http.StatusBadRequest, "invalid status (must be healthy or degraded)")
		return
	}
	lastSeen := time.UnixMilli(req.AtUnixMs)
	if req.AtUnixMs == 0 {
		// Daemon didn't send a timestamp — substitute server-side now.
		lastSeen = time.Now()
	}
	if err := h.st.UpdateProviderHealth(r.Context(), providerID, req.Status, lastSeen); err != nil {
		// Provider row not found is the common case for an unpaired
		// daemon — that's a 404, not a 500.
		h.logger.Warn("update health failed",
			slog.String("provider_id", providerID.String()),
			slog.String("status", req.Status),
			slog.String("error", err.Error()))
		respondError(w, http.StatusNotFound, "provider not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": req.Status})
}

// MarkOffline handles POST /v1/vpn/providers/{providerID}/offline.
//
// Daemons POST this once during graceful shutdown so the customer
// SDK's failover detector (VPN-11) can re-route active sessions
// before the next periodic health tick would otherwise expire. The
// body's `reason` is logged but does not change the stored row.
type MarkOffline struct {
	st     store.Store
	logger *slog.Logger
}

// NewMarkOffline builds a new MarkOffline handler.
func NewMarkOffline(st store.Store, logger *slog.Logger) *MarkOffline {
	return &MarkOffline{st: st, logger: logger}
}

// Handle implements http.Handler for MarkOffline.
func (h *MarkOffline) Handle(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(chi.URLParam(r, "providerID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid provider id")
		return
	}
	req := &offlineReport{}
	// Empty body is valid — the reason + at_unix_ms fields are
	// debug-only; default reason to "unspecified" if missing.
	_ = json.NewDecoder(r.Body).Decode(req)
	if req.Reason == "" {
		req.Reason = "unspecified"
	}
	at := time.UnixMilli(req.AtUnixMs)
	if req.AtUnixMs == 0 {
		at = time.Now()
	}
	if err := h.st.UpdateProviderHealth(r.Context(), providerID, "offline", at); err != nil {
		h.logger.Warn("mark offline failed",
			slog.String("provider_id", providerID.String()),
			slog.String("reason", req.Reason),
			slog.String("error", err.Error()))
		respondError(w, http.StatusNotFound, "provider not found")
		return
	}
	h.logger.Info("provider marked offline",
		slog.String("provider_id", providerID.String()),
		slog.String("reason", req.Reason))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "offline", "reason": req.Reason})
}

// ListProvidersInRegion handles GET /v1/vpn/regions/{region}/providers
type ListProvidersInRegion struct {
	st     store.Store
	logger *slog.Logger
}

func NewListProvidersInRegion(st store.Store, logger *slog.Logger) *ListProvidersInRegion {
	return &ListProvidersInRegion{st: st, logger: logger}
}

func (h *ListProvidersInRegion) Handle(w http.ResponseWriter, r *http.Request) {
	region := chi.URLParam(r, "region")
	if region == "" {
		respondError(w, http.StatusBadRequest, "region required")
		return
	}

	providers, err := h.st.GetProvidersInRegion(r.Context(), region)
	if err != nil {
		h.logger.Error("list providers failed",
			slog.String("region", region),
			slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to list providers")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"region":    region,
		"providers": providers,
		"count":     len(providers),
	})
}

// RegisterProvider handles POST /v1/vpn/providers/{providerID}/register
type RegisterProvider struct {
	st     store.Store
	logger *slog.Logger
}

// NewRegisterProvider builds a new RegisterProvider handler.
func NewRegisterProvider(st store.Store, logger *slog.Logger) *RegisterProvider {
	return &RegisterProvider{st: st, logger: logger}
}

type registerProviderReq struct {
	Region string `json:"region"`
}

// Handle implements http.Handler for RegisterProvider.
// Idempotent — re-registering an existing provider updates region/status
// but preserves session_count (per Store.RegisterProvider contract).
func (h *RegisterProvider) Handle(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(chi.URLParam(r, "providerID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid provider id")
		return
	}
	req := &registerProviderReq{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.Region == "" {
		respondError(w, http.StatusBadRequest, "region required")
		return
	}
	info := &store.ProviderInfo{
		ID:         providerID,
		Region:     req.Region,
		Status:     "healthy",
		LastSeenAt: time.Now(),
	}
	if err := h.st.RegisterProvider(r.Context(), info); err != nil {
		h.logger.Error("register provider failed",
			slog.String("provider_id", providerID.String()),
			slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to register provider")
		return
	}
	h.logger.Info("provider registered",
		slog.String("provider_id", providerID.String()),
		slog.String("region", req.Region))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":      "registered",
		"provider_id": providerID.String(),
		"region":      req.Region,
	})
}

// --- #536: WG peer binding handlers ---------------------------------------

// ListAssignedSessions handles GET /v1/vpn/providers/{providerID}/assigned-sessions
type ListAssignedSessions struct {
	st     store.Store
	logger *slog.Logger
}

func NewListAssignedSessions(st store.Store, logger *slog.Logger) *ListAssignedSessions {
	return &ListAssignedSessions{st: st, logger: logger}
}

func (h *ListAssignedSessions) Handle(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(chi.URLParam(r, "providerID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid provider id")
		return
	}
	sessions, err := h.st.ListAssignedSessions(r.Context(), providerID)
	if err != nil {
		h.logger.Error("list assigned sessions failed",
			slog.String("provider_id", providerID.String()),
			slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to list assigned sessions")
		return
	}
	out := make([]map[string]interface{}, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, map[string]interface{}{
			"session_id":              s.ID.String(),
			"customer_id":             s.CustomerID.String(),
			"region":                  s.Region,
			"current_provider_id":     s.CurrentProvider.String(),
			"customer_wg_public_key":  s.CustomerWgPublicKey,
			"created_at":              s.CreatedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"provider_id": providerID.String(),
		"sessions":    out,
		"count":       len(out),
	})
}

type bindProviderReq struct {
	ProviderWgPublicKey string `json:"provider_wg_public_key"`
}

// BindProvider handles POST /v1/vpn/sessions/{sessionID}/bind-provider
type BindProvider struct {
	st     store.Store
	logger *slog.Logger
}

func NewBindProvider(st store.Store, logger *slog.Logger) *BindProvider {
	return &BindProvider{st: st, logger: logger}
}

func (h *BindProvider) Handle(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	req := &bindProviderReq{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ProviderWgPublicKey == "" {
		respondError(w, http.StatusBadRequest, "provider_wg_public_key required")
		return
	}
	if err := h.st.BindProviderToSession(r.Context(), sessionID, req.ProviderWgPublicKey); err != nil {
		h.logger.Warn("bind provider failed",
			slog.String("session_id", sessionID.String()),
			slog.String("error", err.Error()))
		respondError(w, http.StatusNotFound, "session not found")
		return
	}
	h.logger.Info("provider bound to session",
		slog.String("session_id", sessionID.String()))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "bound"})
}

type bindCustomerWgKeyReq struct {
	CustomerWgPublicKey string `json:"customer_wg_public_key"`
}

// BindCustomerWgKey handles POST /v1/vpn/sessions/{sessionID}/bind-customer-wg-key
type BindCustomerWgKey struct {
	st     store.Store
	logger *slog.Logger
}

func NewBindCustomerWgKey(st store.Store, logger *slog.Logger) *BindCustomerWgKey {
	return &BindCustomerWgKey{st: st, logger: logger}
}

func (h *BindCustomerWgKey) Handle(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	req := &bindCustomerWgKeyReq{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CustomerWgPublicKey == "" {
		respondError(w, http.StatusBadRequest, "customer_wg_public_key required")
		return
	}
	if err := h.st.BindCustomerWgKey(r.Context(), sessionID, req.CustomerWgPublicKey); err != nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "bound"})
}
