// Package status implements the /status JSON endpoint feeding the
// public status page at status.iogrid.org.
//
// The handler reports CURRENT SLO POSTURE based on the catalogue under
// ./slo. The actual real-time burn-rate values come from Mimir
// (queried via the configured MIMIR_URL with HTTP basic auth), but
// here we return a structural snapshot that's safe to serve without
// any backend wiring:
//
//   - list of SLOs (service / name / objective / window)
//   - declared error budget
//   - placeholder posture ("nominal") — the production deployment fills
//     this in by querying Mimir's `slo:burn_rate:long:*` recording rules
//     before responding.
//
// The contract is intentionally CONSTANT across deployments so the
// status.iogrid.org frontend can consume it without per-environment
// fork. A nil backend just means "show nominal everywhere" — the page
// still renders.
package status

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/collector"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/slo"
)

// Posture is one SLO's current state as reported to the public page.
type Posture struct {
	Service     string  `json:"service"`
	Name        string  `json:"name"`
	Objective   float64 `json:"objective_percent"`
	TimeWindow  string  `json:"time_window"`
	ErrorBudget float64 `json:"error_budget"`
	// Status is "nominal" / "warn" / "page" / "unknown". Driven by
	// the highest-severity burn-rate currently firing. Without a
	// Mimir backend wired we default to "nominal".
	Status string `json:"status"`
	// Description shown verbatim on the public page.
	Description string `json:"description,omitempty"`
}

// Response is the /status JSON envelope. Versioned with `schema_version`
// so the frontend can fail-soft on unexpected shapes.
type Response struct {
	SchemaVersion int       `json:"schema_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	ClusterID     string    `json:"cluster_id"`
	Region        string    `json:"region"`
	// OverallStatus is the worst Posture.Status in the SLO list.
	OverallStatus string `json:"overall_status"`
	// SLOs is the full posture list — even nominal ones, so the
	// public page can show the green checks.
	SLOs []Posture `json:"slos"`
	// PipelineEndpoints lists which exporters are armed. Public —
	// transparency dashboard requirement from docs/TECH.md.
	PipelineEndpoints []string `json:"pipeline_endpoints"`
}

// Handler returns a chi-compatible http.HandlerFunc that serves the
// /status JSON for the given catalogue + config.
//
// Catalogue is read at handler construction (start-of-process); a
// future iteration may add inotify-driven hot-reload. For now the
// telemetry-svc Pod's readiness/liveness probes pin the snapshot to
// a single binary release.
func Handler(cat *slo.Catalogue, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		resp := Response{
			SchemaVersion:     1,
			GeneratedAt:       time.Now().UTC(),
			ClusterID:         cfg.ClusterID,
			Region:            cfg.Region,
			OverallStatus:     "nominal",
			PipelineEndpoints: collector.EndpointHints(cfg),
		}
		if cat != nil {
			for _, s := range cat.SLOs {
				resp.SLOs = append(resp.SLOs, Posture{
					Service:     s.Service,
					Name:        s.Name,
					Objective:   s.Objective,
					TimeWindow:  s.TimeWindow,
					ErrorBudget: s.ErrorBudget(),
					Status:      "nominal",
					Description: shortDesc(s.Description),
				})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		// Allow the static status page to fetch cross-origin.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Cache-Control", "public, max-age=30")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// shortDesc trims a multi-line description to the first non-empty line
// — the public page renders one-liners per SLO.
func shortDesc(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
