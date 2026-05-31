package vpn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FailoverResponse is what vpn-svc returns from TriggerFailover.
type FailoverResponse struct {
	Status        string             `json:"status"`
	SessionID     string             `json:"session_id"`
	OldProviderID string             `json:"old_provider_id"`
	NewProviderID string             `json:"new_provider_id"`
	ICECandidates []ICECandidate     `json:"ice_candidates"`
}

// FailoverDetector watches the active tunnel for liveness and, if the
// provider becomes unreachable, asks the Coordinator for an alternate
// provider in the same region and re-establishes the tunnel.
//
// "Unreachable" = N consecutive WireGuard keepalive failures (or no
// inbound bytes for HealthTimeout). Default: 3 strikes over 6s.
type FailoverDetector struct {
	HealthInterval time.Duration
	HealthTimeout  time.Duration
	StrikesNeeded  int

	client          *BastionClient
	strikes         int
	lastByteCount   uint64
	stopCh          chan struct{}
	stoppedCh       chan struct{}
}

// NewFailoverDetector wires a failover detector against an active
// BastionClient. The client must already have a tunnel established
// (Connect() succeeded) before calling Start().
func NewFailoverDetector(client *BastionClient) *FailoverDetector {
	return &FailoverDetector{
		HealthInterval: 2 * time.Second,
		HealthTimeout:  6 * time.Second,
		StrikesNeeded:  3,
		client:         client,
		stopCh:         make(chan struct{}),
		stoppedCh:      make(chan struct{}),
	}
}

// Start begins the failover detection loop. Non-blocking.
func (f *FailoverDetector) Start(ctx context.Context) {
	go f.loop(ctx)
}

// Stop terminates the detection loop and waits for it to finish.
func (f *FailoverDetector) Stop() {
	close(f.stopCh)
	<-f.stoppedCh
}

func (f *FailoverDetector) loop(ctx context.Context) {
	defer close(f.stoppedCh)
	ticker := time.NewTicker(f.HealthInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-f.stopCh:
			return
		case <-ticker.C:
			if f.detectUnreachable() {
				f.strikes++
				fmt.Printf("[FAILOVER] provider unreachable, strike %d/%d\n", f.strikes, f.StrikesNeeded)
				if f.strikes >= f.StrikesNeeded {
					if err := f.triggerFailover(ctx, "endpoint_unreachable"); err != nil {
						fmt.Printf("[FAILOVER] trigger failed: %v — will retry next tick\n", err)
					} else {
						f.strikes = 0
					}
				}
			} else {
				f.strikes = 0
			}
		}
	}
}

// detectUnreachable returns true if the WireGuard tunnel appears dead.
// Reads the peer's RxBytes via TunnelManager.PeerStats — if RxBytes
// hasn't increased since the last check (i.e., no inbound traffic
// from the provider), the tunnel is suspect. After StrikesNeeded
// consecutive unhealthy checks the loop triggers failover.
//
// Returns false (healthy) when PeerStats returns ErrNotImplemented —
// the platform doesn't support live counters so we trust the SDK's
// time-based fallback and roaming detection instead.
func (f *FailoverDetector) detectUnreachable() bool {
	if f.client == nil || f.client.tunnelMgr == nil || f.client.ifName == "" || f.client.providerWgPubKey == "" {
		return false
	}
	stats, err := f.client.tunnelMgr.PeerStats(context.Background(), f.client.ifName, f.client.providerWgPubKey)
	if err != nil {
		return false
	}
	if stats.RxBytes > f.lastByteCount {
		f.lastByteCount = stats.RxBytes
		return false
	}
	// Brand-new tunnel that hasn't received its first packet yet — defer
	// judgment until we have a baseline rather than fire spurious failover.
	if f.lastByteCount == 0 && stats.RxBytes == 0 {
		return false
	}
	return true
}

// triggerFailover calls vpn-svc to switch sessions to an alternate
// provider and re-establishes the WireGuard endpoint on the existing
// interface. <2s wall-clock target per VPN DoD.
func (f *FailoverDetector) triggerFailover(ctx context.Context, reason string) error {
	start := time.Now()
	fmt.Printf("[FAILOVER] triggering failover for session %s (reason: %s)\n", f.client.sessionID, reason)

	resp, err := f.client.callFailoverEndpoint(ctx, reason)
	if err != nil {
		return fmt.Errorf("call coordinator: %w", err)
	}

	fmt.Printf("[FAILOVER] coordinator picked alternate provider: %s\n", resp.NewProviderID)

	// Pick best candidate from alt provider's list
	mockCandidates := make([]*MockIceCandidate, 0, len(resp.ICECandidates))
	for _, c := range resp.ICECandidates {
		mockCandidates = append(mockCandidates, &MockIceCandidate{
			ConnectionAddress: c.ConnectionAddress,
			ConnectionPort:    c.ConnectionPort,
			CandidateType:     c.CandidateType,
			LatencyMs:         c.LatencyMs,
		})
	}
	if len(mockCandidates) == 0 {
		return fmt.Errorf("alternate provider returned 0 candidates")
	}

	// Pick best candidate by RFC 8445 type preference (host > srflx >
	// prflx > relay) — same approach as Connect(). The STUN-probe check
	// removed per #554: WG endpoints don't speak STUN so the probe
	// always failed; if the picked candidate doesn't work, the WG
	// handshake itself will trip a re-failover.
	workingCandidate := mockCandidates[0]
	for _, cand := range mockCandidates {
		if candidatePriority(cand.CandidateType) > candidatePriority(workingCandidate.CandidateType) {
			workingCandidate = cand
		}
	}

	// Re-pin WireGuard endpoint
	newEndpoint := fmt.Sprintf("%s:%d", workingCandidate.ConnectionAddress, workingCandidate.ConnectionPort)
	if err := f.client.tunnelMgr.SetEndpoint(ctx, f.client.ifName, f.client.providerWgPubKey, newEndpoint); err != nil {
		return fmt.Errorf("set new endpoint: %w", err)
	}
	f.client.providerEndpoint = newEndpoint

	elapsed := time.Since(start)
	fmt.Printf("[FAILOVER] ✓ failover complete in %v (DoD: <2s)\n", elapsed)
	return nil
}

// callFailoverEndpoint posts to POST /v1/vpn/sessions/{id}/failover on vpn-svc.
func (c *BastionClient) callFailoverEndpoint(ctx context.Context, reason string) (*FailoverResponse, error) {
	body, _ := json.Marshal(map[string]string{"failure_reason": reason})
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.coordinatorAddr+"/v1/vpn/sessions/"+c.sessionID+"/failover",
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var fr FailoverResponse
	if err := json.NewDecoder(resp.Body).Decode(&fr); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &fr, nil
}
