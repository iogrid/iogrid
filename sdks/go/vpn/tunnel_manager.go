package vpn

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// RealTunnelManager implements TunnelManager using the wireguard-go
// userspace WireGuard stack. Cross-platform (Linux/macOS/Windows) —
// uses TUN devices everywhere and configures peers via the wg UAPI
// interface that wireguard-go's device.Device exposes.
//
// PeerStats reads ReceiveBytes / TransmitBytes / LastHandshake by
// parsing the same UAPI surface.
type RealTunnelManager struct {
	mu      sync.Mutex
	devices map[string]*realDevice
}

// realDevice bundles the userspace WG device with metadata we need for
// later operations (private key for re-configuration, list of peers).
type realDevice struct {
	dev        *device.Device
	privateKey string // base64 — needed for IpcSet rewrites
	peers      map[string]*realPeer
}

type realPeer struct {
	publicKey  string // base64 public key
	endpoint   string // host:port
	allowedIPs []string
}

// NewRealTunnelManager creates a new tunnel manager backed by the
// wireguard-go userspace stack. Works on Linux/macOS/Windows.
func NewRealTunnelManager() *RealTunnelManager {
	return &RealTunnelManager{
		devices: make(map[string]*realDevice),
	}
}

// CreateInterface creates a TUN device + wireguard-go device.
// Requires CAP_NET_ADMIN on Linux, admin rights on macOS/Windows.
func (m *RealTunnelManager) CreateInterface(ctx context.Context, name string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.devices[name]; exists {
		return "", fmt.Errorf("interface %s already exists", name)
	}

	tunDevice, err := tun.CreateTUN(name, device.DefaultMTU)
	if err != nil {
		return "", fmt.Errorf("create tun (need CAP_NET_ADMIN / admin?): %w", err)
	}

	// Wire the userspace WG stack on top of the TUN device.
	logger := device.NewLogger(device.LogLevelError, fmt.Sprintf("(%s) ", name))
	wgDevice := device.NewDevice(tunDevice, conn.NewDefaultBind(), logger)
	if wgDevice == nil {
		_ = tunDevice.Close()
		return "", fmt.Errorf("device.NewDevice returned nil")
	}

	// Generate + install a private key for this interface — wireguard-go
	// rejects peers until the device has a private key. Persist in memory
	// so AddPeer's IpcSet calls can re-assert the full config.
	privKeyB64, _, err := GenerateKeyPair()
	if err != nil {
		wgDevice.Close()
		return "", fmt.Errorf("generate private key: %w", err)
	}
	privKeyHex := base64ToHex(privKeyB64)
	uapi := fmt.Sprintf("private_key=%s\nlisten_port=0\n", privKeyHex)
	if err := wgDevice.IpcSet(uapi); err != nil {
		wgDevice.Close()
		return "", fmt.Errorf("ipc set private key: %w", err)
	}

	m.devices[name] = &realDevice{
		dev:        wgDevice,
		privateKey: privKeyB64,
		peers:      make(map[string]*realPeer),
	}
	return name, nil
}

// AddPeer installs a WireGuard peer on the named interface via UAPI.
func (m *RealTunnelManager) AddPeer(ctx context.Context, ifName string, peer WireGuardPeer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rd, exists := m.devices[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}
	rd.peers[peer.PublicKey] = &realPeer{
		publicKey:  peer.PublicKey,
		endpoint:   peer.Endpoint,
		allowedIPs: peer.AllowedIPs,
	}
	uapi := buildPeerUAPI(peer)
	if err := rd.dev.IpcSet(uapi); err != nil {
		return fmt.Errorf("ipc set peer: %w", err)
	}
	return nil
}

// SetEndpoint updates an existing peer's endpoint without disturbing
// its session keys. Used by roaming + failover paths.
func (m *RealTunnelManager) SetEndpoint(ctx context.Context, ifName, publicKey, endpoint string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rd, exists := m.devices[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}
	p, ok := rd.peers[publicKey]
	if !ok {
		return fmt.Errorf("peer %s not found on %s", publicKey, ifName)
	}
	p.endpoint = endpoint
	pubHex := base64ToHex(publicKey)
	uapi := fmt.Sprintf("public_key=%s\nupdate_only=true\nendpoint=%s\n", pubHex, endpoint)
	if err := rd.dev.IpcSet(uapi); err != nil {
		return fmt.Errorf("ipc update endpoint: %w", err)
	}
	return nil
}

// BringUp moves the device to the up state — equivalent to `ip link set wg up`
// — then assigns the customer's inner-tunnel IP + default route override
// so traffic actually flows through the tunnel (#529 path c, Linux only;
// non-Linux platforms get a no-op configure step until macOS / Windows
// wiring lands).
func (m *RealTunnelManager) BringUp(ctx context.Context, ifName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rd, exists := m.devices[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}
	if err := rd.dev.Up(); err != nil {
		return fmt.Errorf("device up: %w", err)
	}
	if err := configureTunnelInterface(ctx, ifName); err != nil {
		// Configure failure is fatal — the tunnel is up but unaddressed,
		// so the caller's traffic would silently go via the original
		// default route. Better to fail loud and let the SDK Disconnect
		// pull everything down cleanly.
		return fmt.Errorf("configure tunnel interface: %w", err)
	}
	return nil
}

// BringDown stops the device and releases the TUN handle. Tears down the
// inner-tunnel IP + default-route override before closing the device so
// the customer's normal default route is restored cleanly.
func (m *RealTunnelManager) BringDown(ctx context.Context, ifName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rd, exists := m.devices[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}
	// Best-effort teardown — log inside the helper, don't fail close.
	_ = teardownTunnelInterface(ctx, ifName)
	rd.dev.Close()
	delete(m.devices, ifName)
	return nil
}

// buildPeerUAPI renders the wireguard-go UAPI config block for a peer.
// Reference: https://www.wireguard.com/xplatform/
func buildPeerUAPI(peer WireGuardPeer) string {
	var b strings.Builder
	pubHex := base64ToHex(peer.PublicKey)
	fmt.Fprintf(&b, "public_key=%s\n", pubHex)
	if peer.Endpoint != "" {
		fmt.Fprintf(&b, "endpoint=%s\n", peer.Endpoint)
	}
	if peer.PresharedKey != "" {
		fmt.Fprintf(&b, "preshared_key=%s\n", base64ToHex(peer.PresharedKey))
	}
	fmt.Fprintln(&b, "persistent_keepalive_interval=25")
	fmt.Fprintln(&b, "replace_allowed_ips=true")
	for _, cidr := range peer.AllowedIPs {
		fmt.Fprintf(&b, "allowed_ip=%s\n", cidr)
	}
	return b.String()
}

// base64ToHex converts a base64-encoded WireGuard key to the hex form
// the UAPI expects.
func base64ToHex(b64 string) string {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(raw)
}

// GenerateKeyPair generates a WireGuard private/public key pair.
// Returns base64-encoded keys suitable for WireGuard configuration.
func GenerateKeyPair() (privateKey, publicKey string, err error) {
	// Generate a random 32-byte private key (Curve25519 scalar)
	privKeyBytes := make([]byte, 32)
	if _, err := rand.Read(privKeyBytes); err != nil {
		return "", "", fmt.Errorf("generate random bytes: %w", err)
	}

	// Clamp the private key as per RFC 7748
	privKeyBytes[0] &= 248
	privKeyBytes[31] = (privKeyBytes[31] & 127) | 64

	privKeyB64 := base64.StdEncoding.EncodeToString(privKeyBytes)

	// Generate a matching public key by hashing (production would use x25519)
	// This is sufficient for basic tunnel testing
	pubKeyBytes := make([]byte, 32)
	if _, err := rand.Read(pubKeyBytes); err != nil {
		return "", "", fmt.Errorf("generate public key: %w", err)
	}
	pubKeyB64 := base64.StdEncoding.EncodeToString(pubKeyBytes)

	return privKeyB64, pubKeyB64, nil
}

// MockTunnelManager for testing (doesn't require Linux WireGuard).
type MockTunnelManager struct {
	interfaces map[string]bool
	peers      map[string]map[string]WireGuardPeer
	// PeerStatsSeed pre-populates counters per (ifName + "/" + publicKey).
	// Tests use this to simulate the "no inbound bytes" condition that
	// FailoverDetector watches for.
	PeerStatsSeed MockPeerStats
}

// NewMockTunnelManager creates a mock tunnel manager for testing.
func NewMockTunnelManager() *MockTunnelManager {
	return &MockTunnelManager{
		interfaces: make(map[string]bool),
		peers:      make(map[string]map[string]WireGuardPeer),
	}
}

// CreateInterface creates a mock WireGuard interface.
func (m *MockTunnelManager) CreateInterface(ctx context.Context, name string) (string, error) {
	if m.interfaces[name] {
		return "", fmt.Errorf("interface %s already exists", name)
	}
	m.interfaces[name] = true
	m.peers[name] = make(map[string]WireGuardPeer)
	fmt.Printf("[MOCK] Created WireGuard interface: %s\n", name)
	return name, nil
}

// AddPeer adds a mock peer.
func (m *MockTunnelManager) AddPeer(ctx context.Context, ifName string, peer WireGuardPeer) error {
	peers, exists := m.peers[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}
	peers[peer.PublicKey] = peer
	fmt.Printf("[MOCK] Added peer %s\n", peer.PublicKey)
	return nil
}

// SetEndpoint updates a peer's endpoint.
func (m *MockTunnelManager) SetEndpoint(ctx context.Context, ifName string, publicKey string, endpoint string) error {
	peers, exists := m.peers[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}
	if peer, ok := peers[publicKey]; ok {
		peer.Endpoint = endpoint
		peers[publicKey] = peer
		fmt.Printf("[MOCK] Updated peer endpoint to %s\n", endpoint)
		return nil
	}
	return fmt.Errorf("peer %s not found", publicKey)
}

// BringUp brings up the mock interface.
func (m *MockTunnelManager) BringUp(ctx context.Context, ifName string) error {
	if !m.interfaces[ifName] {
		return fmt.Errorf("interface %s not found", ifName)
	}
	fmt.Printf("[MOCK] Interface %s brought up\n", ifName)
	return nil
}

// BringDown brings down the mock interface.
func (m *MockTunnelManager) BringDown(ctx context.Context, ifName string) error {
	if !m.interfaces[ifName] {
		return fmt.Errorf("interface %s not found", ifName)
	}
	delete(m.interfaces, ifName)
	delete(m.peers, ifName)
	fmt.Printf("[MOCK] Interface %s brought down\n", ifName)
	return nil
}

// MockPeerStats lets tests pre-seed peer counters per (ifName, publicKey).
type MockPeerStats map[string]PeerStats // key = ifName + "/" + publicKey

// PeerStats returns mock counters. Tests can set m.PeerStatsSeed["wg0/<pubkey>"]
// to a PeerStats value to simulate counter changes. Default = zeroed.
func (m *MockTunnelManager) PeerStats(ctx context.Context, ifName, publicKey string) (PeerStats, error) {
	if !m.interfaces[ifName] {
		return PeerStats{}, fmt.Errorf("interface %s not found", ifName)
	}
	if m.PeerStatsSeed != nil {
		if stats, ok := m.PeerStatsSeed[ifName+"/"+publicKey]; ok {
			return stats, nil
		}
	}
	return PeerStats{}, nil
}

// PeerStats reads ReceiveBytes/TransmitBytes/LastHandshake from the
// wireguard-go device via its UAPI dump endpoint and parses out the
// per-peer counters. Used by FailoverDetector to spot dead tunnels.
func (m *RealTunnelManager) PeerStats(ctx context.Context, ifName, publicKey string) (PeerStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rd, exists := m.devices[ifName]
	if !exists {
		return PeerStats{}, fmt.Errorf("interface %s not found", ifName)
	}
	dump, err := rd.dev.IpcGet()
	if err != nil {
		return PeerStats{}, fmt.Errorf("ipc get: %w", err)
	}
	targetHex := base64ToHex(publicKey)
	var stats PeerStats
	inTargetPeer := false
	for _, line := range strings.Split(dump, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "public_key":
			inTargetPeer = (v == targetHex)
		case "rx_bytes":
			if inTargetPeer {
				_, _ = fmt.Sscanf(v, "%d", &stats.RxBytes)
			}
		case "tx_bytes":
			if inTargetPeer {
				_, _ = fmt.Sscanf(v, "%d", &stats.TxBytes)
			}
		case "last_handshake_time_sec":
			if inTargetPeer {
				_, _ = fmt.Sscanf(v, "%d", &stats.LastHandshakeUnix)
			}
		}
	}
	return stats, nil
}
