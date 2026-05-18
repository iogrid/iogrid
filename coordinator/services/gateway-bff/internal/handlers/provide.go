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
	pid, ok := resolveProviderID(w, r, claims.UserID().String())
	if !ok {
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
		Earnings: earnings,
		State:    state,
	}
	if events != nil {
		out.RecentEvents = events.Events
	}
	writeJSON(w, http.StatusOK, out)
}

// GetProviderSchedule returns the current scheduling config.
//
//	GET /api/v1/provide/schedule?provider_id=<UUID>
func (a *API) GetProviderSchedule(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	pid, ok := resolveProviderID(w, r, claims.UserID().String())
	if !ok {
		return
	}
	resp, err := a.Clients.ProvidersScheduling.GetSchedulingConfig(r.Context(), &providersv1.GetSchedulingConfigRequest{
		ProviderId: &commonv1.UUID{Value: pid},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
	pid, ok := resolveProviderID(w, r, claims.UserID().String())
	if !ok {
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
	pid, ok := resolveProviderID(w, r, claims.UserID().String())
	if !ok {
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

// resolveProviderID figures out which provider the request should be
// scoped to. Order of precedence: ?provider_id query param > the
// caller's own user id (provider == user in Phase 0 single-tenant
// case). Writes a 400 if the resulting id is malformed.
func resolveProviderID(w http.ResponseWriter, r *http.Request, fallback string) (string, bool) {
	if q := r.URL.Query().Get("provider_id"); q != "" {
		id, ok := parseUUIDParam(w, q, "provider_id")
		if !ok {
			return "", false
		}
		return id.String(), true
	}
	id, ok := parseUUIDParam(w, fallback, "provider_id")
	if !ok {
		return "", false
	}
	return id.String(), true
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
