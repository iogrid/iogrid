// interceptor_test.go: regression coverage for issue #321.
//
// The header-forwarding interceptor in interceptor.go reads the
// caller's *auth.Claims out of the call context via CallerClaims(ctx).
// Until PR #336 wired the interceptor, the BFF called identity-svc
// anonymously; until this file's PR landed, the BFF set the claims in
// the REQUEST context (auth.Middleware) but never re-keyed them under
// the clients package key, so the interceptor still saw nothing and
// /api/v1/me returned 401 end-to-end.
//
// These tests pin the bridge: an inbound HTTP request that carried
// auth.Claims MUST end up with clients.CallerClaims populated by the
// time the handler invokes a downstream Connect-RPC client.

package clients

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
)

func TestPropagateClaimsMiddleware_AttachesClaims(t *testing.T) {
	wantUID := uuid.New()
	wantSID := "sess-123"
	want := &auth.Claims{}
	want.Subject = wantUID.String()
	want.ID = wantSID
	want.PrimaryEmail = "hatice@example.org"
	want.Roles = []string{"USER_ROLE_ADMIN"}

	var got *auth.Claims
	var ok bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got, ok = CallerClaims(r.Context())
	})

	h := PropagateClaimsMiddleware(next)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	// Simulate what auth.Middleware does: stuff claims into the
	// request context under the auth package's key.
	req = req.WithContext(auth.NewContextForTesting(req.Context(), want))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !ok || got == nil {
		t.Fatalf("CallerClaims missing after middleware: ok=%v got=%v", ok, got)
	}
	if got.UserID() != wantUID {
		t.Errorf("UserID: got %s want %s", got.UserID(), wantUID)
	}
	if got.SessionID() != wantSID {
		t.Errorf("SessionID: got %q want %q", got.SessionID(), wantSID)
	}
	if got.PrimaryEmail != want.PrimaryEmail {
		t.Errorf("PrimaryEmail: got %q want %q", got.PrimaryEmail, want.PrimaryEmail)
	}
	if len(got.Roles) != 1 || got.Roles[0] != "USER_ROLE_ADMIN" {
		t.Errorf("Roles: got %v want [USER_ROLE_ADMIN]", got.Roles)
	}
}

func TestPropagateClaimsMiddleware_AnonymousPassesThrough(t *testing.T) {
	called := false
	var seen bool
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		called = true
		_, seen = CallerClaims(r.Context())
	})

	h := PropagateClaimsMiddleware(next)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !called {
		t.Fatal("next handler was not invoked")
	}
	if seen {
		t.Errorf("CallerClaims should be empty on anonymous request, got non-nil")
	}
}
