// Package clients bundles the typed Connect-Go clients gateway-bff
// uses to talk to every other coordinator microservice.
//
// We intentionally narrow each generated client to a small interface so
// the handler tests can pass mocks without spinning up an HTTP server.
// The constructor (New) wires real Connect clients at process boot;
// tests use the per-service interfaces directly.
//
// Retry policy: at this layer we wrap each call with a small
// fixed-budget retry on transient (Unavailable, DeadlineExceeded)
// errors. Anything else propagates as-is. We deliberately do NOT layer
// a circuit-breaker here: the upstream Connect transport handles
// connection pooling, and the chi middleware enforces a global timeout.
package clients

import (
	"context"
	"net/http"
	"time"

	"connectrpc.com/connect"

	abusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1/antiabusev1connect"
	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1/billingv1connect"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1/identityv1connect"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1/providersv1connect"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1/workloadsv1connect"
)

// --- per-service interfaces ----------------------------------------------

// IdentityClient is the subset of identity-svc the BFF calls.
type IdentityClient interface {
	GetUser(ctx context.Context, req *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error)
	UpdateUser(ctx context.Context, req *identityv1.UpdateUserRequest) (*identityv1.UpdateUserResponse, error)
	RemoveIdentifier(ctx context.Context, req *identityv1.RemoveIdentifierRequest) (*identityv1.RemoveIdentifierResponse, error)
	DeleteAccount(ctx context.Context, req *identityv1.DeleteAccountRequest) (*identityv1.DeleteAccountResponse, error)
}

// AuthClient wraps the sign-in / session lifecycle calls.
type AuthClient interface {
	StartGoogleSignIn(ctx context.Context, req *identityv1.StartGoogleSignInRequest) (*identityv1.StartGoogleSignInResponse, error)
	CompleteGoogleSignIn(ctx context.Context, req *identityv1.CompleteGoogleSignInRequest) (*identityv1.CompleteGoogleSignInResponse, error)
	RequestMagicLink(ctx context.Context, req *identityv1.RequestMagicLinkRequest) (*identityv1.RequestMagicLinkResponse, error)
	CompleteMagicLink(ctx context.Context, req *identityv1.CompleteMagicLinkRequest) (*identityv1.CompleteMagicLinkResponse, error)
	RefreshToken(ctx context.Context, req *identityv1.RefreshTokenRequest) (*identityv1.RefreshTokenResponse, error)
	SignOut(ctx context.Context, req *identityv1.SignOutRequest) (*identityv1.SignOutResponse, error)
	ListSessions(ctx context.Context, req *identityv1.ListSessionsRequest) (*identityv1.ListSessionsResponse, error)
	// RevokeSession soft-revokes a session id owned by the caller
	// (issue #322). Identity-svc validates ownership in the WHERE
	// clause of the UPDATE; the BFF only forwards.
	RevokeSession(ctx context.Context, req *identityv1.RevokeSessionRequest) (*identityv1.RevokeSessionResponse, error)
	// SIWS wallet binding (issue #326). The /account/wallets surface
	// backs the $GRID payout promise — every provider must bind a
	// Solana wallet before billing-svc can route a $GRID payout.
	StartSiwsBinding(ctx context.Context, req *identityv1.StartSiwsBindingRequest) (*identityv1.StartSiwsBindingResponse, error)
	CompleteSiwsBinding(ctx context.Context, req *identityv1.CompleteSiwsBindingRequest) (*identityv1.CompleteSiwsBindingResponse, error)
	ListBoundWallets(ctx context.Context, req *identityv1.ListBoundWalletsRequest) (*identityv1.ListBoundWalletsResponse, error)
	UnbindWallet(ctx context.Context, req *identityv1.UnbindWalletRequest) (*identityv1.UnbindWalletResponse, error)
}

// ProvidersDashboardClient backs the /provide/dashboard + audit feed.
type ProvidersDashboardClient interface {
	ListAuditEvents(ctx context.Context, req *providersv1.ListAuditEventsRequest) (*providersv1.ListAuditEventsResponse, error)
	StreamAuditEvents(ctx context.Context, req *providersv1.StreamAuditEventsRequest) (AuditEventStream, error)
	GetEarningsSummary(ctx context.Context, req *providersv1.GetEarningsSummaryRequest) (*providersv1.GetEarningsSummaryResponse, error)
}

// AuditEventStream abstracts the connect server-stream so tests can
// substitute an in-memory channel-backed implementation.
type AuditEventStream interface {
	Receive() bool
	Msg() *providersv1.AuditEvent
	Err() error
	Close() error
}

// ProvidersSchedulingClient backs /provide/schedule.
type ProvidersSchedulingClient interface {
	GetSchedulingConfig(ctx context.Context, req *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error)
	UpdateSchedulingConfig(ctx context.Context, req *providersv1.UpdateSchedulingConfigRequest) (*providersv1.UpdateSchedulingConfigResponse, error)
	GetCurrentState(ctx context.Context, req *providersv1.GetCurrentStateRequest) (*providersv1.GetCurrentStateResponse, error)
}

// ProvidersRegistrationClient is the read-side of the
// ProviderRegistrationService the BFF needs so /provide/* can gate
// responses on actual ownership. Without it, /provide/schedule was
// synthesising a default config keyed by the caller's user_id even when
// the caller had zero paired providers (#305).
type ProvidersRegistrationClient interface {
	ListProviders(ctx context.Context, req *providersv1.ListProvidersRequest) (*providersv1.ListProvidersResponse, error)
}

// WorkloadsClient backs /customer/workloads and the per-workload stream.
type WorkloadsClient interface {
	SubmitWorkload(ctx context.Context, req *workloadsv1.SubmitWorkloadRequest) (*workloadsv1.SubmitWorkloadResponse, error)
	GetWorkload(ctx context.Context, req *workloadsv1.GetWorkloadRequest) (*workloadsv1.GetWorkloadResponse, error)
	ListWorkloads(ctx context.Context, req *workloadsv1.ListWorkloadsRequest) (*workloadsv1.ListWorkloadsResponse, error)
	CancelWorkload(ctx context.Context, req *workloadsv1.CancelWorkloadRequest) (*workloadsv1.CancelWorkloadResponse, error)
	StreamWorkloadEvents(ctx context.Context, req *workloadsv1.StreamWorkloadEventsRequest) (WorkloadEventStream, error)
}

// WorkloadEventStream is the abstracted server stream for workload events.
type WorkloadEventStream interface {
	Receive() bool
	Msg() *workloadsv1.WorkloadEvent
	Err() error
	Close() error
}

// AntiabuseClient backs /admin/abuse-queue.
type AntiabuseClient interface {
	ListFilters(ctx context.Context, req *abusev1.ListFiltersRequest) (*abusev1.ListFiltersResponse, error)
}

// BillingClient backs /customer/usage and Stripe checkout.
type BillingClient interface {
	GetSubscription(ctx context.Context, req *billingv1.GetSubscriptionRequest) (*billingv1.GetSubscriptionResponse, error)
	ListUsage(ctx context.Context, req *billingv1.ListUsageRequest) (*billingv1.ListUsageResponse, error)
	CreateCheckoutSession(ctx context.Context, req *billingv1.CreateCheckoutSessionRequest) (*billingv1.CreateCheckoutSessionResponse, error)
}

// BillingEarningsClient backs the /provide/earnings headline-card surface
// (#324). The three RPCs are owned by billing-svc (NOT providers-svc):
// totals + payout-method election sit on the money side of the bounded-
// context line. gateway-bff fans
//
//	GET /api/v1/provide/earnings/summary → GetEarningsSummary
//	GET /api/v1/provide/payout-method    → GetPayoutMethod
//	PUT /api/v1/provide/payout-method    → SetPayoutMethod
type BillingEarningsClient interface {
	GetEarningsSummary(ctx context.Context, req *billingv1.GetEarningsSummaryRequest) (*billingv1.GetEarningsSummaryResponse, error)
	GetPayoutMethod(ctx context.Context, req *billingv1.GetPayoutMethodRequest) (*billingv1.GetPayoutMethodResponse, error)
	SetPayoutMethod(ctx context.Context, req *billingv1.SetPayoutMethodRequest) (*billingv1.SetPayoutMethodResponse, error)
}

// WorkspaceClient bundles the WorkspaceService RPCs proxied by
// /api/v1/workspaces. Defined here so the wiring stays symmetric with
// the other per-service interfaces; the handler-package mirror
// (handlers.WorkspaceClient) imports back via interface embedding.
type WorkspaceClient interface {
	CreateWorkspace(ctx context.Context, req *identityv1.CreateWorkspaceRequest) (*identityv1.CreateWorkspaceResponse, error)
	GetWorkspace(ctx context.Context, req *identityv1.GetWorkspaceRequest) (*identityv1.GetWorkspaceResponse, error)
	ListWorkspaces(ctx context.Context, req *identityv1.ListWorkspacesRequest) (*identityv1.ListWorkspacesResponse, error)
	UpdateWorkspace(ctx context.Context, req *identityv1.UpdateWorkspaceRequest) (*identityv1.UpdateWorkspaceResponse, error)
	DeleteWorkspace(ctx context.Context, req *identityv1.DeleteWorkspaceRequest) (*identityv1.DeleteWorkspaceResponse, error)
	AddMember(ctx context.Context, req *identityv1.AddMemberRequest) (*identityv1.AddMemberResponse, error)
	RemoveMember(ctx context.Context, req *identityv1.RemoveMemberRequest) (*identityv1.RemoveMemberResponse, error)
	ListMembers(ctx context.Context, req *identityv1.ListMembersRequest) (*identityv1.ListMembersResponse, error)
	UpdateMemberRole(ctx context.Context, req *identityv1.UpdateMemberRoleRequest) (*identityv1.UpdateMemberRoleResponse, error)
}

// --- bundle ---------------------------------------------------------------

// Set bundles every per-service client; one Set per gateway-bff process.
type Set struct {
	Identity              IdentityClient
	Auth                  AuthClient
	ProvidersDashboard    ProvidersDashboardClient
	ProvidersScheduling   ProvidersSchedulingClient
	ProvidersRegistration ProvidersRegistrationClient
	Workloads             WorkloadsClient
	Antiabuse             AntiabuseClient
	Billing               BillingClient
	BillingEarnings       BillingEarningsClient
	Workspaces            WorkspaceClient
}

// Config bundles the downstream URLs + per-call timeout. Comes from
// internal/config; kept narrow so tests can construct a Set without
// caring about every env var.
type Config struct {
	IdentityURL  string
	ProvidersURL string
	WorkloadsURL string
	AntiabuseURL string
	BillingURL   string
	Timeout      time.Duration
	Retries      int
	// ServiceToken is the shared secret identity-svc's Phase 0 shim
	// accepts on Authorization: Bearer <token> alongside the
	// X-Iogrid-User-Id + X-Iogrid-Session-Id headers (issue #322 +
	// #232). When set, every Connect-RPC client wires a header-
	// forwarding interceptor that stamps the caller's identity onto
	// outbound calls. When empty, no header is set and downstream
	// services see anonymous calls — useful for tests / dev where
	// the shim isn't configured.
	ServiceToken string
}

// New constructs a real, network-backed Set. httpClient is reused
// across all Connect clients so connection pooling is shared.
func New(cfg Config, httpClient *http.Client) *Set {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	// Single header-forwarding interceptor shared across every client.
	// Connect's WithInterceptors applies them in order; we keep just
	// the one so call ordering can't surprise reviewers later.
	interceptors := connect.WithInterceptors(newHeaderForwarder(cfg.ServiceToken))
	// Connect-Go retries on the wire transport via its policy options.
	// For simplicity we wrap retries at the call site (see Retry below).
	identityRaw := identityv1connect.NewIdentityServiceClient(httpClient, cfg.IdentityURL, interceptors)
	authRaw := identityv1connect.NewAuthServiceClient(httpClient, cfg.IdentityURL, interceptors)
	workspaceRaw := identityv1connect.NewWorkspaceServiceClient(httpClient, cfg.IdentityURL, interceptors)
	dashRaw := providersv1connect.NewDashboardServiceClient(httpClient, cfg.ProvidersURL, interceptors)
	schedRaw := providersv1connect.NewSchedulingServiceClient(httpClient, cfg.ProvidersURL, interceptors)
	regRaw := providersv1connect.NewProviderRegistrationServiceClient(httpClient, cfg.ProvidersURL, interceptors)
	wlRaw := workloadsv1connect.NewWorkloadSubmissionServiceClient(httpClient, cfg.WorkloadsURL, interceptors)
	abuseRaw := antiabusev1connect.NewAbuseFilterServiceClient(httpClient, cfg.AntiabuseURL, interceptors)
	billRaw := billingv1connect.NewSubscriptionServiceClient(httpClient, cfg.BillingURL, interceptors)
	earnRaw := billingv1connect.NewEarningsServiceClient(httpClient, cfg.BillingURL, interceptors)

	return &Set{
		Identity:              &identityAdapter{c: identityRaw, retries: cfg.Retries},
		Auth:                  &authAdapter{c: authRaw, retries: cfg.Retries},
		ProvidersDashboard:    &dashAdapter{c: dashRaw, retries: cfg.Retries},
		ProvidersScheduling:   &schedAdapter{c: schedRaw, retries: cfg.Retries},
		ProvidersRegistration: &regAdapter{c: regRaw, retries: cfg.Retries},
		Workloads:             &workloadsAdapter{c: wlRaw, retries: cfg.Retries},
		Antiabuse:             &antiabuseAdapter{c: abuseRaw, retries: cfg.Retries},
		Billing:               &billingAdapter{c: billRaw, retries: cfg.Retries},
		BillingEarnings:       &billingEarningsAdapter{c: earnRaw, retries: cfg.Retries},
		Workspaces:            &workspaceAdapter{c: workspaceRaw, retries: cfg.Retries},
	}
}

// --- per-service adapters -------------------------------------------------
//
// Each adapter unwraps connect.Response[*T] into the raw protobuf
// message so handlers don't need to know about the connect envelope.
// They also apply the retry policy.

type identityAdapter struct {
	c       identityv1connect.IdentityServiceClient
	retries int
}

func (a *identityAdapter) GetUser(ctx context.Context, req *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.GetUserResponse, error) {
		r, err := a.c.GetUser(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *identityAdapter) UpdateUser(ctx context.Context, req *identityv1.UpdateUserRequest) (*identityv1.UpdateUserResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.UpdateUserResponse, error) {
		r, err := a.c.UpdateUser(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *identityAdapter) RemoveIdentifier(ctx context.Context, req *identityv1.RemoveIdentifierRequest) (*identityv1.RemoveIdentifierResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.RemoveIdentifierResponse, error) {
		r, err := a.c.RemoveIdentifier(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *identityAdapter) DeleteAccount(ctx context.Context, req *identityv1.DeleteAccountRequest) (*identityv1.DeleteAccountResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.DeleteAccountResponse, error) {
		r, err := a.c.DeleteAccount(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

type authAdapter struct {
	c       identityv1connect.AuthServiceClient
	retries int
}

func (a *authAdapter) StartGoogleSignIn(ctx context.Context, req *identityv1.StartGoogleSignInRequest) (*identityv1.StartGoogleSignInResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.StartGoogleSignInResponse, error) {
		r, err := a.c.StartGoogleSignIn(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *authAdapter) CompleteGoogleSignIn(ctx context.Context, req *identityv1.CompleteGoogleSignInRequest) (*identityv1.CompleteGoogleSignInResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.CompleteGoogleSignInResponse, error) {
		r, err := a.c.CompleteGoogleSignIn(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *authAdapter) RequestMagicLink(ctx context.Context, req *identityv1.RequestMagicLinkRequest) (*identityv1.RequestMagicLinkResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.RequestMagicLinkResponse, error) {
		r, err := a.c.RequestMagicLink(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *authAdapter) CompleteMagicLink(ctx context.Context, req *identityv1.CompleteMagicLinkRequest) (*identityv1.CompleteMagicLinkResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.CompleteMagicLinkResponse, error) {
		r, err := a.c.CompleteMagicLink(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *authAdapter) RefreshToken(ctx context.Context, req *identityv1.RefreshTokenRequest) (*identityv1.RefreshTokenResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.RefreshTokenResponse, error) {
		r, err := a.c.RefreshToken(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *authAdapter) SignOut(ctx context.Context, req *identityv1.SignOutRequest) (*identityv1.SignOutResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.SignOutResponse, error) {
		r, err := a.c.SignOut(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *authAdapter) ListSessions(ctx context.Context, req *identityv1.ListSessionsRequest) (*identityv1.ListSessionsResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.ListSessionsResponse, error) {
		r, err := a.c.ListSessions(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *authAdapter) RevokeSession(ctx context.Context, req *identityv1.RevokeSessionRequest) (*identityv1.RevokeSessionResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.RevokeSessionResponse, error) {
		r, err := a.c.RevokeSession(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

// SIWS wallet RPCs. StartSiwsBinding is idempotent (the second Start
// overwrites the prior nonce in Redis) so it's safe to retry on
// transient transport failures. CompleteSiwsBinding is NOT — the
// challenge GETDELs out of Redis on first call, so a retry would hit
// CodeFailedPrecondition. We deliberately skip the retry wrapper there.
func (a *authAdapter) StartSiwsBinding(ctx context.Context, req *identityv1.StartSiwsBindingRequest) (*identityv1.StartSiwsBindingResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.StartSiwsBindingResponse, error) {
		r, err := a.c.StartSiwsBinding(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *authAdapter) CompleteSiwsBinding(ctx context.Context, req *identityv1.CompleteSiwsBindingRequest) (*identityv1.CompleteSiwsBindingResponse, error) {
	r, err := a.c.CompleteSiwsBinding(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

func (a *authAdapter) ListBoundWallets(ctx context.Context, req *identityv1.ListBoundWalletsRequest) (*identityv1.ListBoundWalletsResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.ListBoundWalletsResponse, error) {
		r, err := a.c.ListBoundWallets(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

// UnbindWallet is idempotent at the DB layer (DELETE WHERE ... matches
// zero rows on second call → ErrNotFound → 404), so a retry on
// Unavailable / DeadlineExceeded is safe.
func (a *authAdapter) UnbindWallet(ctx context.Context, req *identityv1.UnbindWalletRequest) (*identityv1.UnbindWalletResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.UnbindWalletResponse, error) {
		r, err := a.c.UnbindWallet(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

type dashAdapter struct {
	c       providersv1connect.DashboardServiceClient
	retries int
}

func (a *dashAdapter) ListAuditEvents(ctx context.Context, req *providersv1.ListAuditEventsRequest) (*providersv1.ListAuditEventsResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*providersv1.ListAuditEventsResponse, error) {
		r, err := a.c.ListAuditEvents(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *dashAdapter) GetEarningsSummary(ctx context.Context, req *providersv1.GetEarningsSummaryRequest) (*providersv1.GetEarningsSummaryResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*providersv1.GetEarningsSummaryResponse, error) {
		r, err := a.c.GetEarningsSummary(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

// connectAuditStream wraps the generated server-stream so it satisfies
// the AuditEventStream interface without exposing connect types.
type connectAuditStream struct {
	s *connect.ServerStreamForClient[providersv1.AuditEvent]
}

func (s *connectAuditStream) Receive() bool                { return s.s.Receive() }
func (s *connectAuditStream) Msg() *providersv1.AuditEvent { return s.s.Msg() }
func (s *connectAuditStream) Err() error                   { return s.s.Err() }
func (s *connectAuditStream) Close() error                 { return s.s.Close() }

func (a *dashAdapter) StreamAuditEvents(ctx context.Context, req *providersv1.StreamAuditEventsRequest) (AuditEventStream, error) {
	s, err := a.c.StreamAuditEvents(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return &connectAuditStream{s: s}, nil
}

type schedAdapter struct {
	c       providersv1connect.SchedulingServiceClient
	retries int
}

func (a *schedAdapter) GetSchedulingConfig(ctx context.Context, req *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*providersv1.GetSchedulingConfigResponse, error) {
		r, err := a.c.GetSchedulingConfig(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *schedAdapter) UpdateSchedulingConfig(ctx context.Context, req *providersv1.UpdateSchedulingConfigRequest) (*providersv1.UpdateSchedulingConfigResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*providersv1.UpdateSchedulingConfigResponse, error) {
		r, err := a.c.UpdateSchedulingConfig(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *schedAdapter) GetCurrentState(ctx context.Context, req *providersv1.GetCurrentStateRequest) (*providersv1.GetCurrentStateResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*providersv1.GetCurrentStateResponse, error) {
		r, err := a.c.GetCurrentState(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

// regAdapter wraps the ProviderRegistrationService client. Only the
// read-side (ListProviders) is exposed to the BFF; mutating RPCs go
// through dedicated onboarding handlers.
type regAdapter struct {
	c       providersv1connect.ProviderRegistrationServiceClient
	retries int
}

func (a *regAdapter) ListProviders(ctx context.Context, req *providersv1.ListProvidersRequest) (*providersv1.ListProvidersResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*providersv1.ListProvidersResponse, error) {
		r, err := a.c.ListProviders(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

type workloadsAdapter struct {
	c       workloadsv1connect.WorkloadSubmissionServiceClient
	retries int
}

func (a *workloadsAdapter) SubmitWorkload(ctx context.Context, req *workloadsv1.SubmitWorkloadRequest) (*workloadsv1.SubmitWorkloadResponse, error) {
	// SubmitWorkload is non-idempotent (creates DB rows). We do NOT
	// retry it server-side; retry semantics are the customer's
	// responsibility.
	r, err := a.c.SubmitWorkload(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

func (a *workloadsAdapter) GetWorkload(ctx context.Context, req *workloadsv1.GetWorkloadRequest) (*workloadsv1.GetWorkloadResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*workloadsv1.GetWorkloadResponse, error) {
		r, err := a.c.GetWorkload(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *workloadsAdapter) ListWorkloads(ctx context.Context, req *workloadsv1.ListWorkloadsRequest) (*workloadsv1.ListWorkloadsResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*workloadsv1.ListWorkloadsResponse, error) {
		r, err := a.c.ListWorkloads(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *workloadsAdapter) CancelWorkload(ctx context.Context, req *workloadsv1.CancelWorkloadRequest) (*workloadsv1.CancelWorkloadResponse, error) {
	r, err := a.c.CancelWorkload(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

type connectWorkloadStream struct {
	s *connect.ServerStreamForClient[workloadsv1.WorkloadEvent]
}

func (s *connectWorkloadStream) Receive() bool                    { return s.s.Receive() }
func (s *connectWorkloadStream) Msg() *workloadsv1.WorkloadEvent  { return s.s.Msg() }
func (s *connectWorkloadStream) Err() error                       { return s.s.Err() }
func (s *connectWorkloadStream) Close() error                     { return s.s.Close() }

func (a *workloadsAdapter) StreamWorkloadEvents(ctx context.Context, req *workloadsv1.StreamWorkloadEventsRequest) (WorkloadEventStream, error) {
	s, err := a.c.StreamWorkloadEvents(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return &connectWorkloadStream{s: s}, nil
}

type antiabuseAdapter struct {
	c       antiabusev1connect.AbuseFilterServiceClient
	retries int
}

func (a *antiabuseAdapter) ListFilters(ctx context.Context, req *abusev1.ListFiltersRequest) (*abusev1.ListFiltersResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*abusev1.ListFiltersResponse, error) {
		r, err := a.c.ListFilters(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

type billingAdapter struct {
	c       billingv1connect.SubscriptionServiceClient
	retries int
}

func (a *billingAdapter) GetSubscription(ctx context.Context, req *billingv1.GetSubscriptionRequest) (*billingv1.GetSubscriptionResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*billingv1.GetSubscriptionResponse, error) {
		r, err := a.c.GetSubscription(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *billingAdapter) ListUsage(ctx context.Context, req *billingv1.ListUsageRequest) (*billingv1.ListUsageResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*billingv1.ListUsageResponse, error) {
		r, err := a.c.ListUsage(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *billingAdapter) CreateCheckoutSession(ctx context.Context, req *billingv1.CreateCheckoutSessionRequest) (*billingv1.CreateCheckoutSessionResponse, error) {
	r, err := a.c.CreateCheckoutSession(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

// --- billing earnings adapter ---------------------------------------------

type billingEarningsAdapter struct {
	c       billingv1connect.EarningsServiceClient
	retries int
}

func (a *billingEarningsAdapter) GetEarningsSummary(ctx context.Context, req *billingv1.GetEarningsSummaryRequest) (*billingv1.GetEarningsSummaryResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*billingv1.GetEarningsSummaryResponse, error) {
		r, err := a.c.GetEarningsSummary(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *billingEarningsAdapter) GetPayoutMethod(ctx context.Context, req *billingv1.GetPayoutMethodRequest) (*billingv1.GetPayoutMethodResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*billingv1.GetPayoutMethodResponse, error) {
		r, err := a.c.GetPayoutMethod(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

// SetPayoutMethod is idempotent (last-write-wins on user_id) so it's
// safe to retry on transient transport failures.
func (a *billingEarningsAdapter) SetPayoutMethod(ctx context.Context, req *billingv1.SetPayoutMethodRequest) (*billingv1.SetPayoutMethodResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*billingv1.SetPayoutMethodResponse, error) {
		r, err := a.c.SetPayoutMethod(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

// --- workspace adapter ----------------------------------------------------

type workspaceAdapter struct {
	c       identityv1connect.WorkspaceServiceClient
	retries int
}

func (a *workspaceAdapter) CreateWorkspace(ctx context.Context, req *identityv1.CreateWorkspaceRequest) (*identityv1.CreateWorkspaceResponse, error) {
	r, err := a.c.CreateWorkspace(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

func (a *workspaceAdapter) GetWorkspace(ctx context.Context, req *identityv1.GetWorkspaceRequest) (*identityv1.GetWorkspaceResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.GetWorkspaceResponse, error) {
		r, err := a.c.GetWorkspace(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *workspaceAdapter) ListWorkspaces(ctx context.Context, req *identityv1.ListWorkspacesRequest) (*identityv1.ListWorkspacesResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.ListWorkspacesResponse, error) {
		r, err := a.c.ListWorkspaces(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *workspaceAdapter) UpdateWorkspace(ctx context.Context, req *identityv1.UpdateWorkspaceRequest) (*identityv1.UpdateWorkspaceResponse, error) {
	r, err := a.c.UpdateWorkspace(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

func (a *workspaceAdapter) DeleteWorkspace(ctx context.Context, req *identityv1.DeleteWorkspaceRequest) (*identityv1.DeleteWorkspaceResponse, error) {
	r, err := a.c.DeleteWorkspace(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

func (a *workspaceAdapter) AddMember(ctx context.Context, req *identityv1.AddMemberRequest) (*identityv1.AddMemberResponse, error) {
	r, err := a.c.AddMember(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

func (a *workspaceAdapter) RemoveMember(ctx context.Context, req *identityv1.RemoveMemberRequest) (*identityv1.RemoveMemberResponse, error) {
	r, err := a.c.RemoveMember(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

func (a *workspaceAdapter) ListMembers(ctx context.Context, req *identityv1.ListMembersRequest) (*identityv1.ListMembersResponse, error) {
	return retry(ctx, a.retries, func(ctx context.Context) (*identityv1.ListMembersResponse, error) {
		r, err := a.c.ListMembers(ctx, connect.NewRequest(req))
		if err != nil {
			return nil, err
		}
		return r.Msg, nil
	})
}

func (a *workspaceAdapter) UpdateMemberRole(ctx context.Context, req *identityv1.UpdateMemberRoleRequest) (*identityv1.UpdateMemberRoleResponse, error) {
	r, err := a.c.UpdateMemberRole(ctx, connect.NewRequest(req))
	if err != nil {
		return nil, err
	}
	return r.Msg, nil
}

// --- retry policy ---------------------------------------------------------

// retry executes fn up to (1 + attempts) times, sleeping briefly between
// attempts. Retries only on transient connect errors (Unavailable,
// DeadlineExceeded). Non-transient errors propagate immediately.
func retry[T any](ctx context.Context, attempts int, fn func(ctx context.Context) (*T, error)) (*T, error) {
	var lastErr error
	for i := 0; i <= attempts; i++ {
		v, err := fn(ctx)
		if err == nil {
			return v, nil
		}
		lastErr = err
		if !isTransient(err) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(50*(i+1)) * time.Millisecond):
		}
	}
	return nil, lastErr
}

// isTransient mirrors the gRPC retry-categorisation: only Unavailable
// and DeadlineExceeded are safe to re-issue without risking duplicate
// side effects on a non-idempotent RPC.
func isTransient(err error) bool {
	var cerr *connect.Error
	if !asConnectErr(err, &cerr) {
		return false
	}
	switch cerr.Code() {
	case connect.CodeUnavailable, connect.CodeDeadlineExceeded:
		return true
	default:
		return false
	}
}

// asConnectErr unwraps *connect.Error from a wrapped chain. We avoid
// pulling errors.As into the hot path via a tiny inline helper.
func asConnectErr(err error, out **connect.Error) bool {
	for err != nil {
		if c, ok := err.(*connect.Error); ok {
			*out = c
			return true
		}
		type unwrap interface{ Unwrap() error }
		u, ok := err.(unwrap)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
