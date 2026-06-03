package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
)

// allowAllWorkspaces satisfies clients.WorkspaceClient for tests where
// workspace membership is not the subject under test (the #688 guard
// delegates enforcement to identity-svc, so unit tests stub the answer).
type allowAllWorkspaces struct{ denyAll bool }

func (m allowAllWorkspaces) GetWorkspace(ctx context.Context, req *identityv1.GetWorkspaceRequest) (*identityv1.GetWorkspaceResponse, error) {
	if m.denyAll {
		return nil, connect.NewError(connect.CodePermissionDenied, context.Canceled)
	}
	return &identityv1.GetWorkspaceResponse{}, nil
}
func (m allowAllWorkspaces) CreateWorkspace(context.Context, *identityv1.CreateWorkspaceRequest) (*identityv1.CreateWorkspaceResponse, error) {
	return &identityv1.CreateWorkspaceResponse{}, nil
}
func (m allowAllWorkspaces) ListWorkspaces(context.Context, *identityv1.ListWorkspacesRequest) (*identityv1.ListWorkspacesResponse, error) {
	return &identityv1.ListWorkspacesResponse{}, nil
}
func (m allowAllWorkspaces) UpdateWorkspace(context.Context, *identityv1.UpdateWorkspaceRequest) (*identityv1.UpdateWorkspaceResponse, error) {
	return &identityv1.UpdateWorkspaceResponse{}, nil
}
func (m allowAllWorkspaces) DeleteWorkspace(context.Context, *identityv1.DeleteWorkspaceRequest) (*identityv1.DeleteWorkspaceResponse, error) {
	return &identityv1.DeleteWorkspaceResponse{}, nil
}
func (m allowAllWorkspaces) AddMember(context.Context, *identityv1.AddMemberRequest) (*identityv1.AddMemberResponse, error) {
	return &identityv1.AddMemberResponse{}, nil
}
func (m allowAllWorkspaces) RemoveMember(context.Context, *identityv1.RemoveMemberRequest) (*identityv1.RemoveMemberResponse, error) {
	return &identityv1.RemoveMemberResponse{}, nil
}
func (m allowAllWorkspaces) ListMembers(context.Context, *identityv1.ListMembersRequest) (*identityv1.ListMembersResponse, error) {
	return &identityv1.ListMembersResponse{}, nil
}
func (m allowAllWorkspaces) UpdateMemberRole(context.Context, *identityv1.UpdateMemberRoleRequest) (*identityv1.UpdateMemberRoleResponse, error) {
	return &identityv1.UpdateMemberRoleResponse{}, nil
}

// The guard itself: non-members get 403, missing client fails CLOSED.
func TestWorkspaceGuard_DeniesNonMember(t *testing.T) {
	api := newAPI(t, nil)
	api.Workspaces = allowAllWorkspaces{denyAll: true}
	w := httptest.NewRecorder()
	if api.requireWorkspaceAccess(context.Background(), w, uuid.New()) {
		t.Fatalf("expected denial for non-member")
	}
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

func TestWorkspaceGuard_FailsClosedWithoutClient(t *testing.T) {
	api := newAPI(t, nil)
	api.Workspaces = nil
	w := httptest.NewRecorder()
	if api.requireWorkspaceAccess(context.Background(), w, uuid.New()) {
		t.Fatalf("expected fail-closed denial")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", w.Code)
	}
}
