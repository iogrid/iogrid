package vpn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"
)

// BastionClient is a ready-to-use VPN client for the bastion machine.
// It combines ICE checking + WireGuard tunnel management with proper error handling.
type BastionClient struct {
	coordinatorAddr string
	customerID      string
	apiKey          string
	tunnelMgr       TunnelManager
	iceChecker      *ICEChecker
	sessionID       string
	ifName          string
}

// NewBastionClient creates a new bastion VPN client.
func NewBastionClient(coordinatorAddr, customerID, apiKey string) *BastionClient {
	// Use mock tunnel manager by default (can be switched to RealTunnelManager for production)
	tunnelMgr := NewMockTunnelManager()
	return &BastionClient{
		coordinatorAddr: coordinatorAddr,
		customerID:      customerID,
		apiKey:          apiKey,
		tunnelMgr:       tunnelMgr,
		iceChecker:      NewICEChecker(2 * time.Second),
	}
}

// Connect establishes a VPN tunnel to a provider in the specified region.
// This is the main entry point for VPN client usage.
func (c *BastionClient) Connect(ctx context.Context, region string) error {
	fmt.Printf("[BASTION] Connecting to VPN in region: %s\n", region)

	// Step 1: Request VPN session from Coordinator
	fmt.Printf("[BASTION] Requesting session from Coordinator...\n")
	sessionID, err := c.requestSessionFromCoordinator(ctx, region)
	if err != nil {
		return fmt.Errorf("request session: %w", err)
	}
	c.sessionID = sessionID
	fmt.Printf("[BASTION] Session created: %s\n", sessionID)

	// Step 2: Create WireGuard interface
	fmt.Printf("[BASTION] Creating WireGuard interface...\n")
	ifName, err := c.tunnelMgr.CreateInterface(ctx, "wg-iogrid0")
	if err != nil {
		return fmt.Errorf("create interface: %w", err)
	}
	c.ifName = ifName

	// Step 3: Get provider info + ICE candidates from Coordinator
	fmt.Printf("[BASTION] Fetching provider info and ICE candidates...\n")
	providerInfo, candidates, err := c.getProviderInfo(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("get provider info: %w", err)
	}
	fmt.Printf("[BASTION] Got %d ICE candidates from provider\n", len(candidates))

	// Step 4: Check ICE connectivity to find working candidate
	fmt.Printf("[BASTION] Performing ICE connectivity checks...\n")
	workingCandidate, err := c.iceChecker.CheckCandidates(ctx, candidates)
	if err != nil {
		return fmt.Errorf("ice check: %w", err)
	}
	fmt.Printf("[BASTION] Found working candidate: %s:%d (latency: %dms)\n",
		workingCandidate.ConnectionAddress, workingCandidate.ConnectionPort, workingCandidate.LatencyMs)

	// Step 5: Configure WireGuard peer
	fmt.Printf("[BASTION] Configuring WireGuard peer...\n")
	endpoint := fmt.Sprintf("%s:%d", workingCandidate.ConnectionAddress, workingCandidate.ConnectionPort)
	peer := WireGuardPeer{
		PublicKey:  providerInfo.ProviderWgPublicKey,
		AllowedIPs: []string{"0.0.0.0/0"},  // Route all traffic through provider
		Endpoint:   endpoint,
	}
	if err := c.tunnelMgr.AddPeer(ctx, ifName, peer); err != nil {
		return fmt.Errorf("add peer: %w", err)
	}

	// Step 6: Bring up interface
	fmt.Printf("[BASTION] Bringing up WireGuard interface...\n")
	if err := c.tunnelMgr.BringUp(ctx, ifName); err != nil {
		return fmt.Errorf("bring up: %w", err)
	}

	// Step 7: Confirm working candidate to Coordinator
	fmt.Printf("[BASTION] Confirming working candidate to Coordinator...\n")
	if err := c.confirmCandidate(ctx, sessionID, workingCandidate); err != nil {
		return fmt.Errorf("confirm candidate: %w", err)
	}

	fmt.Printf("[BASTION] ✓ VPN tunnel established successfully!\n")
	fmt.Printf("[BASTION] Session ID: %s\n", sessionID)
	fmt.Printf("[BASTION] Interface: %s\n", ifName)
	fmt.Printf("[BASTION] Provider: %s\n", providerInfo.ProviderId)
	fmt.Printf("[BASTION] Latency: %dms\n", workingCandidate.LatencyMs)

	return nil
}

// Disconnect closes the VPN tunnel.
func (c *BastionClient) Disconnect(ctx context.Context) error {
	if c.ifName == "" {
		return fmt.Errorf("no active tunnel")
	}

	fmt.Printf("[BASTION] Disconnecting from VPN...\n")
	if err := c.tunnelMgr.BringDown(ctx, c.ifName); err != nil {
		return fmt.Errorf("bring down: %w", err)
	}

	// Notify Coordinator of termination
	if c.sessionID != "" {
		_ = c.terminateSession(ctx, c.sessionID, "user_initiated")
	}

	fmt.Printf("[BASTION] VPN tunnel closed\n")
	c.ifName = ""
	c.sessionID = ""
	return nil
}

// RefreshMetrics sends updated session metrics to Coordinator (called periodically).
func (c *BastionClient) RefreshMetrics(ctx context.Context, bytesIn, bytesOut uint64) error {
	if c.sessionID == "" {
		return fmt.Errorf("no active session")
	}
	return c.refreshSession(ctx, c.sessionID, bytesIn, bytesOut)
}

// Stub methods (to be implemented with actual RPC calls)

// requestSessionFromCoordinator requests a VPN session.
func (c *BastionClient) requestSessionFromCoordinator(ctx context.Context, region string) (string, error) {
	// TODO: Call vpn-svc POST /v1/vpn/sessions with {customer_id, region, api_key_hash}
	// For MVP: generate a mock session ID
	sessionID := generateMockID()
	return sessionID, nil
}

// getProviderInfo fetches provider details and ICE candidates.
func (c *BastionClient) getProviderInfo(ctx context.Context, sessionID string) (
	*ProviderInfo, []*MockIceCandidate, error) {

	// TODO: Call vpn-svc GET /v1/vpn/sessions/{sessionID}
	// For MVP: return mock data
	providerInfo := &ProviderInfo{
		ProviderId:           "provider-" + generateMockID()[:8],
		ProviderWgPublicKey:  generateMockPublicKey(),
	}

	// Generate mock ICE candidates
	candidates := []*MockIceCandidate{
		{
			ConnectionAddress: "192.0.2.1",  // Example provider IP
			ConnectionPort:    51820,
			CandidateType:     "srflx",
			LatencyMs:         45,
		},
		{
			ConnectionAddress: "198.51.100.1",
			ConnectionPort:    51820,
			CandidateType:     "relay",
			LatencyMs:         150,
		},
	}

	return providerInfo, candidates, nil
}

// confirmCandidate notifies Coordinator of the working candidate.
func (c *BastionClient) confirmCandidate(ctx context.Context, sessionID string, candidate *MockIceCandidate) error {
	// TODO: Call vpn-svc PUT /v1/vpn/sessions/{sessionID}/confirm
	fmt.Printf("[BASTION] Confirmed candidate: %s:%d\n", candidate.ConnectionAddress, candidate.ConnectionPort)
	return nil
}

// refreshSession sends metrics to Coordinator.
func (c *BastionClient) refreshSession(ctx context.Context, sessionID string, bytesIn, bytesOut uint64) error {
	// TODO: Call vpn-svc POST /v1/vpn/sessions/{sessionID}/refresh
	return nil
}

// terminateSession closes the session on Coordinator.
func (c *BastionClient) terminateSession(ctx context.Context, sessionID string, reason string) error {
	// TODO: Call vpn-svc POST /v1/vpn/sessions/{sessionID}/terminate
	return nil
}

// Helper types for MVP (will be replaced with proto messages)

type ProviderInfo struct {
	ProviderId          string
	ProviderWgPublicKey string
}

type MockIceCandidate struct {
	ConnectionAddress string
	ConnectionPort    uint32
	CandidateType     string
	LatencyMs         uint32
}

// Helper functions

func generateMockID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.URLEncoding.EncodeToString(b)
}

func generateMockPublicKey() string {
	b := make([]byte, 32)  // WireGuard keys are 32 bytes
	rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}
