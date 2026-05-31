package vpn

import (
	"context"
	"fmt"
)

// DERPFallback is invoked when ICE checks return zero working candidates
// (symmetric NAT both sides, restrictive firewall, dual-stack IPv6 mismatch).
// The customer falls back to a DERP-style encrypted relay that forwards
// WireGuard packets between peers when no direct path exists.
//
// MVP status: SCAFFOLD ONLY. Real DERP relay servers are tracked in
// issue #521 — this file establishes the abstraction so the SDK
// integration point exists, and a future iogrid DERP fleet can plug in
// without touching ICEChecker or BastionClient.
type DERPFallback struct {
	// RelayHosts is the list of iogrid DERP relays to try in order.
	// Each entry is "host:port" (TCP for WireGuard-in-TCP framing).
	RelayHosts []string
	// Region tag — used by future routing logic to prefer same-region relays.
	Region string
}

// NewDERPFallback creates a fallback configuration for a region.
// If RelayHosts is empty, Try() returns ErrNoRelayConfigured to signal
// the caller should surface the session-failure outcome to the user.
func NewDERPFallback(region string, relayHosts []string) *DERPFallback {
	return &DERPFallback{
		Region:     region,
		RelayHosts: relayHosts,
	}
}

// ErrNoRelayConfigured is returned by Try() when the fallback has no
// configured relays. Caller should fail the session with a clear
// "no path to provider" message rather than retry forever.
var ErrNoRelayConfigured = fmt.Errorf("no DERP relays configured for region")

// Try attempts to establish a relayed WireGuard endpoint via the next
// available DERP relay. Returns the relay endpoint string ("host:port")
// that can be used as a WireGuard peer endpoint.
//
// MVP behavior: returns ErrNoRelayConfigured (since no relays are
// deployed yet). Once the DERP fleet is up (#521), this will:
//  1. Iterate RelayHosts in order
//  2. For each, perform a TLS handshake + control-channel auth
//  3. Reserve a session ID on the relay
//  4. Return the relay's WG-tunnel endpoint
func (d *DERPFallback) Try(ctx context.Context) (string, error) {
	if len(d.RelayHosts) == 0 {
		return "", ErrNoRelayConfigured
	}
	// TODO #521: implement actual DERP control-channel + session reservation.
	// For now, return ErrNoRelayConfigured so callers fail loudly rather
	// than silently fall back to a non-existent relay.
	return "", fmt.Errorf("DERP fallback not yet implemented (issue #521): would try %v", d.RelayHosts)
}

// IsAvailable returns true if Try() has any chance of succeeding (i.e.,
// at least one relay is configured AND a future implementation exists).
// Right now always false — the BastionClient uses this to skip the
// fallback call entirely and surface the ICE failure directly.
func (d *DERPFallback) IsAvailable() bool {
	// Phase 4 — flip to len(d.RelayHosts) > 0 once #521 ships.
	return false
}
