package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"

	abusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
)

// --- shared mocks --------------------------------------------------------

type mockIdentity struct {
	getUser          func(context.Context, *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error)
	updateUser       func(context.Context, *identityv1.UpdateUserRequest) (*identityv1.UpdateUserResponse, error)
	removeIdentifier func(context.Context, *identityv1.RemoveIdentifierRequest) (*identityv1.RemoveIdentifierResponse, error)
	deleteAccount    func(context.Context, *identityv1.DeleteAccountRequest) (*identityv1.DeleteAccountResponse, error)
}

func (m *mockIdentity) GetUser(ctx context.Context, req *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error) {
	return m.getUser(ctx, req)
}
func (m *mockIdentity) UpdateUser(ctx context.Context, req *identityv1.UpdateUserRequest) (*identityv1.UpdateUserResponse, error) {
	return m.updateUser(ctx, req)
}
func (m *mockIdentity) RemoveIdentifier(ctx context.Context, req *identityv1.RemoveIdentifierRequest) (*identityv1.RemoveIdentifierResponse, error) {
	if m.removeIdentifier == nil {
		return &identityv1.RemoveIdentifierResponse{}, nil
	}
	return m.removeIdentifier(ctx, req)
}
func (m *mockIdentity) DeleteAccount(ctx context.Context, req *identityv1.DeleteAccountRequest) (*identityv1.DeleteAccountResponse, error) {
	if m.deleteAccount == nil {
		return &identityv1.DeleteAccountResponse{}, nil
	}
	return m.deleteAccount(ctx, req)
}

type mockAuth struct {
	startGoogle    func(context.Context, *identityv1.StartGoogleSignInRequest) (*identityv1.StartGoogleSignInResponse, error)
	completeGoogle func(context.Context, *identityv1.CompleteGoogleSignInRequest) (*identityv1.CompleteGoogleSignInResponse, error)
	requestMagic   func(context.Context, *identityv1.RequestMagicLinkRequest) (*identityv1.RequestMagicLinkResponse, error)
	completeMagic  func(context.Context, *identityv1.CompleteMagicLinkRequest) (*identityv1.CompleteMagicLinkResponse, error)
	refresh        func(context.Context, *identityv1.RefreshTokenRequest) (*identityv1.RefreshTokenResponse, error)
	signOut        func(context.Context, *identityv1.SignOutRequest) (*identityv1.SignOutResponse, error)
	listSessions   func(context.Context, *identityv1.ListSessionsRequest) (*identityv1.ListSessionsResponse, error)
}

func (m *mockAuth) StartGoogleSignIn(ctx context.Context, req *identityv1.StartGoogleSignInRequest) (*identityv1.StartGoogleSignInResponse, error) {
	return m.startGoogle(ctx, req)
}
func (m *mockAuth) CompleteGoogleSignIn(ctx context.Context, req *identityv1.CompleteGoogleSignInRequest) (*identityv1.CompleteGoogleSignInResponse, error) {
	return m.completeGoogle(ctx, req)
}
func (m *mockAuth) RequestMagicLink(ctx context.Context, req *identityv1.RequestMagicLinkRequest) (*identityv1.RequestMagicLinkResponse, error) {
	return m.requestMagic(ctx, req)
}
func (m *mockAuth) CompleteMagicLink(ctx context.Context, req *identityv1.CompleteMagicLinkRequest) (*identityv1.CompleteMagicLinkResponse, error) {
	return m.completeMagic(ctx, req)
}
func (m *mockAuth) RefreshToken(ctx context.Context, req *identityv1.RefreshTokenRequest) (*identityv1.RefreshTokenResponse, error) {
	return m.refresh(ctx, req)
}
func (m *mockAuth) SignOut(ctx context.Context, req *identityv1.SignOutRequest) (*identityv1.SignOutResponse, error) {
	return m.signOut(ctx, req)
}
func (m *mockAuth) ListSessions(ctx context.Context, req *identityv1.ListSessionsRequest) (*identityv1.ListSessionsResponse, error) {
	return m.listSessions(ctx, req)
}

type mockDashboard struct {
	listEvents      func(context.Context, *providersv1.ListAuditEventsRequest) (*providersv1.ListAuditEventsResponse, error)
	streamEvents    func(context.Context, *providersv1.StreamAuditEventsRequest) (clients.AuditEventStream, error)
	earningsSummary func(context.Context, *providersv1.GetEarningsSummaryRequest) (*providersv1.GetEarningsSummaryResponse, error)
}

func (m *mockDashboard) ListAuditEvents(ctx context.Context, req *providersv1.ListAuditEventsRequest) (*providersv1.ListAuditEventsResponse, error) {
	return m.listEvents(ctx, req)
}
func (m *mockDashboard) StreamAuditEvents(ctx context.Context, req *providersv1.StreamAuditEventsRequest) (clients.AuditEventStream, error) {
	return m.streamEvents(ctx, req)
}
func (m *mockDashboard) GetEarningsSummary(ctx context.Context, req *providersv1.GetEarningsSummaryRequest) (*providersv1.GetEarningsSummaryResponse, error) {
	return m.earningsSummary(ctx, req)
}

type mockScheduling struct {
	getConfig    func(context.Context, *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error)
	updateConfig func(context.Context, *providersv1.UpdateSchedulingConfigRequest) (*providersv1.UpdateSchedulingConfigResponse, error)
	getState     func(context.Context, *providersv1.GetCurrentStateRequest) (*providersv1.GetCurrentStateResponse, error)
}

func (m *mockScheduling) GetSchedulingConfig(ctx context.Context, req *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error) {
	return m.getConfig(ctx, req)
}
func (m *mockScheduling) UpdateSchedulingConfig(ctx context.Context, req *providersv1.UpdateSchedulingConfigRequest) (*providersv1.UpdateSchedulingConfigResponse, error) {
	return m.updateConfig(ctx, req)
}
func (m *mockScheduling) GetCurrentState(ctx context.Context, req *providersv1.GetCurrentStateRequest) (*providersv1.GetCurrentStateResponse, error) {
	return m.getState(ctx, req)
}

// mockRegistration backs the ProviderRegistrationService read-side that
// /provide/* uses to gate by ownership.
type mockRegistration struct {
	listProviders func(context.Context, *providersv1.ListProvidersRequest) (*providersv1.ListProvidersResponse, error)
}

func (m *mockRegistration) ListProviders(ctx context.Context, req *providersv1.ListProvidersRequest) (*providersv1.ListProvidersResponse, error) {
	if m.listProviders == nil {
		return &providersv1.ListProvidersResponse{}, nil
	}
	return m.listProviders(ctx, req)
}

// staticRegistration returns a Registration mock that always reports the
// given list of providers as owned by the caller. Helper used by the
// /provide/* fan-out tests so each one doesn't re-declare the same
// boilerplate.
func staticRegistration(providers ...*providersv1.Provider) *mockRegistration {
	return &mockRegistration{
		listProviders: func(_ context.Context, _ *providersv1.ListProvidersRequest) (*providersv1.ListProvidersResponse, error) {
			return &providersv1.ListProvidersResponse{Providers: providers}, nil
		},
	}
}

type mockWorkloads struct {
	submit     func(context.Context, *workloadsv1.SubmitWorkloadRequest) (*workloadsv1.SubmitWorkloadResponse, error)
	get        func(context.Context, *workloadsv1.GetWorkloadRequest) (*workloadsv1.GetWorkloadResponse, error)
	list       func(context.Context, *workloadsv1.ListWorkloadsRequest) (*workloadsv1.ListWorkloadsResponse, error)
	cancel     func(context.Context, *workloadsv1.CancelWorkloadRequest) (*workloadsv1.CancelWorkloadResponse, error)
	streamEv   func(context.Context, *workloadsv1.StreamWorkloadEventsRequest) (clients.WorkloadEventStream, error)
}

func (m *mockWorkloads) SubmitWorkload(ctx context.Context, req *workloadsv1.SubmitWorkloadRequest) (*workloadsv1.SubmitWorkloadResponse, error) {
	return m.submit(ctx, req)
}
func (m *mockWorkloads) GetWorkload(ctx context.Context, req *workloadsv1.GetWorkloadRequest) (*workloadsv1.GetWorkloadResponse, error) {
	return m.get(ctx, req)
}
func (m *mockWorkloads) ListWorkloads(ctx context.Context, req *workloadsv1.ListWorkloadsRequest) (*workloadsv1.ListWorkloadsResponse, error) {
	return m.list(ctx, req)
}
func (m *mockWorkloads) CancelWorkload(ctx context.Context, req *workloadsv1.CancelWorkloadRequest) (*workloadsv1.CancelWorkloadResponse, error) {
	return m.cancel(ctx, req)
}
func (m *mockWorkloads) StreamWorkloadEvents(ctx context.Context, req *workloadsv1.StreamWorkloadEventsRequest) (clients.WorkloadEventStream, error) {
	return m.streamEv(ctx, req)
}

type mockAntiabuse struct {
	listFilters func(context.Context, *abusev1.ListFiltersRequest) (*abusev1.ListFiltersResponse, error)
}

func (m *mockAntiabuse) ListFilters(ctx context.Context, req *abusev1.ListFiltersRequest) (*abusev1.ListFiltersResponse, error) {
	return m.listFilters(ctx, req)
}

type mockBilling struct {
	getSub          func(context.Context, *billingv1.GetSubscriptionRequest) (*billingv1.GetSubscriptionResponse, error)
	listUsage       func(context.Context, *billingv1.ListUsageRequest) (*billingv1.ListUsageResponse, error)
	createCheckout  func(context.Context, *billingv1.CreateCheckoutSessionRequest) (*billingv1.CreateCheckoutSessionResponse, error)
}

func (m *mockBilling) GetSubscription(ctx context.Context, req *billingv1.GetSubscriptionRequest) (*billingv1.GetSubscriptionResponse, error) {
	return m.getSub(ctx, req)
}
func (m *mockBilling) ListUsage(ctx context.Context, req *billingv1.ListUsageRequest) (*billingv1.ListUsageResponse, error) {
	return m.listUsage(ctx, req)
}
func (m *mockBilling) CreateCheckoutSession(ctx context.Context, req *billingv1.CreateCheckoutSessionRequest) (*billingv1.CreateCheckoutSessionResponse, error) {
	return m.createCheckout(ctx, req)
}

// --- shared fixtures -----------------------------------------------------

const fakeUserID = "11111111-2222-3333-4444-555555555555"

// withAuth returns r with a synthetic Claims wired into the context so
// the per-route auth gates pass.
func withAuth(r *http.Request, roles ...string) *http.Request {
	c := &auth.Claims{Roles: roles}
	c.Subject = fakeUserID
	ctx := context.WithValue(r.Context(), claimsKey{}, c)
	_ = ctx // for documentation; real implementation goes through auth.FromContext
	// Use the auth package's internal context helper via the public
	// Middleware contract: just attach a Bearer header that the test's
	// Verifier accepts. Cleaner: bypass middleware by hand-attaching.
	return r.WithContext(withClaimsForTest(r.Context(), c))
}

// withClaimsForTest is a test-only shim so we don't depend on the
// (unexported) withClaims symbol from the auth package.
func withClaimsForTest(ctx context.Context, c *auth.Claims) context.Context {
	return auth.NewContextForTesting(ctx, c)
}

// claimsKey only exists to satisfy the unused-variable check in
// withAuth; the real plumbing routes through NewContextForTesting.
type claimsKey struct{}

func newAPI(t *testing.T, set *clients.Set) *API {
	t.Helper()
	return New(set, NewMemoryAPIKeyStore(), nil)
}

func mustReadJSON(t *testing.T, body io.Reader, out any) {
	t.Helper()
	b, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("unmarshal: %v: %s", err, string(b))
	}
}

// --- account / me --------------------------------------------------------

func TestGetMe_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	w := httptest.NewRecorder()
	api.GetMe(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestGetMe_ForwardsToIdentity(t *testing.T) {
	called := false
	set := &clients.Set{
		Identity: &mockIdentity{
			getUser: func(_ context.Context, req *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error) {
				called = true
				if req.Id == nil || req.Id.Value != fakeUserID {
					t.Fatalf("unexpected id %#v", req.Id)
				}
				return &identityv1.GetUserResponse{
					User: &identityv1.User{
						Id:           &commonv1.UUID{Value: fakeUserID},
						PrimaryEmail: "test@iogrid.org",
					},
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	w := httptest.NewRecorder()
	api.GetMe(w, r)

	if !called {
		t.Fatal("identity.GetUser not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp identityv1.GetUserResponse
	mustReadJSON(t, w.Body, &resp)
	if resp.User == nil || resp.User.PrimaryEmail != "test@iogrid.org" {
		t.Fatalf("unexpected response %#v", &resp)
	}
}

// --- DELETE /me/identifiers/{id} -----------------------------------------

func TestRemoveMyIdentifier_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/me/identifiers/abc", nil)
	w := httptest.NewRecorder()
	api.RemoveMyIdentifier(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestRemoveMyIdentifier_ForwardsToIdentity(t *testing.T) {
	called := false
	identifierID := "99999999-aaaa-bbbb-cccc-dddddddddddd"
	set := &clients.Set{
		Identity: &mockIdentity{
			getUser: func(_ context.Context, _ *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error) {
				return &identityv1.GetUserResponse{}, nil
			},
			updateUser: func(_ context.Context, _ *identityv1.UpdateUserRequest) (*identityv1.UpdateUserResponse, error) {
				return &identityv1.UpdateUserResponse{}, nil
			},
			removeIdentifier: func(_ context.Context, req *identityv1.RemoveIdentifierRequest) (*identityv1.RemoveIdentifierResponse, error) {
				called = true
				if req.UserId == nil || req.UserId.Value != fakeUserID {
					t.Fatalf("unexpected user_id %#v", req.UserId)
				}
				if req.IdentifierId == nil || req.IdentifierId.Value != identifierID {
					t.Fatalf("unexpected identifier_id %#v", req.IdentifierId)
				}
				return &identityv1.RemoveIdentifierResponse{}, nil
			},
		},
	}
	api := newAPI(t, set)
	// Route through chi so the {id} URL param resolves the same way the
	// production router does.
	router := chi.NewRouter()
	router.Delete("/api/v1/me/identifiers/{id}", api.RemoveMyIdentifier)
	r := withAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/me/identifiers/"+identifierID, nil))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if !called {
		t.Fatal("identity.RemoveIdentifier not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// --- DELETE /me ----------------------------------------------------------

func TestDeleteMyAccount_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	r := httptest.NewRequest(http.MethodDelete, "/api/v1/me", nil)
	w := httptest.NewRecorder()
	api.DeleteMyAccount(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestDeleteMyAccount_ForwardsToIdentity(t *testing.T) {
	called := false
	set := &clients.Set{
		Identity: &mockIdentity{
			getUser: func(_ context.Context, _ *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error) {
				return &identityv1.GetUserResponse{}, nil
			},
			updateUser: func(_ context.Context, _ *identityv1.UpdateUserRequest) (*identityv1.UpdateUserResponse, error) {
				return &identityv1.UpdateUserResponse{}, nil
			},
			deleteAccount: func(_ context.Context, req *identityv1.DeleteAccountRequest) (*identityv1.DeleteAccountResponse, error) {
				called = true
				if req.UserId == nil || req.UserId.Value != fakeUserID {
					t.Fatalf("unexpected user_id %#v", req.UserId)
				}
				if req.Reason != "switching to other provider" {
					t.Fatalf("unexpected reason %q", req.Reason)
				}
				return &identityv1.DeleteAccountResponse{SessionsRevoked: 2}, nil
			},
		},
	}
	api := newAPI(t, set)
	body, _ := json.Marshal(map[string]string{"reason": "switching to other provider"})
	r := withAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/me", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.DeleteMyAccount(w, r)
	if !called {
		t.Fatal("identity.DeleteAccount not called")
	}
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"sessions_revoked":2`) &&
		!strings.Contains(w.Body.String(), `"sessionsRevoked":2`) {
		t.Fatalf("missing sessions_revoked in body: %s", w.Body.String())
	}
}

func TestStartGoogleSignIn(t *testing.T) {
	set := &clients.Set{
		Auth: &mockAuth{
			startGoogle: func(_ context.Context, req *identityv1.StartGoogleSignInRequest) (*identityv1.StartGoogleSignInResponse, error) {
				if req.ReturnTo != "https://app.iogrid.org/post" || req.CodeChallenge != "abc" {
					t.Fatalf("unexpected request %#v", req)
				}
				return &identityv1.StartGoogleSignInResponse{AuthorizeUrl: "https://accounts.google.com/o/oauth2/v2/auth?state=xyz", State: "xyz"}, nil
			},
		},
	}
	api := newAPI(t, set)
	body, _ := json.Marshal(map[string]string{"return_to": "https://app.iogrid.org/post", "code_challenge": "abc"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/account/sign-in/google", bytes.NewReader(body))
	w := httptest.NewRecorder()
	api.StartGoogleSignIn(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "accounts.google.com") {
		t.Fatalf("missing authorize_url: %s", w.Body.String())
	}
}

func TestRequestMagicLink_EmptyEmail400(t *testing.T) {
	api := newAPI(t, &clients.Set{Auth: &mockAuth{}})
	body, _ := json.Marshal(map[string]string{"email": ""})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/account/sign-in/magic", bytes.NewReader(body))
	w := httptest.NewRecorder()
	api.RequestMagicLink(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// --- provide -------------------------------------------------------------

func TestGetProviderDashboard_FansOut(t *testing.T) {
	dashCalls := 0
	earningsCalled := false
	stateCalled := false
	pid := uuid.NewString()
	owned := &providersv1.Provider{
		Id:          &commonv1.UUID{Value: pid},
		OwnerUserId: &commonv1.UUID{Value: fakeUserID},
		Status:      providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE,
	}
	var seenSchedPID, seenEarnPID, seenAuditPID string
	set := &clients.Set{
		ProvidersDashboard: &mockDashboard{
			earningsSummary: func(_ context.Context, req *providersv1.GetEarningsSummaryRequest) (*providersv1.GetEarningsSummaryResponse, error) {
				earningsCalled = true
				seenEarnPID = req.GetProviderId().GetValue()
				return &providersv1.GetEarningsSummaryResponse{Summary: &providersv1.EarningsSummary{}}, nil
			},
			listEvents: func(_ context.Context, req *providersv1.ListAuditEventsRequest) (*providersv1.ListAuditEventsResponse, error) {
				dashCalls++
				seenAuditPID = req.GetProviderId().GetValue()
				return &providersv1.ListAuditEventsResponse{
					Events: []*providersv1.AuditEvent{
						{Id: &commonv1.UUID{Value: uuid.NewString()}},
					},
				}, nil
			},
		},
		ProvidersScheduling: &mockScheduling{
			getState: func(_ context.Context, req *providersv1.GetCurrentStateRequest) (*providersv1.GetCurrentStateResponse, error) {
				stateCalled = true
				seenSchedPID = req.GetProviderId().GetValue()
				return &providersv1.GetCurrentStateResponse{State: providersv1.SchedulerState_SCHEDULER_STATE_ACTIVE}, nil
			},
		},
		ProvidersRegistration: staticRegistration(owned),
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/dashboard", nil))
	w := httptest.NewRecorder()
	api.GetProviderDashboard(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !earningsCalled || !stateCalled || dashCalls != 1 {
		t.Fatalf("fan-out incomplete earnings=%v state=%v events=%d", earningsCalled, stateCalled, dashCalls)
	}
	// Every fan-out leg MUST receive the actual paired provider_id (not
	// the caller's user_id). This is the regression contract for #305.
	if seenSchedPID != pid || seenEarnPID != pid || seenAuditPID != pid {
		t.Fatalf("provider_id not threaded correctly: sched=%s earn=%s audit=%s want=%s", seenSchedPID, seenEarnPID, seenAuditPID, pid)
	}
	if seenSchedPID == fakeUserID {
		t.Fatal("provider_id was leaked as the caller's user_id (#305 regression)")
	}
}

// TestGetProviderSchedule_NoProvider_ReturnsNullConfig is the direct
// regression test for #305: a user with zero paired providers must NOT
// see a synthesised SchedulingConfig keyed by their user_id. They must
// see {"config": null, "has_provider": false} so the frontend can render
// the "Install the daemon" empty state.
func TestGetProviderSchedule_NoProvider_ReturnsNullConfig(t *testing.T) {
	schedCalled := false
	set := &clients.Set{
		ProvidersScheduling: &mockScheduling{
			getConfig: func(_ context.Context, _ *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error) {
				schedCalled = true
				return &providersv1.GetSchedulingConfigResponse{}, nil
			},
		},
		ProvidersRegistration: staticRegistration(), // ← zero providers owned
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/schedule", nil))
	w := httptest.NewRecorder()
	api.GetProviderSchedule(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if schedCalled {
		t.Fatal("GetSchedulingConfig must NOT be called when caller owns no provider (#305)")
	}
	var got providerSchedule
	mustReadJSON(t, w.Body, &got)
	if got.Config != nil {
		t.Fatalf("config should be nil when no provider paired, got %#v", got.Config)
	}
	if got.HasProvider {
		t.Fatal("has_provider should be false when caller owns nothing")
	}
}

// TestGetProviderSchedule_OwnedProvider_ReturnsRealProviderID asserts the
// happy path: when the caller owns a paired provider, the schedule
// upstream is called with THAT provider's id (not the caller's user_id).
func TestGetProviderSchedule_OwnedProvider_ReturnsRealProviderID(t *testing.T) {
	pid := uuid.NewString()
	var seenPID string
	set := &clients.Set{
		ProvidersScheduling: &mockScheduling{
			getConfig: func(_ context.Context, req *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error) {
				seenPID = req.GetProviderId().GetValue()
				return &providersv1.GetSchedulingConfigResponse{
					Config: &providersv1.SchedulingConfig{
						ProviderId: &commonv1.UUID{Value: pid},
					},
				}, nil
			},
		},
		ProvidersRegistration: staticRegistration(&providersv1.Provider{
			Id:          &commonv1.UUID{Value: pid},
			OwnerUserId: &commonv1.UUID{Value: fakeUserID},
			Status:      providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE,
		}),
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/schedule", nil))
	w := httptest.NewRecorder()
	api.GetProviderSchedule(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if seenPID != pid {
		t.Fatalf("providers-svc got provider_id=%s, want %s", seenPID, pid)
	}
	if seenPID == fakeUserID {
		t.Fatal("provider_id was leaked as caller's user_id (#305 regression)")
	}
	var got providerSchedule
	mustReadJSON(t, w.Body, &got)
	if !got.HasProvider {
		t.Fatal("has_provider should be true when caller owns a provider")
	}
	if got.Config.GetProviderId().GetValue() != pid {
		t.Fatalf("returned config.provider_id=%s, want %s", got.Config.GetProviderId().GetValue(), pid)
	}
}

// TestGetProviderSchedule_QueryParam_NotOwned_403 ensures a caller can't
// pivot to read someone else's SchedulingConfig by passing ?provider_id=
// in the URL.
func TestGetProviderSchedule_QueryParam_NotOwned_403(t *testing.T) {
	otherPID := uuid.NewString() // owned by a different user
	myPID := uuid.NewString()
	set := &clients.Set{
		ProvidersScheduling: &mockScheduling{
			getConfig: func(_ context.Context, _ *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error) {
				t.Fatal("upstream must not be called when caller does not own the requested provider_id")
				return nil, nil
			},
		},
		ProvidersRegistration: staticRegistration(&providersv1.Provider{
			Id:          &commonv1.UUID{Value: myPID},
			OwnerUserId: &commonv1.UUID{Value: fakeUserID},
			Status:      providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE,
		}),
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/schedule?provider_id="+otherPID, nil))
	w := httptest.NewRecorder()
	api.GetProviderSchedule(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestUpdateProviderSchedule_RoundTrip(t *testing.T) {
	called := false
	set := &clients.Set{
		ProvidersScheduling: &mockScheduling{
			updateConfig: func(_ context.Context, req *providersv1.UpdateSchedulingConfigRequest) (*providersv1.UpdateSchedulingConfigResponse, error) {
				called = true
				if req.Config == nil || req.Config.Caps == nil || req.Config.Caps.BandwidthCapGbPerMonth != 100 {
					t.Fatalf("unexpected config %#v", req.Config)
				}
				return &providersv1.UpdateSchedulingConfigResponse{Config: req.Config}, nil
			},
		},
	}
	api := newAPI(t, set)
	body, _ := json.Marshal(map[string]any{
		"config": map[string]any{
			"caps": map[string]int{"bandwidth_cap_gb_per_month": 100},
		},
	})
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/provide/schedule", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.UpdateProviderSchedule(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("UpdateSchedulingConfig not called")
	}
}

// --- customer ------------------------------------------------------------

func TestCreateAndListAPIKeys(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	wsID := uuid.NewString()
	body, _ := json.Marshal(map[string]string{"workspace_id": wsID, "label": "ci"})
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/customer/api-keys", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.CreateAPIKey(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	var k APIKey
	mustReadJSON(t, w.Body, &k)
	if k.Plaintext == "" || !strings.HasPrefix(k.Plaintext, "iog_") {
		t.Fatalf("plaintext missing or malformed: %q", k.Plaintext)
	}

	// list
	r2 := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/customer/api-keys?workspace_id="+wsID, nil))
	w2 := httptest.NewRecorder()
	api.ListAPIKeys(w2, r2)
	if w2.Code != http.StatusOK {
		t.Fatalf("list want 200, got %d", w2.Code)
	}
	var got struct {
		Keys []APIKey `json:"keys"`
	}
	mustReadJSON(t, w2.Body, &got)
	if len(got.Keys) != 1 || got.Keys[0].Plaintext != "" {
		t.Fatalf("list result wrong: %+v", got.Keys)
	}
}

func TestDeleteAPIKey_NotFound(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuid.NewString())
	r := withAuth(httptest.NewRequest(http.MethodDelete, "/api/v1/customer/api-keys/x?workspace_id="+uuid.NewString(), nil))
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	api.DeleteAPIKey(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestSubmitWorkload_StampsID(t *testing.T) {
	captured := ""
	set := &clients.Set{
		Workloads: &mockWorkloads{
			submit: func(_ context.Context, req *workloadsv1.SubmitWorkloadRequest) (*workloadsv1.SubmitWorkloadResponse, error) {
				if req.Workload == nil {
					t.Fatal("workload nil")
				}
				captured = req.Workload.Id.Value
				return &workloadsv1.SubmitWorkloadResponse{Workload: req.Workload}, nil
			},
		},
	}
	api := newAPI(t, set)
	body, _ := json.Marshal(map[string]any{
		"workload": map[string]any{
			"workspace_id": map[string]string{"value": uuid.NewString()},
			"type":         1, // WORKLOAD_TYPE_BANDWIDTH — proto enums are wire-numeric.
		},
	})
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/customer/workloads", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.SubmitWorkload(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d body=%s", w.Code, w.Body.String())
	}
	if _, err := uuid.Parse(captured); err != nil {
		t.Fatalf("workload id not stamped or invalid: %q", captured)
	}
}

func TestCustomerUsage(t *testing.T) {
	set := &clients.Set{
		Billing: &mockBilling{
			listUsage: func(_ context.Context, req *billingv1.ListUsageRequest) (*billingv1.ListUsageResponse, error) {
				if req.WorkspaceId == nil || req.WorkspaceId.Value == "" {
					t.Fatal("missing workspace id")
				}
				return &billingv1.ListUsageResponse{
					Usage: []*billingv1.UsageRecord{{Quantity: 42}},
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/customer/usage?workspace_id="+uuid.NewString(), nil))
	w := httptest.NewRecorder()
	api.GetCustomerUsage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

// --- admin ---------------------------------------------------------------

func TestListAbuseQueue(t *testing.T) {
	set := &clients.Set{
		Antiabuse: &mockAntiabuse{
			listFilters: func(_ context.Context, _ *abusev1.ListFiltersRequest) (*abusev1.ListFiltersResponse, error) {
				return &abusev1.ListFiltersResponse{
					Rules:       []*abusev1.FilterRule{{Id: "1", Slug: "csam"}},
					RulesetHash: "deadbeef",
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/admin/abuse-queue", nil), "ADMIN")
	w := httptest.NewRecorder()
	api.ListAbuseQueue(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "deadbeef") {
		t.Fatalf("missing hash: %s", w.Body.String())
	}
}

func TestResolveAbuseEvent_BadDecision(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", uuid.NewString())
	body, _ := json.Marshal(map[string]string{"decision": "wat"})
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/admin/abuse/x/resolve", bytes.NewReader(body)), "ADMIN")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	api.ResolveAbuseEvent(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}

// --- vpn -----------------------------------------------------------------

func TestVPNAccount_FreeTier(t *testing.T) {
	set := &clients.Set{
		Billing: &mockBilling{
			getSub: func(_ context.Context, _ *billingv1.GetSubscriptionRequest) (*billingv1.GetSubscriptionResponse, error) {
				return &billingv1.GetSubscriptionResponse{}, nil
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/vpn/account?workspace_id="+uuid.NewString(), nil))
	w := httptest.NewRecorder()
	api.GetVPNAccount(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "FREE") {
		t.Fatalf("expected FREE tier: %s", w.Body.String())
	}
}

func TestUpgradeVPN_InvokesStripe(t *testing.T) {
	set := &clients.Set{
		Billing: &mockBilling{
			createCheckout: func(_ context.Context, req *billingv1.CreateCheckoutSessionRequest) (*billingv1.CreateCheckoutSessionResponse, error) {
				if req.DesiredTier != billingv1.SubscriptionTier_SUBSCRIPTION_TIER_GROWTH {
					t.Fatalf("unexpected tier %v", req.DesiredTier)
				}
				return &billingv1.CreateCheckoutSessionResponse{CheckoutUrl: "https://checkout.stripe.com/c/sess_123"}, nil
			},
		},
	}
	api := newAPI(t, set)
	body, _ := json.Marshal(map[string]string{
		"workspace_id": uuid.NewString(),
		"tier":         "growth",
		"success_url":  "https://app.iogrid.org/ok",
		"cancel_url":   "https://app.iogrid.org/cancel",
	})
	r := withAuth(httptest.NewRequest(http.MethodPost, "/api/v1/vpn/upgrade", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.UpgradeVPN(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "checkout.stripe.com") {
		t.Fatalf("missing checkout url: %s", w.Body.String())
	}
}

// --- error mapping -------------------------------------------------------

func TestUpstreamErrorMapping_NotFound(t *testing.T) {
	set := &clients.Set{
		Identity: &mockIdentity{
			getUser: func(context.Context, *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error) {
				return nil, errors.New("simulated")
			},
		},
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/me", nil))
	w := httptest.NewRecorder()
	api.GetMe(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 for non-connect error, got %d", w.Code)
	}
}
