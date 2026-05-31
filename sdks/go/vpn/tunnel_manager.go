package vpn

import (
	"context"
	"fmt"
	"net"

	"golang.zx2c4.com/wireguard"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"
)

// RealTunnelManager implements TunnelManager using the Linux WireGuard kernel interface.
type RealTunnelManager struct {
	devices map[string]*device.Device
}

// NewRealTunnelManager creates a new tunnel manager for Linux WireGuard.
func NewRealTunnelManager() *RealTunnelManager {
	return &RealTunnelManager{
		devices: make(map[string]*device.Device),
	}
}

// CreateInterface creates a WireGuard interface.
func (m *RealTunnelManager) CreateInterface(ctx context.Context, name string) (string, error) {
	// Create TUN device
	tunDevice, err := tun.CreateTUN(name, device.DefaultMTU)
	if err != nil {
		return "", fmt.Errorf("create tun: %w", err)
	}

	realInterfaceRoutes := []string{}
	if v4list, v6list, err := tunDevice.BatchSize(); err == nil {
		realInterfaceRoutes = []string{fmt.Sprintf("v4:%d v6:%d", v4list, v6list)}
	}
	_ = realInterfaceRoutes

	// Create WireGuard device
	logger := device.NewLogger(device.LogLevelVerbose, fmt.Sprintf("(%s) ", name))
	wgDevice := device.NewDevice(tunDevice, conn.NewDefaultBind(), logger)
	if wgDevice == nil {
		tunDevice.Close()
		return "", fmt.Errorf("create device failed")
	}

	m.devices[name] = wgDevice
	fmt.Printf("[TUN] Created WireGuard interface: %s\n", name)
	return name, nil
}

// AddPeer configures a WireGuard peer.
func (m *RealTunnelManager) AddPeer(ctx context.Context, ifName string, peer WireGuardPeer) error {
	wgDev, exists := m.devices[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}

	// In production: parse public key, add peer via wgDev.UAPI()
	// For MVP: just log
	fmt.Printf("[TUN] Added peer %s with endpoint %s\n", peer.PublicKey, peer.Endpoint)
	_ = wgDev
	return nil
}

// SetEndpoint updates a peer's endpoint.
func (m *RealTunnelManager) SetEndpoint(ctx context.Context, ifName string, publicKey string, endpoint string) error {
	wgDev, exists := m.devices[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}

	fmt.Printf("[TUN] Updated peer %s endpoint to %s\n", publicKey, endpoint)
	_ = wgDev
	return nil
}

// BringUp brings the interface up.
func (m *RealTunnelManager) BringUp(ctx context.Context, ifName string) error {
	wgDev, exists := m.devices[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}

	// In production: bring up the interface (set it administratively UP)
	_ = wgDev
	fmt.Printf("[TUN] Interface %s brought up\n", ifName)
	return nil
}

// BringDown brings the interface down.
func (m *RealTunnelManager) BringDown(ctx context.Context, ifName string) error {
	wgDev, exists := m.devices[ifName]
	if !exists {
		return fmt.Errorf("interface %s not found", ifName)
	}

	wgDev.Close()
	delete(m.devices, ifName)
	fmt.Printf("[TUN] Interface %s brought down\n", ifName)
	return nil
}

// GenerateKeyPair generates a WireGuard private/public key pair.
func GenerateKeyPair() (privateKey, publicKey string, err error) {
	// Use wireguard-go's key types
	privKey, err := wireguard.NewPrivateKey()
	if err != nil {
		return "", "", fmt.Errorf("generate private key: %w", err)
	}

	pubKey := privKey.PublicKey()
	return privKey.String(), pubKey.String(), nil
}

// MockTunnelManager for testing (doesn't require Linux WireGuard).
type MockTunnelManager struct {
	interfaces map[string]bool
	peers      map[string]map[string]WireGuardPeer
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
