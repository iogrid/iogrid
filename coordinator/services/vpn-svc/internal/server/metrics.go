package server

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Metrics exposes counters + histograms for vpn-svc operations.
// Registered on the default Prometheus registry — the shared server
// package mounts /metrics from there.
var (
	SessionsCreated = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "iogrid",
		Subsystem: "vpn_svc",
		Name:      "sessions_created_total",
		Help:      "Total VPN sessions created via POST /v1/vpn/sessions",
	})

	SessionsTerminated = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "iogrid",
		Subsystem: "vpn_svc",
		Name:      "sessions_terminated_total",
		Help:      "Total VPN sessions terminated, labeled by reason",
	}, []string{"reason"})

	FailoversTriggered = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "iogrid",
		Subsystem: "vpn_svc",
		Name:      "failovers_triggered_total",
		Help:      "Total failovers triggered, labeled by region and outcome (success|no_alternate)",
	}, []string{"region", "outcome"})

	ProviderHealthChanges = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "iogrid",
		Subsystem: "vpn_svc",
		Name:      "provider_health_changes_total",
		Help:      "Provider health status transitions (healthy → degraded → offline)",
	}, []string{"to_status"})

	// ProviderKeyRotations counts how often a provider re-registered with
	// a WireGuard server pubkey that differed from the one already stored
	// (#762). Each such event is a re-provision / wiped-state-dir vector
	// that would otherwise silently strand every client baked against the
	// old server key; the handler force-terminates the affected sessions
	// so clients reconnect against the new key. A non-zero rate here is the
	// signal to make the daemon's wg.key durable (issue (b)).
	ProviderKeyRotations = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "iogrid",
		Subsystem: "vpn_svc",
		Name:      "provider_key_rotations_total",
		Help:      "Provider re-registered with a changed WG server pubkey (forces bound-session invalidation, #762)",
	})

	ICECandidatesRegistered = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "iogrid",
		Subsystem: "vpn_svc",
		Name:      "ice_candidates_registered_total",
		Help:      "Total ICE candidates registered by providers",
	})

	SessionRefreshes = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "iogrid",
		Subsystem: "vpn_svc",
		Name:      "session_refreshes_total",
		Help:      "Total session heartbeat refreshes received",
	})

	STUNRequests = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "iogrid",
		Subsystem: "vpn_svc",
		Name:      "stun_requests_total",
		Help:      "Total STUN BINDING REQUESTs processed (RFC 5389)",
	})

	// MobileSessionRequests counts POST /v1/vpn/sessions/mobile by
	// outcome. Splits the new mobile bring-up flow from the legacy
	// daemon-driven /sessions counter so dashboards + alerts can
	// observe mobile separately. Labels:
	//   outcome ∈ {created, no_peer, bad_request, unauthorized, internal_error}
	// — matches the handler's response paths in
	// internal/server/handlers.go::RequestMobileSession.Handle.
	MobileSessionRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "iogrid",
		Subsystem: "vpn_svc",
		Name:      "mobile_session_requests_total",
		Help:      "Total POST /v1/vpn/sessions/mobile requests, labeled by outcome",
	}, []string{"outcome"})
)
