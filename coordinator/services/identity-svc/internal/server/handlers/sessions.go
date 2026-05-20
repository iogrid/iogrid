// sessions.go: AuthService.{ListSessions, RevokeSession} Connect-RPC
// implementations + the JSON twin used by /v1/account/sessions
// callers and the e2e suite.
//
// Issue #322: prior to this file the /account/sessions UI always
// rendered "No active sessions besides this one" because gateway-bff's
// Connect-RPC call to identity-svc landed on UnimplementedAuthService
// — the AuthServiceHandler was never wired into routes.go. The fix
// ships:
//
//  1. AuthHandler.ListSessions: returns every non-revoked, non-expired
//     session bound to the caller's user_id, with is_current set on the
//     row matching the caller's session id (`jti` on a real JWT or
//     X-Iogrid-Session-Id on the service-token shim path).
//  2. AuthHandler.RevokeSession: validates ownership in the WHERE
//     clause (UPDATE sessions SET revoked_at=now() WHERE id=$1 AND
//     user_id=$2) so a SELECT-then-UPDATE race is impossible, and
//     refuses to revoke the caller's own current session — the user
//     must end the current session via the normal sign-out flow so
//     the refresh-token cookie clears.
//
// Authorization model: both RPCs require an authed user in context.
// gateway-bff threads X-Iogrid-User-Id + X-Iogrid-Session-Id via the
// existing service-token shim; the middleware's JWT path also
// materialises these from the bearer claims.
//
// Other AuthService RPCs (sign-in flows, refresh, sign-out, step-up,
// SIWS) keep returning CodeUnimplemented from the embedded
// UnimplementedAuthServiceHandler — they're tracked under their own
// EPIC #309 issues and reachable today via the chi JSON tree in
// handlers.go. Bundling them in here would expand the blast radius
// without unblocking the surface this issue tracks.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1/identityv1connect"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// errCannotRevokeCurrent is returned when a caller tries to revoke
// their own active session via the /account/sessions surface. The user
// must sign out instead so the refresh-token cookie clears in the
// browser. Surfaced as CodeFailedPrecondition / HTTP 409.
var errCannotRevokeCurrent = errors.New("identity-svc: cannot revoke the caller's own current session — sign out instead")

// AuthHandler implements the subset of identityv1connect.AuthServiceHandler
// that backs /account/sessions. Other RPCs fall through to
// UnimplementedAuthServiceHandler so the service compiles + responds
// with CodeUnimplemented until each is wired through to the same store
// (tracked under EPIC #309).
type AuthHandler struct {
	identityv1connect.UnimplementedAuthServiceHandler
	Store *store.Store
}

// NewAuthHandler wires the dependency.
func NewAuthHandler(s *store.Store) *AuthHandler {
	return &AuthHandler{Store: s}
}

// --- Connect-Go entry points --------------------------------------------

// ListSessions returns every active session for the caller. The caller's
// own session row is flagged is_current=true so the UI can render the
// "Current session" pill + disable its Revoke button. Sessions are
// ordered by last_used_at desc (NULLS LAST) so the most recently used
// row appears first.
func (h *AuthHandler) ListSessions(
	ctx context.Context,
	_ *connect.Request[identityv1.ListSessionsRequest],
) (*connect.Response[identityv1.ListSessionsResponse], error) {
	userID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	if h.Store == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("identity-svc: store not configured"))
	}
	sessions, err := h.Store.ListSessionsForUser(ctx, nil, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	currentSID, _ := authmw.AuthedSessionID(ctx)
	return connect.NewResponse(&identityv1.ListSessionsResponse{
		Sessions: sessionsToProto(sortSessionsByRecency(sessions), currentSID),
	}), nil
}

// RevokeSession soft-revokes a single session belonging to the caller.
// Ownership is asserted directly in the UPDATE WHERE clause (no
// separate SELECT then UPDATE) so a concurrent revoke can't race the
// ownership check. The caller's own current session is refused — the
// browser must sign out via the normal flow so the refresh-token cookie
// clears alongside the server-side revocation.
func (h *AuthHandler) RevokeSession(
	ctx context.Context,
	req *connect.Request[identityv1.RevokeSessionRequest],
) (*connect.Response[identityv1.RevokeSessionResponse], error) {
	userID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	// Validate input + self-revoke gate BEFORE touching the store so a
	// malformed request fails the same way regardless of store state.
	sessionID, err := parseProtoUUID(req.Msg.GetSessionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if currentSID, ok := authmw.AuthedSessionID(ctx); ok && currentSID == sessionID {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errCannotRevokeCurrent)
	}
	if h.Store == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("identity-svc: store not configured"))
	}
	if err := h.revokeOwnedSessionTx(ctx, userID, sessionID); err != nil {
		return nil, mapRevokeSessionError(err)
	}
	return connect.NewResponse(&identityv1.RevokeSessionResponse{}), nil
}

// --- chi JSON surface ---------------------------------------------------

// MountSessionsJSON wires GET /v1/account/sessions + DELETE
// /v1/account/sessions/{sessionID} onto the supplied router. The
// envelopes mirror what handlers.go emits for sibling endpoints so the
// e2e suite can rely on a single shape.
func (h *AuthHandler) MountSessionsJSON(r chi.Router) {
	r.Route("/account/sessions", func(r chi.Router) {
		r.Get("/", h.jsonListSessions)
		r.Delete("/{sessionID}", h.jsonRevokeSession)
	})
}

func (h *AuthHandler) jsonListSessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	if h.Store == nil {
		writeError(w, http.StatusInternalServerError, "internal", "store not configured")
		return
	}
	sessions, err := h.Store.ListSessionsForUser(r.Context(), nil, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	currentSID, _ := authmw.AuthedSessionID(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"sessions": sessionsToJSON(sortSessionsByRecency(sessions), currentSID),
	})
}

func (h *AuthHandler) jsonRevokeSession(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	// Validate input + self-revoke gate BEFORE touching the store so
	// a malformed request fails the same way regardless of store
	// state (and so unit tests can pin these contracts without a
	// real Postgres pool).
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad session id")
		return
	}
	if currentSID, ok := authmw.AuthedSessionID(r.Context()); ok && currentSID == sessionID {
		writeError(w, http.StatusConflict, "cannot_revoke_current", errCannotRevokeCurrent.Error())
		return
	}
	if h.Store == nil {
		writeError(w, http.StatusInternalServerError, "internal", "store not configured")
		return
	}
	if err := h.revokeOwnedSessionTx(r.Context(), userID, sessionID); err != nil {
		writeRevokeSessionError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

// --- shared logic -------------------------------------------------------

// revokeOwnedSessionTx flips revoked_at=now() iff the session is bound
// to the caller AND still live. Implemented as a single UPDATE so the
// ownership check + the revoke happen in one round-trip, with no
// SELECT-then-UPDATE race window. Returns ErrNotFound when zero rows
// matched — same surface for "not found" and "not owned" to keep the
// response leak-free (the caller can't enumerate other users' UUIDs).
func (h *AuthHandler) revokeOwnedSessionTx(ctx context.Context, userID, sessionID uuid.UUID) error {
	return h.Store.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE sessions
			   SET revoked_at = now()
			 WHERE id = $1
			   AND user_id = $2
			   AND revoked_at IS NULL
			   AND expires_at > now()`, sessionID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}

// --- conversion helpers -------------------------------------------------

// sortSessionsByRecency returns a copy of the input ordered by
// last_used_at desc (NULLS LAST). The store query already orders by
// created_at desc; we re-sort here so the UI's "most recently active"
// pill matches a session row even when last_used_at and created_at
// diverge (e.g. a long-lived daemon session refreshed many times).
func sortSessionsByRecency(in []store.Session) []store.Session {
	out := make([]store.Session, len(in))
	copy(out, in)
	// Simple insertion sort: list is bounded by max-sessions-per-user
	// (small N — typically <10) so allocating a Less closure for
	// sort.Slice would be more code than it's worth here.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && sessionRecencyLess(out[j-1], out[j]); j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// sessionRecencyLess reports whether a < b for the recency sort. We
// want "most recent first" so we invert: a is "less" (i.e. should
// come AFTER) when its last_used_at is older than b's. Zero
// last_used_at sorts last (NULLS LAST equivalent).
func sessionRecencyLess(a, b store.Session) bool {
	if a.LastUsedAt.IsZero() && !b.LastUsedAt.IsZero() {
		return true
	}
	if !a.LastUsedAt.IsZero() && b.LastUsedAt.IsZero() {
		return false
	}
	return a.LastUsedAt.Before(b.LastUsedAt)
}

func sessionsToProto(in []store.Session, currentSID uuid.UUID) []*identityv1.Session {
	out := make([]*identityv1.Session, 0, len(in))
	for _, s := range in {
		entry := &identityv1.Session{
			Id:         &commonv1.UUID{Value: s.ID.String()},
			UserId:     &commonv1.UUID{Value: s.UserID.String()},
			UserAgent:  s.UserAgent,
			CreatedAt:  timestamppb.New(s.CreatedAt),
			LastUsedAt: timestamppb.New(s.LastUsedAt),
			ExpiresAt:  timestamppb.New(s.ExpiresAt),
			IsCurrent:  s.ID == currentSID,
		}
		if s.IP != nil {
			entry.IpAddress = s.IP.String()
		}
		out = append(out, entry)
	}
	return out
}

func sessionsToJSON(in []store.Session, currentSID uuid.UUID) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, s := range in {
		entry := map[string]any{
			"id":           s.ID.String(),
			"user_id":      s.UserID.String(),
			"user_agent":   s.UserAgent,
			"created_at":   s.CreatedAt.UTC().Format(time.RFC3339Nano),
			"last_used_at": s.LastUsedAt.UTC().Format(time.RFC3339Nano),
			"expires_at":   s.ExpiresAt.UTC().Format(time.RFC3339Nano),
			"is_current":   s.ID == currentSID,
		}
		if s.IP != nil {
			entry["ip_address"] = s.IP.String()
		}
		out = append(out, entry)
	}
	return out
}

// --- error mapping ------------------------------------------------------

func mapRevokeSessionError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	if errors.Is(err, errCannotRevokeCurrent) {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

func writeRevokeSessionError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, errCannotRevokeCurrent) {
		writeError(w, http.StatusConflict, "cannot_revoke_current", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", err.Error())
}
