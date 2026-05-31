package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

// Mount registers all VPN service routes on the chi router.
func Mount(h chi.Router, st store.Store, logger *slog.Logger) error {
	h.Route("/v1/vpn", func(r chi.Router) {
		// Session endpoints
		r.Post("/sessions", NewRequestSession(st, logger).Handle)
		r.Get("/sessions/{sessionID}", NewGetSession(st, logger).Handle)
		r.Put("/sessions/{sessionID}/confirm", NewConfirmCandidate(st, logger).Handle)
		r.Post("/sessions/{sessionID}/refresh", NewRefreshSession(st, logger).Handle)
		r.Post("/sessions/{sessionID}/terminate", NewTerminateSession(st, logger).Handle)

		// Provider ICE candidate registration
		r.Post("/providers/{providerID}/candidates", NewRegisterCandidates(st, logger).Handle)
		r.Get("/providers/{providerID}/candidates", NewGetCandidates(st, logger).Handle)

		// Provider health probes + graceful shutdown (VPN-7, #511).
		// `/health` is the periodic heartbeat (every 15 s from the
		// daemon); `/offline` is the one-shot SIGTERM notification
		// the daemon emits during graceful shutdown so customer
		// SDKs can failover before the staleness window expires.
		r.Post("/providers/{providerID}/health", NewUpdateHealth(st, logger).Handle)
		r.Post("/providers/{providerID}/offline", NewMarkOffline(st, logger).Handle)

		// Regional failover
		r.Post("/sessions/{sessionID}/failover", NewTriggerFailover(st, logger).Handle)

		// Regional provider listing — for debugging + the VPN-18 smoke
		// test. Returns providers grouped by region with health status
		// + session_count. Read-only; no auth (read-mostly metadata).
		r.Get("/regions/{region}/providers", NewListProvidersInRegion(st, logger).Handle)
	})

	logger.Info("vpn service routes mounted")
	return nil
}

// Helper to respond with JSON errors
func respondError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	w.Header().Set("Content-Type", "application/json")
	// TODO: proper JSON error response format
}
