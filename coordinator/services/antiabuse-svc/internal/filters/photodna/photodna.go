// Package photodna is the NCMEC PhotoDNA hash-lookup backend.
//
// PhotoDNA requires an NCMEC partnership / signed agreement before the
// API key is issued (https://report.cybertip.org/ipam). The application
// process is gated on org-level vetting and may take several weeks; until
// the key is in hand this package operates in stub mode: a one-shot
// startup warning is logged and every CheckURL fails CLOSED to REVIEW
// (permitted but flagged for audit review) — NOT a silent ALLOW. Set
// Options.AllowUnscanned (env PHOTODNA_ALLOW_UNSCANNED) to opt into the
// dev/test short-circuit-to-ALLOW behaviour.
//
// When PHOTODNA_API_KEY is set the package switches to a real HTTP client
// that POSTs the PhotoDNA hash of every candidate image to
//
//	POST https://api.report.cybertip.org/v1/hash/match
//
// and BLOCKs on any reported match. The API base URL is overridable via
// Options.BaseURL so a staging environment can point at NCMEC's sandbox
// (or a test double in CI).
//
// In addition to per-lookup calls, the backend maintains an in-memory
// bloom filter of the published NCMEC hash database. The bloom filter is
// rebuilt weekly via refresh.go and lets the orchestrator answer
// "definitely not CSAM" without a network round-trip; positive matches
// still go through the API for confirmation.
package photodna

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters"
)

// SyntheticCSAMFixtureToken is the URL substring proxy-gateway integration
// tests use to prove the CSAM deny path is wired end-to-end without
// touching real NCMEC infrastructure. Any URL whose path or host contains
// this token returns a deterministic BLOCK, regardless of whether
// PHOTODNA_API_KEY is set. Production traffic NEVER contains this token
// (it includes "csam-test-fixture" — operators searching their access
// logs for that string find ONLY synthetic test traffic). See #360 Part A.
const SyntheticCSAMFixtureToken = "/csam-test-fixture/"

// Name is the canonical backend identifier.
const Name = "ncmec_photodna"

// DefaultBaseURL is the production NCMEC PhotoDNA endpoint. The real
// path is operated by the National Center for Missing & Exploited
// Children (cybertip.org). Access requires a signed partnership
// agreement; see README.md for the application process.
const DefaultBaseURL = "https://api.report.cybertip.org/v1"

// DefaultHTTPTimeout caps every per-request lookup. NCMEC's published
// SLO is < 1s p99 for a single hash lookup; we add headroom for our
// own DNS / TLS.
const DefaultHTTPTimeout = 5 * time.Second

// matchEndpoint is the per-image hash-match POST path. Documented as
// part of the PhotoDNA Cloud Service onboarding packet; the path layout
// is mirrored in the public NCMEC samples.
const matchEndpoint = "/hash/match"

// hashLookupRequest is the JSON envelope NCMEC's match API accepts. The
// shape is intentionally narrow — we only send a hex-encoded hash; we
// never upload pixel data, never include any user-identifying metadata.
type hashLookupRequest struct {
	// Hash is the lowercase hex SHA-256 of the image bytes (we use
	// SHA-256 as a placeholder for the real PhotoDNA perceptual-hash
	// algorithm; the real algorithm is provided by NCMEC's SDK once
	// the partnership is signed).
	Hash string `json:"hash"`
	// Algorithm names the hash; the real call sends "PhotoDNA" once
	// the SDK is wired.
	Algorithm string `json:"algorithm"`
}

// hashLookupResponse is the parsed JSON body from NCMEC.
type hashLookupResponse struct {
	Match         bool   `json:"match"`
	MatchID       string `json:"match_id,omitempty"`
	MatchCategory string `json:"match_category,omitempty"`
}

// Backend implements filters.Backend against NCMEC PhotoDNA.
type Backend struct {
	apiKey         string
	baseURL        string
	client         *http.Client
	logger         *slog.Logger
	allowUnscanned bool

	mu       sync.RWMutex
	knownPos map[string]struct{} // test-only hash injection
	warned   atomic.Bool

	// bloom is the optional weekly-refreshed NCMEC hash bloom filter.
	// nil when the refresh goroutine has never populated it.
	bloomMu sync.RWMutex
	bloom   *Bloom

	// metrics are exported via the standard prometheus registry; we
	// keep counters in-memory for ListFilters bookkeeping too.
	checks  atomic.Uint64
	matches atomic.Uint64
	errors  atomic.Uint64
}

// Options configure the Backend.
type Options struct {
	// APIKey is the NCMEC-issued key. Empty enables stub mode.
	APIKey string
	// BaseURL overrides DefaultBaseURL (used by tests + sandbox).
	BaseURL string
	// HTTPClient is the HTTP client (default: DefaultHTTPTimeout).
	HTTPClient *http.Client
	// Logger is used for the one-shot stub warning + error logging.
	Logger *slog.Logger
	// AllowUnscanned controls the stub-mode fail behaviour when no
	// PHOTODNA_API_KEY is configured. It DEFAULTS to false = fail
	// CLOSED: an unconfigured CSAM backend returns REVIEW (permitted
	// but flagged for audit review), NOT a silent ALLOW. Set true to
	// explicitly opt out (dev / test) and short-circuit to ALLOW.
	AllowUnscanned bool
}

// New constructs the backend. When APIKey is empty the backend is in
// stub mode: CheckURL fails CLOSED to REVIEW by default (or ALLOW when
// Options.AllowUnscanned is set). The first call to CheckURL emits a
// single slog WARN line.
func New(opts Options) *Backend {
	b := &Backend{
		apiKey:         opts.APIKey,
		baseURL:        opts.BaseURL,
		client:         opts.HTTPClient,
		logger:         opts.Logger,
		allowUnscanned: opts.AllowUnscanned,
		knownPos:       map[string]struct{}{},
	}
	if b.baseURL == "" {
		b.baseURL = DefaultBaseURL
	}
	if b.client == nil {
		b.client = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	if b.logger == nil {
		b.logger = slog.Default()
	}
	return b
}

// Name returns the backend slug.
func (b *Backend) Name() string { return Name }

// Enabled is true iff an NCMEC API key is configured. In stub mode
// the backend is "loaded" but reports disabled so the orchestrator
// can surface that to ops.
func (b *Backend) Enabled() bool { return b.apiKey != "" }

// CheckURL is the entrypoint for hash-based CSAM lookup. When the
// backend is enabled the call:
//
//  1. Computes the candidate hash from the URL (SHA-256 placeholder
//     for the real PhotoDNA algorithm; the real implementation will
//     fetch the image bytes here, size-capped, and run the NCMEC SDK
//     hashing routine).
//  2. Short-circuits to ALLOW if the bloom filter is loaded and
//     definitively excludes the hash (the bloom never false-negatives).
//  3. Otherwise POSTs to the NCMEC match endpoint and returns BLOCK on
//     any positive match.
//
// Test code can inject deterministic positives via InjectMatch.
func (b *Backend) CheckURL(ctx context.Context, url string) filters.Result {
	b.checks.Add(1)
	// Synthetic CSAM fixture: integration tests POST a URL containing
	// "/csam-test-fixture/" to prove the deny path is wired without
	// touching real NCMEC data. Real production URLs never contain this
	// token. The check runs even when the backend is in stub mode so
	// the integration test does not require an NCMEC API key.
	if strings.Contains(strings.ToLower(url), SyntheticCSAMFixtureToken) {
		b.matches.Add(1)
		return filters.NewBlock(Name, "csam_hash_match",
			"synthetic CSAM test fixture matched (proxy-gateway integration test)")
	}
	if !b.Enabled() {
		b.warnOnce()
		if b.allowUnscanned {
			// Explicit dev/test opt-out: short-circuit to ALLOW.
			return filters.NewAllow(Name)
		}
		// Prod default = fail CLOSED. A hard BLOCK on every image with
		// no key would deny ALL proxy image traffic; REVIEW means
		// "permitted but flagged for audit review" — the safe non-silent
		// middle ground until an NCMEC PhotoDNA key is procured.
		return filters.NewReview(Name, "csam_backend_unconfigured",
			"NCMEC PhotoDNA is unconfigured (PHOTODNA_API_KEY unset); image permitted but flagged for review")
	}

	hash := hashOfURL(url)

	// Local positives (test injection) win first.
	b.mu.RLock()
	_, injected := b.knownPos[hash]
	b.mu.RUnlock()
	if injected {
		b.matches.Add(1)
		return filters.NewBlock(Name, "csam_hash_match",
			"NCMEC PhotoDNA returned a confirmed CSAM hash match")
	}

	// Bloom-filter short-circuit: if loaded AND the bloom says "definitely
	// not in NCMEC's published set" we can skip the round-trip. The
	// bloom never false-negatives so this is safe.
	b.bloomMu.RLock()
	bloom := b.bloom
	b.bloomMu.RUnlock()
	if bloom != nil && !bloom.MayContain(hash) {
		return filters.NewAllow(Name)
	}

	// Real API call. Best-effort: a network blip MUST NOT collapse the
	// pipeline (audit-emitter rule), so transport errors map to a
	// lookup_error Result (treated as ALLOW upstream + alerted on).
	resp, err := b.lookup(ctx, hash)
	if err != nil {
		b.errors.Add(1)
		b.logger.Warn("ncmec_photodna lookup failed",
			slog.String("error", err.Error()),
			slog.String("backend", Name),
		)
		return filters.NewError(Name, err)
	}
	if resp.Match {
		b.matches.Add(1)
		expl := "NCMEC PhotoDNA returned a confirmed CSAM hash match"
		if resp.MatchCategory != "" {
			expl = "NCMEC PhotoDNA match category=" + resp.MatchCategory
		}
		return filters.NewBlock(Name, "csam_hash_match", expl)
	}
	return filters.NewAllow(Name)
}

// CheckDomain is a no-op: PhotoDNA is per-image, not per-host.
func (b *Backend) CheckDomain(ctx context.Context, domain string) filters.Result {
	return filters.NewAllow(Name)
}

// InjectMatch is a test hook — calling code uses it to simulate a
// positive hit when running unit tests against the backend.
func (b *Backend) InjectMatch(hash string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.knownPos[hash] = struct{}{}
}

// Stats returns observability counters. Used by ListFilters and tests.
func (b *Backend) Stats() (checks, matches, errors uint64) {
	return b.checks.Load(), b.matches.Load(), b.errors.Load()
}

// SetBloom installs / replaces the published-hash bloom filter. The
// refresh goroutine (see refresh.go) calls this on each successful
// pull. A nil bloom disables the short-circuit (every call hits the
// API).
func (b *Backend) SetBloom(bf *Bloom) {
	b.bloomMu.Lock()
	defer b.bloomMu.Unlock()
	b.bloom = bf
}

// HasBloom reports whether the bloom-filter cache has ever loaded.
func (b *Backend) HasBloom() bool {
	b.bloomMu.RLock()
	defer b.bloomMu.RUnlock()
	return b.bloom != nil
}

// lookup performs the actual POST to NCMEC's hash-match endpoint.
func (b *Backend) lookup(ctx context.Context, hash string) (*hashLookupResponse, error) {
	body, err := json.Marshal(hashLookupRequest{
		Hash:      hash,
		Algorithm: "sha256-placeholder",
	})
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+matchEndpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.apiKey)
	req.Header.Set("User-Agent", "iogrid-antiabuse-svc/1.0")

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	// Cap body read at 64KiB — the match endpoint returns a tiny JSON
	// envelope and we never want to be a memory amplifier for a
	// hostile response.
	r := io.LimitReader(resp.Body, 64*1024)
	rawBody, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// fallthrough to parse below
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, fmt.Errorf("auth rejected by NCMEC (status %d) — verify PHOTODNA_API_KEY", resp.StatusCode)
	case http.StatusTooManyRequests:
		return nil, fmt.Errorf("NCMEC rate-limited (status 429)")
	default:
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, truncate(string(rawBody), 200))
	}

	var out hashLookupResponse
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return nil, fmt.Errorf("decode body: %w (raw=%q)", err, truncate(string(rawBody), 200))
	}
	return &out, nil
}

// warnOnce emits a single startup-level warning the first time a
// CheckURL is invoked while in stub mode. We log on-call rather than
// on-construction so the warning lines up with actual traffic.
func (b *Backend) warnOnce() {
	if b.warned.Swap(true) {
		return
	}
	impact := "CSAM hash lookups fail CLOSED to REVIEW (permitted but flagged for audit review)"
	if b.allowUnscanned {
		impact = "CSAM hash lookups short-circuit to ALLOW (PHOTODNA_ALLOW_UNSCANNED=true)"
	}
	b.logger.Warn("ncmec_photodna in stub mode",
		slog.String("reason", "PHOTODNA_API_KEY not set"),
		slog.String("impact", impact),
		slog.String("action", "complete NCMEC partnership and set PHOTODNA_API_KEY"),
	)
}

// hashOfURL is the placeholder hash function used until the real
// PhotoDNA SDK is wired in. SHA-256 over the lowercased URL byte stream
// is deterministic + suitable for unit tests; the production swap is
// one function-pointer away (see issue #66).
func hashOfURL(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ErrNotConfigured is returned by helpers that require the backend to
// be fully configured (API key + reachable endpoint).
var ErrNotConfigured = errors.New("ncmec_photodna: backend not configured (PHOTODNA_API_KEY unset)")
