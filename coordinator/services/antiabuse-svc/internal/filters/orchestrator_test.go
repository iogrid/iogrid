package filters

import (
	"context"
	"testing"
	"time"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
)

// stubBackend lets us drive Orchestrator behaviour directly.
type stubBackend struct {
	name     string
	enabled  bool
	urlRes   Result
	domRes   Result
	urlSleep time.Duration
}

func (s *stubBackend) Name() string  { return s.name }
func (s *stubBackend) Enabled() bool { return s.enabled }
func (s *stubBackend) CheckURL(ctx context.Context, url string) Result {
	if s.urlSleep > 0 {
		select {
		case <-time.After(s.urlSleep):
		case <-ctx.Done():
			return NewError(s.name, ctx.Err())
		}
	}
	return s.urlRes
}
func (s *stubBackend) CheckDomain(ctx context.Context, d string) Result {
	return s.domRes
}

func TestOrchestrator_StrictestBlockWins(t *testing.T) {
	a := &stubBackend{name: "a", enabled: true, urlRes: NewAllow("a")}
	b := &stubBackend{name: "b", enabled: true, urlRes: NewBlock("b", "b_match", "B matched")}
	c := &stubBackend{name: "c", enabled: true, urlRes: NewAllow("c")}
	o := NewOrchestrator(a, b, c)
	v := o.CheckURL(context.Background(), "https://x")
	if v.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Errorf("Decision = %v, want BLOCK", v.Decision)
	}
	if v.Reason != "b_match" {
		t.Errorf("Reason = %q, want b_match", v.Reason)
	}
	if len(v.Results) != 3 {
		t.Errorf("expected 3 per-backend results; got %d", len(v.Results))
	}
}

func TestOrchestrator_ReviewBeatsAllow(t *testing.T) {
	a := &stubBackend{name: "a", enabled: true, urlRes: NewAllow("a")}
	b := &stubBackend{name: "b", enabled: true, urlRes: Result{
		Backend:  "b",
		Match:    false,
		Decision: antiabusev1.FilterDecision_FILTER_DECISION_REVIEW,
		Reason:   "needs_review",
	}}
	o := NewOrchestrator(a, b)
	v := o.CheckURL(context.Background(), "https://x")
	if v.Decision != antiabusev1.FilterDecision_FILTER_DECISION_REVIEW {
		t.Errorf("Decision = %v, want REVIEW", v.Decision)
	}
}

func TestOrchestrator_NoBackendsAllows(t *testing.T) {
	o := NewOrchestrator()
	v := o.CheckURL(context.Background(), "https://x")
	if v.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("Decision = %v, want ALLOW", v.Decision)
	}
}

func TestOrchestrator_BackendErrorTreatedAsAllow(t *testing.T) {
	a := &stubBackend{name: "a", enabled: true, urlRes: NewError("a", context.DeadlineExceeded)}
	b := &stubBackend{name: "b", enabled: true, urlRes: NewAllow("b")}
	o := NewOrchestrator(a, b)
	v := o.CheckURL(context.Background(), "https://x")
	if v.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("Decision = %v, want ALLOW", v.Decision)
	}
}

func TestSnapshot_HashStable(t *testing.T) {
	a := &stubBackend{name: "a", enabled: true}
	b := &stubBackend{name: "b", enabled: false}
	o := NewOrchestrator(a, b)
	r1 := o.Snapshot(time.Now())
	r2 := o.Snapshot(time.Now())
	if RulesetHash(r1) != RulesetHash(r2) {
		t.Errorf("RulesetHash differs across snapshots")
	}
}

func TestOrchestrator_Timeout(t *testing.T) {
	slow := &stubBackend{
		name:     "slow",
		enabled:  true,
		urlRes:   NewAllow("slow"),
		urlSleep: 50 * time.Millisecond,
	}
	o := NewOrchestrator(slow)
	o.SetTimeout(5 * time.Millisecond)
	v := o.CheckURL(context.Background(), "https://x")
	if v.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Errorf("on timeout, expected ALLOW; got %v", v.Decision)
	}
}
