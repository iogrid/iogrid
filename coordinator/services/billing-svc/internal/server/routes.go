// Package server holds the HTTP route definitions for the billing-svc microservice.
//
// Customer subscriptions (Stripe), provider payouts (Stripe Connect),
// metering aggregation, Solana payout/burn loop admin hooks.
//
// The route surface is JSON-over-HTTP; gRPC bindings come from the
// shared proto package and will be wired in a follow-up. All write
// endpoints expect bearer auth verified upstream by gateway-bff.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/solana"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/stripeapi"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/tax"
)

// Deps is the bundle of collaborators the routes need. main.go builds
// this and passes it to Mount.
type Deps struct {
	Store  *store.Store
	Stripe *stripeapi.Service
	Solana *solana.Service
	Tax    *tax.Generator
}

// Mount attaches the billing-svc routes onto the shared chi router.
// Called by main() after /healthz, /readyz, /metrics are already wired
// up by the shared bootstrap.
func Mount(d Deps) func(chi.Router) {
	return func(r chi.Router) {
		h := &handlers{deps: d}
		r.Route("/v1", func(r chi.Router) {
			r.Get("/", h.index)

			// Customer-side
			r.Route("/subscriptions", func(r chi.Router) {
				r.Get("/{workspaceID}", h.getSubscription)
				r.Post("/{workspaceID}/checkout", h.createCheckoutSession)
				r.Post("/{workspaceID}/portal", h.createPortalSession)
				r.Get("/{workspaceID}/invoices", h.listInvoices)
			})

			// Stripe webhook — single endpoint, signature-validated.
			r.Post("/stripe/webhook", h.stripeWebhook)

			// Provider-side payouts (Stripe Connect)
			r.Route("/payouts", func(r chi.Router) {
				r.Post("/{userID}/onboarding", h.startPayoutOnboarding)
				r.Get("/{userID}/account", h.getPayoutAccount)
				r.Post("/{userID}/instant", h.requestInstantPayout)
			})

			// Solana payout/burn admin hooks (internal use; gated by
			// service-to-service mTLS at the gateway).
			r.Route("/solana", func(r chi.Router) {
				r.Get("/wallet", h.solanaWallet)
				r.Post("/run-daily", h.solanaRunDaily)
				r.Post("/burn-now", h.solanaBurnNow)
			})

			// Tax reports
			r.Post("/tax/{userID}/generate", h.taxGenerate)
		})

		// Connect-RPC: ApiKeyService — proxy-gateway + build-gateway
		// call ValidateApiKey on the hot path; gateway-bff calls
		// Create/List/Revoke on behalf of authenticated users.
		apiKeys := NewApiKeyHandler(d.Store)
		path, hh := billingv1connect.NewApiKeyServiceHandler(apiKeys)
		r.Mount(path, hh)
	}
}

// MountStub is a backwards-compatible Mount used by main_test.go's earlier
// scaffolding. It mounts a no-op JSON envelope.
func MountStub(r chi.Router) {
	r.Route("/v1", func(r chi.Router) {
		r.Get("/", indexHandler)
	})
}

func indexHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"service": "billing-svc",
		"status":  "stub",
	})
}

// ── handler set ─────────────────────────────────────────────────────

type handlers struct {
	deps Deps
}

func (h *handlers) index(w http.ResponseWriter, _ *http.Request) {
	stripeEnabled := h.deps.Stripe != nil
	solanaEnabled := h.deps.Solana != nil && h.deps.Solana.Enabled()
	writeJSON(w, http.StatusOK, map[string]any{
		"service":        "billing-svc",
		"status":         "ok",
		"stripe_enabled": stripeEnabled,
		"solana_enabled": solanaEnabled,
		"wallet_address": walletOr(h.deps.Solana, ""),
	})
}

// ── subscriptions ───────────────────────────────────────────────────

func (h *handlers) getSubscription(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	sub, err := h.deps.Store.GetSubscriptionByWorkspace(r.Context(), workspaceID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "no subscription")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sub)
}

type checkoutReq struct {
	Tier       string `json:"tier"`
	SuccessURL string `json:"success_url"`
	CancelURL  string `json:"cancel_url"`
}

func (h *handlers) createCheckoutSession(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	var req checkoutReq
	if err := decodeJSON(r.Body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.deps.Stripe == nil {
		writeErr(w, http.StatusServiceUnavailable, "stripe disabled")
		return
	}
	url, err := h.deps.Stripe.CreateCheckoutSession(r.Context(), workspaceID, req.Tier, req.SuccessURL, req.CancelURL)
	if err != nil {
		if stripeapi.IsStripeDisabled(err) {
			writeErr(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"checkout_url": url})
}

type portalReq struct {
	ReturnURL string `json:"return_url"`
}

func (h *handlers) createPortalSession(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	var req portalReq
	if err := decodeJSON(r.Body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.deps.Stripe == nil {
		writeErr(w, http.StatusServiceUnavailable, "stripe disabled")
		return
	}
	url, err := h.deps.Stripe.CreatePortalSession(r.Context(), workspaceID, req.ReturnURL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"portal_url": url})
}

func (h *handlers) listInvoices(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	limit := intParam(r, "limit", 25)
	offset := intParam(r, "offset", 0)
	invs, err := h.deps.Store.ListInvoicesByWorkspace(r.Context(), workspaceID, limit, offset)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"invoices": invs})
}

// ── webhook ─────────────────────────────────────────────────────────

// stripeWebhook reads the raw request body, validates the Stripe-Signature
// header against STRIPE_WEBHOOK_SECRET, and dispatches to the typed handler.
func (h *handlers) stripeWebhook(w http.ResponseWriter, r *http.Request) {
	if h.deps.Stripe == nil {
		writeErr(w, http.StatusServiceUnavailable, "stripe disabled")
		return
	}
	body, err := stripeapi.ReadBody(r.Body, 1<<20)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "read body")
		return
	}
	if err := h.deps.Stripe.HandleWebhook(r.Context(), body, r.Header.Get("Stripe-Signature")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ── Stripe Connect provider payouts ─────────────────────────────────

type onboardReq struct {
	ReturnURL  string `json:"return_url"`
	RefreshURL string `json:"refresh_url"`
}

func (h *handlers) startPayoutOnboarding(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req onboardReq
	if err := decodeJSON(r.Body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.deps.Stripe == nil {
		writeErr(w, http.StatusServiceUnavailable, "stripe disabled")
		return
	}
	url, err := h.deps.Stripe.StartPayoutOnboarding(r.Context(), userID, req.ReturnURL, req.RefreshURL)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"onboarding_url": url})
}

func (h *handlers) getPayoutAccount(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	if h.deps.Stripe == nil {
		writeErr(w, http.StatusServiceUnavailable, "stripe disabled")
		return
	}
	acct, err := h.deps.Stripe.GetPayoutAccount(r.Context(), userID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "no payout account")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, acct)
}

type instantPayoutReq struct {
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
}

func (h *handlers) requestInstantPayout(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req instantPayoutReq
	if err := decodeJSON(r.Body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.deps.Stripe == nil {
		writeErr(w, http.StatusServiceUnavailable, "stripe disabled")
		return
	}
	p, err := h.deps.Stripe.RequestInstantPayout(r.Context(), userID, req.AmountCents, req.Currency)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// ── Solana ──────────────────────────────────────────────────────────

func (h *handlers) solanaWallet(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Solana == nil {
		writeErr(w, http.StatusServiceUnavailable, "solana disabled")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":        h.deps.Solana.Enabled(),
		"wallet_address": h.deps.Solana.WalletAddress(),
	})
}

func (h *handlers) solanaRunDaily(w http.ResponseWriter, r *http.Request) {
	if h.deps.Solana == nil {
		writeErr(w, http.StatusServiceUnavailable, "solana disabled")
		return
	}
	day := time.Now().UTC().AddDate(0, 0, -1)
	if d := r.URL.Query().Get("day"); d != "" {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "day must be YYYY-MM-DD")
			return
		}
		day = t
	}
	if err := h.deps.Solana.RunDailySwapAndDistribute(r.Context(), day); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handlers) solanaBurnNow(w http.ResponseWriter, r *http.Request) {
	if h.deps.Solana == nil {
		writeErr(w, http.StatusServiceUnavailable, "solana disabled")
		return
	}
	day := time.Now().UTC().AddDate(0, 0, -1)
	if err := h.deps.Solana.RunBurnLoop(r.Context(), day); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ── tax ─────────────────────────────────────────────────────────────

type taxReq struct {
	Year       int   `json:"year"`
	Quarter    int   `json:"quarter"`
	CashCents  int64 `json:"cash_cents"`
	TokenCents int64 `json:"token_cents"`
}

func (h *handlers) taxGenerate(w http.ResponseWriter, r *http.Request) {
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var req taxReq
	if err := decodeJSON(r.Body, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.deps.Tax == nil {
		writeErr(w, http.StatusServiceUnavailable, "tax module disabled")
		return
	}
	rows, err := h.deps.Tax.GenerateAndPersist(r.Context(), userID, tax.Period{Year: req.Year, Quarter: req.Quarter}, req.CashCents, req.TokenCents)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{
			"form_type":   row.FormType,
			"year":        row.TaxYear,
			"quarter":     row.Quarter,
			"cash_cents":  row.CashCents,
			"token_cents": row.TokenCents,
			"pdf_bytes":   len(row.PDFBytes),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": out})
}

// ── helpers ─────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r io.Reader, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func intParam(r *http.Request, name string, def int) int {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func walletOr(s *solana.Service, def string) string {
	if s == nil {
		return def
	}
	addr := s.WalletAddress()
	if addr == "" {
		return def
	}
	return addr
}

// statusFromTier is exported (lowercase) so the test layer can verify
// the small mapping table without importing it from the proto-gen.
var statusFromTier = map[string]string{
	"PAYG":       "active",
	"STARTER":    "active",
	"GROWTH":     "active",
	"ENTERPRISE": "active",
}

// SanitizeTier folds the proto enum tail to the env-var key form.
func SanitizeTier(s string) string {
	return strings.TrimPrefix(strings.ToUpper(s), "SUBSCRIPTION_TIER_")
}
