package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/payment"
)

// EscrowDeps wires the payment service + a session-end callback into
// the escrow handler set. The callback is fired by /heartbeat when the
// escrow exhausts AND by /terminate when the session ends — billing-svc
// then computes refund + provider/iogrid shares (#597) and queues the
// on-chain settlement (#598). When nil, the session ends without a
// settlement (dev-mode + tests with no billing-svc wired).
type EscrowDeps struct {
	Svc          *payment.Service
	SessionEnded SessionEndedCallback
	Logger       *slog.Logger
}

// SessionEndedCallback is invoked when a session terminates with an
// escrow row in flight. Implementations POST the (session_id, escrow)
// payload to billing-svc /v1/grid/session-end (see #597).
type SessionEndedCallback func(ctx context.Context, e *payment.Escrow) error

// HeartbeatRequest is the wire shape of POST /v1/vpn/sessions/{id}/heartbeat.
// Fields are cumulative byte counters from the customer SDK; we treat them
// as authoritative for the BILLING side but bound the per-tick delta via
// max_grid_per_min_atomic to defend against a runaway client.
type HeartbeatRequest struct {
	BytesIn  uint64 `json:"bytes_in"`
	BytesOut uint64 `json:"bytes_out"`
}

// HeartbeatResponse — what /heartbeat returns. `topup_low` is the SSE
// hint per #596: the SDK should surface a "low balance" banner when this
// flips true. `escrow_remaining` is in atomic units (9 decimals).
type HeartbeatResponse struct {
	Status            string `json:"status"`
	EscrowedAtomic    uint64 `json:"escrowed_atomic"`
	ConsumedAtomic    uint64 `json:"consumed_atomic"`
	RemainingAtomic   uint64 `json:"remaining_atomic"`
	TopupLow          bool   `json:"topup_low"`
	Exhausted         bool   `json:"exhausted"`
}

// Heartbeat handles POST /v1/vpn/sessions/{sessionID}/heartbeat.
//
// Unlike /refresh (which is the legacy heartbeat predating $GRID payment),
// /heartbeat performs *bytes → atomic GRID* arithmetic + escrow
// decrement. SDKs that don't yet pay in GRID continue to call /refresh.
// Once Track 4 (#594) lands the mobile-app payment UI, /refresh becomes
// an alias for /heartbeat with zero-cost (free-tier) escrow.
type Heartbeat struct {
	deps *EscrowDeps
}

// NewHeartbeat constructs a Heartbeat handler.
func NewHeartbeat(deps *EscrowDeps) *Heartbeat { return &Heartbeat{deps: deps} }

func (h *Heartbeat) Handle(w http.ResponseWriter, r *http.Request) {
	if h.deps == nil || h.deps.Svc == nil {
		respondError(w, http.StatusServiceUnavailable, "payment service disabled (set GRID_TOKEN_MINT_ADDRESS to enable)")
		return
	}
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	e, low, err := h.deps.Svc.Heartbeat(r.Context(), sessionID, req.BytesIn, req.BytesOut)
	if err != nil {
		if errors.Is(err, payment.ErrEscrowExhausted) {
			// Render 402 + flag exhausted so the SDK tears the tunnel down.
			if h.deps.Logger != nil {
				h.deps.Logger.Info("payment: escrow exhausted",
					slog.String("session_id", sessionID.String()))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			_ = json.NewEncoder(w).Encode(HeartbeatResponse{
				Status:    "exhausted",
				Exhausted: true,
				TopupLow:  true,
			})
			return
		}
		if h.deps.Logger != nil {
			h.deps.Logger.Error("heartbeat failed",
				slog.String("session_id", sessionID.String()),
				slog.String("error", err.Error()))
		}
		respondError(w, http.StatusInternalServerError, "heartbeat failed")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(HeartbeatResponse{
		Status:          "ok",
		EscrowedAtomic:  e.EscrowedAtomic,
		ConsumedAtomic:  e.ConsumedAtomic,
		RemainingAtomic: e.Remaining(),
		TopupLow:        low,
	})
}

// SettleSession handles POST /v1/vpn/sessions/{sessionID}/settle.
//
// Called by the SDK once the tunnel has been torn down (i.e. /terminate
// has already fired). We:
//
//   1. Mark the escrow settled in the local DB.
//   2. Forward the (session_id, consumed_atomic, refund_atomic, etc)
//      payload to billing-svc via the SessionEnded callback.
//
// Idempotent: a re-call is a no-op (settled_at COALESCEs).
type SettleSession struct {
	deps *EscrowDeps
}

func NewSettleSession(deps *EscrowDeps) *SettleSession { return &SettleSession{deps: deps} }

func (h *SettleSession) Handle(w http.ResponseWriter, r *http.Request) {
	if h.deps == nil || h.deps.Svc == nil {
		respondError(w, http.StatusServiceUnavailable, "payment service disabled")
		return
	}
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "invalid session id")
		return
	}
	e, err := h.deps.Svc.Store.GetEscrow(r.Context(), sessionID)
	if err != nil {
		respondError(w, http.StatusNotFound, "escrow not found")
		return
	}
	if e.SettledAt != nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     "already_settled",
			"settled_at": e.SettledAt,
		})
		return
	}
	if err := h.deps.Svc.Store.SettleEscrow(r.Context(), sessionID); err != nil {
		respondError(w, http.StatusInternalServerError, "settle escrow failed")
		return
	}
	if h.deps.SessionEnded != nil {
		// Fire-and-forget — if billing-svc is unreachable, we still
		// return success to the caller; the settlement-worker reconciles
		// from the DB row in the next cron tick.
		go func(snap payment.Escrow) {
			cb, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := h.deps.SessionEnded(cb, &snap); err != nil {
				if h.deps.Logger != nil {
					h.deps.Logger.Warn("session-ended callback failed",
						slog.String("session_id", snap.SessionID.String()),
						slog.String("error", err.Error()))
				}
			}
		}(*e)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":           "settled",
		"escrowed_atomic":  e.EscrowedAtomic,
		"consumed_atomic":  e.ConsumedAtomic,
		"refund_atomic":    e.Remaining(),
	})
}

// NewBillingForwarder is the production SessionEndedCallback. It POSTs to
// billing-svc's /v1/grid/session-end (defined in #597). For dev mode (no
// BILLING_SVC_URL) it logs + returns nil.
func NewBillingForwarder(billingURL string, logger *slog.Logger) SessionEndedCallback {
	if billingURL == "" {
		return func(ctx context.Context, e *payment.Escrow) error {
			if logger != nil {
				logger.Info("session-ended (dev mode — billing-svc unset)",
					slog.String("session_id", e.SessionID.String()))
			}
			return nil
		}
	}
	// strip trailing slash so URL composition is consistent.
	billingURL = strings.TrimRight(billingURL, "/")
	return func(ctx context.Context, e *payment.Escrow) error {
		body, _ := json.Marshal(map[string]any{
			"session_id":       e.SessionID.String(),
			"customer_id":      e.CustomerID.String(),
			"wallet_address":   e.WalletAddress,
			"escrowed_atomic":  e.EscrowedAtomic,
			"consumed_atomic":  e.ConsumedAtomic,
			"started_at":       e.StartedAt,
			"ended_at":         e.LastHeartbeatAt,
		})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			billingURL+"/v1/grid/session-end",
			strings.NewReader(string(body)))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return errors.New("billing-svc /grid/session-end returned " + resp.Status)
		}
		return nil
	}
}
