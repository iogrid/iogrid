// Package server holds the HTTP route definitions for the workloads-svc microservice.
//
// Customer workload submission, scheduling, dispatch, retry/failover, result delivery.
//
// The Connect-Go handlers from the two pb services (WorkloadSubmission,
// WorkloadDispatch) are mounted under their canonical
// `/iogrid.workloads.v1.<svc>/` paths. The `/v1/` JSON envelope kept from
// the scaffolding stays in place for the gateway-bff service-discovery
// probe.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1/workloadsv1connect"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/handlers"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

// Deps bundles the injected dependencies so tests can swap them out.
type Deps struct {
	Store      store.Store
	Dispatcher *dispatcher.D
	Log        *slog.Logger
	// ProviderEndpointTemplate is the host:port advertised to the
	// proxy-gateway as the dial target for any connected daemon's
	// traffic — wired through to DispatchHandler.ProviderEndpointTemplate.
	// Empty == feature off (proxy-gateway uses its dev pool).
	ProviderEndpointTemplate string
	// BuildGateway forwards iOS-build status updates to the build-gateway's
	// internal callback API. nil == not configured.
	BuildGateway handlers.BuildGatewayForwarder
}

// Mount attaches the workloads-svc routes onto the shared chi router. Called by main()
// after /healthz, /readyz, /metrics are already wired up by the shared
// bootstrap.
func Mount(deps Deps) func(chi.Router) {
	return func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			r.Get("/", indexHandler)
			// #705: poll-based dispatch. The bidi-stream Assignment push is
			// dropped by the mothership edge for a REMOTE daemon (only the
			// first server→client frame traverses). A daemon can instead
			// POLL this endpoint (client→server, which traverses fine —
			// the VPN binder works the same way) to pick up its assigned
			// iOS builds.
			r.Get("/providers/{providerID}/assigned-workloads", assignedWorkloadsHandler(deps))
		})

		sub := handlers.NewSubmissionHandler(deps.Store, deps.Dispatcher, deps.Log)
		disp := handlers.NewDispatchHandler(deps.Store, deps.Dispatcher, deps.Log)
		disp.ProviderEndpointTemplate = deps.ProviderEndpointTemplate
		disp.BuildGateway = deps.BuildGateway

		for _, mount := range []func() (string, http.Handler){
			func() (string, http.Handler) { return workloadsv1connect.NewWorkloadSubmissionServiceHandler(sub) },
			func() (string, http.Handler) { return workloadsv1connect.NewWorkloadDispatchServiceHandler(disp) },
		} {
			path, h := mount()
			r.Mount(path, h)
		}
	}
}

// assignedWorkloadsHandler serves a provider's dispatched-but-not-yet-
// running iOS-build assignments (#705 poll-based delivery). The daemon
// polls this, runs each build, and reports RUNNING via the existing
// dispatch status path — which moves the assignment off "dispatched" so
// it stops being served here.
func assignedWorkloadsHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerID := chi.URLParam(r, "providerID")
		if providerID == "" {
			http.Error(w, "providerID required", http.StatusBadRequest)
			return
		}
		assigns, err := deps.Store.ListPendingAssignments(r.Context(), providerID)
		if err != nil {
			http.Error(w, "list assignments failed", http.StatusInternalServerError)
			return
		}
		out := make([]map[string]any, 0, len(assigns))
		for _, a := range assigns {
			wl, err := deps.Store.GetWorkload(r.Context(), a.WorkloadID)
			if err != nil || wl == nil || wl.IOSBuild == nil {
				continue // only iOS builds are pollable; skip others/missing
			}
			b := wl.IOSBuild
			out = append(out, map[string]any{
				"attempt_id":          a.ID,
				"workload_id":         wl.ID,
				"deadline":            a.Deadline,
				"repo_url":            b.RepoURL,
				"git_ref":             b.GitRef,
				"build_command":       b.BuildCommand,
				"tart_image":          b.TartImage,
				"upload_url":          b.UploadURL,
				"artifact_guest_path": b.ArtifactGuestPath,
				"artifact_bucket":     b.ArtifactBucket,
				"artifact_prefix":     b.ArtifactPrefix,
				"cpu":                 b.CPU,
			})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"provider_id": providerID,
			"count":       len(out),
			"assignments": out,
		})
	}
}

// indexHandler returns a stable JSON envelope identifying the service. Used
// by smoke tests and the gateway-bff service discovery probe.
func indexHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"service": "workloads-svc",
		"status":  "ok",
	})
}
