package handler

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/domains"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/ports"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/ratelimit"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/registry"
)

// stubBackend lets handler tests bypass real network calls.
type stubBackend struct {
	name string
	res  filters.Result
}

func (s *stubBackend) Name() string  { return s.name }
func (s *stubBackend) Enabled() bool { return true }
func (s *stubBackend) CheckURL(ctx context.Context, url string) filters.Result {
	return s.res
}
func (s *stubBackend) CheckDomain(ctx context.Context, d string) filters.Result {
	return filters.NewAllow(s.name)
}

func newTestService() *Service {
	allow := &stubBackend{name: "stub-allow", res: filters.NewAllow("stub-allow")}
	return &Service{
		Domains:    domains.NewDefaultPolicy(),
		Ports:      ports.NewDefaultPolicy(),
		Limiter:    ratelimit.New(ratelimit.Config{DefaultCustomerRate: 1000}, nil),
		Reputation: filters.NewOrchestrator(allow),
		Registry:   registry.NewDefaultPolicy(),
	}
}

func TestCheckUrl_AllowsBenign(t *testing.T) {
	s := newTestService()
	resp, err := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{TraceId: "trace-1"},
		Url:     "https://example.com/",
	}))
	if err != nil {
		t.Fatalf("CheckUrl err: %v", err)
	}
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("Decision = %v, want ALLOW", resp.Msg.Verdict.Decision)
	}
}

func TestCheckUrl_BlocksGov(t *testing.T) {
	s := newTestService()
	resp, err := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{},
		Url:     "https://anything.gov/path",
	}))
	if err != nil {
		t.Fatalf("CheckUrl err: %v", err)
	}
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Fatalf("Decision = %v, want BLOCK", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "destination_blocked" {
		t.Errorf("Reason = %q, want destination_blocked", resp.Msg.Verdict.Reason)
	}
}

func TestCheckUrl_BlocksBanking(t *testing.T) {
	s := newTestService()
	resp, _ := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{},
		Url:     "https://chase.com/",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Errorf("expected banking BLOCK; got %v", resp.Msg.Verdict.Decision)
	}
}

func TestCheckUrl_AdultRequiresOptIn(t *testing.T) {
	s := newTestService()
	// Without CategoryLookup → BLOCK
	resp, _ := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{ProviderId: &commonv1.UUID{Value: "p1"}},
		Url:     "https://pornhub.com/",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Errorf("expected adult BLOCK without opt-in; got %v", resp.Msg.Verdict.Decision)
	}
	// With opt-in → ALLOW
	s.CategoryLookup = func(ctx context.Context, providerID string) []antiabusev1.CategorySlug {
		return []antiabusev1.CategorySlug{antiabusev1.CategorySlug_CATEGORY_SLUG_ADULT_CONTENT}
	}
	resp, _ = s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{ProviderId: &commonv1.UUID{Value: "p1"}},
		Url:     "https://pornhub.com/",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("expected adult ALLOW with opt-in; got %v reason=%s", resp.Msg.Verdict.Decision, resp.Msg.Verdict.Reason)
	}
}

func TestCheckDomain_PortPolicy(t *testing.T) {
	s := newTestService()
	resp, err := s.CheckDomain(context.Background(), connect.NewRequest(&antiabusev1.CheckDomainRequest{
		Context: &antiabusev1.FilterContext{},
		Domain:  "smtp.example.com",
		Port:    25,
	}))
	if err != nil {
		t.Fatalf("CheckDomain err: %v", err)
	}
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Errorf("expected SMTP port BLOCK; got %v", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "destination_port_blocked" {
		t.Errorf("Reason = %q, want destination_port_blocked", resp.Msg.Verdict.Reason)
	}
}

func TestCheckUrl_RateLimit(t *testing.T) {
	s := newTestService()
	s.Limiter = ratelimit.New(ratelimit.Config{
		Window:              time.Second,
		DefaultCustomerRate: 1,
	}, nil)
	ctx := context.Background()
	req := func() *connect.Request[antiabusev1.CheckUrlRequest] {
		return connect.NewRequest(&antiabusev1.CheckUrlRequest{
			Context: &antiabusev1.FilterContext{WorkspaceId: &commonv1.UUID{Value: "ws-1"}},
			Url:     "https://example.com/",
		})
	}
	r1, _ := s.CheckUrl(ctx, req())
	r2, _ := s.CheckUrl(ctx, req())
	if r1.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Fatal("1st request should ALLOW")
	}
	if r2.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Errorf("2nd request should BLOCK on rate limit; got %v", r2.Msg.Verdict.Decision)
	}
	if r2.Msg.Verdict.Reason != "rate_limited" {
		t.Errorf("Reason = %q, want rate_limited", r2.Msg.Verdict.Reason)
	}
}

func TestCheckUrl_ReputationBlocks(t *testing.T) {
	s := newTestService()
	s.Reputation = filters.NewOrchestrator(&stubBackend{
		name: "stub-block",
		res:  filters.NewBlock("stub-block", "stub_match", "stubbed"),
	})
	resp, _ := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{},
		Url:     "https://example.com/",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Errorf("expected reputation BLOCK; got %v", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "stub_match" {
		t.Errorf("Reason = %q, want stub_match", resp.Msg.Verdict.Reason)
	}
}

func TestCheckContainerImage_AllowsApproved(t *testing.T) {
	s := newTestService()
	resp, err := s.CheckContainerImage(context.Background(), connect.NewRequest(&antiabusev1.CheckContainerImageRequest{
		ImageRef: "ghcr.io/iogrid/daemon:0.1",
	}))
	if err != nil {
		t.Fatalf("CheckContainerImage err: %v", err)
	}
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("expected ALLOW for ghcr.io/iogrid/...; got %v", resp.Msg.Verdict.Decision)
	}
}

func TestCheckContainerImage_BlocksUnknown(t *testing.T) {
	s := newTestService()
	resp, err := s.CheckContainerImage(context.Background(), connect.NewRequest(&antiabusev1.CheckContainerImageRequest{
		ImageRef: "public.ecr.aws/random/bad:1",
	}))
	if err != nil {
		t.Fatalf("CheckContainerImage err: %v", err)
	}
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Errorf("expected BLOCK for unknown registry; got %v", resp.Msg.Verdict.Decision)
	}
}

func TestListFilters_HashStable(t *testing.T) {
	s := newTestService()
	r1, _ := s.ListFilters(context.Background(), connect.NewRequest(&antiabusev1.ListFiltersRequest{}))
	r2, _ := s.ListFilters(context.Background(), connect.NewRequest(&antiabusev1.ListFiltersRequest{}))
	if r1.Msg.RulesetHash == "" {
		t.Fatal("RulesetHash empty")
	}
	if r1.Msg.RulesetHash != r2.Msg.RulesetHash {
		t.Errorf("RulesetHash unstable: %q vs %q", r1.Msg.RulesetHash, r2.Msg.RulesetHash)
	}
	if len(r1.Msg.Rules) == 0 {
		t.Error("Rules should not be empty")
	}
}

func TestReportEvent(t *testing.T) {
	s := newTestService()
	_, err := s.ReportEvent(context.Background(), connect.NewRequest(&antiabusev1.ReportEventRequest{
		Context:   &antiabusev1.FilterContext{TraceId: "trace-rep"},
		EventKind: "workload_blocked",
		Metadata:  map[string]string{"k": "v"},
	}))
	if err != nil {
		t.Errorf("ReportEvent err: %v", err)
	}
}

func TestCheckUrl_InvalidArgs(t *testing.T) {
	s := newTestService()
	_, err := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{Url: ""}))
	if err == nil {
		t.Error("empty URL should error")
	}
	_, err = s.CheckDomain(context.Background(), connect.NewRequest(&antiabusev1.CheckDomainRequest{Domain: ""}))
	if err == nil {
		t.Error("empty domain should error")
	}
}
