// workspace.go: /api/v1/workspaces — thin proxy over the Connect-RPC
// WorkspaceService exposed by identity-svc.
//
// The BFF forwards bearer auth in the upstream call so identity-svc can
// authorize on the same JWT claims the gateway already verified. Tests
// pass a mock WorkspaceClient (see clients/clients.go) instead.
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

// WorkspaceClient is the subset of the WorkspaceService the BFF needs.
// Lives here (not in clients.go) so this file is the single point of
// truth for the workspace surface.
type WorkspaceClient interface {
	CreateWorkspace(ctx context.Context, req *identityv1.CreateWorkspaceRequest) (*identityv1.CreateWorkspaceResponse, error)
	GetWorkspace(ctx context.Context, req *identityv1.GetWorkspaceRequest) (*identityv1.GetWorkspaceResponse, error)
	ListWorkspaces(ctx context.Context, req *identityv1.ListWorkspacesRequest) (*identityv1.ListWorkspacesResponse, error)
	UpdateWorkspace(ctx context.Context, req *identityv1.UpdateWorkspaceRequest) (*identityv1.UpdateWorkspaceResponse, error)
	DeleteWorkspace(ctx context.Context, req *identityv1.DeleteWorkspaceRequest) (*identityv1.DeleteWorkspaceResponse, error)
	AddMember(ctx context.Context, req *identityv1.AddMemberRequest) (*identityv1.AddMemberResponse, error)
	RemoveMember(ctx context.Context, req *identityv1.RemoveMemberRequest) (*identityv1.RemoveMemberResponse, error)
	ListMembers(ctx context.Context, req *identityv1.ListMembersRequest) (*identityv1.ListMembersResponse, error)
	UpdateMemberRole(ctx context.Context, req *identityv1.UpdateMemberRoleRequest) (*identityv1.UpdateMemberRoleResponse, error)
}

// requireAuthedAPI rejects unauthenticated callers; helper to keep the
// handlers terse.
func (a *API) requireAuthed(w http.ResponseWriter, r *http.Request) bool {
	if _, ok := auth.FromContext(r.Context()); !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return false
	}
	return true
}

// requireWorkspaceClient returns 503 when the BFF was built without a
// workspace client wired (e.g. identity-svc is unreachable at boot).
func (a *API) requireWorkspaceClient(w http.ResponseWriter) bool {
	if a.Workspaces == nil {
		writeError(w, http.StatusServiceUnavailable, "workspace_client_unavailable",
			"identity-svc WorkspaceService not configured")
		return false
	}
	return true
}

// --- routes --------------------------------------------------------------

// ListWorkspaces returns every workspace the caller is a member of.
//
//	GET /api/v1/workspaces
func (a *API) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuthed(w, r) || !a.requireWorkspaceClient(w) {
		return
	}
	resp, err := a.Workspaces.ListWorkspaces(r.Context(), &identityv1.ListWorkspacesRequest{})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// CreateWorkspace mints a new workspace owned by the caller.
//
//	POST /api/v1/workspaces  { name, plan }
func (a *API) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuthed(w, r) || !a.requireWorkspaceClient(w) {
		return
	}
	var body struct {
		Name string `json:"name"`
		Plan string `json:"plan"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	plan := planFromString(body.Plan)
	resp, err := a.Workspaces.CreateWorkspace(r.Context(), &identityv1.CreateWorkspaceRequest{
		Name: strings.TrimSpace(body.Name),
		Plan: plan,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// GetWorkspace returns one workspace.
//
//	GET /api/v1/workspaces/{id}
func (a *API) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuthed(w, r) || !a.requireWorkspaceClient(w) {
		return
	}
	id, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	resp, err := a.Workspaces.GetWorkspace(r.Context(), &identityv1.GetWorkspaceRequest{
		Id: &commonv1.UUID{Value: id.String()},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateWorkspace mutates name / plan.
//
//	PATCH /api/v1/workspaces/{id}  { name, plan }
func (a *API) UpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuthed(w, r) || !a.requireWorkspaceClient(w) {
		return
	}
	id, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var body struct {
		Name string `json:"name"`
		Plan string `json:"plan"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resp, err := a.Workspaces.UpdateWorkspace(r.Context(), &identityv1.UpdateWorkspaceRequest{
		Id:   &commonv1.UUID{Value: id.String()},
		Name: strings.TrimSpace(body.Name),
		Plan: planFromString(body.Plan),
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// DeleteWorkspace soft-deletes a workspace (OWNER only, step-up gated).
//
//	DELETE /api/v1/workspaces/{id}  body: { step_up_token }
func (a *API) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuthed(w, r) || !a.requireWorkspaceClient(w) {
		return
	}
	id, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var body struct {
		StepUpToken string `json:"step_up_token"`
	}
	// Body is optional — DELETE without body just sends the empty
	// request and identity-svc returns step_up_required.
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	_, err := a.Workspaces.DeleteWorkspace(r.Context(), &identityv1.DeleteWorkspaceRequest{
		Id:          &commonv1.UUID{Value: id.String()},
		StepUpToken: body.StepUpToken,
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListMembers returns every member of a workspace.
//
//	GET /api/v1/workspaces/{id}/members
func (a *API) ListMembers(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuthed(w, r) || !a.requireWorkspaceClient(w) {
		return
	}
	id, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	resp, err := a.Workspaces.ListMembers(r.Context(), &identityv1.ListMembersRequest{
		WorkspaceId: &commonv1.UUID{Value: id.String()},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// AddMember invites or directly adds a user.
//
//	POST /api/v1/workspaces/{id}/members  { user_email, role }
func (a *API) AddMember(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuthed(w, r) || !a.requireWorkspaceClient(w) {
		return
	}
	id, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var body struct {
		UserEmail string `json:"user_email"`
		Role      string `json:"role"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resp, err := a.Workspaces.AddMember(r.Context(), &identityv1.AddMemberRequest{
		WorkspaceId: &commonv1.UUID{Value: id.String()},
		UserEmail:   strings.TrimSpace(strings.ToLower(body.UserEmail)),
		Role:        roleFromString(body.Role),
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

// RemoveMember evicts a member.
//
//	DELETE /api/v1/workspaces/{id}/members/{userID}
func (a *API) RemoveMember(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuthed(w, r) || !a.requireWorkspaceClient(w) {
		return
	}
	wsID, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	uID, ok := parseUUIDParam(w, chi.URLParam(r, "userID"), "userID")
	if !ok {
		return
	}
	_, err := a.Workspaces.RemoveMember(r.Context(), &identityv1.RemoveMemberRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID.String()},
		UserId:      &commonv1.UUID{Value: uID.String()},
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// UpdateMemberRole changes a member's role.
//
//	PATCH /api/v1/workspaces/{id}/members/{userID}  { role }
func (a *API) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuthed(w, r) || !a.requireWorkspaceClient(w) {
		return
	}
	wsID, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	uID, ok := parseUUIDParam(w, chi.URLParam(r, "userID"), "userID")
	if !ok {
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	resp, err := a.Workspaces.UpdateMemberRole(r.Context(), &identityv1.UpdateMemberRoleRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID.String()},
		UserId:      &commonv1.UUID{Value: uID.String()},
		Role:        roleFromString(body.Role),
	})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// --- helpers --------------------------------------------------------------

func planFromString(s string) identityv1.WorkspacePlan {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "FREE":
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_FREE
	case "STARTER":
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_STARTER
	case "GROWTH":
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_GROWTH
	case "ENTERPRISE":
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_ENTERPRISE
	default:
		return identityv1.WorkspacePlan_WORKSPACE_PLAN_UNSPECIFIED
	}
}

func roleFromString(s string) identityv1.Role {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "OWNER":
		return identityv1.Role_ROLE_OWNER
	case "ADMIN":
		return identityv1.Role_ROLE_ADMIN
	case "BILLING_ONLY":
		return identityv1.Role_ROLE_BILLING_ONLY
	case "READ_ONLY":
		return identityv1.Role_ROLE_READ_ONLY
	default:
		return identityv1.Role_ROLE_UNSPECIFIED
	}
}
