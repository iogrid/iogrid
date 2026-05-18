package handlers

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

func newTestSubmission() (*SubmissionHandler, store.Store) {
	s := store.NewInMemory()
	d := dispatcher.New(s, nil)
	return NewSubmissionHandler(s, d, nil), s
}

func TestSubmitWorkload_Bandwidth_QueuedWhenNoDaemons(t *testing.T) {
	h, _ := newTestSubmission()
	resp, err := h.SubmitWorkload(context.Background(), connect.NewRequest(&workloadsv1.SubmitWorkloadRequest{
		Workload: &workloadsv1.Workload{
			WorkspaceId: &commonv1.UUID{Value: "ws-1"},
			Type:        commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH,
			Payload: &workloadsv1.Workload_Bandwidth{Bandwidth: &workloadsv1.BandwidthRequest{
				TargetUrl: "https://example.com/feed",
				Category:  "e_commerce",
			}},
		},
	}))
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if resp.Msg.Workload.GetId().GetValue() == "" {
		t.Fatalf("expected id")
	}
	// No daemons connected ⇒ dispatcher rejects ⇒ status REJECTED.
	if got := resp.Msg.Workload.GetStatus(); got != "rejected" {
		t.Fatalf("expected rejected (no daemons), got %q", got)
	}
}

func TestSubmitWorkload_MissingPayload(t *testing.T) {
	h, _ := newTestSubmission()
	_, err := h.SubmitWorkload(context.Background(), connect.NewRequest(&workloadsv1.SubmitWorkloadRequest{
		Workload: &workloadsv1.Workload{
			Type: commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH,
		},
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
	if ce, ok := err.(*connect.Error); !ok || ce.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument, got %v", err)
	}
}

func TestSubmitWorkload_TypeMismatch(t *testing.T) {
	h, _ := newTestSubmission()
	_, err := h.SubmitWorkload(context.Background(), connect.NewRequest(&workloadsv1.SubmitWorkloadRequest{
		Workload: &workloadsv1.Workload{
			Type: commonv1.WorkloadType_WORKLOAD_TYPE_DOCKER,
			Payload: &workloadsv1.Workload_Bandwidth{Bandwidth: &workloadsv1.BandwidthRequest{
				TargetUrl: "https://example.com",
			}},
		},
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestGetWorkload_NotFound(t *testing.T) {
	h, _ := newTestSubmission()
	_, err := h.GetWorkload(context.Background(), connect.NewRequest(&workloadsv1.GetWorkloadRequest{
		Id: &commonv1.UUID{Value: "ghost"},
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
	if ce, ok := err.(*connect.Error); !ok || ce.Code() != connect.CodeNotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
}

func TestListWorkloads_FilterByWorkspace(t *testing.T) {
	h, s := newTestSubmission()
	ctx := context.Background()
	for _, ws := range []string{"a", "a", "b"} {
		_ = s.CreateWorkload(ctx, &store.Workload{
			WorkspaceID: ws,
			Type:        store.TypeBandwidth,
			Bandwidth:   &store.BandwidthSpec{TargetURL: "x"},
		})
	}
	resp, err := h.ListWorkloads(ctx, connect.NewRequest(&workloadsv1.ListWorkloadsRequest{
		WorkspaceId: &commonv1.UUID{Value: "a"},
	}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Msg.Workloads) != 2 {
		t.Fatalf("expected 2, got %d", len(resp.Msg.Workloads))
	}
}

func TestCancelWorkload(t *testing.T) {
	h, s := newTestSubmission()
	ctx := context.Background()
	w := &store.Workload{
		WorkspaceID: "w",
		Type:        store.TypeBandwidth,
		Bandwidth:   &store.BandwidthSpec{TargetURL: "x"},
	}
	_ = s.CreateWorkload(ctx, w)
	resp, err := h.CancelWorkload(ctx, connect.NewRequest(&workloadsv1.CancelWorkloadRequest{
		Id:     &commonv1.UUID{Value: w.ID},
		Reason: "user clicked cancel",
	}))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if got := resp.Msg.Workload.GetStatus(); got != "cancelled" {
		t.Fatalf("expected cancelled, got %q", got)
	}
}
