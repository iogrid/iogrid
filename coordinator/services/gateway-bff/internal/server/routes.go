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
	// OffRamp is the thin HTTP proxy to billing-svc's off-ramp routes.
	// When nil the /api/v1/offramp/* + webhook routes return 503.
	OffRamp *handlers.OffRampProxy
	// BillingSvcBaseURL is billing-svc's base URL, forwarded to the
	// customer prepaid-balance handler (#632). When empty the
	// /api/v1/customer/billing/balance route returns 503.
	BillingSvcBaseURL string
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
	api.OffRamp = deps.OffRamp
	api.BillingSvcBaseURL = deps.BillingSvcBaseURL
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
			// Bridge auth.Claims → clients.CallerClaims so every
			// outbound Connect-RPC call automatically forwards the
			// caller's identity through the header-forwarding
			// interceptor (issue #321). MUST sit directly after
			// auth.Middleware so the claims it set are visible.
			r.Use(clients.PropagateClaimsMiddleware)
			if deps.AuthedLimiter != nil && deps.AnonLimiter != nil {
				r.Use(ratelimit.Middleware(deps.AuthedLimiter, deps.AnonLimiter))
			}

			// /me ---------------------------------------------------------
			r.Get("/me", api.GetMe)
			// DELETE /me triggers identity-svc soft-delete + cascade
			// (closes #197). DELETE /me/identifiers/{id} unbinds one
			// identifier without orphaning the account (closes #196).
			r.Delete("/me", api.DeleteMyAccount)
			r.Delete("/me/identifiers/{id}", api.RemoveMyIdentifier)
			// PUT /me/preferred-landing-role — EPIC #422 /welcome
			// picker. JSON forward to identity-svc's chi route via the
			// service-token shim. Identity-svc owns the enum-cast
			// validation; gateway-bff is a thin transparent proxy.
			r.Put("/me/preferred-landing-role", api.SetMyPreferredLandingRole)

			// /account ----------------------------------------------------
			r.Route("/account", func(r chi.Router) {
				r.Post("/sign-in/google", api.StartGoogleSignIn)
				r.Post("/sign-in/google/complete", api.CompleteGoogleSignIn)
				r.Post("/sign-in/magic", api.RequestMagicLink)
				r.Post("/sign-in/magic/complete", api.CompleteMagicLink)
				r.Post("/sign-out", api.SignOut)
				// Sessions surface (issue #322). GET lists every active
				// session for the caller (with is_current on the row
				// matching this browser's session id). DELETE revokes
				// one. The caller's own current session is refused —
				// the user must sign out via /sign-out so the refresh
				// cookie clears alongside the server-side revocation.
				r.Get("/sessions", api.ListSessionsForAccount)
				r.Delete("/sessions/{id}", api.RevokeAccountSession)

				// Wallets surface (issue #326). Real backing for the
				// /account/wallets page — the $GRID payout target.
				// Replaces the Phase 0 stubs that used to live under
				// /identity/wallets in this same router. Auth gate is
				// inherited from the outer /api/v1 group; the inner
				// chi router does not add a separate RequireAuth so
				// the surface keeps a single response shape on 401.
				r.Route("/wallets", func(r chi.Router) {
					r.Use(auth.RequireAuth)
					r.Get("/", api.ListWallets)
					r.Post("/", api.BindWallet)
					r.Post("/challenge", api.IssueWalletChallenge)
					r.Delete("/{address}", api.UnbindWallet)
				})

				// Auto-update operator surface (#59).
				r.Route("/updates", func(r chi.Router) {
					r.Use(auth.RequireAuth)
					r.Get("/", api.GetUpdates)
					r.Post("/preferences", api.SaveUpdatePreferences)
					r.Post("/check", api.TriggerUpdateCheck)
					r.Post("/apply", api.ApplyPendingUpdate)
					r.Post("/rollback", api.RollbackUpdate)
				})

				// Notification-preferences surface (#631). GET reads the
				// caller's stored channel toggles; POST persists them.
				// Both forward to identity-svc's notification-prefs routes
				// via the service-token shim (durable, server-side).
				r.Route("/notifications", func(r chi.Router) {
					r.Use(auth.RequireAuth)
					r.Get("/", api.GetNotificationPrefs)
					r.Post("/", api.SaveNotificationPrefs)
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
				// Headline-card surface for /provide/earnings page (#324).
				// summary returns lifetime/last_30d/last_7d/pending +
				// workload count; payout-method is the user's election
				// (CASH_USDC | FREE_VPN | CHARITY | UNSPECIFIED).
				r.Get("/earnings/summary", api.GetProviderEarningsSummary)
				r.Get("/payout-method", api.GetProviderPayoutMethod)
				r.Put("/payout-method", api.SetProviderPayoutMethod)
				// Per-owner primary-daemon picker (#325). Used by the
				// schedule editor when the caller owns ≥2 paired
				// daemons. PUT body: {"provider_id":"<UUID>"} —
				// providers-svc validates ownership in SQL.
				r.Put("/primary-provider", api.SetPrimaryProvider)
			})

			// /customer ---------------------------------------------------
			r.Route("/customer", func(r chi.Router) {
				r.Use(auth.RequireAuth)
				r.Post("/api-keys", api.CreateAPIKey)
				r.Get("/api-keys", api.ListAPIKeys)
				r.Delete("/api-keys/{id}", api.DeleteAPIKey)
				r.Get("/usage", api.GetCustomerUsage)
				r.Post("/workloads", api.SubmitWorkload)
				r.Get("/workloads", api.ListWorkloads)
				r.Get("/workloads/{id}/events", api.StreamWorkloadEvents)
				// /customer/vpn/sessions (#541) — active VPN sessions for
				// the signed-in customer. Forwards customer_id from the
				// authenticated session (NOT a wire param) to prevent
				// cross-customer reads.
				r.Get("/vpn/sessions", api.ListCustomerVPNSessions)
				// /customer/billing — Phase 0 empty-state snapshot so
				// /customer/billing renders the FREE/trial tier card
				// instead of a 404 banner. Phase 1 wires this to
				// billing-svc's subscription read model + Stripe portal
				// session generator.
				r.Get("/billing", emptyCustomerBilling)
				// /customer/billing/balance (#632) — prepaid $GRID balance
				// + grace-overage owed. Resolves the caller's bound wallet
				// then reads billing-svc /v1/grid/balance.
				r.Get("/billing/balance", api.GetCustomerBalance)
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

			// /offramp ----------------------------------------------------
			// Off-ramp adapter surface (issue #167 / #169 / #170).
			// /start + /status + /providers require auth. The webhook
			// receiver is unauthed — partners cannot carry our bearer;
			// their signature header is the auth (validated by
			// billing-svc's adapter).
			r.Route("/offramp", func(r chi.Router) {
				r.Get("/providers", api.ListOffRampProviders)
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireAuth)
					r.Post("/start", api.StartOffRamp)
					r.Get("/status/{requestID}", api.GetOffRampStatus)
				})
			})
			r.Route("/webhooks", func(r chi.Router) {
				r.Post("/offramp/{providerName}", api.HandleOffRampWebhook)
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

			// /burn -------------------------------------------------------
			// Burn-audit surface (#294). Phase 0 ships an empty summary
			// snapshot so the /burn dashboard renders an "all zeros"
			// empty state; daily + events lists return 501 until the
			// billing-svc burn ledger ships.
			r.Route("/burn", func(r chi.Router) {
				r.Use(auth.RequireAuth)
				r.Get("/daily", unimplemented("burn ledger"))
				r.Get("/events", unimplemented("burn ledger"))
				r.Get("/summary", emptyBurnSummary)
			})

			// /staking ----------------------------------------------------
			// Staking surface (#294 + #296). GET / returns an
			// "opted-out, zero stake" snapshot; GET /positions returns
			// an empty list so /provide/staking renders the empty
			// positions table. opt-in + stake/claim/early-unlock are
			// Phase 1.
			r.Route("/staking", func(r chi.Router) {
				r.Use(auth.RequireAuth)
				r.Get("/", emptyStakingState)
				r.Get("/positions", emptyStakingPositions)
				r.Post("/opt-in", unimplemented("staking opt-in"))
				r.Post("/stake", unimplemented("staking stake"))
				r.Post("/claim", unimplemented("staking claim"))
				r.Post("/early-unlock", unimplemented("staking early-unlock"))
			})

			// /account/step-up --------------------------------------------
			// Step-up verification surface (#294). Mounted as a child of
			// the existing /account route group above is awkward because
			// /account predates RequireAuth; mounting directly here keeps
			// the auth gate tight without disturbing the sign-in routes.
			r.Route("/account/step-up", func(r chi.Router) {
				r.Use(auth.RequireAuth)
				r.Post("/request", unimplemented("step-up verification"))
				r.Post("/verify", unimplemented("step-up verification"))
			})
		})
	}
}

// unimplemented returns a handler that responds 501 with a stable JSON
// envelope. The browser distinguishes 501 (feature deferred — show
// "coming soon") from 404 (chi default — surface looks broken).
func unimplemented(feature string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotImplemented)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"code":    "unimplemented",
			"message": feature + " backend deferred to Phase 1",
		})
	}
}

// emptyBurnSummary answers GET /api/v1/burn/summary with a zero-state
// snapshot so the dashboard renders the "no burns yet" empty state.
func emptyBurnSummary(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"total_burned": 0,
		"period":       "all",
	})
}

// emptyStakingState answers GET /api/v1/staking/ with a zero-state
// "opted-out" snapshot so the provide/staking page renders the opt-in
// CTA instead of a 404 banner.
func emptyStakingState(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"stake_amount": 0,
		"opted_in":     false,
	})
}

// emptyStakingPositions answers GET /api/v1/staking/positions with an
// empty positions list + zero total_grid so the /provide/staking
// positions table renders the "no active stakes" empty state instead
// of a 404 banner (#296).
func emptyStakingPositions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"positions":  []any{},
		"total_grid": 0,
	})
}

// emptyCustomerBilling answers GET /api/v1/customer/billing with a
// FREE/trial Phase 0 snapshot so the /customer/billing page renders
// the tier card instead of a 404 banner (#296). Phase 1 swaps this
// for a billing-svc subscription read + Stripe portal URL mint.
func emptyCustomerBilling(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"tier":               "FREE",
		"status":             "trial",
		"bandwidth_quota_gb": 50,
		"stripe_portal_url":  "",
	})
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
