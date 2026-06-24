package proxy

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/abuse"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/auth"
	"github.com/iogrid/iogrid/coordinator/services/proxy-gateway/internal/config"
)

// TestPreflight_Unreachable_FailsClosed — issue #823 / #360: when the
// antiabuse filter cannot produce a verdict (DecisionError, e.g. RPC
// unreachable) and ANTIABUSE_FAIL_OPEN is false (the default), preflight
// MUST collapse to DecisionBlock with reason="antiabuse_unavailable".
// A control-plane outage MUST NOT silently disable the kill switch.
func TestPreflight_Unreachable_FailsClosed(t *testing.T) {
	s := &Server{
		Config: config.Config{AntiabuseFailOpen: false},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Filter: &abuse.StaticFilter{
			Verdict: abuse.Verdict{Decision: abuse.DecisionError, Reason: "antiabuse_unavailable"},
			Err:     errors.New("rpc timeout"),
		},
	}
	c := &auth.Customer{CustomerID: "cust-1", WorkspaceID: "ws-1"}
	v, _ := s.preflight(context.Background(), c, "any.example", 443, "trace-fc")
	if v.Decision != abuse.DecisionBlock {
		t.Fatalf("fail-closed: Decision = %v, want DecisionBlock", v.Decision)
	}
	if v.Reason != "antiabuse_unavailable" {
		t.Errorf("Reason = %q, want antiabuse_unavailable", v.Reason)
	}
}

// TestPreflight_Unreachable_FailsOpenWhenConfigured — the opt-in escape
// valve: with ANTIABUSE_FAIL_OPEN=true, an unreachable filter collapses
// to DecisionAllow so operators can keep the data plane flowing during a
// declared antiabuse-svc incident.
func TestPreflight_Unreachable_FailsOpenWhenConfigured(t *testing.T) {
	s := &Server{
		Config: config.Config{AntiabuseFailOpen: true},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Filter: &abuse.StaticFilter{
			Verdict: abuse.Verdict{Decision: abuse.DecisionError, Reason: "antiabuse_unavailable"},
			Err:     errors.New("rpc timeout"),
		},
	}
	c := &auth.Customer{CustomerID: "cust-1", WorkspaceID: "ws-1"}
	v, _ := s.preflight(context.Background(), c, "any.example", 443, "trace-fo")
	if v.Decision != abuse.DecisionAllow {
		t.Fatalf("fail-open: Decision = %v, want DecisionAllow", v.Decision)
	}
	if v.Reason != "antiabuse_fail_open" {
		t.Errorf("Reason = %q, want antiabuse_fail_open", v.Reason)
	}
}
