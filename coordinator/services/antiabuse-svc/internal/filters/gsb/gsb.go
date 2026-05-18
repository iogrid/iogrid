// Package gsb wraps Google Safe Browsing v4 (threatMatches:find).
//
// Unlike PhishTank / OpenPhish this backend is per-request: there is
// no bulk feed to cache. We POST the URL to
// https://safebrowsing.googleapis.com/v4/threatMatches:find and parse
// the response. The implementation keeps a small per-process LRU
// cache (TTL 5 min) to absorb retry storms and identical lookups
// from concurrent workloads.
//
// Reference: https://developers.google.com/safe-browsing/v4/lookup-api
package gsb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	antiabusev1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/antiabuse/v1"
	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters"
)

// Name is the canonical backend identifier.
const Name = "google_safe_browsing"

// DefaultEndpoint is the production v4 lookup URL.
const DefaultEndpoint = "https://safebrowsing.googleapis.com/v4/threatMatches:find"

// Default threat-type list per the v4 docs. The blank entry
// SOCIAL_ENGINEERING covers most phishing pages.
var defaultThreatTypes = []string{
	"MALWARE",
	"SOCIAL_ENGINEERING",
	"UNWANTED_SOFTWARE",
	"POTENTIALLY_HARMFUL_APPLICATION",
}

// Backend implements filters.Backend against Google Safe Browsing v4.
type Backend struct {
	apiKey     string
	endpoint   string
	client     *http.Client
	clientID   string
	clientVer  string
	threatTypes []string

	mu    sync.Mutex
	cache map[string]cacheEntry
	ttl   time.Duration
}

type cacheEntry struct {
	expiresAt time.Time
	hit       bool
	threat    string
}

// Options configure the Backend.
type Options struct {
	// APIKey is required; the backend disables itself if it's empty.
	APIKey string
	// Endpoint defaults to DefaultEndpoint; tests override.
	Endpoint string
	// Client defaults to http.Client{Timeout: 10s}.
	Client *http.Client
	// CacheTTL defaults to 5min.
	CacheTTL time.Duration
	// ClientID and ClientVersion are sent in the request envelope.
	ClientID      string
	ClientVersion string
	// ThreatTypes overrides the default list.
	ThreatTypes []string
}

// New constructs a GSB backend. When APIKey is empty the backend is
// disabled and all checks short-circuit to ALLOW.
func New(opts Options) *Backend {
	b := &Backend{
		apiKey:      opts.APIKey,
		endpoint:    opts.Endpoint,
		client:      opts.Client,
		clientID:    opts.ClientID,
		clientVer:   opts.ClientVersion,
		threatTypes: opts.ThreatTypes,
		cache:       map[string]cacheEntry{},
		ttl:         opts.CacheTTL,
	}
	if b.endpoint == "" {
		b.endpoint = DefaultEndpoint
	}
	if b.client == nil {
		b.client = &http.Client{Timeout: 10 * time.Second}
	}
	if b.clientID == "" {
		b.clientID = "iogrid"
	}
	if b.clientVer == "" {
		b.clientVer = "0.1.0"
	}
	if len(b.threatTypes) == 0 {
		b.threatTypes = defaultThreatTypes
	}
	if b.ttl <= 0 {
		b.ttl = 5 * time.Minute
	}
	return b
}

// Name returns the backend slug.
func (b *Backend) Name() string { return Name }

// Enabled is true iff an API key is configured.
func (b *Backend) Enabled() bool { return b.apiKey != "" }

// CheckURL queries the GSB API for the given URL. Errors short-circuit
// to ALLOW (best-effort) with the error attached to the Result.
func (b *Backend) CheckURL(ctx context.Context, target string) filters.Result {
	if !b.Enabled() {
		return filters.NewAllow(Name)
	}
	t := strings.TrimSpace(target)
	if t == "" {
		return filters.NewAllow(Name)
	}
	if r, ok := b.lookupCache(t); ok {
		return r
	}
	res, err := b.query(ctx, t)
	if err != nil {
		return filters.NewError(Name, err)
	}
	b.storeCache(t, res)
	return res
}

// CheckDomain wraps the bare domain in https:// for the URL query.
func (b *Backend) CheckDomain(ctx context.Context, domain string) filters.Result {
	if !b.Enabled() {
		return filters.NewAllow(Name)
	}
	if domain == "" {
		return filters.NewAllow(Name)
	}
	return b.CheckURL(ctx, "https://"+strings.TrimSpace(domain)+"/")
}

func (b *Backend) lookupCache(key string) (filters.Result, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.cache[key]
	if !ok || time.Now().After(e.expiresAt) {
		if ok {
			delete(b.cache, key)
		}
		return filters.Result{}, false
	}
	if e.hit {
		return filters.NewBlock(Name, "gsb_listed",
			"Google Safe Browsing flagged this URL as "+e.threat), true
	}
	return filters.NewAllow(Name), true
}

func (b *Backend) storeCache(key string, r filters.Result) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cache[key] = cacheEntry{
		expiresAt: time.Now().Add(b.ttl),
		hit:       r.Match,
		threat:    r.Reason,
	}
}

// query performs a single threatMatches:find POST.
func (b *Backend) query(ctx context.Context, target string) (filters.Result, error) {
	body := map[string]any{
		"client": map[string]string{
			"clientId":      b.clientID,
			"clientVersion": b.clientVer,
		},
		"threatInfo": map[string]any{
			"threatTypes":      b.threatTypes,
			"platformTypes":    []string{"ANY_PLATFORM"},
			"threatEntryTypes": []string{"URL"},
			"threatEntries": []map[string]string{
				{"url": target},
			},
		},
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return filters.Result{}, err
	}
	u := b.endpoint + "?key=" + b.apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(buf))
	if err != nil {
		return filters.Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "iogrid-antiabuse-svc/1.0")
	resp, err := b.client.Do(req)
	if err != nil {
		return filters.Result{}, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return filters.Result{}, err
	}
	if resp.StatusCode == http.StatusOK {
		return parseResponse(raw)
	}
	return filters.Result{}, fmt.Errorf("gsb status %d: %s", resp.StatusCode, string(raw))
}

// gsbResponse models the JSON envelope.
type gsbResponse struct {
	Matches []struct {
		ThreatType string `json:"threatType"`
	} `json:"matches"`
}

func parseResponse(raw []byte) (filters.Result, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("{}")) {
		return filters.NewAllow(Name), nil
	}
	var r gsbResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return filters.Result{}, err
	}
	if len(r.Matches) == 0 {
		return filters.NewAllow(Name), nil
	}
	threats := make([]string, 0, len(r.Matches))
	for _, m := range r.Matches {
		threats = append(threats, m.ThreatType)
	}
	return filters.Result{
		Backend:     Name,
		Match:       true,
		Decision:    antiabusev1.FilterDecision_FILTER_DECISION_BLOCK,
		Reason:      "gsb_listed",
		Explanation: "Google Safe Browsing: " + strings.Join(threats, ","),
		CheckedAt:   time.Now(),
	}, nil
}

// errDisabled is the sentinel returned when a disabled backend is
// asked to query (currently unused in production but exposed for
// future direct-call tests).
var errDisabled = errors.New("gsb: backend disabled")

// ErrDisabled returns the disabled-backend sentinel.
func ErrDisabled() error { return errDisabled }
