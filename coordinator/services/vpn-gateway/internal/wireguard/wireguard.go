// Package wireguard is the userspace WireGuard server frontend.
//
// In production the gateway uses kernel WireGuard via the wgctrl-go
// netlink interface when running on a Linux kernel that has it (k8s
// nodes always do — Cilium already requires a recent kernel). On platforms
// without kernel WG (none of our prod nodes; kept as a fallback for dev
// loop on macOS), it uses wireguard-go entirely in userspace.
//
// This file defines the *abstraction* the rest of the gateway sees —
// the actual kernel/userspace binding is wired up at process start in
// main(). The interface is small enough that we can mock it in tests
// without dragging in any of the WG implementation.
package wireguard

import (
	"context"
	"net"
	"time"
)

// Server is the WireGuard backend the gateway data plane talks to.
//
// Each call is sync from the gateway's perspective; an implementation
// is free to batch internally. The set of methods is deliberately tiny
// because the gateway does not need to manage the full WG control plane
// — only enough to (a) admit / revoke a peer and (b) read its byte
// counters for metering.
type Server interface {
	// Start brings the interface up on the supplied UDP listen address
	// and private-key. Idempotent: calling Start twice with the same
	// args is a no-op.
	Start(ctx context.Context, listenAddr *net.UDPAddr, serverPrivateKey [32]byte) error

	// Stop tears down the interface and cancels all in-flight peer ops.
	Stop(ctx context.Context) error

	// AddPeer admits a customer's public key + their assigned tunnel
	// IP. AllowedIPs is the source-CIDR list we accept FROM the peer
	// (typically the customer's /32 only).
	AddPeer(ctx context.Context, pubKey [32]byte, assignedIP net.IP, allowedIPs []*net.IPNet) error

	// RemovePeer drops a peer immediately. Subsequent packets from
	// that pubKey are silently dropped at the WG layer.
	RemovePeer(ctx context.Context, pubKey [32]byte) error

	// PeerStats returns the cumulative byte counters for one peer.
	// Returns (Stats{}, ErrUnknownPeer) on miss.
	PeerStats(ctx context.Context, pubKey [32]byte) (Stats, error)

	// PublicKey returns the server's WireGuard public key derived
	// from its private key. Used by gateway-bff to embed in client
	// configs.
	PublicKey() [32]byte
}

// Stats is the byte counter pair WG exposes per peer.
type Stats struct {
	BytesReceived uint64
	BytesSent     uint64
	LastHandshake time.Time
}

// ErrUnknownPeer is returned by PeerStats / RemovePeer when the pubKey
// is not in the peer table.
type ErrUnknownPeer struct{ PubKeyHex string }

func (e ErrUnknownPeer) Error() string { return "wireguard: unknown peer " + e.PubKeyHex }

// Mock is an in-memory Server implementation suitable for unit and
// integration tests. It tracks peers in a map and lets tests inject
// synthetic byte counts via SimulateTraffic.
//
// Mock is goroutine-safe.
type Mock struct {
	mu         peerMutex
	listenAddr *net.UDPAddr
	privKey    [32]byte
	pubKey     [32]byte
	peers      map[[32]byte]*mockPeer
}

type mockPeer struct {
	assignedIP    net.IP
	allowedIPs    []*net.IPNet
	bytesIn       uint64
	bytesOut      uint64
	lastHandshake time.Time
}

// NewMock constructs the in-memory backend. Public key is derived by
// flipping bit 0 of every byte of the private key — enough to produce
// a deterministic but non-equal pubkey for round-trip tests; real
// derivation is X25519 in the kernel/userspace implementation.
func NewMock() *Mock {
	return &Mock{
		peers: map[[32]byte]*mockPeer{},
	}
}

// Start records the args and returns immediately.
func (m *Mock) Start(_ context.Context, addr *net.UDPAddr, priv [32]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listenAddr = addr
	m.privKey = priv
	for i := range priv {
		m.pubKey[i] = priv[i] ^ 0x01
	}
	return nil
}

// Stop drops all peers.
func (m *Mock) Stop(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers = map[[32]byte]*mockPeer{}
	return nil
}

// AddPeer admits a peer.
func (m *Mock) AddPeer(_ context.Context, pk [32]byte, ip net.IP, allowed []*net.IPNet) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[pk] = &mockPeer{assignedIP: ip, allowedIPs: allowed}
	return nil
}

// RemovePeer drops a peer.
func (m *Mock) RemovePeer(_ context.Context, pk [32]byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.peers[pk]; !ok {
		return ErrUnknownPeer{PubKeyHex: hexOf(pk)}
	}
	delete(m.peers, pk)
	return nil
}

// PeerStats returns the byte counters.
func (m *Mock) PeerStats(_ context.Context, pk [32]byte) (Stats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.peers[pk]
	if !ok {
		return Stats{}, ErrUnknownPeer{PubKeyHex: hexOf(pk)}
	}
	return Stats{
		BytesReceived: p.bytesIn,
		BytesSent:     p.bytesOut,
		LastHandshake: p.lastHandshake,
	}, nil
}

// SimulateTraffic injects synthetic byte counts on a peer (test helper).
func (m *Mock) SimulateTraffic(pk [32]byte, in, out uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p, ok := m.peers[pk]; ok {
		p.bytesIn += in
		p.bytesOut += out
		p.lastHandshake = time.Now().UTC()
	}
}

// PublicKey returns the derived server pubkey.
func (m *Mock) PublicKey() [32]byte { return m.pubKey }

// HasPeer reports whether the peer is currently admitted.
func (m *Mock) HasPeer(pk [32]byte) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.peers[pk]
	return ok
}

// PeerCount returns the number of currently-admitted peers.
func (m *Mock) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// hexOf is duplicated locally rather than imported from
// internal/customer to keep this package dependency-free for tests.
func hexOf(b [32]byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, 64)
	for i, x := range b {
		out[i*2] = hex[x>>4]
		out[i*2+1] = hex[x&0x0f]
	}
	return string(out)
}
