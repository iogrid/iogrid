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
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp/moonpay"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/offramp/sociable_cash"
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

	// Off-ramp adapter registry. Per env OFFRAMP_PROVIDERS, register
	// each adapter we ship. Adapter construction may fail (missing
	// credentials) — we log and skip rather than crashing the service.
	offRampSvc := buildOffRampService(cfg, st, logger)

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
		Store:   st,
		Stripe:  stripeSvc,
		Solana:  solSvc,
		Tax:     taxGen,
		OffRamp: offRampSvc,
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

// buildOffRampService constructs the off-ramp adapter registry from
// configured env vars. Returns nil when no providers are registered;
// the routes layer responds 503 in that case.
//
// Adapter construction failures (e.g. MoonPay enabled but
// MOONPAY_API_KEY missing) are logged and the adapter is skipped — the
// rest of billing-svc continues to function so a misconfigured off-ramp
// doesn't break Stripe/Solana paths.
func buildOffRampService(cfg *config.Config, st *store.Store, logger *slog.Logger) *offramp.Service {
	if len(cfg.OffRampProviders) == 0 {
		logger.Info("offramp: OFFRAMP_PROVIDERS empty — routes return 503")
		return nil
	}
	reg := offramp.NewRegistry()
	for _, name := range cfg.OffRampProviders {
		switch name {
		case moonpay.ProviderName:
			a, err := moonpay.New(moonpay.Config{
				APIKey:        cfg.MoonPayAPIKey,
				WebhookSecret: cfg.MoonPayWebhookSecret,
				BaseURL:       cfg.MoonPayBaseURL,
			})
			if err != nil {
				logger.Error("offramp: moonpay disabled", slog.String("error", err.Error()))
				continue
			}
			if err := reg.Register(a); err != nil {
				logger.Error("offramp: moonpay register failed", slog.String("error", err.Error()))
			}
		case sociable_cash.ProviderName:
			a, err := sociable_cash.New(sociable_cash.Config{
				WebhookSecret: cfg.CashWebhookSecret,
				BaseURL:       cfg.CashBaseURL,
			})
			if err != nil {
				logger.Error("offramp: sociable-cash disabled", slog.String("error", err.Error()))
				continue
			}
			if err := reg.Register(a); err != nil {
				logger.Error("offramp: sociable-cash register failed", slog.String("error", err.Error()))
			}
		default:
			logger.Warn("offramp: unknown provider in OFFRAMP_PROVIDERS",
				slog.String("provider", name))
		}
	}
	if len(reg.ListAvailable()) == 0 {
		logger.Warn("offramp: no adapters registered after parsing OFFRAMP_PROVIDERS")
		return nil
	}
	logger.Info("offramp: registry ready",
		slog.Int("providers", len(reg.ListAvailable())))
	return offramp.NewService(reg, st, logger)
}
