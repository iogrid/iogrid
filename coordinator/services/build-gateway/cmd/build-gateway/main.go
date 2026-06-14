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
//	BUILD_GATEWAY_STATIC_API_KEYS   additional static keys so a dedicated
//	                                internal customer (e.g. iogrid's own
//	                                iOS-build dog-food) coexists with the
//	                                first key. One per line / ';'-separated;
//	                                each entry is key=workspace:user:plan
//	                                (user/plan optional). #806.
//	BUILD_GATEWAY_S3_ENDPOINT       synthetic S3 endpoint used in
//	                                pre-signed URL output (the real S3
//	                                client is plugged in once Hetzner
//	                                credentials are wired).
//	WORKLOADS_SVC_URL               when set, the gateway dispatches builds
//	                                to workloads-svc over Connect-Go (the
//	                                REAL provider path) instead of the
//	                                in-memory test dispatcher.
//	DATABASE_URL                    when set, builds are persisted to
//	                                Postgres so they survive a pod restart;
//	                                empty keeps the in-memory store (unit
//	                                tests / local dev).
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1/providersv1connect"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/gridsettle"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/metering"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/providerwallet"
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

	// Store: Postgres when DATABASE_URL is set (builds survive pod
	// restarts), otherwise the in-memory default for unit tests / local dev.
	st := buildStore(ctx, logger)

	// Dispatcher: the REAL Connect-Go client to workloads-svc when
	// WORKLOADS_SVC_URL is set, otherwise the in-memory test dispatcher.
	disp := buildDispatcher(logger)

	storage := s3artifact.NewInMemory(nil, os.Getenv("BUILD_GATEWAY_S3_ENDPOINT"))
	hub := builds.NewLogHub(2048)
	webhookDisp := webhook.NewAsyncDispatcher(ctx, logger, 4, 256)

	// $GRID build settlement (#700/#712): POST /v1/grid/build-end to
	// billing-svc on terminal status. Empty BILLING_SVC_URL → Noop (dev).
	var gridSettler gridsettle.Settler = gridsettle.Noop{}
	if billingURL := os.Getenv("BILLING_SVC_URL"); billingURL != "" {
		gridSettler = &gridsettle.HTTPSettler{BaseURL: billingURL}
		logger.Info("grid build settlement enabled", slog.String("billing_svc", billingURL))
	}
	// Wallet resolution (#718): identity-svc internal endpoint. Empty
	// IDENTITY_SVC_URL → Noop (builds settle to no wallet = no-op).
	var walletResolver gridsettle.WalletResolver = gridsettle.NoopWalletResolver{}
	if idURL := os.Getenv("IDENTITY_SVC_URL"); idURL != "" {
		walletResolver = &gridsettle.HTTPWalletResolver{
			IdentityURL: idURL,
			Token:       os.Getenv("IDENTITY_INTERNAL_TOKEN"),
		}
		logger.Info("grid wallet resolution enabled", slog.String("identity_svc", idURL))
	}

	// Provider payout-wallet resolution (#748): chain providers-svc GetProvider
	// (provider_id → owner_user_id) into the identity wallet resolver so a
	// finished build's settlement row carries a non-empty provider_wallet —
	// the only rows the settlement-worker drains on-chain. Empty
	// PROVIDERS_SVC_URL → Noop (provider settles to no wallet = no payout).
	var providerWalletResolver gridsettle.ProviderWalletResolver = gridsettle.NoopProviderWalletResolver{}
	if provURL := os.Getenv("PROVIDERS_SVC_URL"); provURL != "" {
		providerWalletResolver = &providerwallet.Resolver{
			Providers: providersv1connect.NewProviderRegistrationServiceClient(http.DefaultClient, provURL),
			Wallets:   walletResolver,
		}
		logger.Info("grid provider payout-wallet resolution enabled", slog.String("providers_svc", provURL))
	}

	svc := builds.NewService(builds.Options{
		Store:           st,
		Dispatcher:      disp,
		Storage:         storage,
		Webhooks:        webhookDisp,
		GridSettle:      gridSettler,
		Wallets:         walletResolver,
		ProviderWallets: providerWalletResolver,
		Metering:        buildMeteringEmitter(logger),
		Logs:            hub,
		Logger:          logger,
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

// buildStore selects the persistence backend. DATABASE_URL set → Postgres
// (durable across pod restarts); empty → in-memory (unit tests / local dev).
// A Postgres connection failure is fatal: silently falling back to in-memory
// would lose builds on the next restart without anyone noticing.
func buildStore(ctx context.Context, logger *slog.Logger) builds.Store {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Warn("no DATABASE_URL set; using in-memory build store",
			slog.String("impact", "builds are LOST on pod restart; set DATABASE_URL for prod"))
		return store.NewInMemory(nil)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		logger.Error("postgres pool create failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	pg := store.NewPostgres(pool, nil)
	if err := pg.EnsureSchema(ctx); err != nil {
		logger.Error("postgres schema ensure failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("postgres build store ready")
	return pg
}

// buildDispatcher selects the workloads-svc dispatch backend.
// WORKLOADS_SVC_URL set → real Connect-Go client; empty → in-memory test
// dispatcher (which never reaches a provider — dev/test only).
func buildDispatcher(logger *slog.Logger) workloadclient.Dispatcher {
	url := os.Getenv("WORKLOADS_SVC_URL")
	if url == "" {
		logger.Warn("no WORKLOADS_SVC_URL set; using in-memory dispatcher",
			slog.String("impact", "builds are accepted but NEVER reach a Mac provider; set WORKLOADS_SVC_URL for prod"))
		return workloadclient.NewInMemory(nil)
	}
	logger.Info("dispatching builds to workloads-svc over Connect-Go",
		slog.String("workloads_svc_url", url))
	return workloadclient.NewConnect(url, nil)
}

// natsMeteringPublisher adapts *nats.Conn to metering.Publisher. Publish +
// Flush so the caller sees a delivery error synchronously (a finished build is
// rare enough that the flush cost is irrelevant).
type natsMeteringPublisher struct{ nc *nats.Conn }

func (p *natsMeteringPublisher) Publish(subject string, data []byte) error {
	if err := p.nc.Publish(subject, data); err != nil {
		return err
	}
	return p.nc.Flush()
}

// buildMeteringEmitter selects the metering backend. NATS_URL set → publish
// per-build usage to the BILLING stream so billing-svc writes a
// provider-attributed usage_event (the row /provide earnings sums, #744);
// empty → in-memory (builds are never metered, earnings stay 0 — dev/test).
func buildMeteringEmitter(logger *slog.Logger) metering.Emitter {
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		logger.Warn("no NATS_URL set; build metering events are NOT published",
			slog.String("impact", "finished builds never write a usage_event → /provide earnings stay 0 for builds; set NATS_URL for prod"))
		return metering.NewInMemory()
	}
	nc, err := nats.Connect(natsURL,
		nats.Name("build-gateway-metering"),
		nats.MaxReconnects(-1),
	)
	if err != nil {
		logger.Error("metering NATS connect failed; builds will NOT meter",
			slog.String("error", err.Error()))
		return metering.NewInMemory()
	}
	logger.Info("build metering → NATS enabled",
		slog.String("nats_url", natsURL),
		slog.String("subject", metering.Subject))
	return &metering.NATSEmitter{Pub: &natsMeteringPublisher{nc: nc}}
}

// buildValidator wires the API-key validator. Production substitutes a
// billing-svc Connect-Go client; until then, an env-driven static
// validator gives us working keys for smoke tests + the dog-food.
//
// Two env shapes are honoured, both populating the same static map:
//
//   - BUILD_GATEWAY_STATIC_API_KEY (+ _WORKSPACE / _USER / _PLAN) — the
//     original single-key form. Unchanged; existing deployments keep working.
//   - BUILD_GATEWAY_STATIC_API_KEYS — additional keys so a dedicated internal
//     customer (e.g. iogrid's own iOS-build dog-food, #806) can coexist with
//     the first key without evicting it. One key per line (or ';'-separated);
//     each line is `key=workspace_id:user_id:plan` (user_id/plan optional,
//     default to the same fallbacks as the singular form). This is the
//     operator-provisioning seam until billing-svc CreateApiKey is wired into
//     the validator — it lets us mint a distinct workspace-scoped key without
//     a code change per customer.
func buildValidator(logger *slog.Logger) auth.Validator {
	static := auth.NewStaticValidator()
	registered := 0

	if key := os.Getenv("BUILD_GATEWAY_STATIC_API_KEY"); key != "" {
		ws := os.Getenv("BUILD_GATEWAY_STATIC_WORKSPACE")
		if ws == "" {
			ws = "default-workspace"
		}
		plan := os.Getenv("BUILD_GATEWAY_STATIC_PLAN")
		if plan == "" {
			plan = "free"
		}
		// UserID is the submitter the build is attributed to. Without it the
		// service skips wallet resolution (Submit: `userID != ""`), so $GRID
		// settlement no-ops — the static key must carry a real user for the
		// dog-food to settle (#718/#740).
		userID := os.Getenv("BUILD_GATEWAY_STATIC_USER")
		static.Add(key, auth.Identity{
			WorkspaceID: ws,
			UserID:      userID,
			Plan:        plan,
		})
		registered++
		logger.Info("static api key registered",
			slog.String("workspace_id", ws),
			slog.String("user_id", userID),
			slog.String("plan", plan),
		)
	}

	// Additional keys (multi-customer): one `key=workspace:user:plan` per line
	// or ';'-separated entry. The key value is NEVER logged — only the
	// workspace/user/plan it binds to.
	if extra := os.Getenv("BUILD_GATEWAY_STATIC_API_KEYS"); strings.TrimSpace(extra) != "" {
		for _, entry := range strings.FieldsFunc(extra, func(r rune) bool { return r == '\n' || r == ';' }) {
			entry = strings.TrimSpace(entry)
			if entry == "" {
				continue
			}
			eq := strings.IndexByte(entry, '=')
			if eq <= 0 {
				logger.Warn("BUILD_GATEWAY_STATIC_API_KEYS entry missing '=' (key=workspace:user:plan); skipped")
				continue
			}
			key := strings.TrimSpace(entry[:eq])
			spec := strings.TrimSpace(entry[eq+1:])
			parts := strings.SplitN(spec, ":", 3)
			ws := strings.TrimSpace(parts[0])
			if ws == "" {
				ws = "default-workspace"
			}
			userID := ""
			if len(parts) > 1 {
				userID = strings.TrimSpace(parts[1])
			}
			plan := "free"
			if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
				plan = strings.TrimSpace(parts[2])
			}
			static.Add(key, auth.Identity{WorkspaceID: ws, UserID: userID, Plan: plan})
			registered++
			logger.Info("static api key registered (multi)",
				slog.String("workspace_id", ws),
				slog.String("user_id", userID),
				slog.String("plan", plan),
			)
		}
	}

	if registered == 0 {
		logger.Warn("no static api key configured; every customer request will 401 until billing-svc wiring lands")
	}
	return auth.NewCachingValidator(static, 0)
}
