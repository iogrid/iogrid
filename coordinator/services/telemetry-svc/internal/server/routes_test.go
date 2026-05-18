package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/slo"
)

func newRouter(t *testing.T, deps Deps) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	MountFunc(deps)(r)
	return r
}

func TestIndex(t *testing.T) {
	r := newRouter(t, Deps{Cfg: config.Config{}, Cat: &slo.Catalogue{}})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	var got map[string]string
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got["service"] != "telemetry-svc" {
		t.Errorf("service = %q", got["service"])
	}
	if got["status"] != "ready" {
		t.Errorf("status = %q", got["status"])
	}
}

func TestSLOsHandler(t *testing.T) {
	cat := &slo.Catalogue{SLOs: []slo.SLO{{
		Service: "proxy-gateway", Name: "availability",
		Objective: 99.9, TimeWindow: "30d",
		SLI: slo.SLI{Good: "g", Total: "t"},
	}}}
	r := newRouter(t, Deps{Cfg: config.Config{}, Cat: cat})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/slos", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "proxy-gateway") {
		t.Errorf("body missing proxy-gateway: %s", rec.Body.String())
	}
}

func TestStatusRoute(t *testing.T) {
	r := newRouter(t, Deps{Cfg: config.Config{ClusterID: "c", Region: "r"}, Cat: &slo.Catalogue{}})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"cluster_id":"c"`) {
		t.Errorf("status response missing cluster_id: %s", rec.Body.String())
	}
}

func TestCollectorConfigHandler(t *testing.T) {
	r := newRouter(t, Deps{Cfg: config.Config{
		ClusterID: "c", Region: "r",
		CollectorGRPCAddr: "0.0.0.0:4317", CollectorHTTPAddr: "0.0.0.0:4318",
	}, Cat: &slo.Catalogue{}})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/collector-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "receivers:") {
		t.Errorf("collector-config response missing receivers block")
	}
}

func TestReloadHandler(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{
		ClusterID:           "c",
		Region:              "r",
		CollectorGRPCAddr:   "0.0.0.0:4317",
		CollectorHTTPAddr:   "0.0.0.0:4318",
		CollectorConfigPath: filepath.Join(dir, "otelcol.yaml"),
	}
	r := newRouter(t, Deps{Cfg: cfg, Cat: &slo.Catalogue{}})
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/admin/reload", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code = %d, body=%s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(cfg.CollectorConfigPath); err != nil {
		t.Errorf("reload did not write collector config: %v", err)
	}
}
