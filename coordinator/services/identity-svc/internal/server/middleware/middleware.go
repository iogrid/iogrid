// Package middleware provides bearer-token auth + step-up decoration for
// identity-svc HTTP routes. Other coordinator services pull the same
// pattern via a future shared package once we extract it.
package middleware

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

// devServiceTokenEnv names the env-var that, when set, enables the
// `x-iogrid-user-id` header bypass below. The env-var holds the shared
// secret a trusted BFF (the Next.js web plane) must present in its
// Authorization header to assert a user identity on behalf of a
// browser session that does not (yet) carry an identity-svc JWT.
//
// Why this exists (issue #232): Phase 0 has no end-to-end NextAuth→
// identity-svc token exchange wired. Until that's built, the customer
// dashboard cannot call /v1/workspaces directly. The Next.js BFF
// (which DOES hold the user's verified NextAuth session) calls
// identity-svc with this service token + the resolved user id; the
// middleware then materialises an authed context just like a real
// bearer JWT would have done. The shared secret never leaves the
// cluster — it's mounted from a sealed Secret into both pods.
//
// When IOGRID_SERVICE_TOKEN is empty (the default), this bypass is a
// no-op and only real JWT bearers authenticate.
const devServiceTokenEnv = "IOGRID_SERVICE_TOKEN"

// devUserHeader is the header carrying the BFF-asserted user UUID.
// Lowercased to match canonical HTTP form Go's `Get` performs.
const devUserHeader = "X-Iogrid-User-Id"

// contextKey wraps a string so context.Value lookups are type-safe.
type contextKey string

const (
	ctxUserID contextKey = "user_id"
	ctxClaims contextKey = "claims"
)

// VerifyBearer parses the Authorization: Bearer <jwt> header and writes
// the user UUID + claims into the context. If no token is supplied, the
// chain continues with an empty user — RequireBearer below enforces the
// presence check on protected routes.
//
// As a Phase 0 stop-gap (issue #232) the middleware ALSO accepts the
// BFF service-token + X-Iogrid-User-Id header combination when the
// IOGRID_SERVICE_TOKEN env-var is set. This lets the Next.js web plane
// proxy authenticated browser sessions into identity-svc without first
// minting a real JWT — see the constant doc above. The bypass only
// fires when (a) the env-var is non-empty AND (b) the supplied bearer
// matches it; any mismatch falls through to the JWT path unchanged.
func VerifyBearer(signer *tokens.Signer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
				next.ServeHTTP(w, r)
				return
			}
			token := strings.TrimSpace(authz[len("bearer "):])

			// BFF service-token short-circuit (Phase 0 stop-gap).
			if svcTok := os.Getenv(devServiceTokenEnv); svcTok != "" && token == svcTok {
				rawUID := r.Header.Get(devUserHeader)
				if rawUID == "" {
					next.ServeHTTP(w, r)
					return
				}
				id, err := uuid.Parse(rawUID)
				if err != nil {
					next.ServeHTTP(w, r)
					return
				}
				ctx := context.WithValue(r.Context(), ctxUserID, id)
				// We deliberately do NOT inject Claims here: step-up
				// gated routes (DeleteAccount, payout change) MUST
				// continue to require a real JWT with `step_up=true`.
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			claims, err := signer.Verify(token)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			id, err := uuid.Parse(claims.Subject)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			ctx := context.WithValue(r.Context(), ctxUserID, id)
			ctx = context.WithValue(ctx, ctxClaims, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireBearer rejects requests whose context has no authed user.
func RequireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := AuthedUser(r.Context()); !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"missing bearer token"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AuthedUser returns the parsed user UUID, if any.
func AuthedUser(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(ctxUserID).(uuid.UUID)
	return v, ok
}

// AuthedClaims returns the parsed JWT claims, if any.
func AuthedClaims(ctx context.Context) (*tokens.AccessClaims, bool) {
	v, ok := ctx.Value(ctxClaims).(*tokens.AccessClaims)
	return v, ok
}

// WithAuthedUser is a test-only helper for handlers that need to assert
// the authed-user path without spinning up a JWT signer. Production
// code reaches this state via VerifyBearer above.
func WithAuthedUser(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxUserID, id)
}

// WithAuthedClaims is the claims counterpart for WithAuthedUser.
func WithAuthedClaims(ctx context.Context, claims *tokens.AccessClaims) context.Context {
	return context.WithValue(ctx, ctxClaims, claims)
}

// RequireStepUp rejects requests whose token doesn't carry the step_up
// flag. Used by payout-change / merge / account-delete endpoints in
// other services that import this package.
func RequireStepUp(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := AuthedClaims(r.Context())
		if !ok || !claims.StepUp {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"code":"step_up_required","message":"step-up auth required"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
