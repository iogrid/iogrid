// Command identity-svc is the iogrid coordinator entrypoint for the identity-svc microservice.
//
// Google OAuth + magic-link issuance + identity merging + JWT issuance.
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

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "identity-svc"
	serviceVersion = "0.0.1"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting identity-svc",
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

	if err := sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		Logger:      logger,
		Health:      hr,
		Mount:       server.Mount,
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
