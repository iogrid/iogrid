package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"errors"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/payment"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/peer"
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
	escrow    *EscrowDeps     // optional — if set, payment_authorization is accepted
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

// WithEscrow wires up the $GRID payment-authorization layer (#596).
// When set, a payment_authorization in the request body is verified
// + escrowed before the session is returned to the caller. When the
// authorization is omitted the session is created as today (free-tier /
// legacy path).
func (h *RequestSession) WithEscrow(e *EscrowDeps) *RequestSession {
	h.escrow = e
	return h
}

// requestSessionReq is the wire body — superset of pb.RequestVpnSession with
// an api_key field. The proto's api_key_hash is treated as historic + ignored;
// new clients send raw api_key and vpn-svc forwards to billing-svc.
//
// PaymentAuthorization is the optional $GRID escrow body (Track 5 / #596).
// When present, vpn-svc verifies the ed25519 signature against the supplied
// Solana wallet, ensures the wallet has at least 0.001 GRID on-chain, and
// records an escrow row keyed by session_id. Clients that don't pay in
// $GRID (legacy / free-tier flows) omit this field entirely.
type requestSessionReq struct {
	CustomerID           string           `json:"customer_id"`
	Region               string           `json:"region"`
	APIKey               string           `json:"api_key"`
	APIKeyHash           string           `json:"api_key_hash"` // deprecated, ignored
	PaymentAuthorization *paymentAuthBody `json:"payment_authorization,omitempty"`
}

// paymentAuthBody mirrors payment.Auth on the wire. Kept as a private
// alias so the JSON shape is stable across refactors of payment.Auth.
type paymentAuthBody struct {
	WalletAddress    string `json:"wallet_address"`
	Signature        string `json:"signature"`
	Message          string `json:"message"`
	Nonce            string `json:"nonce"`
	MaxGRIDPerMinute uint64 `json:"max_grid_per_min"`
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
		State:           pb.VpnSessionState_VPN_SESSION_STATE_CREATING,
		CreatedAt:       time.Now(),
		LastActivityAt:  time.Now(),
	}
	if err := h.st.CreateSession(r.Context(), session); err != nil {
		h.logger.Error("create session failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// --- $GRID payment authorization (#596 / Track 5) ----------------
	// When the client included a payment_authorization, verify the
	// signature, check on-chain $GRID balance, and write an escrow row.
	// Any failure here is FATAL for the session — we tear down the
	// freshly-created row so the client isn't holding a paid-for shell
	// of a session.
	if h.escrow != nil && h.escrow.Svc != nil && req.PaymentAuthorization != nil {
		auth := payment.Auth{
			WalletAddress:    req.PaymentAuthorization.WalletAddress,
			Signature:        req.PaymentAuthorization.Signature,
			Message:          req.PaymentAuthorization.Message,
			Nonce:            req.PaymentAuthorization.Nonce,
			MaxGRIDPerMinute: req.PaymentAuthorization.MaxGRIDPerMinute,
		}
		if _, err := h.escrow.Svc.Authorize(r.Context(), sessionID, customerUUID, auth); err != nil {
			// Roll back the session row so we don't leave a paid-but-empty
			// session in the ledger.
			_ = h.st.TerminateSession(r.Context(), sessionID, "payment_auth_failed")
			h.logger.Warn("payment authorization rejected",
				slog.String("session_id", sessionID.String()),
				slog.String("error", err.Error()))
			switch {
			case errors.Is(err, payment.ErrSigInvalid):
				respondError(w, http.StatusUnauthorized, "payment authorization: signature invalid")
			case errors.Is(err, payment.ErrInsufficientBalance):
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusPaymentRequired)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":     "insufficient_balance",
					"required": payment.MinEscrowAtomic,
					"detail":   "wallet has <0.001 GRID — top up before retrying",
				})
			case errors.Is(err, payment.ErrNonceReplay):
				respondError(w, http.StatusConflict, "payment authorization: nonce replay")
			default:
				respondError(w, http.StatusBadRequest, "payment authorization: "+err.Error())
			}
			return
		}
	}

	SessionsCreated.Inc()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	// quota_state lets the mobile app (#573) render banner / paywall
	// purely from server state. For dev-mode (no validator) we report
	// OK — there's no tier or usage to gate on.
	// EPIC #566 reviewer BLOCKER 1: key is "state" (NOT "status") so the
	// JS coordinator wrapper (mobile/ios/src/lib/coordinator.ts) sees a
	// consistent shape with the GET handler at line 302+.
	json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id":  sessionID.String(),
		"state":       "CREATING",
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
		"session_id":             session.ID.String(),
		"customer_id":            session.CustomerID.String(),
		"region":                 session.Region,
		"primary_provider_id":    session.PrimaryProvider.String(),
		"current_provider_id":    session.CurrentProvider.String(),
		"state":                  session.State.String(),
		"bytes_in":               session.BytesIn,
		"bytes_out":              session.BytesOut,
		"created_at":             session.CreatedAt,
		"last_activity_at":       session.LastActivityAt,
		"provider_id":            session.CurrentProvider.String(),
		"provider_wg_public_key": session.ProviderWgPublicKey,
		"customer_wg_public_key": session.CustomerWgPublicKey,
		// #738: surface the per-session tunnel-inner IP here too so the
		// mobile app can re-fetch it post-connect (issue option b — the
		// lower-risk re-fetch path) if it ever needs to recover the value
		// independently of the create response. `inner_ip` is the bare IP
		// the client reads; `customer_inner_cidr` is the /32 form for the
		// native WGTunnel.swift path. Both are "" for legacy (non-mobile)
		// sessions via the innerCIDR helper, so no bogus "/32" leaks.
		"inner_ip":            session.InnerIP,
		"customer_inner_cidr": innerCIDR(session.InnerIP),
		"ice_candidates":      candidates,
		"quota_state":         qs.String(),
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

	if err := h.st.UpdateSessionState(r.Context(), sessionID, pb.VpnSessionState_VPN_SESSION_STATE_ESTABLISHING); err != nil {
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
				"provider_id":   p.ProviderID.String(),
				"wg_public_key": p.WgPublicKey,
				"candidate_set": p.Candidates,
				"median_rtt_ms": p.MedianRttMs,
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
	// #762: before we persist the (possibly new) server key, detect a
	// server-pubkey rotation and force-invalidate the bound sessions. A
	// daemon re-provisioned onto an empty state-dir mints a FRESH WG static
	// key (load_or_generate_wg_private_key); every client that baked the old
	// server pubkey into its NE tunnel would then MAC1-reject every handshake
	// ("did not decapsulate") until it rebuilt the config. Terminating the
	// affected sessions makes each client reconnect → the mobile bring-up
	// hands back the new peer_public_key and #760's client self-heal rebuilds
	// the tunnel. Best-effort: a store error here must not block the register
	// itself (the provider still needs its health row), so we log + proceed.
	if terminated, changed, err := h.st.InvalidateSessionsOnProviderKeyChange(r.Context(), providerID, req.WgPublicKey); err != nil {
		h.logger.Error("provider key-change invalidation failed (continuing register)",
			slog.String("provider_id", providerID.String()),
			slog.String("error", err.Error()))
	} else if changed {
		ProviderKeyRotations.Inc()
		if terminated > 0 {
			SessionsTerminated.WithLabelValues("provider_key_rotated").Add(float64(terminated))
		}
		h.logger.Warn("provider WG server key rotated — invalidated bound sessions so clients refetch the new key (#762)",
			slog.String("provider_id", providerID.String()),
			slog.Int("sessions_terminated", terminated))
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

// innerCIDR formats a per-session tunnel-inner IP (stored as host(inner_ip),
// e.g. "10.66.176.11") as the /32 CIDR the daemon's binder expects, matching
// the mobile-session response shape. Returns "" for an unset inner IP so a
// legacy (non-mobile) session doesn't get a bogus "/32".
func innerCIDR(innerIP string) string {
	if innerIP == "" {
		return ""
	}
	return innerIP + "/32"
}

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
	writeSessionList(w, providerID, sessions)
}

// writeSessionList renders the daemon-facing session-list wire shape
// (provider_id + sessions[] + count) shared by /assigned-sessions and
// /bound-sessions (#788). Both endpoints emit identical rows so the
// daemon binder can decode either with the same struct.
func writeSessionList(w http.ResponseWriter, providerID uuid.UUID, sessions []*store.Session) {
	out := make([]map[string]interface{}, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, map[string]interface{}{
			"session_id":             s.ID.String(),
			"customer_id":            s.CustomerID.String(),
			"region":                 s.Region,
			"current_provider_id":    s.CurrentProvider.String(),
			"customer_wg_public_key": s.CustomerWgPublicKey,
			// #701: the customer's tunnel-inner IP. The daemon's binder
			// needs it for #695 multi-customer return-routing — without it
			// the provider can't tell which connected peer a return packet
			// belongs to, so general-internet egress doesn't route back.
			"customer_inner_cidr": innerCIDR(s.InnerIP),
			"created_at":          s.CreatedAt,
		})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"provider_id": providerID.String(),
		"sessions":    out,
		"count":       len(out),
	})
}

// ListBoundSessions handles GET /v1/vpn/providers/{providerID}/bound-sessions
// (#788). Returns every still-live session bound to this provider WITH a
// customer key — including already-bound + >15-min-old ones that
// /assigned-sessions hides. The daemon polls this on startup + on a slow
// reconcile tick to repopulate its boringtun peer map after a restart, so
// previously-bound customers aren't stranded ("did not decapsulate").
type ListBoundSessions struct {
	st     store.Store
	logger *slog.Logger
}

func NewListBoundSessions(st store.Store, logger *slog.Logger) *ListBoundSessions {
	return &ListBoundSessions{st: st, logger: logger}
}

func (h *ListBoundSessions) Handle(w http.ResponseWriter, r *http.Request) {
	providerID, err := uuid.Parse(chi.URLParam(r, "providerID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid provider id")
		return
	}
	sessions, err := h.st.ListBoundSessions(r.Context(), providerID)
	if err != nil {
		h.logger.Error("list bound sessions failed",
			slog.String("provider_id", providerID.String()),
			slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to list bound sessions")
		return
	}
	writeSessionList(w, providerID, sessions)
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

// Track 3 (#588): mobile PacketTunnelProvider session bring-up.
// -------------------------------------------------------------------
//
// POST /v1/vpn/sessions/mobile — accepts the mobile-app session
// payload (client_public_key, region, payment_authorization) and
// returns the complete WG peer config (peer_public_key, peer_endpoint,
// customer_inner_cidr, allowed_ips, dns_servers, session_id,
// expires_at) so PacketTunnelProvider can call WireGuardAdapter.start
// without a second round-trip.
//
// Distinct from the legacy POST /v1/vpn/sessions handler which is the
// daemon-side ICE-candidate flow (kept intact for backwards-compat). A
// future PR may converge the two once Track 1's JWT auth lands and the
// legacy daemon path is fully retired.

// MobileDataPlaneDefaults centralises the mobile-flow defaults so a
// future operator override (env var) can replace them without touching
// the handler code. DNSServers matches iCloud Private Relay defaults +
// the WireGuardKit shipping default.
type MobileDataPlaneDefaults struct {
	DNSServers       []string
	SessionTTL       time.Duration
	HeartbeatTimeout time.Duration
	RetryAfter       time.Duration // 503 Retry-After when no peer
}

// DefaultMobileDataPlane returns the canonical defaults per the #588 DoD.
func DefaultMobileDataPlane() MobileDataPlaneDefaults {
	return MobileDataPlaneDefaults{
		DNSServers:       []string{"1.1.1.1", "1.0.0.1"},
		SessionTTL:       24 * time.Hour,
		HeartbeatTimeout: 60 * time.Second,
		RetryAfter:       15 * time.Second,
	}
}

// RequestMobileSession handles POST /v1/vpn/sessions/mobile.
type RequestMobileSession struct {
	st        store.Store
	logger    *slog.Logger
	picker    *peer.Picker
	defaults  MobileDataPlaneDefaults
	validator APIKeyValidator
}

// NewRequestMobileSession builds the mobile handler. validator may be
// nil for dev / smoke mode (per the legacy handler).
func NewRequestMobileSession(st store.Store, logger *slog.Logger) *RequestMobileSession {
	return &RequestMobileSession{
		st:       st,
		logger:   logger,
		picker:   peer.NewPicker(st),
		defaults: DefaultMobileDataPlane(),
	}
}

// WithValidator wires up the optional API-key validator (mirrors the
// legacy handler's contract).
func (h *RequestMobileSession) WithValidator(v APIKeyValidator) *RequestMobileSession {
	h.validator = v
	return h
}

// requestMobileSessionReq is the wire shape — superset of the proto
// RequestMobileVpnSession message. We keep payment_authorization as
// raw json.RawMessage so the structure is opaque to vpn-svc (Track 5
// owns the schema) but still persisted intact for #596 validation.
type requestMobileSessionReq struct {
	CustomerID           string          `json:"customer_id"`
	Region               string          `json:"region"`
	ClientPublicKey      string          `json:"client_public_key"`
	APIKey               string          `json:"api_key"`
	PaymentAuthorization json.RawMessage `json:"payment_authorization,omitempty"`
}

// Handle implements http.Handler for RequestMobileSession.
func (h *RequestMobileSession) Handle(w http.ResponseWriter, r *http.Request) {
	req := &requestMobileSessionReq{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		MobileSessionRequests.WithLabelValues("bad_request").Inc()
		respondError(w, http.StatusBadRequest, "invalid request")
		return
	}
	if req.CustomerID == "" {
		MobileSessionRequests.WithLabelValues("bad_request").Inc()
		respondError(w, http.StatusBadRequest, "customer_id required")
		return
	}
	if req.ClientPublicKey == "" {
		MobileSessionRequests.WithLabelValues("bad_request").Inc()
		respondError(w, http.StatusBadRequest, "client_public_key required")
		return
	}
	// "auto" is the default region for the mobile flow — explicit empty
	// region is coerced to "auto" so the JS layer can omit it.
	if req.Region == "" {
		req.Region = "auto"
	}
	customerUUID, err := uuid.Parse(req.CustomerID)
	if err != nil {
		MobileSessionRequests.WithLabelValues("bad_request").Inc()
		respondError(w, http.StatusBadRequest, "customer_id must be a UUID")
		return
	}

	// Optional API-key validation (#531 parity with the legacy handler).
	resolvedTier := ""
	usedBytes := uint64(0)
	if h.validator != nil {
		if req.APIKey == "" {
			MobileSessionRequests.WithLabelValues("unauthorized").Inc()
			respondError(w, http.StatusUnauthorized, "api_key required")
			return
		}
		_, custID, tier, vErr := h.validator.Validate(r.Context(), req.APIKey)
		if vErr != nil {
			MobileSessionRequests.WithLabelValues("unauthorized").Inc()
			h.logger.Warn("mobile session: api key rejected",
				slog.String("customer_id", req.CustomerID),
				slog.String("error", vErr.Error()))
			respondError(w, http.StatusUnauthorized, "invalid api_key")
			return
		}
		if custID != "" {
			customerUUID = uuid.MustParse(custID)
		}
		resolvedTier = tier

		if isFreeTier(tier) {
			used, sumErr := h.st.SumCustomerBytesThisMonth(r.Context(), customerUUID)
			if sumErr == nil {
				usedBytes = used
				if used >= FreeTierQuotaBytes {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"error":       "quota_exceeded",
						"detail":      "free-tier 2 GiB/month bandwidth quota exhausted — upgrade to plus or pro",
						"quota_bytes": FreeTierQuotaBytes,
						"used_bytes":  used,
						"quota_state": pb.QuotaState_QUOTA_STATE_EXHAUSTED.String(),
					})
					return
				}
			}
		}
	}

	// payment_authorization is captured opaquely. Track 5 (#596) will
	// inject a ValidatePayment hook here; for now we accept anything
	// (including nil) and persist verbatim.
	paymentAuth := []byte(req.PaymentAuthorization)
	if len(paymentAuth) > 0 {
		h.logger.Debug("payment_authorization received (validation deferred to #596)",
			slog.Int("payload_bytes", len(paymentAuth)))
	}

	// Pick a peer in the requested region (or geo-nearest if "auto").
	ipHint := firstForwardedFor(r.Header.Get("X-Forwarded-For"))
	providerID, chosenRegion, pickErr := h.picker.Pick(r.Context(), req.Region, ipHint)
	if pickErr != nil {
		if errors.Is(pickErr, peer.ErrNoPeer) {
			MobileSessionRequests.WithLabelValues("no_peer").Inc()
			h.logger.Warn("mobile session: no peer available",
				slog.String("region", req.Region),
				slog.String("ip_hint", ipHint))
			retryAfter := int(h.defaults.RetryAfter.Seconds())
			w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error":           "no_peer_available",
				"detail":          "no healthy peer in region — retry shortly",
				"region":          req.Region,
				"retry_after_sec": retryAfter,
			})
			return
		}
		MobileSessionRequests.WithLabelValues("internal_error").Inc()
		respondError(w, http.StatusInternalServerError, "peer selection failed")
		return
	}

	// Allocate inner IPv4 from the peer's /24.
	//
	// We must pass the session UUID too — AllocateInnerIP is idempotent
	// on (providerID, sessionID) so a transient retry reuses the same
	// IP instead of burning a new Y. Generate sessionID up front and
	// pass it through to CreateSession below.
	sessionID := uuid.New()
	innerIP, allocErr := h.st.AllocateInnerIP(r.Context(), providerID, sessionID)
	if allocErr != nil {
		MobileSessionRequests.WithLabelValues("internal_error").Inc()
		h.logger.Error("mobile session: inner-ip alloc failed",
			slog.String("provider_id", providerID.String()),
			slog.String("error", allocErr.Error()))
		respondError(w, http.StatusInternalServerError, "inner ip allocation failed")
		return
	}

	// Look up the peer's WG public key + a probable endpoint. The
	// endpoint comes from the provider's freshest ICE candidate set
	// (preferring srflx > host > relay). If none is available we
	// surface 503 — the peer is registered but un-reachable.
	providerInfo, providerErr := h.lookupProvider(r.Context(), providerID, chosenRegion)
	if providerErr != nil {
		MobileSessionRequests.WithLabelValues("no_peer").Inc()
		h.logger.Warn("mobile session: peer endpoint lookup failed",
			slog.String("provider_id", providerID.String()),
			slog.String("error", providerErr.Error()))
		retryAfter := int(h.defaults.RetryAfter.Seconds())
		w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
		respondError(w, http.StatusServiceUnavailable, "peer endpoint not yet published")
		return
	}

	// Create the session row.
	expiresAt := time.Now().Add(h.defaults.SessionTTL)
	session := &store.Session{
		ID:              sessionID,
		CustomerID:      customerUUID,
		Region:          chosenRegion,
		PrimaryProvider: providerID,
		CurrentProvider: providerID,
		State:           pb.VpnSessionState_VPN_SESSION_STATE_CREATING,
		CreatedAt:       time.Now(),
		LastActivityAt:  time.Now(),
		// Persist the in-memory copy of the peer config too so the
		// memory-store-backed tests see it via GetSession even before
		// PersistSessionPeerConfig overwrites the postgres row.
		ClientPublicKey: req.ClientPublicKey,
		// #698: also set CustomerWgPublicKey so the daemon's binder upserts
		// this customer as a WG peer. The binder reads customer_wg_public_key
		// from /assigned-sessions; the mobile flow previously set only
		// ClientPublicKey, so the binder saw an empty customer key and never
		// added the peer → the mobile WG handshake silently failed.
		CustomerWgPublicKey:  req.ClientPublicKey,
		InnerIP:              innerIP,
		ExpiresAt:            &expiresAt,
		PaymentAuthorization: paymentAuth,
	}
	if err := h.st.CreateSession(r.Context(), session); err != nil {
		MobileSessionRequests.WithLabelValues("internal_error").Inc()
		h.logger.Error("mobile session: create failed", slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	// #698: deliberately do NOT pre-set the session's provider_wg_public_key
	// here. The daemon's binder owns the data-plane bind: it polls
	// /assigned-sessions (which lists sessions whose provider_wg_public_key
	// is still ""), upserts THIS customer (CustomerWgPublicKey, set above)
	// as a WG peer, then POSTs bind-provider — which sets
	// provider_wg_public_key. Pre-setting it here marked the session
	// "already bound" and excluded it from the binder, so the provider never
	// added the customer peer and the mobile WG handshake silently failed
	// (proven live, #696/#698). The one-round-trip response below still
	// carries peer_public_key + peer_endpoint (from providerInfo) so the
	// client configures WG immediately; the handshake completes once the
	// binder's next ~5s poll upserts the peer (WireGuard retries the
	// handshake until then). Persisting the endpoint is unnecessary for the
	// mobile flow — the client never re-reads it.

	// TODO(#596): once Track 5 lands wallet-signed authorization, push
	// the client_public_key to the provider daemon's WG peer set via
	// the daemon-transport interface (Track 4 wires this — currently
	// the daemon polls /providers/{id}/assigned-sessions every 5s and
	// will pick up the new session row on its own). The push path is
	// captured here as a no-op for now so the handler's contract
	// matches the #588 DoD verbatim.
	h.logger.Info("mobile session created",
		slog.String("session_id", sessionID.String()),
		slog.String("provider_id", providerID.String()),
		slog.String("region", chosenRegion),
		slog.String("inner_ip", innerIP))

	SessionsCreated.Inc()
	MobileSessionRequests.WithLabelValues("created").Inc()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id":          sessionID.String(),
		"peer_public_key":     providerInfo.WgPublicKey,
		"peer_endpoint":       providerInfo.Endpoint,
		"customer_inner_cidr": innerIP + "/32",
		// #738: the iOS coordinator (mobile/ios/src/lib/coordinator.ts)
		// reads the bare-IP field `inner_ip`, NOT `customer_inner_cidr`.
		// Before this it always decoded undefined → innerIP:"" → the app
		// fell back to the hard-coded default 10.66.0.2/32 (which only
		// happened to work because the peer is registered allowed-ips
		// 0.0.0.0/0, so return traffic wasn't inner-IP-filtered). Surface
		// the real inner IP under the name the client already reads;
		// `customer_inner_cidr` stays for the native WGTunnel.swift path
		// (it parses the CIDR form). Both keys are additive — the working
		// connect flow is unaffected.
		"inner_ip":    innerIP,
		"allowed_ips": "0.0.0.0/0",
		"dns_servers": h.defaults.DNSServers,
		"expires_at":  expiresAt.UTC().Format(time.RFC3339),
		"region":      chosenRegion,
		"quota_state": computeQuotaState(resolvedTier, usedBytes).String(),
	})
}

// mobilePeerInfo bundles the two fields the mobile response surfaces
// from a provider's registration.
type mobilePeerInfo struct {
	WgPublicKey string
	Endpoint    string
}

// lookupProvider resolves a provider UUID to its WG pubkey + a
// best-guess UDP endpoint (preferring srflx > host > relay ICE
// candidates). Returns error if no endpoint is published — the
// handler surfaces that as 503 with Retry-After so the client retries
// once the daemon's next ICE-candidate POST lands.
func (h *RequestMobileSession) lookupProvider(ctx context.Context, providerID uuid.UUID, region string) (mobilePeerInfo, error) {
	probes, err := h.st.SelectTopProvidersInRegion(ctx, region, 50)
	if err != nil {
		return mobilePeerInfo{}, err
	}
	for _, p := range probes {
		if p.ProviderID != providerID {
			continue
		}
		endpoint := pickEndpoint(p.Candidates)
		if endpoint == "" {
			return mobilePeerInfo{}, errors.New("no ICE candidate published yet")
		}
		// Fail safe (#696): a provider that registered without its static WG
		// public key would yield peer_public_key:"" — the customer's tunnel
		// would then configure a peer with no key and silently never
		// handshake (a 201 that never connects). Treat it like an
		// unpublished endpoint: surface 503 + Retry-After so the client
		// retries once the daemon's register (now carrying the key) lands.
		if strings.TrimSpace(p.WgPublicKey) == "" {
			return mobilePeerInfo{}, errors.New("provider WG public key not published yet")
		}
		return mobilePeerInfo{WgPublicKey: p.WgPublicKey, Endpoint: endpoint}, nil
	}
	// Provider isn't in the top-50 probe result — surface 404-shaped
	// error string but as a generic "endpoint not published" so the
	// caller path stays uniform.
	return mobilePeerInfo{}, errors.New("provider not in region top-N (no fresh candidates)")
}

// pickEndpoint applies the srflx > host > relay preference order to a
// fresh ICE candidate set. Returns the dotted-quad+port string the
// mobile client can pass straight to WireGuardKit's Endpoint(from:).
//
// IceCandidate.CandidateType is a string ("host", "srflx", "prflx",
// "relay") — not an enum — so we string-match.
// vpnInnerNet is the inner tunnel CIDR. An address inside it is the VPN's own
// overlay address (the daemon's iogrid-tun0), never a reachable WG endpoint.
var vpnInnerNet = func() *net.IPNet { _, n, _ := net.ParseCIDR("10.66.0.0/16"); return n }()

func pickEndpoint(candidates []*pb.IceCandidate) string {
	// ICE type preference; lower rank = more preferred.
	typeRank := map[string]int{"srflx": 0, "host": 1, "prflx": 2, "relay": 3}
	best := ""
	bestScore := -1
	for _, c := range candidates {
		if c == nil || c.ConnectionAddress == "" || c.ConnectionPort == 0 {
			continue
		}
		// ConnectionAddress may carry a "/NN" mask (e.g. "10.66.0.1/32") — strip it
		// so the endpoint is a clean dotted-quad:port WireGuardKit can parse.
		addr := c.ConnectionAddress
		if i := strings.IndexByte(addr, '/'); i >= 0 {
			addr = addr[:i]
		}
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}
		// Never hand an internet customer a non-routable endpoint: the VPN inner
		// CIDR, loopback, link-local, or unspecified are never valid WG endpoints.
		// (Fixes peer_endpoint=10.66.0.1, which black-holed the mobile tunnel.)
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || vpnInnerNet.Contains(ip) {
			continue
		}
		// Public IPs beat private (NAT-LAN) ones; within a tier keep ICE order.
		tier := 0
		if !ip.IsPrivate() {
			tier = 1
		}
		rank, ok := typeRank[c.CandidateType]
		if !ok {
			rank = 9
		}
		score := tier*100 + (10 - rank)
		if score > bestScore {
			bestScore = score
			best = addr + ":" + strconv.FormatUint(uint64(c.ConnectionPort), 10)
		}
	}
	return best
}

// MobileHeartbeat handles POST /v1/vpn/sessions/{sessionID}/heartbeat.
//
// Per the #588 DoD: accept byte counters. We forward them to the
// existing UpdateSessionMetrics path (so the earnings batcher sees
// them) AND echo the quota signal so the mobile banner stays in sync.
type MobileHeartbeat struct {
	st     store.Store
	logger *slog.Logger
}

// NewMobileHeartbeat builds the heartbeat handler.
func NewMobileHeartbeat(st store.Store, logger *slog.Logger) *MobileHeartbeat {
	return &MobileHeartbeat{st: st, logger: logger}
}

type mobileHeartbeatReq struct {
	BytesIn             uint64 `json:"bytes_in"`
	BytesOut            uint64 `json:"bytes_out"`
	LastHandshakeAgeSec uint32 `json:"last_handshake_age_seconds"`
	PathLatencyMs       uint32 `json:"path_latency_ms"`
	SentAtUnixMs        int64  `json:"sent_at_unix_ms"`
}

// Handle implements http.Handler for MobileHeartbeat.
func (h *MobileHeartbeat) Handle(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	req := &mobileHeartbeatReq{}
	if err := json.NewDecoder(r.Body).Decode(req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Reuse the existing metrics-update path so the earnings batcher
	// + roaming/failover counters stay coherent. We leave
	// roaming_events / failover_count untouched (heartbeat doesn't
	// know about them).
	sess, getErr := h.st.GetSession(r.Context(), sessionID)
	if getErr != nil {
		respondError(w, http.StatusNotFound, "session not found")
		return
	}
	if err := h.st.UpdateSessionMetrics(r.Context(), sessionID,
		req.BytesIn, req.BytesOut,
		sess.RoamingEvents, sess.FailoverCount); err != nil {
		h.logger.Error("mobile heartbeat: update failed",
			slog.String("session_id", sessionID.String()),
			slog.String("error", err.Error()))
		respondError(w, http.StatusInternalServerError, "heartbeat update failed")
		return
	}
	SessionRefreshes.Inc()

	// Quota signal echo — heartbeat doesn't carry an api_key so we
	// degenerate to free-tier semantics (paid customers never accrue
	// enough to flip the state).
	qs := pb.QuotaState_QUOTA_STATE_UNSPECIFIED
	if used, sumErr := h.st.SumCustomerBytesThisMonth(r.Context(), sess.CustomerID); sumErr == nil {
		qs = computeQuotaState("", used)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id":  sessionID.String(),
		"acked_at":    time.Now().UTC().Format(time.RFC3339Nano),
		"quota_state": qs.String(),
	})
}
