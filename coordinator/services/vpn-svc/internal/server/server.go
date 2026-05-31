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

		// Regional failover
		r.Post("/sessions/{sessionID}/failover", NewTriggerFailover(st, logger).Handle)
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
