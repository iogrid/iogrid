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

// ExceptionHosts are IP/32 destinations that MUST keep their original
// route (via the host's pre-VPN default gateway) even after the
// default-route override goes in. Without these, the SDK's own
// coordinator round-trips (confirmCandidate + heartbeat) AND the
// outer WG UDP packets to the provider's endpoint would loop back
// into the unfinished tunnel and stall.
//
// Populated by the caller via AddExceptionHost before
// configureTunnelInterface runs. Resets between connects so a
// roaming-to-new-provider scenario doesn't leak stale exceptions.
var exceptionHosts []net.IP

// AddExceptionHost adds an IP/32 exception. Call before BringUp for
// every coordinator + provider IP the SDK needs to keep reachable
// outside the tunnel. Idempotent — duplicates are filtered at insert.
func AddExceptionHost(ip net.IP) {
	if ip == nil {
		return
	}
	for _, x := range exceptionHosts {
		if x.Equal(ip) {
			return
		}
	}
	exceptionHosts = append(exceptionHosts, ip)
}

// ResetExceptionHosts clears the list. Called by teardown so the next
// connect starts with a clean slate.
func ResetExceptionHosts() {
	exceptionHosts = nil
}

// CustomerInnerCIDR is the inner-tunnel address assigned to the customer's
// wg interface. /16 because the provider daemon TUN sits in the same subnet.
const CustomerInnerCIDR = "10.66.0.2/16"

// configureTunnelInterface assigns the customer inner IP + bring-up flag +
// default-route override on the named WG interface. Pins exception /32
// routes for every host in exceptionHosts via the pre-VPN default route
// FIRST so the SDK's own coordinator round-trips + the outer WG UDP
// packets don't loop back into the unfinished tunnel.
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
		if !isEEXIST(err) {
			return fmt.Errorf("addr add: %w", err)
		}
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("link up: %w", err)
	}

	// ── 1. Pin exception /32 routes via the original default gateway
	// ────────────────────────────────────────────────────────────
	// These MUST land before the /1 override or the next route lookup
	// for the coordinator/provider host picks the broken tunnel.
	origGw, origLink, err := defaultRouteOriginal()
	if err == nil && origGw != nil {
		for _, host := range exceptionHosts {
			h32 := host.To4()
			if h32 == nil {
				continue
			}
			route := &netlink.Route{
				LinkIndex: origLink,
				Dst:       &net.IPNet{IP: h32, Mask: net.CIDRMask(32, 32)},
				Gw:        origGw,
			}
			if err := netlink.RouteAdd(route); err != nil && !isEEXIST(err) {
				return fmt.Errorf("route add exception %s: %w", host, err)
			}
		}
	}

	// ── 2. Default-route override via the tunnel
	// ────────────────────────────────────────────────────────────
	// Two /1 routes that together cover all of IPv4 unicast space but
	// rank more-specific than any existing 0.0.0.0/0 default. Standard
	// pattern WireGuard's AllowedIPs=0.0.0.0/0 expands to.
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

	// ── 3. Read-back verification (#565)
	// ────────────────────────────────────────────────────────────
	// The netlink ops above can return nil and STILL silently no-op if
	// the process lacks CAP_NET_ADMIN — the kernel rejects the message
	// but the Go binding swallows it under some configurations. Read
	// the state back and fail loud if anything is missing. Without
	// this, the CLI prints "tunnel established" with a DOWN interface
	// and no addr, which is what caused the production downtime on
	// 2026-06-02 when an operator ran the manual repro.
	if err := verifyTunnelConfigured(ifName); err != nil {
		return fmt.Errorf("post-configure verification: %w", err)
	}
	return nil
}

// verifyTunnelConfigured reads back the interface state + addr + routes
// after configureTunnelInterface has issued the netlink ops. Returns
// a specific error naming the missing piece so the CLI surfaces the
// real root cause instead of "tunnel established" on a broken stack.
func verifyTunnelConfigured(ifName string) error {
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return fmt.Errorf("link %s vanished: %w", ifName, err)
	}
	// Link must be UP — LinkSetUp can silently no-op without CAP_NET_ADMIN.
	if link.Attrs().Flags&net.FlagUp == 0 {
		return fmt.Errorf(
			"interface %s is not UP after configure — likely missing CAP_NET_ADMIN; "+
				"try: sudo setcap cap_net_admin+eip $(command -v iogrid)",
			ifName,
		)
	}
	// Customer inner IP must be assigned.
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("addr list on %s: %w", ifName, err)
	}
	want := stripCIDR(CustomerInnerCIDR)
	found := false
	for _, a := range addrs {
		if a.IP.String() == want {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf(
			"customer inner IP %s not present on %s after configure — "+
				"likely missing CAP_NET_ADMIN; setcap or run via sudo",
			CustomerInnerCIDR, ifName,
		)
	}
	// Both /1 default-route override entries must exist on the link.
	routes, err := netlink.RouteList(link, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("route list on %s: %w", ifName, err)
	}
	seen := map[string]bool{}
	for _, r := range routes {
		if r.Dst == nil {
			continue
		}
		seen[r.Dst.String()] = true
	}
	for _, expected := range []string{"0.0.0.0/1", "128.0.0.0/1"} {
		if !seen[expected] {
			return fmt.Errorf(
				"default-route override %s missing on %s — partial config; "+
					"tunnel WG handshake may succeed but no traffic will flow",
				expected, ifName,
			)
		}
	}
	return nil
}

// defaultRouteOriginal returns the gateway + link index of the kernel's
// pre-VPN default route. Used to pin /32 exception routes through the
// real ISP gateway instead of the tunnel.
func defaultRouteOriginal() (net.IP, int, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return nil, 0, err
	}
	for _, r := range routes {
		// Default route has Dst == nil OR Dst.IP == 0.0.0.0/0.
		if r.Dst == nil || r.Dst.IP.Equal(net.IPv4(0, 0, 0, 0)) {
			return r.Gw, r.LinkIndex, nil
		}
	}
	return nil, 0, fmt.Errorf("no default route found")
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
