package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"connectrpc.com/connect"

	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1/workloadsv1connect"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/scheduler"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// DispatchHandler implements the WorkloadDispatchService Connect-Go
// interface. The bidi stream is the canonical production path: daemons
// open the stream, identify themselves with a DaemonHello, and the
// coordinator pushes WorkloadAssignment frames over the same channel.
type DispatchHandler struct {
	workloadsv1connect.UnimplementedWorkloadDispatchServiceHandler
	Store      store.Store
	Dispatcher *dispatcher.D
	Log        *slog.Logger
	// ProviderEndpointTemplate is the host:port the proxy-gateway dials
	// to forward customer bytes to *any* daemon connected via this
	// workloads-svc replica. In the Phase 0 NAT-bound layout every
	// daemon's traffic is tunnelled through this single workloads-svc
	// listener (the TCP-over-DispatchFrame forwarder); the per-daemon
	// stream is selected internally by attempt id.
	//
	// Wired from the WORKLOADS_SVC_PROVIDER_ENDPOINT env var by the
	// service's main.go. Empty == "no provider endpoint advertised";
	// proxy-gateway will then fall back to its DEV_PROVIDER_ENDPOINT
	// static pool.
	ProviderEndpointTemplate string
}

// NewDispatchHandler wires the deps.
func NewDispatchHandler(s store.Store, d *dispatcher.D, log *slog.Logger) *DispatchHandler {
	if log == nil {
		log = slog.Default()
	}
	return &DispatchHandler{Store: s, Dispatcher: d, Log: log}
}

// Dispatch is the long-lived bidi stream. The handler:
//
//  1. Waits for a DaemonHello, registers the daemon in the dispatcher.
//  2. Sends a CoordinatorHello ack.
//  3. Forwards every Assignment the dispatcher pushes onto the stream.
//  4. Persists every WorkloadStatusUpdate the daemon sends back.
//  5. On error / EOF / drain, unregisters the daemon cleanly.
func (h *DispatchHandler) Dispatch(
	ctx context.Context,
	stream *connect.BidiStream[workloadsv1.DispatchFrame, workloadsv1.DispatchFrame],
) error {
	// #271: surface the lifecycle of every bidi stream so we can tell
	// apart "daemon never sent a frame" (edge dropped the request body)
	// from "DaemonHello arrived but registration failed" (handler bug).
	// `peer` is best-effort — Connect-Go exposes Headers; the X-Forwarded-For
	// chain Traefik sets is what's actually identifying here.
	xff := stream.RequestHeader().Get("X-Forwarded-For")
	if xff == "" {
		xff = stream.RequestHeader().Get("X-Real-Ip")
	}
	h.Log.Info("dispatch stream opened",
		slog.String("remote_addr", xff),
		slog.String("user_agent", stream.RequestHeader().Get("User-Agent")),
	)
	defer h.Log.Info("dispatch stream closing",
		slog.String("remote_addr", xff),
	)

	// Reception loop runs in this goroutine; sending lives on the
	// `outbox` channel populated by the dispatcher Send hook.
	hello, err := stream.Receive()
	if err != nil {
		if errors.Is(err, io.EOF) {
			h.Log.Warn("dispatch stream: client EOF before DaemonHello — edge dropped the request body or client gave up",
				slog.String("remote_addr", xff),
			)
			return nil
		}
		h.Log.Warn("dispatch stream: recv error before DaemonHello",
			slog.String("remote_addr", xff),
			slog.String("error", err.Error()),
		)
		return err
	}
	dh := hello.GetDaemonHello()
	if dh == nil {
		h.Log.Warn("dispatch stream: first frame was not DaemonHello",
			slog.String("remote_addr", xff),
		)
		return connect.NewError(connect.CodeInvalidArgument, errors.New("first frame must be daemon_hello"))
	}
	providerID := uuidString(dh.GetProviderId())
	if providerID == "" {
		h.Log.Warn("dispatch stream: DaemonHello.provider_id was empty",
			slog.String("remote_addr", xff),
		)
		return connect.NewError(connect.CodeInvalidArgument, errors.New("daemon_hello.provider_id required"))
	}
	h.Log.Info("daemon hello received",
		slog.String("provider_id", providerID),
		slog.String("remote_addr", xff),
		slog.Int("eligible_types", len(dh.GetEligibleTypes())),
		slog.Int("max_concurrent", int(dh.GetMaxConcurrent())),
	)

	// Send back a coordinator-hello so the daemon knows we adopted it.
	if err := stream.Send(&workloadsv1.DispatchFrame{
		Frame: &workloadsv1.DispatchFrame_CoordinatorHello{
			CoordinatorHello: &workloadsv1.CoordinatorHello{
				ProviderId: uuidProto(providerID),
				AcceptedAt: timestamppb.New(time.Now().UTC()),
			},
		},
	}); err != nil {
		return err
	}

	// Register with the dispatcher. The Send hook converts the
	// dispatcher's internal Assignment struct into the wire frame.
	var sendMu sync.Mutex
	sendFrame := func(f *workloadsv1.DispatchFrame) error {
		sendMu.Lock()
		defer sendMu.Unlock()
		return stream.Send(f)
	}
	conn := &dispatcher.Connection{
		ProviderID:   providerID,
		EndpointHint: h.ProviderEndpointTemplate,
		Snapshot: scheduler.ProviderSnapshot{
			ID:             providerID,
			Status:         "active",
			State:          "SCHEDULER_STATE_ACTIVE",
			SupportedTypes: capabilityTypesFromHello(dh),
		},
		Send: func(a *dispatcher.Assignment) error {
			w, err := h.Store.GetWorkload(ctx, a.WorkloadID)
			if err != nil {
				return err
			}
			return sendFrame(&workloadsv1.DispatchFrame{
				Frame: &workloadsv1.DispatchFrame_Assignment{
					Assignment: &workloadsv1.WorkloadAssignment{
						Workload:  workloadToProto(w),
						AttemptId: uuidProto(a.ID),
						Deadline:  timestamppb.New(a.Deadline),
					},
				},
			})
		},
		SendTunnelOpen: func(attemptID, targetHostPort string) error {
			return sendFrame(&workloadsv1.DispatchFrame{
				Frame: &workloadsv1.DispatchFrame_TunnelOpen{
					TunnelOpen: &workloadsv1.TunnelOpen{
						AttemptId:      uuidProto(attemptID),
						TargetHostPort: targetHostPort,
					},
				},
			})
		},
		SendTunnelData: func(attemptID string, payload []byte) error {
			return sendFrame(&workloadsv1.DispatchFrame{
				Frame: &workloadsv1.DispatchFrame_TunnelData{
					TunnelData: &workloadsv1.TunnelData{
						AttemptId: uuidProto(attemptID),
						Payload:   payload,
					},
				},
			})
		},
		SendTunnelClose: func(attemptID, reason string) error {
			return sendFrame(&workloadsv1.DispatchFrame{
				Frame: &workloadsv1.DispatchFrame_TunnelClose{
					TunnelClose: &workloadsv1.TunnelClose{
						AttemptId: uuidProto(attemptID),
						Error:     reason,
					},
				},
			})
		},
	}
	h.Dispatcher.Register(conn)
	defer h.Dispatcher.Unregister(providerID)

	// Drain frames from the daemon until disconnect.
	for {
		f, err := stream.Receive()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		switch {
		case f.GetUpdate() != nil:
			u := f.GetUpdate()
			wid := uuidString(u.GetWorkloadId())
			if wid == "" {
				continue
			}
			s := statusFromProto(u.GetStatus())
			_ = h.Store.UpdateWorkloadStatus(ctx, wid, s, u.GetNote())
			if isTerminal(s) {
				_ = h.Store.SetWorkloadResult(ctx, wid, &store.Result{
					TerminalStatus: string(s),
					ExitCode:       u.GetExitCode(),
					LogsS3Key:      u.GetLogsS3Key(),
					BytesIn:        u.GetBytesIn(),
					BytesOut:       u.GetBytesOut(),
					ArtifactS3Keys: append([]string(nil), u.GetArtifactS3Keys()...),
					CompletedAt:    time.Now().UTC(),
				})
			}
		case f.GetPing() != nil:
			// daemon liveness — no-op (otelhttp records the rtt).
		case f.GetTunnelData() != nil:
			td := f.GetTunnelData()
			aid := uuidString(td.GetAttemptId())
			if aid == "" {
				continue
			}
			if !h.Dispatcher.DeliverTunnelData(aid, td.GetPayload()) {
				// No live forwarder bound — the proxy-gateway side
				// already closed. Drop the bytes (the daemon will
				// see TunnelClose on its next read attempt).
				h.Log.Debug("tunnel_data for unknown attempt",
					slog.String("attempt_id", aid))
			}
		case f.GetTunnelClose() != nil:
			tc := f.GetTunnelClose()
			aid := uuidString(tc.GetAttemptId())
			if aid == "" {
				continue
			}
			h.Dispatcher.DeliverTunnelClose(aid, tc.GetError())
		case f.GetDrain():
			h.Log.Info("daemon requested drain",
				slog.String("provider_id", providerID))
			return nil
		}
	}
}

// AckAssignment is the side-band path for replay / debug tooling — the
// production path is the bidi Update frame above.
func (h *DispatchHandler) AckAssignment(
	ctx context.Context,
	req *connect.Request[workloadsv1.AckAssignmentRequest],
) (*connect.Response[workloadsv1.AckAssignmentResponse], error) {
	id := uuidString(req.Msg.GetAttemptId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("attempt_id required"))
	}
	a, err := h.Store.GetAssignment(ctx, id)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	a.Accepted = req.Msg.GetAccepted()
	if !a.Accepted {
		a.LatestStatus = store.StatusRejected
		a.RejectionReason = req.Msg.GetRejectionReason()
	} else {
		a.LatestStatus = store.StatusRunning
	}
	if err := h.Store.UpdateAssignment(ctx, a); err != nil {
		return nil, mapStoreErr(err)
	}
	return connect.NewResponse(&workloadsv1.AckAssignmentResponse{}), nil
}

func (h *DispatchHandler) GetAssignment(
	ctx context.Context,
	req *connect.Request[workloadsv1.GetAssignmentRequest],
) (*connect.Response[workloadsv1.GetAssignmentResponse], error) {
	id := uuidString(req.Msg.GetAttemptId())
	if id == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("attempt_id required"))
	}
	a, err := h.Store.GetAssignment(ctx, id)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	w, err := h.Store.GetWorkload(ctx, a.WorkloadID)
	if err != nil {
		return nil, mapStoreErr(err)
	}
	return connect.NewResponse(&workloadsv1.GetAssignmentResponse{
		Assignment: &workloadsv1.WorkloadAssignment{
			Workload:  workloadToProto(w),
			AttemptId: uuidProto(a.ID),
			Deadline:  timestamppb.New(a.Deadline),
		},
		LatestStatus: statusToProto(a.LatestStatus),
	}), nil
}

// capabilityTypesFromHello extracts the slug list the dispatcher needs
// from the DaemonHello frame.
func capabilityTypesFromHello(dh *workloadsv1.DaemonHello) []string {
	out := make([]string, 0, len(dh.GetEligibleTypes()))
	for _, t := range dh.GetEligibleTypes() {
		if s := workloadTypeSlug(t); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func isTerminal(s store.Status) bool {
	switch s {
	case store.StatusSucceeded, store.StatusFailed, store.StatusTimedOut, store.StatusCancelled, store.StatusRejected:
		return true
	default:
		return false
	}
}
