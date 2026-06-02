// earnings.go handles the /provide/earnings headline-card + payout-
// method surface (#324). Three routes:
//
//	GET  /api/v1/provide/earnings/summary  → billing-svc.GetEarningsSummary
//	GET  /api/v1/provide/payout-method     → billing-svc.GetPayoutMethod
//	PUT  /api/v1/provide/payout-method     → billing-svc.SetPayoutMethod
//
// The earnings-summary route runs the same resolveOwnedProviderID gate
// the other /provide/* endpoints use (#305 / PR #310), so an
// authenticated user with zero paired providers gets a zero-typed
// envelope rather than a synthesised one keyed by user_id.
//
// The payout-method routes are user-scoped (NOT provider-scoped) — a
// user with multiple daemons receives one consolidated election —
// because billing-svc's payout_methods table primary-keys on user_id.
package handlers

import (
	"net/http"
	"strings"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"

	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// GetProviderEarningsSummary returns the headline-card aggregation that
// /provide/earnings renders at the top of the page.
//
//	GET /api/v1/provide/earnings/summary
func (a *API) GetProviderEarningsSummary(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	if a.Clients == nil || a.Clients.BillingEarnings == nil {
		writeError(w, http.StatusServiceUnavailable, "earnings_client_unwired",
			"billing-svc Earnings client not configured")
		return
	}
	pid, _, ok := a.resolveOwnedProviderID(w, r, claims.UserID().String())
	if !ok {
		return
	}
	if pid == "" {
		// Zero-provider Phase-0 path: return a typed empty envelope with
		// currency "GRID" so the page renders "0 $GRID" via #315's
		// formatMoney, instead of "—" or a 404.
		//
		// protojson (NOT stdlib writeJSON) so the web protobuf-es client
		// reads camelCase fields (totalEarned, pendingPayout) and Money as
		// {currency, micros}. See #633.
		writeProtoJSON(w, http.StatusOK, &billingv1.GetEarningsSummaryResponse{
			Summary: emptyGridSummary(),
		})
		return
	}
	resp, err := a.Clients.BillingEarnings.GetEarningsSummary(r.Context(), &billingv1.GetEarningsSummaryRequest{
		ProviderId: &commonv1.UUID{Value: pid},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	// protojson so the headline cards (lifetime / last_30d / last_7d /
	// pending) actually populate on the web — stdlib snake_case made every
	// card read `undefined` ⇒ "0 $GRID" even for credited providers. #633.
	writeProtoJSON(w, http.StatusOK, resp)
}

// GetProviderPayoutMethod returns the caller's saved election.
//
//	GET /api/v1/provide/payout-method
func (a *API) GetProviderPayoutMethod(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	if a.Clients == nil || a.Clients.BillingEarnings == nil {
		writeError(w, http.StatusServiceUnavailable, "earnings_client_unwired",
			"billing-svc Earnings client not configured")
		return
	}
	uid := claims.UserID().String()
	resp, err := a.Clients.BillingEarnings.GetPayoutMethod(r.Context(), &billingv1.GetPayoutMethodRequest{
		UserId: &commonv1.UUID{Value: uid},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// payoutMethodBody mirrors the JSON the web client sends.
//
// `kind` is the proto enum tail (UNSPECIFIED | CASH_USDC | FREE_VPN |
// CHARITY); destination_address and charity_id are required only for the
// kind that uses them — billing-svc enforces that, the BFF just forwards.
type payoutMethodBody struct {
	Kind               string `json:"kind"`
	DestinationAddress string `json:"destination_address,omitempty"`
	CharityID          string `json:"charity_id,omitempty"`
}

// SetProviderPayoutMethod persists the election. The path is PUT
// (NOT POST) because the underlying storage is upsert-keyed-by-user_id;
// PUT signals idempotent replacement to API consumers.
//
//	PUT /api/v1/provide/payout-method  { kind, destination_address?, charity_id? }
func (a *API) SetProviderPayoutMethod(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	if a.Clients == nil || a.Clients.BillingEarnings == nil {
		writeError(w, http.StatusServiceUnavailable, "earnings_client_unwired",
			"billing-svc Earnings client not configured")
		return
	}
	var body payoutMethodBody
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	kind, ok := parsePayoutMethodKind(body.Kind)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_request", "unknown payout method kind")
		return
	}
	uid := claims.UserID().String()
	resp, err := a.Clients.BillingEarnings.SetPayoutMethod(r.Context(), &billingv1.SetPayoutMethodRequest{
		UserId:             &commonv1.UUID{Value: uid},
		Kind:               kind,
		DestinationAddress: strings.TrimSpace(body.DestinationAddress),
		CharityId:          strings.TrimSpace(body.CharityID),
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// parsePayoutMethodKind maps the JSON string to the proto enum.
// Accepts both the bare form ("CASH_USDC") and the fully-qualified
// form ("PAYOUT_METHOD_KIND_CASH_USDC") so the web client can send
// whichever it prefers. Empty string defaults to UNSPECIFIED (user is
// reverting to "hold $GRID").
func parsePayoutMethodKind(s string) (billingv1.PayoutMethodKind, bool) {
	v := strings.ToUpper(strings.TrimSpace(s))
	v = strings.TrimPrefix(v, "PAYOUT_METHOD_KIND_")
	switch v {
	case "", "UNSPECIFIED":
		return billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_UNSPECIFIED, true
	case "CASH_USDC", "CASH":
		return billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CASH_USDC, true
	case "FREE_VPN", "VPN":
		return billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_FREE_VPN, true
	case "CHARITY":
		return billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_CHARITY, true
	default:
		return billingv1.PayoutMethodKind_PAYOUT_METHOD_KIND_UNSPECIFIED, false
	}
}

// emptyGridSummary returns the zero-state envelope the no-provider
// path emits. Currency is "GRID" (the native ledger currency, #312/
// #315), all Money values are typed-zero so formatMoney renders
// "0 $GRID" on the cards instead of "—".
func emptyGridSummary() *billingv1.EarningsSummary {
	z := &commonv1.Money{Currency: "GRID", Micros: 0}
	return &billingv1.EarningsSummary{
		TotalEarned:   z,
		Last_30D:      z,
		Last_7D:       z,
		PendingPayout: z,
	}
}

