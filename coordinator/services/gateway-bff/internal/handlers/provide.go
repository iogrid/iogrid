package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/encoding/protojson"
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
// Config is serialised via protojson (NOT stdlib encoding/json) because the
// web client uses protobuf-es, which speaks proto3-JSON (camelCase jsonName
// e.g. "bandwidthCapGbPerMonth"). The protoc-gen-go struct tags are snake_case
// ("bandwidth_cap_gb_per_month"), so a stdlib round-trip silently dropped
// every cap on read and rejected the camelCase payload on write with
// `json: unknown field "bandwidthCapGbPerMonth"` (#630). json.RawMessage lets
// us splice the protojson-encoded config into this stdlib-encoded envelope.
type providerSchedule struct {
	Config      json.RawMessage         `json:"config"`
	HasProvider bool                    `json:"has_provider"`
	Providers   []*providersv1.Provider `json:"providers"`
}

// scheduleEnvelope builds the GET /provide/schedule response, encoding the
// SchedulingConfig with protojson so its field names match the web's
// protobuf-es client. A nil cfg serialises as JSON null.
func scheduleEnvelope(cfg *providersv1.SchedulingConfig, hasProvider bool, providers []*providersv1.Provider) (providerSchedule, error) {
	env := providerSchedule{Config: json.RawMessage("null"), HasProvider: hasProvider, Providers: providers}
	if cfg != nil {
		raw, err := protojson.Marshal(cfg)
		if err != nil {
			return env, err
		}
		env.Config = raw
	}
	return env, nil
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
		env, _ := scheduleEnvelope(nil, false, owned)
		writeJSON(w, http.StatusOK, env)
		return
	}
	resp, err := a.Clients.ProvidersScheduling.GetSchedulingConfig(r.Context(), &providersv1.GetSchedulingConfigRequest{
		ProviderId: &commonv1.UUID{Value: pid},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	env, err := scheduleEnvelope(resp.GetConfig(), true, owned)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "failed to encode scheduling config")
		return
	}
	writeJSON(w, http.StatusOK, env)
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
	// Config carries the protobuf-es proto3-JSON shape (camelCase). Capture
	// it raw, then protojson.Unmarshal — stdlib decode against the snake_case
	// protoc-gen-go tags rejected the camelCase payload (#630).
	var body struct {
		Config json.RawMessage `json:"config"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if len(body.Config) == 0 || string(body.Config) == "null" {
		writeError(w, http.StatusBadRequest, "bad_request", "config required")
		return
	}
	cfg := &providersv1.SchedulingConfig{}
	if err := protojson.Unmarshal(body.Config, cfg); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resp, err := a.Clients.ProvidersScheduling.UpdateSchedulingConfig(r.Context(), &providersv1.UpdateSchedulingConfigRequest{
		Config: cfg,
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
//  2. No query param: pick the caller's PRIMARY provider deterministically.
//     Ordering (see sortOwnedProviders):
//       a. is_primary DESC
//       b. status == ACTIVE > anything else (an OFFLINE primary still
//          beats an ACTIVE secondary)
//       c. last_seen_at DESC (more recent heartbeat wins ties)
//       d. registered_at DESC (newer pair wins last-second ties)
//       e. id ASC (stable lexical tiebreaker)
//     Owners with zero rows return ("", []Provider{}, true) so the
//     handler renders the empty-state #313 envelope.
//
// Returns ("", nil, false) only when the handler should bail out (HTTP
// response already written). Returns ("", []Provider{}, true) when the
// caller is authenticated but has no paired provider yet — that's a
// 200/OK empty-state path, NOT an error.
//
// Replaces the pre-#325 "first ACTIVE" pick, which was indeterminate
// for owners with ≥2 paired daemons because ListProviders ordered by
// id ASC — Hatice's manual-test daemon happened to win against her real
// Mac, so /provide/schedule rendered the wrong caps. Issue #325 (family
// of #305).
func (a *API) resolveOwnedProviderID(w http.ResponseWriter, r *http.Request, callerUserID string) (string, []*providersv1.Provider, bool) {
	owned, ok := a.listOwnedProviders(w, r.Context(), callerUserID)
	if !ok {
		return "", nil, false
	}
	owned = sortOwnedProviders(owned)
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
	return owned[0].GetId().GetValue(), owned, true
}

// sortOwnedProviders returns owned re-sorted by the deterministic
// per-owner primary-first ordering used by /provide/* default selection.
// Pure: never mutates the input slice.
func sortOwnedProviders(owned []*providersv1.Provider) []*providersv1.Provider {
	out := append([]*providersv1.Provider(nil), owned...)
	sort.SliceStable(out, func(i, j int) bool {
		ai, bi := out[i], out[j]
		// 1. is_primary DESC
		if ai.GetIsPrimary() != bi.GetIsPrimary() {
			return ai.GetIsPrimary()
		}
		// 2. ACTIVE before anything else (so a stale-OFFLINE non-primary
		//    doesn't outrank a live ACTIVE one when neither is primary).
		ais := ai.GetStatus() == providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE
		bis := bi.GetStatus() == providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE
		if ais != bis {
			return ais
		}
		// 3. last_seen_at DESC (most-recently-heartbeated wins ties)
		al := ai.GetLastSeenAt().AsTime()
		bl := bi.GetLastSeenAt().AsTime()
		if !al.Equal(bl) {
			return al.After(bl)
		}
		// 4. registered_at DESC (newer pair wins last-second ties)
		ar := ai.GetRegisteredAt().AsTime()
		br := bi.GetRegisteredAt().AsTime()
		if !ar.Equal(br) {
			return ar.After(br)
		}
		// 5. id ASC (stable lexical tiebreaker)
		return ai.GetId().GetValue() < bi.GetId().GetValue()
	})
	return out
}

// SetPrimaryProvider promotes one of the caller's owned providers to
// the per-owner primary slot. Backs PUT /api/v1/provide/primary-provider.
// Request body: {"provider_id": "<UUID>"}. Returns the freshly-promoted
// Provider proto on success.
//
//	PUT /api/v1/provide/primary-provider  { "provider_id": "..." }
func (a *API) SetPrimaryProvider(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	if a.Clients == nil || a.Clients.ProvidersRegistration == nil {
		writeError(w, http.StatusServiceUnavailable, "registration_client_unwired",
			"providers-svc Registration client not configured")
		return
	}
	var body struct {
		ProviderID string `json:"provider_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	pidParsed, ok := parseUUIDParam(w, body.ProviderID, "provider_id")
	if !ok {
		return
	}
	resp, err := a.Clients.ProvidersRegistration.SetPrimaryProvider(r.Context(), &providersv1.SetPrimaryProviderRequest{
		OwnerUserId: &commonv1.UUID{Value: claims.UserID().String()},
		ProviderId:  &commonv1.UUID{Value: pidParsed.String()},
	})
	if err != nil {
		// providers-svc returns PERMISSION_DENIED when caller doesn't
		// own the provider; writeUpstreamError maps that to HTTP 403.
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
