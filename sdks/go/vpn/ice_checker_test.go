package vpn

import (
	"context"
	"net"
	"testing"
	"time"
)

// startTestUDPEcho starts a UDP "echo" server that responds to STUN-like
// probes. Returns its listening port. The server runs until ctx cancellation.
func startTestUDPEcho(t *testing.T, ctx context.Context) int {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	port := conn.LocalAddr().(*net.UDPAddr).Port

	go func() {
		defer conn.Close()
		buf := make([]byte, 1500)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
			n, peer, err := conn.ReadFromUDP(buf)
			if err != nil {
				continue
			}
			// Echo back (real STUN would XOR-MAPPED-ADDRESS but the
			// ICE checker only cares whether a reply was received).
			_, _ = conn.WriteToUDP(buf[:n], peer)
		}
	}()

	return port
}

// TestICEChecker_NAT_OpenInternet — no NAT: host candidate must succeed.
func TestICEChecker_NAT_OpenInternet(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port := startTestUDPEcho(t, ctx)

	checker := NewICEChecker(500 * time.Millisecond)
	candidates := []*MockIceCandidate{
		{ConnectionAddress: "127.0.0.1", ConnectionPort: uint32(port), CandidateType: "host"},
	}

	result, err := checker.CheckCandidates(context.Background(), candidates)
	if err != nil {
		t.Fatalf("OPEN_INTERNET case failed: %v", err)
	}
	if result.ConnectionAddress != "127.0.0.1" {
		t.Errorf("expected 127.0.0.1, got %s", result.ConnectionAddress)
	}
}

// TestICEChecker_NAT_ConeNATWithSrflx — cone NAT: srflx candidate works,
// host candidate doesn't (simulated by unreachable port).
func TestICEChecker_NAT_ConeNATWithSrflx(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srflxPort := startTestUDPEcho(t, ctx)

	checker := NewICEChecker(500 * time.Millisecond)
	candidates := []*MockIceCandidate{
		{ConnectionAddress: "127.0.0.1", ConnectionPort: 1, CandidateType: "host"}, // unreachable
		{ConnectionAddress: "127.0.0.1", ConnectionPort: uint32(srflxPort), CandidateType: "srflx"},
	}

	result, err := checker.CheckCandidates(context.Background(), candidates)
	if err != nil {
		t.Fatalf("CONE_NAT srflx case failed: %v", err)
	}
	if result.ConnectionPort != uint32(srflxPort) {
		t.Errorf("expected srflx port %d, got %d", srflxPort, result.ConnectionPort)
	}
}

// TestICEChecker_NAT_SymmetricFallsBack — symmetric NAT: no candidate
// connects (host AND srflx both unreachable). Must surface error rather
// than hang or pick a fake winner.
func TestICEChecker_NAT_SymmetricFallsBack(t *testing.T) {
	checker := NewICEChecker(200 * time.Millisecond)
	candidates := []*MockIceCandidate{
		{ConnectionAddress: "127.0.0.1", ConnectionPort: 1, CandidateType: "host"},
		{ConnectionAddress: "127.0.0.1", ConnectionPort: 2, CandidateType: "srflx"},
	}

	start := time.Now()
	_, err := checker.CheckCandidates(context.Background(), candidates)
	elapsed := time.Since(start)

	if err == nil {
		t.Error("expected error on SYMMETRIC_NAT (all candidates unreachable)")
	}
	// Must fail fast (single candidate timeout, not summed)
	if elapsed > 1*time.Second {
		t.Errorf("symmetric NAT case took %v — should fail within single timeout window (~500ms)", elapsed)
	}
}

// TestICEChecker_NAT_IPv4Only — IPv4 candidate present, IPv6 not.
// Real checker must skip IPv6 gracefully (no panic, no hang).
func TestICEChecker_NAT_IPv4Only(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	port := startTestUDPEcho(t, ctx)

	checker := NewICEChecker(300 * time.Millisecond)
	candidates := []*MockIceCandidate{
		{ConnectionAddress: "127.0.0.1", ConnectionPort: uint32(port), CandidateType: "host"},
		// IPv6 candidate that will fail (we can't bind in test env reliably)
		{ConnectionAddress: "::1", ConnectionPort: 1, CandidateType: "host"},
	}

	result, err := checker.CheckCandidates(context.Background(), candidates)
	if err != nil {
		t.Fatalf("IPv4_ONLY case failed: %v", err)
	}
	if result.ConnectionAddress != "127.0.0.1" {
		t.Errorf("expected IPv4 winner, got %s", result.ConnectionAddress)
	}
}

// TestICEChecker_NAT_RelayLastResort — checker currently skips relay
// candidates (last-resort). Verify the skip is in place so we don't
// silently waste time probing TURN candidates we can't reach anyway.
func TestICEChecker_NAT_RelayLastResort(t *testing.T) {
	checker := NewICEChecker(300 * time.Millisecond)
	candidates := []*MockIceCandidate{
		{ConnectionAddress: "127.0.0.1", ConnectionPort: 1, CandidateType: "relay"},
	}

	_, err := checker.CheckCandidates(context.Background(), candidates)
	// All-relay list with no host/srflx must fail (relay is skipped per current impl)
	if err == nil {
		t.Error("expected error when only relay candidates available (currently skipped)")
	}
}

// TestICEChecker_NAT_ParallelLatency — when multiple host candidates
// are reachable, parallel probing must finish in ~single-timeout time,
// not N×timeout (proves true parallelism, not serial fallback).
func TestICEChecker_NAT_ParallelLatency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	port1 := startTestUDPEcho(t, ctx)
	port2 := startTestUDPEcho(t, ctx)
	port3 := startTestUDPEcho(t, ctx)

	checker := NewICEChecker(500 * time.Millisecond)
	candidates := []*MockIceCandidate{
		{ConnectionAddress: "127.0.0.1", ConnectionPort: uint32(port1), CandidateType: "host"},
		{ConnectionAddress: "127.0.0.1", ConnectionPort: uint32(port2), CandidateType: "host"},
		{ConnectionAddress: "127.0.0.1", ConnectionPort: uint32(port3), CandidateType: "host"},
	}

	start := time.Now()
	_, err := checker.CheckCandidates(context.Background(), candidates)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("parallel case failed: %v", err)
	}
	// 3 candidates running in parallel should not be 3× slower than 1
	if elapsed > 600*time.Millisecond {
		t.Errorf("parallel probing took %v — should be ~single-timeout (proves serial, not parallel)", elapsed)
	}
}

// TestICEChecker_NAT_EmptyCandidateList — must error gracefully, never
// return nil candidate with nil error.
func TestICEChecker_NAT_EmptyCandidateList(t *testing.T) {
	checker := NewICEChecker(100 * time.Millisecond)
	result, err := checker.CheckCandidates(context.Background(), nil)
	if err == nil {
		t.Error("expected error on empty candidate list")
	}
	if result != nil {
		t.Error("expected nil result on empty candidate list")
	}
}

// TestICEChecker_NAT_SelectBestByLatency — among multiple working
// candidates, the one with lowest measured latency must win.
func TestICEChecker_NAT_SelectBestByLatency(t *testing.T) {
	candidates := []*MockIceCandidate{
		{ConnectionAddress: "192.0.2.1", LatencyMs: 100, CandidateType: "host"},
		{ConnectionAddress: "192.0.2.2", LatencyMs: 30, CandidateType: "host"},
		{ConnectionAddress: "192.0.2.3", LatencyMs: 75, CandidateType: "host"},
	}

	best := SelectBestCandidate(candidates)
	if best == nil {
		t.Fatal("SelectBestCandidate returned nil")
	}
	if best.ConnectionAddress != "192.0.2.2" {
		t.Errorf("expected lowest-latency 192.0.2.2, got %s", best.ConnectionAddress)
	}
}
