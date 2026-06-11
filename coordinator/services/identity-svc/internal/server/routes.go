// Package server holds the HTTP route definitions for the identity-svc
// microservice. It composes the bearer-token middleware over the
// per-feature handlers in internal/server/handlers.
//
// The Mount() function preserves the contract the shared bootstrap
// (coordinator/shared/server) expects: a single callback that decorates
// a chi.Router with all business endpoints. Health, readiness, and
// metrics are wired by the shared bootstrap before Mount fires.
package server

import (
	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1/identityv1connect"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/handlers"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

// MountConfig bundles every collaborator the routes need. We pass the
// API + signer in rather than reach for globals.
type MountConfig struct {
	API       *handlers.API
	Workspace *handlers.WorkspaceHandler
	// Identity hosts the RemoveIdentifier + DeleteAccount RPCs the
	// /account/identifiers + /account/danger-zone surfaces depend on.
	// Optional so existing test wiring that constructs MountConfig
	// without it keeps compiling.
	Identity *handlers.IdentityHandler
	// Auth hosts AuthService.{ListSessions, RevokeSession} that back
	// /account/sessions (issue #322). Other AuthService RPCs (sign-in
	// flows, refresh, sign-out, step-up, SIWS) remain on the chi JSON
	// tree in handlers.go until they're migrated one-by-one. Optional
	// so existing test wiring without sessions keeps compiling.
	Auth   *handlers.AuthHandler
	Signer *tokens.Signer
	// InternalToken guards the cluster-internal, NON-bearer endpoints
	// (e.g. build-gateway wallet resolution, #718). Empty disables them
	// (fail closed). Wired from IDENTITY_INTERNAL_TOKEN in main.
	InternalToken string
}

// MountFunc returns the function the shared bootstrap will hand to its
// internal chi.Router. Decorates every request with bearer-token parsing
// so handlers can read the authed user from context.
func MountFunc(cfg MountConfig) func(r chi.Router) {
	return func(r chi.Router) {
		// shared bootstrap mounts /healthz, /readyz, /metrics before
		// calling MountFunc, so we cannot `r.Use(...)` on the parent —
		// chi rejects middleware additions once routes are present.
		// Scope the bearer middleware to a Group sub-router instead.
		r.Group(func(r chi.Router) {
			r.Use(authmw.VerifyBearer(cfg.Signer))
			// All four handler trees (API, Workspace, Identity, Auth)
			// share the /v1 prefix. chi.Mux allows Route("/v1", ...)
			// only once per parent — so own the /v1 here and mount
			// each handler's sub-paths inside via MountV1 /
			// MountXxxJSON.
			r.Route("/v1", func(r chi.Router) {
				cfg.API.MountV1(r)
				if cfg.Workspace != nil {
					cfg.Workspace.MountWorkspaceJSON(r)
				}
				if cfg.Identity != nil {
					cfg.Identity.MountIdentityJSON(r)
				}
				if cfg.Auth != nil {
					cfg.Auth.MountSessionsJSON(r)
				}
			})
			// Connect-RPC handlers own their own absolute paths derived
			// from the proto package — outside /v1.
			if cfg.Workspace != nil {
				path, hh := identityv1connect.NewWorkspaceServiceHandler(cfg.Workspace)
				r.Mount(path, hh)
			}
			if cfg.Identity != nil {
				path, hh := identityv1connect.NewIdentityServiceHandler(cfg.Identity)
				r.Mount(path, hh)
			}
			if cfg.Auth != nil {
				path, hh := identityv1connect.NewAuthServiceHandler(cfg.Auth)
				r.Mount(path, hh)
			}
		})
		// Cluster-internal, NON-bearer endpoints (token-guarded). Added to
		// the parent OUTSIDE the bearer Group above — these are reached by
		// trusted services, not user sessions. #718: build-gateway wallet
		// resolution for $GRID build settlement.
		if cfg.Auth != nil {
			r.Get("/internal/v1/users/{userID}/wallet",
				handlers.InternalAuth(cfg.InternalToken, cfg.Auth.InternalGetUserWallet))
		}
	}
}
