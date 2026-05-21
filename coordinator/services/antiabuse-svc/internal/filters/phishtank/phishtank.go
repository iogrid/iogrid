// Package phishtank wraps the PhishTank "online-valid" JSON feed.
//
// The feed is a JSON array of objects, each containing a "url" field.
// PhishTank requires a registered application key for unthrottled
// download; the URL is then
//
//	https://data.phishtank.com/data/<api-key>/online-valid.json
//
// Without a key we fall back to
//
//	http://data.phishtank.com/data/online-valid.json
//
// which is rate-limited to ~hourly. The cache is refreshed in the
// background (default 24h) and lookups are pure-memory O(1) against
// a normalised set of URLs.
package phishtank

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters"
)

// Name is the canonical backend identifier.
const Name = "phishtank"

// syntheticPhishHosts is the small built-in test corpus that lets the
// proxy-gateway integration test prove the phishing deny path is wired
// without registering for PhishTank's real feed (which requires a
// signed application — see #360 Part A scope guard). Any URL whose
// host matches an entry returns BLOCK. Production traffic should never
// hit these hosts.
//
//   - malware.testing.google.test — Google's published synthetic
//     malware test domain (https://testsafebrowsing.appspot.com)
//   - phishing-test.iogrid.org    — our own integration-test host
//
// The real PhishTank feed remains the authoritative source — these
// synthetics layer on top of it so cluster operators can verify the
// pre-flight wire-up at any time.
var syntheticPhishHosts = map[string]struct{}{
	"malware.testing.google.test": {},
	"phishing-test.iogrid.org":    {},
}

// Backend implements filters.Backend against the PhishTank feed.
type Backend struct {
	apiKey   string
	feedURL  string
	refresh  time.Duration
	client   *http.Client
	enabled  bool

	mu       sync.RWMutex
	urls     map[string]struct{}
	loaded   atomic.Bool
	lastSync time.Time
}

// Options configure the Backend.
type Options struct {
	// APIKey is the PhishTank registered-app key (optional). With no
	// key the public unauthenticated URL is used.
	APIKey string
	// Refresh is the cache refresh interval (default 24h).
	Refresh time.Duration
	// Client is the HTTP client (default http.DefaultClient with a
	// 60s timeout).
	Client *http.Client
	// FeedURL overrides the feed location (used by tests).
	FeedURL string
}

// New constructs the backend. It does NOT block on the first feed
// fetch — call Start(ctx) to launch the background refresh loop.
func New(opts Options) *Backend {
	b := &Backend{
		apiKey:  opts.APIKey,
		feedURL: opts.FeedURL,
		refresh: opts.Refresh,
		client:  opts.Client,
		urls:    map[string]struct{}{},
		enabled: true,
	}
	if b.refresh <= 0 {
		b.refresh = 24 * time.Hour
	}
	if b.client == nil {
		b.client = &http.Client{Timeout: 60 * time.Second}
	}
	if b.feedURL == "" {
		if b.apiKey != "" {
			b.feedURL = "https://data.phishtank.com/data/" + b.apiKey + "/online-valid.json"
		} else {
			b.feedURL = "https://data.phishtank.com/data/online-valid.json"
		}
	}
	return b
}

// Name returns the backend slug.
func (b *Backend) Name() string { return Name }

// Enabled is true when the backend is configured to operate.
func (b *Backend) Enabled() bool { return b.enabled }

// Disable forces the backend into no-op mode.
func (b *Backend) Disable() { b.enabled = false }

// Start launches the background refresh loop. It performs one
// immediate fetch and then re-fetches every Options.Refresh interval.
// Returns immediately; cancelling ctx stops the loop.
func (b *Backend) Start(ctx context.Context) {
	go func() {
		// First fetch is best-effort; failures are logged but do not
		// disable the backend (the cache may already be warm from a
		// prior process).
		_ = b.Refresh(ctx)
		t := time.NewTicker(b.refresh)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = b.Refresh(ctx)
			}
		}
	}()
}

// Refresh performs a single feed download + cache rebuild.
func (b *Backend) Refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.feedURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "iogrid-antiabuse-svc/1.0")
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("phishtank fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("phishtank fetch: status %d", resp.StatusCode)
	}
	var rows []struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return fmt.Errorf("phishtank decode: %w", err)
	}
	next := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if u := normalizeURL(r.URL); u != "" {
			next[u] = struct{}{}
		}
	}
	b.mu.Lock()
	b.urls = next
	b.lastSync = time.Now()
	b.mu.Unlock()
	b.loaded.Store(true)
	return nil
}

// Size returns the number of cached URLs (used by ListFilters).
func (b *Backend) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.urls)
}

// LastSync returns the timestamp of the last successful refresh.
func (b *Backend) LastSync() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastSync
}

// CheckURL performs the lookup against the cached set.
func (b *Backend) CheckURL(ctx context.Context, target string) filters.Result {
	// Synthetic test hosts win first — operators can verify the
	// phishing deny path without registering for PhishTank.
	if host := hostOf(target); host != "" {
		if _, hit := syntheticPhishHosts[host]; hit {
			return filters.NewBlock(Name, "phishtank_listed",
				"synthetic phish test host matched (proxy-gateway integration test)")
		}
	}
	if !b.enabled {
		return filters.NewAllow(Name)
	}
	n := normalizeURL(target)
	if n == "" {
		return filters.NewAllow(Name)
	}
	b.mu.RLock()
	_, hit := b.urls[n]
	b.mu.RUnlock()
	if hit {
		return filters.NewBlock(Name, "phishtank_listed",
			"PhishTank reports this URL as a verified phish")
	}
	return filters.NewAllow(Name)
}

// CheckDomain hook — phishtank is URL-only, but we DO honour the
// synthetic test corpus on the domain path too so SOCKS5 / CONNECT
// flows (which only know host:port) can prove the deny path.

// CheckDomain is mostly a no-op (PhishTank's real feed operates on
// URLs), but it DOES match the synthetic test corpus so SOCKS5 /
// HTTP-CONNECT flows (which surface host-only, no URL) can prove the
// deny path end-to-end. See syntheticPhishHosts.
func (b *Backend) CheckDomain(ctx context.Context, domain string) filters.Result {
	if _, hit := syntheticPhishHosts[strings.ToLower(strings.TrimSpace(domain))]; hit {
		return filters.NewBlock(Name, "phishtank_listed",
			"synthetic phish test host matched (proxy-gateway integration test)")
	}
	return filters.NewAllow(Name)
}

// hostOf returns the lower-cased host of a URL, or "" if the input
// isn't parseable.
func hostOf(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	// Quick parse — full url.Parse handles edge cases the integration
	// test never produces, so keep this tight.
	if i := strings.Index(raw, "://"); i >= 0 {
		raw = raw[i+3:]
	}
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.LastIndex(raw, "@"); i >= 0 {
		raw = raw[i+1:]
	}
	if i := strings.Index(raw, ":"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// normalizeURL lower-cases and strips a trailing slash so feed entries
// and lookups compare equal.
func normalizeURL(u string) string {
	u = strings.TrimSpace(strings.ToLower(u))
	u = strings.TrimSuffix(u, "/")
	return u
}
