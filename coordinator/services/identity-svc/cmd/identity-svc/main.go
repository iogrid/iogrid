// Command identity-svc is the iogrid coordinator entrypoint for the
// identity-svc microservice.
//
// At boot we:
//   1. Parse env config (config.Load)
//   2. Open the Postgres pool + run goose migrations
//   3. Open Redis (best-effort — failure falls back to in-memory rate
//      limiter and OAuth state store)
//   4. Build the Google OIDC client + RS256 JWT signer + Stalwart SMTP
//      sender
//   5. Wire the auth.Service + handlers.API + bearer middleware onto
//      the shared chi router
//   6. Mark ready and serve on $LISTEN_ADDR
//
// /healthz + /readyz + /metrics come from the shared bootstrap.
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/config"
	idb "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/db"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/mail"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/oauth/google"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/ratelimit"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/handlers"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/siws"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
	"github.com/iogrid/iogrid/coordinator/shared/db"
	"github.com/iogrid/iogrid/coordinator/shared/health"
	logpkg "github.com/iogrid/iogrid/coordinator/shared/log"
	"github.com/iogrid/iogrid/coordinator/shared/otel"
	sharedserver "github.com/iogrid/iogrid/coordinator/shared/server"
)

const (
	serviceName    = "identity-svc"
	serviceVersion = "0.1.0"
)

func main() {
	logger := logpkg.Setup(serviceName)
	logger.Info("starting identity-svc", slog.String("version", serviceVersion))

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

	hr := health.New()

	// --- Postgres ----------------------------------------------------
	pool, err := db.NewPool(ctx, db.Config{URL: cfg.DatabaseURL})
	if err != nil {
		logger.Error("db pool failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer pool.Close()
	hr.AddProbe("db", db.PingProbe(pool))

	if err := idb.Apply(ctx, cfg.DatabaseURL); err != nil {
		logger.Error("db migrations failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	st := store.New(pool)

	// --- Redis (best-effort) -----------------------------------------
	var redisClient *redis.Client
	if cfg.RedisURL != "" {
		opts, err := redis.ParseURL(cfg.RedisURL)
		if err != nil {
			logger.Warn("redis url parse failed; falling back to in-memory", slog.String("error", err.Error()))
		} else {
			redisClient = redis.NewClient(opts)
			pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			if err := redisClient.Ping(pingCtx).Err(); err != nil {
				logger.Warn("redis ping failed; falling back to in-memory", slog.String("error", err.Error()))
				redisClient = nil
			}
			cancel()
		}
	}

	// --- JWT signer --------------------------------------------------
	// JWT_KEYPAIR_AUTOGEN=1 mints an ephemeral RSA-2048 keypair, writes
	// it under JWTAutogenDir, and overrides the configured paths. This
	// is for dev / e2e ONLY — tokens do not survive pod restart and
	// downstream verifiers that cached the previous public key will
	// reject them. The deployment manifest provides an emptyDir mount
	// at /tmp/jwt-keys so this works under readOnlyRootFilesystem=true.
	jwtPrivPath := cfg.JWTPrivateKeyPath
	jwtPubPath := cfg.JWTPublicKeyPath
	if cfg.JWTKeypairAutogen {
		p, pub, err := tokens.EnsureAutogenKeypair(cfg.JWTAutogenDir)
		if err != nil {
			logger.Error("jwt autogen failed", slog.String("error", err.Error()))
			os.Exit(1)
		}
		jwtPrivPath, jwtPubPath = p, pub
		logger.Warn("JWT_KEYPAIR_AUTOGEN=1 — generated EPHEMERAL keypair",
			slog.String("dir", cfg.JWTAutogenDir),
			slog.String("impact", "tokens are invalidated on pod restart; NEVER set in prod"),
		)
	}
	signer, err := tokens.NewSigner(tokens.SignerConfig{
		PrivateKeyPath: jwtPrivPath,
		PublicKeyPath:  jwtPubPath,
		KeyID:          cfg.JWTKeyID,
		Issuer:         cfg.JWTIssuer,
		Audience:       cfg.JWTAudience,
		AccessTokenTTL: cfg.AccessTokenTTL,
	})
	if err != nil {
		logger.Error("jwt signer failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// --- SMTP sender -------------------------------------------------
	sender, err := mail.NewSMTP(mail.Config{
		Host: cfg.SMTPHost, Port: cfg.SMTPPort,
		Username: cfg.SMTPUsername, Password: cfg.SMTPPassword,
		From: cfg.SMTPFrom, FromName: cfg.SMTPFromName,
		StartTLS: cfg.SMTPStartTLS,
	})
	if err != nil {
		logger.Error("smtp sender failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// --- Google OAuth client -----------------------------------------
	googleClient, err := google.New(ctx, google.Config{
		ClientID:     cfg.GoogleClientID,
		ClientSecret: cfg.GoogleClientSecret,
		RedirectURL:  cfg.GoogleRedirectURL,
	}, redisClient)
	if err != nil {
		logger.Error("google client failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// --- rate limiter ------------------------------------------------
	var limiter ratelimit.Limiter
	if redisClient != nil {
		limiter = &ratelimit.RedisLimiter{Client: redisClient, Prefix: "iogrid:identity"}
	} else {
		limiter = ratelimit.NewMemory()
	}

	// --- auth service + handlers ------------------------------------
	authSvc := auth.New(auth.Options{
		Store:                    st,
		Google:                   googleClient,
		Mail:                     sender,
		Signer:                   signer,
		Limiter:                  limiter,
		Logger:                   logger,
		BaseURL:                  cfg.BaseURL,
		AllowedReturnHosts:       cfg.AllowedReturnHosts,
		MagicLinkTTL:             cfg.MagicLinkTTL,
		RefreshTokenTTL:          cfg.RefreshTokenTTL,
		StepUpTTL:                cfg.StepUpTTL,
		MagicLinkPerEmailPerHour: cfg.MagicLinkPerEmailPerHour,
		MagicLinkPerIPPerHour:    cfg.MagicLinkPerIPPerHour,
	})

	// --- SIWS challenge store --------------------------------------
	// Production wiring: Redis-backed, keyed by wallet address with a
	// 5-minute TTL. When Redis is unavailable the in-memory fallback
	// kicks in via Service.challenges() — single-pod dev only.
	if redisClient != nil {
		authSvc.WithSiwsChallenges(&siws.RedisChallengeStore{Client: redisClient})
	} else {
		authSvc.WithSiwsChallenges(siws.NewMemoryChallengeStore())
		logger.Warn("siws: redis unavailable; using in-memory challenge store (single-pod dev only)")
	}

	api := handlers.New(authSvc, st, logger)
	wsHandler := handlers.NewWorkspaceHandler(st)
	// idHandler ships the RemoveIdentifier + DeleteAccount RPCs that
	// back /account/identifiers + /account/danger-zone in the web plane.
	idHandler := handlers.NewIdentityHandler(st)
	// authHandler ships AuthService.{ListSessions, RevokeSession} that
	// back /account/sessions (issue #322) plus the SIWS wallet RPCs
	// (StartSiwsBinding, CompleteSiwsBinding, ListBoundWallets,
	// UnbindWallet) that back /account/wallets (issue #326). Other
	// AuthService RPCs keep flowing through the chi JSON tree on api
	// until each is migrated to Connect-RPC under EPIC #309.
	authHandler := handlers.NewAuthHandler(st, authSvc)

	// --- background: session cleanup ---------------------------------
	cleanupCtx, cancelCleanup := context.WithCancel(ctx)
	defer cancelCleanup()
	go runSessionCleaner(cleanupCtx, st, logger)

	hr.MarkReady()

	if err := sharedserver.Run(ctx, sharedserver.Options{
		ServiceName: serviceName,
		Logger:      logger,
		Health:      hr,
		ListenAddr:  cfg.ListenAddr,
		Mount: server.MountFunc(server.MountConfig{
			API:       api,
			Workspace: wsHandler,
			Identity:  idHandler,
			Auth:      authHandler,
			Signer:    signer,
		}),
	}); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// runSessionCleaner deletes expired refresh-tokens + magic-link tokens
// once an hour. Idle SQL is cheap; the alternative (relying on application
// queries to filter) would let abandoned tokens grow indefinitely.
func runSessionCleaner(ctx context.Context, st *store.Store, logger *slog.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			cutoff := now.Add(-time.Hour) // small grace so racing clients don't trip on row deletion
			if n, err := st.PurgeExpiredSessions(ctx, nil, cutoff); err != nil {
				logger.Warn("purge expired sessions failed", slog.String("error", err.Error()))
			} else if n > 0 {
				logger.Info("purged expired sessions", slog.Int64("count", n))
			}
			if n, err := st.PurgeExpiredMagicLinkTokens(ctx, nil, cutoff); err != nil {
				logger.Warn("purge expired magic links failed", slog.String("error", err.Error()))
			} else if n > 0 {
				logger.Info("purged expired magic links", slog.Int64("count", n))
			}
		}
	}
}
