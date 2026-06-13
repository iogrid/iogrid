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

	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/db"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/forwarder"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/handlers"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
	shareddb "github.com/iogrid/iogrid/coordinator/shared/db"
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

	// Store selection (#771). The poll-dispatch path (#705) is split-brain
	// across replicas with the in-memory store: a long iOS build's
	// terminal-status POST can land on a different replica than the one that
	// created the assignment → GetAssignment 404 → the build-gateway
	// ForwardStatus never fires → the build stays "running" and metering /
	// $GRID settle never run (ping's #770 Ping.app built but never settled).
	//
	// DATABASE_URL set → Postgres-backed store (shared `workloads` database in
	// the CNPG cluster — every replica reads the same assignments) + embedded
	// migrations. DATABASE_URL empty → in-memory store (unit tests, local dev)
	// with a WARN, because multi-replica + in-memory is exactly the bug.
	var st store.Store
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL != "" {
		pool, perr := shareddb.NewPool(ctx, shareddb.Config{URL: databaseURL})
		if perr != nil {
			logger.Error("db pool failed", slog.String("error", perr.Error()))
			os.Exit(1)
		}
		defer pool.Close()
		hr.AddProbe("db", shareddb.PingProbe(pool))

		logger.Info("running migrations", slog.String("backend", "postgres"))
		if merr := db.Apply(ctx, databaseURL); merr != nil {
			logger.Error("db migrations failed", slog.String("error", merr.Error()))
			os.Exit(1)
		}
		st = store.NewPostgres(pool)
		logger.Info("workloads store ready", slog.String("store", "postgres"))
	} else {
		st = store.NewInMemory()
		logger.Warn("workloads store ready", slog.String("store", "memory"),
			slog.String("impact", "assignments are PER-REPLICA + LOST on restart; long iOS builds' terminal status 404s across replicas (#771) — set DATABASE_URL for prod"))
	}
	hr.MarkReady()

	disp := dispatcher.New(st, logger)

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

	// BUILD_GATEWAY_INTERNAL_URL is the build-gateway internal base URL
	// (e.g. http://build-gateway:8080). When set, iOS-build status updates
	// the daemon reports are forwarded to the build-gateway so the
	// customer-facing build record advances. BUILD_GATEWAY_DISPATCH_TOKEN is
	// the shared secret guarding the gateway's internal routes (must match
	// the gateway's BUILD_GATEWAY_DISPATCH_TOKEN). Empty == forwarding off.
	buildGatewayURL := os.Getenv("BUILD_GATEWAY_INTERNAL_URL")
	buildGatewayFwd := handlers.NewHTTPBuildGatewayForwarder(
		buildGatewayURL,
		os.Getenv("BUILD_GATEWAY_DISPATCH_TOKEN"),
	)
	if buildGatewayURL != "" {
		logger.Info("build-gateway status forwarding enabled",
			slog.String("build_gateway_url", buildGatewayURL))
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
			Store:                    st,
			Dispatcher:               disp,
			Log:                      logger,
			ProviderEndpointTemplate: providerEndpoint,
			BuildGateway:             buildGatewayFwd,
		}),
		LongLivedStreams: true,
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
