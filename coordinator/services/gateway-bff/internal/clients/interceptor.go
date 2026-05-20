// interceptor.go: forwarding the BFF-authed identity downstream.
//
// gateway-bff verifies an inbound JWT via internal/auth.Middleware and
// materialises the caller's *auth.Claims into the request context. The
// downstream Connect-RPC services (identity-svc today; providers-svc,
// workloads-svc, billing-svc as they migrate) need to know who the
// caller is to scope their queries — but identity-svc has no inbound
// JWT verifier wired and only accepts the Phase 0 service-token shim
// (`Authorization: Bearer <IOGRID_SERVICE_TOKEN>` + X-Iogrid-User-Id +
// X-Iogrid-Session-Id + X-Iogrid-User-Roles).
//
// This interceptor reads the *auth.Claims out of the call's context
// (we re-set it via WithCallerClaims before each downstream call) and
// stamps the shim headers onto every outbound Connect-RPC request. It
// is intentionally narrow: no payload mutation, no retries, just
// header propagation. Plays cleanly alongside the per-adapter retry
// wrapper in clients.go (the retry runs OUTSIDE the interceptor; the
// interceptor's headers are applied on every attempt).
//
// Issue #322: prior to this interceptor, gateway-bff called
// identity-svc anonymously over Connect-RPC, so AuthService.ListSessions
// could not know which user to query (and would have returned
// CodeUnauthenticated even if it were wired). The interceptor closes
// that gap.
package clients

import (
	"context"
	"net/http"

	"connectrpc.com/connect"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

// ctxCallerClaimsKey is the context key the per-adapter Wrap helpers
// use to thread the caller's *auth.Claims into the Connect call. We
// keep a private type so external callers can't accidentally pollute
// the same key.
type ctxCallerClaimsKey struct{}

// WithCallerClaims attaches the caller's *auth.Claims to ctx so the
// header-forwarding interceptor can read them on its way out. Handlers
// MUST call this with the claims they read via auth.FromContext(r)
// before invoking any downstream Connect-RPC method.
func WithCallerClaims(ctx context.Context, c *auth.Claims) context.Context {
	if c == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxCallerClaimsKey{}, c)
}

// CallerClaims returns the *auth.Claims previously attached by
// WithCallerClaims. Exported so test fixtures can assert the
// propagation works.
func CallerClaims(ctx context.Context) (*auth.Claims, bool) {
	c, ok := ctx.Value(ctxCallerClaimsKey{}).(*auth.Claims)
	return c, ok
}

// PropagateClaimsMiddleware bridges auth.Middleware's request-context
// claims into the clients-package context key the outbound Connect
// interceptor reads (newHeaderForwarder above). Without this bridge
// every handler would need to call WithCallerClaims manually before
// each downstream call — that was the gap that caused /api/v1/me to
// 401 end-to-end despite identity-svc's service-token shim working
// (issue #321).
//
// Place this middleware DIRECTLY AFTER auth.Middleware in the chain.
// On every authenticated request it copies the *auth.Claims out of
// the request context and re-attaches them under the clients
// package's ctxCallerClaimsKey, so a downstream call invoked with
// r.Context() automatically carries them.
//
// Anonymous (no claims) requests pass through untouched — downstream
// services treat the call as service-anonymous and return 401 if the
// route requires an authed user.
func PropagateClaimsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, ok := auth.FromContext(r.Context()); ok && c != nil {
			r = r.WithContext(WithCallerClaims(r.Context(), c))
		}
		next.ServeHTTP(w, r)
	})
}

// newHeaderForwarder returns a Connect UnaryInterceptorFunc that, on
// every outbound (client-side) call, stamps the caller's identity onto
// the request headers using the same shim pattern the web BFF uses to
// reach gateway-bff:
//
//	Authorization:        Bearer <serviceToken>
//	X-Iogrid-User-Id:     <UUID>
//	X-Iogrid-User-Roles:  CSV of role strings
//	X-Iogrid-User-Email:  primary email when known
//	X-Iogrid-Session-Id:  <UUID> (the `jti` claim)
//
// When the context has no claims (e.g. a background reconciler ping)
// the call goes through unchanged — downstream services treat that as
// "anonymous" and reject anything that needs an authed user. We also
// no-op when serviceToken is empty: tests / dev without the shim
// rely on the upstream JSON path which doesn't need these headers.
func newHeaderForwarder(serviceToken string) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			// Only stamp on client-side calls — the same interceptor
			// would otherwise be invoked on the server side and could
			// leak the headers back into a reply. Connect models the
			// direction on the request itself.
			if req.Spec().IsClient {
				if c, ok := CallerClaims(ctx); ok && c != nil && serviceToken != "" {
					h := req.Header()
					if uid := c.UserID(); uid.String() != "00000000-0000-0000-0000-000000000000" {
						h.Set("Authorization", "Bearer "+serviceToken)
						h.Set(headerUserID, uid.String())
						if sid := c.SessionID(); sid != "" {
							h.Set(headerSessionID, sid)
						}
						if email := c.PrimaryEmail; email != "" {
							h.Set(headerUserEmail, email)
						}
						if len(c.Roles) > 0 {
							h.Set(headerUserRoles, joinCSV(c.Roles))
						}
					}
				}
			}
			return next(ctx, req)
		}
	}
}

// Header names mirror the ones identity-svc's middleware reads.
// Lowercased to canonical HTTP form so Go's Header.Set normalises
// without a Set-vs-Add ordering surprise.
const (
	headerUserID     = "X-Iogrid-User-Id"
	headerSessionID  = "X-Iogrid-Session-Id"
	headerUserRoles  = "X-Iogrid-User-Roles"
	headerUserEmail  = "X-Iogrid-User-Email"
)

// joinCSV joins a slice with commas without allocating an
// intermediate strings.Builder for the common 1-element case.
func joinCSV(in []string) string {
	switch len(in) {
	case 0:
		return ""
	case 1:
		return in[0]
	}
	n := len(in) - 1
	for _, s := range in {
		n += len(s)
	}
	b := make([]byte, 0, n)
	for i, s := range in {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, s...)
	}
	return string(b)
}
