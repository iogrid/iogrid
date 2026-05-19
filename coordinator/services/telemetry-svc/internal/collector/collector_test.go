package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
)

func baseCfg() config.Config {
	return config.Config{
		ClusterID:                  "iogrid-prod",
		Region:                     "eu-central-1",
		CollectorGRPCAddr:          "0.0.0.0:4317",
		CollectorHTTPAddr:          "0.0.0.0:4318",
		BaselineSamplingPercent:    1,
		SlowTraceThresholdMS:       500,
		MemoryLimitMiB:             400,
		BatchTimeout:               5 * time.Second,
		BatchMaxSize:               8192,
		PrometheusScrapeNamespaces: []string{"iogrid"},
		CollectorConfigPath:        "/tmp/otelcol.yaml",
	}
}

func TestBuild_AllDisabled(t *testing.T) {
	out, err := Build(baseCfg())
	if err != nil {
		t.Fatalf("Build err: %v", err)
	}
	s := string(out)
	if strings.Contains(s, "prometheusremotewrite/mimir:") {
		t.Errorf("mimir exporter must be absent when MIMIR_URL is empty")
	}
	if strings.Contains(s, "loki:\n    endpoint") {
		t.Errorf("loki exporter must be absent when LOKI_URL is empty")
	}
	if strings.Contains(s, "otlp/tempo:") {
		t.Errorf("tempo exporter must be absent when TEMPO_URL is empty")
	}
	// receivers / processors are unconditional
	for _, want := range []string{
		"receivers:",
		"otlp:",
		"prometheus:",
		"memory_limiter:",
		"attributes/iogrid_tags:",
		"tail_sampling:",
		"batch:",
		"cluster_id",
		"iogrid-prod",
		"eu-central-1",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered config missing %q\n--- output ---\n%s", want, s)
		}
	}
}

func TestBuild_AllEnabled(t *testing.T) {
	c := baseCfg()
	c.MimirURL = "https://mimir.example/api/v1/push"
	c.MimirTenant = "iogrid"
	c.LokiURL = "https://loki.example/loki/api/v1/push"
	c.LokiTenant = "iogrid"
	c.TempoURL = "tempo.example:4317"
	c.TempoTenant = "iogrid"

	out, err := Build(c)
	if err != nil {
		t.Fatalf("Build err: %v", err)
	}
	s := string(out)
	for _, want := range []string{
		"prometheusremotewrite/mimir:",
		"https://mimir.example/api/v1/push",
		"loki:",
		"https://loki.example/loki/api/v1/push",
		"otlp/tempo:",
		"tempo.example:4317",
		"X-Scope-OrgID",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendered config missing %q", want)
		}
	}
}

func TestBuild_SamplingPolicies(t *testing.T) {
	out, _ := Build(baseCfg())
	s := string(out)
	for _, want := range []string{
		"errors-always",
		"slow-always",
		"flagged-abuse-always",
		"baseline-probabilistic",
		"iogrid.flagged_abuse",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("tail_sampling missing policy %q", want)
		}
	}
}

func TestBuild_FilterDropsNoisySpans(t *testing.T) {
	out, _ := Build(baseCfg())
	s := string(out)
	for _, want := range []string{
		`attributes["http.target"] == "/healthz"`,
		`attributes["http.target"] == "/readyz"`,
		`attributes["http.target"] == "/metrics"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("filter/drop_noisy missing rule %q", want)
		}
	}
}

func TestBuild_Deterministic(t *testing.T) {
	c := baseCfg()
	a, _ := Build(c)
	b, _ := Build(c)
	if string(a) != string(b) {
		t.Errorf("Build is not deterministic for the same input")
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	c := baseCfg()
	c.CollectorConfigPath = filepath.Join(dir, "sub", "otelcol.yaml")
	if err := WriteAtomic(c); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}
	data, err := os.ReadFile(c.CollectorConfigPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(data), "receivers:") {
		t.Errorf("written file missing receivers block")
	}
}

func TestEndpointHints(t *testing.T) {
	c := baseCfg()
	c.MimirURL = "https://m.example/push"
	hints := EndpointHints(c)
	gotMimir, gotLoki, gotTempo := false, false, false
	for _, h := range hints {
		if strings.HasPrefix(h, "mimir:https://m.example/push") {
			gotMimir = true
		}
		if strings.HasPrefix(h, "loki:DISABLED") {
			gotLoki = true
		}
		if strings.HasPrefix(h, "tempo:DISABLED") {
			gotTempo = true
		}
	}
	if !gotMimir || !gotLoki || !gotTempo {
		t.Errorf("EndpointHints unexpected: %v", hints)
	}
}

func TestNamespacesSummary(t *testing.T) {
	c := baseCfg()
	c.PrometheusScrapeNamespaces = []string{"iogrid", "monitoring"}
	if got := NamespacesSummary(c); got != "iogrid,monitoring" {
		t.Errorf("NamespacesSummary = %q", got)
	}
	c.PrometheusScrapeNamespaces = nil
	if got := NamespacesSummary(c); got != "(none)" {
		t.Errorf("NamespacesSummary empty = %q", got)
	}
}
