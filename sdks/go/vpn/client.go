package vpn

import (
	"context"
	"fmt"
	"time"

	pb "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/vpn/v1"
)

// Client represents a customer VPN client that manages a tunnel session.
type Client struct {
	coordinatorAddr string
	customerID      string
	apiKey          string
	iceChecker      *ICEChecker
	tunnelMgr       TunnelManager
}

// TunnelManager abstracts WireGuard tunnel operations (mocked for MVP).
type TunnelManager interface {
	// CreateInterface creates a WireGuard interface and returns its name.
	CreateInterface(ctx context.Context, name string) (string, error)
	// AddPeer configures a WireGuard peer.
	AddPeer(ctx context.Context, ifName string, peer WireGuardPeer) error
	// SetEndpoint updates a peer's endpoint address.
	SetEndpoint(ctx context.Context, ifName string, publicKey string, endpoint string) error
	// BringUp brings the interface up.
	BringUp(ctx context.Context, ifName string) error
	// BringDown brings the interface down.
	BringDown(ctx context.Context, ifName string) error
}

// WireGuardPeer represents a WireGuard peer configuration.
type WireGuardPeer struct {
	PublicKey     string   // Base64-encoded
	AllowedIPs    []string // CIDR ranges
	Endpoint      string   // IP:port
	PresharedKey  string   // Optional (base64-encoded)
}

// NewClient creates a new VPN client.
func NewClient(coordinatorAddr, customerID, apiKey string, tunnelMgr TunnelManager) *Client {
	return &Client{
		coordinatorAddr: coordinatorAddr,
		customerID:      customerID,
		apiKey:          apiKey,
		iceChecker:      NewICEChecker(2 * time.Second),
		tunnelMgr:       tunnelMgr,
	}
}

// EstablishTunnel performs the full VPN session establishment flow:
// 1. Request session from Coordinator
// 2. Perform ICE connectivity checks
// 3. Create WireGuard interface
// 4. Add provider peer
// 5. Confirm working candidate back to Coordinator
func (c *Client) EstablishTunnel(ctx context.Context, region string) (string, error) {
	// Step 1: Request session from Coordinator
	sessionAssign, err := c.requestSession(ctx, region)
	if err != nil {
		return "", fmt.Errorf("request session: %w", err)
	}

	sessionID := sessionAssign.SessionId
	fmt.Printf("[VPN] Session created: %s\n", sessionID)

	// Step 2: Check ICE candidates
	candidates := sessionAssign.Candidates
	if len(candidates) == 0 {
		return "", fmt.Errorf("no candidates provided")
	}

	fmt.Printf("[VPN] Checking %d candidates...\n", len(candidates))
	workingCandidate, err := c.iceChecker.CheckCandidates(ctx, candidates)
	if err != nil {
		return "", fmt.Errorf("ice check: %w", err)
	}
	fmt.Printf("[VPN] Found working candidate: %s:%d (latency: %dms)\n",
		workingCandidate.ConnectionAddress, workingCandidate.ConnectionPort, workingCandidate.LatencyMs)

	// Step 3: Create WireGuard interface
	ifName, err := c.tunnelMgr.CreateInterface(ctx, "wg-iogrid0")
	if err != nil {
		return "", fmt.Errorf("create interface: %w", err)
	}
	fmt.Printf("[VPN] Created WireGuard interface: %s\n", ifName)

	// Step 4: Add provider peer
	providerPeer := WireGuardPeer{
		PublicKey:  sessionAssign.ProviderWgPublicKey,
		AllowedIPs: []string{"0.0.0.0/0"}, // Route all traffic through provider
		Endpoint:   fmt.Sprintf("%s:%d", workingCandidate.ConnectionAddress, workingCandidate.ConnectionPort),
	}
	if err := c.tunnelMgr.AddPeer(ctx, ifName, providerPeer); err != nil {
		return "", fmt.Errorf("add peer: %w", err)
	}
	fmt.Printf("[VPN] Added provider peer with endpoint: %s\n", providerPeer.Endpoint)

	// Step 5: Bring up interface
	if err := c.tunnelMgr.BringUp(ctx, ifName); err != nil {
		return "", fmt.Errorf("bring up interface: %w", err)
	}
	fmt.Printf("[VPN] Interface %s is up\n", ifName)

	// Step 6: Confirm working candidate to Coordinator
	if err := c.confirmCandidate(ctx, sessionID, sessionAssign.ProviderId, workingCandidate); err != nil {
		return "", fmt.Errorf("confirm candidate: %w", err)
	}
	fmt.Printf("[VPN] Confirmed working candidate to Coordinator\n")

	return sessionID, nil
}

// requestSession sends a RequestVpnSession to the Coordinator.
// (Stub implementation — actual RPC would go here)
func (c *Client) requestSession(ctx context.Context, region string) (*pb.VpnSessionAssignment, error) {
	// TODO: Implement gRPC call to vpn-svc
	// POST /v1/vpn/sessions {customer_id, region, api_key_hash}
	return nil, fmt.Errorf("not implemented")
}

// confirmCandidate sends a ConfirmWorkingCandidate to the Coordinator.
// (Stub implementation — actual RPC would go here)
func (c *Client) confirmCandidate(ctx context.Context, sessionID, providerID string, candidate *pb.IceCandidate) error {
	// TODO: Implement gRPC call to vpn-svc
	// PUT /v1/vpn/sessions/{sessionID}/confirm {chosen_candidate, customer_wg_public_key}
	return nil
}

// RefreshSession sends a periodic session refresh (heartbeat + metrics).
// (Stub implementation)
func (c *Client) RefreshSession(ctx context.Context, sessionID string, bytesIn, bytesOut uint64) error {
	// TODO: Implement gRPC call to vpn-svc
	// POST /v1/vpn/sessions/{sessionID}/refresh {bytes_in, bytes_out, roaming_events, failover_count}
	return nil
}

// TerminateSession closes a VPN session.
// (Stub implementation)
func (c *Client) TerminateSession(ctx context.Context, sessionID string, reason string) error {
	// TODO: Implement gRPC call to vpn-svc
	// POST /v1/vpn/sessions/{sessionID}/terminate {reason}
	return nil
}
