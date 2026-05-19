// Package handler implements the iogrid.antiabuse.v1.AbuseFilterService
// Connect-RPC contract. It wires:
//
//   - domain classification (banking / .gov / .mil / adult)
//   - outbound-port policy
//   - per-customer + per-provider rate limits
//   - PhishTank + OpenPhish + Google Safe Browsing + NCMEC PhotoDNA
//     reputation backends
//   - audit-event emission to NATS JetStream
//
// Every Check* RPC takes the strictest decision returned by any layer
// (rate-limit → ports → domain class → reputation feeds). The Service
// satisfies the antiabusev1connect.AbuseFilterServiceHandler interface.
package handler

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/audit"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/domains"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/ports"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/ratelimit"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/registry"
)

// Service is the Connect-RPC handler.
type Service struct {
	Domains   *domains.Policy
	Ports     *ports.Policy
	Limiter   *ratelimit.Limiter
	Reputation *filters.Orchestrator
	Registry  *registry.Policy
	Audit     *audit.Emitter
	// PremiumLookup, if set, is consulted to decide tier (returns
	// true if customerID is a premium customer). Defaults to "all
	// default tier" when nil.
	PremiumLookup func(ctx context.Context, customerID string) bool
	// CategoryLookup, if set, returns the categories a provider
	// has opted-in to (used for adult opt-in gating). Defaults to
	// "no opt-ins".
	CategoryLookup func(ctx context.Context, providerID string) []antiabusev1.CategorySlug
}

// CheckUrl runs the full pre-flight pipeline against the supplied URL.
func (s *Service) CheckUrl(ctx context.Context, req *connect.Request[antiabusev1.CheckUrlRequest]) (*connect.Response[antiabusev1.CheckUrlResponse], error) {
	in := req.Msg
	if in == nil || strings.TrimSpace(in.Url) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("url is required"))
	}
	host, port := splitURL(in.Url)
	verdict := s.evaluate(ctx, evaluateInput{
		Context:    in.Context,
		CheckType:  "check_url",
		RawTarget:  in.Url,
		Host:       host,
		Port:       port,
		IsURL:      true,
	})
	return connect.NewResponse(&antiabusev1.CheckUrlResponse{Verdict: verdict}), nil
}

// CheckDomain runs the pipeline minus the URL-hash reputation feeds.
func (s *Service) CheckDomain(ctx context.Context, req *connect.Request[antiabusev1.CheckDomainRequest]) (*connect.Response[antiabusev1.CheckDomainResponse], error) {
	in := req.Msg
	if in == nil || strings.TrimSpace(in.Domain) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("domain is required"))
	}
	verdict := s.evaluate(ctx, evaluateInput{
		Context:    in.Context,
		CheckType:  "check_domain",
		RawTarget:  in.Domain,
		Host:       in.Domain,
		Port:       in.Port,
		IsURL:      false,
	})
	return connect.NewResponse(&antiabusev1.CheckDomainResponse{Verdict: verdict}), nil
}

// CheckContainerImage validates the image reference against the
// approved-registry list.
func (s *Service) CheckContainerImage(ctx context.Context, req *connect.Request[antiabusev1.CheckContainerImageRequest]) (*connect.Response[antiabusev1.CheckContainerImageResponse], error) {
	in := req.Msg
	if in == nil || strings.TrimSpace(in.ImageRef) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("image_ref is required"))
	}
	verdict := &antiabusev1.FilterVerdict{
		Decision: antiabusev1.FilterDecision_FILTER_DECISION_ALLOW,
		Reason:   "allow",
	}
	if s.Registry != nil {
		d := s.Registry.Check(in.ImageRef)
		if !d.Allowed {
			verdict = &antiabusev1.FilterVerdict{
				Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
				Reason:      d.Slug,
				Explanation: d.Explanation,
			}
		}
	}
	s.emitAudit(ctx, in.Context, "check_container_image", in.ImageRef, verdict, nil)
	return connect.NewResponse(&antiabusev1.CheckContainerImageResponse{Verdict: verdict}), nil
}

// ReportEvent ingests a flagged event from another service (proxy
// gateway, daemon-side bridge). The payload is forwarded straight
// into the audit pipeline.
func (s *Service) ReportEvent(ctx context.Context, req *connect.Request[antiabusev1.ReportEventRequest]) (*connect.Response[antiabusev1.ReportEventResponse], error) {
	in := req.Msg
	if in == nil || strings.TrimSpace(in.EventKind) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("event_kind is required"))
	}
	ev := audit.Event{
		CheckType: in.EventKind,
		Metadata:  in.Metadata,
		Timestamp: time.Now(),
	}
	if in.OccurredAt != nil && in.OccurredAt.IsValid() {
		ev.Timestamp = in.OccurredAt.AsTime()
	}
	if in.Context != nil {
		ev.TraceID = in.Context.TraceId
		if in.Context.ProviderId != nil {
			ev.ProviderID = in.Context.ProviderId.Value
		}
		if in.Context.WorkspaceId != nil {
			ev.WorkspaceID = in.Context.WorkspaceId.Value
		}
		if in.Context.WorkloadId != nil {
			ev.WorkloadID = in.Context.WorkloadId.Value
		}
	}
	if s.Audit != nil {
		_ = s.Audit.Emit(ctx, ev)
	}
	return connect.NewResponse(&antiabusev1.ReportEventResponse{}), nil
}

// ListFilters returns the current rule snapshot so the daemon can
// mirror it locally.
func (s *Service) ListFilters(ctx context.Context, req *connect.Request[antiabusev1.ListFiltersRequest]) (*connect.Response[antiabusev1.ListFiltersResponse], error) {
	rules := make([]*antiabusev1.FilterRule, 0, 16)
	now := time.Now()
	if s.Reputation != nil {
		rules = append(rules, s.Reputation.Snapshot(now)...)
	}
	rules = append(rules,
		&antiabusev1.FilterRule{
			Id:          "ports.default",
			Slug:        "ports.default",
			Description: "Outbound port allow/deny policy",
			Version:     "1",
			LastUpdatedAt: timestamppb.New(now),
		},
		&antiabusev1.FilterRule{
			Id:          "domains.banking",
			Slug:        "domains.banking",
			Description: "Banking-domain block (KYC-only)",
			Version:     "1",
			LastUpdatedAt: timestamppb.New(now),
		},
		&antiabusev1.FilterRule{
			Id:          "domains.government",
			Slug:        "domains.government",
			Description: ".gov / .mil unconditional block",
			Version:     "1",
			LastUpdatedAt: timestamppb.New(now),
		},
		&antiabusev1.FilterRule{
			Id:          "domains.adult",
			Slug:        "domains.adult",
			Description: "Adult content requires provider opt-in",
			Version:     "1",
			LastUpdatedAt: timestamppb.New(now),
		},
		&antiabusev1.FilterRule{
			Id:          "domains.blocked",
			Slug:        "domains.blocked",
			Description: "Operator deny-list (BLOCK_DOMAINS env / DB-backed loader)",
			Version:     "1",
			LastUpdatedAt: timestamppb.New(now),
		},
		&antiabusev1.FilterRule{
			Id:          "ratelimit.customer",
			Slug:        "ratelimit.customer",
			Description: "Per-customer aggregate RPS cap",
			Version:     "1",
			LastUpdatedAt: timestamppb.New(now),
		},
		&antiabusev1.FilterRule{
			Id:          "ratelimit.provider_destination",
			Slug:        "ratelimit.provider_destination",
			Description: "Per-provider per-destination RPS cap (high-value targets)",
			Version:     "1",
			LastUpdatedAt: timestamppb.New(now),
		},
		&antiabusev1.FilterRule{
			Id:          "registry.docker_allowlist",
			Slug:        "registry.docker_allowlist",
			Description: "Approved container-image registries",
			Version:     "1",
			LastUpdatedAt: timestamppb.New(now),
		},
	)
	return connect.NewResponse(&antiabusev1.ListFiltersResponse{
		Rules:        rules,
		RulesetHash:  filters.RulesetHash(rules),
	}), nil
}

// evaluateInput carries the data needed to run the unified pipeline.
type evaluateInput struct {
	Context   *antiabusev1.FilterContext
	CheckType string
	RawTarget string
	Host      string
	Port      uint32
	IsURL     bool
}

// evaluate runs the pipeline in this order:
//
//  1. Port policy
//  2. Domain class (gov / banking / adult opt-in)
//  3. Customer + provider-destination rate limits
//  4. External reputation feeds (URL only — domain skips PhishTank /
//     OpenPhish which are URL-bound)
//
// The strictest BLOCK wins; otherwise the most-severe REVIEW; otherwise
// ALLOW.
func (s *Service) evaluate(ctx context.Context, in evaluateInput) *antiabusev1.FilterVerdict {
	verdict := &antiabusev1.FilterVerdict{
		Decision: antiabusev1.FilterDecision_FILTER_DECISION_ALLOW,
		Reason:   "allow",
	}
	defer func() {
		s.emitAudit(ctx, in.Context, in.CheckType, in.RawTarget, verdict, nil)
	}()

	// 1) Port policy.
	if s.Ports != nil && in.Port != 0 {
		if d := s.Ports.Check(in.Port); !d.Allowed {
			verdict = &antiabusev1.FilterVerdict{
				Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
				Reason:      d.Slug,
				Explanation: d.Reason,
			}
			return verdict
		}
	}

	// 2) Domain class.
	if s.Domains != nil {
		class := s.Domains.Classify(in.Host)
		switch class {
		case domains.ClassBlocked:
			verdict = &antiabusev1.FilterVerdict{
				Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
				Reason:      "destination_blocked",
				Explanation: "destination matches operator deny-list (BLOCK_DOMAINS)",
			}
			return verdict
		case domains.ClassGovernment:
			verdict = &antiabusev1.FilterVerdict{
				Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
				Reason:      "destination_blocked",
				Explanation: ".gov / .mil destinations are blocked unconditionally",
			}
			return verdict
		case domains.ClassBanking:
			verdict = &antiabusev1.FilterVerdict{
				Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
				Reason:      "destination_blocked",
				Explanation: "banking destinations require KYC-verified customer opt-in (Phase 2)",
			}
			return verdict
		case domains.ClassAdult:
			if !s.providerOptedIntoAdult(ctx, in.Context) {
				verdict = &antiabusev1.FilterVerdict{
					Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
					Reason:      "category_disallowed",
					Explanation: "adult content requires provider opt-in (scheduling category ADULT_CONTENT)",
				}
				return verdict
			}
		}
	}

	// 3) Rate limits.
	if s.Limiter != nil {
		customerID := ""
		if in.Context != nil && in.Context.WorkspaceId != nil {
			customerID = in.Context.WorkspaceId.Value
		}
		if customerID != "" {
			tier := ratelimit.TierDefault
			if s.PremiumLookup != nil && s.PremiumLookup(ctx, customerID) {
				tier = ratelimit.TierPremium
			}
			if d := s.Limiter.CheckCustomer(ctx, customerID, tier); !d.Allowed {
				verdict = &antiabusev1.FilterVerdict{
					Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
					Reason:      "rate_limited",
					Explanation: d.Reason,
					RetryAfter:  timestamppb.New(time.Now().Add(d.RetryAfter)),
				}
				return verdict
			}
		}
		providerID := ""
		if in.Context != nil && in.Context.ProviderId != nil {
			providerID = in.Context.ProviderId.Value
		}
		if providerID != "" && in.Host != "" {
			if d := s.Limiter.CheckProviderDestination(ctx, providerID, in.Host); !d.Allowed {
				verdict = &antiabusev1.FilterVerdict{
					Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
					Reason:      "rate_limited",
					Explanation: d.Reason,
					RetryAfter:  timestamppb.New(time.Now().Add(d.RetryAfter)),
				}
				return verdict
			}
		}
	}

	// 4) Reputation feeds. URLs hit every backend; domains hit the
	// host-level feeds only (GSB + PhotoDNA stub).
	if s.Reputation != nil {
		var v filters.Verdict
		if in.IsURL {
			v = s.Reputation.CheckURL(ctx, in.RawTarget)
		} else {
			v = s.Reputation.CheckDomain(ctx, in.Host)
		}
		if v.Decision == antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
			verdict = &antiabusev1.FilterVerdict{
				Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
				Reason:      v.Reason,
				Explanation: v.Explanation,
			}
			return verdict
		}
		if v.Decision == antiabusev1.FilterDecision_FILTER_DECISION_REVIEW {
			verdict = &antiabusev1.FilterVerdict{
				Decision:    antiabusev1.FilterDecision_FILTER_DECISION_REVIEW,
				Reason:      v.Reason,
				Explanation: v.Explanation,
			}
			return verdict
		}
	}
	return verdict
}

// providerOptedIntoAdult consults the CategoryLookup hook (provider
// scheduling-config). Default = false (safest).
func (s *Service) providerOptedIntoAdult(ctx context.Context, c *antiabusev1.FilterContext) bool {
	if s.CategoryLookup == nil || c == nil || c.ProviderId == nil {
		return false
	}
	for _, cat := range s.CategoryLookup(ctx, c.ProviderId.Value) {
		if cat == antiabusev1.CategorySlug_CATEGORY_SLUG_ADULT_CONTENT {
			return true
		}
	}
	return false
}

func (s *Service) emitAudit(ctx context.Context, fc *antiabusev1.FilterContext, checkType, target string, v *antiabusev1.FilterVerdict, meta map[string]string) {
	if s.Audit == nil {
		return
	}
	// Dry-run mode (daemon surfacing rules) MUST NOT record.
	if fc != nil && fc.DryRun {
		return
	}
	ev := audit.Event{
		Timestamp: time.Now(),
		CheckType: checkType,
		Target:    target,
		Metadata:  meta,
	}
	if v != nil {
		ev.Decision = v.Decision.String()
		ev.Reason = v.Reason
		ev.Explanation = v.Explanation
	}
	if fc != nil {
		ev.TraceID = fc.TraceId
		if fc.ProviderId != nil {
			ev.ProviderID = fc.ProviderId.Value
		}
		if fc.WorkspaceId != nil {
			ev.WorkspaceID = fc.WorkspaceId.Value
		}
		if fc.WorkloadId != nil {
			ev.WorkloadID = fc.WorkloadId.Value
		}
	}
	_ = s.Audit.Emit(ctx, ev)
}

// splitURL extracts the host + port from a URL. Schemes default to
// 443 for https / 80 for http; anything else leaves port=0.
func splitURL(raw string) (host string, port uint32) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", 0
	}
	host = u.Hostname()
	switch p := u.Port(); p {
	case "":
		switch u.Scheme {
		case "http":
			port = 80
		case "https":
			port = 443
		}
	default:
		// Best-effort numeric parse; bad data → 0.
		var n uint32
		for _, r := range p {
			if r < '0' || r > '9' {
				return host, 0
			}
			n = n*10 + uint32(r-'0')
		}
		port = n
	}
	return host, port
}
