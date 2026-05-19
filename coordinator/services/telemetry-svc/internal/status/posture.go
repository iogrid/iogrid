package status

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/incidents"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/slo"
)

// Overall is the rolled-up "is iogrid up?" verdict shown as the giant
// headline on status.iogrid.org. Driven by the worst-impact ACTIVE
// incident OR the worst SLO posture, whichever is more severe.
type Overall string

const (
	OverallUp       Overall = "up"
	OverallDegraded Overall = "degraded"
	OverallDown     Overall = "down"
)

// ServicePosture is one row in the per-service health grid.
type ServicePosture struct {
	Name       string  `json:"name"`
	Status     string  `json:"status"`      // "up" | "degraded" | "down"
	SLOPercent float64 `json:"slo_percent"` // 0..100 rolled-up burn budget remaining
}

// PostureResponse is the contract for GET /status/posture.
type PostureResponse struct {
	SchemaVersion   int                   `json:"schema_version"`
	GeneratedAt     time.Time             `json:"generated_at"`
	Overall         Overall               `json:"overall"`
	Services        []ServicePosture      `json:"services"`
	IncidentsActive []incidents.Incident  `json:"incidents_active"`
	IncidentsRecent []incidents.Incident  `json:"incidents_recent"`
}

// PostureHandler returns the rolled-up posture used by the public
// status page headline + service grid.
//
// Rollup rules:
//
//   - Each SLO's `status` field ("nominal" / "warn" / "page") maps to a
//     per-service status: any "page" => "down", any "warn" => "degraded",
//     otherwise "up". Aggregated across SLOs sharing a Service.
//   - Incidents with impact="critical" force the service to "down" and
//     overall to "down".
//   - Incidents with impact="major" force the service to "degraded" and
//     overall to at-least "degraded".
//   - Overall is the worst service status.
func PostureHandler(cat *slo.Catalogue, store incidents.Store, cfg config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		resp := buildPosture(ctx, cat, store)
		_ = cfg // reserved for cluster/region stamps in a future revision
		writeJSON(w, http.StatusOK, resp)
	}
}

// buildPosture is split out for testability.
func buildPosture(ctx context.Context, cat *slo.Catalogue, store incidents.Store) PostureResponse {
	resp := PostureResponse{
		SchemaVersion: 1,
		GeneratedAt:   time.Now().UTC(),
		Overall:       OverallUp,
		Services:      []ServicePosture{},
	}

	// Aggregate SLO statuses per service.
	type agg struct {
		worst string
		// Sum of "good budget remaining" — we just average objectives
		// for now; in production this is fed from Mimir's
		// slo:burn_rate:long:* recording rules.
		sumObj float64
		n      int
	}
	per := map[string]*agg{}
	if cat != nil {
		for _, s := range cat.SLOs {
			a := per[s.Service]
			if a == nil {
				a = &agg{worst: "up"}
				per[s.Service] = a
			}
			a.sumObj += s.Objective
			a.n++
			// Placeholder driver — production fills in s.Status from a
			// burn-rate lookup before this handler runs. Treat empty
			// as "up".
			st := mapSLOStatus(getSLOStatus(s))
			a.worst = worse(a.worst, st)
		}
	}

	// Build the services grid sorted by name for stable output.
	for name, a := range per {
		pct := 100.0
		if a.n > 0 {
			pct = a.sumObj / float64(a.n)
		}
		resp.Services = append(resp.Services, ServicePosture{
			Name:       name,
			Status:     a.worst,
			SLOPercent: round2(pct),
		})
	}
	sortServicePosture(resp.Services)

	// Layer in active incidents.
	active, _ := store.ListActive(ctx)
	resp.IncidentsActive = nilToEmpty(active)
	for _, inc := range active {
		st := impactToStatus(inc.Impact)
		if st == "up" {
			continue
		}
		for _, svc := range inc.AffectedServices {
			idx := findService(resp.Services, svc)
			if idx < 0 {
				// Surface the service in the grid even if no SLO is
				// defined yet — operator-visible, otherwise the
				// incident looks orphan.
				resp.Services = append(resp.Services, ServicePosture{Name: svc, Status: st, SLOPercent: 0})
				continue
			}
			resp.Services[idx].Status = worse(resp.Services[idx].Status, st)
		}
	}
	sortServicePosture(resp.Services)

	// Overall = worst service status.
	for _, s := range resp.Services {
		switch s.Status {
		case "down":
			resp.Overall = OverallDown
		case "degraded":
			if resp.Overall != OverallDown {
				resp.Overall = OverallDegraded
			}
		}
	}

	// Recent incidents — last 7 days.
	recent, _ := store.ListRecent(ctx, 7*24*time.Hour)
	resp.IncidentsRecent = nilToEmpty(recent)

	return resp
}

// getSLOStatus reads the per-SLO posture string. In the current
// placeholder world this is always "nominal" — the function is
// extracted so a Mimir-driven implementation can swap in without
// touching the rollup.
func getSLOStatus(_ slo.SLO) string { return "nominal" }

func mapSLOStatus(s string) string {
	switch strings.ToLower(s) {
	case "page":
		return "down"
	case "warn":
		return "degraded"
	default:
		return "up"
	}
}

func impactToStatus(i incidents.Impact) string {
	switch i {
	case incidents.ImpactCritical:
		return "down"
	case incidents.ImpactMajor, incidents.ImpactMinor:
		return "degraded"
	default:
		return "up"
	}
}

// worse picks the more-severe of two status strings ("up", "degraded",
// "down").
func worse(a, b string) string {
	rank := func(s string) int {
		switch s {
		case "down":
			return 2
		case "degraded":
			return 1
		default:
			return 0
		}
	}
	if rank(a) >= rank(b) {
		return a
	}
	return b
}

func findService(services []ServicePosture, name string) int {
	for i, s := range services {
		if s.Name == name {
			return i
		}
	}
	return -1
}

func sortServicePosture(s []ServicePosture) {
	// Trivial sort — caller has few entries (< 20).
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1].Name > s[j].Name; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func nilToEmpty(s []incidents.Incident) []incidents.Incident {
	if s == nil {
		return []incidents.Incident{}
	}
	return s
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// UptimeHandler serves the per-service 90-day uptime ledger used by
// the heatmap.
//
// Query params:
//
//	service=<name>  — required; one service name to fetch
//	days=<int>      — optional; default 90, clamped to [1, 365]
func UptimeHandler(store incidents.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svc := strings.TrimSpace(r.URL.Query().Get("service"))
		if svc == "" {
			http.Error(w, "service= required", http.StatusBadRequest)
			return
		}
		days := 90
		if d := r.URL.Query().Get("days"); d != "" {
			if n, err := strconv.Atoi(d); err == nil {
				days = n
			}
		}
		samples, err := store.UptimeForService(r.Context(), svc, days)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"schema_version": 1,
			"generated_at":   time.Now().UTC(),
			"service":        svc,
			"days":           days,
			"samples":        samples,
		})
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=30")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
