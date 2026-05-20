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

func TestListAuditEvents_FilterByKind(t *testing.T) {
	s := store.NewInMemory()
	ctx := context.Background()
	_ = s.AppendAuditEvent(ctx, store.AuditEvent{ProviderID: "p1", Kind: "EVENT_KIND_WORKLOAD_DISPATCHED"})
	_ = s.AppendAuditEvent(ctx, store.AuditEvent{ProviderID: "p1", Kind: "EVENT_KIND_WORKLOAD_BLOCKED"})
	_ = s.AppendAuditEvent(ctx, store.AuditEvent{ProviderID: "p1", Kind: "EVENT_KIND_WORKLOAD_DISPATCHED"})

	h := NewDashboardHandler(s, nil)
	resp, err := h.ListAuditEvents(ctx, connect.NewRequest(&providersv1.ListAuditEventsRequest{
		ProviderId:  &commonv1.UUID{Value: "p1"},
		KindFilter:  []providersv1.EventKind{providersv1.EventKind_EVENT_KIND_WORKLOAD_DISPATCHED},
	}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Msg.Events) != 2 {
		t.Fatalf("expected 2 dispatched events, got %d", len(resp.Msg.Events))
	}
}

// TestGetEarningsSummary_Empty is the regression test for #312: when a
// caller's provider has zero earnings_entries (the Phase-0 zero-workload
// state), the headline Money must carry currency="GRID" (not "USD") so
// the web layer renders "0 $GRID" — not "$0.00" and not "—" (which is
// what proto3 zero-omission + Intl.NumberFormat("USD") would produce).
func TestGetEarningsSummary_Empty(t *testing.T) {
	s := store.NewInMemory()
	h := NewDashboardHandler(s, nil)
	resp, err := h.GetEarningsSummary(context.Background(), connect.NewRequest(&providersv1.GetEarningsSummaryRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.Msg.Summary.TotalEarned == nil {
		t.Fatal("TotalEarned must be set even on empty state — frontend reads currencyCode off it")
	}
	if resp.Msg.Summary.TotalEarned.Micros != 0 {
		t.Fatalf("expected 0 micros")
	}
	if resp.Msg.Summary.TotalEarned.Currency != "GRID" {
		t.Fatalf("expected GRID default (Phase-0 native ledger currency), got %q", resp.Msg.Summary.TotalEarned.Currency)
	}
}

func TestGetEarningsSummary_Breakdown(t *testing.T) {
	s := store.NewInMemory()
	ctx := context.Background()
	now := time.Now()
	_ = s.CreditEarnings(ctx, store.EarningsEntry{ProviderID: "p1", WorkloadType: "bandwidth", OccurredAt: now, Currency: "GRID", Micros: 100})
	_ = s.CreditEarnings(ctx, store.EarningsEntry{ProviderID: "p1", WorkloadType: "docker", OccurredAt: now, Currency: "GRID", Micros: 250})

	h := NewDashboardHandler(s, nil)
	resp, err := h.GetEarningsSummary(ctx, connect.NewRequest(&providersv1.GetEarningsSummaryRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.Msg.Summary.TotalEarned.Micros != 350 {
		t.Fatalf("total: got %d want 350", resp.Msg.Summary.TotalEarned.Micros)
	}
	if resp.Msg.Summary.TotalEarned.Currency != "GRID" {
		t.Fatalf("currency: got %q want GRID", resp.Msg.Summary.TotalEarned.Currency)
	}
	if resp.Msg.Summary.ByWorkloadType["bandwidth"].Micros != 100 {
		t.Fatalf("bandwidth: %v", resp.Msg.Summary.ByWorkloadType["bandwidth"])
	}
}

func TestListAuditEvents_MissingProviderID(t *testing.T) {
	h := NewDashboardHandler(store.NewInMemory(), nil)
	_, err := h.ListAuditEvents(context.Background(), connect.NewRequest(&providersv1.ListAuditEventsRequest{}))
	if err == nil {
		t.Fatalf("expected error")
	}
}

// TestStreamAuditEvents_FirstFrameIsKeepalive is the regression test for
// #323. Before the fix, StreamAuditEvents entered a `select { case <-sub
// }` loop without sending anything, so Connect-Go deferred response
// headers until the first real event — for a freshly paired provider
// with zero workloads, that meant the gateway-bff Connect client sat in
// Receive() until its per-call timeout fired, surfacing as
// deadline_exceeded at the BFF and a permanent "Connecting…" status in
// the web /provide/audit feed. Hatice flagged this on the DoD walk for
// EPIC #309.
//
// After the fix, the server emits a KEEPALIVE AuditEvent synchronously
// BEFORE entering the receive loop, so:
//  1. Connect flushes response headers within ms,
//  2. the BFF's stream.Receive() returns true,
//  3. the SSE pipe stays healthy end-to-end.
//
// We drive the live Connect-RPC server over h2c via httptest so we
// exercise the actual ServerStream.Send code path (Connect framing,
// HTTP/2 trailers, the lot) rather than mocking it away.
func TestStreamAuditEvents_FirstFrameIsKeepalive(t *testing.T) {
	t.Parallel()

	s := store.NewInMemory()
	h := NewDashboardHandler(s, nil)

	mux := http.NewServeMux()
	path, handler := providersv1connect.NewDashboardServiceHandler(h)
	mux.Handle(path, handler)
	srv := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	srv.EnableHTTP2 = true
	srv.Start()
	defer srv.Close()

	client := providersv1connect.NewDashboardServiceClient(
		newDashboardH2CTestClient(),
		srv.URL,
		connect.WithGRPC(),
	)

	// Use a short context — the test fails (and bounds runtime) if the
	// first frame doesn't arrive in 1 s. The pre-fix code would block
	// here forever (subject to the test framework's overall timeout).
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	const providerID = "11111111-1111-1111-1111-111111111111"

	stream, err := client.StreamAuditEvents(ctx, connect.NewRequest(&providersv1.StreamAuditEventsRequest{
		ProviderId: &commonv1.UUID{Value: providerID},
	}))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close()

	// First frame MUST arrive within the deadline AND be a KEEPALIVE.
	gotFrame := make(chan *providersv1.AuditEvent, 1)
	gotErr := make(chan error, 1)
	go func() {
		if !stream.Receive() {
			gotErr <- stream.Err()
			return
		}
		gotFrame <- stream.Msg()
	}()

	select {
	case <-time.After(900 * time.Millisecond):
		t.Fatalf("first frame did not arrive within 900ms — bug #323 regression: StreamAuditEvents is not flushing initial headers")
	case err := <-gotErr:
		t.Fatalf("receive error before first frame: %v", err)
	case ev := <-gotFrame:
		if ev == nil {
			t.Fatal("first frame was nil")
		}
		if ev.GetKind() != providersv1.EventKind_EVENT_KIND_KEEPALIVE {
			t.Fatalf("first frame kind = %v, want EVENT_KIND_KEEPALIVE", ev.GetKind())
		}
		if got := ev.GetProviderId().GetValue(); got != providerID {
			t.Fatalf("first frame provider_id = %q, want %q", got, providerID)
		}
	}
}

// TestStreamAuditEvents_RealEventsStillFlow guards the obvious follow-up
// regression: the keepalive injection MUST NOT swallow real audit events
// appended to the store while the stream is open.
func TestStreamAuditEvents_RealEventsStillFlow(t *testing.T) {
	t.Parallel()

	s := store.NewInMemory()
	h := NewDashboardHandler(s, nil)

	mux := http.NewServeMux()
	path, handler := providersv1connect.NewDashboardServiceHandler(h)
	mux.Handle(path, handler)
	srv := httptest.NewUnstartedServer(h2c.NewHandler(mux, &http2.Server{}))
	srv.EnableHTTP2 = true
	srv.Start()
	defer srv.Close()

	client := providersv1connect.NewDashboardServiceClient(
		newDashboardH2CTestClient(),
		srv.URL,
		connect.WithGRPC(),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	const providerID = "22222222-2222-2222-2222-222222222222"

	stream, err := client.StreamAuditEvents(ctx, connect.NewRequest(&providersv1.StreamAuditEventsRequest{
		ProviderId: &commonv1.UUID{Value: providerID},
	}))
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	defer stream.Close()

	// Drain the initial KEEPALIVE.
	if !stream.Receive() {
		t.Fatalf("expected initial keepalive, got err: %v", stream.Err())
	}
	first := stream.Msg()
	if first.GetKind() != providersv1.EventKind_EVENT_KIND_KEEPALIVE {
		t.Fatalf("first frame kind = %v, want EVENT_KIND_KEEPALIVE", first.GetKind())
	}

	// Append a real workload-dispatched event AFTER the subscription
	// is live, then assert it lands on the stream.
	go func() {
		// Tiny sleep so the goroutine ordering reliably exercises the
		// "subscription already open" path rather than racing with
		// subscribe-then-publish.
		time.Sleep(50 * time.Millisecond)
		_ = s.AppendAuditEvent(context.Background(), store.AuditEvent{
			ID:           "evt-1",
			ProviderID:   providerID,
			Kind:         "EVENT_KIND_WORKLOAD_DISPATCHED",
			OccurredAt:   time.Now().UTC(),
			WorkloadType: "bandwidth",
			Category:     "e_commerce",
		})
	}()

	// Read subsequent frames until we see the dispatched event. We may
	// encounter additional KEEPALIVEs from the 15s ticker if scheduling
	// is unlucky (we shouldn't — 50 ms << 15 s — but be tolerant).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !stream.Receive() {
			t.Fatalf("stream closed before dispatched event arrived: %v", stream.Err())
		}
		ev := stream.Msg()
		if ev.GetKind() == providersv1.EventKind_EVENT_KIND_KEEPALIVE {
			continue
		}
		if ev.GetKind() != providersv1.EventKind_EVENT_KIND_WORKLOAD_DISPATCHED {
			t.Fatalf("unexpected event kind: %v", ev.GetKind())
		}
		if ev.GetId().GetValue() != "evt-1" {
			t.Fatalf("event id = %q, want evt-1", ev.GetId().GetValue())
		}
		return // happy path
	}
	t.Fatal("dispatched audit event never reached the stream")
}

// newDashboardH2CTestClient returns an *http.Client that speaks h2c
// (HTTP/2 cleartext) — required for streaming Connect-RPC over the
// httptest server's plain HTTP listener. Mirrors newH2CTestClient in
// scheduling_test.go; duplicated here to keep the two test files
// independently runnable.
func newDashboardH2CTestClient() *http.Client {
	return &http.Client{
		Transport: &http2.Transport{
			AllowHTTP: true,
			DialTLSContext: func(ctx context.Context, network, addr string, _ *tls.Config) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, network, addr)
			},
		},
	}
}
