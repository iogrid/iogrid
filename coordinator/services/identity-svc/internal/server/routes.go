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

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/handlers"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

// MountConfig bundles every collaborator the routes need. We pass the
// API + signer in rather than reach for globals.
type MountConfig struct {
	API    *handlers.API
	Signer *tokens.Signer
}

// MountFunc returns the function the shared bootstrap will hand to its
// internal chi.Router. Decorates every request with bearer-token parsing
// so handlers can read the authed user from context.
func MountFunc(cfg MountConfig) func(r chi.Router) {
	return func(r chi.Router) {
		r.Use(authmw.VerifyBearer(cfg.Signer))
		cfg.API.Mount(r)
	}
}
