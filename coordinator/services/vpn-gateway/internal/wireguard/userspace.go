// Package wireguard — UserspaceServer: pure-Go WireGuard endpoint that does
// NOT require CAP_NET_ADMIN or any kernel module. Uses wireguard-go's
// device.Device with gVisor netstack as the TUN backend so the pod can run
// under PodSecurity 'restricted:latest'. Refs iogrid/iogrid#478.
//
// Architecture:
//
//	UDP :51820 ← WireGuard handshake + encrypted datagrams
//	     ↓
//	wireguard-go (device.Device) — IKE handshake, session keys, decapsulation
//	     ↓
//	gVisor netstack — pure-Go TCP/IP stack, no kernel TUN device
//	     ↓
//	forwardTCPLoop — proxies TCP from the tunnel to the internet
//	                 (Phase-0: direct dial; Phase-1: provider SOCKS5 pool)

package wireguard

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.zx2c4.com/wireguard/conn"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/netstack"
)

const (
	// gatewayVPNAddr is the server's IP inside the WireGuard tunnel.
	gatewayVPNAddr = "10.99.0.1"
	// wgMTU is the WireGuard MTU (1500 outer − 80 WG overhead).
	wgMTU = 1420
)

var _ Server = (*UserspaceServer)(nil)

// UserspaceServer is a WireGuard Server backed by wireguard-go + gVisor
// netstack. No kernel privileges are required.
type UserspaceServer struct {
	mu      sync.RWMutex
	dev     *device.Device
	tnet    *netstack.Net
	privKey [32]byte
	pubKey  [32]byte
	peers   map[[32]byte]*userspaceP
}

type userspaceP struct {
	assignedIP net.IP
	allowedIPs []*net.IPNet
}

// NewUserspace constructs the userspace WireGuard server. Call Start to
// bring the WireGuard listener up.
func NewUserspace() *UserspaceServer {
	return &UserspaceServer{
		peers: map[[32]byte]*userspaceP{},
	}
}

// Start configures the WireGuard device and binds the UDP listener.
//
// If serverPrivateKey is all-zero, a fresh ephemeral key is generated.
// Phase-0 accepts key rotation on pod restart; Phase-1 should load from
// a sealed secret for persistent client configs.
func (u *UserspaceServer) Start(ctx context.Context, listenAddr *net.UDPAddr, serverPrivateKey [32]byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.dev != nil {
		return nil // idempotent
	}

	var privKey [32]byte
	if allZero(serverPrivateKey) {
		// Generate a fresh ephemeral key and clamp per RFC 7748 §5.
		if _, err := rand.Read(privKey[:]); err != nil {
			return fmt.Errorf("wireguard generate private key: %w", err)
		}
		privKey[0] &= 248
		privKey[31] &= 127
		privKey[31] |= 64
	} else {
		privKey = serverPrivateKey
	}
	u.privKey = privKey

	// Derive the WireGuard public key (X25519 scalar base-point mult).
	var pubKey [32]byte
	curve25519.ScalarBaseMult(&pubKey, &privKey)
	u.pubKey = pubKey

	// Build the gVisor netstack TUN — pure Go, zero kernel capabilities.
	gatewayIP := netip.MustParseAddr(gatewayVPNAddr)
	tun, tnet, err := netstack.CreateNetTUN(
		[]netip.Addr{gatewayIP},
		nil,   // DNS resolvers (not needed server-side)
		wgMTU,
	)
	if err != nil {
		return fmt.Errorf("wireguard netstack TUN: %w", err)
	}
	u.tnet = tnet

	logger := device.NewLogger(device.LogLevelError, "[wireguard] ")
	dev := device.NewDevice(tun, conn.NewDefaultBind(), logger)

	// Configure via UAPI IPC — no netlink, no privileges.
	port := 51820
	if listenAddr != nil && listenAddr.Port != 0 {
		port = listenAddr.Port
	}
	ipcConf := fmt.Sprintf("private_key=%s\nlisten_port=%d\n",
		hex.EncodeToString(privKey[:]), port)
	if err := dev.IpcSet(ipcConf); err != nil {
		dev.Close()
		return fmt.Errorf("wireguard IpcSet: %w", err)
	}
	if err := dev.Up(); err != nil {
		dev.Close()
		return fmt.Errorf("wireguard device up: %w", err)
	}

	u.dev = dev
	go u.forwardTCPLoop(ctx)
	return nil
}

// forwardTCPLoop accepts TCP connections arriving through the WireGuard
// tunnel's netstack and proxies them to their destination.
// Phase-0: direct TCP dial. Phase-1: route via provider SOCKS5 pool.
func (u *UserspaceServer) forwardTCPLoop(ctx context.Context) {
	l, err := u.tnet.ListenTCP(&net.TCPAddr{})
	if err != nil {
		return
	}
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go u.handleTCPConn(ctx, c)
	}
}

func (u *UserspaceServer) handleTCPConn(ctx context.Context, client net.Conn) {
	defer client.Close()
	dst := client.LocalAddr().String()
	upstream, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", dst)
	if err != nil {
		return
	}
	defer upstream.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(upstream, client)
	}()
	_, _ = io.Copy(client, upstream)
	<-done
}

// Stop tears down the device.
func (u *UserspaceServer) Stop(_ context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.dev == nil {
		return nil
	}
	u.dev.Down()
	u.dev.Close()
	u.dev = nil
	return nil
}

// AddPeer admits a VPN client.
func (u *UserspaceServer) AddPeer(_ context.Context, pubKey [32]byte, assignedIP net.IP, allowedIPs []*net.IPNet) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.dev == nil {
		return errors.New("wireguard: server not started")
	}
	var sb strings.Builder
	sb.WriteString("public_key=" + hex.EncodeToString(pubKey[:]) + "\n")
	for _, cidr := range allowedIPs {
		sb.WriteString("allowed_ip=" + cidr.String() + "\n")
	}
	if err := u.dev.IpcSet(sb.String()); err != nil {
		return fmt.Errorf("wireguard AddPeer: %w", err)
	}
	u.peers[pubKey] = &userspaceP{assignedIP: assignedIP, allowedIPs: allowedIPs}
	return nil
}

// RemovePeer revokes a VPN client.
func (u *UserspaceServer) RemovePeer(_ context.Context, pubKey [32]byte) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.dev == nil {
		return errors.New("wireguard: server not started")
	}
	if _, ok := u.peers[pubKey]; !ok {
		return ErrUnknownPeer{PubKeyHex: hexOf(pubKey)}
	}
	conf := "public_key=" + hex.EncodeToString(pubKey[:]) + "\nremove=true\n"
	if err := u.dev.IpcSet(conf); err != nil {
		return fmt.Errorf("wireguard RemovePeer: %w", err)
	}
	delete(u.peers, pubKey)
	return nil
}

// PeerStats returns cumulative byte counters from the WireGuard device.
func (u *UserspaceServer) PeerStats(_ context.Context, pubKey [32]byte) (Stats, error) {
	u.mu.RLock()
	defer u.mu.RUnlock()
	if u.dev == nil {
		return Stats{}, errors.New("wireguard: server not started")
	}
	if _, ok := u.peers[pubKey]; !ok {
		return Stats{}, ErrUnknownPeer{PubKeyHex: hexOf(pubKey)}
	}
	raw, err := u.dev.IpcGet()
	if err != nil {
		return Stats{}, fmt.Errorf("wireguard IpcGet: %w", err)
	}
	return parseIpcStats(raw, hex.EncodeToString(pubKey[:])), nil
}

// PublicKey returns the server's WireGuard public key.
func (u *UserspaceServer) PublicKey() [32]byte {
	u.mu.RLock()
	defer u.mu.RUnlock()
	return u.pubKey
}

// parseIpcStats scans wireguard-go UAPI output for a specific peer's
// rx_bytes / tx_bytes / last_handshake_time_sec fields.
func parseIpcStats(raw, peerHexKey string) Stats {
	var (
		s      Stats
		inPeer bool
	)
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			inPeer = false
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "public_key":
			inPeer = (v == peerHexKey)
		case "rx_bytes":
			if inPeer {
				fmt.Sscanf(v, "%d", &s.BytesReceived)
			}
		case "tx_bytes":
			if inPeer {
				fmt.Sscanf(v, "%d", &s.BytesSent)
			}
		case "last_handshake_time_sec":
			if inPeer {
				var sec int64
				fmt.Sscanf(v, "%d", &sec)
				if sec > 0 {
					s.LastHandshake = time.Unix(sec, 0)
				}
			}
		}
	}
	return s
}

// allZero reports whether every byte in b is zero.
func allZero(b [32]byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}
