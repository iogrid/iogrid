// Command gateway-bff is the iogrid coordinator entrypoint for the
// gateway-bff microservice — Backend-for-Frontend for the Next.js
// management plane (aggregation + SSE streaming).
//
// The binary boots a chi router on $LISTEN_ADDR (default :8080) with
// /healthz, /readyz, /metrics, /v1/, and the /api/v1/* customer-facing
// surface mounted via internal/server. Structured logging (slog/JSON),
// OpenTelemetry tracing (OTLP/gRPC), and graceful shutdown on
// SIGINT/SIGTERM are wired up by the shared bootstrap package.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/handlers"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/ratelimit"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/server"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "gateway-bff"
	serviceVersion = "0.0.1"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting gateway-bff", slog.String("version", serviceVersion))

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", slog.String("error", err.Error()))
		os.Exit(1)
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

	// Single shared HTTP client for all downstream Connect clients —
	// connection pooling, TLS session reuse.
	httpClient := &http.Client{Timeout: cfg.DownstreamTimeout}
	clientSet := clients.New(clients.Config{
		IdentityURL:  cfg.IdentitySvcURL,
		ProvidersURL: cfg.ProvidersSvcURL,
		WorkloadsURL: cfg.WorkloadsSvcURL,
		AntiabuseURL: cfg.AntiAbuseSvcURL,
		BillingURL:   cfg.BillingSvcURL,
		Timeout:      cfg.DownstreamTimeout,
		Retries:      cfg.DownstreamRetries,
	}, httpClient)

	// JWKS resolver — caches identity-svc's RSA public key set and
	// rotates on the configured interval.
	resolver := auth.NewJWKSResolver(cfg.JWKSURL, cfg.JWKSRefreshInterval, httpClient, logger)
	verifier := &auth.JWTVerifier{
		Resolver: resolver,
		Issuer:   cfg.JWTIssuer,
		Audience: cfg.JWTAudience,
	}

	// Per-user + per-IP token-bucket limiters.
	authedLim := ratelimit.New(cfg.AuthedRatePerSec, cfg.AuthedBurst, 5*time.Minute)
	anonLim := ratelimit.New(cfg.AnonymousRatePerSec, cfg.AnonymousBurst, 5*time.Minute)
	go reaperLoop(ctx, authedLim, anonLim, logger)

	// In-memory API key store. Will be swapped for a Postgres-backed
	// store once the corresponding proto + identity-svc table land.
	apiKeyStore := handlers.NewMemoryAPIKeyStore()

	hr := health.New()
	hr.MarkReady()

	deps := server.Deps{
		Config:        cfg,
		Clients:       clientSet,
		Verifier:      verifier,
		APIKeyStore:   apiKeyStore,
		AuthedLimiter: authedLim,
		AnonLimiter:   anonLim,
		Logger:        logger,
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

// reaperLoop periodically reaps idle limiter buckets so the per-key
// map can't grow unbounded under churn.
func reaperLoop(ctx context.Context, a, b *ratelimit.Limiter, logger *slog.Logger) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			a.Reap()
			b.Reap()
			logger.Debug("ratelimit reap", slog.Int("authed", a.Size()), slog.Int("anon", b.Size()))
		}
	}
}
