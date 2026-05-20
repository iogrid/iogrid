package handlers

import (
	"context"
	"net/http"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/sse"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
)

// providerDashboard is the aggregated payload returned by GET
// /api/v1/provide/dashboard. We fan out to providers-svc in parallel.
type providerDashboard struct {
	Earnings     *providersv1.GetEarningsSummaryResponse `json:"earnings"`
	State        *providersv1.GetCurrentStateResponse    `json:"state"`
	RecentEvents []*providersv1.AuditEvent               `json:"recent_events"`
	// HasProvider is false when the caller owns zero paired providers.
	// In that case Earnings/State/RecentEvents are also nil. The web
	// layer falls through to the "Install the daemon" empty-state.
	HasProvider bool                     `json:"has_provider"`
	Providers   []*providersv1.Provider  `json:"providers"`
}

// providerSchedule mirrors the JSON envelope returned by GET
// /api/v1/provide/schedule. Carries an explicit has_provider flag so the
// frontend can distinguish "no daemon paired yet" from "daemon paired
// but config never set". Issue #305.
type providerSchedule struct {
	Config      *providersv1.SchedulingConfig `json:"config"`
	HasProvider bool                          `json:"has_provider"`
	Providers   []*providersv1.Provider       `json:"providers"`
}

// GetProviderDashboard fans out three parallel calls to providers-svc
// (earnings, current state, recent audit page) and combines them into
// a single JSON envelope.
//
//	GET /api/v1/provide/dashboard?provider_id=<UUID>
func (a *API) GetProviderDashboard(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	pid, owned, ok := a.resolveOwnedProviderID(w, r, claims.UserID().String())
	if !ok {
		return
	}
	if pid == "" {
		// Caller owns no paired providers — return an empty envelope so
		// the web layer can render the "Install the daemon" CTA. We
		// deliberately do NOT call providers-svc with a synthesised
		// provider_id (#305).
		writeJSON(w, http.StatusOK, providerDashboard{HasProvider: false, Providers: owned})
		return
	}
	now := time.Now().UTC()
	window := &commonv1.TimeWindow{
		Start: timestamppb.New(now.AddDate(0, -1, 0)),
		End:   timestamppb.New(now),
	}

	g, ctx := errgroup.WithContext(r.Context())
	var (
		earnings *providersv1.GetEarningsSummaryResponse
		state    *providersv1.GetCurrentStateResponse
		events   *providersv1.ListAuditEventsResponse
	)
	g.Go(func() error {
		resp, err := a.Clients.ProvidersDashboard.GetEarningsSummary(ctx, &providersv1.GetEarningsSummaryRequest{
			ProviderId: &commonv1.UUID{Value: pid},
			Window:     window,
		})
		if err != nil {
			return err
		}
		earnings = resp
		return nil
	})
	g.Go(func() error {
		resp, err := a.Clients.ProvidersScheduling.GetCurrentState(ctx, &providersv1.GetCurrentStateRequest{
			ProviderId: &commonv1.UUID{Value: pid},
		})
		if err != nil {
			return err
		}
		state = resp
		return nil
	})
	g.Go(func() error {
		resp, err := a.Clients.ProvidersDashboard.ListAuditEvents(ctx, &providersv1.ListAuditEventsRequest{
			ProviderId: &commonv1.UUID{Value: pid},
			Page:       &commonv1.PageRequest{PageSize: 20},
		})
		if err != nil {
			return err
		}
		events = resp
		return nil
	})
	if err := g.Wait(); err != nil {
		writeUpstreamError(w, err)
		return
	}
	out := providerDashboard{
		Earnings:    earnings,
		State:       state,
		HasProvider: true,
		Providers:   owned,
	}
	if events != nil {
		out.RecentEvents = events.Events
	}
	writeJSON(w, http.StatusOK, out)
}

// GetProviderSchedule returns the current scheduling config keyed by the
// provider actually owned by the caller (NOT by the caller's user_id).
// If the caller has zero paired providers, the response is
// {"config": null, "has_provider": false, "providers": []} with HTTP 200
// — the web layer falls through to the "Install the daemon" CTA. See
// #305 for the bug this fixes: previously this endpoint synthesised a
// SchedulingConfig keyed by the caller's user_id, which leaked confusing
// data and could cross-wire identity with provider_id in downstream code.
//
//	GET /api/v1/provide/schedule?provider_id=<UUID>
func (a *API) GetProviderSchedule(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	pid, owned, ok := a.resolveOwnedProviderID(w, r, claims.UserID().String())
	if !ok {
		return
	}
	if pid == "" {
		writeJSON(w, http.StatusOK, providerSchedule{HasProvider: false, Providers: owned})
		return
	}
	resp, err := a.Clients.ProvidersScheduling.GetSchedulingConfig(r.Context(), &providersv1.GetSchedulingConfigRequest{
		ProviderId: &commonv1.UUID{Value: pid},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, providerSchedule{
		Config:      resp.GetConfig(),
		HasProvider: true,
		Providers:   owned,
	})
}

// UpdateProviderSchedule replaces the scheduling config (read-modify-
// write semantics enforced by providers-svc).
//
//	POST /api/v1/provide/schedule
//	  { config: { ...full SchedulingConfig... } }
func (a *API) UpdateProviderSchedule(w http.ResponseWriter, r *http.Request) {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	var body struct {
		Config *providersv1.SchedulingConfig `json:"config"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if body.Config == nil {
		writeError(w, http.StatusBadRequest, "bad_request", "config required")
		return
	}
	resp, err := a.Clients.ProvidersScheduling.UpdateSchedulingConfig(r.Context(), &providersv1.UpdateSchedulingConfigRequest{
		Config: body.Config,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// StreamProviderAudit produces the live transparency feed for the
// current user's provider. Backed by providers-svc's bidi audit stream.
//
//	GET /api/v1/provide/audit/stream?provider_id=<UUID>  (SSE)
func (a *API) StreamProviderAudit(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	pid, _, ok := a.resolveOwnedProviderID(w, r, claims.UserID().String())
	if !ok {
		return
	}
	if pid == "" {
		writeError(w, http.StatusNotFound, "no_provider", "caller owns no paired provider")
		return
	}
	sse.Handler(sse.ProducerFunc(func(ctx context.Context, lastEventID string, emit func(sse.Event) error) error {
		stream, err := a.Clients.ProvidersDashboard.StreamAuditEvents(ctx, &providersv1.StreamAuditEventsRequest{
			ProviderId: &commonv1.UUID{Value: pid},
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
			// Drop providers-svc KEEPALIVE frames at this layer. They
			// exist solely to flush Connect response headers + tick
			// against the BFF Connect client's per-call timeout (see
			// providers-svc dashboard.go #323). The downstream SSE
			// handler ticks its own `:keep-alive` comments at the
			// same cadence, so we don't need to push pseudo
			// audit_event frames at the browser — that would only
			// muddy the transparency feed.
			if ev.GetKind() == providersv1.EventKind_EVENT_KIND_KEEPALIVE {
				continue
			}
			id := ""
			if ev.Id != nil {
				id = ev.Id.Value
			}
			if err := emit(sse.Event{
				ID:       id,
				Kind:     "audit_event",
				DataJSON: ev,
			}); err != nil {
				return err
			}
		}
		return stream.Err()
	}), 15*time.Second).ServeHTTP(w, r)
}

// GetProviderEarnings returns just the earnings summary (faster than
// the full dashboard when the caller only needs the headline number).
//
//	GET /api/v1/provide/earnings?provider_id=<UUID>&start=<ISO>&end=<ISO>
func (a *API) GetProviderEarnings(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	pid, _, ok := a.resolveOwnedProviderID(w, r, claims.UserID().String())
	if !ok {
		return
	}
	if pid == "" {
		writeJSON(w, http.StatusOK, &providersv1.GetEarningsSummaryResponse{})
		return
	}
	window := parseTimeWindow(r)
	resp, err := a.Clients.ProvidersDashboard.GetEarningsSummary(r.Context(), &providersv1.GetEarningsSummaryRequest{
		ProviderId: &commonv1.UUID{Value: pid},
		Window:     window,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// resolveOwnedProviderID figures out which provider the request should
// be scoped to, GATED ON OWNERSHIP. Order of precedence:
//
//  1. ?provider_id=<UUID> in the query string — caller must actually own
//     this provider (we validate by listing the caller's providers and
//     matching). Mismatch returns ("", nil, false) with HTTP 403.
//  2. No query param: look up the caller's owned providers via
//     ProviderRegistrationService.ListProviders(owner_user_id=caller). If
//     exactly one paired provider exists, return its id. If zero, return
//     ("", []Provider{}, true) so the caller can render an empty state.
//     If multiple, return the first ACTIVE one (Phase-0 single-daemon
//     assumption — multi-daemon-per-user is a future feature).
//
// Returns ("", nil, false) only when the handler should bail out (HTTP
// response already written). Returns ("", []Provider{}, true) when the
// caller is authenticated but has no paired provider yet — that's a
// 200/OK empty-state path, NOT an error.
//
// Replaces the legacy resolveProviderID, which fell back to the caller's
// user_id as if it were a provider_id. Downstream providers-svc then
// synthesised a default SchedulingConfig keyed by that user_id (#305).
func (a *API) resolveOwnedProviderID(w http.ResponseWriter, r *http.Request, callerUserID string) (string, []*providersv1.Provider, bool) {
	owned, ok := a.listOwnedProviders(w, r.Context(), callerUserID)
	if !ok {
		return "", nil, false
	}
	if q := r.URL.Query().Get("provider_id"); q != "" {
		id, ok := parseUUIDParam(w, q, "provider_id")
		if !ok {
			return "", nil, false
		}
		want := id.String()
		for _, p := range owned {
			if p.GetId().GetValue() == want {
				return want, owned, true
			}
		}
		writeError(w, http.StatusForbidden, "forbidden", "caller does not own provider "+want)
		return "", nil, false
	}
	if len(owned) == 0 {
		return "", owned, true
	}
	// Prefer the first ACTIVE provider; otherwise fall through to the
	// first row so the dashboard still shows the suspended/pending
	// daemon. Phase 0 only ever ships 1 daemon per user.
	for _, p := range owned {
		if p.GetStatus() == providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE {
			return p.GetId().GetValue(), owned, true
		}
	}
	return owned[0].GetId().GetValue(), owned, true
}

// listOwnedProviders queries providers-svc for the set of providers
// owned by callerUserID. Surfaces upstream errors via writeUpstreamError
// (and writes HTTP 503 if the Registration client isn't wired — e.g.
// pre-#305 binaries that have only Dashboard+Scheduling clients).
func (a *API) listOwnedProviders(w http.ResponseWriter, ctx context.Context, callerUserID string) ([]*providersv1.Provider, bool) {
	if a.Clients == nil || a.Clients.ProvidersRegistration == nil {
		writeError(w, http.StatusServiceUnavailable, "registration_client_unwired",
			"providers-svc Registration client not configured; cannot gate /provide/* by ownership")
		return nil, false
	}
	uid, ok := parseUUIDParam(w, callerUserID, "caller_user_id")
	if !ok {
		return nil, false
	}
	resp, err := a.Clients.ProvidersRegistration.ListProviders(ctx, &providersv1.ListProvidersRequest{
		OwnerUserId: &commonv1.UUID{Value: uid.String()},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return nil, false
	}
	return resp.GetProviders(), true
}

// parseTimeWindow reads ?start= and ?end= query params in RFC 3339
// form. Either may be omitted (= open bound).
func parseTimeWindow(r *http.Request) *commonv1.TimeWindow {
	tw := &commonv1.TimeWindow{}
	if s := r.URL.Query().Get("start"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			tw.Start = timestamppb.New(t)
		}
	}
	if e := r.URL.Query().Get("end"); e != "" {
		if t, err := time.Parse(time.RFC3339, e); err == nil {
			tw.End = timestamppb.New(t)
		}
	}
	if tw.Start == nil && tw.End == nil {
		return nil
	}
	return tw
}
