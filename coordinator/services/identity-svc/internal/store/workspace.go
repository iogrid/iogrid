// workspace.go: typed Postgres accessors for the Workspace bounded
// context. Methods follow the same Querier pattern as store.go so
// handlers can compose them inside the auth transactions.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// --- workspaces ----------------------------------------------------------

// CreateWorkspace inserts a new workspace row AND the owner Membership
// in the same transaction. The caller MUST supply a Querier that is a
// pgx.Tx; passing the pool leaves us racing with a concurrent reader.
func (s *Store) CreateWorkspace(ctx context.Context, q Querier, w *Workspace) error {
	if q == nil {
		q = s.Pool
	}
	if w.Plan == "" {
		w.Plan = PlanFree
	}
	if strings.TrimSpace(w.Name) == "" {
		return fmt.Errorf("workspace name required")
	}
	row := q.QueryRow(ctx, `
		INSERT INTO workspaces (owner_user_id, name, plan, billing_customer_id_stripe)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at`,
		w.OwnerUserID, w.Name, string(w.Plan), w.BillingCustomerIDStripe)
	if err := row.Scan(&w.ID, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return err
	}
	// Owner Membership is part of the same logical operation.
	if _, err := q.Exec(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, $3)`, w.ID, w.OwnerUserID, string(RoleOwner)); err != nil {
		return fmt.Errorf("insert owner membership: %w", err)
	}
	return nil
}

// GetWorkspace returns one workspace by id. Returns ErrNotFound when
// the row is missing OR soft-deleted.
func (s *Store) GetWorkspace(ctx context.Context, q Querier, id uuid.UUID) (*Workspace, error) {
	if q == nil {
		q = s.Pool
	}
	w := &Workspace{}
	var plan string
	err := q.QueryRow(ctx, `
		SELECT id, owner_user_id, name, plan, billing_customer_id_stripe,
		       created_at, updated_at, deleted_at
		  FROM workspaces
		 WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&w.ID, &w.OwnerUserID, &w.Name, &plan, &w.BillingCustomerIDStripe,
			&w.CreatedAt, &w.UpdatedAt, &w.DeletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	w.Plan = WorkspacePlan(plan)
	return w, nil
}

// ListWorkspacesForUser returns every active workspace the user is a
// member of, newest-first, along with the user's role per workspace.
func (s *Store) ListWorkspacesForUser(ctx context.Context, q Querier, userID uuid.UUID, limit, offset int) ([]Workspace, []Role, error) {
	if q == nil {
		q = s.Pool
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := q.Query(ctx, `
		SELECT w.id, w.owner_user_id, w.name, w.plan, w.billing_customer_id_stripe,
		       w.created_at, w.updated_at, w.deleted_at, m.role
		  FROM workspaces w
		  JOIN workspace_members m ON m.workspace_id = w.id
		 WHERE m.user_id = $1 AND w.deleted_at IS NULL
		 ORDER BY w.created_at DESC
		 LIMIT $2 OFFSET $3`, userID, limit, offset)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var workspaces []Workspace
	var roles []Role
	for rows.Next() {
		var w Workspace
		var plan, role string
		if err := rows.Scan(&w.ID, &w.OwnerUserID, &w.Name, &plan, &w.BillingCustomerIDStripe,
			&w.CreatedAt, &w.UpdatedAt, &w.DeletedAt, &role); err != nil {
			return nil, nil, err
		}
		w.Plan = WorkspacePlan(plan)
		workspaces = append(workspaces, w)
		roles = append(roles, Role(role))
	}
	return workspaces, roles, rows.Err()
}

// UpdateWorkspace mutates the user-editable fields. Empty values are
// treated as "do not change".
func (s *Store) UpdateWorkspace(ctx context.Context, q Querier, id uuid.UUID, name string, plan WorkspacePlan) (*Workspace, error) {
	if q == nil {
		q = s.Pool
	}
	sets := []string{"updated_at = now()"}
	args := []any{id}
	if name != "" {
		args = append(args, name)
		sets = append(sets, fmt.Sprintf("name = $%d", len(args)))
	}
	if plan != "" {
		args = append(args, string(plan))
		sets = append(sets, fmt.Sprintf("plan = $%d", len(args)))
	}
	sql := fmt.Sprintf("UPDATE workspaces SET %s WHERE id = $1 AND deleted_at IS NULL", strings.Join(sets, ", "))
	tag, err := q.Exec(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetWorkspace(ctx, q, id)
}

// SetWorkspaceStripeCustomer wires the billing_customer_id_stripe set by
// billing-svc after a Stripe checkout completes. Called via a downstream
// RPC; we keep it small so the call site stays terse.
func (s *Store) SetWorkspaceStripeCustomer(ctx context.Context, q Querier, id uuid.UUID, stripeID string) error {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `
		UPDATE workspaces SET billing_customer_id_stripe = $2, updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`, id, stripeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SoftDeleteWorkspace flips deleted_at. Memberships cascade-delete via
// FK; downstream services (billing, workloads) keep their rows for
// audit but should treat the workspace as inactive.
func (s *Store) SoftDeleteWorkspace(ctx context.Context, q Querier, id uuid.UUID) error {
	if q == nil {
		q = s.Pool
	}
	tag, err := q.Exec(ctx, `
		UPDATE workspaces SET deleted_at = now(), updated_at = now()
		 WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// EnsurePersonalWorkspace mints one workspace for the user if they
// don't yet have any membership. Called from the auth flow on user
// creation. Returns the workspace (newly minted OR pre-existing).
func (s *Store) EnsurePersonalWorkspace(ctx context.Context, q Querier, user *User) (*Workspace, error) {
	if q == nil {
		q = s.Pool
	}
	// Already a member of something? — bail.
	var existingID uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT w.id
		  FROM workspaces w
		  JOIN workspace_members m ON m.workspace_id = w.id
		 WHERE m.user_id = $1 AND w.deleted_at IS NULL
		 ORDER BY w.created_at LIMIT 1`, user.ID).Scan(&existingID)
	if err == nil {
		return s.GetWorkspace(ctx, q, existingID)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	name := user.DisplayName
	if name == "" {
		name = user.PrimaryEmail
	}
	if name == "" {
		name = "Personal workspace"
	}
	if len(name) > 90 {
		name = name[:90]
	}
	w := &Workspace{
		OwnerUserID: user.ID,
		Name:        name + " — Personal",
		Plan:        PlanFree,
	}
	if err := s.CreateWorkspace(ctx, q, w); err != nil {
		return nil, err
	}
	return w, nil
}

// --- memberships ---------------------------------------------------------

// GetMembership returns the (workspace, user) row. ErrNotFound when the
// user isn't a member.
func (s *Store) GetMembership(ctx context.Context, q Querier, workspaceID, userID uuid.UUID) (*WorkspaceMember, error) {
	if q == nil {
		q = s.Pool
	}
	m := &WorkspaceMember{}
	var role string
	err := q.QueryRow(ctx, `
		SELECT workspace_id, user_id, role, joined_at
		  FROM workspace_members
		 WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID).
		Scan(&m.WorkspaceID, &m.UserID, &role, &m.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.Role = Role(role)
	return m, nil
}

// AddMember inserts a workspace_members row. Returns ErrUniqueViolation
// (wrapped) if the user is already a member — caller usually treats
// that as success.
func (s *Store) AddMember(ctx context.Context, q Querier, m *WorkspaceMember) error {
	if q == nil {
		q = s.Pool
	}
	row := q.QueryRow(ctx, `
		INSERT INTO workspace_members (workspace_id, user_id, role)
		VALUES ($1, $2, $3)
		RETURNING joined_at`, m.WorkspaceID, m.UserID, string(m.Role))
	return row.Scan(&m.JoinedAt)
}

// UpdateMemberRole mutates the role. ErrNotFound when the row is gone.
// Refuses to move the last OWNER out of OWNER — the workspace MUST
// keep exactly one OWNER at all times.
func (s *Store) UpdateMemberRole(ctx context.Context, q Querier, workspaceID, userID uuid.UUID, role Role) (*WorkspaceMember, error) {
	if q == nil {
		q = s.Pool
	}
	// Refuse to demote the only OWNER: count OWNERs before+after.
	var ownerCount int
	if err := q.QueryRow(ctx, `
		SELECT COUNT(*) FROM workspace_members
		 WHERE workspace_id = $1 AND role = 'OWNER'`, workspaceID).Scan(&ownerCount); err != nil {
		return nil, err
	}
	if ownerCount <= 1 {
		// Is the row we're updating the lone OWNER?
		var currentRole string
		if err := q.QueryRow(ctx, `
			SELECT role FROM workspace_members
			 WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID).Scan(&currentRole); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		if currentRole == string(RoleOwner) && role != RoleOwner {
			return nil, fmt.Errorf("cannot demote the only OWNER; transfer ownership first")
		}
	}
	tag, err := q.Exec(ctx, `
		UPDATE workspace_members SET role = $3
		 WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID, string(role))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.GetMembership(ctx, q, workspaceID, userID)
}

// RemoveMember deletes a workspace_members row. Refuses to remove the
// last OWNER.
func (s *Store) RemoveMember(ctx context.Context, q Querier, workspaceID, userID uuid.UUID) error {
	if q == nil {
		q = s.Pool
	}
	var role string
	err := q.QueryRow(ctx, `
		SELECT role FROM workspace_members
		 WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if role == string(RoleOwner) {
		var ownerCount int
		if err := q.QueryRow(ctx, `
			SELECT COUNT(*) FROM workspace_members
			 WHERE workspace_id = $1 AND role = 'OWNER'`, workspaceID).Scan(&ownerCount); err != nil {
			return err
		}
		if ownerCount <= 1 {
			return fmt.Errorf("cannot remove the only OWNER; transfer ownership first")
		}
	}
	tag, err := q.Exec(ctx, `
		DELETE FROM workspace_members
		 WHERE workspace_id = $1 AND user_id = $2`, workspaceID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListMembers returns the membership rows joined with the user record
// for the "Members" tab in the management plane.
func (s *Store) ListMembers(ctx context.Context, q Querier, workspaceID uuid.UUID, limit, offset int) ([]WorkspaceMemberDetail, error) {
	if q == nil {
		q = s.Pool
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := q.Query(ctx, `
		SELECT m.workspace_id, m.user_id, m.role, m.joined_at,
		       COALESCE(u.primary_email::text, ''), COALESCE(u.display_name, '')
		  FROM workspace_members m
		  LEFT JOIN users u ON u.id = m.user_id
		 WHERE m.workspace_id = $1
		 ORDER BY m.joined_at
		 LIMIT $2 OFFSET $3`, workspaceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceMemberDetail
	for rows.Next() {
		var d WorkspaceMemberDetail
		var role string
		if err := rows.Scan(&d.Member.WorkspaceID, &d.Member.UserID, &role, &d.Member.JoinedAt,
			&d.PrimaryEmail, &d.DisplayName); err != nil {
			return nil, err
		}
		d.Member.Role = Role(role)
		out = append(out, d)
	}
	return out, rows.Err()
}

// --- invites -------------------------------------------------------------

// CreateInvite inserts a pending invite row. Used when AddMember is
// called for an email that doesn't match any existing user. Idempotent
// per (workspace, email) — the partial unique index gates duplicates.
func (s *Store) CreateInvite(ctx context.Context, q Querier, inv *WorkspaceInvite) error {
	if q == nil {
		q = s.Pool
	}
	row := q.QueryRow(ctx, `
		INSERT INTO workspace_invites (workspace_id, invitee_email, role, invited_by)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at`,
		inv.WorkspaceID, inv.InviteeEmail, string(inv.Role), inv.InvitedBy)
	return row.Scan(&inv.ID, &inv.CreatedAt)
}

// ConsumeInvitesForEmail marks every pending invite matching the email
// as consumed AND inserts the matching workspace_members rows. Called
// when a user signs in for the first time and we want to upgrade their
// pending invites into real memberships. Returns the list of memberships
// that were newly created.
func (s *Store) ConsumeInvitesForEmail(ctx context.Context, q Querier, email string, userID uuid.UUID) ([]WorkspaceMember, error) {
	if q == nil {
		q = s.Pool
	}
	rows, err := q.Query(ctx, `
		UPDATE workspace_invites
		   SET consumed_at = now()
		 WHERE invitee_email = $1 AND consumed_at IS NULL
		RETURNING workspace_id, role`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type pending struct {
		WorkspaceID uuid.UUID
		Role        string
	}
	var pendings []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.WorkspaceID, &p.Role); err != nil {
			return nil, err
		}
		pendings = append(pendings, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]WorkspaceMember, 0, len(pendings))
	for _, p := range pendings {
		m := WorkspaceMember{
			WorkspaceID: p.WorkspaceID,
			UserID:      userID,
			Role:        Role(p.Role),
			JoinedAt:    time.Now(),
		}
		// Best-effort: a race where the invite was consumed twice
		// would surface as a unique-violation; treat it as "already a
		// member" and continue.
		if err := s.AddMember(ctx, q, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}
