package handlers

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1/providersv1connect"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

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

	for {
		select {
		case <-ctx.Done():
			return nil
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
		}
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
