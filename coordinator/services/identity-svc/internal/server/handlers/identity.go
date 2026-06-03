// identity.go: post-auth account-management RPCs that the JSON tree in
// handlers.go does not yet cover — specifically the Remove-Identifier
// and Delete-Account flows used by /account/identifiers and
// /account/danger-zone in the web management plane.
//
// Mirrors the WorkspaceHandler pattern: one struct, two surfaces.
//
//  1. Connect-Go handler that satisfies identityv1connect.IdentityServiceHandler
//     so gateway-bff (and any future cross-service caller) can invoke
//     the RPCs via the generated stubs.
//  2. Parallel chi JSON tree mounted under /v1 (same envelope shape
//     handlers.go already emits) for e2e tests and direct curl callers.
//
// Authorization model:
//   - Both RPCs require a valid bearer token (caller is the user-of-record).
//   - RemoveIdentifier refuses to remove the last *verified* identifier;
//     the user would be locked out otherwise.
//   - DeleteAccount requires the bearer's JWT to carry step_up=true. We
//     also accept a `step_up_token` field on the request body for
//     forward-compat with the proto contract, but the cryptographic
//     check is on the JWT (which is what gateway-bff actually presents
//     after a step-up flow rotates the access token).
package handlers

import (
	"context"
	"encoding/json"
	"errors"
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
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// IdentityHandler implements identityv1connect.IdentityServiceHandler.
// Ships GetUser + RemoveIdentifier + DeleteAccount; the remaining
// methods (ListUsers, UpdateUser, DeleteUser, MergeIdentities) stay on
// UnimplementedIdentityServiceHandler so the service compiles +
// responds with CodeUnimplemented until each is wired through to the
// same store.
type IdentityHandler struct {
	identityv1connect.UnimplementedIdentityServiceHandler
	Store *store.Store
}

// NewIdentityHandler wires the dependency.
func NewIdentityHandler(s *store.Store) *IdentityHandler {
	return &IdentityHandler{Store: s}
}

// --- Connect-Go entry points --------------------------------------------

// GetUser returns the canonical User record + every bound Identifier for
// the supplied user id. Authorization:
//
//   - With no id (zero UUID): resolves to the caller's own record. This
//     is the path gateway-bff exercises for /api/v1/me — the BFF passes
//     claims.UserID() through but the handler accepts an empty id for
//     defence-in-depth so future "/api/v1/me" callers cannot leak the
//     authed user id by accident.
//   - With id == caller: same as above — caller reading own record.
//   - With id != caller: requires the caller to hold the
//     USER_ROLE_ADMIN role; otherwise CodePermissionDenied. Cross-user
//     reads from the management plane go through this branch (admin
//     directory; user-impersonation flows).
//
// CodeNotFound surfaces when the requested user has been soft-deleted
// or never existed — the store layer maps pgx.ErrNoRows to
// store.ErrNotFound which mapStoreError turns into the right Connect
// code.
func (h *IdentityHandler) GetUser(
	ctx context.Context,
	req *connect.Request[identityv1.GetUserRequest],
) (*connect.Response[identityv1.GetUserResponse], error) {
	callerID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}

	// Resolve the target user id. An empty / nil id resolves to the
	// caller — this is what gateway-bff.GetMe relies on after stripping
	// the bearer.
	targetID := callerID
	if req.Msg.GetId().GetValue() != "" {
		parsed, err := uuid.Parse(req.Msg.GetId().GetValue())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		targetID = parsed
	}

	// Cross-user reads require admin. Self-reads are always allowed.
	if targetID != callerID && !callerHasRole(ctx, "USER_ROLE_ADMIN") {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("cross-user GetUser requires admin"))
	}

	if h.Store == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("identity-svc: store not configured"))
	}
	u, err := h.Store.GetUser(ctx, nil, targetID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	ids, err := h.Store.ListIdentifiersForUser(ctx, nil, targetID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return connect.NewResponse(&identityv1.GetUserResponse{
		User:        userToProto(u),
		Identifiers: identifiersToProto(ids),
	}), nil
}

// RemoveIdentifier deletes a single identifier owned by the caller. The
// handler enforces "at least one verified identifier remains" inside a
// serializable transaction; we surface that as Connect's
// CodeFailedPrecondition (HTTP 409 on the JSON twin).
func (h *IdentityHandler) RemoveIdentifier(
	ctx context.Context,
	req *connect.Request[identityv1.RemoveIdentifierRequest],
) (*connect.Response[identityv1.RemoveIdentifierResponse], error) {
	authedID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	userID, err := parseProtoUUID(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if userID != authedID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("user_id does not match caller"))
	}
	identifierID, err := parseProtoUUID(req.Msg.GetIdentifierId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	remaining, err := h.removeIdentifierTx(ctx, userID, identifierID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return connect.NewResponse(&identityv1.RemoveIdentifierResponse{
		Remaining: identifiersToProto(remaining),
	}), nil
}

// EnsureIdentifier idempotently binds an identifier to the caller's own
// account. This is the registration half the web's NextAuth magic-link
// flow was missing (#685): web sign-in authenticates outside identity-svc
// (AuthService.CompleteMagicLink is not in that path), so magic-link users
// existed with zero identifier rows — /account/identifiers told signed-in
// users "No identifiers bound". gateway-bff calls this from NextAuth's
// signIn event; re-calls are no-ops (created=false), so existing accounts
// heal on their next sign-in.
func (h *IdentityHandler) EnsureIdentifier(
	ctx context.Context,
	req *connect.Request[identityv1.EnsureIdentifierRequest],
) (*connect.Response[identityv1.EnsureIdentifierResponse], error) {
	authedID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	userID, err := parseProtoUUID(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if userID != authedID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("user_id does not match caller"))
	}
	kind := identifierKindFromProto(req.Msg.GetKind())
	if kind == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("kind required"))
	}
	email := strings.ToLower(strings.TrimSpace(req.Msg.GetVerifiedEmail()))
	subject := strings.TrimSpace(req.Msg.GetSubject())
	if email == "" && subject == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("verified_email or subject required"))
	}
	if h.Store == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("identity-svc: store not configured"))
	}

	// Idempotency: OAuth kinds match on subject; magic-link matches on the
	// (case-insensitive) email.
	existing, err := h.Store.ListIdentifiersForUserByKind(ctx, nil, userID, kind)
	if err != nil {
		return nil, mapStoreError(err)
	}
	for i := range existing {
		e := existing[i]
		if (subject != "" && e.Subject == subject) ||
			(subject == "" && strings.EqualFold(e.Email, email)) {
			return connect.NewResponse(&identityv1.EnsureIdentifierResponse{
				Identifier: identifiersToProto([]store.Identifier{e})[0],
				Created:    false,
			}), nil
		}
	}

	ident := &store.Identifier{
		UserID:   userID,
		Kind:     kind,
		Subject:  subject,
		Email:    email,
		Verified: true, // magic-link: inbox control proven by clicking the link
	}
	if err := h.Store.CreateIdentifier(ctx, nil, ident); err != nil {
		return nil, mapStoreError(err)
	}
	return connect.NewResponse(&identityv1.EnsureIdentifierResponse{
		Identifier: identifiersToProto([]store.Identifier{*ident})[0],
		Created:    true,
	}), nil
}

// DeleteAccount soft-deletes the caller's user row + revokes every live
// session. Downstream purge (workspace cascade, billing wind-down) is
// expected to land via the outbound-events bus emitted by future
// migrations of this transaction; this PR ships the identity-svc-local
// half of the cascade.
func (h *IdentityHandler) DeleteAccount(
	ctx context.Context,
	req *connect.Request[identityv1.DeleteAccountRequest],
) (*connect.Response[identityv1.DeleteAccountResponse], error) {
	authedID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	userID, err := parseProtoUUID(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if userID != authedID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("user_id does not match caller"))
	}
	if !h.hasStepUp(ctx, req.Msg.GetStepUpToken()) {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("step_up_required"))
	}
	deletedAt, revoked, err := h.deleteAccountTx(ctx, userID)
	if err != nil {
		return nil, mapStoreError(err)
	}
	return connect.NewResponse(&identityv1.DeleteAccountResponse{
		DeletedAt:       timestamppb.New(deletedAt),
		SessionsRevoked: uint32(revoked),
	}), nil
}

// --- chi JSON surface ---------------------------------------------------

// MountIdentityJSON wires DELETE /v1/users/{userID}/identifiers/{identifierID}
// and DELETE /v1/users/{userID} onto the supplied router. The routes
// mirror the Connect contracts above; the JSON envelopes are identical
// to what handlers.go emits elsewhere so the e2e suite can rely on a
// single shape.
func (h *IdentityHandler) MountIdentityJSON(r chi.Router) {
	r.Route("/users/{userID}", func(r chi.Router) {
		r.Delete("/identifiers/{identifierID}", h.jsonRemoveIdentifier)
		r.Delete("/", h.jsonDeleteAccount)
	})
}

func (h *IdentityHandler) jsonRemoveIdentifier(w http.ResponseWriter, r *http.Request) {
	authedID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad user id")
		return
	}
	if userID != authedID {
		writeError(w, http.StatusForbidden, "permission_denied", "user_id does not match caller")
		return
	}
	identifierID, err := uuid.Parse(chi.URLParam(r, "identifierID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad identifier id")
		return
	}
	remaining, err := h.removeIdentifierTx(r.Context(), userID, identifierID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"remaining": identifiersToJSON(remaining),
	})
}

type deleteAccountBody struct {
	StepUpToken string `json:"step_up_token"`
	Reason      string `json:"reason"`
}

func (h *IdentityHandler) jsonDeleteAccount(w http.ResponseWriter, r *http.Request) {
	authedID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad user id")
		return
	}
	if userID != authedID {
		writeError(w, http.StatusForbidden, "permission_denied", "user_id does not match caller")
		return
	}
	var body deleteAccountBody
	if r.Body != http.NoBody {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	if !h.hasStepUp(r.Context(), body.StepUpToken) {
		writeError(w, http.StatusForbidden, "step_up_required", "step-up auth required")
		return
	}
	deletedAt, revoked, err := h.deleteAccountTx(r.Context(), userID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted_at":       deletedAt.UTC().Format(time.RFC3339Nano),
		"sessions_revoked": revoked,
	})
}

// --- shared logic -------------------------------------------------------

// removeIdentifierTx executes the lookup + survivor-check + delete +
// re-list inside one serializable transaction so a concurrent remove
// cannot race the survivor count.
func (h *IdentityHandler) removeIdentifierTx(ctx context.Context, userID, identifierID uuid.UUID) ([]store.Identifier, error) {
	if h.Store == nil {
		return nil, errors.New("identity-svc: store not configured")
	}
	var out []store.Identifier
	txErr := h.Store.WithTx(ctx, func(tx pgx.Tx) error {
		current, qerr := h.Store.GetIdentifierForUser(ctx, tx, identifierID, userID)
		if qerr != nil {
			return qerr
		}
		all, qerr := h.Store.ListIdentifiersForUser(ctx, tx, userID)
		if qerr != nil {
			return qerr
		}
		verified := 0
		for _, i := range all {
			if i.Verified {
				verified++
			}
		}
		if current.Verified && verified <= 1 {
			return errLastIdentifier
		}
		if qerr := h.Store.DeleteIdentifier(ctx, tx, identifierID); qerr != nil {
			return qerr
		}
		remaining, qerr := h.Store.ListIdentifiersForUser(ctx, tx, userID)
		if qerr != nil {
			return qerr
		}
		out = remaining
		return nil
	})
	return out, txErr
}

// deleteAccountTx flips users.deleted_at + revokes every active session
// in one serializable transaction.
func (h *IdentityHandler) deleteAccountTx(ctx context.Context, userID uuid.UUID) (time.Time, int, error) {
	if h.Store == nil {
		return time.Time{}, 0, errors.New("identity-svc: store not configured")
	}
	var deletedAt time.Time
	var revoked int
	txErr := h.Store.WithTx(ctx, func(tx pgx.Tx) error {
		sessions, qerr := h.Store.ListSessionsForUser(ctx, tx, userID)
		if qerr != nil {
			return qerr
		}
		for _, s := range sessions {
			if qerr := h.Store.RevokeSession(ctx, tx, s.ID); qerr != nil {
				return qerr
			}
		}
		revoked = len(sessions)
		if qerr := h.Store.SoftDeleteUser(ctx, tx, userID); qerr != nil {
			return qerr
		}
		u, qerr := h.Store.GetUser(ctx, tx, userID)
		if qerr != nil {
			return qerr
		}
		if u.DeletedAt != nil {
			deletedAt = *u.DeletedAt
		}
		return nil
	})
	return deletedAt, revoked, txErr
}

// hasStepUp returns true when the caller's JWT carries step_up=true.
// The proto-defined step_up_token field is accepted for forward-compat
// — a future flow that mints opaque step-up tokens can validate them
// here without changing call sites — but today we trust the JWT.
func (h *IdentityHandler) hasStepUp(ctx context.Context, _ string) bool {
	claims, ok := authmw.AuthedClaims(ctx)
	if !ok || claims == nil {
		return false
	}
	return claims.StepUp
}

// --- conversion helpers -------------------------------------------------

func parseProtoUUID(u *commonv1.UUID) (uuid.UUID, error) {
	if u == nil || u.GetValue() == "" {
		return uuid.Nil, errors.New("uuid required")
	}
	return uuid.Parse(u.GetValue())
}

// callerHasRole returns true when the authed user's JWT claims carry
// the supplied SCREAMING_SNAKE_CASE role. Used for cross-user GetUser
// gating; admin self-impersonation flows pass through.
//
// On the service-token shim path (used by the Phase 0 Next.js BFF) no
// claims are injected, so role-gated branches default to denied. That
// matches the threat model — the BFF only ever speaks for its own
// browser session, never on behalf of an admin acting on another user.
func callerHasRole(ctx context.Context, role string) bool {
	claims, ok := authmw.AuthedClaims(ctx)
	if !ok || claims == nil {
		return false
	}
	for _, r := range claims.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// userToProto materialises a store.User as its identity.v1.User proto
// counterpart. Optional timestamps (LastLoginAt, DeletedAt) round-trip
// as nil when unset — proto3 lets the field stay absent on the wire.
func userToProto(u *store.User) *identityv1.User {
	if u == nil {
		return nil
	}
	out := &identityv1.User{
		Id:           &commonv1.UUID{Value: u.ID.String()},
		PrimaryEmail: u.PrimaryEmail,
		DisplayName:  u.DisplayName,
		PictureUrl:   u.PictureURL,
		Roles:        rolesFromStrings(u.Roles),
		CreatedAt:    timestamppb.New(u.CreatedAt),
		UpdatedAt:    timestamppb.New(u.UpdatedAt),
	}
	if u.LastLoginAt != nil {
		out.LastLoginAt = timestamppb.New(*u.LastLoginAt)
	}
	if u.DeletedAt != nil {
		out.DeletedAt = timestamppb.New(*u.DeletedAt)
	}
	if u.NotificationPrefs != nil {
		out.NotificationPrefs = *u.NotificationPrefs
	}
	return out
}

// rolesFromStrings parses the SCREAMING_SNAKE_CASE role names stored
// in users.roles into their proto3 UserRole enum values. Unknown
// strings map to USER_ROLE_UNSPECIFIED — a new role added in the proto
// but not yet rolled out to the store should not panic the handler.
func rolesFromStrings(in []string) []identityv1.UserRole {
	if len(in) == 0 {
		return nil
	}
	out := make([]identityv1.UserRole, 0, len(in))
	for _, s := range in {
		if v, ok := identityv1.UserRole_value[s]; ok {
			out = append(out, identityv1.UserRole(v))
		} else {
			out = append(out, identityv1.UserRole_USER_ROLE_UNSPECIFIED)
		}
	}
	return out
}

func identifiersToProto(in []store.Identifier) []*identityv1.Identifier {
	out := make([]*identityv1.Identifier, 0, len(in))
	for _, i := range in {
		out = append(out, &identityv1.Identifier{
			Id:            &commonv1.UUID{Value: i.ID.String()},
			UserId:        &commonv1.UUID{Value: i.UserID.String()},
			Kind:          identifierKindToProto(i.Kind),
			Subject:       i.Subject,
			VerifiedEmail: i.Email,
			HostedDomain:  i.HostedDomain,
			CreatedAt:     timestamppb.New(i.CreatedAt),
			LastUsedAt:    timestamppb.New(i.LastUsedAt),
		})
	}
	return out
}

func identifiersToJSON(in []store.Identifier) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, i := range in {
		out = append(out, map[string]any{
			"id":            i.ID.String(),
			"kind":          string(i.Kind),
			"subject":       i.Subject,
			"email":         i.Email,
			"verified":      i.Verified,
			"hosted_domain": i.HostedDomain,
			"created_at":    i.CreatedAt.UTC().Format(time.RFC3339Nano),
			"last_used_at":  i.LastUsedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return out
}

// identifierKindFromProto is the inverse of identifierKindToProto. Returns
// "" for unspecified/unknown so callers can reject with InvalidArgument.
func identifierKindFromProto(k identityv1.IdentifierKind) store.IdentifierKind {
	switch k {
	case identityv1.IdentifierKind_IDENTIFIER_KIND_GOOGLE:
		return store.KindGoogle
	case identityv1.IdentifierKind_IDENTIFIER_KIND_MAGIC_LINK:
		return store.KindMagicLink
	case identityv1.IdentifierKind_IDENTIFIER_KIND_APPLE:
		return store.KindApple
	case identityv1.IdentifierKind_IDENTIFIER_KIND_GITHUB:
		return store.KindGitHub
	case identityv1.IdentifierKind_IDENTIFIER_KIND_SOLANA:
		return store.KindSolana
	default:
		return ""
	}
}

func identifierKindToProto(k store.IdentifierKind) identityv1.IdentifierKind {
	switch k {
	case store.KindGoogle:
		return identityv1.IdentifierKind_IDENTIFIER_KIND_GOOGLE
	case store.KindMagicLink:
		return identityv1.IdentifierKind_IDENTIFIER_KIND_MAGIC_LINK
	case store.KindApple:
		return identityv1.IdentifierKind_IDENTIFIER_KIND_APPLE
	case store.KindGitHub:
		return identityv1.IdentifierKind_IDENTIFIER_KIND_GITHUB
	case store.KindSolana:
		return identityv1.IdentifierKind_IDENTIFIER_KIND_SOLANA
	default:
		return identityv1.IdentifierKind_IDENTIFIER_KIND_UNSPECIFIED
	}
}

// --- error mapping ------------------------------------------------------

var errLastIdentifier = errors.New("identity-svc: cannot remove the last verified identifier")

func mapStoreError(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return connect.NewError(connect.CodeNotFound, err)
	}
	if errors.Is(err, errLastIdentifier) {
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, errLastIdentifier) {
		writeError(w, http.StatusConflict, "last_identifier", err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", err.Error())
}
