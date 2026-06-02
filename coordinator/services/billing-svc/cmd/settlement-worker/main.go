// Command settlement-worker is the long-running cron that batches
// unsettled $GRID rows into on-chain SPL transfers every 5 minutes.
//
// Refs iogrid/iogrid#598 (Track 5 / EPIC #581).
//
// Boot contract
// =============
//
//   DATABASE_URL              required (same database as billing-svc)
//   GRID_TOKEN_MINT_ADDRESS   required (must match billing-svc env)
//   SOLANA_RPC_URL            required (Helius free-tier endpoint usually)
//   TREASURY_PRIVATE_KEY_PATH required (path to Solana CLI keypair JSON)
//   SETTLEMENT_TICK           optional (default 5m, format: time.ParseDuration)
//   SETTLEMENT_BATCH_LIMIT    optional (default 500)
//
// Lifecycle: starts a chi router on $LISTEN_ADDR (default :8080) exposing
// /healthz + /readyz + /metrics; the actual settlement loop runs in a
// background goroutine. SIGINT/SIGTERM trigger graceful shutdown — the
// in-flight tick is allowed to complete.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/grid"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/solana"
	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/shared/db"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
)

const (
	serviceName    = "settlement-worker"
	serviceVersion = "0.1.0"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting settlement-worker",
		slog.String("version", serviceVersion))

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Postgres pool — shared db helper.
	dbCtx, dbCancel := context.WithTimeout(ctx, 15*time.Second)
	defer dbCancel()
	pool, err := db.NewPool(dbCtx, db.Config{URL: cfg.DatabaseURL})
	if err != nil {
		logger.Error("db pool failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()

	// We don't run migrations here — billing-svc owns the schema. We
	// trust it to have applied 0006_grid_settlement.sql before the worker
	// boots (same release cycle).
	gridStore := grid.NewPostgresStore(pool)
	billingStore := store.New(pool)

	// Solana service — same construction as billing-svc.
	solSvc, err := solana.New(cfg, billingStore, logger)
	if err != nil {
		logger.Error("solana setup failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if !solSvc.Enabled() {
		logger.Error("solana stub mode — settlement-worker has nothing to do; configure GRID_TOKEN_MINT_ADDRESS + SOLANA_HOT_WALLET_KEYPAIR_PATH")
		os.Exit(1)
	}
	logger.Info("solana live",
		slog.String("treasury", solSvc.WalletAddress()),
		slog.String("mint", cfg.GRIDTokenMint))

	tick := 5 * time.Minute
	if t := os.Getenv("SETTLEMENT_TICK"); t != "" {
		if d, err := time.ParseDuration(t); err == nil {
			tick = d
		} else {
			logger.Warn("invalid SETTLEMENT_TICK; defaulting to 5m",
				slog.String("value", t))
		}
	}
	batchLimit := 500
	if b := os.Getenv("SETTLEMENT_BATCH_LIMIT"); b != "" {
		if n, err := strconv.Atoi(b); err == nil && n > 0 {
			batchLimit = n
		}
	}

	cron := &grid.SettlementCron{
		Store:      gridStore,
		Solana:     solSvc,
		Metrics:    grid.NewPromMetrics(),
		Logger:     logger.With(slog.String("subsystem", "settlement-cron")),
		Tick:       tick,
		BatchLimit: batchLimit,
		Alerter: func(ctx context.Context, body string) {
			// Production: chepherd.alert_human goes here. For now we just
			// log at ERROR so the alertmanager rule on `level=ERROR
			// subsystem=settlement-cron` catches it.
			logger.Error("settlement alert",
				slog.String("subsystem", "settlement-cron"),
				slog.String("alert", body))
		},
	}

	go func() {
		if err := cron.Run(ctx); err != nil && err != context.Canceled {
			logger.Error("settlement cron stopped",
				slog.String("error", err.Error()))
		}
	}()

	// Health surface — basic; not a full Connect-RPC server.
	hr := health.New()
	hr.AddProbe("db", db.PingProbe(pool))
	hr.MarkReady()

	mux := chi.NewRouter()
	mux.Get("/healthz", hr.Healthz)
	mux.Get("/readyz", hr.Readyz)
	mux.Post("/admin/settle-now", func(w http.ResponseWriter, r *http.Request) {
		if err := cron.RunOnce(r.Context()); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	addr := cfg.ListenAddr
	if addr == "" {
		addr = ":8080"
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		logger.Info("health/admin server listening", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", slog.String("error", err.Error()))
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received")
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	logger.Info("settlement-worker stopped")
}
