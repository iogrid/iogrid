// Command proxy-gateway is the iogrid coordinator entrypoint for the
// customer-facing SOCKS5 / HTTP CONNECT proxy at proxy.iogrid.org:443.
//
// Two listeners run in parallel:
//
//   - The proxy listener on $LISTEN_ADDR (default :443) terminates TLS
//     (when TLS_CERT_PATH / TLS_KEY_PATH are set) and disambiguates
//     SOCKS5 vs HTTP CONNECT off the first byte of the byte stream.
//   - The HTTP listener on $HEALTH_ADDR (default :8080) serves /healthz,
//     /readyz, /metrics, and the legacy /v1 service-discovery probe.
//
// Upstream services are reached over Connect-RPC:
//
//   - antiabuse-svc — CheckUrl pre-flight on every customer destination
//   - workloads-svc — SubmitWorkload returns the chosen provider's
//     tunnel endpoint + short-lived session token
//   - billing-svc — ValidateApiKey (Static fallback until the RPC lands)
//
// Sticky-session state lives in Redis; NATS JetStream receives audit
// (legal retention, AUDIT stream) + metering (BILLING stream) events.
//
// All optional dependencies fall back to safe in-memory / slog
// implementations so local-dev + unit tests need no infrastructure.
package main

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/abuse"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/audit"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/dispatch"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/proxy"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/sessions"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	"github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "proxy-gateway"
	serviceVersion = "0.0.1"
)

func main() {
	logger := log.Setup(serviceName)
	logger.Info("starting proxy-gateway",
		slog.String("version", serviceVersion),
	)

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	otelShutdown, err := otel.Setup(rootCtx, serviceName, serviceVersion)
	if err != nil {
		logger.Error("otel setup failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = otelShutdown(shutdownCtx)
	}()

	cfg := config.Load()
	srv, cleanup, err := build(rootCtx, cfg, logger)
	if err != nil {
		logger.Error("proxy-gateway build failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer cleanup()

	hr := health.New()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := srv.ListenAndServe(rootCtx); err != nil {
			logger.Error("proxy listener exited", slog.String("error", err.Error()))
			stop()
		}
	}()

	hr.MarkReady()

	// HTTP control plane (health, metrics, OpenAPI hint).
	healthAddr := cfg.HealthAddr
	if healthAddr == "" {
		healthAddr = ":8080"
	}
	if err := sharedserver.Run(rootCtx, sharedserver.Options{
		ServiceName: serviceName,
		ListenAddr:  healthAddr,
		Logger:      logger,
		Health:      hr,
		Mount:       server.Mount,
	}); err != nil {
		logger.Error("control-plane server exited with error", slog.String("error", err.Error()))
	}

	// Tear down proxy listener if control plane exited first.
	_ = srv.Close()
	wg.Wait()
}

// build wires every dependency into a *proxy.Server.
func build(ctx context.Context, cfg config.Config, logger *slog.Logger) (*proxy.Server, func(), error) {
	var cleanups []func()
	addCleanup := func(f func()) { cleanups = append(cleanups, f) }
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	// Audit + billing emitter (NATS JetStream w/ slog fallback).
	emitter := audit.New(ctx, audit.Options{
		NATSURL: cfg.NATSURL,
		Logger:  logger,
	})
	addCleanup(emitter.Close)

	// Session store: Redis if REDIS_URL set, else in-memory.
	var sessStore sessions.Store
	if cfg.RedisURL != "" {
		opt, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			logger.Warn("REDIS_URL parse failed; falling back to in-memory sessions",
				slog.String("error", err.Error()))
			sessStore = sessions.NewMemory(cfg.SessionTTL)
		} else {
			rdb := redis.NewClient(opt)
			addCleanup(func() { _ = rdb.Close() })
			sessStore = sessions.NewRedis(rdb, cfg.SessionTTL)
		}
	} else {
		logger.Info("REDIS_URL unset; using in-memory sticky-session store")
		sessStore = sessions.NewMemory(cfg.SessionTTL)
	}

	// Antiabuse client (allow-everything stub when ANTIABUSE_SVC_URL unset).
	var filter abuse.Filter
	if cfg.AntiabuseSvcURL != "" {
		hc := &http.Client{Timeout: 5 * time.Second}
		filter = abuse.NewConnectFilter(cfg.AntiabuseSvcURL, hc)
	} else {
		logger.Warn("ANTIABUSE_SVC_URL unset; pre-flight checks bypassed (DEV ONLY)")
		filter = &abuse.StaticFilter{Verdict: abuse.Verdict{Decision: abuse.DecisionAllow, Reason: "allow_dev"}}
	}

	// Workloads dispatcher.
	var disp dispatch.Dispatcher
	if cfg.WorkloadsSvcURL != "" {
		hc := &http.Client{Timeout: cfg.DialTimeout}
		disp = dispatch.NewConnectDispatcher(cfg.WorkloadsSvcURL, hc)
	} else {
		logger.Warn("WORKLOADS_SVC_URL unset; using in-memory static provider pool (DEV ONLY)")
		// One entry from DEV_PROVIDER_ENDPOINT env so the binary
		// can be smoke-tested end-to-end on a developer's box.
		entries := []dispatch.ProviderEntry{}
		if endpoint := os.Getenv("DEV_PROVIDER_ENDPOINT"); endpoint != "" {
			entries = append(entries, dispatch.ProviderEntry{
				ID:       "dev-provider",
				Endpoint: endpoint,
				Online:   true,
				Token:    "dev-token",
			})
		}
		disp = dispatch.NewStaticPool(entries)
	}

	// Validator: Connect to billing-svc when BILLING_SVC_URL is set.
	// Falls back to an in-memory Static seeded from DEV_API_KEYS for
	// local dev and tests — BILLING_SVC_URL must be set in production.
	var validator auth.Validator
	if cfg.BillingSvcURL != "" {
		validator = auth.NewConnect(cfg.BillingSvcURL, &http.Client{Timeout: 3 * time.Second})
		logger.Info("billing-svc ValidateApiKey client wired",
			slog.String("url", cfg.BillingSvcURL))
	} else {
		logger.Warn("BILLING_SVC_URL unset; using static API key store from DEV_API_KEYS (DEV ONLY)")
		validator = auth.NewStatic(parseDevAPIKeys(os.Getenv("DEV_API_KEYS")))
	}

	// TLS (optional).
	var tlsCfg *tls.Config
	if cfg.TLSEnabled() {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertPath, cfg.TLSKeyPath)
		if err != nil {
			return nil, cleanup, err
		}
		tlsCfg = &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}
	}

	srv := proxy.New(cfg, logger)
	srv.Validator = validator
	srv.Filter = filter
	srv.Dispatcher = disp
	srv.Sessions = sessStore
	srv.Emitter = emitter
	srv.TLSConfig = tlsCfg
	// When the real workloads-svc Connect dispatcher is wired, the
	// assignment Endpoint points at workloads-svc's TCP-over-DispatchFrame
	// forwarder, which expects an IOGRID-TUN/1 preamble (issue #222) before
	// the customer's raw TLS bytes. In dev mode (StaticPool) the endpoint
	// is a raw TCP echo target and the preamble is OFF. Refs iogrid#279.
	srv.EnableForwarderPreamble = cfg.WorkloadsSvcURL != ""

	// Cleanup also stops the proxy server on shutdown.
	addCleanup(func() { _ = srv.Close() })
	return srv, cleanup, nil
}

// parseDevAPIKeys reads a "key1=workspace1;key2=workspace2" env string
// into the seed map. Empty input returns nil so Validator answers
// ErrInvalidKey for every credential — production deployments MUST set
// BILLING_SVC_URL.
func parseDevAPIKeys(s string) map[string]auth.Customer {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	out := map[string]auth.Customer{}
	for _, pair := range strings.Split(s, ";") {
		k, w, ok := strings.Cut(pair, "=")
		if !ok || k == "" {
			continue
		}
		out[strings.TrimSpace(k)] = auth.Customer{
			WorkspaceID: strings.TrimSpace(w),
			CustomerID:  strings.TrimSpace(w),
			Tier:        "starter",
		}
	}
	return out
}
