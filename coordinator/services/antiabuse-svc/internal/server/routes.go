// Package server holds the HTTP route definitions for the antiabuse-svc microservice.
//
// Pre-flight filtering (CSAM, fraud, port restrictions, rate limits), abuse detection.
//
// The Connect-RPC AbuseFilterService is mounted under its canonical
// "/iogrid.antiabuse.v1.AbuseFilterService/" path; the legacy /v1
// JSON index handler is retained for service-discovery probes.
package server

import (
	"encoding/json"
	"net/http"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1/antiabusev1connect"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/handler"
)

// Mount attaches the antiabuse-svc routes onto the shared chi router.
// Pass a fully-wired *handler.Service; nil skips RPC routes (legacy
// /v1 stub still mounted so smoke tests keep working).
func Mount(r chi.Router, svc *handler.Service, opts ...connect.HandlerOption) {
	r.Route("/v1", func(r chi.Router) {
		r.Get("/", indexHandler)
	})
	if svc == nil {
		return
	}
	path, h := antiabusev1connect.NewAbuseFilterServiceHandler(svc, opts...)
	r.Mount(path, h)
}

// indexHandler returns a stable JSON envelope identifying the service.
func indexHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"service": "antiabuse-svc",
		"status":  "ready",
		"rpc":     "iogrid.antiabuse.v1.AbuseFilterService",
	})
}
