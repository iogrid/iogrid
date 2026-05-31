package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

// RequestSession handles POST /v1/vpn/sessions
type RequestSession struct {
	st     store.Store
	logger *slog.Logger
}

func NewRequestSession(st store.Store, logger *slog.Logger) *RequestSession {
	return &RequestSession{st: st, logger: logger}
}

func (h *RequestSession) Handle(w http.ResponseWriter, r *http.Request) {
	req := &pb.RequestVpnSession{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}

	// Create session in store
	sessionID := uuid.New()
	session := &store.Session{
		ID:         sessionID,
		CustomerID: uuid.MustParse(req.CustomerId),
		Region:     req.Region,
		State:      pb.VpnSessionState_CREATING,
	}
	if err := h.st.CreateSession(r.Context(), session); err != nil {
		h.logger.Error("create session failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(session)
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

	// Select alternate provider in same region
	altProviderID, err := h.st.SelectProviderForSession(r.Context(), session.Region)
	if err != nil {
		h.logger.Error("no alternate provider available",
			slog.String("region", session.Region),
			slog.String("error", err.Error()))
		respondError(w, http.StatusServiceUnavailable, "no alternate provider available")
		return
	}

	// Trigger failover in store (updates session.CurrentProvider, increments FailoverCount, sets FAILING_OVER state)
	if err := h.st.TriggerFailover(r.Context(), sessionID, session.CurrentProvider, altProviderID); err != nil {
		h.logger.Error("failover failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to trigger failover")
		return
	}

	// Mark previous provider as degraded (if reason indicates unreachability)
	if req.FailureReason != "" {
		_ = h.st.UpdateProviderHealth(r.Context(), session.CurrentProvider, "degraded", time.Now())
	}

	// Fetch alt provider's ICE candidates
	altCandidates, _ := h.st.GetProviderCandidates(r.Context(), altProviderID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "failover_complete",
		"session_id":      sessionID.String(),
		"old_provider_id": session.CurrentProvider.String(),
		"new_provider_id": altProviderID.String(),
		"ice_candidates":  altCandidates,
	})
}
