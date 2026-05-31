package vpn

import (
	"context"
	"fmt"
	"time"
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
	// PeerStats returns liveness counters for a peer. Used by
	// FailoverDetector to detect a dead tunnel (no inbound bytes
	// in the health window → trigger failover). Returning
	// ErrNotImplemented from a mock or unsupported platform is fine —
	// the detector falls back to its time-based heuristic.
	PeerStats(ctx context.Context, ifName, publicKey string) (PeerStats, error)
}

// PeerStats is a snapshot of WireGuard peer counters at one point in time.
type PeerStats struct {
	RxBytes           uint64 // bytes received from peer
	TxBytes           uint64 // bytes sent to peer
	LastHandshakeUnix int64  // unix seconds of last successful handshake; 0 if never
}

// ErrNotImplemented is returned by TunnelManager methods on platforms
// or implementations where the underlying primitive isn't available.
// Callers should treat it as "unknown" rather than "failure".
var ErrNotImplemented = fmt.Errorf("not implemented on this platform")

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

// EstablishTunnel is deprecated — use BastionClient for full implementation.
// This is kept for backward compatibility but returns an error.
func (c *Client) EstablishTunnel(ctx context.Context, region string) (string, error) {
	return "", fmt.Errorf("EstablishTunnel is deprecated - use BastionClient instead")
}

// requestSession sends a RequestVpnSession to the Coordinator.
// (Stub implementation — use BastionClient instead for full implementation)
func (c *Client) requestSession(ctx context.Context, region string) (string, error) {
	// TODO: Implement HTTP call to vpn-svc
	// POST /v1/vpn/sessions {customer_id, region, api_key_hash}
	return "", fmt.Errorf("not implemented - use BastionClient instead")
}

// confirmCandidate sends a ConfirmWorkingCandidate to the Coordinator.
// (Stub implementation — use BastionClient instead for full implementation)
func (c *Client) confirmCandidate(ctx context.Context, sessionID, providerID string, candidateAddr string) error {
	// TODO: Implement HTTP call to vpn-svc
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
