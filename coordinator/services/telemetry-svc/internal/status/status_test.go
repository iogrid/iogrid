package status

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/slo"
)

func TestHandler_NilCatalogue(t *testing.T) {
	cfg := config.Config{ClusterID: "iogrid-prod", Region: "eu-central-1"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	Handler(nil, cfg)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.OverallStatus != "nominal" {
		t.Errorf("overall_status = %q", resp.OverallStatus)
	}
	if len(resp.SLOs) != 0 {
		t.Errorf("want 0 SLOs, got %d", len(resp.SLOs))
	}
	if len(resp.PipelineEndpoints) != 3 {
		t.Errorf("want 3 pipeline endpoints, got %d", len(resp.PipelineEndpoints))
	}
	if resp.SchemaVersion != 1 {
		t.Errorf("schema_version = %d", resp.SchemaVersion)
	}
	if resp.GeneratedAt.IsZero() || time.Since(resp.GeneratedAt) > time.Minute {
		t.Errorf("generated_at = %v", resp.GeneratedAt)
	}
}

func TestHandler_PopulatedCatalogue(t *testing.T) {
	cat := &slo.Catalogue{SLOs: []slo.SLO{
		{
			Service: "proxy-gateway", Name: "availability",
			Objective: 99.9, TimeWindow: "30d",
			Description: "Multi-line\nsecond line",
			SLI:         slo.SLI{Good: "g", Total: "t"},
		},
		{
			Service: "identity-svc", Name: "magic-link",
			Objective: 95, TimeWindow: "30d",
			Description: "single line",
			SLI:         slo.SLI{Good: "g", Total: "t"},
		},
	}}
	cfg := config.Config{ClusterID: "c", Region: "r"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	Handler(cat, cfg)(rec, req)

	var resp Response
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.SLOs) != 2 {
		t.Fatalf("want 2 SLOs, got %d", len(resp.SLOs))
	}
	if resp.SLOs[0].Description != "Multi-line" {
		t.Errorf("Description not truncated: %q", resp.SLOs[0].Description)
	}
	if resp.SLOs[1].Description != "single line" {
		t.Errorf("Description = %q", resp.SLOs[1].Description)
	}
	if resp.SLOs[0].ErrorBudget < 0.0009 || resp.SLOs[0].ErrorBudget > 0.0011 {
		t.Errorf("ErrorBudget = %f", resp.SLOs[0].ErrorBudget)
	}
}

func TestHandler_CORSHeader(t *testing.T) {
	cfg := config.Config{ClusterID: "c", Region: "r"}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	Handler(nil, cfg)(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("CORS header missing — public status page needs cross-origin fetch")
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
}
