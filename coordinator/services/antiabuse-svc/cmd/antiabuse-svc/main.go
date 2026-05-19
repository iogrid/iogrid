// Command antiabuse-svc is the iogrid coordinator entrypoint for the antiabuse-svc microservice.
//
// Pre-flight filtering (CSAM, fraud, port restrictions, rate limits), abuse detection.
//
// The binary boots a chi router on $LISTEN_ADDR (default :8080) with
// /healthz, /readyz, /metrics, and the Connect-RPC AbuseFilterService
// mounted at /iogrid.antiabuse.v1.AbuseFilterService/. Structured
// logging (slog/JSON), OpenTelemetry tracing (OTLP/gRPC), and graceful
// shutdown on SIGINT/SIGTERM are wired up by the shared bootstrap.
package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"

	"github.com/go-chi/chi/v5"
	_ "github.com/jackc/pgx/v5/stdlib" // registers "pgx" driver
	"github.com/redis/go-redis/v9"

	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/audit"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/domains"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters/gsb"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters/openphish"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters/photodna"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters/phishtank"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/handler"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/ports"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/ratelimit"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/registry"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/server"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "antiabuse-svc"
	serviceVersion = "0.0.1"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting antiabuse-svc",
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

	cfg := config.Load()
	svc, cleanup := build(ctx, cfg, logger)
	defer cleanup()

	hr := health.New()
	hr.MarkReady()

	if err := sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		Logger:      logger,
		Health:      hr,
		Mount: func(r chi.Router) {
			server.Mount(r, svc)
		},
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// build composes every dependency into the handler.Service and returns
// a cleanup func that releases background resources (NATS, Redis,
// feed-refresh goroutines).
func build(ctx context.Context, cfg config.Config, logger *slog.Logger) (*handler.Service, func()) {
	cleanups := []func(){}
	addCleanup := func(f func()) { cleanups = append(cleanups, f) }

	// Redis (optional).
	var rdb *redis.Client
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			logger.Warn("REDIS_URL parse failed; using in-memory rate limiter",
				slog.String("error", err.Error()))
		} else {
			rdb = redis.NewClient(opt)
			addCleanup(func() { _ = rdb.Close() })
		}
	}

	// Audit emitter (NATS or slog fallback).
	auditEmitter, err := audit.New(ctx, audit.Options{
		NATSURL:       cfg.NATSURL,
		Logger:        logger,
		RetentionDays: cfg.AuditRetentionDays,
	})
	if err != nil {
		logger.Warn("audit emitter init failed", slog.String("error", err.Error()))
	}
	if auditEmitter != nil {
		addCleanup(auditEmitter.Close)
	}

	// Optional Postgres mirror — only opened when DSN is set so the
	// dev-mode boot stays zero-dependency. The pgx stdlib driver was
	// linked at the top-level import.
	var auditDB *sql.DB
	if cfg.AuditPostgresDSN != "" {
		db, err := sql.Open("pgx", cfg.AuditPostgresDSN)
		if err != nil {
			logger.Warn("AUDIT_POSTGRES_DSN open failed; skipping mirror",
				slog.String("error", err.Error()))
		} else {
			auditDB = db
			addCleanup(func() { _ = db.Close() })
		}
	}

	// Background retention pruner — enforces the 90-day TTL on the
	// JetStream stream (re-asserts MaxAge) and on the Postgres mirror
	// (batched DELETE) every 24h.
	pruneCtx, pruneCancel := context.WithCancel(context.Background())
	addCleanup(pruneCancel)
	prunerOpts := audit.PrunerOptions{
		RetentionDays: cfg.AuditRetentionDays,
		DB:            auditDB,
		Logger:        logger,
	}
	if auditEmitter != nil {
		prunerOpts.Stream = auditEmitter.Stream()
		prunerOpts.JS = auditEmitter.JS()
	}
	audit.NewPruner(prunerOpts).Start(pruneCtx)

	// Reputation backends. Each one Start()s its own refresh goroutine.
	ptCtx, ptCancel := context.WithCancel(context.Background())
	addCleanup(ptCancel)
	pt := phishtank.New(phishtank.Options{
		APIKey:  cfg.PhishTankAPIKey,
		Refresh: cfg.PhishTankRefresh,
	})
	if cfg.PhishTankAPIKey == "" {
		logger.Warn("PHISHTANK_API_KEY unset; using public unauthenticated feed",
			slog.String("impact", "rate-limited to ~hourly refresh"))
	}
	pt.Start(ptCtx)

	opCtx, opCancel := context.WithCancel(context.Background())
	addCleanup(opCancel)
	op := openphish.New(openphish.Options{Refresh: cfg.OpenPhishRefresh})
	op.Start(opCtx)

	gs := gsb.New(gsb.Options{APIKey: cfg.GSBAPIKey, ClientID: "iogrid", ClientVersion: serviceVersion})
	if !gs.Enabled() {
		logger.Warn("GSB_API_KEY unset; Google Safe Browsing lookups disabled")
	}

	pd := photodna.New(photodna.Options{APIKey: cfg.PhotoDNAAPIKey, Logger: logger})
	if !pd.Enabled() {
		logger.Warn("PHOTODNA_API_KEY unset; NCMEC PhotoDNA in stub mode",
			slog.String("action", "complete NCMEC partnership and set PHOTODNA_API_KEY before Phase 1 onboarding"))
	} else {
		// Weekly refresh of NCMEC's published hash list into the
		// in-memory bloom filter. No-op in stub mode.
		pdCtx, pdCancel := context.WithCancel(context.Background())
		addCleanup(pdCancel)
		photodna.NewRefresher(pd, photodna.RefresherOptions{
			Interval: cfg.PhotoDNABloomRefresh,
			Logger:   logger,
		}).Start(pdCtx)
	}

	orch := filters.NewOrchestrator(pt, op, gs, pd)

	limiter := ratelimit.New(ratelimit.Config{
		DefaultCustomerRate:   cfg.DefaultCustomerRPS,
		PremiumCustomerRate:   cfg.PremiumCustomerRPS,
		HighValueProviderRate: cfg.HighValueProviderRPS,
		HighValueTargets:      cfg.HighValueTargets,
	}, rdb)

	domainPolicy := domains.NewDefaultPolicy()
	if len(cfg.BlockDomains) > 0 {
		domainPolicy.LoadBlocked(cfg.BlockDomains)
		logger.Info("loaded operator deny-list from BLOCK_DOMAINS",
			slog.Int("count", domainPolicy.BlockedCount()))
	}

	svc := &handler.Service{
		Domains:    domainPolicy,
		Ports:      ports.NewDefaultPolicy(),
		Limiter:    limiter,
		Reputation: orch,
		Registry:   registry.NewDefaultPolicy(),
		Audit:      auditEmitter,
	}
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
	return svc, cleanup
}
