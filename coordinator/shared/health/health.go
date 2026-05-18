// Package health provides shared /healthz and /readyz handlers for iogrid
// coordinator microservices. Liveness ("am I running?") is decoupled from
// readiness ("am I able to serve traffic?") so Kubernetes can restart a
// deadlocked pod without removing a slow-starting pod from the Service.
package health

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
)

// ReadinessProbe is a function that returns nil when the underlying
// dependency (db, nats, redis, etc.) is healthy.
type ReadinessProbe func() error

// Registry tracks readiness probes; /readyz returns 503 until every probe
// returns nil. /healthz is always 200 once the process has booted (it is a
// pure liveness signal — used by k8s to decide whether to kill the pod).
type Registry struct {
	ready  atomic.Bool
	probes []namedProbe
}

type namedProbe struct {
	name  string
	probe ReadinessProbe
}

// New constructs a registry. Call MarkReady() once startup is complete.
func New() *Registry {
	return &Registry{}
}

// AddProbe registers a readiness probe. Probes are evaluated in registration
// order on each /readyz request.
func (r *Registry) AddProbe(name string, p ReadinessProbe) {
	r.probes = append(r.probes, namedProbe{name: name, probe: p})
}

// MarkReady flips the global readiness latch. Until this is called, /readyz
// returns 503 regardless of probe state — this lets the service finish
// migrations / warm caches before accepting traffic.
func (r *Registry) MarkReady() {
	r.ready.Store(true)
}

// Healthz responds 200 OK if the process is alive. No dependency checks.
func (r *Registry) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// Readyz responds 200 OK only when MarkReady has been called and every probe
// returns nil. On failure, returns 503 with a JSON body naming the first
// failing probe.
func (r *Registry) Readyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if !r.ready.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "starting"})
		return
	}
	for _, np := range r.probes {
		if err := np.probe(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "not_ready",
				"probe":  np.name,
				"error":  err.Error(),
			})
			return
		}
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
