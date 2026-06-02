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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"

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
	startGoogle         func(context.Context, *identityv1.StartGoogleSignInRequest) (*identityv1.StartGoogleSignInResponse, error)
	completeGoogle      func(context.Context, *identityv1.CompleteGoogleSignInRequest) (*identityv1.CompleteGoogleSignInResponse, error)
	requestMagic        func(context.Context, *identityv1.RequestMagicLinkRequest) (*identityv1.RequestMagicLinkResponse, error)
	completeMagic       func(context.Context, *identityv1.CompleteMagicLinkRequest) (*identityv1.CompleteMagicLinkResponse, error)
	refresh             func(context.Context, *identityv1.RefreshTokenRequest) (*identityv1.RefreshTokenResponse, error)
	signOut             func(context.Context, *identityv1.SignOutRequest) (*identityv1.SignOutResponse, error)
	listSessions        func(context.Context, *identityv1.ListSessionsRequest) (*identityv1.ListSessionsResponse, error)
	revokeSession       func(context.Context, *identityv1.RevokeSessionRequest) (*identityv1.RevokeSessionResponse, error)
	startSiwsBinding    func(context.Context, *identityv1.StartSiwsBindingRequest) (*identityv1.StartSiwsBindingResponse, error)
	completeSiwsBinding func(context.Context, *identityv1.CompleteSiwsBindingRequest) (*identityv1.CompleteSiwsBindingResponse, error)
	listBoundWallets    func(context.Context, *identityv1.ListBoundWalletsRequest) (*identityv1.ListBoundWalletsResponse, error)
	unbindWallet        func(context.Context, *identityv1.UnbindWalletRequest) (*identityv1.UnbindWalletResponse, error)
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
func (m *mockAuth) RevokeSession(ctx context.Context, req *identityv1.RevokeSessionRequest) (*identityv1.RevokeSessionResponse, error) {
	if m.revokeSession == nil {
		return &identityv1.RevokeSessionResponse{}, nil
	}
	return m.revokeSession(ctx, req)
}
func (m *mockAuth) StartSiwsBinding(ctx context.Context, req *identityv1.StartSiwsBindingRequest) (*identityv1.StartSiwsBindingResponse, error) {
	if m.startSiwsBinding == nil {
		return &identityv1.StartSiwsBindingResponse{}, nil
	}
	return m.startSiwsBinding(ctx, req)
}
func (m *mockAuth) CompleteSiwsBinding(ctx context.Context, req *identityv1.CompleteSiwsBindingRequest) (*identityv1.CompleteSiwsBindingResponse, error) {
	if m.completeSiwsBinding == nil {
		return &identityv1.CompleteSiwsBindingResponse{}, nil
	}
	return m.completeSiwsBinding(ctx, req)
}
func (m *mockAuth) ListBoundWallets(ctx context.Context, req *identityv1.ListBoundWalletsRequest) (*identityv1.ListBoundWalletsResponse, error) {
	if m.listBoundWallets == nil {
		return &identityv1.ListBoundWalletsResponse{}, nil
	}
	return m.listBoundWallets(ctx, req)
}
func (m *mockAuth) UnbindWallet(ctx context.Context, req *identityv1.UnbindWalletRequest) (*identityv1.UnbindWalletResponse, error) {
	if m.unbindWallet == nil {
		return &identityv1.UnbindWalletResponse{}, nil
	}
	return m.unbindWallet(ctx, req)
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
	listProviders      func(context.Context, *providersv1.ListProvidersRequest) (*providersv1.ListProvidersResponse, error)
	setPrimaryProvider func(context.Context, *providersv1.SetPrimaryProviderRequest) (*providersv1.SetPrimaryProviderResponse, error)
}

func (m *mockRegistration) ListProviders(ctx context.Context, req *providersv1.ListProvidersRequest) (*providersv1.ListProvidersResponse, error) {
	if m.listProviders == nil {
		return &providersv1.ListProvidersResponse{}, nil
	}
	return m.listProviders(ctx, req)
}

func (m *mockRegistration) SetPrimaryProvider(ctx context.Context, req *providersv1.SetPrimaryProviderRequest) (*providersv1.SetPrimaryProviderResponse, error) {
	if m.setPrimaryProvider == nil {
		return &providersv1.SetPrimaryProviderResponse{}, nil
	}
	return m.setPrimaryProvider(ctx, req)
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
	if s := string(got.Config); s != "null" {
		t.Fatalf("config should serialise as JSON null when no provider paired, got %q", s)
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
	// Config is now protojson-encoded raw JSON (#630); decode it back into
	// the proto message to assert on fields. This also exercises that the
	// emitted JSON is valid proto3-JSON (camelCase, parseable by protojson).
	gotCfg := &providersv1.SchedulingConfig{}
	if err := protojson.Unmarshal(got.Config, gotCfg); err != nil {
		t.Fatalf("config not valid proto3-JSON: %v (body=%s)", err, string(got.Config))
	}
	if gotCfg.GetProviderId().GetValue() != pid {
		t.Fatalf("returned config.provider_id=%s, want %s", gotCfg.GetProviderId().GetValue(), pid)
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

// TestUpdateProviderSchedule_CamelCasePayload is the #630 regression guard:
// the web protobuf-es client posts proto3-JSON camelCase ("bandwidthCapGbPerMonth"),
// which the old stdlib DisallowUnknownFields decode rejected with
// `json: unknown field "bandwidthCapGbPerMonth"`. protojson must accept it.
func TestUpdateProviderSchedule_CamelCasePayload(t *testing.T) {
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
			"caps": map[string]int{"bandwidthCapGbPerMonth": 100},
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
	// Regression for #630 bug family: the web reads `res.checkoutUrl`
	// (proto3-JSON camelCase). The handler MUST serialise via protojson —
	// a stdlib encoding/json round-trip would emit the snake_case
	// `checkout_url`, leaving res.checkoutUrl undefined and navigating the
	// upgrade button to `undefined`.
	var got struct {
		CheckoutURL string `json:"checkoutUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v (%s)", err, w.Body.String())
	}
	if got.CheckoutURL != "https://checkout.stripe.com/c/sess_123" {
		t.Fatalf("checkoutUrl not in proto3-JSON camelCase form: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "checkout_url") {
		t.Fatalf("response leaked snake_case checkout_url; web reads camelCase checkoutUrl: %s", w.Body.String())
	}
}

// --- /provide/audit/stream — KEEPALIVE filter (#323) ---------------------

// fakeAuditEventStream is a deterministic in-memory implementation of
// clients.AuditEventStream used by the SSE-proxy filter test. It walks
// a slice of *AuditEvent (or returns false when exhausted).
type fakeAuditEventStream struct {
	events []*providersv1.AuditEvent
	idx    int
	closed bool
}

func (s *fakeAuditEventStream) Receive() bool {
	if s.idx >= len(s.events) {
		return false
	}
	return true
}
func (s *fakeAuditEventStream) Msg() *providersv1.AuditEvent {
	ev := s.events[s.idx]
	s.idx++
	return ev
}
func (s *fakeAuditEventStream) Err() error   { return nil }
func (s *fakeAuditEventStream) Close() error { s.closed = true; return nil }

// TestStreamProviderAudit_FiltersKeepalive is the regression test for
// the BFF half of #323. KEEPALIVE frames flushed by providers-svc to
// unstick the Connect response headers MUST NOT be forwarded to the
// browser as SSE `event: audit_event` frames — the SSE handler ticks
// its own `:keep-alive` comment at the same cadence, so passing the
// proto KEEPALIVE through would render bogus rows in the transparency
// feed.
func TestStreamProviderAudit_FiltersKeepalive(t *testing.T) {
	pid := uuid.NewString()
	owned := &providersv1.Provider{
		Id:          &commonv1.UUID{Value: pid},
		OwnerUserId: &commonv1.UUID{Value: fakeUserID},
		Status:      providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE,
	}
	realEventID := uuid.NewString()
	stream := &fakeAuditEventStream{
		events: []*providersv1.AuditEvent{
			// 1) Initial KEEPALIVE that providers-svc emits to flush
			//    headers — MUST be dropped by the BFF.
			{
				ProviderId: &commonv1.UUID{Value: pid},
				Kind:       providersv1.EventKind_EVENT_KIND_KEEPALIVE,
			},
			// 2) A real workload-dispatched event — MUST pass through.
			{
				Id:         &commonv1.UUID{Value: realEventID},
				ProviderId: &commonv1.UUID{Value: pid},
				Kind:       providersv1.EventKind_EVENT_KIND_WORKLOAD_DISPATCHED,
			},
			// 3) Another KEEPALIVE — also dropped.
			{
				ProviderId: &commonv1.UUID{Value: pid},
				Kind:       providersv1.EventKind_EVENT_KIND_KEEPALIVE,
			},
		},
	}
	set := &clients.Set{
		ProvidersDashboard: &mockDashboard{
			streamEvents: func(_ context.Context, _ *providersv1.StreamAuditEventsRequest) (clients.AuditEventStream, error) {
				return stream, nil
			},
		},
		ProvidersRegistration: staticRegistration(owned),
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/audit/stream", nil))
	w := httptest.NewRecorder()
	api.StreamProviderAudit(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	// The real event MUST appear in the SSE body.
	if !strings.Contains(body, realEventID) {
		t.Fatalf("real audit event id %q missing from SSE body:\n%s", realEventID, body)
	}
	// EVENT_KIND_KEEPALIVE proto frames MUST NOT have been written as
	// SSE `event: audit_event` lines — but the canonical SSE
	// `:keep-alive` comment is fine. Scan for the proto enum string.
	if strings.Contains(body, "EVENT_KIND_KEEPALIVE") {
		t.Fatalf("KEEPALIVE proto frame leaked into SSE body:\n%s", body)
	}
	// Count audit_event lines — exactly one (the real event).
	if got := strings.Count(body, "event: audit_event"); got != 1 {
		t.Fatalf("expected exactly 1 audit_event SSE frame, got %d:\n%s", got, body)
	}
	if !stream.closed {
		t.Fatal("upstream stream not closed by handler")
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

// --- #325 — primary-provider selection -----------------------------------

// TestResolveOwnedProviderID_PrimaryWinsOverActiveNonPrimary is the
// direct regression receipt for #325: with two paired daemons, the
// primary-flagged row MUST be returned even if a non-primary row is
// ACTIVE and the primary is OFFLINE. Pre-fix the BFF picked "first
// ACTIVE", so Hatice's manual-test daemon (ACTIVE) shadowed her real
// Mac (primary, OFFLINE between heartbeats).
func TestResolveOwnedProviderID_PrimaryWinsOverActiveNonPrimary(t *testing.T) {
	primary := uuid.NewString()
	other := uuid.NewString()
	set := &clients.Set{
		ProvidersScheduling: &mockScheduling{
			getConfig: func(_ context.Context, req *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error) {
				if req.GetProviderId().GetValue() != primary {
					t.Fatalf("expected primary %s, got %s", primary, req.GetProviderId().GetValue())
				}
				return &providersv1.GetSchedulingConfigResponse{}, nil
			},
		},
		ProvidersRegistration: staticRegistration(
			// Non-primary, ACTIVE, smaller UUID ASC — the pre-#325
			// pick. The fix must NOT return this one.
			&providersv1.Provider{
				Id:          &commonv1.UUID{Value: other},
				OwnerUserId: &commonv1.UUID{Value: fakeUserID},
				Status:      providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE,
				IsPrimary:   false,
			},
			&providersv1.Provider{
				Id:          &commonv1.UUID{Value: primary},
				OwnerUserId: &commonv1.UUID{Value: fakeUserID},
				Status:      providersv1.ProviderStatus_PROVIDER_STATUS_OFFLINE,
				IsPrimary:   true,
			},
		),
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/schedule", nil))
	w := httptest.NewRecorder()
	api.GetProviderSchedule(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestResolveOwnedProviderID_ExplicitProviderIDOverridesPrimary asserts
// the ?provider_id= query param still wins (when caller actually owns
// it). Picker UI relies on this: selecting a non-primary daemon from
// the dropdown re-fetches with ?provider_id=X.
func TestResolveOwnedProviderID_ExplicitProviderIDOverridesPrimary(t *testing.T) {
	primary := uuid.NewString()
	picked := uuid.NewString()
	set := &clients.Set{
		ProvidersScheduling: &mockScheduling{
			getConfig: func(_ context.Context, req *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error) {
				if req.GetProviderId().GetValue() != picked {
					t.Fatalf("expected picked %s (query param), got %s", picked, req.GetProviderId().GetValue())
				}
				return &providersv1.GetSchedulingConfigResponse{}, nil
			},
		},
		ProvidersRegistration: staticRegistration(
			&providersv1.Provider{
				Id:          &commonv1.UUID{Value: primary},
				OwnerUserId: &commonv1.UUID{Value: fakeUserID},
				Status:      providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE,
				IsPrimary:   true,
			},
			&providersv1.Provider{
				Id:          &commonv1.UUID{Value: picked},
				OwnerUserId: &commonv1.UUID{Value: fakeUserID},
				Status:      providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE,
				IsPrimary:   false,
			},
		),
	}
	api := newAPI(t, set)
	r := withAuth(httptest.NewRequest(http.MethodGet, "/api/v1/provide/schedule?provider_id="+picked, nil))
	w := httptest.NewRecorder()
	api.GetProviderSchedule(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestSortOwnedProviders_Determinism unit-tests the per-owner sort.
// Pure function — order in MUST be irrelevant to order out.
func TestSortOwnedProviders_Determinism(t *testing.T) {
	now := time.Now().UTC()
	primary := uuid.NewString()
	freshActive := uuid.NewString()
	staleActive := uuid.NewString()
	offline := uuid.NewString()
	in := []*providersv1.Provider{
		{Id: &commonv1.UUID{Value: staleActive}, Status: providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE, LastSeenAt: timestamppb.New(now.Add(-time.Hour))},
		{Id: &commonv1.UUID{Value: offline}, Status: providersv1.ProviderStatus_PROVIDER_STATUS_OFFLINE, LastSeenAt: timestamppb.New(now)},
		{Id: &commonv1.UUID{Value: primary}, Status: providersv1.ProviderStatus_PROVIDER_STATUS_OFFLINE, LastSeenAt: timestamppb.New(now.Add(-2 * time.Hour)), IsPrimary: true},
		{Id: &commonv1.UUID{Value: freshActive}, Status: providersv1.ProviderStatus_PROVIDER_STATUS_ACTIVE, LastSeenAt: timestamppb.New(now)},
	}
	out := sortOwnedProviders(in)
	if out[0].GetId().GetValue() != primary {
		t.Fatalf("primary must be first, got %s", out[0].GetId().GetValue())
	}
	if out[1].GetId().GetValue() != freshActive {
		t.Fatalf("fresh ACTIVE non-primary must be second, got %s", out[1].GetId().GetValue())
	}
	if out[2].GetId().GetValue() != staleActive {
		t.Fatalf("stale ACTIVE must come before OFFLINE, got %s", out[2].GetId().GetValue())
	}
	if out[3].GetId().GetValue() != offline {
		t.Fatalf("OFFLINE non-primary must be last, got %s", out[3].GetId().GetValue())
	}
}

// TestSetPrimaryProvider_RoundTrip exercises the PUT handler end-to-end:
// validates the body parses, the upstream RPC is called with the
// authenticated owner_user_id, and the response is forwarded.
func TestSetPrimaryProvider_RoundTrip(t *testing.T) {
	pid := uuid.NewString()
	var seenOwner, seenPID string
	set := &clients.Set{
		ProvidersRegistration: &mockRegistration{
			setPrimaryProvider: func(_ context.Context, req *providersv1.SetPrimaryProviderRequest) (*providersv1.SetPrimaryProviderResponse, error) {
				seenOwner = req.GetOwnerUserId().GetValue()
				seenPID = req.GetProviderId().GetValue()
				return &providersv1.SetPrimaryProviderResponse{
					Provider: &providersv1.Provider{
						Id:        &commonv1.UUID{Value: pid},
						IsPrimary: true,
					},
				}, nil
			},
		},
	}
	api := newAPI(t, set)
	body, _ := json.Marshal(map[string]string{"provider_id": pid})
	r := withAuth(httptest.NewRequest(http.MethodPut, "/api/v1/provide/primary-provider", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.SetPrimaryProvider(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if seenOwner != fakeUserID {
		t.Fatalf("upstream owner_user_id = %s, want caller %s", seenOwner, fakeUserID)
	}
	if seenPID != pid {
		t.Fatalf("upstream provider_id = %s, want %s", seenPID, pid)
	}
}

// TestSetPrimaryProvider_RequiresProviderID rejects requests with an
// empty body or no provider_id.
func TestSetPrimaryProvider_RequiresProviderID(t *testing.T) {
	set := &clients.Set{
		ProvidersRegistration: &mockRegistration{
			setPrimaryProvider: func(context.Context, *providersv1.SetPrimaryProviderRequest) (*providersv1.SetPrimaryProviderResponse, error) {
				t.Fatal("upstream must not be called when provider_id is missing")
				return nil, nil
			},
		},
	}
	api := newAPI(t, set)
	body, _ := json.Marshal(map[string]string{})
	r := withAuth(httptest.NewRequest(http.MethodPut, "/api/v1/provide/primary-provider", bytes.NewReader(body)))
	w := httptest.NewRecorder()
	api.SetPrimaryProvider(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

// TestSetPrimaryProvider_RequiresAuth makes sure the route refuses
// unauthenticated callers — same gate as every other /provide/* path.
func TestSetPrimaryProvider_RequiresAuth(t *testing.T) {
	api := newAPI(t, &clients.Set{})
	body, _ := json.Marshal(map[string]string{"provider_id": uuid.NewString()})
	r := httptest.NewRequest(http.MethodPut, "/api/v1/provide/primary-provider", bytes.NewReader(body))
	w := httptest.NewRecorder()
	api.SetPrimaryProvider(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}
