// Package server holds the HTTP route definitions for the telemetry-svc microservice.
//
// telemetry-svc itself is the control surface — the otelcol gateway runs
// in a separate container in the same Pod (see infra/k8s/base/telemetry-svc).
// This service:
//
//   - Renders the otelcol gateway config from env + template
//   - Loads + validates the SLO catalogue from ./slo
//   - Serves /status (public status page feed) + /v1/slos (admin listing)
//   - Exposes /admin/reload to regenerate the collector config on the
//     fly (e.g. after a ConfigMap update lands but the otelcol watcher
//     hasn't fired)
package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/collector"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/incidents"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/slo"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/status"
)

// Deps carries the long-lived objects the handlers need. main() builds
// one of these at boot and passes it to MountFunc.
type Deps struct {
	Cfg   config.Config
	Cat   *slo.Catalogue
	Store incidents.Store
}

// MountFunc returns a chi.Mount-compatible callback closed over Deps.
// Kept separate from a top-level Mount() so main() can wire dependencies
// without globals.
//
// A nil Deps.Store is replaced with an in-memory fallback so tests and
// the local-dev binary keep working without a Postgres backend wired up.
func MountFunc(d Deps) func(chi.Router) {
	if d.Store == nil {
		d.Store = incidents.NewInMemory()
	}
	return func(r chi.Router) {
		// Public status page feed (legacy, kept for the existing
		// status.iogrid.org JSON contract). Mounted at /status as well
		// as nested under the /status/* tree for completeness.
		r.Get("/status", status.Handler(d.Cat, d.Cfg))

		// New /status/* tree powering the redesigned page.
		r.Route("/status/", func(r chi.Router) {
			// Public reads.
			r.Get("/posture", status.PostureHandler(d.Cat, d.Store, d.Cfg))
			r.Get("/uptime", status.UptimeHandler(d.Store))
			// Subscribe is publicly POSTable (with per-IP rate-limit).
			r.Post("/subscribe", status.SubscribeHandler(d.Store))

			// Admin mutations.
			r.Group(func(r chi.Router) {
				r.Use(status.AdminAuth(d.Cfg.AdminToken))
				r.Post("/incidents", status.CreateIncidentHandler(d.Store))
				r.Post("/incidents/{id}/updates", status.AppendIncidentUpdateHandler(d.Store))
			})
		})

		// Service-discovery probe used by gateway-bff.
		r.Route("/v1", func(r chi.Router) {
			r.Get("/", indexHandler)
			r.Get("/slos", slosHandler(d.Cat))
		})

		// Admin (cluster-internal only — NetworkPolicy gates this).
		r.Route("/admin", func(r chi.Router) {
			r.Post("/reload", reloadHandler(d.Cfg))
			r.Get("/collector-config", collectorConfigHandler(d.Cfg))
		})
	}
}

// indexHandler returns a stable JSON envelope identifying the service.
func indexHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"service": "telemetry-svc",
		"status":  "ready",
	})
}

// slosHandler returns the parsed SLO catalogue as JSON for inspection.
// Admin-only UI in the management plane consumes this.
func slosHandler(cat *slo.Catalogue) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if cat == nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"slos": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"slos": cat.SLOs})
	}
}

// reloadHandler regenerates the collector config and writes it to disk.
// The otelcol container picks up the file change and hot-reloads its
// pipelines. Returns 204 on success; 5xx on render error.
func reloadHandler(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		if err := collector.WriteAtomic(cfg); err != nil {
			http.Error(w, fmt.Sprintf("collector reload: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// collectorConfigHandler returns the rendered collector YAML so an
// operator can inspect the live config without exec-ing into the Pod.
func collectorConfigHandler(cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		out, err := collector.Build(cfg)
		if err != nil {
			http.Error(w, fmt.Sprintf("render: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write(out)
	}
}

// Mount keeps the legacy stub signature so the shared server's Options
// continues to compile with the existing main.go boot sequence. Real
// wiring goes through MountFunc(Deps).
//
// Deprecated: prefer MountFunc. Removed once main.go is migrated.
func Mount(r chi.Router) {
	r.Route("/v1", func(r chi.Router) {
		r.Get("/", indexHandler)
	})
}
