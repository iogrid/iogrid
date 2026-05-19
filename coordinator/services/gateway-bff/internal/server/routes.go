// Package server wires the gateway-bff route tree onto the shared chi
// router. The Mount function is the only public entry point — main()
// calls it via shared/server.Run.
//
// Route conventions:
//
//	/api/v1/me                     —  GET   (auth required)
//	/api/v1/account/*              —  POST  (mostly anon — sign-in flows)
//	/api/v1/provide/*              —  GET/POST (auth, provider role)
//	/api/v1/customer/*             —  GET/POST/DELETE (auth, customer role)
//	/api/v1/admin/*                —  any  (auth, ADMIN role)
//	/api/v1/vpn/*                  —  GET/POST (auth)
//
// Cross-cutting middleware (CORS, JWT parse, rate-limit) is layered
// once at the top of the v1 sub-router.
package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/cors"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/handlers"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/ratelimit"
)

// Deps bundles everything Mount needs. Constructed at boot in main.go.
type Deps struct {
	Config        *config.Config
	Clients       *clients.Set
	Verifier      auth.Verifier
	APIKeyStore   handlers.APIKeyStore
	AuthedLimiter *ratelimit.Limiter
	AnonLimiter   *ratelimit.Limiter
	Logger        *slog.Logger
	// VPNGateway is optional; when present the BFF surfaces
	// /api/v1/vpn/config-for-platform proxying to the vpn-gateway pod.
	VPNGateway *handlers.VPNGatewayProxy
	// Transparency is optional; when present the BFF accepts the
	// antiabuse-svc CronJob's POST at /api/v1/transparency/publish
	// and serves the cached snapshots at /status/transparency/*.
	Transparency handlers.TransparencyStore
	// TransparencyPublishToken, when set, is required as a Bearer
	// token on POST /api/v1/transparency/publish (the CronJob is
	// configured with the same secret). When empty the endpoint
	// relies on NetworkPolicy + in-cluster trust.
	TransparencyPublishToken string
	// Workspaces is the optional client over identity-svc's
	// WorkspaceService. When nil the /api/v1/workspaces tree returns
	// 503 — useful in dev environments where identity-svc isn't up.
	Workspaces handlers.WorkspaceClient
}

// Mount builds the routes from the supplied Deps. Pass the returned
// function as shared.server.Options.Mount.
func Mount(deps Deps) func(chi.Router) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	api := handlers.New(deps.Clients, deps.APIKeyStore, deps.Logger)
	api.VPNGateway = deps.VPNGateway
	api.Transparency = deps.Transparency
	if api.Transparency == nil {
		// Default in-memory store so the CronJob POST never 503s in
		// the dev-mode boot path. Production main.go can override
		// with a Redis/DB-backed implementation.
		api.Transparency = handlers.NewMemoryTransparencyStore()
	}
	api.Workspaces = deps.Workspaces
	// Phase 0: default to an in-memory customer-onboard store so
	// POST /api/v1/onboard/customer works end-to-end without further
	// wiring. Phase 1 swaps this for the identity-svc-backed impl.
	if api.CustomerOnboardStore == nil {
		api.CustomerOnboardStore = handlers.NewMemoryCustomerOnboardStore()
	}

	return func(r chi.Router) {
		// Index lives under /v1 for symmetry with the other services
		// (it predates the BFF's customer-facing /api/v1/* tree).
		r.Route("/v1", func(r chi.Router) {
			r.Get("/", indexHandler)
		})

		// Public transparency endpoints — unauthenticated by design
		// (the docs/LEGAL.md commitment is to publish these).
		r.Route("/status/transparency", func(r chi.Router) {
			r.Get("/", api.ListTransparencyReports)
			r.Get("/{year}/{quarter}", api.GetTransparencyReport)
		})

		r.Route("/api/v1", func(r chi.Router) {
			if len(deps.Config.CORSAllowedOrigins) > 0 {
				r.Use(cors.Middleware(cors.Options{
					AllowedOrigins: deps.Config.CORSAllowedOrigins,
				}))
			}
			r.Use(auth.Middleware(deps.Verifier, deps.Logger))
			if deps.AuthedLimiter != nil && deps.AnonLimiter != nil {
				r.Use(ratelimit.Middleware(deps.AuthedLimiter, deps.AnonLimiter))
			}

			// /me ---------------------------------------------------------
			r.Get("/me", api.GetMe)

			// /account ----------------------------------------------------
			r.Route("/account", func(r chi.Router) {
				r.Post("/sign-in/google", api.StartGoogleSignIn)
				r.Post("/sign-in/google/complete", api.CompleteGoogleSignIn)
				r.Post("/sign-in/magic", api.RequestMagicLink)
				r.Post("/sign-in/magic/complete", api.CompleteMagicLink)
				r.Post("/sign-out", api.SignOut)
				r.Get("/sessions", api.ListSessions)

				// Auto-update operator surface (#59).
				r.Route("/updates", func(r chi.Router) {
					r.Use(auth.RequireAuth)
					r.Get("/", api.GetUpdates)
					r.Post("/preferences", api.SaveUpdatePreferences)
					r.Post("/check", api.TriggerUpdateCheck)
					r.Post("/apply", api.ApplyPendingUpdate)
					r.Post("/rollback", api.RollbackUpdate)
				})
			})

			// /onboard ----------------------------------------------------
			// Daemon-pairing handshake. /start + /complete require the
			// browser user to be signed in; /poll is called by the daemon
			// itself (no Bearer token — proof-of-liveness via the daemon
			// pubkey it includes in the body). EPIC #5.
			//
			// /customer is the self-service B2B customer signup endpoint —
			// docs/ROADMAP.md Phase 0 deliverable B. Reserves a workspace
			// handle + mints the first API key + returns the proxy entry
			// point. Plaintext API key returned ONCE.
			r.Route("/onboard", func(r chi.Router) {
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireAuth)
					r.Post("/start", api.StartOnboard)
					r.Post("/complete", api.CompleteOnboard)
					r.Post("/customer", api.OnboardCustomer)
				})
				r.Post("/poll", api.PollOnboard)
			})

			// /provide ----------------------------------------------------
			r.Route("/provide", func(r chi.Router) {
				r.Use(auth.RequireAuth)
				r.Get("/dashboard", api.GetProviderDashboard)
				r.Get("/schedule", api.GetProviderSchedule)
				r.Post("/schedule", api.UpdateProviderSchedule)
				r.Get("/audit/stream", api.StreamProviderAudit)
				r.Get("/earnings", api.GetProviderEarnings)
			})

			// /customer ---------------------------------------------------
			r.Route("/customer", func(r chi.Router) {
				r.Use(auth.RequireAuth)
				r.Post("/api-keys", api.CreateAPIKey)
				r.Get("/api-keys", api.ListAPIKeys)
				r.Delete("/api-keys/{id}", api.DeleteAPIKey)
				r.Get("/usage", api.GetCustomerUsage)
				r.Post("/workloads", api.SubmitWorkload)
				r.Get("/workloads/{id}/events", api.StreamWorkloadEvents)
			})

			// /admin ------------------------------------------------------
			r.Route("/admin", func(r chi.Router) {
				r.Use(auth.RequireRole("ADMIN"))
				r.Get("/abuse-queue", api.ListAbuseQueue)
				r.Post("/abuse/{id}/resolve", api.ResolveAbuseEvent)
			})

			// /transparency ----------------------------------------------
			// POST is the antiabuse-svc CronJob ingest hook. Optional
			// bearer-token guard via TransparencyPublishToken; absent
			// guard relies on cluster NetworkPolicy.
			r.Route("/transparency", func(r chi.Router) {
				if deps.TransparencyPublishToken != "" {
					tok := deps.TransparencyPublishToken
					r.Use(func(next http.Handler) http.Handler {
						return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
							if req.Header.Get("Authorization") != "Bearer "+tok {
								http.Error(w, `{"code":"unauthorized","message":"bad publish token"}`, http.StatusUnauthorized)
								return
							}
							next.ServeHTTP(w, req)
						})
					})
				}
				r.Post("/publish", api.PublishTransparencyReport)
			})

			// /workspaces ---------------------------------------------
			// Workspace bounded-context (#146). Auth-required; proxies
			// to identity-svc WorkspaceService.
			r.Route("/workspaces", func(r chi.Router) {
				r.Use(auth.RequireAuth)
				r.Get("/", api.ListWorkspaces)
				r.Post("/", api.CreateWorkspace)
				r.Get("/{id}", api.GetWorkspace)
				r.Patch("/{id}", api.UpdateWorkspace)
				r.Delete("/{id}", api.DeleteWorkspace)
				r.Get("/{id}/members", api.ListMembers)
				r.Post("/{id}/members", api.AddMember)
				r.Patch("/{id}/members/{userID}", api.UpdateMemberRole)
				r.Delete("/{id}/members/{userID}", api.RemoveMember)
			})

			// /vpn --------------------------------------------------------
			r.Route("/vpn", func(r chi.Router) {
				r.Use(auth.RequireAuth)
				r.Get("/account", api.GetVPNAccount)
				r.Post("/upgrade", api.UpgradeVPN)
				// config-for-platform proxies to vpn-gateway and streams
				// the per-platform artefact (.conf | .mobileconfig | QR
				// payload) straight to the browser.
				r.Get("/config-for-platform", api.GetVPNConfigForPlatform)
			})
		})
	}
}

// indexHandler returns a stable JSON envelope identifying the service.
// Kept for backwards-compat with smoke tests + service-discovery probes.
func indexHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"service": "gateway-bff",
		"status":  "ok",
	})
}
