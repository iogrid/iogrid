package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	for _, k := range []string{
		"LISTEN_ADDR", "CLUSTER_ID", "REGION",
		"COLLECTOR_GRPC_ADDR", "COLLECTOR_HTTP_ADDR",
		"MIMIR_URL", "MIMIR_TENANT", "MIMIR_BASIC_AUTH",
		"LOKI_URL", "LOKI_TENANT", "LOKI_BASIC_AUTH",
		"TEMPO_URL", "TEMPO_TENANT", "TEMPO_INSECURE",
		"BASELINE_SAMPLING_PERCENT", "SLOW_TRACE_THRESHOLD_MS",
		"MEMORY_LIMIT_MIB", "BATCH_TIMEOUT", "BATCH_MAX_SIZE",
		"SLO_DIR", "PROM_SCRAPE_NAMESPACES", "COLLECTOR_CONFIG_PATH",
	} {
		_ = os.Unsetenv(k)
	}
	c := Load()
	if c.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080", c.ListenAddr)
	}
	if c.ClusterID != "iogrid-prod" {
		t.Errorf("ClusterID = %q, want iogrid-prod", c.ClusterID)
	}
	if c.Region != "eu-central-1" {
		t.Errorf("Region = %q, want eu-central-1", c.Region)
	}
	if c.CollectorGRPCAddr != "0.0.0.0:4317" {
		t.Errorf("CollectorGRPCAddr = %q, want 0.0.0.0:4317", c.CollectorGRPCAddr)
	}
	if c.CollectorHTTPAddr != "0.0.0.0:4318" {
		t.Errorf("CollectorHTTPAddr = %q, want 0.0.0.0:4318", c.CollectorHTTPAddr)
	}
	if c.BaselineSamplingPercent != 1 {
		t.Errorf("BaselineSamplingPercent = %d, want 1", c.BaselineSamplingPercent)
	}
	if c.SlowTraceThresholdMS != 500 {
		t.Errorf("SlowTraceThresholdMS = %d, want 500", c.SlowTraceThresholdMS)
	}
	if c.MemoryLimitMiB != 400 {
		t.Errorf("MemoryLimitMiB = %d, want 400", c.MemoryLimitMiB)
	}
	if c.BatchTimeout != 5*time.Second {
		t.Errorf("BatchTimeout = %v, want 5s", c.BatchTimeout)
	}
	if c.BatchMaxSize != 8192 {
		t.Errorf("BatchMaxSize = %d, want 8192", c.BatchMaxSize)
	}
	if len(c.PrometheusScrapeNamespaces) != 1 || c.PrometheusScrapeNamespaces[0] != "iogrid" {
		t.Errorf("PrometheusScrapeNamespaces = %v, want [iogrid]", c.PrometheusScrapeNamespaces)
	}
}

func TestLoad_Overrides(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9090")
	t.Setenv("CLUSTER_ID", "iogrid-staging")
	t.Setenv("MIMIR_URL", "https://mimir.example/api/v1/push")
	t.Setenv("LOKI_URL", "https://loki.example/loki/api/v1/push")
	t.Setenv("TEMPO_URL", "tempo.example:4317")
	t.Setenv("BASELINE_SAMPLING_PERCENT", "5")
	t.Setenv("SLOW_TRACE_THRESHOLD_MS", "1000")
	t.Setenv("PROM_SCRAPE_NAMESPACES", "iogrid,monitoring,observability")

	c := Load()
	if c.ListenAddr != ":9090" {
		t.Errorf("ListenAddr override failed: %q", c.ListenAddr)
	}
	if c.ClusterID != "iogrid-staging" {
		t.Errorf("ClusterID override failed: %q", c.ClusterID)
	}
	if c.MimirURL != "https://mimir.example/api/v1/push" {
		t.Errorf("MimirURL override failed: %q", c.MimirURL)
	}
	if c.LokiURL != "https://loki.example/loki/api/v1/push" {
		t.Errorf("LokiURL override failed: %q", c.LokiURL)
	}
	if c.TempoURL != "tempo.example:4317" {
		t.Errorf("TempoURL override failed: %q", c.TempoURL)
	}
	if c.BaselineSamplingPercent != 5 {
		t.Errorf("BaselineSamplingPercent override failed: %d", c.BaselineSamplingPercent)
	}
	if c.SlowTraceThresholdMS != 1000 {
		t.Errorf("SlowTraceThresholdMS override failed: %d", c.SlowTraceThresholdMS)
	}
	if len(c.PrometheusScrapeNamespaces) != 3 {
		t.Errorf("PrometheusScrapeNamespaces override failed: %v", c.PrometheusScrapeNamespaces)
	}
}

func TestLoad_InvalidNumbersFallBack(t *testing.T) {
	t.Setenv("BASELINE_SAMPLING_PERCENT", "not-a-number")
	t.Setenv("BATCH_TIMEOUT", "not-a-duration")
	c := Load()
	if c.BaselineSamplingPercent != 1 {
		t.Errorf("BaselineSamplingPercent = %d, want fallback 1", c.BaselineSamplingPercent)
	}
	if c.BatchTimeout != 5*time.Second {
		t.Errorf("BatchTimeout = %v, want fallback 5s", c.BatchTimeout)
	}
}
