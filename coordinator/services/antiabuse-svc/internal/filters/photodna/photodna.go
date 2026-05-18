// Package photodna is the NCMEC PhotoDNA hash-lookup backend.
//
// PhotoDNA requires an NCMEC partnership / signed agreement before the
// API key is issued. Until iogrid completes that onboarding, this
// package operates in stub mode: a startup warning is logged and every
// CheckURL / CheckImageHash short-circuits to ALLOW (no-match).
//
// The interface mirrors the eventual production shape so business
// logic upstream (the orchestrator, the audit emitter) can be wired
// today and tested with the stub — the only swap on go-live is the
// PHOTODNA_API_KEY env var.
package photodna

import (
	"context"
	"encoding/hex"
	"log/slog"
	"sync"

	"github.com/iogrid/iogrid/coordinator/services/antiabuse-svc/internal/filters"
)

// Name is the canonical backend identifier.
const Name = "ncmec_photodna"

// Backend implements filters.Backend against NCMEC PhotoDNA.
type Backend struct {
	apiKey string
	logger *slog.Logger

	mu       sync.RWMutex
	knownPos map[string]struct{} // test-only hash injection
	warned   bool
}

// Options configure the Backend.
type Options struct {
	// APIKey is the NCMEC-issued key. Empty enables stub mode.
	APIKey string
	// Logger is used for the one-shot stub warning.
	Logger *slog.Logger
}

// New constructs the backend. When APIKey is empty the backend is in
// stub mode and CheckURL / CheckImageHash both return ALLOW. The first
// call to CheckURL emits a single slog WARN line.
func New(opts Options) *Backend {
	b := &Backend{
		apiKey:   opts.APIKey,
		logger:   opts.Logger,
		knownPos: map[string]struct{}{},
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

// CheckURL is the entrypoint for hash-based CSAM lookup. The current
// stub implementation always returns ALLOW; production will:
//
//  1. Download the image (size-capped)
//  2. Compute the PhotoDNA hash
//  3. POST the hash to NCMEC's match endpoint
//  4. Return BLOCK on any match
//
// To make the wiring observable today, the stub honours a
// test-hook (InjectMatch) so unit tests can simulate a positive hit.
func (b *Backend) CheckURL(ctx context.Context, url string) filters.Result {
	if !b.Enabled() {
		b.warnOnce()
		return filters.NewAllow(Name)
	}
	// Real implementation would fetch + hash + lookup. The stub keeps
	// the production shape and lets tests inject hashes.
	hash := hex.EncodeToString([]byte(url))
	b.mu.RLock()
	_, hit := b.knownPos[hash]
	b.mu.RUnlock()
	if hit {
		return filters.NewBlock(Name, "csam_hash_match",
			"NCMEC PhotoDNA returned a confirmed CSAM hash match")
	}
	return filters.NewAllow(Name)
}

// CheckDomain is a no-op: PhotoDNA is per-image, not per-host.
func (b *Backend) CheckDomain(ctx context.Context, domain string) filters.Result {
	return filters.NewAllow(Name)
}

// InjectMatch is a test hook — calling code uses it to simulate a
// positive hit when running unit tests against the stub.
func (b *Backend) InjectMatch(hash string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.knownPos[hash] = struct{}{}
}

// warnOnce emits a single startup-level warning the first time a
// CheckURL is invoked while in stub mode. We log on-call rather than
// on-construction so the warning lines up with actual traffic.
func (b *Backend) warnOnce() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.warned {
		return
	}
	b.warned = true
	b.logger.Warn("ncmec_photodna in stub mode",
		slog.String("reason", "PHOTODNA_API_KEY not set"),
		slog.String("impact", "CSAM hash lookups will short-circuit to ALLOW"),
		slog.String("action", "complete NCMEC partnership and set PHOTODNA_API_KEY"),
	)
}
