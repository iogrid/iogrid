package handlers

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1/workloadsv1connect"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// SubmissionHandler implements the WorkloadSubmissionService Connect-Go
// interface. Submission validates the request shape, persists the
// workload as QUEUED, then hands it to the dispatcher; if the dispatcher
// finds a candidate the workload moves to DISPATCHED.
type SubmissionHandler struct {
	workloadsv1connect.UnimplementedWorkloadSubmissionServiceHandler
	Store      store.Store
	Dispatcher *dispatcher.D
	Log        *slog.Logger
}

// NewSubmissionHandler wires the dependencies. Dispatcher may be nil for
// pure-store unit tests; in that case submitted workloads stay QUEUED.
func NewSubmissionHandler(s store.Store, d *dispatcher.D, log *slog.Logger) *SubmissionHandler {
	if log == nil {
		log = slog.Default()
	}
	return &SubmissionHandler{Store: s, Dispatcher: d, Log: log}
}

func (h *SubmissionHandler) SubmitWorkload(
	ctx context.Context,
	req *connect.Request[workloadsv1.SubmitWorkloadRequest],
) (*connect.Response[workloadsv1.SubmitWorkloadResponse], error) {
	in := req.Msg.GetWorkload()
	if in == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload required"))
	}
	w := workloadFromProto(in)
	if w == nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload required"))
	}
	if w.Type == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("workload.type required"))
	}
	if err := validateWorkload(w); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	w.Status = store.StatusQueued
	if err := h.Store.CreateWorkload(ctx, w); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	_ = h.Store.AppendEvent(ctx, store.Event{
		WorkloadID: w.ID,
		NewStatus:  string(store.StatusQueued),
		Note:       "submitted",
	})

	// Best-effort dispatch attempt. If no candidates are available the
	// dispatcher updates the workload to REJECTED but submission is still
	// reported as successful — the caller already has the id and can
	// resubmit when capacity returns.
	if h.Dispatcher != nil {
		if _, err := h.Dispatcher.TryAssign(ctx, w); err != nil {
			h.Log.Info("dispatcher could not place workload yet",
				slog.String("workload_id", w.ID),
				slog.String("error", err.Error()))
		}
	}
	// Refresh from store so the returned status reflects post-dispatch.
	if got, err := h.Store.GetWorkload(ctx, w.ID); err == nil {
		w = got
	}
	return connect.NewResponse(&workloadsv1.SubmitWorkloadResponse{Workload: workloadToProto(w)}), nil
}

func (h *SubmissionHandler) GetWorkload(
	ctx context.Context,
	req *connect.Request[workloadsv1.GetWorkloadRequest],
) (*connect.Response[workloadsv1.GetWorkloadResponse], error) {
	id := uuidString(req.Msg.GetId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("id required"))
	}
	w, err := h.Store.GetWorkload(ctx, id)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return connect.NewResponse(&workloadsv1.GetWorkloadResponse{
		Workload: workloadToProto(w),
		Result:   resultToProto(w),
	}), nil
}

func (h *SubmissionHandler) ListWorkloads(
	ctx context.Context,
	req *connect.Request[workloadsv1.ListWorkloadsRequest],
) (*connect.Response[workloadsv1.ListWorkloadsResponse], error) {
	opts := store.ListOptions{
		WorkspaceID: uuidString(req.Msg.GetWorkspaceId()),
		Type:        workloadTypeSlug(req.Msg.GetTypeFilter()),
		Status:      store.Status(req.Msg.GetStatusFilter()),
	}
	if p := req.Msg.GetPage(); p != nil {
		opts.PageSize = int(p.GetPageSize())
		opts.PageToken = p.GetPageToken()
	}
	if w := req.Msg.GetSubmittedWindow(); w != nil {
		opts.From = tsOrZero(w.GetStart())
		opts.To = tsOrZero(w.GetEnd())
	}
	ws, next, err := h.Store.ListWorkloads(ctx, opts)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &workloadsv1.ListWorkloadsResponse{
		Workloads: make([]*workloadsv1.Workload, 0, len(ws)),
		Page:      &commonv1.PageResponse{NextPageToken: next},
	}
	for _, w := range ws {
		out.Workloads = append(out.Workloads, workloadToProto(w))
	}
	return connect.NewResponse(out), nil
}

func (h *SubmissionHandler) CancelWorkload(
	ctx context.Context,
	req *connect.Request[workloadsv1.CancelWorkloadRequest],
) (*connect.Response[workloadsv1.CancelWorkloadResponse], error) {
	id := uuidString(req.Msg.GetId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("id required"))
	}
	if err := h.Store.CancelWorkload(ctx, id, req.Msg.GetReason()); err != nil {
		return nil, mapStoreErr(err)
	}
	w, err := h.Store.GetWorkload(ctx, id)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return connect.NewResponse(&workloadsv1.CancelWorkloadResponse{Workload: workloadToProto(w)}), nil
}

// StreamWorkloadEvents replays every status transition for one workload.
// The first frame is synthesised from the workload's current status so
// late subscribers always get a baseline.
func (h *SubmissionHandler) StreamWorkloadEvents(
	ctx context.Context,
	req *connect.Request[workloadsv1.StreamWorkloadEventsRequest],
	stream *connect.ServerStream[workloadsv1.WorkloadEvent],
) error {
	id := uuidString(req.Msg.GetId())
	if id == "" {
		return connect.NewError(connect.CodeInvalidArgument, errors.New("id required"))
	}
	w, err := h.Store.GetWorkload(ctx, id)
	if err != nil {
		return mapStoreErr(err)
	}
	if err := stream.Send(&workloadsv1.WorkloadEvent{
		WorkloadId: uuidProto(id),
		NewStatus:  string(w.Status),
		OccurredAt: timestamppb.New(time.Now().UTC()),
		Note:       "baseline",
	}); err != nil {
		return err
	}

	sub, cancel := h.Store.SubscribeWorkloadEvents(id)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			return nil
		case e, ok := <-sub:
			if !ok {
				return nil
			}
			if err := stream.Send(&workloadsv1.WorkloadEvent{
				WorkloadId: uuidProto(e.WorkloadID),
				NewStatus:  e.NewStatus,
				OccurredAt: timestamppb.New(e.OccurredAt),
				Note:       e.Note,
			}); err != nil {
				return err
			}
		}
	}
}

// validateWorkload enforces the minimal "exactly one spec set" invariant
// + per-type sanity bounds. The full schema is enforced by the proto
// definitions; this is the last-line server-side check.
func validateWorkload(w *store.Workload) error {
	specs := 0
	if w.Bandwidth != nil {
		specs++
	}
	if w.Docker != nil {
		specs++
	}
	if w.GPU != nil {
		specs++
	}
	if w.IOSBuild != nil {
		specs++
	}
	if specs != 1 {
		return errors.New("exactly one of bandwidth|docker|gpu|ios_build payloads must be set")
	}
	switch w.Type {
	case store.TypeBandwidth:
		if w.Bandwidth == nil {
			return errors.New("workload.type=bandwidth requires bandwidth payload")
		}
		if w.Bandwidth.TargetURL == "" {
			return errors.New("bandwidth.target_url required")
		}
	case store.TypeDocker:
		if w.Docker == nil {
			return errors.New("workload.type=docker requires docker payload")
		}
		if w.Docker.Image == "" {
			return errors.New("docker.image required")
		}
	case store.TypeGPU:
		if w.GPU == nil {
			return errors.New("workload.type=gpu requires gpu payload")
		}
		if w.GPU.Image == "" {
			return errors.New("gpu.image required")
		}
	case store.TypeIOSBuild:
		if w.IOSBuild == nil {
			return errors.New("workload.type=ios_build requires ios_build payload")
		}
		if w.IOSBuild.SourceTarballS3Key == "" {
			return errors.New("ios_build.source_tarball_s3_key required")
		}
	default:
		return errors.New("unknown workload.type")
	}
	return nil
}

func mapStoreErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, store.ErrInvalidState):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}
