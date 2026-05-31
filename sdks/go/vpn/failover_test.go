package vpn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFailoverDetector_StartStop(t *testing.T) {
	// Build a client with a mock tunnel manager so we can Stop cleanly
	mock := NewMockTunnelManager()
	client := &BastionClient{
		tunnelMgr:  mock,
		iceChecker: NewICEChecker(1 * time.Second),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	d := NewFailoverDetector(client)
	d.HealthInterval = 50 * time.Millisecond

	d.Start(context.Background())
	time.Sleep(150 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		d.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() did not return within 1s")
	}
}

func TestFailoverDetector_HTTPRoundtrip(t *testing.T) {
	// Coordinator responds with a synthetic alternate provider
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/failover") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "bad path", 400)
			return
		}
		// Verify request body
		var reqBody map[string]string
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if reqBody["failure_reason"] == "" {
			t.Error("expected failure_reason in body")
		}

		resp := FailoverResponse{
			Status:        "failover_complete",
			SessionID:     "test-session",
			OldProviderID: "old-prov",
			NewProviderID: "new-prov",
			ICECandidates: []ICECandidate{
				{ConnectionAddress: "192.0.2.50", ConnectionPort: 51820, CandidateType: "srflx", LatencyMs: 40},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := &BastionClient{
		coordinatorAddr: server.URL,
		sessionID:       "test-session",
		httpClient:      &http.Client{Timeout: 5 * time.Second},
	}

	resp, err := client.callFailoverEndpoint(context.Background(), "endpoint_unreachable")
	if err != nil {
		t.Fatalf("callFailoverEndpoint: %v", err)
	}
	if resp.NewProviderID != "new-prov" {
		t.Errorf("NewProviderID = %q, want new-prov", resp.NewProviderID)
	}
	if len(resp.ICECandidates) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(resp.ICECandidates))
	}
	if resp.ICECandidates[0].ConnectionAddress != "192.0.2.50" {
		t.Errorf("ConnectionAddress = %q, want 192.0.2.50", resp.ICECandidates[0].ConnectionAddress)
	}
}

func TestFailoverDetector_HTTPError(t *testing.T) {
	// Coordinator returns 503 (no alternate available)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no alternate provider", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := &BastionClient{
		coordinatorAddr: server.URL,
		sessionID:       "test-session",
		httpClient:      &http.Client{Timeout: 5 * time.Second},
	}

	_, err := client.callFailoverEndpoint(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error on 503 response, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("expected error to mention status 503, got %v", err)
	}
}

func TestFailoverDetector_DetectsRxBytesStall(t *testing.T) {
	mock := NewMockTunnelManager()
	_, _ = mock.CreateInterface(context.Background(), "wg-test")
	_ = mock.AddPeer(context.Background(), "wg-test", WireGuardPeer{PublicKey: "test-pubkey"})
	mock.PeerStatsSeed = MockPeerStats{
		"wg-test/test-pubkey": {RxBytes: 10000},
	}
	client := &BastionClient{
		tunnelMgr:        mock,
		ifName:           "wg-test",
		providerWgPubKey: "test-pubkey",
	}
	d := NewFailoverDetector(client)
	d.lastByteCount = 10000 // baseline matches current → flat-line

	if !d.detectUnreachable() {
		t.Error("expected detectUnreachable=true when RxBytes flat-lined at 10000")
	}
}

func TestFailoverDetector_HealthyWhenRxBytesGrowing(t *testing.T) {
	mock := NewMockTunnelManager()
	_, _ = mock.CreateInterface(context.Background(), "wg-test")
	_ = mock.AddPeer(context.Background(), "wg-test", WireGuardPeer{PublicKey: "test-pubkey"})
	mock.PeerStatsSeed = MockPeerStats{
		"wg-test/test-pubkey": {RxBytes: 20000},
	}
	client := &BastionClient{
		tunnelMgr:        mock,
		ifName:           "wg-test",
		providerWgPubKey: "test-pubkey",
	}
	d := NewFailoverDetector(client)
	d.lastByteCount = 10000

	if d.detectUnreachable() {
		t.Error("healthy when RxBytes grew 10000 → 20000")
	}
	if d.lastByteCount != 20000 {
		t.Errorf("lastByteCount = %d, want 20000", d.lastByteCount)
	}
}

func TestFailoverDetector_FreshTunnelNoBaseline(t *testing.T) {
	mock := NewMockTunnelManager()
	_, _ = mock.CreateInterface(context.Background(), "wg-test")
	_ = mock.AddPeer(context.Background(), "wg-test", WireGuardPeer{PublicKey: "test-pubkey"})
	client := &BastionClient{
		tunnelMgr:        mock,
		ifName:           "wg-test",
		providerWgPubKey: "test-pubkey",
	}
	d := NewFailoverDetector(client)
	if d.detectUnreachable() {
		t.Error("fresh tunnel (0/0) must not be flagged on first poll")
	}
}

func TestFailoverDetector_StrikesAccumulate(t *testing.T) {
	// No tunnel context → detectUnreachable returns false (insufficient info),
	// so strikes should never accumulate.
	client := &BastionClient{tunnelMgr: NewMockTunnelManager()}
	d := NewFailoverDetector(client)
	d.HealthInterval = 30 * time.Millisecond
	d.StrikesNeeded = 1

	d.Start(context.Background())
	time.Sleep(200 * time.Millisecond)
	d.Stop()

	if d.strikes != 0 {
		t.Errorf("MVP strikes = %d, want 0 (detectUnreachable is stub)", d.strikes)
	}
}
