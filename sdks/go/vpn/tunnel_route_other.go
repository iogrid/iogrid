//go:build !linux

// tunnel_route_other.go — stubs for non-Linux builds. Pairs with
// tunnel_route_linux.go which carries the real netlink implementation.
//
// macOS + Windows need different syscalls (utun/route(8) + WinTun/PowerShell
// netsh respectively); follow-up to #529 path c will add them under their
// own build tags. Until then non-Linux builds compile cleanly but `iogrid
// vpn connect` won't route traffic — the tunnel handshake succeeds, but
// the customer's default route stays where it was.

package vpn

import (
	"context"
	"net"
)

// configureTunnelInterface is a no-op on non-Linux platforms.
func configureTunnelInterface(_ context.Context, _ string) error {
	return nil
}

// teardownTunnelInterface is a no-op on non-Linux platforms.
func teardownTunnelInterface(_ context.Context, _ string) error {
	return nil
}

// AddExceptionHost is a no-op on non-Linux platforms. The Linux build
// pins /32 exception routes via the pre-VPN default gateway; non-Linux
// platforms haven't implemented the route override yet so there's
// nothing to except. Stub here so bastion_client.go compiles for
// darwin / windows.
func AddExceptionHost(_ net.IP) {}

// ResetExceptionHosts is a no-op on non-Linux platforms. Pairs with
// AddExceptionHost.
func ResetExceptionHosts() {}
