// Command build-gateway is the iogrid coordinator entrypoint for the build-gateway microservice.
//
// Customer-facing iOS-CI gateway: receives build jobs, schedules to Mac providers.
//
// The binary boots a chi router on $LISTEN_ADDR (default :8080) with
// /healthz, /readyz, /metrics, and the service-specific routes mounted via
// internal/server. Structured logging (slog/JSON), OpenTelemetry tracing
// (OTLP/gRPC), and graceful shutdown on SIGINT/SIGTERM are wired up by the
// shared bootstrap package.
//
// Environment variables:
//
//	LISTEN_ADDR                     bind address, default :8080
//	BUILD_GATEWAY_DISPATCH_TOKEN    shared secret required on internal
//	                                provider-side endpoints (artifact
//	                                upload, status callbacks). Empty
//	                                disables the check (dev-only).
//	BUILD_GATEWAY_STATIC_API_KEY    when set, registers this single key
//	                                in the in-memory validator. Stops
//	                                the gateway from being a wide-open
//	                                test fixture if the deployment is
//	                                missing a billing-svc wiring.
//	BUILD_GATEWAY_STATIC_WORKSPACE  workspace id paired with
//	                                BUILD_GATEWAY_STATIC_API_KEY.
//	BUILD_GATEWAY_STATIC_PLAN       plan (free / pro / enterprise),
//	                                default "free".
//	BUILD_GATEWAY_S3_ENDPOINT       synthetic S3 endpoint used in
//	                                pre-signed URL output (the real S3
//	                                client is plugged in once Hetzner
//	                                credentials are wired).
//	WORKLOADS_SVC_URL               when set, the gateway dispatches builds
//	                                to workloads-svc over Connect-Go (the
//	                                REAL provider path) instead of the
//	                                in-memory test dispatcher.
//	DATABASE_URL                    when set, builds are persisted to
//	                                Postgres so they survive a pod restart;
//	                                empty keeps the in-memory store (unit
//	                                tests / local dev).
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/gridsettle"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/s3artifact"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/webhook"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/workloadclient"

	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "build-gateway"
	serviceVersion = "0.0.1"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting build-gateway",
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
	hr.MarkReady()

	// Wire dependencies. Every concrete impl here is the development
	// default; production overrides land via env-driven config (S3
	// credentials, billing-svc Connect-Go client, NATS publisher).

	// Store: Postgres when DATABASE_URL is set (builds survive pod
	// restarts), otherwise the in-memory default for unit tests / local dev.
	st := buildStore(ctx, logger)

	// Dispatcher: the REAL Connect-Go client to workloads-svc when
	// WORKLOADS_SVC_URL is set, otherwise the in-memory test dispatcher.
	disp := buildDispatcher(logger)

	storage := s3artifact.NewInMemory(nil, os.Getenv("BUILD_GATEWAY_S3_ENDPOINT"))
	hub := builds.NewLogHub(2048)
	webhookDisp := webhook.NewAsyncDispatcher(ctx, logger, 4, 256)

	// $GRID build settlement (#700/#712): POST /v1/grid/build-end to
	// billing-svc on terminal status. Empty BILLING_SVC_URL → Noop (dev).
	var gridSettler gridsettle.Settler = gridsettle.Noop{}
	if billingURL := os.Getenv("BILLING_SVC_URL"); billingURL != "" {
		gridSettler = &gridsettle.HTTPSettler{BaseURL: billingURL}
		logger.Info("grid build settlement enabled", slog.String("billing_svc", billingURL))
	}
	// Wallet resolution (#718): identity-svc internal endpoint. Empty
	// IDENTITY_SVC_URL → Noop (builds settle to no wallet = no-op).
	var walletResolver gridsettle.WalletResolver = gridsettle.NoopWalletResolver{}
	if idURL := os.Getenv("IDENTITY_SVC_URL"); idURL != "" {
		walletResolver = &gridsettle.HTTPWalletResolver{
			IdentityURL: idURL,
			Token:       os.Getenv("IDENTITY_INTERNAL_TOKEN"),
		}
		logger.Info("grid wallet resolution enabled", slog.String("identity_svc", idURL))
	}

	svc := builds.NewService(builds.Options{
		Store:      st,
		Dispatcher: disp,
		Storage:    storage,
		Webhooks:   webhookDisp,
		GridSettle: gridSettler,
		Wallets:    walletResolver,
		Logs:       hub,
		Logger:     logger,
	})

	validator := buildValidator(logger)

	mount := server.New(server.Deps{
		Service:       svc,
		Validator:     validator,
		DispatchToken: os.Getenv("BUILD_GATEWAY_DISPATCH_TOKEN"),
		Logger:        logger,
	})

	if err := sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		Logger:      logger,
		Health:      hr,
		Mount:       mount,
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// buildStore selects the persistence backend. DATABASE_URL set → Postgres
// (durable across pod restarts); empty → in-memory (unit tests / local dev).
// A Postgres connection failure is fatal: silently falling back to in-memory
// would lose builds on the next restart without anyone noticing.
func buildStore(ctx context.Context, logger *slog.Logger) builds.Store {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Warn("no DATABASE_URL set; using in-memory build store",
			slog.String("impact", "builds are LOST on pod restart; set DATABASE_URL for prod"))
		return store.NewInMemory(nil)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		logger.Error("postgres pool create failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	pg := store.NewPostgres(pool, nil)
	if err := pg.EnsureSchema(ctx); err != nil {
		logger.Error("postgres schema ensure failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("postgres build store ready")
	return pg
}

// buildDispatcher selects the workloads-svc dispatch backend.
// WORKLOADS_SVC_URL set → real Connect-Go client; empty → in-memory test
// dispatcher (which never reaches a provider — dev/test only).
func buildDispatcher(logger *slog.Logger) workloadclient.Dispatcher {
	url := os.Getenv("WORKLOADS_SVC_URL")
	if url == "" {
		logger.Warn("no WORKLOADS_SVC_URL set; using in-memory dispatcher",
			slog.String("impact", "builds are accepted but NEVER reach a Mac provider; set WORKLOADS_SVC_URL for prod"))
		return workloadclient.NewInMemory(nil)
	}
	logger.Info("dispatching builds to workloads-svc over Connect-Go",
		slog.String("workloads_svc_url", url))
	return workloadclient.NewConnect(url, nil)
}

// buildValidator wires the API-key validator. Production substitutes a
// billing-svc Connect-Go client; until then, an env-driven static
// validator gives us a single working key for smoke tests.
func buildValidator(logger *slog.Logger) auth.Validator {
	static := auth.NewStaticValidator()
	if key := os.Getenv("BUILD_GATEWAY_STATIC_API_KEY"); key != "" {
		ws := os.Getenv("BUILD_GATEWAY_STATIC_WORKSPACE")
		if ws == "" {
			ws = "default-workspace"
		}
		plan := os.Getenv("BUILD_GATEWAY_STATIC_PLAN")
		if plan == "" {
			plan = "free"
		}
		// UserID is the submitter the build is attributed to. Without it the
		// service skips wallet resolution (Submit: `userID != ""`), so $GRID
		// settlement no-ops — the static key must carry a real user for the
		// dog-food to settle (#718/#740).
		userID := os.Getenv("BUILD_GATEWAY_STATIC_USER")
		static.Add(key, auth.Identity{
			WorkspaceID: ws,
			UserID:      userID,
			Plan:        plan,
		})
		logger.Info("static api key registered",
			slog.String("workspace_id", ws),
			slog.String("user_id", userID),
			slog.String("plan", plan),
		)
	} else {
		logger.Warn("no static api key configured; every customer request will 401 until billing-svc wiring lands")
	}
	return auth.NewCachingValidator(static, 0)
}
