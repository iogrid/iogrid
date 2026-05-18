// Package server bundles the boilerplate every iogrid coordinator service
// needs: chi router + otelhttp instrumentation + prometheus /metrics +
// health/readyz handlers + graceful shutdown on SIGINT/SIGTERM.
//
// Services keep their business endpoints on the same router by passing a
// Mount callback to Run().
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/iogrid/iogrid/coordinator/shared/health"
)

// Options configure the bundled server.
type Options struct {
	// ServiceName is logged on every line and used as the otelhttp span
	// name prefix.
	ServiceName string
	// ListenAddr defaults to :8080. Override via LISTEN_ADDR env.
	ListenAddr string
	// Logger is used for startup / shutdown lines.
	Logger *slog.Logger
	// Mount, if set, is called with the chi router after /healthz,
	// /readyz, /metrics are wired up so the service can attach its own
	// routes.
	Mount func(r chi.Router)
	// Health is the registry used for /healthz and /readyz. If nil, a
	// fresh registry is created and marked Ready immediately.
	Health *health.Registry
	// LongLivedStreams, when true, disables the request Timeout middleware
	// and the WriteTimeout / ReadTimeout on the http.Server so that
	// Connect bidi / server-stream RPCs can stay open for minutes-to-hours
	// (heartbeats, dispatch, audit feeds).
	LongLivedStreams bool
}

// Run starts the HTTP server and blocks until SIGINT/SIGTERM. Returns the
// first error encountered during shutdown (nil on clean exit).
func Run(ctx context.Context, opts Options) error {
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	addr := opts.ListenAddr
	if addr == "" {
		addr = os.Getenv("LISTEN_ADDR")
	}
	if addr == "" {
		addr = ":8080"
	}

	hr := opts.Health
	if hr == nil {
		hr = health.New()
		hr.MarkReady()
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	if !opts.LongLivedStreams {
		r.Use(middleware.Timeout(60 * time.Second))
	}

	r.Get("/healthz", hr.Healthz)
	r.Get("/readyz", hr.Readyz)
	r.Handle("/metrics", promhttp.Handler())

	if opts.Mount != nil {
		opts.Mount(r)
	}

	handler := otelhttp.NewHandler(r, opts.ServiceName,
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
	)

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	if !opts.LongLivedStreams {
		srv.ReadTimeout = 30 * time.Second
		srv.WriteTimeout = 60 * time.Second
	}

	// Listen for SIGINT/SIGTERM and trigger graceful shutdown.
	shutdownCtx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server starting", slog.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-shutdownCtx.Done():
		log.Info("shutdown signal received, draining connections")
	}

	drainCtx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		log.Error("graceful shutdown failed", slog.String("error", err.Error()))
		return err
	}
	log.Info("http server stopped cleanly")
	return nil
}
