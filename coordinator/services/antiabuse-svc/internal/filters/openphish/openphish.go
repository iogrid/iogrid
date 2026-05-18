// Package openphish wraps the public OpenPhish feed
// (https://openphish.com/feed.txt). The feed is a newline-delimited
// list of malicious URLs, refreshed by OpenPhish every few hours.
//
// No authentication is required. The cache is rebuilt on a
// configurable interval (default 6h) and lookups are pure-memory.
package openphish

import (
	"bufio"
	"context"
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
const Name = "openphish"

// DefaultFeedURL is the public community feed.
const DefaultFeedURL = "https://openphish.com/feed.txt"

// Backend implements filters.Backend against the OpenPhish feed.
type Backend struct {
	feedURL string
	refresh time.Duration
	client  *http.Client
	enabled bool

	mu       sync.RWMutex
	urls     map[string]struct{}
	lastSync time.Time
	loaded   atomic.Bool
}

// Options configure the Backend.
type Options struct {
	// Refresh is the cache refresh interval (default 6h).
	Refresh time.Duration
	// Client is the HTTP client (default 60s timeout).
	Client *http.Client
	// FeedURL overrides DefaultFeedURL (used by tests).
	FeedURL string
}

// New constructs the backend.
func New(opts Options) *Backend {
	b := &Backend{
		feedURL: opts.FeedURL,
		refresh: opts.Refresh,
		client:  opts.Client,
		urls:    map[string]struct{}{},
		enabled: true,
	}
	if b.feedURL == "" {
		b.feedURL = DefaultFeedURL
	}
	if b.refresh <= 0 {
		b.refresh = 6 * time.Hour
	}
	if b.client == nil {
		b.client = &http.Client{Timeout: 60 * time.Second}
	}
	return b
}

// Name returns the backend slug.
func (b *Backend) Name() string { return Name }

// Enabled reports whether the backend is active. OpenPhish has no API
// key, so it's always enabled unless explicitly disabled.
func (b *Backend) Enabled() bool { return b.enabled }

// Disable forces the backend into no-op mode.
func (b *Backend) Disable() { b.enabled = false }

// Start launches the background refresh loop.
func (b *Backend) Start(ctx context.Context) {
	go func() {
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

// Refresh fetches the feed and rebuilds the cache.
func (b *Backend) Refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.feedURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "iogrid-antiabuse-svc/1.0")
	resp, err := b.client.Do(req)
	if err != nil {
		return fmt.Errorf("openphish fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("openphish fetch: status %d", resp.StatusCode)
	}
	next := map[string]struct{}{}
	scanner := bufio.NewScanner(resp.Body)
	// OpenPhish lines are short; default buffer (64KiB) is fine, but
	// raise the cap to be safe against long URLs.
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)
	for scanner.Scan() {
		line := normalizeURL(scanner.Text())
		if line == "" {
			continue
		}
		next[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("openphish scan: %w", err)
	}
	b.mu.Lock()
	b.urls = next
	b.lastSync = time.Now()
	b.mu.Unlock()
	b.loaded.Store(true)
	return nil
}

// Size returns the number of cached URLs.
func (b *Backend) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.urls)
}

// LastSync returns the last successful refresh.
func (b *Backend) LastSync() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastSync
}

// CheckURL performs the lookup against the cached set.
func (b *Backend) CheckURL(ctx context.Context, target string) filters.Result {
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
		return filters.NewBlock(Name, "openphish_listed",
			"OpenPhish reports this URL as a confirmed phish")
	}
	return filters.NewAllow(Name)
}

// CheckDomain is a no-op: OpenPhish is URL-only.
func (b *Backend) CheckDomain(ctx context.Context, domain string) filters.Result {
	return filters.NewAllow(Name)
}

func normalizeURL(u string) string {
	u = strings.TrimSpace(strings.ToLower(u))
	u = strings.TrimSuffix(u, "/")
	return u
}
