// Command vpn-svc is the iogrid coordinator entrypoint for VPN session management.
//
// Session ledger, ICE candidate tracking, provider-to-customer assignment,
// regional failover logic.
//
// The binary boots a chi router on $LISTEN_ADDR (default :8080) with
// /healthz, /readyz, /metrics, and the service-specific routes mounted via
// internal/server. Structured logging (slog/JSON), OpenTelemetry tracing
// (OTLP/gRPC), and graceful shutdown on SIGINT/SIGTERM are wired up by the
// shared bootstrap package.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	vdb "github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/db"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/earnings"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/ice"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/shared/db"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "vpn-svc"
	serviceVersion = "0.1.0"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting vpn-svc",
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
		if err := vdb.Apply(ctx, databaseURL); err != nil {
			logger.Error("db migrations failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		st = store.NewPostgres(pool)
		logger.Info("vpn store ready", slog.String("store", "postgres"))
	} else {
		st = store.NewMemory()
		logger.Warn("vpn store ready", slog.String("store", "memory"),
			slog.String("impact", "sessions are LOST on pod restart; set DATABASE_URL for prod"))
	}

	// --- STUN server setup (RFC 5389) ----------------------------------------
	stunAddr := os.Getenv("STUN_LISTEN_ADDR")
	if stunAddr == "" {
		stunAddr = ":3478" // Standard STUN port
	}
	stunServer, err := ice.NewSTUNServer(stunAddr, logger)
	if err != nil {
		logger.Error("stun server setup failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Start STUN server in background
	go func() {
		if err := stunServer.Start(); err != nil {
			logger.Error("stun server error", slog.String("error", err.Error()))
		}
	}()
	defer stunServer.Close()

	// --- API key validator (#531) ------------------------------------------
	// When BILLING_SVC_URL is set, every POST /v1/vpn/sessions must carry
	// a valid api_key (validated against billing-svc.ValidateApiKey with a
	// 60s positive cache). Unset = dev/smoke mode (unauthenticated,
	// boot WARN logs).
	var validator server.APIKeyValidator
	if billingURL := os.Getenv("BILLING_SVC_URL"); billingURL != "" {
		validator = server.NewBillingValidator(billingURL, nil)
		logger.Info("api key validation enabled", slog.String("billing_url", billingURL))
	} else {
		logger.Warn("api key validation DISABLED — set BILLING_SVC_URL to enable",
			slog.String("impact", "every POST /v1/vpn/sessions is unauthenticated"))
	}

	// --- Earnings batcher (#547) -------------------------------------------
	// Periodic loop credits residential providers via the BILLING NATS
	// stream. NATS_URL empty → batcher disabled (dev/local mode), the
	// service still serves traffic but providers don't earn.
	if natsURL := os.Getenv("NATS_URL"); natsURL != "" {
		nc, err := nats.Connect(natsURL,
			nats.Name(serviceName),
			nats.MaxReconnects(-1))
		if err != nil {
			logger.Error("nats connect failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		defer nc.Close()
		batcher, err := earnings.New(earnings.Config{
			Store:     st,
			Publisher: &earnings.NATSPublisher{NC: nc},
			Logger:    logger.With(slog.String("subsystem", "earnings")),
		})
		if err != nil {
			logger.Error("earnings batcher setup failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		go func() {
			if err := batcher.Run(ctx); err != nil && err != context.Canceled {
				logger.Warn("earnings batcher exited", slog.String("error", err.Error()))
			}
		}()
		logger.Info("earnings batcher started", slog.String("nats_url", natsURL))
	} else {
		logger.Warn("earnings batcher DISABLED — set NATS_URL to enable",
			slog.String("impact", "providers earn nothing from VPN bytes"))
	}

	// --- HTTP server setup + run -------------------------------------------------
	hr.MarkReady()
	err = sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		Logger:      logger,
		Health:      hr,
		Mount: func(r chi.Router) {
			if err := server.Mount(r, st, logger, validator); err != nil {
				logger.Error("server mount failed", slog.String("error", err.Error()))
				os.Exit(1)
			}
		},
	})
	if err != nil {
		logger.Error("http server error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
