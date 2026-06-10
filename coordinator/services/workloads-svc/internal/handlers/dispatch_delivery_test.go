package handlers

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1/workloadsv1connect"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

// h2cClient returns an HTTP client that speaks cleartext HTTP/2 (h2c) —
// the same scheme the dispatch IngressRoute uses (scheme: h2c) and the
// daemon's tonic channel negotiates. Connect's bidi streaming requires
// HTTP/2; this lets the test exercise the real server-push path.
func h2cClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(_ context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				return net.Dial(network, addr)
			},
		},
	}
}

// TestDispatch_ServerPushesAssignmentToConnectedDaemon is the in-cluster
// half of the #705 bisect: it stands up the REAL connect-go
// WorkloadDispatchService handler on an h2c httptest server (NO Traefik
// edge), connects a client bidi stream as a daemon advertising IOS_BUILD,
// then submits an IOS_BUILD workload from a different goroutine and
// asserts the client receives the WorkloadAssignment frame.
//
// This is the exact server→client push that fails for a REMOTE daemon
// through the mothership edge (#705): CoordinatorHello (sent inline at
// stream open) arrives, but the later Assignment (pushed from the
// dispatcher goroutine via conn.Send → stream.Send) does not. If THIS
// test passes, the connect-go server delivers cross-goroutine pushes
// correctly and the bug is the edge; if it fails, the bug is in
// workloads-svc and this test pins the fix.
func TestDispatch_ServerPushesAssignmentToConnectedDaemon(t *testing.T) {
	st := store.NewInMemory()
	d := dispatcher.New(st, nil)
	dh := NewDispatchHandler(st, d, nil) // nil Log → slog.Default()
	sub := NewSubmissionHandler(st, d, nil)

	mux := http.NewServeMux()
	path, handler := workloadsv1connect.NewWorkloadDispatchServiceHandler(dh)
	mux.Handle(path, handler)
	// Serve h2c (cleartext HTTP/2) so the bidi stream's server-push works —
	// matches the dispatch IngressRoute's scheme:h2c.
	srv := httptest.NewServer(h2c.NewHandler(mux, &http2.Server{}))
	defer srv.Close()

	providerID := "c0138910-9f41-4a05-972f-c6915760e0f0"
	client := workloadsv1connect.NewWorkloadDispatchServiceClient(h2cClient(), srv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	stream := client.Dispatch(ctx)
	// 1. Send DaemonHello advertising IOS_BUILD (a macOS build provider).
	if err := stream.Send(&workloadsv1.DispatchFrame{
		Frame: &workloadsv1.DispatchFrame_DaemonHello{DaemonHello: &workloadsv1.DaemonHello{
			ProviderId: &commonv1.UUID{Value: providerID},
			EligibleTypes: []commonv1.WorkloadType{
				commonv1.WorkloadType_WORKLOAD_TYPE_BANDWIDTH,
				commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD,
			},
			MaxConcurrent: 4,
		}},
	}); err != nil {
		t.Fatalf("send DaemonHello: %v", err)
	}

	// 2. First server→client frame is CoordinatorHello (sent inline at
	//    stream open) — this always arrives, even through the edge.
	first, err := stream.Receive()
	if err != nil {
		t.Fatalf("receive CoordinatorHello: %v", err)
	}
	if first.GetCoordinatorHello() == nil {
		t.Fatalf("first frame = %T, want CoordinatorHello", first.GetFrame())
	}

	// 3. Submit an IOS_BUILD workload from a separate goroutine. The
	//    submission handler's TryAssign places it on our connected daemon
	//    and pushes the Assignment via conn.Send → stream.Send — the
	//    cross-goroutine server push under test. Retry briefly: Register()
	//    runs just after CoordinatorHello is sent, so the connection may
	//    not be in the dispatcher's map for the first submit.
	submitErr := make(chan error, 1)
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for {
			resp, err := sub.SubmitWorkload(context.Background(), connect.NewRequest(&workloadsv1.SubmitWorkloadRequest{
				Workload: &workloadsv1.Workload{
					WorkspaceId: &commonv1.UUID{Value: "11111111-1111-1111-1111-111111111111"},
					Type:        commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD,
					Payload: &workloadsv1.Workload_IosBuild{IosBuild: &workloadsv1.IosBuildRequest{
						RepoUrl:      "https://github.com/iogrid/iogrid.git",
						GitRef:       "main",
						BuildCommand: "echo build",
					}},
				},
			}))
			if err != nil {
				submitErr <- err
				return
			}
			if resp.Msg.GetWorkload().GetStatus() == "dispatched" || time.Now().After(deadline) {
				submitErr <- nil
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	// 4. Receive the next server→client frame and assert it's the
	//    Assignment. This is what does NOT arrive through the edge (#705).
	type recvResult struct {
		f   *workloadsv1.DispatchFrame
		err error
	}
	recvCh := make(chan recvResult, 1)
	go func() {
		f, err := stream.Receive()
		recvCh <- recvResult{f, err}
	}()

	select {
	case r := <-recvCh:
		if r.err != nil {
			t.Fatalf("receive Assignment: %v", r.err)
		}
		asn := r.f.GetAssignment()
		if asn == nil {
			t.Fatalf("pushed frame = %T, want WorkloadAssignment", r.f.GetFrame())
		}
		if asn.GetWorkload().GetType() != commonv1.WorkloadType_WORKLOAD_TYPE_IOS_BUILD {
			t.Errorf("assignment workload type = %v, want IOS_BUILD", asn.GetWorkload().GetType())
		}
		if asn.GetAttemptId().GetValue() == "" {
			t.Error("assignment missing attempt_id")
		}
	case <-time.After(8 * time.Second):
		if e := <-submitErr; e != nil {
			t.Fatalf("submit errored before push: %v", e)
		}
		t.Fatal("TIMED OUT waiting for the Assignment server-push — the connect-go " +
			"server did NOT deliver a cross-goroutine push (this would mean #705 is " +
			"an app-layer bug, not the edge)")
	}
}
