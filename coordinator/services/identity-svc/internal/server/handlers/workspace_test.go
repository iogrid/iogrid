package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// contextWithAuthedUser wraps an ambient context with the authed-user
// signal the handlers inspect. Used by every test that exercises the
// post-bearer-middleware code path without lifting the JWT signer.
func contextWithAuthedUser(id uuid.UUID) context.Context {
	return authmw.WithAuthedUser(context.Background(), id)
}

// TestRoleRank pins the ordering so a future refactor can't accidentally
// shuffle the privilege ladder.
func TestRoleRank(t *testing.T) {
	cases := []struct {
		role store.Role
		want int
	}{
		{store.RoleOwner, 40},
		{store.RoleAdmin, 30},
		{store.RoleBillingOnly, 20},
		{store.RoleReadOnly, 10},
		{store.Role("UNKNOWN"), -1},
	}
	for _, c := range cases {
		if got := roleRank(c.role); got != c.want {
			t.Errorf("roleRank(%s) = %d, want %d", c.role, got, c.want)
		}
	}
}

// TestRequireRank covers the rank-based gate used by every privileged RPC.
func TestRequireRank(t *testing.T) {
	if err := requireRank(store.RoleOwner, roleRank(store.RoleAdmin)); err != nil {
		t.Errorf("OWNER should clear ADMIN gate, got: %v", err)
	}
	if err := requireRank(store.RoleAdmin, roleRank(store.RoleAdmin)); err != nil {
		t.Errorf("ADMIN should clear ADMIN gate, got: %v", err)
	}
	if err := requireRank(store.RoleReadOnly, roleRank(store.RoleAdmin)); err == nil {
		t.Errorf("READ_ONLY should NOT clear ADMIN gate")
	}
	if err := requireRank(store.RoleBillingOnly, roleRank(store.RoleAdmin)); err == nil {
		t.Errorf("BILLING_ONLY should NOT clear ADMIN gate")
	}
}

// TestProtoConversion roundtrips every enum value to catch a missing
// case branch in plan/role mapping.
func TestProtoConversion(t *testing.T) {
	plans := []identityv1.WorkspacePlan{
		identityv1.WorkspacePlan_WORKSPACE_PLAN_FREE,
		identityv1.WorkspacePlan_WORKSPACE_PLAN_STARTER,
		identityv1.WorkspacePlan_WORKSPACE_PLAN_GROWTH,
		identityv1.WorkspacePlan_WORKSPACE_PLAN_ENTERPRISE,
	}
	for _, p := range plans {
		sp := protoToPlan(p)
		if sp == "" {
			t.Errorf("protoToPlan(%v) returned empty", p)
		}
		back := planToProto(sp)
		if back != p {
			t.Errorf("roundtrip %v → %v → %v", p, sp, back)
		}
	}
	roles := []identityv1.Role{
		identityv1.Role_ROLE_OWNER,
		identityv1.Role_ROLE_ADMIN,
		identityv1.Role_ROLE_BILLING_ONLY,
		identityv1.Role_ROLE_READ_ONLY,
	}
	for _, r := range roles {
		sr := protoToRole(r)
		if sr == "" {
			t.Errorf("protoToRole(%v) returned empty", r)
		}
		back := roleToProto(sr)
		if back != r {
			t.Errorf("roundtrip %v → %v → %v", r, sr, back)
		}
	}
}

// TestWorkspaceToProto: nil-safe + carries every field.
func TestWorkspaceToProto(t *testing.T) {
	if workspaceToProto(nil) != nil {
		t.Errorf("nil input must return nil")
	}
	id := uuid.New()
	owner := uuid.New()
	w := &store.Workspace{
		ID:                      id,
		OwnerUserID:             owner,
		Name:                    "acme",
		Plan:                    store.PlanGrowth,
		BillingCustomerIDStripe: "cus_X",
	}
	got := workspaceToProto(w)
	if got.GetId().GetValue() != id.String() {
		t.Errorf("id mismatch: %s", got.GetId().GetValue())
	}
	if got.GetOwnerUserId().GetValue() != owner.String() {
		t.Errorf("owner mismatch")
	}
	if got.GetName() != "acme" {
		t.Errorf("name mismatch")
	}
	if got.GetPlan() != identityv1.WorkspacePlan_WORKSPACE_PLAN_GROWTH {
		t.Errorf("plan mismatch: %v", got.GetPlan())
	}
	if got.GetBillingCustomerIdStripe() != "cus_X" {
		t.Errorf("stripe id mismatch")
	}
}

// TestParseUUIDProto covers nil + malformed strings + happy path.
func TestParseUUIDProto(t *testing.T) {
	if _, err := parseUUIDProto(nil); err == nil {
		t.Errorf("nil should error")
	}
	if _, err := parseUUIDProto(&commonv1.UUID{Value: "not-a-uuid"}); err == nil {
		t.Errorf("bad uuid should error")
	}
	id := uuid.New()
	got, err := parseUUIDProto(&commonv1.UUID{Value: id.String()})
	if err != nil {
		t.Fatalf("happy path errored: %v", err)
	}
	if got != id {
		t.Errorf("mismatch: %v != %v", got, id)
	}
}

// TestWorkspaceHandler_CreateWorkspace_NoAuth verifies the Connect path
// rejects an unauthenticated call before reaching the store.
func TestWorkspaceHandler_CreateWorkspace_NoAuth(t *testing.T) {
	h := NewWorkspaceHandler(nil)
	_, err := h.CreateWorkspace(context.Background(), connect.NewRequest(&identityv1.CreateWorkspaceRequest{
		Name: "test",
	}))
	if err == nil {
		t.Fatalf("expected unauthenticated error")
	}
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("expected Unauthenticated, got %v", connect.CodeOf(err))
	}
}

// TestWorkspaceHandler_CreateWorkspace_EmptyName verifies validation
// even when auth is present.
func TestWorkspaceHandler_CreateWorkspace_EmptyName(t *testing.T) {
	h := NewWorkspaceHandler(nil)
	ctx := contextWithAuthedUser(uuid.New())
	_, err := h.CreateWorkspace(ctx, connect.NewRequest(&identityv1.CreateWorkspaceRequest{Name: "  "}))
	if err == nil {
		t.Fatalf("expected error for empty name")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", connect.CodeOf(err))
	}
}

// TestWorkspaceHandler_GetWorkspace_BadID rejects malformed UUID before
// any store lookup.
func TestWorkspaceHandler_GetWorkspace_BadID(t *testing.T) {
	h := NewWorkspaceHandler(nil)
	_, err := h.GetWorkspace(context.Background(), connect.NewRequest(&identityv1.GetWorkspaceRequest{
		Id: &commonv1.UUID{Value: "not-a-uuid"},
	}))
	if err == nil {
		t.Fatalf("expected error")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", connect.CodeOf(err))
	}
}

// TestWorkspaceJSON_RoutesMounted confirms every JSON endpoint is wired
// even without a Postgres backend (each route returns 401 before
// touching the store).
func TestWorkspaceJSON_RoutesMounted(t *testing.T) {
	h := NewWorkspaceHandler(nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountWorkspaceJSON(r)
	})

	wsID := uuid.New().String()
	userID := uuid.New().String()

	cases := []struct {
		method, path string
		body         string
	}{
		{"GET", "/v1/workspaces/", ""},
		{"POST", "/v1/workspaces/", `{"name":"test"}`},
		{"GET", "/v1/workspaces/" + wsID, ""},
		{"PATCH", "/v1/workspaces/" + wsID, `{"name":"new"}`},
		{"DELETE", "/v1/workspaces/" + wsID, ""},
		{"GET", "/v1/workspaces/" + wsID + "/members", ""},
		{"POST", "/v1/workspaces/" + wsID + "/members", `{"user_email":"a@b.com","role":"ADMIN"}`},
		{"PATCH", "/v1/workspaces/" + wsID + "/members/" + userID, `{"role":"ADMIN"}`},
		{"DELETE", "/v1/workspaces/" + wsID + "/members/" + userID, ""},
	}
	for _, c := range cases {
		var body *bytes.Buffer
		if c.body != "" {
			body = bytes.NewBufferString(c.body)
		} else {
			body = bytes.NewBuffer(nil)
		}
		req := httptest.NewRequest(c.method, c.path, body)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		// Each route requires bearer auth that we haven't supplied
		// → expect 401. (The list endpoint also rejects with 401 in
		// our handler.) The crucial assertion is "not 404" — the
		// route is wired.
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s returned 404 (route not wired)", c.method, c.path)
		}
	}
}

// TestWorkspaceJSON_CreateWithoutAuth: 401, doesn't reach store.
func TestWorkspaceJSON_CreateWithoutAuth(t *testing.T) {
	h := NewWorkspaceHandler(nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountWorkspaceJSON(r)
	})
	body, _ := json.Marshal(map[string]string{"name": "acme"})
	req := httptest.NewRequest(http.MethodPost, "/v1/workspaces/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestWorkspaceJSON_BadUUID: 400 for non-UUID path segments.
func TestWorkspaceJSON_BadUUID(t *testing.T) {
	h := NewWorkspaceHandler(nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountWorkspaceJSON(r)
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/workspaces/not-a-uuid", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
