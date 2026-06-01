//go:build linux

// tunnel_route_linux.go — assign inner-tunnel IP + override default route
// on the customer-side WireGuard interface so traffic actually flows
// through the tunnel.
//
// Without this step the `wg-iogrid0` device is brought up by
// wireguard-go but stays unaddressed and out of the routing table, so
// `curl ifconfig.me` still goes via the customer's normal default
// route — the tunnel is technically established but carries zero
// traffic. Refs #529 path c.
//
// Both ends agree on the static /16:
//   * provider daemon TUN: 10.66.0.1/16
//   * customer SDK wg:     10.66.0.2/16  (single-customer demo)
//
// Multi-customer IP allocation is a coordinator follow-up: vpn-svc
// should hand out unique addresses on RequestSession and surface them
// in GetSession. Until then the demo runs one customer at a time.
//
// Default-route override pattern (vs replacing it):
//   ip route add 0.0.0.0/1 dev <if>
//   ip route add 128.0.0.0/1 dev <if>
// These two /1 routes together cover the entire IPv4 unicast space
// but are more specific than the existing default route. The kernel
// picks the most-specific match, so packets to anywhere go via the
// tunnel; the original default route is preserved so the WG outer UDP
// (to the provider's public IP) doesn't loop back into the tunnel —
// it goes via the original default unmodified.

package vpn

import (
	"context"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

// CustomerInnerCIDR is the inner-tunnel address assigned to the customer's
// wg interface. /16 because the provider daemon TUN sits in the same subnet.
const CustomerInnerCIDR = "10.66.0.2/16"

// configureTunnelInterface assigns the customer inner IP + bring-up flag +
// default-route override on the named WG interface. Idempotent — calling
// twice is a no-op (netlink errors on duplicates are swallowed in tests
// and surfaced in real runs only on first failure).
func configureTunnelInterface(_ context.Context, ifName string) error {
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("link %s: %w", ifName, err)
	}

	addr, err := netlink.ParseAddr(CustomerInnerCIDR)
	if err != nil {
		return fmt.Errorf("parse %s: %w", CustomerInnerCIDR, err)
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		// EEXIST is the only ignorable error — we already configured.
		if !isEEXIST(err) {
			return fmt.Errorf("addr add: %w", err)
		}
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("link up: %w", err)
	}

	// Default-route split: two /1 routes that together cover all of
	// IPv4 unicast space but rank more-specific than any existing
	// 0.0.0.0/0 default. This is the standard pattern WireGuard's own
	// AllowedIPs = 0.0.0.0/0 expands to; we replicate it directly.
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		_, dst, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("parse route %s: %w", cidr, err)
		}
		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       dst,
			Scope:     netlink.SCOPE_LINK,
		}
		if err := netlink.RouteAdd(route); err != nil && !isEEXIST(err) {
			return fmt.Errorf("route add %s: %w", cidr, err)
		}
	}
	return nil
}

// teardownTunnelInterface reverses configureTunnelInterface. Called on
// Disconnect — best-effort, errors are logged not propagated because the
// caller is already in the disconnect path and the kernel will GC the
// routes when the interface goes down anyway.
func teardownTunnelInterface(_ context.Context, ifName string) error {
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		// Interface already gone — nothing to clean.
		return nil
	}
	// Delete the two /1 routes; ignore ENODEV/ENOENT (already gone).
	for _, cidr := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		_, dst, _ := net.ParseCIDR(cidr)
		route := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       dst,
			Scope:     netlink.SCOPE_LINK,
		}
		_ = netlink.RouteDel(route)
	}
	addr, err := netlink.ParseAddr(CustomerInnerCIDR)
	if err == nil {
		_ = netlink.AddrDel(link, addr)
	}
	return nil
}

// isEEXIST matches the netlink error string for "file exists" — Go
// doesn't expose the underlying syscall errno through the netlink lib,
// so we string-match. The exact text is stable across kernel versions.
func isEEXIST(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return s == "file exists" ||
		s == "address already assigned" ||
		s == "RTNETLINK answers: File exists"
}
