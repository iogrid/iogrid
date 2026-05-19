// Package collector builds the OpenTelemetry Collector gateway
// configuration consumed by the otelcol container that runs alongside
// telemetry-svc inside the iogrid namespace.
//
// The collector is a CENTRAL GATEWAY (not a per-pod sidecar): every
// coordinator microservice (and the Rust daemon, via egress NetworkPolicy)
// pushes OTLP to a single ClusterIP Service on :4317 (gRPC) / :4318 (HTTP).
// The gateway aggregates, attributes, filters, tail-samples, and fans out
// to the existing OpenOva mothership Grafana stack (Mimir for metrics,
// Loki for logs, Tempo for traces).
//
// Why a gateway, not a sidecar:
//   - Tail-sampling needs the full trace assembled in one process.
//   - Per-flow attribute enrichment (cluster_id / region tags) lives in
//     ONE place — no chance of drift between services.
//   - Single egress point to the mothership = single NetworkPolicy rule.
//
// The config is generated from a Go text/template at boot, written to
// the path in config.CollectorConfigPath, and watched by the otelcol
// container for hot reload (otelcol's built-in config-watcher trigger).
//
// Why generate from Go rather than hand-write a static YAML? Because the
// exporter endpoints (Mimir / Loki / Tempo URLs) come from env vars at
// deploy time and an exporter MUST NOT appear in the pipeline list if its
// URL is empty — otelcol would fail startup with "endpoint not specified".
// A static YAML can't conditionally omit a pipeline entry; a template can.
package collector

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
)

// templateData is the strongly-typed payload fed into collectorTemplate.
//
// Each *Enabled flag drives whether the corresponding exporter and its
// pipeline entry appear in the rendered YAML. Sampling rules are
// emitted unconditionally — they cost nothing if no traces flow.
type templateData struct {
	ClusterID    string
	Region       string
	GRPCEndpoint string
	HTTPEndpoint string

	MimirEnabled bool
	MimirURL     string
	MimirHeaders map[string]string

	LokiEnabled bool
	LokiURL     string
	LokiHeaders map[string]string

	TempoEnabled  bool
	TempoEndpoint string
	TempoHeaders  map[string]string
	TempoInsecure bool

	BaselineSamplingPercent int
	SlowTraceThresholdMS    int
	MemoryLimitMiB          int
	BatchTimeoutSeconds     int
	BatchMaxSize            int

	ScrapeNamespaces []string
}

// Build renders the collector config YAML for the supplied runtime
// config. The returned bytes are deterministic for the same input — the
// caller can hash them to detect a no-op write and skip the reload.
func Build(cfg config.Config) ([]byte, error) {
	data := templateData{
		ClusterID:               cfg.ClusterID,
		Region:                  cfg.Region,
		GRPCEndpoint:            cfg.CollectorGRPCAddr,
		HTTPEndpoint:            cfg.CollectorHTTPAddr,
		MimirEnabled:            cfg.MimirURL != "",
		MimirURL:                cfg.MimirURL,
		MimirHeaders:            mimirHeaders(cfg),
		LokiEnabled:             cfg.LokiURL != "",
		LokiURL:                 cfg.LokiURL,
		LokiHeaders:             lokiHeaders(cfg),
		TempoEnabled:            cfg.TempoURL != "",
		TempoEndpoint:           cfg.TempoURL,
		TempoHeaders:            tempoHeaders(cfg),
		TempoInsecure:           cfg.TempoInsecure,
		BaselineSamplingPercent: cfg.BaselineSamplingPercent,
		SlowTraceThresholdMS:    cfg.SlowTraceThresholdMS,
		MemoryLimitMiB:          cfg.MemoryLimitMiB,
		BatchTimeoutSeconds:     int(cfg.BatchTimeout.Seconds()),
		BatchMaxSize:            cfg.BatchMaxSize,
		ScrapeNamespaces:        cfg.PrometheusScrapeNamespaces,
	}

	tpl, err := template.New("otelcol").Parse(collectorTemplate)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute template: %w", err)
	}
	return buf.Bytes(), nil
}

// WriteAtomic renders the config and writes it to cfg.CollectorConfigPath
// using a write-temp-then-rename pattern so an in-flight otelcol reload
// never observes a half-written file.
func WriteAtomic(cfg config.Config) error {
	out, err := Build(cfg)
	if err != nil {
		return err
	}
	dir := filepath.Dir(cfg.CollectorConfigPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".otelcol-*.yaml")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmp.Name(), cfg.CollectorConfigPath); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp.Name(), cfg.CollectorConfigPath, err)
	}
	return nil
}

func mimirHeaders(cfg config.Config) map[string]string {
	h := map[string]string{}
	if cfg.MimirTenant != "" {
		h["X-Scope-OrgID"] = cfg.MimirTenant
	}
	if cfg.MimirBasicAuth != "" {
		h["Authorization"] = basicAuth(cfg.MimirBasicAuth)
	}
	return h
}

func lokiHeaders(cfg config.Config) map[string]string {
	h := map[string]string{}
	if cfg.LokiTenant != "" {
		h["X-Scope-OrgID"] = cfg.LokiTenant
	}
	if cfg.LokiBasicAuth != "" {
		h["Authorization"] = basicAuth(cfg.LokiBasicAuth)
	}
	return h
}

func tempoHeaders(cfg config.Config) map[string]string {
	h := map[string]string{}
	if cfg.TempoTenant != "" {
		h["X-Scope-OrgID"] = cfg.TempoTenant
	}
	return h
}

// basicAuth wraps a "user:password" string into the standard
// Authorization header value. The collector consumes this verbatim.
func basicAuth(userPass string) string {
	return "Basic ${BASE64:" + userPass + "}"
}

// EndpointHints returns a human-readable summary of which exporters are
// armed. Used by the /status handler and startup logs so operators can
// confirm a deploy didn't silently drop a leg of the pipeline.
func EndpointHints(cfg config.Config) []string {
	hints := []string{}
	if cfg.MimirURL != "" {
		hints = append(hints, "mimir:"+cfg.MimirURL)
	} else {
		hints = append(hints, "mimir:DISABLED (set MIMIR_URL)")
	}
	if cfg.LokiURL != "" {
		hints = append(hints, "loki:"+cfg.LokiURL)
	} else {
		hints = append(hints, "loki:DISABLED (set LOKI_URL)")
	}
	if cfg.TempoURL != "" {
		hints = append(hints, "tempo:"+cfg.TempoURL)
	} else {
		hints = append(hints, "tempo:DISABLED (set TEMPO_URL)")
	}
	return hints
}

// NamespacesSummary returns the scrape namespaces joined for log output.
func NamespacesSummary(cfg config.Config) string {
	if len(cfg.PrometheusScrapeNamespaces) == 0 {
		return "(none)"
	}
	return strings.Join(cfg.PrometheusScrapeNamespaces, ",")
}
