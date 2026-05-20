// Command workloads-svc is the iogrid coordinator entrypoint for the workloads-svc microservice.
//
// Customer workload submission, scheduling, dispatch, retry/failover, result delivery.
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

	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/forwarder"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "workloads-svc"
	serviceVersion = "0.0.1"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting workloads-svc",
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

	// In-memory store + dispatcher for the local-dev binary. The
	// pg-backed store lives behind the `postgres` build tag — see
	// internal/store/store_pg.go (TBD) and ../identity-svc for the
	// migration pattern.
	memStore := store.NewInMemory()
	disp := dispatcher.New(memStore, logger)

	// WORKLOADS_SVC_PROVIDER_ENDPOINT is the host:port the
	// proxy-gateway should dial when forwarding customer bytes to any
	// daemon connected via this workloads-svc replica. In the Phase 0
	// NAT-bound layout that's the workloads-svc TCP-over-DispatchFrame
	// forwarder's own listener address. Empty == off (proxy-gateway
	// uses its DEV_PROVIDER_ENDPOINT static pool). See issue #217.
	providerEndpoint := os.Getenv("WORKLOADS_SVC_PROVIDER_ENDPOINT")
	if providerEndpoint != "" {
		logger.Info("provider endpoint template configured",
			slog.String("endpoint", providerEndpoint))
	}

	// FORWARDER_LISTEN_ADDR controls the TCP-over-DispatchFrame
	// forwarder's bind address (issue #222). Defaults to ":9091".
	// When the env var "WORKLOADS_SVC_PROVIDER_ENDPOINT" is empty we
	// still start the forwarder, but the EndpointHint advertised in
	// assignments will be empty (proxy-gateway falls back to its dev
	// pool). The two are intentionally separate: the listener may
	// bind to ":9091" while the publicly-routable endpoint is the
	// Kubernetes Service DNS name.
	//
	// The default moved from ":9090" to ":9091" in the PR for #267 —
	// port 9090 was colliding with the Prometheus /metrics listener
	// exposed by the shared bootstrap, causing whichever side won the
	// boot race to silently shadow the other. Metrics stay on 9090,
	// the forwarder lives on 9091.
	forwarderAddr := os.Getenv("FORWARDER_LISTEN_ADDR")
	if forwarderAddr == "" {
		forwarderAddr = ":9091"
	}
	fwd := forwarder.New(forwarder.Options{
		ListenAddr: forwarderAddr,
		Dispatcher: disp,
		Log:        logger,
	})
	if _, err := fwd.Start(ctx); err != nil {
		logger.Error("forwarder start failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() { _ = fwd.Close() }()

	if err := sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		Logger:      logger,
		Health:      hr,
		Mount: server.Mount(server.Deps{
			Store:                    memStore,
			Dispatcher:               disp,
			Log:                      logger,
			ProviderEndpointTemplate: providerEndpoint,
		}),
		LongLivedStreams: true,
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
