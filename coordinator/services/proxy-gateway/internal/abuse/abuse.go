// Package abuse wraps the antiabuse-svc Connect-RPC client behind a
// small, testable interface.
//
// docs/LEGAL.md mandates a pre-flight CheckUrl (or CheckDomain) call
// for every customer request BEFORE any bytes flow. The check covers:
//
//   - CSAM hash (NCMEC PhotoDNA)
//   - Phishing feeds (PhishTank, OpenPhish, Google Safe Browsing)
//   - Per-customer + per-provider rate limits (Redis token bucket
//     inside antiabuse-svc)
//   - Domain class (banking → KYC required, .gov/.mil → block,
//     adult → opt-in required)
//
// proxy-gateway never inspects content — it relies entirely on the
// destination URL / SNI / hostname for filtering.
package abuse

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1/antiabusev1connect"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
)

// Decision is the proxy-side reduction of the antiabuse FilterDecision.
type Decision int

const (
	// DecisionAllow — relay permitted.
	DecisionAllow Decision = iota
	// DecisionReview — relay permitted but flagged for audit review.
	DecisionReview
	// DecisionBlock — relay denied; emit SOCKS5 0x01 (General SOCKS
	// server failure, distinct from the 0x02 ConnNotAllowed reserved
	// for port-policy blocks) / HTTP 403. Per issue #360 the deny
	// code differs by reason so customer-side libraries can tell
	// "policy denial" from "antiabuse kill switch fired".
	DecisionBlock
	// DecisionRateLimit — denied; emit SOCKS5 0x01 / HTTP 429 with
	// the RetryAfter timestamp on the verdict.
	DecisionRateLimit
	// DecisionError — the filter could not produce a verdict (RPC
	// error, transport timeout, empty response). proxy.go translates
	// this to DecisionBlock (fail-closed, default per docs/LEGAL.md)
	// or DecisionAllow (fail-open, opt-in via ANTIABUSE_FAIL_OPEN env)
	// based on operator policy. The distinction is preserved here so
	// the proxy can attach a `antiabuse_unavailable` reason on the
	// audit row, separate from `phishtank_listed` etc — operators
	// reading the abuse_audit feed need to tell "filter said no" from
	// "filter couldn't reach the backend". See issue #360.
	DecisionError
)

// Verdict is the proxy-side decision envelope.
type Verdict struct {
	Decision   Decision
	Reason     string
	RetryAfter time.Time
}

// CheckInput is the per-request payload.
type CheckInput struct {
	CustomerID  string
	WorkspaceID string
	ProviderID  string // may be empty before provider is chosen
	WorkloadID  string
	Category    string // e.g. "e_commerce", "seo"
	URL         string // canonical full URL; falls back to "https://host:port" for raw CONNECT
	Method      string // HTTP method when known; empty for SOCKS5 CONNECT
	Host        string // SOCKS5/CONNECT destination host
	Port        uint32
	TraceID     string
}

// Filter is the contract — small and easy to fake in tests.
type Filter interface {
	// Check returns the pre-flight verdict. On any transport error the
	// implementation MUST return Decision = DecisionBlock with a clear
	// reason so the proxy fails closed.
	Check(ctx context.Context, in CheckInput) (Verdict, error)
}

// StaticFilter is a testing Filter that returns a fixed Verdict.
type StaticFilter struct {
	Verdict Verdict
	Err     error
}

// Check implements Filter.
func (s *StaticFilter) Check(_ context.Context, _ CheckInput) (Verdict, error) {
	return s.Verdict, s.Err
}

// ConnectFilter calls antiabuse-svc over Connect-RPC.
type ConnectFilter struct {
	Client antiabusev1connect.AbuseFilterServiceClient
}

// NewConnectFilter returns a Filter backed by antiabuse-svc.
func NewConnectFilter(baseURL string, httpClient connect.HTTPClient) *ConnectFilter {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	return &ConnectFilter{
		Client: antiabusev1connect.NewAbuseFilterServiceClient(httpClient, baseURL),
	}
}

// Check implements Filter.
func (c *ConnectFilter) Check(ctx context.Context, in CheckInput) (Verdict, error) {
	if c == nil || c.Client == nil {
		return Verdict{Decision: DecisionError, Reason: "antiabuse_unavailable"}, errors.New("antiabuse client nil")
	}
	pctx := &antiabusev1.FilterContext{
		Category: categorySlug(in.Category),
		TraceId:  in.TraceID,
	}
	if in.WorkspaceID != "" {
		pctx.WorkspaceId = &commonv1.UUID{Value: in.WorkspaceID}
	}
	if in.ProviderID != "" {
		pctx.ProviderId = &commonv1.UUID{Value: in.ProviderID}
	}
	if in.WorkloadID != "" {
		pctx.WorkloadId = &commonv1.UUID{Value: in.WorkloadID}
	}
	urlStr := in.URL
	if urlStr == "" {
		urlStr = fmt.Sprintf("https://%s:%d/", in.Host, in.Port)
	}
	resp, err := c.Client.CheckUrl(ctx, connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: pctx,
		Url:     urlStr,
		Method:  strings.ToUpper(in.Method),
	}))
	if err != nil {
		return Verdict{Decision: DecisionError, Reason: "antiabuse_unavailable"}, err
	}
	v := resp.Msg.GetVerdict()
	if v == nil {
		return Verdict{Decision: DecisionError, Reason: "antiabuse_unavailable"}, nil
	}
	out := Verdict{Reason: v.Reason}
	switch v.Decision {
	case antiabusev1.FilterDecision_FILTER_DECISION_ALLOW:
		out.Decision = DecisionAllow
	case antiabusev1.FilterDecision_FILTER_DECISION_REVIEW:
		out.Decision = DecisionReview
	default:
		out.Decision = DecisionBlock
	}
	if isRateLimitReason(v.Reason) {
		out.Decision = DecisionRateLimit
		if v.RetryAfter != nil {
			out.RetryAfter = v.RetryAfter.AsTime()
		}
	}
	_ = timestamppb.Now // keep import in case future code paths emit timestamps
	return out, nil
}

// isRateLimitReason maps antiabuse reason slugs to the rate-limit decision.
func isRateLimitReason(reason string) bool {
	switch reason {
	case "rate_limited", "rate_limit", "rate_limit_customer", "rate_limit_provider":
		return true
	}
	return false
}

// categorySlug maps the proxy's string-keyed category into the proto enum.
func categorySlug(s string) antiabusev1.CategorySlug {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "e_commerce", "ecommerce":
		return antiabusev1.CategorySlug_CATEGORY_SLUG_E_COMMERCE
	case "seo":
		return antiabusev1.CategorySlug_CATEGORY_SLUG_SEO
	case "ad_verification", "adverification":
		return antiabusev1.CategorySlug_CATEGORY_SLUG_AD_VERIFICATION
	case "ai_training_data", "ai":
		return antiabusev1.CategorySlug_CATEGORY_SLUG_AI_TRAINING_DATA
	case "iogrid_internal":
		return antiabusev1.CategorySlug_CATEGORY_SLUG_IOGRID_INTERNAL
	case "lead_gen", "leadgen":
		return antiabusev1.CategorySlug_CATEGORY_SLUG_LEAD_GEN
	case "social_intel":
		return antiabusev1.CategorySlug_CATEGORY_SLUG_SOCIAL_INTEL
	case "adult", "adult_content":
		return antiabusev1.CategorySlug_CATEGORY_SLUG_ADULT_CONTENT
	}
	return antiabusev1.CategorySlug_CATEGORY_SLUG_UNSPECIFIED
}
