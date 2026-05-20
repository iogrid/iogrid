// Route-level smoke tests for the IdentityHandler. The full happy-path
// (Postgres-backed store, real bearer, transactional remove) is covered
// in identity-svc's integration suite; these unit tests pin the
// "rejects without bearer" + "rejects cross-user" contracts that
// reviewers care about.
package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/tokens"
)

func TestIdentityHandler_RemoveIdentifier_RequiresBearer(t *testing.T) {
	h := NewIdentityHandler(nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountIdentityJSON(r)
	})

	uid := uuid.New().String()
	idID := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/v1/users/"+uid+"/identifiers/"+idID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestIdentityHandler_RemoveIdentifier_RejectsCrossUser(t *testing.T) {
	h := NewIdentityHandler(nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountIdentityJSON(r)
	})

	authed := uuid.New()
	other := uuid.New().String()
	idID := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/v1/users/"+other+"/identifiers/"+idID, nil)
	req = req.WithContext(authmw.WithAuthedUser(req.Context(), authed))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestIdentityHandler_DeleteAccount_RequiresBearer(t *testing.T) {
	h := NewIdentityHandler(nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountIdentityJSON(r)
	})

	uid := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/v1/users/"+uid+"/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestIdentityHandler_DeleteAccount_RejectsCrossUser(t *testing.T) {
	h := NewIdentityHandler(nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountIdentityJSON(r)
	})

	authed := uuid.New()
	other := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/v1/users/"+other+"/", nil)
	req = req.WithContext(authmw.WithAuthedUser(req.Context(), authed))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- GetUser ------------------------------------------------------------

// GetUser without a bearer must return CodeUnauthenticated regardless of
// whether the request supplies a target id. Pins the auth boundary.
func TestIdentityHandler_GetUser_RequiresBearer(t *testing.T) {
	h := NewIdentityHandler(nil)
	_, err := h.GetUser(context.Background(), connect.NewRequest(&identityv1.GetUserRequest{}))
	if err == nil {
		t.Fatalf("expected CodeUnauthenticated, got nil error")
	}
	if got := connect.CodeOf(err); got != connect.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated, got %s", got)
	}
}

// Cross-user GetUser (target id ≠ caller id) without the USER_ROLE_ADMIN
// claim must return CodePermissionDenied. The store is intentionally nil
// so the test fails fast if the gate slips and the handler dives into
// the store layer.
func TestIdentityHandler_GetUser_RejectsCrossUserWithoutAdmin(t *testing.T) {
	h := NewIdentityHandler(nil)
	caller := uuid.New()
	target := uuid.New()
	ctx := authmw.WithAuthedUser(context.Background(), caller)
	// Caller has roles but none of them is admin.
	ctx = authmw.WithAuthedClaims(ctx, &tokens.AccessClaims{Roles: []string{"USER_ROLE_PROVIDER"}})
	_, err := h.GetUser(ctx, connect.NewRequest(&identityv1.GetUserRequest{
		Id: &commonv1.UUID{Value: target.String()},
	}))
	if err == nil {
		t.Fatalf("expected CodePermissionDenied, got nil error")
	}
	if got := connect.CodeOf(err); got != connect.CodePermissionDenied {
		t.Fatalf("expected CodePermissionDenied, got %s", got)
	}
}

// GetUser with a malformed id surfaces CodeInvalidArgument rather than
// crashing or silently coercing to the caller. Defence-in-depth against
// upstream BFF sloppiness.
func TestIdentityHandler_GetUser_RejectsMalformedID(t *testing.T) {
	h := NewIdentityHandler(nil)
	caller := uuid.New()
	ctx := authmw.WithAuthedUser(context.Background(), caller)
	_, err := h.GetUser(ctx, connect.NewRequest(&identityv1.GetUserRequest{
		Id: &commonv1.UUID{Value: "not-a-uuid"},
	}))
	if err == nil {
		t.Fatalf("expected CodeInvalidArgument, got nil error")
	}
	if got := connect.CodeOf(err); got != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got %s", got)
	}
}

// GetUser with a nil store surfaces CodeInternal — the caller is
// authed and the id resolution path completes, but persistence is
// unavailable. Covers the misconfigured-process case.
func TestIdentityHandler_GetUser_NilStoreReturnsInternal(t *testing.T) {
	h := NewIdentityHandler(nil)
	caller := uuid.New()
	ctx := authmw.WithAuthedUser(context.Background(), caller)
	_, err := h.GetUser(ctx, connect.NewRequest(&identityv1.GetUserRequest{}))
	if err == nil {
		t.Fatalf("expected CodeInternal, got nil error")
	}
	if got := connect.CodeOf(err); got != connect.CodeInternal {
		t.Fatalf("expected CodeInternal, got %s", got)
	}
}

func TestIdentityHandler_DeleteAccount_RequiresStepUp(t *testing.T) {
	h := NewIdentityHandler(nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountIdentityJSON(r)
	})

	authed := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/v1/users/"+authed.String()+"/", nil)
	// Authed but no step-up claim attached.
	req = req.WithContext(authmw.WithAuthedUser(req.Context(), authed))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 (step_up_required), got %d body=%s", w.Code, w.Body.String())
	}
}
