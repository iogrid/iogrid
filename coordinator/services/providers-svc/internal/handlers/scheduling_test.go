package handlers

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1/providersv1connect"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

func TestGetSchedulingConfig_Defaults(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	resp, err := h.GetSchedulingConfig(context.Background(), connect.NewRequest(&providersv1.GetSchedulingConfigRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	caps := resp.Msg.Config.GetCaps()
	if caps.GetBandwidthCapGbPerMonth() != 50 {
		t.Fatalf("bandwidth default: got %d want 50", caps.GetBandwidthCapGbPerMonth())
	}
	if caps.GetCpuCapPercent() != 30 || caps.GetMemoryCapPercent() != 25 {
		t.Fatalf("cpu/mem defaults wrong: %+v", caps)
	}
}

func TestUpdateSchedulingConfig_RoundTrip(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	ctx := context.Background()
	cfg := &providersv1.SchedulingConfig{
		ProviderId: &commonv1.UUID{Value: "p1"},
		Caps: &providersv1.ResourceCaps{
			BandwidthCapGbPerMonth: 200,
			CpuCapPercent:          40,
			MemoryCapPercent:       35,
		},
		Idle: &providersv1.IdleDetection{Enabled: true, IdleThresholdSeconds: 600},
		Calendar: &providersv1.CalendarSchedule{
			Windows: []*providersv1.CalendarWindow{{
				DaysOfWeek:     []uint32{1, 2, 3, 4, 5},
				StartLocalTime: "22:00",
				EndLocalTime:   "07:00",
				Timezone:       "America/New_York",
			}},
		},
	}
	if _, err := h.UpdateSchedulingConfig(ctx, connect.NewRequest(&providersv1.UpdateSchedulingConfigRequest{Config: cfg})); err != nil {
		t.Fatalf("update: %v", err)
	}
	resp, err := h.GetSchedulingConfig(ctx, connect.NewRequest(&providersv1.GetSchedulingConfigRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.Msg.Config.Caps.BandwidthCapGbPerMonth != 200 {
		t.Fatalf("did not persist")
	}
}

func TestUpdateSchedulingConfig_RejectsBadCalendar(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	_, err := h.UpdateSchedulingConfig(context.Background(), connect.NewRequest(&providersv1.UpdateSchedulingConfigRequest{
		Config: &providersv1.SchedulingConfig{
			ProviderId: &commonv1.UUID{Value: "p1"},
			Calendar: &providersv1.CalendarSchedule{Windows: []*providersv1.CalendarWindow{{
				StartLocalTime: "25:99",
				EndLocalTime:   "11:11",
				Timezone:       "UTC",
				DaysOfWeek:     []uint32{1},
			}}},
		},
	}))
	if err == nil {
		t.Fatalf("expected validation failure")
	}
}

func TestUpdateSchedulingConfig_RejectsOver100Pct(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	_, err := h.UpdateSchedulingConfig(context.Background(), connect.NewRequest(&providersv1.UpdateSchedulingConfigRequest{
		Config: &providersv1.SchedulingConfig{
			ProviderId: &commonv1.UUID{Value: "p1"},
			Caps:       &providersv1.ResourceCaps{CpuCapPercent: 150},
		},
	}))
	if err == nil {
		t.Fatalf("expected validation failure")
	}
}

// #311: this is the test that would have caught the original bug. The
// daemon's StreamHeartbeats bidi MUST update providers.last_seen_at on
// every received heartbeat. Without it, /admin/providers shows paired
// daemons frozen at registered_at forever.
//
// We drive the live Connect-RPC server over h2c via httptest so the
// handler's actual code path runs (Receive() → Store.UpdateLastSeen →
// Send(ack)), not just the in-process function call.
func TestStreamHeartbeats_BumpsLastSeenAt(t *testing.T) {
	ctx := context.Background()
	s := store.NewInMemory()

	// Seed a paired provider; capture the "before" timestamp.
	p := &store.Provider{
		OwnerUserID:  "owner-x",
		DisplayName:  "hatice mbp",
		RegisteredAt: time.Now().Add(-24 * time.Hour).UTC(),
	}
	if err := s.CreateProvider(ctx, p); err != nil {
		t.Fatalf("seed: %v", err)
	}
	before, _ := s.GetProvider(ctx, p.ID)
	if !before.LastSeenAt.Before(time.Now().Add(-1 * time.Hour)) {
		t.Fatalf("seed precondition violated — LastSeenAt should be stale (was %v)", before.LastSeenAt)
	}

	sched := NewSchedulingHandler(s, nil)
	mux := http.NewServeMux()
	path, handler := providersv1connect.NewSchedulingServiceHandler(sched)
	mux.Handle(path, handler)
	srv := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	srv.EnableHTTP2 = true
	srv.Start()
	defer srv.Close()

	// h2c client over plain HTTP for the bidi stream.
	client := providersv1connect.NewSchedulingServiceClient(
		newH2CTestClient(),
		srv.URL,
		connect.WithGRPC(),
	)
	stream := client.StreamHeartbeats(ctx)
	hb := &providersv1.Heartbeat{
		ProviderId: &commonv1.UUID{Value: p.ID},
		State:      providersv1.SchedulerState_SCHEDULER_STATE_ACTIVE,
		Sequence:   1,
	}
	if err := stream.Send(hb); err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := stream.Receive(); err != nil {
		t.Fatalf("recv ack: %v", err)
	}
	_ = stream.CloseRequest()
	_ = stream.CloseResponse()

	after, err := s.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if !after.LastSeenAt.After(before.LastSeenAt) {
		t.Fatalf("LastSeenAt did not advance: before=%v after=%v",
			before.LastSeenAt, after.LastSeenAt)
	}
	if time.Since(after.LastSeenAt) > 30*time.Second {
		t.Fatalf("LastSeenAt not recent: %v ago", time.Since(after.LastSeenAt))
	}
}

// newH2CTestClient returns an *http.Client that speaks h2c (HTTP/2
// cleartext) — required for streaming Connect-RPC over the httptest
// server's plain HTTP listener.
func newH2CTestClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			// h2c: ignore the TLS config, dial plain TCP. This mirrors
			// the pattern recommended by the Connect-Go docs for
			// in-process bidi-streaming tests.
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}

func TestGetCurrentState_NoHeartbeatYet(t *testing.T) {
	h := NewSchedulingHandler(store.NewInMemory(), nil)
	resp, err := h.GetCurrentState(context.Background(), connect.NewRequest(&providersv1.GetCurrentStateRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.Msg.State != providersv1.SchedulerState_SCHEDULER_STATE_ACTIVE {
		t.Fatalf("expected ACTIVE default, got %s", resp.Msg.State)
	}
}
