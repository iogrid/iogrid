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
	Config         *config.Config
	Clients        *clients.Set
	Verifier       auth.Verifier
	APIKeyStore    handlers.APIKeyStore
	AuthedLimiter  *ratelimit.Limiter
	AnonLimiter    *ratelimit.Limiter
	Logger         *slog.Logger
}

// Mount builds the routes from the supplied Deps. Pass the returned
// function as shared.server.Options.Mount.
func Mount(deps Deps) func(chi.Router) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	api := handlers.New(deps.Clients, deps.APIKeyStore, deps.Logger)

	return func(r chi.Router) {
		// Index lives under /v1 for symmetry with the other services
		// (it predates the BFF's customer-facing /api/v1/* tree).
		r.Route("/v1", func(r chi.Router) {
			r.Get("/", indexHandler)
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

			// /vpn --------------------------------------------------------
			r.Route("/vpn", func(r chi.Router) {
				r.Use(auth.RequireAuth)
				r.Get("/account", api.GetVPNAccount)
				r.Post("/upgrade", api.UpgradeVPN)
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
