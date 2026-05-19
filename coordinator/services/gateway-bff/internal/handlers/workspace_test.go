package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

// stubWorkspaceClient records every call so tests can assert wiring +
// returns canned responses. nil fields default to empty proto messages.
type stubWorkspaceClient struct {
	listResp   *identityv1.ListWorkspacesResponse
	createResp *identityv1.CreateWorkspaceResponse
	getResp    *identityv1.GetWorkspaceResponse
	addResp    *identityv1.AddMemberResponse
	listMembersResp *identityv1.ListMembersResponse
	err        error

	lastListReq   *identityv1.ListWorkspacesRequest
	lastCreateReq *identityv1.CreateWorkspaceRequest
	lastGetReq    *identityv1.GetWorkspaceRequest
	lastUpdReq    *identityv1.UpdateWorkspaceRequest
	lastDelReq    *identityv1.DeleteWorkspaceRequest
	lastAddReq    *identityv1.AddMemberRequest
	lastRemReq    *identityv1.RemoveMemberRequest
	lastLMReq     *identityv1.ListMembersRequest
	lastURReq     *identityv1.UpdateMemberRoleRequest
}

func (s *stubWorkspaceClient) CreateWorkspace(_ context.Context, r *identityv1.CreateWorkspaceRequest) (*identityv1.CreateWorkspaceResponse, error) {
	s.lastCreateReq = r
	if s.err != nil {
		return nil, s.err
	}
	if s.createResp != nil {
		return s.createResp, nil
	}
	return &identityv1.CreateWorkspaceResponse{}, nil
}
func (s *stubWorkspaceClient) GetWorkspace(_ context.Context, r *identityv1.GetWorkspaceRequest) (*identityv1.GetWorkspaceResponse, error) {
	s.lastGetReq = r
	if s.err != nil {
		return nil, s.err
	}
	if s.getResp != nil {
		return s.getResp, nil
	}
	return &identityv1.GetWorkspaceResponse{}, nil
}
func (s *stubWorkspaceClient) ListWorkspaces(_ context.Context, r *identityv1.ListWorkspacesRequest) (*identityv1.ListWorkspacesResponse, error) {
	s.lastListReq = r
	if s.err != nil {
		return nil, s.err
	}
	if s.listResp != nil {
		return s.listResp, nil
	}
	return &identityv1.ListWorkspacesResponse{}, nil
}
func (s *stubWorkspaceClient) UpdateWorkspace(_ context.Context, r *identityv1.UpdateWorkspaceRequest) (*identityv1.UpdateWorkspaceResponse, error) {
	s.lastUpdReq = r
	if s.err != nil {
		return nil, s.err
	}
	return &identityv1.UpdateWorkspaceResponse{}, nil
}
func (s *stubWorkspaceClient) DeleteWorkspace(_ context.Context, r *identityv1.DeleteWorkspaceRequest) (*identityv1.DeleteWorkspaceResponse, error) {
	s.lastDelReq = r
	if s.err != nil {
		return nil, s.err
	}
	return &identityv1.DeleteWorkspaceResponse{}, nil
}
func (s *stubWorkspaceClient) AddMember(_ context.Context, r *identityv1.AddMemberRequest) (*identityv1.AddMemberResponse, error) {
	s.lastAddReq = r
	if s.err != nil {
		return nil, s.err
	}
	if s.addResp != nil {
		return s.addResp, nil
	}
	return &identityv1.AddMemberResponse{}, nil
}
func (s *stubWorkspaceClient) RemoveMember(_ context.Context, r *identityv1.RemoveMemberRequest) (*identityv1.RemoveMemberResponse, error) {
	s.lastRemReq = r
	if s.err != nil {
		return nil, s.err
	}
	return &identityv1.RemoveMemberResponse{}, nil
}
func (s *stubWorkspaceClient) ListMembers(_ context.Context, r *identityv1.ListMembersRequest) (*identityv1.ListMembersResponse, error) {
	s.lastLMReq = r
	if s.err != nil {
		return nil, s.err
	}
	if s.listMembersResp != nil {
		return s.listMembersResp, nil
	}
	return &identityv1.ListMembersResponse{}, nil
}
func (s *stubWorkspaceClient) UpdateMemberRole(_ context.Context, r *identityv1.UpdateMemberRoleRequest) (*identityv1.UpdateMemberRoleResponse, error) {
	s.lastURReq = r
	if s.err != nil {
		return nil, s.err
	}
	return &identityv1.UpdateMemberRoleResponse{}, nil
}

// makeAuthedReq builds an authed request via the auth package's test
// helper — saves us from spinning up a real JWT signer.
func makeAuthedReq(method, target string, body []byte) *http.Request {
	r := httptest.NewRequest(method, target, bytes.NewReader(body))
	c := &auth.Claims{Roles: []string{"USER_ROLE_CUSTOMER"}}
	c.Subject = uuid.NewString()
	return r.WithContext(auth.NewContextForTesting(r.Context(), c))
}

// TestWorkspaceHandler_503WithoutClient asserts the 503 fall-through
// when no WorkspaceClient is configured.
func TestWorkspaceHandler_503WithoutClient(t *testing.T) {
	api := New(nil, nil, nil)
	// no api.Workspaces set
	r := chi.NewRouter()
	r.Get("/api/v1/workspaces", api.ListWorkspaces)

	req := makeAuthedReq(http.MethodGet, "/api/v1/workspaces", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestWorkspaceHandler_NoAuth: every workspace route requires auth.
func TestWorkspaceHandler_NoAuth(t *testing.T) {
	api := New(nil, nil, nil)
	api.Workspaces = &stubWorkspaceClient{}
	r := chi.NewRouter()
	r.Get("/api/v1/workspaces", api.ListWorkspaces)
	r.Post("/api/v1/workspaces", api.CreateWorkspace)

	for _, m := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(m, "/api/v1/workspaces", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected 401, got %d", m, w.Code)
		}
	}
}

// TestWorkspaceHandler_CreateForwardsBody asserts the BFF forwards the
// JSON body's name + plan into the Connect request.
func TestWorkspaceHandler_CreateForwardsBody(t *testing.T) {
	stub := &stubWorkspaceClient{}
	api := New(nil, nil, nil)
	api.Workspaces = stub
	r := chi.NewRouter()
	r.Post("/api/v1/workspaces", api.CreateWorkspace)

	body, _ := json.Marshal(map[string]string{"name": "acme", "plan": "STARTER"})
	req := makeAuthedReq(http.MethodPost, "/api/v1/workspaces", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if stub.lastCreateReq == nil {
		t.Fatalf("upstream not called")
	}
	if stub.lastCreateReq.GetName() != "acme" {
		t.Errorf("name mismatch: %q", stub.lastCreateReq.GetName())
	}
	if stub.lastCreateReq.GetPlan() != identityv1.WorkspacePlan_WORKSPACE_PLAN_STARTER {
		t.Errorf("plan mismatch: %v", stub.lastCreateReq.GetPlan())
	}
}

// TestWorkspaceHandler_GetForwardsID covers the path param → UUID parse.
func TestWorkspaceHandler_GetForwardsID(t *testing.T) {
	id := uuid.NewString()
	stub := &stubWorkspaceClient{
		getResp: &identityv1.GetWorkspaceResponse{
			Workspace: &identityv1.Workspace{
				Id:   &commonv1.UUID{Value: id},
				Name: "ws",
			},
			CallerRole: identityv1.Role_ROLE_OWNER,
		},
	}
	api := New(nil, nil, nil)
	api.Workspaces = stub
	r := chi.NewRouter()
	r.Get("/api/v1/workspaces/{id}", api.GetWorkspace)

	req := makeAuthedReq(http.MethodGet, "/api/v1/workspaces/"+id, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	if stub.lastGetReq == nil {
		t.Fatalf("upstream not called")
	}
	if stub.lastGetReq.GetId().GetValue() != id {
		t.Errorf("id forwarded incorrectly: %q != %q", stub.lastGetReq.GetId().GetValue(), id)
	}
}

// TestWorkspaceHandler_GetRejectsBadID returns 400 not 500 on a non-UUID.
func TestWorkspaceHandler_GetRejectsBadID(t *testing.T) {
	api := New(nil, nil, nil)
	api.Workspaces = &stubWorkspaceClient{}
	r := chi.NewRouter()
	r.Get("/api/v1/workspaces/{id}", api.GetWorkspace)

	req := makeAuthedReq(http.MethodGet, "/api/v1/workspaces/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// TestWorkspaceHandler_AddMemberForwardsAll covers the full request body
// → proto conversion.
func TestWorkspaceHandler_AddMemberForwardsAll(t *testing.T) {
	id := uuid.NewString()
	stub := &stubWorkspaceClient{}
	api := New(nil, nil, nil)
	api.Workspaces = stub
	r := chi.NewRouter()
	r.Post("/api/v1/workspaces/{id}/members", api.AddMember)

	body, _ := json.Marshal(map[string]string{"user_email": "Bob@Example.COM", "role": "admin"})
	req := makeAuthedReq(http.MethodPost, "/api/v1/workspaces/"+id+"/members", body)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d body=%s", w.Code, w.Body.String())
	}
	if stub.lastAddReq.GetUserEmail() != "bob@example.com" {
		t.Errorf("email not lowercased: %q", stub.lastAddReq.GetUserEmail())
	}
	if stub.lastAddReq.GetRole() != identityv1.Role_ROLE_ADMIN {
		t.Errorf("role mismatch: %v", stub.lastAddReq.GetRole())
	}
}

// TestWorkspaceHandler_UpstreamErrorPropagates: a Connect error from the
// upstream client surfaces as a non-2xx response (mapped by writeUpstreamError).
func TestWorkspaceHandler_UpstreamErrorPropagates(t *testing.T) {
	stub := &stubWorkspaceClient{err: errors.New("upstream blew up")}
	api := New(nil, nil, nil)
	api.Workspaces = stub
	r := chi.NewRouter()
	r.Get("/api/v1/workspaces", api.ListWorkspaces)

	req := makeAuthedReq(http.MethodGet, "/api/v1/workspaces", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code < 400 {
		t.Fatalf("expected non-2xx, got %d", w.Code)
	}
}

// TestPlanFromString roundtrips every public plan.
func TestPlanFromString(t *testing.T) {
	cases := map[string]identityv1.WorkspacePlan{
		"":           identityv1.WorkspacePlan_WORKSPACE_PLAN_UNSPECIFIED,
		" FREE":      identityv1.WorkspacePlan_WORKSPACE_PLAN_FREE,
		"Starter":    identityv1.WorkspacePlan_WORKSPACE_PLAN_STARTER,
		"growth":     identityv1.WorkspacePlan_WORKSPACE_PLAN_GROWTH,
		"ENTERPRISE": identityv1.WorkspacePlan_WORKSPACE_PLAN_ENTERPRISE,
	}
	for in, want := range cases {
		if got := planFromString(in); got != want {
			t.Errorf("planFromString(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestRoleFromString roundtrips every public role.
func TestRoleFromString(t *testing.T) {
	cases := map[string]identityv1.Role{
		"":             identityv1.Role_ROLE_UNSPECIFIED,
		"OWNER":        identityv1.Role_ROLE_OWNER,
		"admin":        identityv1.Role_ROLE_ADMIN,
		"BILLING_ONLY": identityv1.Role_ROLE_BILLING_ONLY,
		"read_only":    identityv1.Role_ROLE_READ_ONLY,
		"GIBBERISH":    identityv1.Role_ROLE_UNSPECIFIED,
	}
	for in, want := range cases {
		if got := roleFromString(in); got != want {
			t.Errorf("roleFromString(%q) = %v, want %v", in, got, want)
		}
	}
}
