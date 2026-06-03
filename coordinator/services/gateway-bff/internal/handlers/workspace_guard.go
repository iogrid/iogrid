package handlers

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
)

// requireWorkspaceAccess closes the IDOR shape #688 tracks: the BFF
// historically verified only that the caller was AUTHENTICATED, then
// forwarded the client-supplied workspace_id downstream — any signed-in
// user who obtained another workspace's UUID could act on it.
//
// Enforcement is delegated to identity-svc: WorkspaceService.GetWorkspace
// runs requireMembership (CodePermissionDenied for non-members), and the
// #321 claims-propagation interceptor forwards the caller's identity on
// every outbound RPC, so a single GetWorkspace round-trip IS the
// membership check. Costs one extra in-cluster hop per request — fine
// for Phase 0; add a short TTL cache here if it ever shows in traces.
//
// Returns true when the caller may act on the workspace; otherwise the
// HTTP error has already been written:
//
//	403 workspace_forbidden — authenticated but not a member
//	404 workspace_not_found — workspace does not exist
//	503 workspace_guard_unavailable — identity-svc unreachable (fail
//	    CLOSED: an authz check that can't run must deny)
func (a *API) requireWorkspaceAccess(ctx context.Context, w http.ResponseWriter, wsID uuid.UUID) bool {
	if a.Workspaces == nil {
		writeError(w, http.StatusServiceUnavailable, "workspace_guard_unavailable", "workspace membership check unavailable")
		return false
	}
	_, err := a.Workspaces.GetWorkspace(ctx, &identityv1.GetWorkspaceRequest{
		Id: &commonv1.UUID{Value: wsID.String()},
	})
	if err == nil {
		return true
	}
	switch connect.CodeOf(err) {
	case connect.CodePermissionDenied:
		writeError(w, http.StatusForbidden, "workspace_forbidden", "you are not a member of this workspace")
	case connect.CodeNotFound:
		writeError(w, http.StatusNotFound, "workspace_not_found", "workspace not found")
	default:
		// Includes Unavailable/timeouts: fail closed — an authz check
		// that can't run must deny, not wave through.
		writeError(w, http.StatusServiceUnavailable, "workspace_guard_unavailable", "workspace membership check failed")
	}
	return false
}
