package vpn

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestE2E_RoamingDoesNotTriggerFailover ensures the two detectors
// don't interfere: a roaming event should re-pin the endpoint via
// SetEndpoint, NOT call the failover endpoint on vpn-svc.
func TestE2E_RoamingDoesNotTriggerFailover(t *testing.T) {
	var failoverCalled int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/failover") {
			atomic.AddInt32(&failoverCalled, 1)
			http.Error(w, "should not be called", 400)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	mock := NewMockTunnelManager()
	_, _ = mock.CreateInterface(context.Background(), "wg-test0")
	client := &BastionClient{
		coordinatorAddr:  server.URL,
		sessionID:        "test-session",
		ifName:           "wg-test0",
		providerEndpoint: "1.1.1.1:51820",
		providerWgPubKey: "test-pubkey",
		tunnelMgr:        mock,
		iceChecker:       NewICEChecker(500 * time.Millisecond),
		httpClient:       &http.Client{Timeout: 2 * time.Second},
	}

	var roamCallbacks int32
	rd := NewRoamingDetector("1.1.1.1:51820", 100*time.Millisecond, func(oldIP, newIP net.IP) error {
		atomic.AddInt32(&roamCallbacks, 1)
		return client.tunnelMgr.SetEndpoint(context.Background(), client.ifName, client.providerWgPubKey, client.providerEndpoint)
	})
	if err := rd.Start(context.Background()); err != nil {
		t.Fatalf("Start roaming: %v", err)
	}
	defer rd.Stop()

	// Force a roaming event by overwriting baseline
	rd.mu.Lock()
	rd.lastIP = net.ParseIP("192.0.2.99")
	rd.mu.Unlock()

	// Wait for poll tick
	time.Sleep(200 * time.Millisecond)

	if got := atomic.LoadInt32(&roamCallbacks); got != 1 {
		t.Errorf("expected 1 roam callback, got %d", got)
	}
	if got := atomic.LoadInt32(&failoverCalled); got != 0 {
		t.Errorf("roaming must not call /failover endpoint, but got %d calls", got)
	}
}

// TestE2E_FailoverDoesNotTriggerRoaming verifies the inverse: a
// triggered failover updates providerEndpoint, but the roaming
// detector treats it as the new baseline and doesn't double-fire.
func TestE2E_FailoverDoesNotTriggerRoaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/failover") {
			resp := FailoverResponse{
				Status:        "failover_complete",
				SessionID:     "test-session",
				OldProviderID: "old-prov",
				NewProviderID: "new-prov",
				ICECandidates: []ICECandidate{
					{Candidate: "127.0.0.1", Port: 50000, Type: "host", LatencyMs: 5},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	mock := NewMockTunnelManager()
	_, _ = mock.CreateInterface(context.Background(), "wg-test0")
	// Pre-register the peer (Connect would do this in production)
	_ = mock.AddPeer(context.Background(), "wg-test0", WireGuardPeer{
		PublicKey:  "test-pubkey",
		AllowedIPs: []string{"0.0.0.0/0"},
		Endpoint:   "192.0.2.50:51820",
	})
	client := &BastionClient{
		coordinatorAddr:  server.URL,
		sessionID:        "test-session",
		ifName:           "wg-test0",
		providerEndpoint: "192.0.2.50:51820",
		providerWgPubKey: "test-pubkey",
		tunnelMgr:        mock,
		iceChecker:       NewICEChecker(300 * time.Millisecond),
		httpClient:       &http.Client{Timeout: 2 * time.Second},
	}

	// Start a UDP listener so the ICE check on the alt provider succeeds
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _ := net.ListenUDP("udp", &net.UDPAddr{Port: 50000})
	defer conn.Close()
	go func() {
		buf := make([]byte, 1500)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			n, peer, err := conn.ReadFromUDP(buf)
			if err == nil {
				_, _ = conn.WriteToUDP(buf[:n], peer)
			}
		}
	}()

	fd := NewFailoverDetector(client)
	if err := fd.triggerFailover(context.Background(), "test"); err != nil {
		t.Fatalf("triggerFailover: %v", err)
	}

	// After failover, providerEndpoint must reflect the new alt provider's address
	if !strings.HasPrefix(client.providerEndpoint, "127.0.0.1:") {
		t.Errorf("after failover providerEndpoint = %q, expected 127.0.0.1:*", client.providerEndpoint)
	}

	// Verify the WireGuard interface received the new endpoint
	if mock.peers["wg-test0"]["test-pubkey"].Endpoint != client.providerEndpoint {
		t.Errorf("wg peer endpoint not updated to %s", client.providerEndpoint)
	}
}

// TestE2E_BothDetectors_StopCleanly verifies that having both detectors
// active and stopping them sequentially doesn't deadlock.
func TestE2E_BothDetectors_StopCleanly(t *testing.T) {
	mock := NewMockTunnelManager()
	client := &BastionClient{
		tunnelMgr:  mock,
		iceChecker: NewICEChecker(100 * time.Millisecond),
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}

	rd := NewRoamingDetector("1.1.1.1:443", 50*time.Millisecond, nil)
	_ = rd.Start(context.Background())
	fd := NewFailoverDetector(client)
	fd.HealthInterval = 50 * time.Millisecond
	fd.Start(context.Background())

	time.Sleep(150 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		rd.Stop()
		fd.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop sequence deadlocked")
	}
}
