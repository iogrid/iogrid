// Command providers-svc is the iogrid coordinator entrypoint for the providers-svc microservice.
//
// Provider registration, capability inventory, scheduling state, transparency dashboard backend.
//
// The binary boots a chi router on $LISTEN_ADDR (default :8080) with
// /healthz, /readyz, /metrics, and the service-specific routes mounted via
// internal/server. Structured logging (slog/JSON), OpenTelemetry tracing
// (OTLP/gRPC), and graceful shutdown on SIGINT/SIGTERM are wired up by the
// shared bootstrap package.
//
// Store selection
//
// When DATABASE_URL is set (production / cluster), we open a pgxpool, run
// embedded goose migrations, and use the Postgres-backed store. When it is
// empty we keep the in-memory store — unit tests and local dev have no DB
// dependency.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/ca"
	pdb "github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/db"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/transparency"
	"github.com/iogrid/iogrid/coordinator/shared/db"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "providers-svc"
	serviceVersion = "0.0.1"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting providers-svc",
		slog.String("version", serviceVersion),
	)

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

	// --- Store selection --------------------------------------------------
	// DATABASE_URL set → Postgres-backed store + embedded migrations.
	// DATABASE_URL empty → in-memory store (unit tests, local dev).
	var st store.Store
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL != "" {
		pool, err := db.NewPool(ctx, db.Config{URL: databaseURL})
		if err != nil {
			logger.Error("db pool failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer pool.Close()
		hr.AddProbe("db", db.PingProbe(pool))

		logger.Info("running migrations", slog.String("backend", "postgres"))
		if err := pdb.Apply(ctx, databaseURL); err != nil {
			logger.Error("db migrations failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		st = store.NewPostgres(pool)
		logger.Info("providers store ready", slog.String("store", "postgres"))
	} else {
		st = store.NewInMemory()
		logger.Warn("providers store ready", slog.String("store", "memory"),
			slog.String("impact", "paired daemons are LOST on pod restart; set DATABASE_URL for prod"))
	}

	internalCA, err := ca.NewInMemory()
	if err != nil {
		logger.Error("ca bootstrap failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Transparency bridge — subscribes to proxy-gateway's
	// "iogrid.audit.proxy.abuse_flagged" subject and projects every
	// blocked-by-antiabuse event into the providers-svc audit_events
	// table so the per-provider transparency feed surfaces the kill
	// switch firing in real time (issue #360). Failure to wire NATS
	// is non-fatal — the per-provider feed simply stays empty for
	// abuse events; the proxy-gateway's AUDIT stream remains the
	// legal-retention source of truth.
	bridge := &transparency.Bridge{Store: st}
	bridgeCleanup, err := transparency.Start(ctx, bridge, transparency.Options{
		NATSURL: os.Getenv("NATS_URL"),
		Logger:  logger,
	})
	if err != nil {
		logger.Warn("transparency abuse bridge: start returned error (continuing)",
			slog.String("error", err.Error()))
	}
	defer bridgeCleanup()

	hr.MarkReady()

	if err := sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		Logger:      logger,
		Health:      hr,
		Mount: server.Mount(server.Deps{
			Store: st,
			CA:    internalCA,
			Log:   logger,
		}),
		LongLivedStreams: true,
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
