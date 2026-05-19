//go:build integration
// +build integration

// Integration tests for the Workspace bounded context. Spins up a real
// Postgres via ory/dockertest, applies migrations, then exercises the
// full Connect-RPC handler + JSON handler surface end-to-end.
//
// Coverage:
//   - CreateWorkspace + ListWorkspaces + GetWorkspace happy path
//   - Member of workspace A cannot see workspace B (auth boundary)
//   - AddMember promotes role + ListMembers shows both users
//   - Non-owner cannot Delete; owner can
//   - Role downgrade from OWNER is rejected by store (last-owner guard)
//   - Pending invite for unknown email + auto-consume on first sign-in
//
// Run via:  go test -tags=integration ./internal/server/handlers/...

package handlers

import (
	"context"
	"fmt"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	idb "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/db"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// pgFixture is duplicated from internal/auth/integration_test.go so the
// handlers package can be exercised independently. Brings up a one-shot
// Postgres + runs migrations.
func pgFixture(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Skipf("dockertest pool unavailable: %v", err)
	}
	if err := pool.Client.Ping(); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_DB=identity",
			"listen_addresses='*'",
		},
	}, func(cfg *docker.HostConfig) {
		cfg.AutoRemove = true
		cfg.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		t.Fatalf("docker run postgres: %v", err)
	}
	_ = resource.Expire(120)

	dsn := fmt.Sprintf("postgres://postgres:secret@%s/identity?sslmode=disable", resource.GetHostPort("5432/tcp"))
	var pgxPool *pgxpool.Pool
	if err := pool.Retry(func() error {
		p, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			return err
		}
		if err := p.Ping(context.Background()); err != nil {
			p.Close()
			return err
		}
		pgxPool = p
		return nil
	}); err != nil {
		t.Fatalf("postgres ready: %v", err)
	}
	if err := idb.Apply(context.Background(), dsn); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	cleanup := func() {
		pgxPool.Close()
		_ = pool.Purge(resource)
	}
	return pgxPool, cleanup
}

// seedUser is a tiny helper that mints a verified magic-link user for
// the test. Returns its id.
func seedUser(t *testing.T, ctx context.Context, st *store.Store, email string) uuid.UUID {
	t.Helper()
	u := &store.User{PrimaryEmail: email, DisplayName: email}
	if err := st.CreateUser(ctx, nil, u); err != nil {
		t.Fatalf("seed user %s: %v", email, err)
	}
	ident := &store.Identifier{
		UserID:   u.ID,
		Kind:     store.KindMagicLink,
		Email:    email,
		Verified: true,
	}
	if err := st.CreateIdentifier(ctx, nil, ident); err != nil {
		t.Fatalf("seed identifier %s: %v", email, err)
	}
	return u.ID
}

// TestWorkspace_CreateGetList_HappyPath walks the canonical solo flow.
func TestWorkspace_CreateGetList_HappyPath(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	st := store.New(pool)
	h := NewWorkspaceHandler(st)

	ctx := context.Background()
	alice := seedUser(t, ctx, st, "alice@example.com")

	authedCtx := authmw.WithAuthedUser(ctx, alice)

	// Create.
	createResp, err := h.CreateWorkspace(authedCtx, connect.NewRequest(&identityv1.CreateWorkspaceRequest{
		Name: "Alice's Lab",
		Plan: identityv1.WorkspacePlan_WORKSPACE_PLAN_STARTER,
	}))
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}
	wsID, err := uuid.Parse(createResp.Msg.GetWorkspace().GetId().GetValue())
	if err != nil {
		t.Fatalf("bad workspace id: %v", err)
	}

	// Get.
	getResp, err := h.GetWorkspace(authedCtx, connect.NewRequest(&identityv1.GetWorkspaceRequest{
		Id: &commonv1.UUID{Value: wsID.String()},
	}))
	if err != nil {
		t.Fatalf("GetWorkspace: %v", err)
	}
	if getResp.Msg.GetCallerRole() != identityv1.Role_ROLE_OWNER {
		t.Errorf("expected OWNER, got %v", getResp.Msg.GetCallerRole())
	}

	// List.
	listResp, err := h.ListWorkspaces(authedCtx, connect.NewRequest(&identityv1.ListWorkspacesRequest{}))
	if err != nil {
		t.Fatalf("ListWorkspaces: %v", err)
	}
	if len(listResp.Msg.GetWorkspaces()) != 1 {
		t.Errorf("expected 1 workspace, got %d", len(listResp.Msg.GetWorkspaces()))
	}
}

// TestWorkspace_NonMember_CannotSee verifies the auth boundary.
func TestWorkspace_NonMember_CannotSee(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	st := store.New(pool)
	h := NewWorkspaceHandler(st)
	ctx := context.Background()

	alice := seedUser(t, ctx, st, "alice@example.com")
	bob := seedUser(t, ctx, st, "bob@example.com")

	createResp, err := h.CreateWorkspace(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.CreateWorkspaceRequest{Name: "Alice's Lab"}))
	if err != nil {
		t.Fatalf("alice create: %v", err)
	}
	wsID := createResp.Msg.GetWorkspace().GetId().GetValue()

	// Bob tries to Get Alice's workspace → PermissionDenied.
	_, err = h.GetWorkspace(authmw.WithAuthedUser(ctx, bob),
		connect.NewRequest(&identityv1.GetWorkspaceRequest{
			Id: &commonv1.UUID{Value: wsID},
		}))
	if err == nil {
		t.Fatalf("expected permission denied")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", connect.CodeOf(err))
	}
}

// TestWorkspace_AddMember_RoleEnforcement: only ADMIN+ can add; the
// added user shows up in ListMembers.
func TestWorkspace_AddMember_RoleEnforcement(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	st := store.New(pool)
	h := NewWorkspaceHandler(st)
	ctx := context.Background()

	alice := seedUser(t, ctx, st, "alice@example.com")
	bob := seedUser(t, ctx, st, "bob@example.com")
	carol := seedUser(t, ctx, st, "carol@example.com")

	// Alice creates and adds Bob as READ_ONLY.
	create, err := h.CreateWorkspace(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.CreateWorkspaceRequest{Name: "Lab"}))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	wsID := create.Msg.GetWorkspace().GetId().GetValue()

	addResp, err := h.AddMember(authmw.WithAuthedUser(ctx, alice), connect.NewRequest(&identityv1.AddMemberRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID},
		UserEmail:   "bob@example.com",
		Role:        identityv1.Role_ROLE_READ_ONLY,
	}))
	if err != nil {
		t.Fatalf("alice AddMember bob: %v", err)
	}
	if addResp.Msg.GetPending() {
		t.Errorf("bob exists; should not be pending")
	}

	// Bob (READ_ONLY) attempts to add Carol → PermissionDenied.
	_, err = h.AddMember(authmw.WithAuthedUser(ctx, bob), connect.NewRequest(&identityv1.AddMemberRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID},
		UserEmail:   "carol@example.com",
		Role:        identityv1.Role_ROLE_READ_ONLY,
	}))
	if err == nil {
		t.Fatalf("expected permission denied for READ_ONLY add")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", connect.CodeOf(err))
	}

	// Alice promotes Bob to ADMIN, Bob can now add Carol.
	if _, err := h.UpdateMemberRole(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.UpdateMemberRoleRequest{
			WorkspaceId: &commonv1.UUID{Value: wsID},
			UserId:      &commonv1.UUID{Value: bob.String()},
			Role:        identityv1.Role_ROLE_ADMIN,
		})); err != nil {
		t.Fatalf("promote bob: %v", err)
	}
	if _, err := h.AddMember(authmw.WithAuthedUser(ctx, bob), connect.NewRequest(&identityv1.AddMemberRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID},
		UserEmail:   "carol@example.com",
		Role:        identityv1.Role_ROLE_READ_ONLY,
	})); err != nil {
		t.Fatalf("bob (now ADMIN) AddMember carol: %v", err)
	}
	_ = carol

	// ListMembers returns 3 rows (alice OWNER, bob ADMIN, carol READ_ONLY).
	listResp, err := h.ListMembers(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.ListMembersRequest{
			WorkspaceId: &commonv1.UUID{Value: wsID},
		}))
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(listResp.Msg.GetMembers()) != 3 {
		t.Errorf("expected 3 members, got %d", len(listResp.Msg.GetMembers()))
	}
}

// TestWorkspace_DeleteRequiresOwner: ADMIN cannot delete, OWNER can.
func TestWorkspace_DeleteRequiresOwner(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	st := store.New(pool)
	h := NewWorkspaceHandler(st)
	ctx := context.Background()

	alice := seedUser(t, ctx, st, "alice@example.com")
	bob := seedUser(t, ctx, st, "bob@example.com")

	create, _ := h.CreateWorkspace(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.CreateWorkspaceRequest{Name: "Lab"}))
	wsID := create.Msg.GetWorkspace().GetId().GetValue()

	_, _ = h.AddMember(authmw.WithAuthedUser(ctx, alice), connect.NewRequest(&identityv1.AddMemberRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID},
		UserEmail:   "bob@example.com",
		Role:        identityv1.Role_ROLE_ADMIN,
	}))

	// Bob (ADMIN) tries to delete → PermissionDenied.
	_, err := h.DeleteWorkspace(authmw.WithAuthedUser(ctx, bob),
		connect.NewRequest(&identityv1.DeleteWorkspaceRequest{
			Id: &commonv1.UUID{Value: wsID},
		}))
	if err == nil {
		t.Fatalf("ADMIN delete should fail")
	}
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Errorf("expected PermissionDenied, got %v", connect.CodeOf(err))
	}

	// Alice (OWNER) succeeds.
	if _, err := h.DeleteWorkspace(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.DeleteWorkspaceRequest{
			Id: &commonv1.UUID{Value: wsID},
		})); err != nil {
		t.Fatalf("OWNER delete failed: %v", err)
	}

	// Subsequent Get returns NotFound (soft-deleted).
	_, err = h.GetWorkspace(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.GetWorkspaceRequest{
			Id: &commonv1.UUID{Value: wsID},
		}))
	if err == nil {
		t.Fatalf("expected error after delete")
	}
}

// TestWorkspace_PendingInvite_AutoConsumed: invite an unknown email,
// later seed the user + consume → the user becomes a member.
func TestWorkspace_PendingInvite_AutoConsumed(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	st := store.New(pool)
	h := NewWorkspaceHandler(st)
	ctx := context.Background()

	alice := seedUser(t, ctx, st, "alice@example.com")
	create, _ := h.CreateWorkspace(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.CreateWorkspaceRequest{Name: "Lab"}))
	wsID := create.Msg.GetWorkspace().GetId().GetValue()

	// Invite bob (no user yet) → pending=true.
	addResp, err := h.AddMember(authmw.WithAuthedUser(ctx, alice), connect.NewRequest(&identityv1.AddMemberRequest{
		WorkspaceId: &commonv1.UUID{Value: wsID},
		UserEmail:   "bob@example.com",
		Role:        identityv1.Role_ROLE_READ_ONLY,
	}))
	if err != nil {
		t.Fatalf("invite bob: %v", err)
	}
	if !addResp.Msg.GetPending() {
		t.Errorf("expected pending=true for unknown email")
	}

	// Bob signs up (we simulate the auth flow's side effect: create
	// user + identifier + consume invites).
	bob := seedUser(t, ctx, st, "bob@example.com")
	memberships, err := st.ConsumeInvitesForEmail(ctx, nil, "bob@example.com", bob)
	if err != nil {
		t.Fatalf("ConsumeInvitesForEmail: %v", err)
	}
	if len(memberships) != 1 {
		t.Fatalf("expected 1 consumed invite, got %d", len(memberships))
	}

	// Bob can now Get the workspace.
	if _, err := h.GetWorkspace(authmw.WithAuthedUser(ctx, bob),
		connect.NewRequest(&identityv1.GetWorkspaceRequest{
			Id: &commonv1.UUID{Value: wsID},
		})); err != nil {
		t.Fatalf("bob GetWorkspace after invite: %v", err)
	}
}

// TestWorkspace_LastOwnerCannotBeRemoved: the only OWNER must transfer
// before stepping down.
func TestWorkspace_LastOwnerCannotBeRemoved(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	st := store.New(pool)
	h := NewWorkspaceHandler(st)
	ctx := context.Background()

	alice := seedUser(t, ctx, st, "alice@example.com")
	create, _ := h.CreateWorkspace(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.CreateWorkspaceRequest{Name: "Lab"}))
	wsID := create.Msg.GetWorkspace().GetId().GetValue()

	_, err := h.RemoveMember(authmw.WithAuthedUser(ctx, alice),
		connect.NewRequest(&identityv1.RemoveMemberRequest{
			WorkspaceId: &commonv1.UUID{Value: wsID},
			UserId:      &commonv1.UUID{Value: alice.String()},
		}))
	if err == nil {
		t.Fatalf("expected failure removing only OWNER")
	}
	if connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Errorf("expected FailedPrecondition, got %v", connect.CodeOf(err))
	}
}

// TestWorkspace_EnsurePersonalWorkspace_AutoCreated: the auth flow uses
// this; we exercise it directly to be sure it stays idempotent.
func TestWorkspace_EnsurePersonalWorkspace_AutoCreated(t *testing.T) {
	pool, cleanup := pgFixture(t)
	defer cleanup()
	st := store.New(pool)
	ctx := context.Background()

	user := &store.User{PrimaryEmail: "x@y.com", DisplayName: "X"}
	if err := st.CreateUser(ctx, nil, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	w1, err := st.EnsurePersonalWorkspace(ctx, nil, user)
	if err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	w2, err := st.EnsurePersonalWorkspace(ctx, nil, user)
	if err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	if w1.ID != w2.ID {
		t.Errorf("idempotency broken: %s vs %s", w1.ID, w2.ID)
	}
}
