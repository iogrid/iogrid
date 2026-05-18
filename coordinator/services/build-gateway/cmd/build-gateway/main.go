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
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
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
	st := store.NewInMemory(nil)
	disp := workloadclient.NewInMemory(nil)
	storage := s3artifact.NewInMemory(nil, os.Getenv("BUILD_GATEWAY_S3_ENDPOINT"))
	hub := builds.NewLogHub(2048)
	webhookDisp := webhook.NewAsyncDispatcher(ctx, logger, 4, 256)

	svc := builds.NewService(builds.Options{
		Store:      st,
		Dispatcher: disp,
		Storage:    storage,
		Webhooks:   webhookDisp,
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
		static.Add(key, auth.Identity{
			WorkspaceID: ws,
			Plan:        plan,
		})
		logger.Info("static api key registered",
			slog.String("workspace_id", ws),
			slog.String("plan", plan),
		)
	} else {
		logger.Warn("no static api key configured; every customer request will 401 until billing-svc wiring lands")
	}
	return auth.NewCachingValidator(static, 0)
}
