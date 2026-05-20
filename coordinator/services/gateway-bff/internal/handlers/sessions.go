// sessions.go: /api/v1/account/sessions surface.
//
// Issue #322: /account/sessions in the web management plane always
// rendered "No active sessions besides this one" because the upstream
// AuthService.ListSessions returned CodeUnimplemented. The fix is
// landed in identity-svc; gateway-bff only needs to (a) forward
// the caller's identity downstream (via the clients.WithCallerClaims
// pre-step + the header-forwarding interceptor), and (b) expose a
// matching DELETE route so the UI's Revoke button has somewhere to
// land.
//
// GET /api/v1/account/sessions is already served by account.go's
// ListSessions; this file owns DELETE /api/v1/account/sessions/{id}.
// Co-located so a future read+write split (audit log, paginated list,
// "revoke all other sessions" bulk action) lives in one place.
//
// NOTE: account.go's ListSessions predates this file and was the
// scaffolding the issue reports as broken; the actual call path is
// fixed by the identity-svc wiring + the header-forwarding
// interceptor in clients/, not by changes here. We re-export an
// updated handler below that also attaches the caller's claims so
// the interceptor can stamp them — earlier callers relied on
// gateway-bff calling identity-svc anonymously, which only works
// when no auth scoping is required.
package handlers

import (
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
)

// ListSessionsForAccount returns every active session for the caller.
// Wraps account.go's ListSessions to also seed clients.WithCallerClaims
// so the header-forwarding interceptor can stamp the caller's
// X-Iogrid-User-Id + X-Iogrid-Session-Id on the outbound Connect-RPC
// call. account.go's older ListSessions is kept for binary-compat with
// existing tests but routes.go is repointed to this one.
//
//	GET /api/v1/account/sessions
//	-> 200 { sessions: [{id, ip_address, user_agent, created_at,
//	                     last_used_at, expires_at, is_current}] }
//	   401 unauthenticated
func (a *API) ListSessionsForAccount(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	ctx := clients.WithCallerClaims(r.Context(), claims)
	resp, err := a.Clients.Auth.ListSessions(ctx, &identityv1.ListSessionsRequest{})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// RevokeAccountSession soft-revokes one of the caller's sessions.
// Ownership and "cannot revoke your own current session" are enforced
// in identity-svc; the BFF only forwards.
//
//	DELETE /api/v1/account/sessions/{id}
//	-> 200 {}
//	   401 unauthenticated
//	   404 not_found (session does not exist or is not owned by caller)
//	   409 cannot_revoke_current (must sign out instead)
func (a *API) RevokeAccountSession(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return
	}
	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "session id required")
		return
	}
	ctx := clients.WithCallerClaims(r.Context(), claims)
	_, err := a.Clients.Auth.RevokeSession(ctx, &identityv1.RevokeSessionRequest{
		SessionId: &commonv1.UUID{Value: sessionID},
	})
	if err != nil {
		// Translate the Connect codes the upstream emits onto the
		// HTTP shapes the panel renders. writeUpstreamError would
		// land 4xx as 5xx for CodeFailedPrecondition, which the UI
		// needs to distinguish.
		var cErr *connect.Error
		if errors.As(err, &cErr) {
			switch cErr.Code() {
			case connect.CodeFailedPrecondition:
				writeError(w, http.StatusConflict, "cannot_revoke_current", cErr.Message())
				return
			case connect.CodeNotFound:
				writeError(w, http.StatusNotFound, "not_found", cErr.Message())
				return
			case connect.CodeInvalidArgument:
				writeError(w, http.StatusBadRequest, "invalid_argument", cErr.Message())
				return
			case connect.CodeUnauthenticated:
				writeError(w, http.StatusUnauthorized, "unauthenticated", cErr.Message())
				return
			}
		}
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
