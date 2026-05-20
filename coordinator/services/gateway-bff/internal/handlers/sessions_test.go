// Tests for /api/v1/account/sessions surface (issue #322). Mirror the
// shape of the GetMe / RemoveMyIdentifier tests in handlers_test.go.
package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
)

// withRouter mounts the per-test handler on a chi router so the URLParam
// path-binding fires the same way it does in production. Sessions
// handlers read `id` via chi.URLParam.
func withRouter(h http.HandlerFunc, pattern string) http.Handler {
	r := chi.NewRouter()
	r.MethodFunc(http.MethodDelete, pattern, h)
	r.MethodFunc(http.MethodGet, pattern, h)
	return r
}

func TestListSessionsForAccount_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/account/sessions", nil)
	w := httptest.NewRecorder()
	api.ListSessionsForAccount(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestListSessionsForAccount_ForwardsToAuth(t *testing.T) {
	called := false
	set := &clients.Set{
		Auth: &mockAuth{
			listSessions: func(ctx context.Context, _ *identityv1.ListSessionsRequest) (*identityv1.ListSessionsResponse, error) {
				called = true
				// Caller-claim threading: gateway-bff should have
				// stashed the caller's claims on the ctx before
				// invoking the client so the header-forwarding
				// interceptor can stamp them on the outbound call.
				if _, ok := clients.CallerClaims(ctx); !ok {
					t.Errorf("expected caller claims on outbound ctx")
				}
				return &identityv1.ListSessionsResponse{
					Sessions: []*identityv1.Session{
						{
							Id:        &commonv1.UUID{Value: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
							UserAgent: "Mozilla/5.0",
							IpAddress: "203.0.113.1",
							IsCurrent: true,
						},
					},
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/account/sessions", nil))
	w := httptest.NewRecorder()
	api.ListSessionsForAccount(w, r)

	if !called {
		t.Fatal("auth.ListSessions not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp identityv1.ListSessionsResponse
	mustReadJSON(t, w.Body, &resp)
	if len(resp.Sessions) != 1 || !resp.Sessions[0].IsCurrent {
		t.Fatalf("unexpected response %#v", &resp)
	}
}

func TestRevokeAccountSession_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/account/sessions/abc", nil)
	w := httptest.NewRecorder()
	api.RevokeAccountSession(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestRevokeAccountSession_ForwardsToAuth(t *testing.T) {
	called := false
	sid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	set := &clients.Set{
		Auth: &mockAuth{
			revokeSession: func(ctx context.Context, req *identityv1.RevokeSessionRequest) (*identityv1.RevokeSessionResponse, error) {
				called = true
				if req.SessionId == nil || req.SessionId.Value != sid {
					t.Errorf("unexpected id %#v", req.SessionId)
				}
				if _, ok := clients.CallerClaims(ctx); !ok {
					t.Errorf("expected caller claims on outbound ctx")
				}
				return &identityv1.RevokeSessionResponse{}, nil
			},
		},
	}
	api := newAPI(t, set)
	srv := withRouter(api.RevokeAccountSession, "/api/v1/account/sessions/{id}")
	r := withAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/account/sessions/"+sid, nil))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)

	if !called {
		t.Fatal("auth.RevokeSession not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRevokeAccountSession_MapsConflict(t *testing.T) {
	set := &clients.Set{
		Auth: &mockAuth{
			revokeSession: func(_ context.Context, _ *identityv1.RevokeSessionRequest) (*identityv1.RevokeSessionResponse, error) {
				return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("cannot revoke current"))
			},
		},
	}
	api := newAPI(t, set)
	srv := withRouter(api.RevokeAccountSession, "/api/v1/account/sessions/{id}")
	sid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	r := withAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/account/sessions/"+sid, nil))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestRevokeAccountSession_MapsNotFound(t *testing.T) {
	set := &clients.Set{
		Auth: &mockAuth{
			revokeSession: func(_ context.Context, _ *identityv1.RevokeSessionRequest) (*identityv1.RevokeSessionResponse, error) {
				return nil, connect.NewError(connect.CodeNotFound, errors.New("not found"))
			},
		},
	}
	api := newAPI(t, set)
	srv := withRouter(api.RevokeAccountSession, "/api/v1/account/sessions/{id}")
	sid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	r := withAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/account/sessions/"+sid, nil))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}
