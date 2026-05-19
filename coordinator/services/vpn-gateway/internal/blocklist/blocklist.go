// Package blocklist implements the DNS-level ad/tracker block list used by
// the Pro tier of the consumer VPN gateway.
//
// The blocklist is loaded from one or more sources (file path, HTTP URL, or
// in-memory []string). The de-facto industry source is StevenBlack/hosts
// (https://github.com/StevenBlack/hosts) at ~150K entries. Format is the
// standard hosts(5):
//
//	0.0.0.0 doubleclick.net
//	0.0.0.0 google-analytics.com
//
// Lines starting with '#' are comments, blank lines are ignored, only the
// hostname column is retained. The "0.0.0.0" IP column is discarded — the
// VPN gateway responds NXDOMAIN, not a sinkhole IP, because clients should
// not retry against a sinkhole.
//
// Matching is exact-or-subdomain: a query for "ads.example.com" matches
// blocklist entry "example.com" if and only if "example.com" appears with
// no prefix (i.e. as a registered domain). Implementation uses a
// trie-style suffix lookup so 150K entries match in ~O(label count).
package blocklist

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Set is the matcher. Safe for concurrent reads while a single goroutine
// hot-swaps via Reload. We use atomic.Value to swap the underlying node
// map without taking a write lock on every query.
type Set struct {
	root atomic.Value // *node
	size atomic.Int64
}

// New returns an empty Set. Use Load to populate.
func New() *Set {
	s := &Set{}
	s.root.Store(newNode())
	return s
}

// node is a single trie node keyed on a DNS label, walked right-to-left
// (so "ads.example.com" walks "com" -> "example" -> "ads"). terminal
// means "everything at-or-below this node is blocked".
type node struct {
	children map[string]*node
	terminal bool
}

func newNode() *node {
	return &node{children: map[string]*node{}}
}

// Size reports the number of distinct hostnames currently blocked.
func (s *Set) Size() int64 {
	return s.size.Load()
}

// Block reports whether the supplied hostname (or any of its labels)
// is in the set. Matching is exact-or-subdomain.
//
// Empty hostname returns false. Trailing dot is stripped. Comparison is
// case-insensitive.
func (s *Set) Block(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" {
		return false
	}
	labels := strings.Split(host, ".")
	cur, _ := s.root.Load().(*node)
	if cur == nil {
		return false
	}
	for i := len(labels) - 1; i >= 0; i-- {
		nxt, ok := cur.children[labels[i]]
		if !ok {
			return false
		}
		if nxt.terminal {
			return true
		}
		cur = nxt
	}
	return false
}

// Load parses StevenBlack-style hosts lines from r and atomically swaps
// the internal trie. Returns the number of distinct hosts inserted.
func (s *Set) Load(r io.Reader) (int, error) {
	root := newNode()
	count := 0
	scanner := bufio.NewScanner(r)
	// hosts files can have very long lines if commentary is appended;
	// raise the default 64KB buffer to 1MB.
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		host, ok := parseHostsLine(scanner.Text())
		if !ok {
			continue
		}
		if insertHost(root, host) {
			count++
		}
	}
	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("blocklist scan: %w", err)
	}
	s.root.Store(root)
	s.size.Store(int64(count))
	return count, nil
}

// parseHostsLine extracts the hostname column from a hosts(5) line.
// Returns ("", false) if the line should be skipped (comment, blank,
// localhost, or malformed).
func parseHostsLine(line string) (string, bool) {
	// Strip inline comment.
	if i := strings.Index(line, "#"); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return "", false
	}
	fields := strings.Fields(line)
	var host string
	switch {
	case len(fields) == 1:
		// Hostname-only list (some sources publish this format).
		host = fields[0]
	case len(fields) >= 2:
		// Standard hosts(5): "0.0.0.0 example.com"
		host = fields[1]
	default:
		return "", false
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if host == "" {
		return "", false
	}
	// Skip loopback/local entries that StevenBlack carries verbatim.
	switch host {
	case "localhost", "localhost.localdomain", "broadcasthost",
		"ip6-localhost", "ip6-loopback", "ip6-localnet", "ip6-mcastprefix",
		"ip6-allnodes", "ip6-allrouters", "ip6-allhosts", "0.0.0.0":
		return "", false
	}
	if !looksLikeHostname(host) {
		return "", false
	}
	return host, true
}

// looksLikeHostname is a fast cheap filter — no full RFC 1123 parse.
// We just want to drop lines that obviously aren't domains (IPs, IPv6,
// bare punctuation, etc.).
func looksLikeHostname(s string) bool {
	if !strings.Contains(s, ".") {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '.' || r == '_':
		default:
			return false
		}
	}
	return true
}

// insertHost adds `host` to the trie. Returns true if newly inserted,
// false if it was already covered by an existing entry.
func insertHost(root *node, host string) bool {
	labels := strings.Split(host, ".")
	cur := root
	for i := len(labels) - 1; i >= 0; i-- {
		// If a shorter suffix is already terminal, this entry is
		// covered — skip.
		if cur.terminal {
			return false
		}
		l := labels[i]
		nxt, ok := cur.children[l]
		if !ok {
			nxt = newNode()
			cur.children[l] = nxt
		}
		cur = nxt
	}
	if cur.terminal {
		return false
	}
	cur.terminal = true
	// Collapsing: drop children we now cover.
	cur.children = map[string]*node{}
	return true
}

// LoadFile loads from a local file path.
func (s *Set) LoadFile(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("blocklist open %s: %w", path, err)
	}
	defer f.Close()
	return s.Load(f)
}

// LoadURL loads from an HTTP/HTTPS source. The caller's context bounds
// the fetch lifetime. A 30s default timeout is used if the context has
// no deadline.
func (s *Set) LoadURL(ctx context.Context, url string) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("blocklist req: %w", err)
	}
	req.Header.Set("User-Agent", "iogrid-vpn-gateway/blocklist-fetcher")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("blocklist fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("blocklist fetch %s: status %d", url, resp.StatusCode)
	}
	return s.Load(resp.Body)
}

// Refresher runs Load periodically. Cancel ctx to stop.
//
// StevenBlack publishes daily; we refresh weekly so a freshly-deployed
// gateway always has an up-to-date list within 7 days of any new ad
// network appearing.
type Refresher struct {
	Set      *Set
	URL      string
	Interval time.Duration
	OnReload func(count int, err error)
	stopOnce sync.Once
	done     chan struct{}
}

// Start kicks off the background refresh loop. The first refresh fires
// after Interval; the caller is expected to call Set.LoadURL synchronously
// at startup if a populated list is required before serving traffic.
func (r *Refresher) Start(ctx context.Context) {
	if r.Interval == 0 {
		r.Interval = 7 * 24 * time.Hour
	}
	r.done = make(chan struct{})
	go func() {
		defer close(r.done)
		t := time.NewTicker(r.Interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				count, err := r.Set.LoadURL(ctx, r.URL)
				if r.OnReload != nil {
					r.OnReload(count, err)
				}
			}
		}
	}()
}

// Stop blocks until the background refresh loop has exited. Safe to call
// multiple times.
func (r *Refresher) Stop() {
	r.stopOnce.Do(func() {
		if r.done != nil {
			<-r.done
		}
	})
}

// ErrEmpty is returned by LoadFromAny when no source could be loaded.
var ErrEmpty = errors.New("blocklist: no sources loaded")
