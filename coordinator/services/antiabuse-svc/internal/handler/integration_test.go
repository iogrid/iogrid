//go:build integration

// Integration tests for the antiabuse-svc handler. Requires Redis +
// a local HTTP mock for the PhishTank / OpenPhish feeds.
//
// Run with:
//
//	go test -tags=integration ./internal/handler/...
//
// REDIS_URL (default redis://localhost:6379/0) controls the rate-limit
// backend. The PhishTank + OpenPhish feeds are mocked inside the test
// so no public Internet is needed.
package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/redis/go-redis/v9"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/domains"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters/openphish"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters/phishtank"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/ports"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/ratelimit"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/registry"
)

func redisURL() string {
	if u := os.Getenv("REDIS_URL"); u != "" {
		return u
	}
	return "redis://localhost:6379/0"
}

func TestIntegration_EndToEnd(t *testing.T) {
	// Mock PhishTank.
	pt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"url":"https://malicious.example/login"}]`))
	}))
	defer pt.Close()
	// Mock OpenPhish.
	op := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("https://malicious.example/login\n"))
	}))
	defer op.Close()

	opt, err := redis.ParseURL(redisURL())
	if err != nil {
		t.Fatalf("redis parse: %v", err)
	}
	rdb := redis.NewClient(opt)
	defer rdb.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not reachable (%v): skipping integration test", err)
	}

	ptB := phishtank.New(phishtank.Options{FeedURL: pt.URL})
	if err := ptB.Refresh(ctx); err != nil {
		t.Fatalf("PhishTank refresh: %v", err)
	}
	opB := openphish.New(openphish.Options{FeedURL: op.URL})
	if err := opB.Refresh(ctx); err != nil {
		t.Fatalf("OpenPhish refresh: %v", err)
	}

	s := &Service{
		Domains:    domains.NewDefaultPolicy(),
		Ports:      ports.NewDefaultPolicy(),
		Limiter:    ratelimit.New(ratelimit.Config{DefaultCustomerRate: 5}, rdb),
		Reputation: filters.NewOrchestrator(ptB, opB),
		Registry:   registry.NewDefaultPolicy(),
	}

	// Benign URL → ALLOW
	resp, _ := s.CheckUrl(ctx, connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{WorkspaceId: &commonv1.UUID{Value: "ws-int-1"}},
		Url:     "https://example.com/",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
		t.Fatalf("benign URL: Decision = %v", resp.Msg.Verdict.Decision)
	}

	// Phish URL → BLOCK
	resp, _ = s.CheckUrl(ctx, connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{WorkspaceId: &commonv1.UUID{Value: "ws-int-2"}},
		Url:     "https://malicious.example/login",
	}))
	if resp.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Fatalf("phish URL: Decision = %v", resp.Msg.Verdict.Decision)
	}

	// Redis-backed rate limit kicks in after 5 allows for the same
	// customer. Use a unique workspace so we don't conflict with prior tests.
	wsID := "ws-int-rl-" + time.Now().Format("150405.000000000")
	for i := 0; i < 5; i++ {
		r, _ := s.CheckUrl(ctx, connect.NewRequest(&antiabusev1.CheckUrlRequest{
			Context: &antiabusev1.FilterContext{WorkspaceId: &commonv1.UUID{Value: wsID}},
			Url:     "https://example.com/",
		}))
		if r.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_ALLOW {
			t.Fatalf("RL request %d should ALLOW; got %v", i, r.Msg.Verdict.Decision)
		}
	}
	r, _ := s.CheckUrl(ctx, connect.NewRequest(&antiabusev1.CheckUrlRequest{
		Context: &antiabusev1.FilterContext{WorkspaceId: &commonv1.UUID{Value: wsID}},
		Url:     "https://example.com/",
	}))
	if r.Msg.Verdict.Decision != antiabusev1.FilterDecision_FILTER_DECISION_BLOCK {
		t.Errorf("6th RL request should BLOCK; got %v", r.Msg.Verdict.Decision)
	}
}
