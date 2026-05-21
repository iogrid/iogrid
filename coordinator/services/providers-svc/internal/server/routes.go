// Package server holds the HTTP route definitions for the providers-svc microservice.
//
// Provider registration, capability inventory, scheduling state, transparency dashboard backend.
//
// The Connect-Go handlers from the three pb services (ProviderRegistration,
// Scheduling, Dashboard) are mounted under their canonical
// `/iogrid.providers.v1.<svc>/` paths. The `/v1/` JSON envelope kept from
// the scaffolding stays in place for the gateway-bff service-discovery
// probe.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1/providersv1connect"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/ca"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/geoip"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/handlers"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

// Deps bundles the injected dependencies so tests can swap them out.
type Deps struct {
	Store store.Store
	CA    *ca.CA
	// GeoIP resolves observed source IPs into country/region for the
	// providers row. main wires either the .mmdb-backed reader or a
	// NoopLookuper; tests inject geoip.StubLookuper. May be nil — the
	// handler constructors substitute NoopLookuper in that case.
	GeoIP geoip.Lookuper
	Log   *slog.Logger
}

// Mount attaches the providers-svc routes onto the shared chi router. Called by main()
// after /healthz, /readyz, /metrics are already wired up by the shared
// bootstrap.
func Mount(deps Deps) func(chi.Router) {
	return func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			r.Get("/", indexHandler)
		})

		reg := handlers.NewRegistrationHandler(deps.Store, deps.CA, deps.GeoIP, deps.Log)
		sched := handlers.NewSchedulingHandler(deps.Store, deps.GeoIP, deps.Log)
		dash := handlers.NewDashboardHandler(deps.Store, deps.Log)

		// REST shim — the Rust daemon's iogrid-transport identity flow
		// POSTs to /api/v1/providers/pair with the lean
		// PairingRequest JSON shape. The shim translates that to the
		// canonical Connect PairDaemon RPC in-process so we keep a
		// single source of truth (the RegistrationHandler) for both
		// surfaces. See handlers/rest_pair.go for the wire shapes.
		r.Route("/api/v1/providers", func(r chi.Router) {
			r.Post("/pair", reg.PairDaemonREST)
		})

		// Connect-Go handlers return (path, http.Handler) — the path is the
		// "/iogrid.providers.v1.<Service>/" prefix.
		for _, mount := range []func() (string, http.Handler){
			func() (string, http.Handler) { return providersv1connect.NewProviderRegistrationServiceHandler(reg) },
			func() (string, http.Handler) { return providersv1connect.NewSchedulingServiceHandler(sched) },
			func() (string, http.Handler) { return providersv1connect.NewDashboardServiceHandler(dash) },
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
		"service": "providers-svc",
		"status":  "ok",
	})
}
