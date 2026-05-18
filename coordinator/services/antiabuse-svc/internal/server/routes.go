// Package server holds the HTTP route definitions for the antiabuse-svc microservice.
//
// Pre-flight filtering (CSAM, fraud, port restrictions, rate limits), abuse detection.
//
// At this stage the routes are stubs that document the intended surface area
// without making any external calls. They return JSON envelopes shaped the
// same way the final implementation will, so downstream callers can be
// scaffolded in parallel.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Mount attaches the antiabuse-svc routes onto the shared chi router. Called by main()
// after /healthz, /readyz, /metrics are already wired up by the shared
// bootstrap.
func Mount(r chi.Router) {
	r.Route("/v1", func(r chi.Router) {
		r.Get("/", indexHandler)
	})
}

// indexHandler returns a stable JSON envelope identifying the service. Used
// by smoke tests and the gateway-bff service discovery probe.
func indexHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"service": "antiabuse-svc",
		"status":  "stub",
	})
}
