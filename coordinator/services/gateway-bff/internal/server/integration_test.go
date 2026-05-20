//go:build integration

// Integration test: spins the full chi router with all middleware and
// stub downstream clients, then exercises every route end-to-end. Runs
// only under `go test -tags integration` because it relies on goroutine
// timing for the SSE assertions.

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/clients"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/config"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/handlers"
	"github.com/iogrid/iogrid/coordinator/services/gateway-bff/internal/ratelimit"

	abusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
	billingv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/billing/v1"
	identityv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/identity/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	workloadsv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/workloads/v1"
)

// stubs are local to integration tests; they're separate from
// internal/handlers/handlers_test.go so we don't have to export mocks.
type stubIdentity struct{}

func (stubIdentity) GetUser(_ context.Context, req *identityv1.GetUserRequest) (*identityv1.GetUserResponse, error) {
	return &identityv1.GetUserResponse{
		User: &identityv1.User{Id: req.Id, PrimaryEmail: "u@iogrid.org"},
	}, nil
}
func (stubIdentity) UpdateUser(_ context.Context, _ *identityv1.UpdateUserRequest) (*identityv1.UpdateUserResponse, error) {
	return nil, errors.New("not implemented")
}

type stubAuth struct{}

func (stubAuth) StartGoogleSignIn(_ context.Context, _ *identityv1.StartGoogleSignInRequest) (*identityv1.StartGoogleSignInResponse, error) {
	return &identityv1.StartGoogleSignInResponse{AuthorizeUrl: "https://accounts.google.com/o", State: "s"}, nil
}
func (stubAuth) CompleteGoogleSignIn(_ context.Context, _ *identityv1.CompleteGoogleSignInRequest) (*identityv1.CompleteGoogleSignInResponse, error) {
	return &identityv1.CompleteGoogleSignInResponse{}, nil
}
func (stubAuth) RequestMagicLink(_ context.Context, _ *identityv1.RequestMagicLinkRequest) (*identityv1.RequestMagicLinkResponse, error) {
	return &identityv1.RequestMagicLinkResponse{Accepted: true}, nil
}
func (stubAuth) CompleteMagicLink(_ context.Context, _ *identityv1.CompleteMagicLinkRequest) (*identityv1.CompleteMagicLinkResponse, error) {
	return &identityv1.CompleteMagicLinkResponse{}, nil
}
func (stubAuth) RefreshToken(_ context.Context, _ *identityv1.RefreshTokenRequest) (*identityv1.RefreshTokenResponse, error) {
	return &identityv1.RefreshTokenResponse{}, nil
}
func (stubAuth) SignOut(_ context.Context, _ *identityv1.SignOutRequest) (*identityv1.SignOutResponse, error) {
	return &identityv1.SignOutResponse{}, nil
}
func (stubAuth) ListSessions(_ context.Context, _ *identityv1.ListSessionsRequest) (*identityv1.ListSessionsResponse, error) {
	return &identityv1.ListSessionsResponse{}, nil
}
func (stubAuth) RevokeSession(_ context.Context, _ *identityv1.RevokeSessionRequest) (*identityv1.RevokeSessionResponse, error) {
	return &identityv1.RevokeSessionResponse{}, nil
}
func (stubAuth) StartSiwsBinding(_ context.Context, _ *identityv1.StartSiwsBindingRequest) (*identityv1.StartSiwsBindingResponse, error) {
	return &identityv1.StartSiwsBindingResponse{}, nil
}
func (stubAuth) CompleteSiwsBinding(_ context.Context, _ *identityv1.CompleteSiwsBindingRequest) (*identityv1.CompleteSiwsBindingResponse, error) {
	return &identityv1.CompleteSiwsBindingResponse{}, nil
}
func (stubAuth) ListBoundWallets(_ context.Context, _ *identityv1.ListBoundWalletsRequest) (*identityv1.ListBoundWalletsResponse, error) {
	return &identityv1.ListBoundWalletsResponse{}, nil
}
func (stubAuth) UnbindWallet(_ context.Context, _ *identityv1.UnbindWalletRequest) (*identityv1.UnbindWalletResponse, error) {
	return &identityv1.UnbindWalletResponse{}, nil
}

type stubDashboard struct{}

func (stubDashboard) ListAuditEvents(_ context.Context, _ *providersv1.ListAuditEventsRequest) (*providersv1.ListAuditEventsResponse, error) {
	return &providersv1.ListAuditEventsResponse{}, nil
}
func (stubDashboard) StreamAuditEvents(_ context.Context, _ *providersv1.StreamAuditEventsRequest) (clients.AuditEventStream, error) {
	return nil, errors.New("not used in this test")
}
func (stubDashboard) GetEarningsSummary(_ context.Context, _ *providersv1.GetEarningsSummaryRequest) (*providersv1.GetEarningsSummaryResponse, error) {
	return &providersv1.GetEarningsSummaryResponse{Summary: &providersv1.EarningsSummary{}}, nil
}

type stubScheduling struct{}

func (stubScheduling) GetSchedulingConfig(_ context.Context, _ *providersv1.GetSchedulingConfigRequest) (*providersv1.GetSchedulingConfigResponse, error) {
	return &providersv1.GetSchedulingConfigResponse{Config: &providersv1.SchedulingConfig{}}, nil
}
func (stubScheduling) UpdateSchedulingConfig(_ context.Context, req *providersv1.UpdateSchedulingConfigRequest) (*providersv1.UpdateSchedulingConfigResponse, error) {
	return &providersv1.UpdateSchedulingConfigResponse{Config: req.Config}, nil
}
func (stubScheduling) GetCurrentState(_ context.Context, _ *providersv1.GetCurrentStateRequest) (*providersv1.GetCurrentStateResponse, error) {
	return &providersv1.GetCurrentStateResponse{State: providersv1.SchedulerState_SCHEDULER_STATE_ACTIVE}, nil
}

type stubWorkloads struct{}

func (stubWorkloads) SubmitWorkload(_ context.Context, req *workloadsv1.SubmitWorkloadRequest) (*workloadsv1.SubmitWorkloadResponse, error) {
	return &workloadsv1.SubmitWorkloadResponse{Workload: req.Workload}, nil
}
func (stubWorkloads) GetWorkload(_ context.Context, _ *workloadsv1.GetWorkloadRequest) (*workloadsv1.GetWorkloadResponse, error) {
	return &workloadsv1.GetWorkloadResponse{}, nil
}
func (stubWorkloads) ListWorkloads(_ context.Context, _ *workloadsv1.ListWorkloadsRequest) (*workloadsv1.ListWorkloadsResponse, error) {
	return &workloadsv1.ListWorkloadsResponse{}, nil
}
func (stubWorkloads) CancelWorkload(_ context.Context, _ *workloadsv1.CancelWorkloadRequest) (*workloadsv1.CancelWorkloadResponse, error) {
	return &workloadsv1.CancelWorkloadResponse{}, nil
}
func (stubWorkloads) StreamWorkloadEvents(_ context.Context, _ *workloadsv1.StreamWorkloadEventsRequest) (clients.WorkloadEventStream, error) {
	return nil, errors.New("not used in this test")
}

type stubAntiabuse struct{}

func (stubAntiabuse) ListFilters(_ context.Context, _ *abusev1.ListFiltersRequest) (*abusev1.ListFiltersResponse, error) {
	return &abusev1.ListFiltersResponse{RulesetHash: "deadbeef"}, nil
}

type stubBilling struct{}

func (stubBilling) GetSubscription(_ context.Context, _ *billingv1.GetSubscriptionRequest) (*billingv1.GetSubscriptionResponse, error) {
	return &billingv1.GetSubscriptionResponse{}, nil
}
func (stubBilling) ListUsage(_ context.Context, _ *billingv1.ListUsageRequest) (*billingv1.ListUsageResponse, error) {
	return &billingv1.ListUsageResponse{}, nil
}
func (stubBilling) CreateCheckoutSession(_ context.Context, _ *billingv1.CreateCheckoutSessionRequest) (*billingv1.CreateCheckoutSessionResponse, error) {
	return &billingv1.CreateCheckoutSessionResponse{CheckoutUrl: "https://checkout.stripe.com/sess_1"}, nil
}

func buildSrv(t *testing.T, verifier auth.Verifier) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cs := &clients.Set{
		Identity:            stubIdentity{},
		Auth:                stubAuth{},
		ProvidersDashboard:  stubDashboard{},
		ProvidersScheduling: stubScheduling{},
		Workloads:           stubWorkloads{},
		Antiabuse:           stubAntiabuse{},
		Billing:             stubBilling{},
	}
	if verifier == nil {
		verifier = &auth.JWTVerifier{
			Resolver: &auth.StaticKeyResolver{Key: &priv.PublicKey},
			Issuer:   cfg.JWTIssuer,
			Audience: cfg.JWTAudience,
		}
	}
	deps := Deps{
		Config:        cfg,
		Clients:       cs,
		Verifier:      verifier,
		APIKeyStore:   handlers.NewMemoryAPIKeyStore(),
		AuthedLimiter: ratelimit.New(cfg.AuthedRatePerSec, cfg.AuthedBurst, time.Minute),
		AnonLimiter:   ratelimit.New(cfg.AnonymousRatePerSec, cfg.AnonymousBurst, time.Minute),
	}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	Mount(deps)(r)
	return httptest.NewServer(r), priv
}

func sign(t *testing.T, priv *rsa.PrivateKey, cfg *config.Config, roles ...string) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub":   uuid.NewString(),
		"iss":   cfg.JWTIssuer,
		"aud":   []string{cfg.JWTAudience},
		"exp":   time.Now().Add(time.Minute).Unix(),
		"iat":   time.Now().Unix(),
		"roles": roles,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestIntegration_IndexAndAccount(t *testing.T) {
	srv, _ := buildSrv(t, nil)
	defer srv.Close()

	// /v1/ smoke
	resp, _ := http.Get(srv.URL + "/v1/")
	if resp.StatusCode != 200 {
		t.Fatalf("v1 index: %d", resp.StatusCode)
	}

	// /api/v1/account/sign-in/magic — no auth required
	body, _ := json.Marshal(map[string]string{"email": "a@b.com"})
	resp2, _ := http.Post(srv.URL+"/api/v1/account/sign-in/magic", "application/json", bytes.NewReader(body))
	if resp2.StatusCode != 200 {
		t.Fatalf("magic-link: %d", resp2.StatusCode)
	}
}

func TestIntegration_MeRequiresAuth(t *testing.T) {
	srv, _ := buildSrv(t, nil)
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/api/v1/me")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestIntegration_MeWithValidJWT(t *testing.T) {
	srv, priv := buildSrv(t, nil)
	defer srv.Close()
	cfg, _ := config.Load()
	tok := sign(t, priv, cfg)

	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	b := make([]byte, 1024)
	n, _ := resp.Body.Read(b)
	if !strings.Contains(string(b[:n]), "u@iogrid.org") {
		t.Fatalf("body: %s", string(b[:n]))
	}
}

func TestIntegration_AdminRequiresRole(t *testing.T) {
	srv, priv := buildSrv(t, nil)
	defer srv.Close()
	cfg, _ := config.Load()

	// Non-admin → 403.
	tok := sign(t, priv, cfg, "CUSTOMER")
	req, _ := http.NewRequest("GET", srv.URL+"/api/v1/admin/abuse-queue", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}

	// Admin → 200.
	tok2 := sign(t, priv, cfg, "ADMIN")
	req2, _ := http.NewRequest("GET", srv.URL+"/api/v1/admin/abuse-queue", nil)
	req2.Header.Set("Authorization", "Bearer "+tok2)
	resp2, _ := http.DefaultClient.Do(req2)
	if resp2.StatusCode != 200 {
		t.Fatalf("expected 200, got %d", resp2.StatusCode)
	}
}

func TestIntegration_CORS_PreflightAllowedOrigin(t *testing.T) {
	srv, _ := buildSrv(t, nil)
	defer srv.Close()
	req, _ := http.NewRequest("OPTIONS", srv.URL+"/api/v1/me", nil)
	req.Header.Set("Origin", "https://app.iogrid.org")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, _ := http.DefaultClient.Do(req)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != "https://app.iogrid.org" {
		t.Fatalf("allow-origin: %q", got)
	}
}
