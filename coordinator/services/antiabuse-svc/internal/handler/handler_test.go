package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/domains"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters/phishtank"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters/photodna"
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

var errScanFailed = errors.New("scanner backend unreachable")

// fakeScanner is a registry.Scanner test double.
type fakeScanner struct {
	vulns []registry.Vulnerability
	err   error
}

func (f fakeScanner) Scan(_ context.Context, _ string) ([]registry.Vulnerability, error) {
	return f.vulns, f.err
}

// TestCheckContainerImage_RequireScanner_NoScanner_Reviews — issue #823
// HOLE 2: when REQUIRE_IMAGE_SCANNER is set but no real scanner is wired,
// an APPROVED-registry image must fail CLOSED to REVIEW (not silent ALLOW).
func TestCheckContainerImage_RequireScanner_NoScanner_Reviews(t *testing.T) {
	s := newTestService()
	s.RequireImageScanner = true
	s.Scanner = nil
	resp, err := s.CheckContainerImage(context.Background(), connect.NewRequest(&antiabusev1.CheckContainerImageRequest{
		ImageRef: "ghcr.io/iogrid/daemon:1",
	}))
	if err != nil {
		t.Fatalf("CheckContainerImage err: %v", err)
	}
	if resp.Msg.Verdict.Decision == antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Fatalf("RequireImageScanner=true + nil Scanner must NOT silently ALLOW: %+v", resp.Msg.Verdict)
	}
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_REVIEW {
		t.Errorf("Decision = %v, want REVIEW", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "image_scan_unavailable" {
		t.Errorf("Reason = %q, want image_scan_unavailable", resp.Msg.Verdict.Reason)
	}
}

// TestCheckContainerImage_RequireScanner_NoopScanner_Reviews — a Noop
// scanner counts as "no real scanner": still fail-closed to REVIEW.
func TestCheckContainerImage_RequireScanner_NoopScanner_Reviews(t *testing.T) {
	s := newTestService()
	s.RequireImageScanner = true
	s.Scanner = registry.NoopScanner{}
	resp, _ := s.CheckContainerImage(context.Background(), connect.NewRequest(&antiabusev1.CheckContainerImageRequest{
		ImageRef: "ghcr.io/iogrid/daemon:1",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_REVIEW {
		t.Errorf("NoopScanner must fail closed to REVIEW; got %v", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "image_scan_unavailable" {
		t.Errorf("Reason = %q, want image_scan_unavailable", resp.Msg.Verdict.Reason)
	}
}

// TestCheckContainerImage_NoRequire_PreservesAllow — default behaviour is
// preserved: with RequireImageScanner=false and no scanner, an approved
// image still ALLOWs (registry allowlist only).
func TestCheckContainerImage_NoRequire_PreservesAllow(t *testing.T) {
	s := newTestService()
	s.RequireImageScanner = false
	s.Scanner = nil
	resp, _ := s.CheckContainerImage(context.Background(), connect.NewRequest(&antiabusev1.CheckContainerImageRequest{
		ImageRef: "ghcr.io/iogrid/daemon:1",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("RequireImageScanner=false must preserve ALLOW; got %v reason=%s",
			resp.Msg.Verdict.Decision, resp.Msg.Verdict.Reason)
	}
}

// TestCheckContainerImage_CriticalVuln_Blocks — a real scanner reporting a
// CRITICAL vulnerability BLOCKs (slug image_vuln_critical).
func TestCheckContainerImage_CriticalVuln_Blocks(t *testing.T) {
	s := newTestService()
	s.Scanner = fakeScanner{vulns: []registry.Vulnerability{
		{ID: "CVE-2024-0001", Severity: "CRITICAL", Summary: "rce"},
	}}
	resp, _ := s.CheckContainerImage(context.Background(), connect.NewRequest(&antiabusev1.CheckContainerImageRequest{
		ImageRef: "ghcr.io/iogrid/daemon:1",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Fatalf("CRITICAL vuln must BLOCK; got %v", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "image_vuln_critical" {
		t.Errorf("Reason = %q, want image_vuln_critical", resp.Msg.Verdict.Reason)
	}
}

// TestCheckContainerImage_ScanError_Reviews — a real scanner that errors
// fails CLOSED to REVIEW (slug image_scan_error).
func TestCheckContainerImage_ScanError_Reviews(t *testing.T) {
	s := newTestService()
	s.Scanner = fakeScanner{err: errScanFailed}
	resp, _ := s.CheckContainerImage(context.Background(), connect.NewRequest(&antiabusev1.CheckContainerImageRequest{
		ImageRef: "ghcr.io/iogrid/daemon:1",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_REVIEW {
		t.Fatalf("scan error must fail closed to REVIEW; got %v", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "image_scan_error" {
		t.Errorf("Reason = %q, want image_scan_error", resp.Msg.Verdict.Reason)
	}
}

// TestCheckContainerImage_CleanScan_Allows — a real scanner with no
// CRITICAL/HIGH findings allows an approved image.
func TestCheckContainerImage_CleanScan_Allows(t *testing.T) {
	s := newTestService()
	s.Scanner = fakeScanner{vulns: []registry.Vulnerability{
		{ID: "CVE-2024-9999", Severity: "LOW", Summary: "noise"},
	}}
	resp, _ := s.CheckContainerImage(context.Background(), connect.NewRequest(&antiabusev1.CheckContainerImageRequest{
		ImageRef: "ghcr.io/iogrid/daemon:1",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("clean scan (LOW only) must ALLOW; got %v", resp.Msg.Verdict.Decision)
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

// TestCheckUrl_BlocksOutboundSMTPPort — issue #360 acceptance criterion
// #2: SOCKS5 to port 25 returns BLOCK with reason="outbound_port_blocked".
// The reason slug must match what the proxy-gateway audit emitter
// will surface to the customer-visible audit log + transparency feed.
func TestCheckUrl_BlocksOutboundSMTPPort(t *testing.T) {
	s := newTestService()
	resp, err := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{TraceId: "trace-smtp"},
		Url:     "tcp://mail.example:25/",
	}))
	if err != nil {
		t.Fatalf("CheckUrl err: %v", err)
	}
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Fatalf("port 25: Decision = %v, want BLOCK", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "destination_port_blocked" {
		t.Errorf("port 25: Reason = %q", resp.Msg.Verdict.Reason)
	}
}

// TestCheckUrl_BlocksAllOutboundSMTPVariants — every SMTP variant in
// docs/LEGAL.md MUST block. Caught a regression in scaffold rev where
// only 25 was wired (587/465/2525 silently passed).
func TestCheckUrl_BlocksAllOutboundSMTPVariants(t *testing.T) {
	s := newTestService()
	for _, p := range []uint32{25, 465, 587, 2525, 6667, 6697, 9001, 9030} {
		req := &antiabusev1.CheckDomainRequest{
			Context: &antiabusev1.FilterContext{},
			Domain:  "any.example",
			Port:    p,
		}
		resp, err := s.CheckDomain(context.Background(), connect.NewRequest(req))
		if err != nil {
			t.Fatalf("CheckDomain port=%d err: %v", p, err)
		}
		if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
			t.Errorf("port %d: Decision = %v, want BLOCK", p, resp.Msg.Verdict.Decision)
		}
	}
}

// TestCheckUrl_SyntheticPhishHosts — issue #360 acceptance criterion
// #1: SOCKS5 / HTTP CONNECT to a known-phish destination returns
// BLOCK with reason="phishtank_listed". The proxy-gateway integration
// test uses these synthetic hosts to verify the full deny path
// without registering for PhishTank's real feed.
func TestCheckUrl_SyntheticPhishHosts(t *testing.T) {
	pt := phishtank.New(phishtank.Options{}) // no API key, no live feed
	pd := photodna.New(photodna.Options{})   // stub mode
	s := &Service{
		Domains:    domains.NewDefaultPolicy(),
		Ports:      ports.NewDefaultPolicy(),
		Limiter:    ratelimit.New(ratelimit.Config{DefaultCustomerRate: 1000}, nil),
		Reputation: filters.NewOrchestrator(pt, pd),
		Registry:   registry.NewDefaultPolicy(),
	}
	for _, host := range []string{
		"malware.testing.google.test",
		"phishing-test.iogrid.org",
	} {
		// CheckUrl path
		resp, err := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
			Context: &antiabusev1.FilterContext{},
			Url:     "https://" + host + "/payload",
		}))
		if err != nil {
			t.Fatalf("host=%s err: %v", host, err)
		}
		if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
			t.Errorf("host=%s CheckUrl Decision = %v, want BLOCK", host, resp.Msg.Verdict.Decision)
		}
		if resp.Msg.Verdict.Reason != "phishtank_listed" {
			t.Errorf("host=%s CheckUrl Reason = %q, want phishtank_listed", host, resp.Msg.Verdict.Reason)
		}
		// CheckDomain path (SOCKS5/CONNECT exercise this)
		dr, err := s.CheckDomain(context.Background(), connect.NewRequest(&antiabusev1.CheckDomainRequest{
			Context: &antiabusev1.FilterContext{},
			Domain:  host,
			Port:    443,
		}))
		if err != nil {
			t.Fatalf("host=%s domain err: %v", host, err)
		}
		if dr.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
			t.Errorf("host=%s CheckDomain Decision = %v, want BLOCK", host, dr.Msg.Verdict.Decision)
		}
	}
}

// TestCheckUrl_SyntheticCSAMFixture — issue #360 Part A. The
// "/csam-test-fixture/" URL token triggers a deterministic BLOCK
// even with PHOTODNA_API_KEY unset, so the proxy-gateway integration
// test can prove the CSAM-deny path without an NCMEC partnership.
func TestCheckUrl_SyntheticCSAMFixture(t *testing.T) {
	pt := phishtank.New(phishtank.Options{}) // no API key
	pd := photodna.New(photodna.Options{})   // stub mode
	s := &Service{
		Domains:    domains.NewDefaultPolicy(),
		Ports:      ports.NewDefaultPolicy(),
		Limiter:    ratelimit.New(ratelimit.Config{DefaultCustomerRate: 1000}, nil),
		Reputation: filters.NewOrchestrator(pt, pd),
		Registry:   registry.NewDefaultPolicy(),
	}
	resp, err := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{},
		Url:     "https://image-host.example/csam-test-fixture/sample.jpg",
	}))
	if err != nil {
		t.Fatalf("CheckUrl err: %v", err)
	}
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Fatalf("CSAM fixture: Decision = %v, want BLOCK", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "csam_hash_match" {
		t.Errorf("CSAM fixture: Reason = %q, want csam_hash_match", resp.Msg.Verdict.Reason)
	}
}

// TestCheckUrl_DecisionMergeBlocksWin — when several filters disagree
// (e.g. port allowed + domain class clean + reputation BLOCK), the
// merged verdict MUST be the strictest. This is the legal-defence
// invariant from docs/LEGAL.md: a single fired filter is enough to
// kill the request.
func TestCheckUrl_DecisionMergeBlocksWin(t *testing.T) {
	// Inject a reputation backend that BLOCKs.
	block := &stubBackend{name: "stub-block", res: filters.NewBlock("stub-block", "csam_hash_match", "test")}
	allow := &stubBackend{name: "stub-allow", res: filters.NewAllow("stub-allow")}
	s := &Service{
		Domains:    domains.NewDefaultPolicy(),
		Ports:      ports.NewDefaultPolicy(),
		Limiter:    ratelimit.New(ratelimit.Config{DefaultCustomerRate: 1000}, nil),
		Reputation: filters.NewOrchestrator(allow, block, allow),
		Registry:   registry.NewDefaultPolicy(),
	}
	resp, err := s.CheckUrl(context.Background(), connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{},
		Url:     "https://example.com/", // domain class = clean, port 443 = allowed
	}))
	if err != nil {
		t.Fatalf("CheckUrl err: %v", err)
	}
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Fatalf("merge decision = %v, want BLOCK (strictest wins)", resp.Msg.Verdict.Decision)
	}
	if resp.Msg.Verdict.Reason != "csam_hash_match" {
		t.Errorf("Reason = %q, want csam_hash_match", resp.Msg.Verdict.Reason)
	}
}
