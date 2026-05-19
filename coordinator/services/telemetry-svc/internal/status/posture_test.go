package status

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/incidents"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/slo"
)

// TestBuildPosture_RollsUpMultipleServicesCorrectly is the headline
// integration test for the public status page contract.
//
// It exercises the full posture rollup with:
//   - 3 services worth of SLOs (proxy-gateway, identity-svc, workloads-svc)
//   - 2 active incidents at different severities (critical + minor)
//   - 1 resolved incident which must NOT influence the rollup
//
// Expectations:
//   - Overall = "down" because the critical incident is active
//   - proxy-gateway = "down" (critical impact, listed in affected_services)
//   - identity-svc = "degraded" (minor impact, listed in affected_services)
//   - workloads-svc = "up" (no incident, no SLO breach)
//   - IncidentsActive has both unresolved incidents in newest-first order
//   - IncidentsRecent contains all three (resolved one within 7d window)
func TestBuildPosture_RollsUpMultipleServicesCorrectly(t *testing.T) {
	ctx := context.Background()
	store := incidents.NewInMemory()

	cat := &slo.Catalogue{SLOs: []slo.SLO{
		{Service: "proxy-gateway", Name: "availability", Objective: 99.9, TimeWindow: "30d", SLI: slo.SLI{Good: "g", Total: "t"}},
		{Service: "identity-svc", Name: "magic-link", Objective: 95.0, TimeWindow: "30d", SLI: slo.SLI{Good: "g", Total: "t"}},
		{Service: "workloads-svc", Name: "dispatch-latency", Objective: 99.5, TimeWindow: "30d", SLI: slo.SLI{Good: "g", Total: "t"}},
	}}

	// One resolved incident (last week) — must not pollute active rollup.
	resolved, err := store.CreateIncident(ctx, incidents.CreateIncidentInput{
		Title: "Past outage", Impact: incidents.ImpactMajor,
		AffectedServices: []string{"proxy-gateway"},
	})
	if err != nil {
		t.Fatalf("create resolved: %v", err)
	}
	if _, err := store.AppendUpdate(ctx, resolved.ID, incidents.UpdateIncidentInput{Status: incidents.StatusResolved, Body: "fixed"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	// One critical active incident on proxy-gateway.
	if _, err := store.CreateIncident(ctx, incidents.CreateIncidentInput{
		Title: "Proxy regional outage", Impact: incidents.ImpactCritical,
		AffectedServices: []string{"proxy-gateway"},
	}); err != nil {
		t.Fatalf("create critical: %v", err)
	}

	// One minor active incident on identity-svc.
	if _, err := store.CreateIncident(ctx, incidents.CreateIncidentInput{
		Title: "Magic-link mail lag", Impact: incidents.ImpactMinor,
		AffectedServices: []string{"identity-svc"},
	}); err != nil {
		t.Fatalf("create minor: %v", err)
	}

	resp := buildPosture(ctx, cat, store)

	if resp.Overall != OverallDown {
		t.Errorf("Overall = %q, want %q (critical incident is active)", resp.Overall, OverallDown)
	}
	if resp.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", resp.SchemaVersion)
	}

	want := map[string]string{
		"proxy-gateway": "down",
		"identity-svc":  "degraded",
		"workloads-svc": "up",
	}
	if len(resp.Services) != len(want) {
		t.Errorf("services count = %d, want %d (%+v)", len(resp.Services), len(want), resp.Services)
	}
	for _, svc := range resp.Services {
		got, ok := want[svc.Name]
		if !ok {
			t.Errorf("unexpected service %q in rollup", svc.Name)
			continue
		}
		if svc.Status != got {
			t.Errorf("service %q status = %q, want %q", svc.Name, svc.Status, got)
		}
	}

	if len(resp.IncidentsActive) != 2 {
		t.Errorf("IncidentsActive = %d, want 2 (the resolved one must be excluded)", len(resp.IncidentsActive))
	}
	// Newest-first ordering: minor was created after critical.
	if len(resp.IncidentsActive) == 2 && !resp.IncidentsActive[0].StartedAt.After(resp.IncidentsActive[1].StartedAt) {
		t.Errorf("IncidentsActive not sorted newest-first: %v vs %v",
			resp.IncidentsActive[0].StartedAt, resp.IncidentsActive[1].StartedAt)
	}
	if len(resp.IncidentsRecent) != 3 {
		t.Errorf("IncidentsRecent = %d, want 3 (within 7d window)", len(resp.IncidentsRecent))
	}
}

func TestBuildPosture_EmptyEverything(t *testing.T) {
	resp := buildPosture(context.Background(), &slo.Catalogue{}, incidents.NewInMemory())
	if resp.Overall != OverallUp {
		t.Errorf("Overall = %q, want up (nothing to break)", resp.Overall)
	}
	if len(resp.Services) != 0 {
		t.Errorf("services = %d, want 0", len(resp.Services))
	}
	if resp.IncidentsActive == nil || resp.IncidentsRecent == nil {
		t.Error("incident slices should be empty arrays, not nil — JSON contract")
	}
}

func TestBuildPosture_IncidentAffectsUnknownService(t *testing.T) {
	ctx := context.Background()
	store := incidents.NewInMemory()
	if _, err := store.CreateIncident(ctx, incidents.CreateIncidentInput{
		Title: "Mystery", Impact: incidents.ImpactCritical,
		AffectedServices: []string{"build-gateway"}, // no SLO defined
	}); err != nil {
		t.Fatalf("create: %v", err)
	}
	resp := buildPosture(ctx, &slo.Catalogue{}, store)
	if resp.Overall != OverallDown {
		t.Errorf("Overall = %q, want down", resp.Overall)
	}
	if len(resp.Services) != 1 || resp.Services[0].Name != "build-gateway" || resp.Services[0].Status != "down" {
		t.Errorf("services = %+v, want one synthesised build-gateway/down entry", resp.Services)
	}
}

func TestPostureHandler_ServesJSON(t *testing.T) {
	cfg := config.Config{}
	store := incidents.NewInMemory()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status/posture", nil)
	PostureHandler(&slo.Catalogue{}, store, cfg)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cors := rec.Header().Get("Access-Control-Allow-Origin"); cors != "*" {
		t.Errorf("CORS header missing — status page needs cross-origin fetch")
	}
	var resp PostureResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Overall != OverallUp {
		t.Errorf("default overall = %q, want up", resp.Overall)
	}
}

func TestUptimeHandler_RequiresService(t *testing.T) {
	store := incidents.NewInMemory()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status/uptime", nil)
	UptimeHandler(store)(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400 (missing service= param)", rec.Code)
	}
}

func TestUptimeHandler_HappyPath(t *testing.T) {
	store := incidents.NewInMemory()
	_ = store.RecordSample(context.Background(), incidents.UptimeSample{Service: "proxy-gateway", Day: "2026-05-19", State: "op", SLIPct: 100})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status/uptime?service=proxy-gateway&days=30", nil)
	UptimeHandler(store)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["service"] != "proxy-gateway" {
		t.Errorf("service = %v", body["service"])
	}
	samples, _ := body["samples"].([]any)
	if len(samples) != 30 {
		t.Errorf("samples = %d, want 30", len(samples))
	}
}
