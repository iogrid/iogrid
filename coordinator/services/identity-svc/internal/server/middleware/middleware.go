// Package middleware provides bearer-token auth + step-up decoration for
// identity-svc HTTP routes. Other coordinator services pull the same
// pattern via a future shared package once we extract it.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

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
func VerifyBearer(signer *tokens.Signer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
				next.ServeHTTP(w, r)
				return
			}
			token := strings.TrimSpace(authz[len("bearer "):])
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
