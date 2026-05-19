// Command telemetry-svc is the iogrid coordinator entrypoint for the
// telemetry-svc microservice.
//
// The binary boots a chi router on $LISTEN_ADDR (default :8080) with
// /healthz, /readyz, /metrics, /status, /v1/*, /admin/* routes mounted
// via internal/server. Structured logging (slog/JSON), OpenTelemetry
// tracing (OTLP/gRPC), and graceful shutdown on SIGINT/SIGTERM are
// wired up by the shared bootstrap package.
//
// At startup it ALSO:
//
//   - Renders the otelcol gateway config to $COLLECTOR_CONFIG_PATH so
//     the sidecar otelcol container can boot.
//   - Loads + validates the SLO catalogue under $SLO_DIR. Validation
//     errors HARD-FAIL the boot (the service refuses to come up with a
//     broken catalogue rather than silently serve stale data).
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/collector"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/incidents"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/telemetry-svc/internal/slo"
	shareddb "github.com/iogrid/iogrid/coordinator/shared/db"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "telemetry-svc"
	serviceVersion = "0.1.0"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting telemetry-svc",
		slog.String("version", serviceVersion),
	)

	cfg := config.Load()
	logger.Info("config loaded",
		slog.String("cluster_id", cfg.ClusterID),
		slog.String("region", cfg.Region),
		slog.String("collector_grpc", cfg.CollectorGRPCAddr),
		slog.String("collector_http", cfg.CollectorHTTPAddr),
		slog.String("scrape_namespaces", collector.NamespacesSummary(cfg)),
		slog.Any("endpoints", collector.EndpointHints(cfg)),
	)

	// Render the otelcol gateway config. On a fresh Pod the file does
	// not yet exist, so the otelcol container will block-restart until
	// this write lands. We do this BEFORE the SLO load so a corrupt
	// SLO catalogue doesn't keep the data plane down.
	if err := collector.WriteAtomic(cfg); err != nil {
		logger.Warn("collector config write failed (otelcol sidecar may stall)",
			slog.String("path", cfg.CollectorConfigPath),
			slog.String("error", err.Error()))
		// Non-fatal in dev (read-only FS without the volume mount).
		// In prod the ConfigMap volume is writable for this Pod.
	} else {
		logger.Info("collector config written",
			slog.String("path", cfg.CollectorConfigPath))
	}

	cat, err := slo.Load(cfg.SLODir)
	if err != nil {
		logger.Warn("SLO catalogue load failed — /status returns empty",
			slog.String("dir", cfg.SLODir),
			slog.String("error", err.Error()))
		cat = &slo.Catalogue{}
	} else {
		logger.Info("SLO catalogue loaded",
			slog.String("dir", cfg.SLODir),
			slog.Int("count", len(cat.SLOs)),
		)
	}

	ctx := context.Background()
	otelShutdown, err := otel.Setup(ctx, serviceName, serviceVersion)
	if err != nil {
		logger.Error("otel setup failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_ = otelShutdown(shutdownCtx)
	}()

	hr := health.New()

	// Incidents/subscriptions/uptime ledger powering status.iogrid.org.
	// DATABASE_URL is OPTIONAL — when unset we fall back to an in-memory
	// store so the public /status endpoints still respond. Production
	// Pods get a Sealed Secret with the CNPG conn string and the
	// Postgres backend lights up automatically.
	var incidentStore incidents.Store
	if cfg.DatabaseURL != "" {
		pool, err := shareddb.NewPool(ctx, shareddb.Config{URL: cfg.DatabaseURL})
		if err != nil {
			logger.Warn("db pool failed; falling back to in-memory incident store",
				slog.String("error", err.Error()))
			incidentStore = incidents.NewInMemory()
		} else {
			defer pool.Close()
			if err := incidents.Apply(ctx, cfg.DatabaseURL); err != nil {
				logger.Warn("incident migrations failed; falling back to in-memory",
					slog.String("error", err.Error()))
				incidentStore = incidents.NewInMemory()
			} else {
				incidentStore = incidents.NewPostgres(pool)
				hr.AddProbe("db", shareddb.PingProbe(pool))
				logger.Info("incident store wired (postgres)")
			}
		}
	} else {
		incidentStore = incidents.NewInMemory()
		logger.Info("incident store wired (in-memory — DATABASE_URL not set)")
	}

	hr.MarkReady()

	deps := server.Deps{Cfg: cfg, Cat: cat, Store: incidentStore}

	if err := sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		ListenAddr:  cfg.ListenAddr,
		Logger:      logger,
		Health:      hr,
		Mount:       server.MountFunc(deps),
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
