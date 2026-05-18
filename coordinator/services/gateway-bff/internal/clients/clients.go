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

// --- bundle ---------------------------------------------------------------

// Set bundles every per-service client; one Set per gateway-bff process.
type Set struct {
	Identity            IdentityClient
	Auth                AuthClient
	ProvidersDashboard  ProvidersDashboardClient
	ProvidersScheduling ProvidersSchedulingClient
	Workloads           WorkloadsClient
	Antiabuse           AntiabuseClient
	Billing             BillingClient
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
}

// New constructs a real, network-backed Set. httpClient is reused
// across all Connect clients so connection pooling is shared.
func New(cfg Config, httpClient *http.Client) *Set {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: cfg.Timeout}
	}
	// Connect-Go retries on the wire transport via its policy options.
	// For simplicity we wrap retries at the call site (see Retry below).
	identityRaw := identityv1connect.NewIdentityServiceClient(httpClient, cfg.IdentityURL)
	authRaw := identityv1connect.NewAuthServiceClient(httpClient, cfg.IdentityURL)
	dashRaw := providersv1connect.NewDashboardServiceClient(httpClient, cfg.ProvidersURL)
	schedRaw := providersv1connect.NewSchedulingServiceClient(httpClient, cfg.ProvidersURL)
	wlRaw := workloadsv1connect.NewWorkloadSubmissionServiceClient(httpClient, cfg.WorkloadsURL)
	abuseRaw := antiabusev1connect.NewAbuseFilterServiceClient(httpClient, cfg.AntiabuseURL)
	billRaw := billingv1connect.NewSubscriptionServiceClient(httpClient, cfg.BillingURL)

	return &Set{
		Identity:            &identityAdapter{c: identityRaw, retries: cfg.Retries},
		Auth:                &authAdapter{c: authRaw, retries: cfg.Retries},
		ProvidersDashboard:  &dashAdapter{c: dashRaw, retries: cfg.Retries},
		ProvidersScheduling: &schedAdapter{c: schedRaw, retries: cfg.Retries},
		Workloads:           &workloadsAdapter{c: wlRaw, retries: cfg.Retries},
		Antiabuse:           &antiabuseAdapter{c: abuseRaw, retries: cfg.Retries},
		Billing:             &billingAdapter{c: billRaw, retries: cfg.Retries},
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
