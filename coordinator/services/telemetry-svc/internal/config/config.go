// Package config centralises the env-var contract for telemetry-svc.
//
// telemetry-svc has two main responsibilities:
//  1. Build the OpenTelemetry Collector gateway configuration the cluster
//     boots alongside it (Mimir / Loki / Tempo exporter endpoints, sampling
//     rules, cluster/region attribution).
//  2. Parse the SLO catalogue under ./slo, expose burn-rate state via
//     /status, and surface burn-rate alerts to Alertmanager via the
//     generated PrometheusRule CRDs.
//
// All values have safe defaults so the binary boots without any env var
// set (exporters become disabled / no-op, sampling stays at 1%). This
// keeps local development frictionless and unit tests offline.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the resolved runtime configuration. Build via Load().
type Config struct {
	// ListenAddr is the telemetry-svc HTTP bind address (default :8080).
	ListenAddr string

	// ClusterID identifies the coordinator cluster (default "iogrid-prod").
	// Stamped on every metric / log / trace via the OTel attributes
	// processor so the mothership Grafana stack can multi-tenant by
	// cluster.
	ClusterID string
	// Region identifies the geographic region (default "eu-central-1").
	Region string

	// CollectorGRPCAddr is the OTLP/gRPC receiver bind for the
	// collector gateway (default :4317).
	CollectorGRPCAddr string
	// CollectorHTTPAddr is the OTLP/HTTP receiver bind (default :4318).
	CollectorHTTPAddr string

	// MimirURL is the Mimir / VictoriaMetrics remote-write endpoint
	// (e.g. https://mimir.openova.io/api/v1/push). Empty disables
	// metric export — the collector still receives metrics so local
	// /metrics scrape works.
	MimirURL string
	// MimirTenant is the X-Scope-OrgID header value (default "iogrid").
	MimirTenant string
	// MimirBasicAuth is "user:password" for HTTP basic auth. Empty
	// disables auth.
	MimirBasicAuth string

	// LokiURL is the Loki push endpoint
	// (e.g. https://loki.openova.io/loki/api/v1/push). Empty disables
	// log export.
	LokiURL string
	// LokiTenant is the X-Scope-OrgID header for Loki.
	LokiTenant string
	// LokiBasicAuth is "user:password" for HTTP basic auth.
	LokiBasicAuth string

	// TempoURL is the Tempo OTLP/gRPC endpoint
	// (e.g. tempo.openova.io:4317). Empty disables trace export.
	TempoURL string
	// TempoTenant is the X-Scope-OrgID header for Tempo.
	TempoTenant string
	// TempoInsecure disables TLS verification on the tempo exporter
	// (default false — production should ALWAYS use TLS).
	TempoInsecure bool

	// BaselineSamplingPercent is the trace tail-sampling baseline
	// (default 1 = 1% of non-error / non-slow spans).
	BaselineSamplingPercent int
	// SlowTraceThresholdMS is the latency threshold (in milliseconds)
	// above which a trace is sampled at 100% (default 500).
	SlowTraceThresholdMS int

	// MemoryLimitMiB caps the collector's heap (default 400).
	MemoryLimitMiB int
	// BatchTimeout is the OTel batch processor flush cadence
	// (default 5s).
	BatchTimeout time.Duration
	// BatchMaxSize is the max batch size (default 8192).
	BatchMaxSize int

	// SLODir is the directory holding the SLO catalogue YAMLs.
	// Defaults to ./slo relative to the binary.
	SLODir string

	// PrometheusScrapeNamespaces is the comma-separated list of k8s
	// namespaces the collector's prometheus receiver auto-discovers
	// (default "iogrid").
	PrometheusScrapeNamespaces []string

	// CollectorConfigPath is where the rendered collector YAML is
	// written. Default /etc/otelcol/config.yaml — k8s ConfigMap mounts
	// here.
	CollectorConfigPath string
}

// Load reads the environment and returns a populated Config.
func Load() Config {
	return Config{
		ListenAddr:                 getenv("LISTEN_ADDR", ":8080"),
		ClusterID:                  getenv("CLUSTER_ID", "iogrid-prod"),
		Region:                     getenv("REGION", "eu-central-1"),
		CollectorGRPCAddr:          getenv("COLLECTOR_GRPC_ADDR", "0.0.0.0:4317"),
		CollectorHTTPAddr:          getenv("COLLECTOR_HTTP_ADDR", "0.0.0.0:4318"),
		MimirURL:                   os.Getenv("MIMIR_URL"),
		MimirTenant:                getenv("MIMIR_TENANT", "iogrid"),
		MimirBasicAuth:             os.Getenv("MIMIR_BASIC_AUTH"),
		LokiURL:                    os.Getenv("LOKI_URL"),
		LokiTenant:                 getenv("LOKI_TENANT", "iogrid"),
		LokiBasicAuth:              os.Getenv("LOKI_BASIC_AUTH"),
		TempoURL:                   os.Getenv("TEMPO_URL"),
		TempoTenant:                getenv("TEMPO_TENANT", "iogrid"),
		TempoInsecure:              boolEnv("TEMPO_INSECURE", false),
		BaselineSamplingPercent:    intEnv("BASELINE_SAMPLING_PERCENT", 1),
		SlowTraceThresholdMS:       intEnv("SLOW_TRACE_THRESHOLD_MS", 500),
		MemoryLimitMiB:             intEnv("MEMORY_LIMIT_MIB", 400),
		BatchTimeout:               durationEnv("BATCH_TIMEOUT", 5*time.Second),
		BatchMaxSize:               intEnv("BATCH_MAX_SIZE", 8192),
		SLODir:                     getenv("SLO_DIR", "./slo"),
		PrometheusScrapeNamespaces: csv(getenv("PROM_SCRAPE_NAMESPACES", "iogrid")),
		CollectorConfigPath:        getenv("COLLECTOR_CONFIG_PATH", "/etc/otelcol/config.yaml"),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func intEnv(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func boolEnv(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return fallback
}

func csv(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
