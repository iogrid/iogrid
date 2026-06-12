package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

// recordingForwarder is a fake handlers.BuildGatewayForwarder that captures
// every ForwardStatus call so a test can assert the poll path propagates.
type recordingForwarder struct {
	calls []fwdCall
}

type fwdCall struct {
	buildID  string
	status   string
	note     string
	exitCode int32
}

func (f *recordingForwarder) ForwardStatus(_ context.Context, buildID, status, note string, exitCode int32) error {
	f.calls = append(f.calls, fwdCall{buildID, status, note, exitCode})
	return nil
}

// #740: the poll-dispatch status callback must forward to the build-gateway
// (routing key = the workload's "build_id" label). Without it a build that ran
// to exit 0 via poll stayed "dispatched" in the gateway forever, so metering +
// settle never fired. Mirrors the dispatch-stream forward (dispatch.go).
func TestAssignedWorkloadStatusForwardsToBuildGateway(t *testing.T) {
	st := store.NewInMemory()
	ctx := context.Background()
	const prov = "33333333-3333-3333-3333-333333333333"

	wl := &store.Workload{
		ID:     "wl-fwd",
		Type:   store.TypeIOSBuild,
		Status: store.StatusDispatched,
		Labels: map[string]string{"build_id": "bld-1"},
		IOSBuild: &store.IOSBuildSpec{
			RepoURL:      "https://github.com/iogrid/iogrid.git",
			GitRef:       "main",
			BuildCommand: "xcodebuild build",
		},
	}
	if err := st.CreateWorkload(ctx, wl); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAssignment(ctx, &store.Assignment{
		ID: "att-fwd", WorkloadID: "wl-fwd", ProviderID: prov, LatestStatus: store.StatusDispatched,
	}); err != nil {
		t.Fatal(err)
	}

	fwd := &recordingForwarder{}
	r := chi.NewRouter()
	r.Group(Mount(Deps{Store: st, Dispatcher: dispatcher.New(st, nil), BuildGateway: fwd, Log: slog.Default()}))
	srv := httptest.NewServer(r)
	defer srv.Close()

	body := `{"status":"succeeded","exit_code":0,"note":"done"}`
	resp, err := http.Post(srv.URL+"/v1/providers/"+prov+"/assigned-workloads/att-fwd/status",
		"application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status POST: want 200, got %d", resp.StatusCode)
	}

	if len(fwd.calls) != 1 {
		t.Fatalf("want exactly 1 forward to build-gateway, got %d", len(fwd.calls))
	}
	if c := fwd.calls[0]; c.buildID != "bld-1" || c.status != "succeeded" || c.exitCode != 0 {
		t.Fatalf("unexpected forward: %+v", c)
	}
}

// #705: the poll endpoint serves a provider's dispatched-but-not-running
// iOS-build assignments so a remote daemon (whose server→client push half
// the edge drops) can pick them up by polling.
func TestAssignedWorkloadsPoll(t *testing.T) {
	st := store.NewInMemory()
	ctx := context.Background()
	const provA = "11111111-1111-1111-1111-111111111111"
	const provB = "22222222-2222-2222-2222-222222222222"

	// An iOS build dispatched to provider A.
	wl := &store.Workload{
		ID:     "wl-1",
		Type:   store.TypeIOSBuild,
		Status: store.StatusDispatched,
		IOSBuild: &store.IOSBuildSpec{
			RepoURL:      "https://github.com/iogrid/iogrid.git",
			GitRef:       "main",
			BuildCommand: "xcodebuild build",
		},
	}
	if err := st.CreateWorkload(ctx, wl); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateAssignment(ctx, &store.Assignment{
		ID: "att-1", WorkloadID: "wl-1", ProviderID: provA, LatestStatus: store.StatusDispatched,
	}); err != nil {
		t.Fatal(err)
	}
	// An assignment already RUNNING (must NOT be served).
	if err := st.CreateAssignment(ctx, &store.Assignment{
		ID: "att-2", WorkloadID: "wl-1", ProviderID: provA, LatestStatus: store.StatusRunning,
	}); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	r.Group(Mount(Deps{Store: st, Dispatcher: dispatcher.New(st, nil)}))
	srv := httptest.NewServer(r)
	defer srv.Close()

	t.Run("provider A gets its pending iOS build", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/v1/providers/" + provA + "/assigned-workloads")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}
		var out struct {
			Count       int `json:"count"`
			Assignments []struct {
				AttemptID    string `json:"attempt_id"`
				RepoURL      string `json:"repo_url"`
				BuildCommand string `json:"build_command"`
			} `json:"assignments"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if out.Count != 1 {
			t.Fatalf("count = %d, want 1 (only the dispatched one; the RUNNING one is excluded)", out.Count)
		}
		if out.Assignments[0].AttemptID != "att-1" || out.Assignments[0].BuildCommand != "xcodebuild build" {
			t.Fatalf("unexpected assignment payload: %+v", out.Assignments[0])
		}
	})

	t.Run("status report drains the assignment from the poll list", func(t *testing.T) {
		// report terminal status for att-1 → it should leave the poll list.
		body := `{"status":"succeeded","exit_code":0}`
		resp, err := http.Post(srv.URL+"/v1/providers/"+provA+"/assigned-workloads/att-1/status",
			"application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status POST: want 200, got %d", resp.StatusCode)
		}
		// poll again → att-1 no longer dispatched.
		r2, err := http.Get(srv.URL + "/v1/providers/" + provA + "/assigned-workloads")
		if err != nil {
			t.Fatal(err)
		}
		defer r2.Body.Close()
		var out struct {
			Count int `json:"count"`
		}
		_ = json.NewDecoder(r2.Body).Decode(&out)
		if out.Count != 0 {
			t.Fatalf("after status drain, count = %d, want 0", out.Count)
		}
	})

	t.Run("provider B gets nothing", func(t *testing.T) {
		resp, err := http.Get(srv.URL + "/v1/providers/" + provB + "/assigned-workloads")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out struct {
			Count int `json:"count"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		if out.Count != 0 {
			t.Fatalf("provider B count = %d, want 0", out.Count)
		}
	})
}
