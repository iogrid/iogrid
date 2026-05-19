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
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/proxy"
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
// through the iogrid SOCKS5 entry point. The customer API key is sent
// as the SOCKS5 password (RFC 1929 USERPASS), the workspace handle as
// the username. Anything else fails the gateway's pre-flight check.
func newProxyClient(cfg *Config) (*http.Client, error) {
	auth := &proxy.Auth{User: cfg.Workspace, Password: cfg.APIKey}
	dialer, err := proxy.SOCKS5("tcp", cfg.ProxyHostPort, auth, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("socks5 dialer: %w", err)
	}
	tr := &http.Transport{
		// Use the SOCKS5 dialer for outbound TCP. Context-aware Dial
		// is required because Go's http.Transport calls DialContext;
		// the proxy.SOCKS5 dialer satisfies the ContextDialer interface
		// in golang.org/x/net.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if cd, ok := dialer.(proxy.ContextDialer); ok {
				return cd.DialContext(ctx, network, addr)
			}
			return dialer.Dial(network, addr)
		},
		// We let HTTPS handshakes happen E2E — the proxy MUST NOT
		// terminate TLS, otherwise the customer would have to trust
		// the proxy's CA. Force HTTP/1.1 to keep header parsing
		// deterministic for the HTML extractor.
		ForceAttemptHTTP2: false,
	}
	return &http.Client{
		Timeout:   cfg.Timeout,
		Transport: tr,
	}, nil
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
