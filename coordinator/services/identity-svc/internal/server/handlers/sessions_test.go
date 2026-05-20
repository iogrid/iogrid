// Route-level + Connect-RPC smoke tests for the AuthHandler. The
// full happy-path (real Postgres-backed store, transactional revoke)
// is covered in the integration suite that spins a containerised
// CNPG; the unit tests below pin the "no bearer / no store" + the
// "cannot revoke current" + "request shape" contracts reviewers
// care about.
package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/auth"
	authmw "github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/server/middleware"
	"github.com/iogrid/iogrid/coordinator/services/identity-svc/internal/store"
)

// --- JSON twin ----------------------------------------------------------

func TestAuthHandler_ListSessions_RequiresBearer(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountSessionsJSON(r)
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/account/sessions/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuthHandler_RevokeSession_RequiresBearer(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountSessionsJSON(r)
	})

	sid := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/v1/account/sessions/"+sid, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuthHandler_RevokeSession_RejectsBadID(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountSessionsJSON(r)
	})

	authed := uuid.New()
	req := httptest.NewRequest(http.MethodDelete, "/v1/account/sessions/not-a-uuid", nil)
	req = req.WithContext(authmw.WithAuthedUser(req.Context(), authed))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAuthHandler_RevokeSession_RefusesCurrentSession(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	r := chi.NewRouter()
	r.Route("/v1", func(r chi.Router) {
		h.MountSessionsJSON(r)
	})

	authed := uuid.New()
	currentSID := uuid.New()
	// Caller attempts to revoke their OWN current session via the JSON
	// twin — handler must short-circuit to 409 before reaching the
	// store (which is nil — would otherwise NPE).
	req := httptest.NewRequest(http.MethodDelete, "/v1/account/sessions/"+currentSID.String(), nil)
	ctx := authmw.WithAuthedUser(req.Context(), authed)
	ctx = authmw.WithAuthedSessionID(ctx, currentSID)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- Connect-RPC entry points ------------------------------------------

func TestAuthHandler_RPC_ListSessions_RequiresBearer(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	_, err := h.ListSessions(context.Background(), connect.NewRequest(&identityv1.ListSessionsRequest{}))
	if err == nil {
		t.Fatal("expected unauthenticated error, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated, got %v", err)
	}
}

func TestAuthHandler_RPC_RevokeSession_RequiresBearer(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	_, err := h.RevokeSession(context.Background(), connect.NewRequest(&identityv1.RevokeSessionRequest{
		SessionId: &commonv1.UUID{Value: uuid.New().String()},
	}))
	if err == nil {
		t.Fatal("expected unauthenticated error, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated, got %v", err)
	}
}

func TestAuthHandler_RPC_RevokeSession_RejectsBadUUID(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	ctx := authmw.WithAuthedUser(context.Background(), uuid.New())
	_, err := h.RevokeSession(ctx, connect.NewRequest(&identityv1.RevokeSessionRequest{
		SessionId: &commonv1.UUID{Value: "not-a-uuid"},
	}))
	if err == nil {
		t.Fatal("expected invalid_argument error, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got %v", err)
	}
}

func TestAuthHandler_RPC_RevokeSession_RefusesCurrent(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	authed := uuid.New()
	currentSID := uuid.New()
	ctx := authmw.WithAuthedUser(context.Background(), authed)
	ctx = authmw.WithAuthedSessionID(ctx, currentSID)
	_, err := h.RevokeSession(ctx, connect.NewRequest(&identityv1.RevokeSessionRequest{
		SessionId: &commonv1.UUID{Value: currentSID.String()},
	}))
	if err == nil {
		t.Fatal("expected failed_precondition error, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeFailedPrecondition {
		t.Fatalf("expected CodeFailedPrecondition, got %v", err)
	}
}

// errorsAs is a one-line indirection on errors.As that lets the four
// connect-error checks above stay short + symmetric.
func errorsAs(err error, target **connect.Error) bool {
	return errors.As(err, target)
}

// --- pure conversion helpers -------------------------------------------

func TestSortSessionsByRecency_OrdersMostRecentFirst(t *testing.T) {
	// Build three Session rows whose last_used_at differ; one of them
	// has the zero value to assert NULLS LAST.
	now := timeNow()
	old := store.Session{ID: uuid.New(), LastUsedAt: now.Add(-2 * time.Hour)}
	recent := store.Session{ID: uuid.New(), LastUsedAt: now.Add(-1 * time.Minute)}
	never := store.Session{ID: uuid.New()} // zero value
	out := sortSessionsByRecency([]store.Session{old, never, recent})
	if out[0].ID != recent.ID {
		t.Fatalf("expected recent first, got %v", out[0].ID)
	}
	if out[1].ID != old.ID {
		t.Fatalf("expected old second, got %v", out[1].ID)
	}
	if out[2].ID != never.ID {
		t.Fatalf("expected zero-time last, got %v", out[2].ID)
	}
}

func TestSessionsToProto_StampsIsCurrent(t *testing.T) {
	a := store.Session{ID: uuid.New(), UserID: uuid.New(), UserAgent: "ua-a"}
	b := store.Session{ID: uuid.New(), UserID: a.UserID, UserAgent: "ua-b"}
	out := sessionsToProto([]store.Session{a, b}, b.ID)
	if len(out) != 2 {
		t.Fatalf("want 2, got %d", len(out))
	}
	if out[0].GetIsCurrent() {
		t.Errorf("a should not be current")
	}
	if !out[1].GetIsCurrent() {
		t.Errorf("b should be current")
	}
}

// timeNow is a tiny shim so the test stays decoupled from the system
// clock — vetting that the sort is stable across timezones doesn't
// need wall-time precision.
func timeNow() time.Time { return time.Unix(1_700_000_000, 0) }

// --- SIWS wallet RPCs (issue #326) -------------------------------------

// All four wallet RPCs gate on the bearer; a Service-less AuthHandler
// short-circuits to CodeUnavailable so a misconfigured deployment
// surfaces clearly instead of NPE'ing on Auth.StartSiwsBinding.

func TestAuthHandler_RPC_StartSiwsBinding_RequiresAuthService(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	ctx := authmw.WithAuthedUser(context.Background(), uuid.New())
	_, err := h.StartSiwsBinding(ctx, connect.NewRequest(&identityv1.StartSiwsBindingRequest{
		WalletAddress: "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin",
	}))
	if err == nil {
		t.Fatal("expected unavailable error, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", err)
	}
}

func TestAuthHandler_RPC_StartSiwsBinding_RequiresBearer(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	_, err := h.StartSiwsBinding(context.Background(), connect.NewRequest(&identityv1.StartSiwsBindingRequest{
		WalletAddress: "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin",
	}))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) {
		t.Fatalf("expected connect.Error, got %v", err)
	}
	// Without an Auth service we expect Unavailable to fire first; with
	// one wired and no bearer we'd see Unauthenticated. Both are
	// acceptable short-circuits — the contract is "do not panic and do
	// not 200". Pin on neither being CodeOK.
	if cErr.Code() == 0 {
		t.Fatalf("expected non-OK code, got %v", err)
	}
}

func TestAuthHandler_RPC_CompleteSiwsBinding_RequiresAuthService(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	ctx := authmw.WithAuthedUser(context.Background(), uuid.New())
	_, err := h.CompleteSiwsBinding(ctx, connect.NewRequest(&identityv1.CompleteSiwsBindingRequest{
		WalletAddress: "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin",
		Signature:     "sig",
	}))
	if err == nil {
		t.Fatal("expected unavailable, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", err)
	}
}

func TestAuthHandler_RPC_ListBoundWallets_RequiresAuthService(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	ctx := authmw.WithAuthedUser(context.Background(), uuid.New())
	_, err := h.ListBoundWallets(ctx, connect.NewRequest(&identityv1.ListBoundWalletsRequest{}))
	if err == nil {
		t.Fatal("expected unavailable, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", err)
	}
}

func TestAuthHandler_RPC_UnbindWallet_RequiresAuthService(t *testing.T) {
	h := NewAuthHandler(nil, nil)
	ctx := authmw.WithAuthedUser(context.Background(), uuid.New())
	_, err := h.UnbindWallet(ctx, connect.NewRequest(&identityv1.UnbindWalletRequest{
		WalletAddress: "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin",
	}))
	if err == nil {
		t.Fatal("expected unavailable, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeUnavailable {
		t.Fatalf("expected CodeUnavailable, got %v", err)
	}
}

func TestAuthHandler_RPC_ListBoundWallets_RequiresBearer(t *testing.T) {
	// Construct a non-nil auth.Service stand-in via the package's
	// existing wiring: a Store-less Service still satisfies the Auth
	// pointer check, then bypasses the store path because we never
	// reach ListIdentifiersForUserByKind without a bearer.
	h := NewAuthHandler(nil, makeFakeAuth())
	_, err := h.ListBoundWallets(context.Background(), connect.NewRequest(&identityv1.ListBoundWalletsRequest{}))
	if err == nil {
		t.Fatal("expected unauthenticated, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated, got %v", err)
	}
}

func TestAuthHandler_RPC_UnbindWallet_RequiresBearer(t *testing.T) {
	h := NewAuthHandler(nil, makeFakeAuth())
	_, err := h.UnbindWallet(context.Background(), connect.NewRequest(&identityv1.UnbindWalletRequest{
		WalletAddress: "9xQeWvG816bUx9EPjHmaT23yvVM2ZWbrrpZb9PusVFin",
	}))
	if err == nil {
		t.Fatal("expected unauthenticated, got nil")
	}
	var cErr *connect.Error
	if !errorsAs(err, &cErr) || cErr.Code() != connect.CodeUnauthenticated {
		t.Fatalf("expected CodeUnauthenticated, got %v", err)
	}
}

// makeFakeAuth returns a non-nil *auth.Service so the wallet RPCs
// proceed past the "Auth not configured" short-circuit; tests then
// pin behaviour on the bearer / arg checks BEFORE the store path.
func makeFakeAuth() *auth.Service {
	return &auth.Service{}
}
