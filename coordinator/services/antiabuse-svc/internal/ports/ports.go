// Package ports enforces the outbound-port allow/deny policy described
// in docs/LEGAL.md "Outbound port restrictions":
//
//   - SMTP (25, 465, 587, 2525)            — blocked (no spam relay)
//   - IRC (6667, 6697)                     — blocked (no DDoS coordination)
//   - Tor exit relay (9001, 9030)          — blocked (don't be a Tor exit)
//   - telnet (23)                          — blocked (no plaintext shells)
//   - common HTTP(S) (80, 443, 8080, 8443) — explicitly allowed
//
// Anything else falls through to ALLOW unless an explicit deny list
// override is provided.
package ports

import (
	"strconv"
	"strings"
)

// Default port policy values matching docs/LEGAL.md.
var (
	defaultDenied = map[uint32]string{
		23:   "telnet (plaintext shell, no audit trail)",
		25:   "SMTP (spam relay risk)",
		465:  "SMTPS (spam relay risk)",
		587:  "SMTP submission (spam relay risk)",
		2525: "SMTP alt (spam relay risk)",
		6667: "IRC (DDoS coordination)",
		6697: "IRCS (DDoS coordination)",
		9001: "Tor OR (avoid running as Tor exit)",
		9030: "Tor dir (avoid running as Tor exit)",
	}
	defaultAllowed = map[uint32]struct{}{
		80:   {},
		443:  {},
		8080: {},
		8443: {},
	}
)

// Decision is the outcome of a port lookup.
type Decision struct {
	// Allowed is true iff the port may be used.
	Allowed bool
	// Reason is the human-readable explanation when blocked.
	Reason string
	// Slug is a machine-readable identifier for telemetry.
	Slug string
}

// Policy holds the per-instance allow/deny configuration. The zero
// value behaves as the documented default.
type Policy struct {
	denied  map[uint32]string
	allowed map[uint32]struct{}
}

// NewDefaultPolicy returns the LEGAL.md baseline policy.
func NewDefaultPolicy() *Policy {
	p := &Policy{
		denied:  make(map[uint32]string, len(defaultDenied)),
		allowed: make(map[uint32]struct{}, len(defaultAllowed)),
	}
	for k, v := range defaultDenied {
		p.denied[k] = v
	}
	for k := range defaultAllowed {
		p.allowed[k] = struct{}{}
	}
	return p
}

// Allow whitelists an additional port (overrides any deny).
func (p *Policy) Allow(port uint32) {
	p.allowed[port] = struct{}{}
	delete(p.denied, port)
}

// Deny blacklists an additional port with the given reason.
func (p *Policy) Deny(port uint32, reason string) {
	p.denied[port] = reason
	delete(p.allowed, port)
}

// Check returns the decision for a given port. The zero port maps to
// ALLOW (caller didn't specify, e.g. a domain-only check).
func (p *Policy) Check(port uint32) Decision {
	if port == 0 {
		return Decision{Allowed: true}
	}
	if reason, blocked := p.denied[port]; blocked {
		return Decision{
			Allowed: false,
			Reason:  reason,
			Slug:    "destination_port_blocked",
		}
	}
	return Decision{Allowed: true}
}

// CheckExplicit is like Check but also asserts the port is on the allow
// list (used by daemon strict-mode for SOCKS5 CONNECT).
func (p *Policy) CheckExplicit(port uint32) Decision {
	d := p.Check(port)
	if !d.Allowed {
		return d
	}
	if _, ok := p.allowed[port]; !ok {
		return Decision{
			Allowed: false,
			Reason:  "port not in explicit allow list",
			Slug:    "destination_port_not_whitelisted",
		}
	}
	return Decision{Allowed: true}
}

// Snapshot returns the policy in a human-readable form, used by
// ListFilters to mirror to the daemon.
func (p *Policy) Snapshot() string {
	denied := make([]string, 0, len(p.denied))
	for port, reason := range p.denied {
		denied = append(denied, strconv.FormatUint(uint64(port), 10)+":"+reason)
	}
	allowed := make([]string, 0, len(p.allowed))
	for port := range p.allowed {
		allowed = append(allowed, strconv.FormatUint(uint64(port), 10))
	}
	return "denied=[" + strings.Join(denied, ",") + "] allowed=[" + strings.Join(allowed, ",") + "]"
}
