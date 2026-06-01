package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
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
	Validate(ctx context.Context, apiKey string) (workspaceID string, customerID string, tier string, err error)
}

// FreeTierQuotaBytes is the per-month byte cap for free-tier customers
// (#548). 2 GiB matches the README marketing copy. PAID tiers
// (STARTER / GROWTH / ENTERPRISE) are unlimited.
const FreeTierQuotaBytes = uint64(2 * 1024 * 1024 * 1024)

// isFreeTier maps the billing-svc tier enum string onto VPN's free-vs-paid
// binary. UNSPECIFIED + PAYG are treated as Free; STARTER and above are
// treated as Paid (unlimited bandwidth). Refs #548.
func isFreeTier(tier string) bool {
	switch tier {
	case "SUBSCRIPTION_TIER_STARTER",
		"SUBSCRIPTION_TIER_GROWTH",
		"SUBSCRIPTION_TIER_ENTERPRISE":
		return false
	}
	return true
}

// freeTierThrottleBytes is the soft-throttle threshold: once a free-tier
// customer has burned through this many bytes this month, vpn-svc reports
// QUOTA_STATE_THROTTLED so the mobile app (#573) can render a "you're at
// 80%" banner BEFORE the hard 429 kicks in. Set to 80% of FreeTierQuotaBytes.
const freeTierThrottleBytes = (FreeTierQuotaBytes / 10) * 8

// computeQuotaState derives the QuotaState enum from a customer's tier
// string + their month-to-date bytes total. Pure function — kept separate
// from RequestSession.Handle so it's unit-testable in isolation (#573).
//
// Contract:
//   - Paid tiers (STARTER/GROWTH/ENTERPRISE)         -> QUOTA_STATE_OK
//   - Free + used < 80% of FreeTierQuotaBytes        -> QUOTA_STATE_OK
//   - Free + 80% <= used < 100% of FreeTierQuotaBytes -> QUOTA_STATE_THROTTLED
//   - Free + used >= FreeTierQuotaBytes              -> QUOTA_STATE_EXHAUSTED
func computeQuotaState(tier string, used uint64) pb.QuotaState {
	if !isFreeTier(tier) {
		return pb.QuotaState_QUOTA_STATE_OK
	}
	if used >= FreeTierQuotaBytes {
		return pb.QuotaState_QUOTA_STATE_EXHAUSTED
	}
	if used >= freeTierThrottleBytes {
		return pb.QuotaState_QUOTA_STATE_THROTTLED
	}
	return pb.QuotaState_QUOTA_STATE_OK
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
	//
	// We track the resolved tier + month-to-date usage across this block so
	// the success-path response can attach a quota_state hint (#573) without
	// re-querying the store.
	resolvedTier := ""
	usedBytes := uint64(0)
	if h.validator != nil {
		if req.APIKey == "" {
			respondError(w, http.StatusUnauthorized, "api_key required")
			return
		}
		wsID, custID, tier, err := h.validator.Validate(r.Context(), req.APIKey)
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
		resolvedTier = tier

		// Free-tier quota gate (#548). Paid tiers are unlimited; free
		// tiers get FreeTierQuotaBytes (2 GiB/mo). We sum bytes_in +
		// bytes_out across this calendar month's sessions and reject
		// with 429 if the customer is already over.
		if isFreeTier(tier) {
			used, err := h.st.SumCustomerBytesThisMonth(r.Context(), customerUUID)
			if err != nil {
				h.logger.Warn("free-tier quota query failed (allowing through)",
					slog.String("customer_id", customerUUID.String()),
					slog.String("error", err.Error()))
			} else {
				usedBytes = used
				if used >= FreeTierQuotaBytes {
					h.logger.Info("free-tier quota exhausted",
						slog.String("customer_id", customerUUID.String()),
						slog.Uint64("used_bytes", used),
						slog.Uint64("quota_bytes", FreeTierQuotaBytes))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"error":       "quota_exceeded",
						"detail":      "free-tier 2 GiB/month bandwidth quota exhausted — upgrade to plus or pro at https://iogrid.org/customer/vpn",
						"quota_bytes": FreeTierQuotaBytes,
						"used_bytes":  used,
						"quota_state": pb.QuotaState_QUOTA_STATE_EXHAUSTED.String(),
					})
					return
				}
			}
		}
	} else {
		h.logger.Warn("api key validation skipped (dev mode — set VPN_SVC_BILLING_URL to enable)")
	}

	// Assign a provider. region=="auto" (#570) → cross-region best-
	// scoring pick (mobile-app flow); else region-specific pick. The
	// Postgres schema constraints fk_primary_provider + fk_current_provider
	// require non-zero provider UUIDs at INSERT time, so we MUST pick
	// one here. If no healthy provider, 503.
	var providerID uuid.UUID
	chosenRegion := req.Region
	if req.Region == "auto" {
		// Geo-affinity hint from X-Forwarded-For — the first IP in the
		// list is the client per RFC 7239 §5.2. Empty when called
		// directly (e.g. integration tests with no proxy).
		ipHint := firstForwardedFor(r.Header.Get("X-Forwarded-For"))
		var err error
		providerID, chosenRegion, err = h.st.SelectProviderAcrossRegions(r.Context(), ipHint)
		if err != nil {
			h.logger.Warn("no healthy provider across regions",
				slog.String("client_ip_hint", ipHint),
				slog.String("error", err.Error()))
			respondError(w, http.StatusServiceUnavailable,
				"no healthy provider available")
			return
		}
	} else {
		var err error
		providerID, err = h.st.SelectProviderForSession(r.Context(), req.Region)
		if err != nil {
			h.logger.Warn("no healthy provider in region",
				slog.String("region", req.Region),
				slog.String("error", err.Error()))
			respondError(w, http.StatusServiceUnavailable,
				"no healthy provider in region "+req.Region)
			return
		}
	}

	// Create session in store
	sessionID := uuid.New()
	session := &store.Session{
		ID:              sessionID,
		CustomerID:      customerUUID,
		Region:          chosenRegion,
		PrimaryProvider: providerID,
		CurrentProvider: providerID,
		State:           pb.VpnSessionState_CREATING,
		CreatedAt:       time.Now(),
		LastActivityAt:  time.Now(),
	}
	if err := h.st.CreateSession(r.Context(), session); err != nil {
		h.logger.Error("create session failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	SessionsCreated.Inc()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	// quota_state lets the mobile app (#573) render banner / paywall
	// purely from server state. For dev-mode (no validator) we report
	// OK — there's no tier or usage to gate on.
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id":  sessionID.String(),
		"status":      "CREATING",
		"provider_id": providerID.String(),
		"region":      chosenRegion,
		"quota_state": computeQuotaState(resolvedTier, usedBytes).String(),
	})
}

// firstForwardedFor returns the first IP in a comma-separated
// X-Forwarded-For header value (the originating client per RFC 7239 §5.2).
// Returns "" when the header is empty or malformed — the caller treats
// "" as "no geo hint available" and falls back to least-loaded picks.
func firstForwardedFor(xff string) string {
	xff = strings.TrimSpace(xff)
	if xff == "" {
		return ""
	}
	if i := strings.IndexByte(xff, ','); i >= 0 {
		return strings.TrimSpace(xff[:i])
	}
	return xff
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
	// quota_state (#573): compute against the customer's month-to-date
	// bytes total. GET has no API key, so we can't ask billing-svc for
	// the tier — we default to free-tier semantics, which is the
	// conservative path for the mobile-app banner (a paid customer
	// will never accrue enough usage to flip the state, since they
	// don't pay the gate).
	used, _ := h.st.SumCustomerBytesThisMonth(r.Context(), session.CustomerID)
	qs := computeQuotaState("", used)
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
		"quota_state":             qs.String(),
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

	// quota_state (#573): mobile app polls /refresh as its heartbeat;
	// surface the current quota signal so the banner / paywall reflects
	// live usage without a second round-trip. We look up the session
	// for its CustomerID, then compute MTD bytes — the metrics write
	// above has already been applied, so this reflects post-refresh
	// state. Failures fall back to UNSPECIFIED rather than failing the
	// heartbeat (this is a hint, not a gate).
	qs := pb.QuotaState_QUOTA_STATE_UNSPECIFIED
	if sess, err := h.st.GetSession(r.Context(), sessionID); err == nil {
		used, sumErr := h.st.SumCustomerBytesThisMonth(r.Context(), sess.CustomerID)
		if sumErr == nil {
			qs = computeQuotaState("", used)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	SessionRefreshes.Inc()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":      "refreshed",
		"quota_state": qs.String(),
	})
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

// TerminateAllForCustomer handles
// POST /v1/vpn/customers/{customerID}/sessions/terminate-all
//
// Caller passes the body {"reason":"<exit_reason>"} (defaults to
// "user_logout"). Returns {"terminated": <count>} so callers can log
// how many sessions were yanked.
type TerminateAllForCustomer struct {
	st     store.Store
	logger *slog.Logger
}

func NewTerminateAllForCustomer(st store.Store, logger *slog.Logger) *TerminateAllForCustomer {
	return &TerminateAllForCustomer{st: st, logger: logger}
}

func (h *TerminateAllForCustomer) Handle(w http.ResponseWriter, r *http.Request) {
	customerID, err := uuid.Parse(chi.URLParam(r, "customerID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	// Empty body is fine — default reason.
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Reason == "" {
		req.Reason = "user_logout"
	}
	n, err := h.st.TerminateAllForCustomer(r.Context(), customerID, req.Reason)
	if err != nil {
		h.logger.Error("terminate all failed",
			slog.String("customer_id", customerID.String()),
			slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to terminate sessions")
		return
	}
	h.logger.Info("customer sessions terminated",
		slog.String("customer_id", customerID.String()),
		slog.String("reason", req.Reason),
		slog.Int("terminated", n))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"terminated": n})
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

	// `?limit=N` switches to the mobile-app probe shape (#570) —
	// top-N least-loaded providers WITH their fresh ICE candidate set,
	// wg_public_key, and median RTT over the last hour. Without limit
	// we keep the legacy shape (flat list of ProviderInfo) so existing
	// smoke-test callers don't break.
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			respondError(w, http.StatusBadRequest, "limit must be a positive integer")
			return
		}
		// Cap at 50 — anything more is almost certainly a misuse, and we
		// don't want a single mobile client fanning out to every provider
		// in a region.
		if limit > 50 {
			limit = 50
		}
		probes, err := h.st.SelectTopProvidersInRegion(r.Context(), region, limit)
		if err != nil {
			h.logger.Error("select top providers failed",
				slog.String("region", region),
				slog.Int("limit", limit),
				slog.String("error", err.Error()))
			respondError(w, http.StatusInternalServerError, "failed to list providers")
			return
		}
		out := make([]map[string]interface{}, 0, len(probes))
		for _, p := range probes {
			out = append(out, map[string]interface{}{
				"provider_id":    p.ProviderID.String(),
				"wg_public_key":  p.WgPublicKey,
				"candidate_set":  p.Candidates,
				"median_rtt_ms":  p.MedianRttMs,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"region":    region,
			"providers": out,
			"count":     len(out),
		})
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
	// WgPublicKey is the daemon's static WireGuard public key (#570).
	// Optional — legacy daemons that pre-date the schema bump send "";
	// the store layer preserves any previously registered key in that
	// case rather than blanking it.
	WgPublicKey string `json:"wg_public_key"`
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
		ID:          providerID,
		Region:      req.Region,
		Status:      "healthy",
		LastSeenAt:  time.Now(),
		WgPublicKey: req.WgPublicKey,
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

// --- #541: customer sessions listing for /customer/vpn web page -----------

// ListSessionsByCustomer handles GET /v1/vpn/customers/{customerID}/sessions
type ListSessionsByCustomer struct {
	st     store.Store
	logger *slog.Logger
}

func NewListSessionsByCustomer(st store.Store, logger *slog.Logger) *ListSessionsByCustomer {
	return &ListSessionsByCustomer{st: st, logger: logger}
}

func (h *ListSessionsByCustomer) Handle(w http.ResponseWriter, r *http.Request) {
	customerID, err := uuid.Parse(chi.URLParam(r, "customerID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid customer id")
		return
	}
	sessions, err := h.st.ListSessionsByCustomer(r.Context(), customerID)
	if err != nil {
		h.logger.Error("list sessions failed",
			slog.String("customer_id", customerID.String()),
			slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}
	// Wire format: only ACTIVE (non-terminated) sessions, with the fields
	// the web UI displays.
	out := make([]map[string]interface{}, 0, len(sessions))
	for _, s := range sessions {
		if s.TerminatedAt != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"session_id":          s.ID.String(),
			"region":              s.Region,
			"current_provider_id": s.CurrentProvider.String(),
			"state":               s.State.String(),
			"bytes_in":            s.BytesIn,
			"bytes_out":           s.BytesOut,
			"created_at":          s.CreatedAt,
			"last_activity_at":    s.LastActivityAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"customer_id": customerID.String(),
		"sessions":    out,
		"count":       len(out),
	})
}

// ListRegions handles GET /v1/vpn/regions — customer region picker (#545).
type ListRegions struct {
	st     store.Store
	logger *slog.Logger
}

func NewListRegions(st store.Store, logger *slog.Logger) *ListRegions {
	return &ListRegions{st: st, logger: logger}
}

func (h *ListRegions) Handle(w http.ResponseWriter, r *http.Request) {
	regions, err := h.st.ListRegions(r.Context())
	if err != nil {
		h.logger.Error("list regions failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to list regions")
		return
	}
	out := make([]map[string]interface{}, 0, len(regions))
	for _, s := range regions {
		out = append(out, map[string]interface{}{
			"region":            s.Region,
			"healthy_providers": s.HealthyProviders,
			"total_providers":   s.TotalProviders,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"regions": out,
		"count":   len(out),
	})
}
