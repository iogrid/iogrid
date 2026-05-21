// Package geoip resolves a public IPv4/IPv6 address into the
// ISO-3166-1 alpha-2 country code, English subdivision (state/region)
// name, and a stable lower-snake slug suitable for the
// providers.region_slug column.
//
// Why a thin wrapper instead of calling oschwald/geoip2-golang directly
// from the registration handler: we want the lookup to be (a) easy to
// stub out in unit tests, (b) tolerant of a missing .mmdb file at boot
// (Phase-0 dev clusters may not ship the database) and (c) safe to call
// hundreds of times per second from the heartbeat hot path — the
// underlying maxmind reader is concurrent-safe so no mutex is needed.
//
// Design constraints (issue #359):
//
//   - LOOKUP IS SERVER-SIDE ONLY. Daemons may not supply their own
//     country/region — a malicious provider could falsely advertise as
//     a high-payout US-residential when running on a EU datacentre IP.
//     The observed source IP (X-Forwarded-For from Traefik, falling
//     back to the connection RemoteAddr) is the only authoritative
//     signal.
//   - The .mmdb file is sourced from db-ip.com's Lite IP-to-City feed
//     (CC BY 4.0, no license key required). The MaxMind GeoLite2-City
//     feed is equivalent but requires an account-id + license key,
//     which complicates Phase-0 air-gapped deploys. The init container
//     in infra/k8s/base/providers-svc downloads + ungzips the .mmdb at
//     pod start; this package just opens the resulting file.
//
// Refs #359.
package geoip

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/oschwald/geoip2-golang"
)

// Result is the resolved geo posture for one IP.
type Result struct {
	// CountryCode is the ISO 3166-1 alpha-2 (e.g. "TR", "US"). Empty
	// when the IP could not be mapped (private RFC1918, reserved,
	// unknown).
	CountryCode string
	// CountryName is the English country name (e.g. "Turkey"). Best-
	// effort, included so the UI doesn't need a second lookup just to
	// render a label.
	CountryName string
	// RegionName is the English subdivision/state name (e.g. "Istanbul",
	// "California"). Empty when the DB only resolves to country.
	RegionName string
	// RegionSlug is a stable lower-snake identifier derived from
	// CountryCode + RegionName, suitable for the providers.region_slug
	// column (e.g. "tr-istanbul", "us-california"). Falls back to just
	// the lower-case country code when no subdivision is known.
	RegionSlug string
}

// Lookuper is the dependency the handlers consume. Production wires
// the .mmdb-backed implementation; tests inject a stub.
type Lookuper interface {
	Lookup(ip string) (Result, error)
}

// ErrNotFound is returned when the IP is structurally valid but the
// database has no record for it (RFC1918, unannounced ranges, fresh
// allocations the DB snapshot doesn't yet cover).
var ErrNotFound = errors.New("geoip: ip not found in database")

// ErrUnavailable is returned by the noop Lookuper when no .mmdb was
// loaded at boot. Handlers should treat this as a soft miss and
// proceed without writing country/region columns.
var ErrUnavailable = errors.New("geoip: database not loaded")

// reader is the production Lookuper.
type reader struct {
	db *geoip2.Reader
	mu sync.Mutex // only protects Close; lookups are concurrent-safe
}

// New opens the .mmdb file at the supplied path. Returns ErrUnavailable
// when the path is empty so the caller can fall back to NoopLookuper
// without special-casing the empty-string sentinel.
func New(path string) (Lookuper, error) {
	if path == "" {
		return nil, ErrUnavailable
	}
	db, err := geoip2.Open(path)
	if err != nil {
		return nil, fmt.Errorf("geoip: open %q: %w", path, err)
	}
	return &reader{db: db}, nil
}

// Close releases the underlying mmap. Safe to call multiple times.
func (r *reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db == nil {
		return nil
	}
	err := r.db.Close()
	r.db = nil
	return err
}

// Lookup resolves the IP and returns the geo posture. Returns
// ErrNotFound when the address is valid but unmapped (e.g. RFC1918).
// Returns an error wrapping the underlying maxmind parse failure on
// truly malformed input.
func (r *reader) Lookup(ipText string) (Result, error) {
	ipText = strings.TrimSpace(ipText)
	if ipText == "" {
		return Result{}, ErrNotFound
	}
	ip := net.ParseIP(ipText)
	if ip == nil {
		return Result{}, fmt.Errorf("geoip: parse %q: not an IP", ipText)
	}
	// Reject loopback, link-local, and private ranges up-front — the
	// lookup would either return ErrNotFound (RFC1918 isn't in any
	// commercial DB) or, worse, a stale "test record" from the .mmdb.
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return Result{}, ErrNotFound
	}

	city, err := r.db.City(ip)
	if err != nil {
		return Result{}, fmt.Errorf("geoip: lookup %q: %w", ipText, err)
	}
	if city == nil || city.Country.IsoCode == "" {
		return Result{}, ErrNotFound
	}

	out := Result{
		CountryCode: city.Country.IsoCode,
		CountryName: city.Country.Names["en"],
	}
	// Subdivisions[0] is the most-specific known administrative
	// division (state/province/region). Some entries lack it (small
	// island nations, free-tier DB rows) — fall back to the country
	// slug in that case so we still get something filterable.
	if len(city.Subdivisions) > 0 {
		out.RegionName = city.Subdivisions[0].Names["en"]
	}
	out.RegionSlug = makeRegionSlug(out.CountryCode, out.RegionName)
	return out, nil
}

// makeRegionSlug produces the lower-snake slug used by the
// providers.region_slug column. Examples:
//
//	("TR", "Istanbul")     → "tr-istanbul"
//	("US", "California")   → "us-california"
//	("US", "")             → "us"
//	("DE", "Berlin Stadt") → "de-berlin-stadt"
func makeRegionSlug(country, region string) string {
	cc := strings.ToLower(strings.TrimSpace(country))
	if cc == "" {
		return ""
	}
	rn := strings.ToLower(strings.TrimSpace(region))
	if rn == "" {
		return cc
	}
	// Replace anything not [a-z0-9] with '-' then collapse runs.
	var b strings.Builder
	b.Grow(len(cc) + 1 + len(rn))
	b.WriteString(cc)
	b.WriteByte('-')
	prevDash := false
	for _, r := range rn {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	return out
}

// --- noop -------------------------------------------------------------------

// NoopLookuper is the fallback used when no .mmdb is configured. Every
// Lookup returns ErrUnavailable so callers can short-circuit without
// writing empty country/region rows on top of previously-good values.
type NoopLookuper struct{}

// Lookup always returns ErrUnavailable.
func (NoopLookuper) Lookup(string) (Result, error) {
	return Result{}, ErrUnavailable
}

// --- helpers ---------------------------------------------------------------

// ExtractClientIP returns the most-trustworthy public source IP for an
// HTTP request, walking the standard forwarding-header chain in
// best-to-worst order:
//
//  1. RFC 7239 `Forwarded: for=<ip>` (the modern standard, emitted by
//     newer proxies + some service meshes)
//  2. `X-Forwarded-For` left-most entry (the legacy de-facto standard,
//     emitted by Traefik, nginx, ALB, Cloudflare, …)
//  3. `X-Real-Ip` / `X-Real-IP` (single value, emitted by nginx,
//     some Cilium Gateway flavours, and certain Traefik plugins)
//  4. `Cf-Connecting-IP` / `True-Client-IP` (Cloudflare + Akamai;
//     unused on Phase-0 but cheap to support so the same helper works
//     on a future Cloudflare-fronted deploy)
//  5. `remoteAddr` host part (the connection peer — usually an
//     in-cluster RFC1918 IP for a coordinator behind Traefik, but
//     better than an empty string for diagnostic logging)
//
// Returns "" when nothing is plausible.
//
// We trust the forwarded-headers here because Traefik (and every other
// edge the platform ships with) overwrites them on ingress; daemons
// cannot forge them because their connection terminates at Traefik
// first. If a future deploy puts providers-svc behind an untrusted L4
// LB the caller should use a more careful chain-walker.
//
// Refs #381 — added RFC 7239 `Forwarded` + peer-addr fallback after
// PR #378's heartbeat geo path silently no-op'd on hatice's daemon
// because `clientIP` was empty (cause traced to the mothership
// Traefik's `forwardedHeaders` static config; the multi-header walk
// below also reduces the blast radius of any single upstream stripping
// one of them).
func ExtractClientIP(getHeader func(string) string, remoteAddr string) string {
	if getHeader != nil {
		if v := parseForwardedFor(getHeader("Forwarded")); v != "" {
			return v
		}
		if xff := strings.TrimSpace(getHeader("X-Forwarded-For")); xff != "" {
			// Left-most entry is the original client.
			if i := strings.IndexByte(xff, ','); i > 0 {
				return strings.TrimSpace(xff[:i])
			}
			return xff
		}
		if xri := strings.TrimSpace(getHeader("X-Real-Ip")); xri != "" {
			return xri
		}
		if xri := strings.TrimSpace(getHeader("X-Real-IP")); xri != "" {
			return xri
		}
		if cf := strings.TrimSpace(getHeader("Cf-Connecting-Ip")); cf != "" {
			return cf
		}
		if cf := strings.TrimSpace(getHeader("True-Client-Ip")); cf != "" {
			return cf
		}
	}
	if remoteAddr == "" {
		return ""
	}
	// RemoteAddr is "host:port" for HTTP/1, "[v6]:port" for HTTP/2.
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// parseForwardedFor extracts the left-most `for=...` value from an RFC
// 7239 `Forwarded` header. Returns the bare IP literal (IPv4 dotted
// quad or IPv6 hex form), stripping quotes, brackets, and port suffix
// per the grammar in §4. Returns "" when the header is empty or
// contains no `for=` element.
//
// Examples (input → output):
//
//	`for=203.0.113.7`                           → `203.0.113.7`
//	`for="203.0.113.7:54321", for=10.0.0.5`     → `203.0.113.7`
//	`for="[2001:db8::1]:54321"; proto=https`    → `2001:db8::1`
//	`proto=https; by=10.0.0.5`                  → `` (no for=)
//
// Tolerant of mixed-case directive names (`For=`, `FOR=`) per §6 of
// the RFC. Does NOT validate the address is a real IP — that's the
// caller's job (Lookup rejects malformed input).
func parseForwardedFor(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return ""
	}
	// Left-most element wins (original client). Elements separated by ','.
	if i := strings.IndexByte(h, ','); i >= 0 {
		h = h[:i]
	}
	// Parameters within an element separated by ';'.
	for _, part := range strings.Split(h, ";") {
		part = strings.TrimSpace(part)
		// Case-insensitive `for=` prefix per RFC 7239 §6.
		if len(part) < 4 || !strings.EqualFold(part[:4], "for=") {
			continue
		}
		v := strings.TrimSpace(part[4:])
		// Strip surrounding quotes per quoted-string grammar.
		v = strings.Trim(v, `"`)
		// `[2001:db8::1]:54321` → strip brackets + port.
		if strings.HasPrefix(v, "[") {
			if end := strings.IndexByte(v, ']'); end > 0 {
				return v[1:end]
			}
			return strings.TrimPrefix(v, "[")
		}
		// `203.0.113.7:54321` → strip port. IPv4 only has one ':'.
		if i := strings.IndexByte(v, ':'); i > 0 && strings.Count(v, ":") == 1 {
			return v[:i]
		}
		return v
	}
	return ""
}
