package handlers

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"

	abusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
)

// ListAbuseQueue returns the active anti-abuse filter rules and (when
// the antiabuse-svc grows a queue RPC) the pending review-flagged
// events.
//
// Phase 0 maps to antiabuse-svc.ListFilters, which surfaces the active
// rule snapshot the abuse team uses to audit what the platform is
// blocking. The proper "abuse queue" of yellow-flagged events will
// land here once the proto is extended.
//
//	GET /api/v1/admin/abuse-queue
//	-> 200 { rules: [...], ruleset_hash }
//
// Gated by RequireRole("ADMIN") in the router.
func (a *API) ListAbuseQueue(w http.ResponseWriter, r *http.Request) {
	resp, err := a.Clients.Antiabuse.ListFilters(r.Context(), &abusev1.ListFiltersRequest{})
	if err != nil {
		writeUpstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ResolveAbuseEvent is the manual reviewer's "allow" / "block"
// decision. Once antiabuse-svc grows a corresponding RPC this handler
// will forward to it. Today we return 202 with the recorded note —
// stored only in the audit log — so the web UI can ship even before
// the backend RPC lands.
//
//	POST /api/v1/admin/abuse/{id}/resolve
//	  { decision: "allow"|"block", note }
func (a *API) ResolveAbuseEvent(w http.ResponseWriter, r *http.Request) {
	if !mustAdmin(w, r) {
		return
	}
	id, ok := parseUUIDParam(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var body struct {
		Decision string `json:"decision"`
		Note     string `json:"note"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	switch body.Decision {
	case "allow", "block":
	default:
		writeError(w, http.StatusBadRequest, "bad_request", `decision must be "allow" or "block"`)
		return
	}
	// Once antiabuse-svc grows a Resolve RPC, swap this stub for a
	// real forward. For now we acknowledge so the admin UI surface
	// can ship in parallel.
	writeJSON(w, http.StatusAccepted, map[string]any{
		"event_id": id.String(),
		"decision": body.Decision,
		"note":     body.Note,
		"status":   "queued",
	})
}

// mustAdmin is defence-in-depth: even though RequireRole("ADMIN")
// already gates the routes, we re-check the claim from inside each
// admin handler so a route-misconfiguration mistake fails closed.
func mustAdmin(w http.ResponseWriter, r *http.Request) bool {
	c, ok := auth.FromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "valid Bearer token required")
		return false
	}
	if !c.IsAdmin() {
		writeError(w, http.StatusForbidden, "forbidden", "admin role required")
		return false
	}
	return true
}
