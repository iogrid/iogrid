// Route-level smoke tests for the IdentityHandler. The full happy-path
// (Postgres-backed store, real bearer, transactional remove) is covered
// in identity-svc's integration suite; these unit tests pin the
// "rejects without bearer" + "rejects cross-user" contracts that
// reviewers care about.
package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
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
