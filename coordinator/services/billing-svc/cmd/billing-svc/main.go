// Command billing-svc is the iogrid coordinator entrypoint for the
// billing-svc microservice.
//
// Customer subscriptions (Stripe), provider payouts (Stripe Connect),
// metering aggregation (NATS JetStream → Postgres), Solana payout/burn
// loop (daily cron), quarterly 1099 generation.
//
// The binary boots a chi router on $LISTEN_ADDR (default :8080) with
// /healthz, /readyz, /metrics, and the service-specific routes mounted
// via internal/server. Structured logging (slog/JSON), OpenTelemetry
// tracing (OTLP/gRPC), and graceful shutdown on SIGINT/SIGTERM are wired
// up by the shared bootstrap package.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/metering"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/solana"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/stripeapi"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/tax"
	"github.com/iogrid/iogrid/coordinator/shared/db"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "billing-svc"
	serviceVersion = "0.1.0"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting billing-svc", slog.String("version", serviceVersion))

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

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Postgres pool — shared infra package handles DSN + pooling.
	dbCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pool, err := db.NewPool(dbCtx, db.Config{URL: cfg.DatabaseURL})
	if err != nil {
		logger.Error("db pool failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	st := store.New(pool)

	// Stripe — boots regardless of cfg.StripeEnabled so routes return
	// 503 with a clear message when the key is missing.
	stripeSvc := stripeapi.New(cfg, st)

	// Solana — stub mode when GRID_TOKEN_MINT_ADDRESS empty.
	solSvc, err := solana.New(cfg, st, logger)
	if err != nil {
		logger.Error("solana setup failed (continuing in stub mode)",
			slog.String("error", err.Error()))
	}
	// In-process daily cron (off by default; on when DAILY_PAYOUT_ENABLED=true).
	// Production deployments typically use the k8s CronJob from the chart;
	// the in-process loop is for single-binary dev / Phase-0 demos.
	if solSvc != nil {
		solSvc.StartDailyCron(ctx)
	}

	// Tax generator — always available; no external deps.
	taxGen := tax.New(st)

	// Metering consumer — only runs when NATS_URL configured.
	if cfg.NATSURL != "" {
		go func() {
			cons, err := metering.NewConsumer(ctx, cfg.NATSURL, st, logger)
			if err != nil {
				logger.Error("metering consumer setup failed",
					slog.String("error", err.Error()))
				return
			}
			defer cons.Close()
			if err := cons.Run(ctx); err != nil {
				logger.Error("metering consumer exited",
					slog.String("error", err.Error()))
			}
		}()
	} else {
		logger.Warn("NATS_URL not set — metering consumer disabled")
	}

	hr := health.New()
	hr.AddProbe("db", db.PingProbe(pool))
	hr.MarkReady()

	deps := server.Deps{
		Store:  st,
		Stripe: stripeSvc,
		Solana: solSvc,
		Tax:    taxGen,
	}

	if err := sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		ListenAddr:  cfg.ListenAddr,
		Logger:      logger,
		Health:      hr,
		Mount:       server.Mount(deps),
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
