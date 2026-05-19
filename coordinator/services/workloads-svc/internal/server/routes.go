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
}

// Mount attaches the workloads-svc routes onto the shared chi router. Called by main()
// after /healthz, /readyz, /metrics are already wired up by the shared
// bootstrap.
func Mount(deps Deps) func(chi.Router) {
	return func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			r.Get("/", indexHandler)
		})

		sub := handlers.NewSubmissionHandler(deps.Store, deps.Dispatcher, deps.Log)
		disp := handlers.NewDispatchHandler(deps.Store, deps.Dispatcher, deps.Log)
		disp.ProviderEndpointTemplate = deps.ProviderEndpointTemplate

		for _, mount := range []func() (string, http.Handler){
			func() (string, http.Handler) { return workloadsv1connect.NewWorkloadSubmissionServiceHandler(sub) },
			func() (string, http.Handler) { return workloadsv1connect.NewWorkloadDispatchServiceHandler(disp) },
		} {
			path, h := mount()
			r.Mount(path, h)
		}
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
