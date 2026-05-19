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
	Signer   *tokens.Signer
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
			cfg.API.Mount(r)
			// chi only allows Route("/v1", ...) once — handlers that
			// share the /v1 prefix must register inside a single Route.
			if cfg.Workspace != nil || cfg.Identity != nil {
				r.Route("/v1", func(r chi.Router) {
					if cfg.Workspace != nil {
						cfg.Workspace.MountWorkspaceJSON(r)
					}
					if cfg.Identity != nil {
						cfg.Identity.MountIdentityJSON(r)
					}
				})
			}
			if cfg.Workspace != nil {
				path, hh := identityv1connect.NewWorkspaceServiceHandler(cfg.Workspace)
				r.Mount(path, hh)
			}
			if cfg.Identity != nil {
				path, hh := identityv1connect.NewIdentityServiceHandler(cfg.Identity)
				r.Mount(path, hh)
			}
		})
	}
}
