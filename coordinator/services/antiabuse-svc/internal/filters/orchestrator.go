package filters

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
	"time"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
)

// Orchestrator fans a CheckURL / CheckDomain call out to every
// configured Backend in parallel and aggregates the results into a
// single FilterVerdict.
//
// Aggregation rule:
//
//   - any backend returning BLOCK wins (strictest decision)
//   - otherwise, REVIEW wins
//   - otherwise, ALLOW
//
// Backend errors are surfaced via the Result.Err field but do NOT
// flip the decision — best-effort + alert-on-error is the policy
// per docs/LEGAL.md ("filters must be functional and verified" but
// not single-point-of-failure on transient feed outages).
type Orchestrator struct {
	backends []Backend
	timeout  time.Duration
}

// NewOrchestrator constructs an Orchestrator with the given backends.
// Default per-backend timeout is 5s.
func NewOrchestrator(backends ...Backend) *Orchestrator {
	return &Orchestrator{backends: backends, timeout: 5 * time.Second}
}

// SetTimeout overrides the per-backend timeout.
func (o *Orchestrator) SetTimeout(d time.Duration) {
	if d > 0 {
		o.timeout = d
	}
}

// Backends returns the configured backends (for ListFilters mirroring).
func (o *Orchestrator) Backends() []Backend { return o.backends }

// Verdict is the aggregated outcome of running every backend.
type Verdict struct {
	// Decision is the strictest decision returned by any backend.
	Decision antiabusev1.FilterDecision
	// Reason is the slug of the first matching backend (or
	// "no_match" / "allow" otherwise).
	Reason string
	// Explanation surfaces the matching backend's prose.
	Explanation string
	// Results holds every per-backend Result for audit logging.
	Results []Result
}

// CheckURL fans out across every backend and aggregates.
func (o *Orchestrator) CheckURL(ctx context.Context, url string) Verdict {
	return o.run(ctx, func(b Backend, ctx context.Context) Result {
		return b.CheckURL(ctx, url)
	})
}

// CheckDomain fans out across every backend and aggregates.
func (o *Orchestrator) CheckDomain(ctx context.Context, domain string) Verdict {
	return o.run(ctx, func(b Backend, ctx context.Context) Result {
		return b.CheckDomain(ctx, domain)
	})
}

func (o *Orchestrator) run(parent context.Context, fn func(Backend, context.Context) Result) Verdict {
	if len(o.backends) == 0 {
		return Verdict{
			Decision: antiabusev1.FilterDecision_FILTER_DECISION_ALLOW,
			Reason:   "no_backends_configured",
		}
	}
	results := make([]Result, len(o.backends))
	var wg sync.WaitGroup
	for i, b := range o.backends {
		i, b := i, b
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(parent, o.timeout)
			defer cancel()
			results[i] = fn(b, ctx)
			if results[i].Backend == "" {
				results[i].Backend = b.Name()
			}
		}()
	}
	wg.Wait()

	v := Verdict{
		Decision: antiabusev1.FilterDecision_FILTER_DECISION_ALLOW,
		Reason:   "no_match",
		Results:  results,
	}
	// Strictest wins; preserve the first matching backend's prose so
	// the operator can see which feed flagged it.
	for _, r := range results {
		if r.Decision == antiabusev1.FilterDecision_FILTER_DECISION_BLOCK && r.Match {
			v.Decision = antiabusev1.FilterDecision_FILTER_DECISION_BLOCK
			v.Reason = r.Reason
			v.Explanation = r.Explanation
			break
		}
	}
	if v.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		for _, r := range results {
			if r.Decision == antiabusev1.FilterDecision_FILTER_DECISION_REVIEW {
				v.Decision = antiabusev1.FilterDecision_FILTER_DECISION_REVIEW
				v.Reason = r.Reason
				v.Explanation = r.Explanation
				break
			}
		}
	}
	return v
}

// Snapshot returns the static rule list mirrored to the daemon via
// ListFilters. Each rule slug is stable so the daemon can dedupe.
func (o *Orchestrator) Snapshot(now time.Time) []*antiabusev1.FilterRule {
	rules := make([]*antiabusev1.FilterRule, 0, len(o.backends))
	for _, b := range o.backends {
		state := "disabled"
		if b.Enabled() {
			state = "enabled"
		}
		rules = append(rules, &antiabusev1.FilterRule{
			Id:          b.Name(),
			Slug:        b.Name(),
			Description: "external reputation feed (" + state + ")",
			Version:     "1",
		})
	}
	return rules
}

// RulesetHash builds the stable hash the daemon compares against the
// local mirror. Order-independent across backends.
func RulesetHash(rules []*antiabusev1.FilterRule) string {
	slugs := make([]string, 0, len(rules))
	for _, r := range rules {
		slugs = append(slugs, r.Slug+":"+r.Version)
	}
	sort.Strings(slugs)
	sum := sha256.Sum256([]byte(strings.Join(slugs, "|")))
	return hex.EncodeToString(sum[:])
}
