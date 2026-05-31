package vpn

import (
	"context"
	"fmt"
	"net"
	"time"
)

// ICEChecker performs RFC 8445 ICE connectivity checks on provider candidates.
type ICEChecker struct {
	timeout time.Duration // per-candidate timeout
}

// NewICEChecker creates a new ICE connectivity checker.
func NewICEChecker(timeout time.Duration) *ICEChecker {
	if timeout == 0 {
		timeout = 2 * time.Second // RFC 8445 default
	}
	return &ICEChecker{timeout: timeout}
}

// CheckCandidates performs connectivity checks on all provided ICE candidates
// and returns the first one that responds, or an error if all fail.
func (c *ICEChecker) CheckCandidates(ctx context.Context, candidates []*MockIceCandidate) (*MockIceCandidate, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidates to check")
	}

	// Check all candidates in parallel
	type result struct {
		candidate *MockIceCandidate
		latency   uint32
		err       error
	}
	results := make(chan result, len(candidates))

	for _, cand := range candidates {
		go func(candidate *MockIceCandidate) {
			latency, err := c.checkCandidate(ctx, candidate)
			results <- result{
				candidate: candidate,
				latency:   latency,
				err:       err,
			}
		}(cand)
	}

	// Collect results
	var best *MockIceCandidate
	var bestLatency uint32 = ^uint32(0)
	var lastErr error

	for i := 0; i < len(candidates); i++ {
		res := <-results
		if res.err != nil {
			lastErr = res.err
			continue
		}
		// Found a working candidate
		if res.latency < bestLatency {
			best = res.candidate
			bestLatency = res.latency
		}
	}

	if best == nil {
		return nil, fmt.Errorf("all candidates failed: %w", lastErr)
	}

	best.LatencyMs = bestLatency
	return best, nil
}

// checkCandidate performs a single connectivity check to a candidate endpoint.
// Returns latency in ms if successful, or error if unreachable.
func (c *ICEChecker) checkCandidate(ctx context.Context, candidate *MockIceCandidate) (uint32, error) {
	if candidate.CandidateType != "host" && candidate.CandidateType != "srflx" {
		// Relay candidates are last-resort; skip for now
		return 0, fmt.Errorf("skipping %s candidate", candidate.CandidateType)
	}

	endpoint := fmt.Sprintf("%s:%d", candidate.ConnectionAddress, candidate.ConnectionPort)

	// Create a UDP socket and send a STUN BINDING REQUEST
	// (simplified: we'll use UDP echo as a connectivity probe for MVP)
	conn, err := net.DialTimeout("udp", endpoint, c.timeout)
	if err != nil {
		return 0, fmt.Errorf("dial %s: %w", endpoint, err)
	}
	defer conn.Close()

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(c.timeout))

	// Send a simple STUN BINDING REQUEST
	// (RFC 5389 format: type=0x0001, length=0, magic cookie, transaction ID)
	stunReq := []byte{
		0x00, 0x01, // STUN BINDING REQUEST
		0x00, 0x00, // Message length = 0
		0x21, 0x12, 0xa4, 0x42, // Magic cookie
		0x00, 0x00, 0x00, 0x00, // Transaction ID (first 4 bytes)
		0x00, 0x00, 0x00, 0x00, // Transaction ID (next 4 bytes)
		0x00, 0x00, 0x00, 0x00, // Transaction ID (last 4 bytes)
	}

	start := time.Now()
	_, err = conn.Write(stunReq)
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", endpoint, err)
	}

	// Wait for response
	resp := make([]byte, 128)
	_, err = conn.Read(resp)
	latency := time.Since(start)

	if err != nil {
		return 0, fmt.Errorf("read %s: %w", endpoint, err)
	}

	return uint32(latency.Milliseconds()), nil
}

// SelectBestCandidate selects the candidate with lowest latency.
// Returns nil if no candidates have latency measurements.
func SelectBestCandidate(candidates []*MockIceCandidate) *MockIceCandidate {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for _, cand := range candidates[1:] {
		if cand.LatencyMs > 0 && cand.LatencyMs < best.LatencyMs {
			best = cand
		}
	}
	return best
}
