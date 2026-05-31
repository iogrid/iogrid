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
)
