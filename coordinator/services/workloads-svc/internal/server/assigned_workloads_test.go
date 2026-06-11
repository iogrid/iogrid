package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

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
