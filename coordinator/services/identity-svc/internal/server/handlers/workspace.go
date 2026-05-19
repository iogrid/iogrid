// workspace.go: Connect-Go handler for the WorkspaceService RPCs +
// the parallel HTTP+JSON surface mounted under /v1/workspaces. The
// Connect handler is what gateway-bff / billing-svc / workloads-svc
// call over the wire; the JSON surface is what e2e tests and direct
// curl callers use.
//
// Authorization model:
//   - Every RPC except CreateWorkspace + ListWorkspaces requires the
//     caller to be a member of the workspace.
//   - UpdateWorkspace / Add/Remove/UpdateMember require role rank
//     OWNER or ADMIN.
//   - DeleteWorkspace requires OWNER.
//
// The Connect handler reads the authed user from the same bearer-token
// middleware as the JSON handlers (chi-mounted, see routes.go).
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
	"google.golang.org/protobuf/types/known/timestamppb"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1/identityv1connect"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// WorkspaceHandler implements identityv1connect.WorkspaceServiceHandler
// AND mounts a parallel /v1/workspaces JSON tree.
type WorkspaceHandler struct {
	identityv1connect.UnimplementedWorkspaceServiceHandler
	Store *store.Store
}

// NewWorkspaceHandler wires the dependency.
func NewWorkspaceHandler(s *store.Store) *WorkspaceHandler {
	return &WorkspaceHandler{Store: s}
}

// --- shared helpers ------------------------------------------------------

// roleRank assigns an ordinal so we can compare permissions terselly.
// Higher = more privilege. Unknown roles map to -1 (denied).
func roleRank(r store.Role) int {
	switch r {
	case store.RoleOwner:
		return 40
	case store.RoleAdmin:
		return 30
	case store.RoleBillingOnly:
		return 20
	case store.RoleReadOnly:
		return 10
	default:
		return -1
	}
}

// requireMembership returns the caller's role within the workspace.
// Errors with PermissionDenied when the caller isn't a member.
func (h *WorkspaceHandler) requireMembership(ctx context.Context, workspaceID uuid.UUID) (uuid.UUID, store.Role, error) {
	userID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return uuid.Nil, "", connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	m, err := h.Store.GetMembership(ctx, nil, workspaceID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return userID, "", connect.NewError(connect.CodePermissionDenied, errors.New("not a member of this workspace"))
		}
		return userID, "", connect.NewError(connect.CodeInternal, err)
	}
	return userID, m.Role, nil
}

// requireRank rejects callers whose role is below minRank.
func requireRank(role store.Role, min int) error {
	if roleRank(role) < min {
		return connect.NewError(connect.CodePermissionDenied,
			fmt.Errorf("role %s insufficient for this operation", role))
	}
	return nil
}

// protoToPlan converts the proto enum into the store-level constant.
// Unknown / unspecified maps to FREE (the default for self-serve).
func protoToPlan(p identityv1.WorkspacePlan) store.WorkspacePlan {
	switch p {
	case identityv1.WorkspacePlan_WORKSPACE_PLAN_FREE:
		return store.PlanFree
	case identityv1.WorkspacePlan_WORKSPACE_PLAN_STARTER:
		return store.PlanStarter
	case identityv1.WorkspacePlan_WORKSPACE_PLAN_GROWTH:
		return store.PlanGrowth
	case identityv1.WorkspacePlan_WORKSPACE_PLAN_ENTERPRISE:
		return store.PlanEnterprise
	default:
		return ""
	}
}

// planToProto is the inverse.
func planToProto(p store.WorkspacePlan) identityv1.WorkspacePlan {
	switch p {
	case store.PlanFree:
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_FREE
	case store.PlanStarter:
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_STARTER
	case store.PlanGrowth:
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_GROWTH
	case store.PlanEnterprise:
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_ENTERPRISE
	default:
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_UNSPECIFIED
	}
}

// protoToRole / roleToProto are the equivalent for Role.
func protoToRole(r identityv1.Role) store.Role {
	switch r {
	case identityv1.Role_ROLE_OWNER:
		return store.RoleOwner
	case identityv1.Role_ROLE_ADMIN:
		return store.RoleAdmin
	case identityv1.Role_ROLE_BILLING_ONLY:
		return store.RoleBillingOnly
	case identityv1.Role_ROLE_READ_ONLY:
		return store.RoleReadOnly
	default:
		return ""
	}
}

func roleToProto(r store.Role) identityv1.Role {
	switch r {
	case store.RoleOwner:
		return identityv1.Role_ROLE_OWNER
	case store.RoleAdmin:
		return identityv1.Role_ROLE_ADMIN
	case store.RoleBillingOnly:
		return identityv1.Role_ROLE_BILLING_ONLY
	case store.RoleReadOnly:
		return identityv1.Role_ROLE_READ_ONLY
	default:
		return identityv1.Role_ROLE_UNSPECIFIED
	}
}

func workspaceToProto(w *store.Workspace) *identityv1.Workspace {
	if w == nil {
		return nil
	}
	pb := &identityv1.Workspace{
		Id:                      &commonv1.UUID{Value: w.ID.String()},
		OwnerUserId:             &commonv1.UUID{Value: w.OwnerUserID.String()},
		Name:                    w.Name,
		Plan:                    planToProto(w.Plan),
		BillingCustomerIdStripe: w.BillingCustomerIDStripe,
		CreatedAt:               timestamppb.New(w.CreatedAt),
		UpdatedAt:               timestamppb.New(w.UpdatedAt),
	}
	return pb
}

func membershipToProto(m store.WorkspaceMember) *identityv1.Membership {
	return &identityv1.Membership{
		WorkspaceId: &commonv1.UUID{Value: m.WorkspaceID.String()},
		UserId:      &commonv1.UUID{Value: m.UserID.String()},
		Role:        roleToProto(m.Role),
		JoinedAt:    timestamppb.New(m.JoinedAt),
	}
}

// parseUUIDProto handles the *commonv1.UUID wrapper.
func parseUUIDProto(u *commonv1.UUID) (uuid.UUID, error) {
	if u == nil {
		return uuid.Nil, errors.New("uuid required")
	}
	return uuid.Parse(u.GetValue())
}

// --- Connect handler: CreateWorkspace ------------------------------------

func (h *WorkspaceHandler) CreateWorkspace(
	ctx context.Context,
	req *connect.Request[identityv1.CreateWorkspaceRequest],
) (*connect.Response[identityv1.CreateWorkspaceResponse], error) {
	userID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	name := strings.TrimSpace(req.Msg.GetName())
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name required"))
	}
	if len(name) > 100 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name too long (max 100)"))
	}
	plan := protoToPlan(req.Msg.GetPlan())
	if plan == "" {
		plan = store.PlanFree
	}
	w := &store.Workspace{
		OwnerUserID: userID,
		Name:        name,
		Plan:        plan,
	}
	// CreateWorkspace inserts the workspace row AND owner membership.
	// We don't wrap in a transaction here because the only logical
	// failure mode (workspace insert succeeds but membership fails) is
	// degenerate and surfaces as an error — the workspace row is
	// soft-deletable so an operator can clean it up. The auth flow's
	// EnsurePersonalWorkspace runs inside its existing pgx.Tx.
	if err := h.Store.CreateWorkspace(ctx, nil, w); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&identityv1.CreateWorkspaceResponse{
		Workspace: workspaceToProto(w),
	}), nil
}

// --- Connect handler: GetWorkspace ---------------------------------------

func (h *WorkspaceHandler) GetWorkspace(
	ctx context.Context,
	req *connect.Request[identityv1.GetWorkspaceRequest],
) (*connect.Response[identityv1.GetWorkspaceResponse], error) {
	id, err := parseUUIDProto(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	_, role, err := h.requireMembership(ctx, id)
	if err != nil {
		return nil, err
	}
	w, err := h.Store.GetWorkspace(ctx, nil, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&identityv1.GetWorkspaceResponse{
		Workspace:  workspaceToProto(w),
		CallerRole: roleToProto(role),
	}), nil
}

// --- Connect handler: ListWorkspaces -------------------------------------

func (h *WorkspaceHandler) ListWorkspaces(
	ctx context.Context,
	req *connect.Request[identityv1.ListWorkspacesRequest],
) (*connect.Response[identityv1.ListWorkspacesResponse], error) {
	callerID, ok := authmw.AuthedUser(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("missing bearer token"))
	}
	target := callerID
	if u := req.Msg.GetUserId(); u != nil && u.GetValue() != "" {
		// We only honour user_id when it matches the caller; cross-user
		// listing is an admin-only op exposed elsewhere.
		parsed, err := uuid.Parse(u.GetValue())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		if parsed != callerID {
			return nil, connect.NewError(connect.CodePermissionDenied,
				errors.New("cross-user listing requires admin scope"))
		}
		target = parsed
	}
	page := req.Msg.GetPage()
	limit := int(page.GetPageSize())
	if limit <= 0 {
		limit = 50
	}
	workspaces, roles, err := h.Store.ListWorkspacesForUser(ctx, nil, target, limit, 0)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &identityv1.ListWorkspacesResponse{
		Workspaces: make([]*identityv1.Workspace, 0, len(workspaces)),
		Roles:      make([]identityv1.Role, 0, len(roles)),
		Page:       &commonv1.PageResponse{NextPageToken: ""},
	}
	for i := range workspaces {
		out.Workspaces = append(out.Workspaces, workspaceToProto(&workspaces[i]))
		out.Roles = append(out.Roles, roleToProto(roles[i]))
	}
	return connect.NewResponse(out), nil
}

// --- Connect handler: UpdateWorkspace ------------------------------------

func (h *WorkspaceHandler) UpdateWorkspace(
	ctx context.Context,
	req *connect.Request[identityv1.UpdateWorkspaceRequest],
) (*connect.Response[identityv1.UpdateWorkspaceResponse], error) {
	id, err := parseUUIDProto(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	_, role, err := h.requireMembership(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := requireRank(role, roleRank(store.RoleAdmin)); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.Msg.GetName())
	if len(name) > 100 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name too long (max 100)"))
	}
	plan := protoToPlan(req.Msg.GetPlan())
	w, err := h.Store.UpdateWorkspace(ctx, nil, id, name, plan)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&identityv1.UpdateWorkspaceResponse{
		Workspace: workspaceToProto(w),
	}), nil
}

// --- Connect handler: DeleteWorkspace ------------------------------------

func (h *WorkspaceHandler) DeleteWorkspace(
	ctx context.Context,
	req *connect.Request[identityv1.DeleteWorkspaceRequest],
) (*connect.Response[identityv1.DeleteWorkspaceResponse], error) {
	id, err := parseUUIDProto(req.Msg.GetId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	_, role, err := h.requireMembership(ctx, id)
	if err != nil {
		return nil, err
	}
	if role != store.RoleOwner {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("only OWNER can delete a workspace"))
	}
	// Step-up token is checked at the bearer middleware level; we
	// surface a clear error here when the claim is absent.
	if claims, ok := authmw.AuthedClaims(ctx); ok && !claims.StepUp {
		return nil, connect.NewError(connect.CodePermissionDenied,
			errors.New("step-up auth required"))
	}
	if err := h.Store.SoftDeleteWorkspace(ctx, nil, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&identityv1.DeleteWorkspaceResponse{}), nil
}

// --- Connect handler: AddMember ------------------------------------------

func (h *WorkspaceHandler) AddMember(
	ctx context.Context,
	req *connect.Request[identityv1.AddMemberRequest],
) (*connect.Response[identityv1.AddMemberResponse], error) {
	wsID, err := parseUUIDProto(req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	callerID, role, err := h.requireMembership(ctx, wsID)
	if err != nil {
		return nil, err
	}
	if err := requireRank(role, roleRank(store.RoleAdmin)); err != nil {
		return nil, err
	}
	email := strings.TrimSpace(strings.ToLower(req.Msg.GetUserEmail()))
	if email == "" || !strings.Contains(email, "@") {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("valid user_email required"))
	}
	memberRole := protoToRole(req.Msg.GetRole())
	if memberRole == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role required"))
	}
	if memberRole == store.RoleOwner {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("cannot add a second OWNER; transfer ownership instead"))
	}
	// Try to resolve the email to an existing user. Identifiers table
	// indexes verified emails; we look there first.
	identifiers, err := h.Store.FindIdentifiersByEmail(ctx, nil, email)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	var targetUserID uuid.UUID
	for _, idnt := range identifiers {
		if idnt.Verified {
			targetUserID = idnt.UserID
			break
		}
	}
	if targetUserID == uuid.Nil {
		// Fall back to an invite row — the user gets the membership on
		// their next sign-in (see store.ConsumeInvitesForEmail).
		inv := &store.WorkspaceInvite{
			WorkspaceID:  wsID,
			InviteeEmail: email,
			Role:         memberRole,
			InvitedBy:    callerID,
		}
		if err := h.Store.CreateInvite(ctx, nil, inv); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		return connect.NewResponse(&identityv1.AddMemberResponse{
			Membership: &identityv1.Membership{
				WorkspaceId: &commonv1.UUID{Value: wsID.String()},
				UserId:      &commonv1.UUID{Value: ""},
				Role:        roleToProto(memberRole),
				JoinedAt:    timestamppb.New(inv.CreatedAt),
			},
			Pending: true,
		}), nil
	}
	m := &store.WorkspaceMember{
		WorkspaceID: wsID,
		UserID:      targetUserID,
		Role:        memberRole,
	}
	if err := h.Store.AddMember(ctx, nil, m); err != nil {
		return nil, connect.NewError(connect.CodeAlreadyExists, err)
	}
	return connect.NewResponse(&identityv1.AddMemberResponse{
		Membership: membershipToProto(*m),
		Pending:    false,
	}), nil
}

// --- Connect handler: RemoveMember ---------------------------------------

func (h *WorkspaceHandler) RemoveMember(
	ctx context.Context,
	req *connect.Request[identityv1.RemoveMemberRequest],
) (*connect.Response[identityv1.RemoveMemberResponse], error) {
	wsID, err := parseUUIDProto(req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	uID, err := parseUUIDProto(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	callerID, role, err := h.requireMembership(ctx, wsID)
	if err != nil {
		return nil, err
	}
	// Self-removal is always allowed (except for the last OWNER, which
	// the store layer blocks). Otherwise require ADMIN+.
	if callerID != uID {
		if err := requireRank(role, roleRank(store.RoleAdmin)); err != nil {
			return nil, err
		}
	}
	if err := h.Store.RemoveMember(ctx, nil, wsID, uID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		// store.RemoveMember returns a plain error for "last OWNER"; map
		// that case to FailedPrecondition.
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&identityv1.RemoveMemberResponse{}), nil
}

// --- Connect handler: ListMembers ---------------------------------------

func (h *WorkspaceHandler) ListMembers(
	ctx context.Context,
	req *connect.Request[identityv1.ListMembersRequest],
) (*connect.Response[identityv1.ListMembersResponse], error) {
	wsID, err := parseUUIDProto(req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, _, err := h.requireMembership(ctx, wsID); err != nil {
		return nil, err
	}
	limit := int(req.Msg.GetPage().GetPageSize())
	if limit <= 0 {
		limit = 50
	}
	rows, err := h.Store.ListMembers(ctx, nil, wsID, limit, 0)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &identityv1.ListMembersResponse{
		Members: make([]*identityv1.MemberDetail, 0, len(rows)),
		Page:    &commonv1.PageResponse{NextPageToken: ""},
	}
	for _, d := range rows {
		out.Members = append(out.Members, &identityv1.MemberDetail{
			Membership:   membershipToProto(d.Member),
			PrimaryEmail: d.PrimaryEmail,
			DisplayName:  d.DisplayName,
		})
	}
	return connect.NewResponse(out), nil
}

// --- Connect handler: UpdateMemberRole -----------------------------------

func (h *WorkspaceHandler) UpdateMemberRole(
	ctx context.Context,
	req *connect.Request[identityv1.UpdateMemberRoleRequest],
) (*connect.Response[identityv1.UpdateMemberRoleResponse], error) {
	wsID, err := parseUUIDProto(req.Msg.GetWorkspaceId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	uID, err := parseUUIDProto(req.Msg.GetUserId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	_, role, err := h.requireMembership(ctx, wsID)
	if err != nil {
		return nil, err
	}
	if err := requireRank(role, roleRank(store.RoleAdmin)); err != nil {
		return nil, err
	}
	newRole := protoToRole(req.Msg.GetRole())
	if newRole == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("role required"))
	}
	if newRole == store.RoleOwner {
		// Transferring ownership is a separate, step-up-gated flow.
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("use transfer-ownership flow to grant OWNER"))
	}
	m, err := h.Store.UpdateMemberRole(ctx, nil, wsID, uID, newRole)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, connect.NewError(connect.CodeNotFound, err)
		}
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewResponse(&identityv1.UpdateMemberRoleResponse{
		Membership: membershipToProto(*m),
	}), nil
}

// --- JSON surface --------------------------------------------------------
//
// Parallel /v1/workspaces tree. The shape mirrors the Connect-RPC
// contracts so e2e tests can roundtrip with either transport.

// MountWorkspaceJSON attaches /v1/workspaces under the supplied router.
// The Connect-RPC handler lives at its own /iogrid.identity.v1.WorkspaceService/
// path (mounted by the parent server.routes).
func (h *WorkspaceHandler) MountWorkspaceJSON(r chi.Router) {
	r.Route("/workspaces", func(r chi.Router) {
		r.Get("/", h.jsonListWorkspaces)
		r.Post("/", h.jsonCreateWorkspace)
		r.Get("/{id}", h.jsonGetWorkspace)
		r.Patch("/{id}", h.jsonUpdateWorkspace)
		r.Delete("/{id}", h.jsonDeleteWorkspace)
		r.Get("/{id}/members", h.jsonListMembers)
		r.Post("/{id}/members", h.jsonAddMember)
		r.Patch("/{id}/members/{userID}", h.jsonUpdateMemberRole)
		r.Delete("/{id}/members/{userID}", h.jsonRemoveMember)
	})
}

func workspaceToJSON(w *store.Workspace) map[string]any {
	if w == nil {
		return nil
	}
	out := map[string]any{
		"id":                         w.ID.String(),
		"owner_user_id":              w.OwnerUserID.String(),
		"name":                       w.Name,
		"plan":                       string(w.Plan),
		"billing_customer_id_stripe": w.BillingCustomerIDStripe,
		"created_at":                 w.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":                 w.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	return out
}

func memberDetailToJSON(d store.WorkspaceMemberDetail) map[string]any {
	return map[string]any{
		"workspace_id":  d.Member.WorkspaceID.String(),
		"user_id":       d.Member.UserID.String(),
		"role":          string(d.Member.Role),
		"joined_at":     d.Member.JoinedAt.UTC().Format(time.RFC3339Nano),
		"primary_email": d.PrimaryEmail,
		"display_name":  d.DisplayName,
	}
}

type createWorkspaceJSONReq struct {
	Name string `json:"name"`
	Plan string `json:"plan"`
}

func (h *WorkspaceHandler) jsonCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	var req createWorkspaceJSONReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "name required")
		return
	}
	plan := store.WorkspacePlan(strings.ToUpper(req.Plan))
	switch plan {
	case "", store.PlanFree, store.PlanStarter, store.PlanGrowth, store.PlanEnterprise:
	default:
		writeError(w, http.StatusBadRequest, "invalid_argument", "unknown plan")
		return
	}
	if plan == "" {
		plan = store.PlanFree
	}
	ws := &store.Workspace{OwnerUserID: userID, Name: name, Plan: plan}
	if err := h.Store.CreateWorkspace(r.Context(), nil, ws); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, workspaceToJSON(ws))
}

func (h *WorkspaceHandler) jsonGetWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad workspace id")
		return
	}
	userID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	m, err := h.Store.GetMembership(r.Context(), nil, id, userID)
	if err != nil {
		writeError(w, http.StatusForbidden, "permission_denied", "not a member")
		return
	}
	ws, err := h.Store.GetWorkspace(r.Context(), nil, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "workspace not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	body := workspaceToJSON(ws)
	body["caller_role"] = string(m.Role)
	writeJSON(w, http.StatusOK, body)
}

func (h *WorkspaceHandler) jsonListWorkspaces(w http.ResponseWriter, r *http.Request) {
	userID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	workspaces, roles, err := h.Store.ListWorkspacesForUser(r.Context(), nil, userID, 50, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(workspaces))
	for i := range workspaces {
		entry := workspaceToJSON(&workspaces[i])
		entry["caller_role"] = string(roles[i])
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"workspaces": out})
}

type updateWorkspaceJSONReq struct {
	Name string `json:"name"`
	Plan string `json:"plan"`
}

func (h *WorkspaceHandler) jsonUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad workspace id")
		return
	}
	userID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	m, err := h.Store.GetMembership(r.Context(), nil, id, userID)
	if err != nil {
		writeError(w, http.StatusForbidden, "permission_denied", "not a member")
		return
	}
	if roleRank(m.Role) < roleRank(store.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "ADMIN or OWNER required")
		return
	}
	var req updateWorkspaceJSONReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	plan := store.WorkspacePlan(strings.ToUpper(req.Plan))
	switch plan {
	case "", store.PlanFree, store.PlanStarter, store.PlanGrowth, store.PlanEnterprise:
	default:
		writeError(w, http.StatusBadRequest, "invalid_argument", "unknown plan")
		return
	}
	ws, err := h.Store.UpdateWorkspace(r.Context(), nil, id, strings.TrimSpace(req.Name), plan)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "workspace not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, workspaceToJSON(ws))
}

func (h *WorkspaceHandler) jsonDeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad workspace id")
		return
	}
	userID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	m, err := h.Store.GetMembership(r.Context(), nil, id, userID)
	if err != nil {
		writeError(w, http.StatusForbidden, "permission_denied", "not a member")
		return
	}
	if m.Role != store.RoleOwner {
		writeError(w, http.StatusForbidden, "permission_denied", "OWNER required")
		return
	}
	if claims, ok := authmw.AuthedClaims(r.Context()); ok && !claims.StepUp {
		writeError(w, http.StatusForbidden, "step_up_required", "step-up auth required")
		return
	}
	if err := h.Store.SoftDeleteWorkspace(r.Context(), nil, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "workspace not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *WorkspaceHandler) jsonListMembers(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad workspace id")
		return
	}
	userID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	if _, err := h.Store.GetMembership(r.Context(), nil, id, userID); err != nil {
		writeError(w, http.StatusForbidden, "permission_denied", "not a member")
		return
	}
	rows, err := h.Store.ListMembers(r.Context(), nil, id, 50, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, d := range rows {
		out = append(out, memberDetailToJSON(d))
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": out})
}

type addMemberJSONReq struct {
	UserEmail string `json:"user_email"`
	Role      string `json:"role"`
}

func (h *WorkspaceHandler) jsonAddMember(w http.ResponseWriter, r *http.Request) {
	wsID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad workspace id")
		return
	}
	callerID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	cm, err := h.Store.GetMembership(r.Context(), nil, wsID, callerID)
	if err != nil {
		writeError(w, http.StatusForbidden, "permission_denied", "not a member")
		return
	}
	if roleRank(cm.Role) < roleRank(store.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "ADMIN or OWNER required")
		return
	}
	var req addMemberJSONReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.UserEmail))
	if email == "" || !strings.Contains(email, "@") {
		writeError(w, http.StatusBadRequest, "invalid_argument", "valid user_email required")
		return
	}
	role := store.Role(strings.ToUpper(req.Role))
	switch role {
	case store.RoleAdmin, store.RoleBillingOnly, store.RoleReadOnly:
	case store.RoleOwner:
		writeError(w, http.StatusBadRequest, "invalid_argument", "cannot add a second OWNER")
		return
	default:
		writeError(w, http.StatusBadRequest, "invalid_argument", "unknown role")
		return
	}
	identifiers, err := h.Store.FindIdentifiersByEmail(r.Context(), nil, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	var targetUserID uuid.UUID
	for _, idnt := range identifiers {
		if idnt.Verified {
			targetUserID = idnt.UserID
			break
		}
	}
	if targetUserID == uuid.Nil {
		inv := &store.WorkspaceInvite{
			WorkspaceID:  wsID,
			InviteeEmail: email,
			Role:         role,
			InvitedBy:    callerID,
		}
		if err := h.Store.CreateInvite(r.Context(), nil, inv); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"pending":      true,
			"workspace_id": wsID.String(),
			"user_email":   email,
			"role":         string(role),
		})
		return
	}
	m := &store.WorkspaceMember{
		WorkspaceID: wsID,
		UserID:      targetUserID,
		Role:        role,
	}
	if err := h.Store.AddMember(r.Context(), nil, m); err != nil {
		writeError(w, http.StatusConflict, "already_exists", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"pending":      false,
		"workspace_id": m.WorkspaceID.String(),
		"user_id":      m.UserID.String(),
		"role":         string(m.Role),
		"joined_at":    m.JoinedAt.UTC().Format(time.RFC3339Nano),
	})
}

type updateMemberRoleJSONReq struct {
	Role string `json:"role"`
}

func (h *WorkspaceHandler) jsonUpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	wsID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad workspace id")
		return
	}
	uID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad user id")
		return
	}
	callerID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	cm, err := h.Store.GetMembership(r.Context(), nil, wsID, callerID)
	if err != nil {
		writeError(w, http.StatusForbidden, "permission_denied", "not a member")
		return
	}
	if roleRank(cm.Role) < roleRank(store.RoleAdmin) {
		writeError(w, http.StatusForbidden, "permission_denied", "ADMIN or OWNER required")
		return
	}
	var req updateMemberRoleJSONReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error())
		return
	}
	role := store.Role(strings.ToUpper(req.Role))
	switch role {
	case store.RoleAdmin, store.RoleBillingOnly, store.RoleReadOnly:
	case store.RoleOwner:
		writeError(w, http.StatusBadRequest, "invalid_argument",
			"use transfer-ownership flow to grant OWNER")
		return
	default:
		writeError(w, http.StatusBadRequest, "invalid_argument", "unknown role")
		return
	}
	m, err := h.Store.UpdateMemberRole(r.Context(), nil, wsID, uID, role)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "member not found")
			return
		}
		writeError(w, http.StatusBadRequest, "failed_precondition", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"workspace_id": m.WorkspaceID.String(),
		"user_id":      m.UserID.String(),
		"role":         string(m.Role),
		"joined_at":    m.JoinedAt.UTC().Format(time.RFC3339Nano),
	})
}

func (h *WorkspaceHandler) jsonRemoveMember(w http.ResponseWriter, r *http.Request) {
	wsID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad workspace id")
		return
	}
	uID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "bad user id")
		return
	}
	callerID, ok := authmw.AuthedUser(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "missing bearer token")
		return
	}
	cm, err := h.Store.GetMembership(r.Context(), nil, wsID, callerID)
	if err != nil {
		writeError(w, http.StatusForbidden, "permission_denied", "not a member")
		return
	}
	if callerID != uID {
		if roleRank(cm.Role) < roleRank(store.RoleAdmin) {
			writeError(w, http.StatusForbidden, "permission_denied", "ADMIN or OWNER required")
			return
		}
	}
	if err := h.Store.RemoveMember(r.Context(), nil, wsID, uID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "member not found")
			return
		}
		writeError(w, http.StatusBadRequest, "failed_precondition", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
