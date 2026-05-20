package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1/providersv1connect"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// streamKeepaliveInterval is the cadence at which StreamAuditEvents
// emits a KEEPALIVE AuditEvent when no real events have flowed. It must
// stay shorter than (a) the gateway-bff Connect call timeout and
// (b) any intermediate proxy idle-timeout (typically 30s on traefik).
// 15s is the same value the gateway-bff SSE handler uses for its
// downstream comment ticker; matching them keeps the two layers in
// lockstep.
const streamKeepaliveInterval = 15 * time.Second

// DashboardHandler implements the DashboardService gRPC.
type DashboardHandler struct {
	providersv1connect.UnimplementedDashboardServiceHandler
	Store store.Store
	Log   *slog.Logger
}

func NewDashboardHandler(s store.Store, log *slog.Logger) *DashboardHandler {
	if log == nil {
		log = slog.Default()
	}
	return &DashboardHandler{Store: s, Log: log}
}

func (h *DashboardHandler) ListAuditEvents(
	ctx context.Context,
	req *connect.Request[providersv1.ListAuditEventsRequest],
) (*connect.Response[providersv1.ListAuditEventsResponse], error) {
	id := uuidString(req.Msg.GetProviderId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	q := store.AuditQuery{}
	if p := req.Msg.GetPage(); p != nil {
		q.PageSize = int(p.GetPageSize())
		q.PageToken = p.GetPageToken()
	}
	if w := req.Msg.GetWindow(); w != nil {
		q.From = tsOrZero(w.GetStart())
		q.To = tsOrZero(w.GetEnd())
	}
	for _, k := range req.Msg.GetKindFilter() {
		q.Kinds = append(q.Kinds, k.String())
	}

	events, next, err := h.Store.ListAuditEvents(ctx, id, q)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	out := &providersv1.ListAuditEventsResponse{
		Events: make([]*providersv1.AuditEvent, 0, len(events)),
		Page:   &commonv1.PageResponse{NextPageToken: next},
	}
	for _, e := range events {
		out.Events = append(out.Events, auditEventToProto(e))
	}
	return connect.NewResponse(out), nil
}

// StreamAuditEvents emits events newly appended to the audit log for the
// given provider until the client disconnects or the server shuts down.
//
// Wire-level contract (#323): the FIRST frame on the stream is ALWAYS a
// KEEPALIVE AuditEvent, emitted synchronously before we block on the
// store subscription. Connect-Go's server-streaming protocol defers
// sending HTTP response headers until the first Send — so without this
// initial frame a freshly paired provider (which by definition has zero
// workload events) leaves the gateway-bff Connect client blocked in
// Receive() until the per-call timeout fires, surfacing as
// deadline_exceeded at the BFF and a permanent "Connecting…" pill in
// the web /provide/audit feed. Hatice flagged this on the DoD walk for
// EPIC #309.
//
// While the subscription is alive we additionally tick a KEEPALIVE
// every streamKeepaliveInterval to defeat intermediate proxy idle
// timeouts AND to keep the gateway-bff Connect client from tripping
// its own per-call timeout when the provider has long stretches with
// no real events.
//
// UI consumers MUST treat KEEPALIVE as "connection alive, no new
// data" — see the proto comment on EVENT_KIND_KEEPALIVE.
func (h *DashboardHandler) StreamAuditEvents(
	ctx context.Context,
	req *connect.Request[providersv1.StreamAuditEventsRequest],
	stream *connect.ServerStream[providersv1.AuditEvent],
) error {
	id := uuidString(req.Msg.GetProviderId())
	if id == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	kindFilter := map[string]struct{}{}
	for _, k := range req.Msg.GetKindFilter() {
		kindFilter[k.String()] = struct{}{}
	}

	sub, cancel := h.Store.SubscribeAuditEvents(id)
	defer cancel()

	// Emit the initial KEEPALIVE synchronously BEFORE entering the
	// receive loop. Connect's ServerStream.Send flushes the underlying
	// HTTP response writer per frame, so this guarantees a response
	// header + first frame land on the wire within ms of the request
	// reaching this handler — which is the load-bearing invariant for
	// the BFF SSE pipe to flip "Connecting" → "Live" promptly.
	if err := stream.Send(newKeepaliveEvent(id)); err != nil {
		return err
	}

	ticker := time.NewTicker(streamKeepaliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			// No real event in the last interval — send a KEEPALIVE
			// to keep the wire alive end-to-end (proxies + BFF
			// Connect client deadlines).
			if err := stream.Send(newKeepaliveEvent(id)); err != nil {
				return err
			}
		case e, ok := <-sub:
			if !ok {
				return nil
			}
			if len(kindFilter) > 0 {
				if _, want := kindFilter[e.Kind]; !want {
					continue
				}
			}
			if err := stream.Send(auditEventToProto(e)); err != nil {
				return err
			}
			// Real traffic resets the keepalive cadence so we don't
			// double-tick right after a burst.
			ticker.Reset(streamKeepaliveInterval)
		}
	}
}

// newKeepaliveEvent builds a minimal KEEPALIVE AuditEvent scoped to the
// given provider. occurred_at carries server-now so UIs that decide to
// log keepalives for diagnostics have a usable timestamp; every other
// field is left at the proto zero value.
func newKeepaliveEvent(providerID string) *providersv1.AuditEvent {
	return &providersv1.AuditEvent{
		ProviderId: &commonv1.UUID{Value: providerID},
		Kind:       providersv1.EventKind_EVENT_KIND_KEEPALIVE,
		OccurredAt: timestamppb.Now(),
	}
}

func (h *DashboardHandler) GetEarningsSummary(
	ctx context.Context,
	req *connect.Request[providersv1.GetEarningsSummaryRequest],
) (*connect.Response[providersv1.GetEarningsSummaryResponse], error) {
	id := uuidString(req.Msg.GetProviderId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("provider_id required"))
	}
	window := req.Msg.GetWindow()
	var from, to = tsOrZero(window.GetStart()), tsOrZero(window.GetEnd())

	total, byType, currency, err := h.Store.SumEarnings(ctx, id, from, to)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &providersv1.EarningsSummary{
		ProviderId:  &commonv1.UUID{Value: id},
		Window:      window,
		TotalEarned: &commonv1.Money{Currency: currency, Micros: total},
		ByWorkloadType: make(map[string]*commonv1.Money, len(byType)),
	}
	for wt, m := range byType {
		out.ByWorkloadType[wt] = &commonv1.Money{Currency: currency, Micros: m}
	}
	return connect.NewResponse(&providersv1.GetEarningsSummaryResponse{Summary: out}), nil
}

func auditEventToProto(e store.AuditEvent) *providersv1.AuditEvent {
	return &providersv1.AuditEvent{
		Id:                  &commonv1.UUID{Value: e.ID},
		ProviderId:          &commonv1.UUID{Value: e.ProviderID},
		Kind:                providersv1.EventKind(providersv1.EventKind_value[e.Kind]),
		OccurredAt:          timestamppb.New(e.OccurredAt),
		WorkloadType:        slugToWorkloadType(e.WorkloadType),
		Category:            e.Category,
		CustomerDisplayName: e.CustomerDisplayName,
		DestinationSummary:  e.DestinationSummary,
		Bytes:               e.Bytes,
		Metadata:            cloneMap(e.Metadata),
	}
}

func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
