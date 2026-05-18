//go:build integration

package collector

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
)

// TestIntegration_ValidateRenderedConfig spins the rendered collector
// config through `otelcol-contrib validate` to catch any YAML the
// upstream collector binary would reject at boot.
//
// Build tag `integration` keeps it out of the default `go test ./...`
// run — CI sets `-tags=integration` only after installing
// otelcol-contrib in the runner image.
//
// docker run --rm -i otel/opentelemetry-collector-contrib:0.105.0 validate --config=/dev/stdin < /tmp/otelcol.yaml
//
// The simpler local form below shells out to a binary on $PATH so the
// dev loop is `go test -tags=integration ./...`.
func TestIntegration_ValidateRenderedConfig(t *testing.T) {
	bin, err := exec.LookPath("otelcol-contrib")
	if err != nil {
		t.Skipf("otelcol-contrib not on PATH; install via `go install github.com/open-telemetry/opentelemetry-collector-releases/cmd/otelcol-contrib@latest` or run inside the upstream image")
	}

	dir := t.TempDir()
	cfg := config.Config{
		ClusterID:                  "iogrid-test",
		Region:                     "ci",
		CollectorGRPCAddr:          "0.0.0.0:4317",
		CollectorHTTPAddr:          "0.0.0.0:4318",
		MimirURL:                   "https://mimir.example/api/v1/push",
		MimirTenant:                "iogrid",
		LokiURL:                    "https://loki.example/loki/api/v1/push",
		LokiTenant:                 "iogrid",
		TempoURL:                   "tempo.example:4317",
		TempoTenant:                "iogrid",
		TempoInsecure:              true,
		BaselineSamplingPercent:    1,
		SlowTraceThresholdMS:       500,
		MemoryLimitMiB:             400,
		BatchTimeout:               5 * time.Second,
		BatchMaxSize:               8192,
		PrometheusScrapeNamespaces: []string{"iogrid"},
		CollectorConfigPath:        filepath.Join(dir, "otelcol.yaml"),
	}
	if err := WriteAtomic(cfg); err != nil {
		t.Fatalf("WriteAtomic: %v", err)
	}

	cmd := exec.Command(bin, "validate", "--config="+cfg.CollectorConfigPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("otelcol-contrib validate failed: %v\n--- output ---\n%s\n--- config ---\n%s",
			err, string(out), mustRead(cfg.CollectorConfigPath))
	}
}

func mustRead(p string) string {
	b, _ := os.ReadFile(p)
	return string(b)
}
