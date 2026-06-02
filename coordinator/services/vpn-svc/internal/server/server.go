package server

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

// Mount registers all VPN service routes on the chi router.
//
// validator may be nil for dev / smoke mode — every POST /v1/vpn/sessions
// is then accepted unauthenticated and the boot WARN log fires. Production
// passes a BillingValidator pointed at billing-svc.
func Mount(h chi.Router, st store.Store, logger *slog.Logger, validator APIKeyValidator) error {
	h.Route("/v1/vpn", func(r chi.Router) {
		// Session endpoints
		r.Post("/sessions", NewRequestSession(st, logger).WithValidator(validator).Handle)
		r.Get("/sessions/{sessionID}", NewGetSession(st, logger).Handle)
		r.Put("/sessions/{sessionID}/confirm", NewConfirmCandidate(st, logger).Handle)
		r.Post("/sessions/{sessionID}/refresh", NewRefreshSession(st, logger).Handle)
		r.Post("/sessions/{sessionID}/terminate", NewTerminateSession(st, logger).Handle)

		// Track 3 (#588): mobile PacketTunnelProvider session bring-up.
		// Distinct from the legacy POST /sessions handler which is the
		// daemon-side ICE-candidate flow — kept separate so the two
		// flows can co-exist while we migrate. The mobile handler
		// returns the complete WG peer config in one round-trip so
		// PacketTunnelProvider can call WireGuardAdapter.start without
		// follow-up calls.
		r.Post("/sessions/mobile", NewRequestMobileSession(st, logger).WithValidator(validator).Handle)
		// Mobile heartbeat (#588 DoD: accept byte counters). Aliased
		// at `/heartbeat` so the mobile JS layer doesn't have to map
		// to the legacy `/refresh` shape.
		r.Post("/sessions/{sessionID}/heartbeat", NewMobileHeartbeat(st, logger).Handle)

		// Provider lifecycle:
		// - POST /providers/{id}/register — daemon's first call on startup,
		//   inserts the row that later health/candidate calls will UPDATE.
		// - candidate registration / retrieval
		r.Post("/providers/{providerID}/register", NewRegisterProvider(st, logger).Handle)
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

		// Region picker for customer CLI / web UI (#545).
		r.Get("/regions", NewListRegions(st, logger).Handle)

		// Customer's own sessions — used by /customer/vpn web page (#541).
		// Auth is by customer_id query param; for now no upstream key
		// validation since this is read-only. Gateway-bff scopes by
		// authenticated workspace before forwarding.
		r.Get("/customers/{customerID}/sessions", NewListSessionsByCustomer(st, logger).Handle)

		// Cascade-terminate every active session owned by the customer
		// (#549). gateway-bff calls this from /logout AFTER the local
		// API key is revoked so a compromised key can't outlive the
		// logout. Returns {"terminated": <count>}.
		r.Post("/customers/{customerID}/sessions/terminate-all", NewTerminateAllForCustomer(st, logger).Handle)

		// WG peer binding (#536) — provider daemon side. Daemon polls
		// /providers/{id}/assigned-sessions every 5s; for each new
		// session it allocates a peer slot and POSTs back its WG
		// public key via /sessions/{id}/bind-provider. Customer SDK
		// reads the same key via GET /sessions/{id} once bound.
		r.Get("/providers/{providerID}/assigned-sessions", NewListAssignedSessions(st, logger).Handle)
		r.Post("/sessions/{sessionID}/bind-provider", NewBindProvider(st, logger).Handle)
		r.Post("/sessions/{sessionID}/bind-customer-wg-key", NewBindCustomerWgKey(st, logger).Handle)
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
