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
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1/identityv1connect"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/auth"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/siws"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// errCannotRevokeCurrent is returned when a caller tries to revoke
// their own active session via the /account/sessions surface. The user
// must sign out instead so the refresh-token cookie clears in the
// browser. Surfaced as CodeFailedPrecondition / HTTP 409.
var errCannotRevokeCurrent = errors.New("identity-svc: cannot revoke the caller's own current session — sign out instead")

// AuthHandler implements the subset of identityv1connect.AuthServiceHandler
// that backs /account/sessions plus the SIWS wallet-binding RPCs that
// back /account/wallets (issue #326). Other RPCs fall through to
// UnimplementedAuthServiceHandler so the service compiles + responds
// with CodeUnimplemented until each is wired through to the same store
// (tracked under EPIC #309).
type AuthHandler struct {
	identityv1connect.UnimplementedAuthServiceHandler
	Store *store.Store
	// Auth is the auth.Service that owns the SIWS challenge store +
	// transactional bind / unbind logic. Optional so legacy test
	// fixtures that only need ListSessions / RevokeSession can pass
	// nil; the wallet RPCs return CodeUnavailable when it's missing.
	Auth *auth.Service
}

// NewAuthHandler wires the dependencies. authSvc may be nil for test
// fixtures that don't exercise the wallet RPCs; in that case the SIWS
// methods return CodeUnavailable rather than panicking.
func NewAuthHandler(s *store.Store, authSvc *auth.Service) *AuthHandler {
	return &AuthHandler{Store: s, Auth: authSvc}
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

// --- SIWS wallet binding (Connect-RPC) ---------------------------------
//
// Issue #326: /account/wallets was hollow because gateway-bff couldn't
// reach a Connect-RPC method that wraps auth.Service.StartSiwsBinding /
// CompleteSiwsBinding / ListBoundWallets / UnbindWallet. The chi JSON
// surface in handlers.go already shipped these, but the BFF's
// per-service AuthClient adapter is Connect-RPC only — so the surface
// fell through to UnimplementedAuthServiceHandler.
//
// The Connect-RPC twins below delegate to the same auth.Service that
// backs the chi JSON tree, so both transport paths see the same
// transactional logic (challenge consume → signature verify → identifier
// insert) and the same replay defence.

// StartSiwsBinding mints a fresh SIWS challenge (32-byte nonce + canonical
// message) for the caller to sign with their wallet. The caller MUST be
// authenticated; the wallet binds to the bearer's user_id. Anonymous
// binds (first-time wallet sign-in) are reserved for the chi JSON
// /v1/auth/siws/start path — Connect-RPC callers are always coming from
// gateway-bff with a materialised user.
func (h *AuthHandler) StartSiwsBinding(
	ctx context.Context,
	req *connect.Request[identityv1.StartSiwsBindingRequest],
) (*connect.Response[identityv1.StartSiwsBindingResponse], error) {
	if h.Auth == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("identity-svc: auth service not configured"))
	}
	userID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	// When the client supplies a user_id, lock it to the authed
	// principal — the caller cannot mint a challenge for someone else
	// (matches the chi JSON handler's defence in handlers.go).
	if got := req.Msg.GetUserId(); got != nil && got.GetValue() != "" {
		gotID, err := uuid.Parse(got.GetValue())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("bad user_id: %w", err))
		}
		if gotID != userID {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("user_id does not match bearer"))
		}
	}
	res, err := h.Auth.StartSiwsBinding(ctx, userID, req.Msg.GetWalletAddress())
	if err != nil {
		return nil, mapSiwsError(err)
	}
	return connect.NewResponse(&identityv1.StartSiwsBindingResponse{
		Challenge: res.Challenge,
		ExpiresAt: timestamppb.New(res.ExpiresAt),
	}), nil
}

// CompleteSiwsBinding verifies the wallet's ed25519 signature over the
// previously-issued challenge, consumes the challenge atomically (GETDEL
// in Redis — replay defence), and inserts an Identifier row with
// kind=solana. Reject paths: bad signature → CodeUnauthenticated;
// expired / unknown challenge → CodeFailedPrecondition; wallet bound
// to a different user → CodePermissionDenied.
func (h *AuthHandler) CompleteSiwsBinding(
	ctx context.Context,
	req *connect.Request[identityv1.CompleteSiwsBindingRequest],
) (*connect.Response[identityv1.CompleteSiwsBindingResponse], error) {
	if h.Auth == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("identity-svc: auth service not configured"))
	}
	userID, ok := authmw.AuthedUser(ctx)
	if !ok && !req.Msg.GetCreateIfMissing() {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	if got := req.Msg.GetUserId(); got != nil && got.GetValue() != "" {
		gotID, err := uuid.Parse(got.GetValue())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("bad user_id: %w", err))
		}
		if ok && gotID != userID {
			return nil, connect.NewError(connect.CodePermissionDenied, errors.New("user_id does not match bearer"))
		}
		userID = gotID
	}
	// Connect-RPC server handlers don't see an *http.Request — pass nil
	// to CompleteSiwsBinding; the bundle-issue path only uses it for IP
	// / user-agent capture which is best-effort.
	res, err := h.Auth.CompleteSiwsBinding(ctx, userID, req.Msg.GetWalletAddress(), req.Msg.GetSignature(), req.Msg.GetCreateIfMissing(), nil)
	if err != nil {
		return nil, mapSiwsError(err)
	}
	resp := &identityv1.CompleteSiwsBindingResponse{
		Binding: &identityv1.WalletBinding{
			Id:         &commonv1.UUID{Value: res.IdentifierID.String()},
			UserId:     &commonv1.UUID{Value: res.UserID.String()},
			Address:    res.Address,
			CreatedAt:  timestamppb.New(res.BoundAt),
			LastUsedAt: timestamppb.New(res.BoundAt),
		},
		NewUser: res.NewUser,
	}
	if res.Bundle != nil {
		resp.Bundle = bundleToProto(res.Bundle)
	}
	return connect.NewResponse(resp), nil
}

// ListBoundWallets returns every Solana wallet bound to the caller.
// Ownership is taken from the bearer — the request's user_id is
// ignored when present so callers can't enumerate other users'
// wallets even with a leaked Connect-RPC client.
func (h *AuthHandler) ListBoundWallets(
	ctx context.Context,
	_ *connect.Request[identityv1.ListBoundWalletsRequest],
) (*connect.Response[identityv1.ListBoundWalletsResponse], error) {
	if h.Auth == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("identity-svc: auth service not configured"))
	}
	userID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	bindings, err := h.Auth.ListBoundWallets(ctx, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := make([]*identityv1.WalletBinding, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, &identityv1.WalletBinding{
			Id:         &commonv1.UUID{Value: b.ID.String()},
			UserId:     &commonv1.UUID{Value: b.UserID.String()},
			Address:    b.Subject,
			CreatedAt:  timestamppb.New(b.CreatedAt),
			LastUsedAt: timestamppb.New(b.LastUsedAt),
		})
	}
	return connect.NewResponse(&identityv1.ListBoundWalletsResponse{Bindings: out}), nil
}

// UnbindWallet removes a Solana identifier from the caller's user.
// Ownership is asserted in the WHERE clause (DELETE FROM identifiers
// WHERE kind='solana' AND subject=$addr AND user_id=$caller) so a
// missing row is indistinguishable from "not yours" — anti-enumeration.
func (h *AuthHandler) UnbindWallet(
	ctx context.Context,
	req *connect.Request[identityv1.UnbindWalletRequest],
) (*connect.Response[identityv1.UnbindWalletResponse], error) {
	if h.Auth == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("identity-svc: auth service not configured"))
	}
	userID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	if err := h.Auth.UnbindWallet(ctx, userID, req.Msg.GetWalletAddress()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&identityv1.UnbindWalletResponse{}), nil
}

// mapSiwsError translates auth/siws sentinels into Connect codes so
// the BFF can surface 400 / 401 / 403 / 412 distinctly. Anything else
// becomes CodeInternal so the operator sees the raw error in logs.
func mapSiwsError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, siws.ErrInvalidAddress):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, siws.ErrInvalidSignature):
		return connect.NewError(connect.CodeUnauthenticated, err)
	case errors.Is(err, siws.ErrChallengeNotFound):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, store.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, err)
	}
	// Permission-denied sentinels carried as strings by auth.Service
	// (the SIWS layer pre-dates a typed sentinel for this case).
	msg := err.Error()
	if strings.Contains(msg, "bound to another user") ||
		strings.Contains(msg, "does not match challenge") ||
		strings.Contains(msg, "anonymous bind requires") {
		return connect.NewError(connect.CodePermissionDenied, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// bundleToProto converts an auth.Bundle to its proto twin. Mirrors
// bundleToJSON in handlers.go so the Connect-RPC + chi JSON surfaces
// emit the same fields (modulo wire format). User fields are restricted
// to what the WalletBinding caller cares about; the caller can fetch
// the full User via the AuthService.RefreshToken path if they need it.
func bundleToProto(b *auth.Bundle) *identityv1.AuthBundle {
	if b == nil {
		return nil
	}
	out := &identityv1.AuthBundle{
		AccessToken:           b.AccessToken,
		AccessTokenExpiresAt:  timestamppb.New(b.AccessTokenExpiresAt),
		RefreshToken:          b.RefreshToken,
		RefreshTokenExpiresAt: timestamppb.New(b.RefreshTokenExpiresAt),
	}
	if b.User != nil {
		out.User = &identityv1.User{
			Id:           &commonv1.UUID{Value: b.User.ID.String()},
			PrimaryEmail: b.User.PrimaryEmail,
			DisplayName:  b.User.DisplayName,
			PictureUrl:   b.User.PictureURL,
		}
	}
	return out
}
