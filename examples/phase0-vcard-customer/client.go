// Phase 0 customer demo — vCard LinkedIn enrichment via iogrid proxy.
//
// Build:
//
//	go build -o vcard-enrich ./examples/phase0-vcard-customer
//
// Run:
//
//	IOGRID_API_KEY=ig_live_... \
//	IOGRID_WORKSPACE=vcard-prod \
//	./vcard-enrich -vanity satyanadella
//
// What this binary does:
//
//  1. Reads the customer API key + workspace handle from the env.
//  2. Opens a SOCKS5 (RFC 1928 + RFC 1929 USERPASS) connection to the
//     iogrid proxy gateway at $PROXY_URL (default proxy.iogrid.org:443).
//     The gateway resolves the API key via billing-svc.ValidateApiKey,
//     pre-flights the destination via antiabuse-svc.CheckUrl, and
//     dispatches the request to a provider whose `social-intel` opt-in
//     matches LinkedIn.
//  3. Through the SOCKS5 tunnel, performs an HTTP GET on
//     https://www.linkedin.com/in/<vanity>. The TLS handshake is
//     end-to-end between this client and LinkedIn — the proxy never
//     sees the plaintext.
//  4. Parses the response HTML with a permissive walker
//     (golang.org/x/net/html — no headless browser) and extracts the
//     name, title, and current company from the canonical SSR markup.
//  5. Emits a single JSON object on stdout with the extracted fields
//     plus per-request latency.
//
// Intended usage shapes:
//   - Stand-alone CLI (the example above).
//   - Library: call EnrichVanity from another Go program directly.
//   - CronJob: see kustomization.yaml + cronjob.yaml in this directory.
//
// LinkedIn ToS / scraping gray area: see README.md. This demo is a
// reference implementation. Production use carries the customer's own
// ToS-compliance responsibility — exactly the same model every
// residential-proxy vendor (Bright Data / Oxylabs / Smartproxy)
// operates under.
package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// EnrichResult is the JSON shape this binary emits.
type EnrichResult struct {
	Vanity          string `json:"vanity"`
	Name            string `json:"name,omitempty"`
	Title           string `json:"title,omitempty"`
	Company         string `json:"company,omitempty"`
	LatencyMS       int64  `json:"latency_ms"`
	ProxyUsed       bool   `json:"proxy_used"`
	ProviderCountry string `json:"provider_country,omitempty"`
}

// Config bundles every knob the demo respects. Constructed by
// loadConfig from env + flags.
type Config struct {
	APIKey        string        // IOGRID_API_KEY (SOCKS5 password)
	Workspace     string        // IOGRID_WORKSPACE (SOCKS5 username)
	ProxyHostPort string        // PROXY_URL (default proxy.iogrid.org:443)
	Vanity        string        // -vanity flag (LinkedIn public handle)
	Timeout       time.Duration // -timeout flag
	UserAgent     string        // -user-agent flag
	DryRun        bool          // -dry-run flag (no network; for CI tests)
}

func loadConfig(args []string) (*Config, error) {
	fs := flag.NewFlagSet("vcard-enrich", flag.ContinueOnError)
	vanity := fs.String("vanity", "", "LinkedIn vanity handle (required, e.g. 'satyanadella')")
	timeout := fs.Duration("timeout", 10*time.Second, "per-request timeout")
	ua := fs.String("user-agent", "Mozilla/5.0 (compatible; vCardEnrich/0.1; +https://vcard.dynolabs.io/bot)", "User-Agent header")
	dryRun := fs.Bool("dry-run", false, "skip the network round-trip (CI mode — uses bundled fixture HTML)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *vanity == "" && !*dryRun {
		return nil, errors.New("-vanity is required")
	}
	cfg := &Config{
		APIKey:        os.Getenv("IOGRID_API_KEY"),
		Workspace:     os.Getenv("IOGRID_WORKSPACE"),
		ProxyHostPort: os.Getenv("PROXY_URL"),
		Vanity:        *vanity,
		Timeout:       *timeout,
		UserAgent:     *ua,
		DryRun:        *dryRun,
	}
	if cfg.ProxyHostPort == "" {
		cfg.ProxyHostPort = "proxy.iogrid.org:443"
	}
	if !cfg.DryRun {
		if cfg.APIKey == "" {
			return nil, errors.New("IOGRID_API_KEY env var is required (issued by the customer onboarding flow — see docs/PHASE0_FIRST_CUSTOMER.md)")
		}
		if cfg.Workspace == "" {
			return nil, errors.New("IOGRID_WORKSPACE env var is required")
		}
	}
	return cfg, nil
}

// newProxyClient returns an *http.Client that tunnels every request
// through the iogrid SOCKS5 entry point.
//
// Wire protocol (see issue #265 and the README "How the proxy is wired"
// diagram): Traefik terminates TLS at the edge on proxy.iogrid.org:443
// (IngressRouteTCP with HostSNI(`proxy.iogrid.org`) and the public ACME
// cert) and forwards plain TCP to the in-cluster proxy-gateway which
// speaks SOCKS5. Therefore the client must:
//
//  1. tls.Dial to proxy.iogrid.org:443 (SNI = host) — this satisfies
//     Traefik's HostSNI router and establishes the edge-TLS layer.
//  2. Speak RFC 1928 SOCKS5 + RFC 1929 USERPASS on top of the *tls.Conn*.
//     User = workspace handle, Password = ig_live API key.
//  3. After CONNECT succeeds the same byte stream tunnels the customer's
//     end-to-end TLS handshake with LinkedIn — the proxy still never
//     sees plaintext, the gateway only sees opaque bytes.
//
// Prior to this fix the client called golang.org/x/net/proxy.SOCKS5
// which dials raw TCP. Against a Traefik TLS-terminating ingress that
// hangs forever — Traefik buffers waiting for a ClientHello while the
// SOCKS5 greeting is interpreted as malformed TLS, the connection idles
// out, and the caller sees `context deadline exceeded`. The Go x/net
// proxy package has no hook to swap in a pre-dialled net.Conn, so we
// inline a minimal SOCKS5 client below.
func newProxyClient(cfg *Config) (*http.Client, error) {
	host, _, err := net.SplitHostPort(cfg.ProxyHostPort)
	if err != nil {
		return nil, fmt.Errorf("PROXY_URL must be host:port — got %q: %w", cfg.ProxyHostPort, err)
	}
	tr := &http.Transport{
		// DialContext does, per request:
		//   1. tls.Dial to the iogrid edge (Traefik strips TLS),
		//   2. SOCKS5 greet + USERPASS auth + CONNECT to `addr`,
		//   3. return the *tls.Conn as a tunnelled byte stream.
		// http.Transport then performs the destination TLS handshake
		// (E2E to LinkedIn) over this stream — see the layering diagram
		// in README.md.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialThroughTLSSOCKS5(ctx, cfg.ProxyHostPort, host, cfg.Workspace, cfg.APIKey, network, addr)
		},
		// HTTPS handshakes happen E2E — the proxy MUST NOT terminate
		// TLS, otherwise the customer would have to trust the proxy's
		// CA. Force HTTP/1.1 to keep header parsing deterministic for
		// the HTML extractor.
		ForceAttemptHTTP2: false,
	}
	return &http.Client{
		Timeout:   cfg.Timeout,
		Transport: tr,
	}, nil
}

// dialThroughTLSSOCKS5 performs:
//   - tls.Dial proxyHostPort with SNI = sni (Traefik HostSNI match),
//   - RFC 1928 SOCKS5 greet + RFC 1929 USERPASS auth on the *tls.Conn,
//   - CONNECT to `addr` (destination passed by http.Transport).
//
// It returns the live tls.Conn — anything written next goes straight
// through the tunnel to `addr`.
//
// network must be "tcp", "tcp4", or "tcp6"; SOCKS5 doesn't support
// anything else and http.Transport never asks for anything else.
func dialThroughTLSSOCKS5(ctx context.Context, proxyHostPort, sni, user, pass, network, addr string) (net.Conn, error) {
	switch network {
	case "tcp", "tcp4", "tcp6":
	default:
		return nil, fmt.Errorf("socks5: unsupported network %q", network)
	}

	// Step 1 — TLS handshake to the iogrid edge.
	d := &tls.Dialer{
		Config: &tls.Config{
			ServerName: sni,
			MinVersion: tls.VersionTLS12,
		},
	}
	rawConn, err := d.DialContext(ctx, "tcp", proxyHostPort)
	if err != nil {
		return nil, fmt.Errorf("tls.Dial proxy: %w", err)
	}
	conn := rawConn.(*tls.Conn)

	// Propagate ctx cancellation into the connection so a context
	// timeout during the SOCKS5 handshake aborts cleanly. We clear
	// the deadline on success.
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}

	if err := socks5Handshake(conn, user, pass, addr); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Reset deadline so http.Transport's per-request timeouts win.
	_ = conn.SetDeadline(time.Time{})
	return conn, nil
}

// socks5Handshake performs greet + USERPASS auth + CONNECT against an
// existing connection. See RFC 1928 §3 (greet/method-select), §4
// (CONNECT request/reply), and RFC 1929 (USERPASS sub-negotiation).
func socks5Handshake(conn net.Conn, user, pass, addr string) error {
	// RFC 1928 §3 — version identifier / method-selection.
	// We advertise only method 0x02 (USERPASS) — the iogrid gateway
	// rejects NoAuth on Phase 0 so there's no point listing it.
	if _, err := conn.Write([]byte{0x05, 0x01, 0x02}); err != nil {
		return fmt.Errorf("socks5 greet: %w", err)
	}
	var sel [2]byte
	if _, err := io.ReadFull(conn, sel[:]); err != nil {
		return fmt.Errorf("socks5 read method select: %w", err)
	}
	if sel[0] != 0x05 {
		return fmt.Errorf("socks5: bad version %#x in method-select reply", sel[0])
	}
	switch sel[1] {
	case 0x02:
		// proceed to USERPASS
	case 0xff:
		return errors.New("socks5: server rejected all offered auth methods (expected USERPASS)")
	default:
		return fmt.Errorf("socks5: server selected method %#x, expected 0x02 (USERPASS)", sel[1])
	}

	// RFC 1929 — USERPASS sub-negotiation.
	if len(user) > 255 || len(pass) > 255 {
		return errors.New("socks5: username/password exceed 255 bytes (RFC 1929 limit)")
	}
	req := make([]byte, 0, 3+len(user)+len(pass))
	req = append(req, 0x01)            // VER (sub-negotiation)
	req = append(req, byte(len(user))) // ULEN
	req = append(req, user...)         // UNAME
	req = append(req, byte(len(pass))) // PLEN
	req = append(req, pass...)         // PASSWD
	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("socks5 userpass write: %w", err)
	}
	var authResp [2]byte
	if _, err := io.ReadFull(conn, authResp[:]); err != nil {
		return fmt.Errorf("socks5 read userpass reply: %w", err)
	}
	if authResp[0] != 0x01 {
		return fmt.Errorf("socks5: bad userpass version %#x", authResp[0])
	}
	if authResp[1] != 0x00 {
		return fmt.Errorf("socks5: authentication failed (status %#x)", authResp[1])
	}

	// RFC 1928 §4 — CONNECT request.
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("socks5: bad destination %q: %w", addr, err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return fmt.Errorf("socks5: bad destination port %q: %w", portStr, err)
	}

	connectReq := []byte{0x05, 0x01, 0x00} // VER, CMD=CONNECT, RSV
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			connectReq = append(connectReq, 0x01) // ATYP=IPv4
			connectReq = append(connectReq, v4...)
		} else {
			connectReq = append(connectReq, 0x04) // ATYP=IPv6
			connectReq = append(connectReq, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return fmt.Errorf("socks5: hostname %q exceeds 255 bytes", host)
		}
		connectReq = append(connectReq, 0x03)            // ATYP=DOMAINNAME
		connectReq = append(connectReq, byte(len(host))) // length-prefixed
		connectReq = append(connectReq, host...)
	}
	var portBytes [2]byte
	binary.BigEndian.PutUint16(portBytes[:], uint16(port))
	connectReq = append(connectReq, portBytes[:]...)

	if _, err := conn.Write(connectReq); err != nil {
		return fmt.Errorf("socks5 CONNECT write: %w", err)
	}

	// CONNECT reply: VER REP RSV ATYP BND.ADDR BND.PORT.
	// Read the fixed 4-byte header then a variable BND.ADDR + 2-byte port.
	var hdr [4]byte
	if _, err := io.ReadFull(conn, hdr[:]); err != nil {
		return fmt.Errorf("socks5 CONNECT read header: %w", err)
	}
	if hdr[0] != 0x05 {
		return fmt.Errorf("socks5: bad CONNECT reply version %#x", hdr[0])
	}
	if hdr[1] != 0x00 {
		return fmt.Errorf("socks5: CONNECT rejected (REP=%s)", socks5RepName(hdr[1]))
	}
	// Drain BND.ADDR + port so the byte stream is aligned for the caller.
	switch hdr[3] {
	case 0x01: // IPv4
		var skip [4 + 2]byte
		if _, err := io.ReadFull(conn, skip[:]); err != nil {
			return fmt.Errorf("socks5 CONNECT read bnd v4: %w", err)
		}
	case 0x04: // IPv6
		var skip [16 + 2]byte
		if _, err := io.ReadFull(conn, skip[:]); err != nil {
			return fmt.Errorf("socks5 CONNECT read bnd v6: %w", err)
		}
	case 0x03: // DOMAINNAME
		var l [1]byte
		if _, err := io.ReadFull(conn, l[:]); err != nil {
			return fmt.Errorf("socks5 CONNECT read bnd domain len: %w", err)
		}
		skip := make([]byte, int(l[0])+2)
		if _, err := io.ReadFull(conn, skip); err != nil {
			return fmt.Errorf("socks5 CONNECT read bnd domain: %w", err)
		}
	default:
		return fmt.Errorf("socks5: unexpected ATYP %#x in CONNECT reply", hdr[3])
	}
	return nil
}

// socks5RepName turns a SOCKS5 REP byte (RFC 1928 §6) into a human label.
func socks5RepName(b byte) string {
	switch b {
	case 0x00:
		return "succeeded"
	case 0x01:
		return "general SOCKS server failure"
	case 0x02:
		return "connection not allowed by ruleset"
	case 0x03:
		return "network unreachable"
	case 0x04:
		return "host unreachable"
	case 0x05:
		return "connection refused"
	case 0x06:
		return "TTL expired"
	case 0x07:
		return "command not supported"
	case 0x08:
		return "address type not supported"
	default:
		return fmt.Sprintf("unknown(%#x)", b)
	}
}

// EnrichVanity fetches the LinkedIn profile page for vanity and
// extracts the name / title / company fields. Network errors are
// returned verbatim; an HTTP non-200 is returned as an error too so
// callers can distinguish transport problems from parse problems.
func EnrichVanity(ctx context.Context, cfg *Config, hc *http.Client) (*EnrichResult, error) {
	if cfg.DryRun {
		return enrichFixture(cfg.Vanity), nil
	}
	target := "https://www.linkedin.com/in/" + url.PathEscape(cfg.Vanity)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	t0 := time.Now()
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("proxy dial / fetch: %w", err)
	}
	defer resp.Body.Close()
	latency := time.Since(t0)

	if resp.StatusCode == http.StatusForbidden {
		// LinkedIn serves a 403 when the residential IP has been
		// rate-limited or flagged. The dispatcher picks a different
		// provider on the next call. Surface the status to the caller.
		return nil, fmt.Errorf("upstream rejected the request (status=%d) — try again, the scheduler will rotate to a new provider", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 4 MiB ceiling — a profile page is ~250 KB
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	r := ExtractProfile(string(body))
	r.Vanity = cfg.Vanity
	r.LatencyMS = latency.Milliseconds()
	r.ProxyUsed = true
	// The provider's country is surfaced by the gateway via a custom
	// response header. Best-effort — missing on dev fixtures.
	if v := resp.Header.Get("X-Iogrid-Provider-Country"); v != "" {
		r.ProviderCountry = v
	}
	return r, nil
}

// ExtractProfile walks the LinkedIn profile HTML and extracts the
// public name, title, and current company. The strategy is permissive
// and matches the canonical SSR markup as of 2026-05; the helpers
// fall back to <title> + meta tags so a server-rendered variant still
// gives us at least the name.
//
// Exported so tests can hit it directly with fixture HTML.
func ExtractProfile(body string) *EnrichResult {
	r := &EnrichResult{}
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return r
	}

	// Pass 1: pull og:title + og:description meta tags (most reliable;
	// LinkedIn ships them on every public profile page).
	walk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "meta" {
			return
		}
		prop := attr(n, "property")
		c := attr(n, "content")
		if c == "" {
			return
		}
		switch prop {
		case "og:title":
			// "Satya Nadella - Chairman and CEO at Microsoft - Microsoft | LinkedIn"
			parts := splitMetaTitle(c)
			if len(parts) > 0 && r.Name == "" {
				r.Name = strings.TrimSpace(parts[0])
			}
			if len(parts) > 1 && r.Title == "" {
				r.Title = strings.TrimSpace(parts[1])
			}
			if len(parts) > 2 && r.Company == "" {
				r.Company = strings.TrimSpace(parts[2])
			}
		case "og:description":
			// Some profiles ship a richer description here; ignore for now.
		}
	})

	// Pass 2: derive Company from Title if og:title only had 2 parts
	// ("Name - Title at Company"). The split-on-" at " heuristic is
	// imperfect but matches LinkedIn's canonical convention.
	if r.Company == "" && r.Title != "" {
		if idx := strings.Index(strings.ToLower(r.Title), " at "); idx > 0 {
			r.Company = strings.TrimSpace(r.Title[idx+4:])
		}
	}

	// Pass 3: <title> fallback. Strip the trailing " | LinkedIn".
	if r.Name == "" {
		walk(doc, func(n *html.Node) {
			if n.Type == html.ElementNode && n.Data == "title" && n.FirstChild != nil {
				t := strings.TrimSpace(n.FirstChild.Data)
				t = strings.TrimSuffix(t, " | LinkedIn")
				if r.Name == "" {
					r.Name = t
				}
			}
		})
	}
	return r
}

// splitMetaTitle splits LinkedIn's "Name - Title - Company | LinkedIn"
// envelope. Returns up to 3 trimmed segments.
func splitMetaTitle(s string) []string {
	// Strip the trailing " | LinkedIn" if present.
	if i := strings.LastIndex(s, "|"); i > 0 {
		s = strings.TrimSpace(s[:i])
	}
	parts := strings.Split(s, " - ")
	out := make([]string, 0, 3)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
		if len(out) == 3 {
			break
		}
	}
	return out
}

// walk is a depth-first visitor over the HTML tree. Visit is called
// once per node; we don't bother recursing further when we've found
// every field — the perf overhead is negligible for a 250 KB doc.
func walk(n *html.Node, visit func(*html.Node)) {
	visit(n)
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		walk(c, visit)
	}
}

// attr returns the value of the named attribute or "".
func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// enrichFixture returns a deterministic result for -dry-run mode (CI
// smoke + unit tests). The HTML matches a representative LinkedIn
// profile page in 2026-05.
func enrichFixture(vanity string) *EnrichResult {
	r := ExtractProfile(fixtureHTML)
	r.Vanity = vanity
	r.LatencyMS = 0
	r.ProxyUsed = false
	r.ProviderCountry = "FIXTURE"
	return r
}

// fixtureHTML is the canonical 2026-05 LinkedIn SSR shape — kept
// minimal but realistic so ExtractProfile is exercised against the
// same envelope production hits. Used by both -dry-run mode and the
// unit tests.
const fixtureHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta property="og:title" content="Satya Nadella - Chairman and CEO at Microsoft - Microsoft | LinkedIn">
<meta property="og:description" content="As Chairman and Chief Executive Officer of Microsoft...">
<title>Satya Nadella - Microsoft | LinkedIn</title>
</head>
<body>
<main>
<h1 class="text-heading-xlarge">Satya Nadella</h1>
<div class="text-body-medium break-words">Chairman and CEO at Microsoft</div>
</main>
</body>
</html>`

func main() {
	cfg, err := loadConfig(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "config error:", err)
		os.Exit(2)
	}
	var hc *http.Client
	if !cfg.DryRun {
		hc, err = newProxyClient(cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "proxy client:", err)
			os.Exit(1)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	out, err := EnrichVanity(ctx, cfg, hc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "enrich:", err)
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "encode:", err)
		os.Exit(1)
	}
}
