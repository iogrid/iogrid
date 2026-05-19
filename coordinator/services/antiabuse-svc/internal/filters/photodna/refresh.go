package photodna

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultRefreshInterval is the cadence at which the NCMEC hash database
// is re-pulled into the bloom filter. NCMEC's published export is
// updated weekly; matching that cadence keeps the refresh footprint
// minimal while ensuring we ingest new hashes within 7 days.
const DefaultRefreshInterval = 7 * 24 * time.Hour

// DefaultHashExportPath is the (placeholder) hash-export route exposed
// by the NCMEC partner API. Real path is provided at onboarding; the
// shape (newline-delimited hex hashes, gzipped) is documented in
// NCMEC's PhotoDNA Cloud Service spec. Tests override via Options.
const DefaultHashExportPath = "/hash/export"

// RefresherOptions configures the background refresh goroutine.
type RefresherOptions struct {
	// Interval is the refresh cadence (default DefaultRefreshInterval).
	Interval time.Duration
	// ExpectedHashes is the bloom-filter target size; defaults to 1M.
	ExpectedHashes int
	// FalsePositiveRate is the bloom-filter FPR; defaults to 0.001.
	FalsePositiveRate float64
	// ExportPath overrides DefaultHashExportPath (used by tests).
	ExportPath string
	// Logger is the structured logger; defaults to slog.Default.
	Logger *slog.Logger
}

// Refresher is the periodic NCMEC hash-set downloader. It pulls the
// published hash list, rebuilds the bloom filter, and atomically
// installs it on the supplied Backend.
type Refresher struct {
	backend  *Backend
	opts     RefresherOptions
	logger   *slog.Logger

	mu        sync.Mutex
	lastSync  time.Time
	lastErr   error
	hashCount uint64
}

// NewRefresher constructs a Refresher bound to the given Backend.
func NewRefresher(backend *Backend, opts RefresherOptions) *Refresher {
	if opts.Interval <= 0 {
		opts.Interval = DefaultRefreshInterval
	}
	if opts.ExpectedHashes <= 0 {
		opts.ExpectedHashes = 1_000_000
	}
	if opts.FalsePositiveRate <= 0 || opts.FalsePositiveRate >= 1 {
		opts.FalsePositiveRate = 0.001
	}
	if opts.ExportPath == "" {
		opts.ExportPath = DefaultHashExportPath
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Refresher{backend: backend, opts: opts, logger: opts.Logger}
}

// Start launches the refresh goroutine. It performs one immediate
// refresh (best-effort) then ticks at the configured interval. Returns
// immediately; the goroutine exits when ctx is cancelled.
//
// In stub mode (backend.Enabled() == false) Start is a no-op: there is
// no API to pull from. The Refresher logs a single info line so ops
// know why the bloom filter is empty.
func (r *Refresher) Start(ctx context.Context) {
	if !r.backend.Enabled() {
		r.logger.Info("ncmec_photodna refresh disabled (stub mode)",
			slog.String("reason", "PHOTODNA_API_KEY not set"))
		return
	}
	go r.loop(ctx)
}

func (r *Refresher) loop(ctx context.Context) {
	// Initial pull — best-effort, swallow error (the periodic
	// timer will retry).
	if err := r.Refresh(ctx); err != nil {
		r.logger.Warn("ncmec_photodna initial refresh failed",
			slog.String("error", err.Error()))
	}
	t := time.NewTicker(r.opts.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := r.Refresh(ctx); err != nil {
				r.logger.Warn("ncmec_photodna refresh failed",
					slog.String("error", err.Error()))
			}
		}
	}
}

// Refresh fetches the latest hash list and rebuilds the bloom filter.
// Exposed (instead of private) so an admin endpoint or unit test can
// force a refresh out-of-band.
func (r *Refresher) Refresh(ctx context.Context) error {
	if !r.backend.Enabled() {
		return ErrNotConfigured
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.backend.baseURL+r.opts.ExportPath, nil)
	if err != nil {
		r.recordErr(err)
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.backend.apiKey)
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "iogrid-antiabuse-svc/1.0")

	resp, err := r.backend.client.Do(req)
	if err != nil {
		r.recordErr(err)
		return fmt.Errorf("ncmec hash export: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		err := fmt.Errorf("ncmec hash export: status %d", resp.StatusCode)
		r.recordErr(err)
		return err
	}

	// Stream the response line-by-line into a fresh bloom filter so we
	// never hold the whole hash list in memory. Each line is one
	// hex-encoded hash; empty lines / lines starting with '#' are
	// comments and skipped.
	bf := NewBloom(r.opts.ExpectedHashes, r.opts.FalsePositiveRate)
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 512<<20)) // 512MiB safety cap
	scanner.Buffer(make([]byte, 0, 256*1024), 4*1024*1024)
	var n uint64
	for scanner.Scan() {
		line := strings.TrimSpace(strings.ToLower(scanner.Text()))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		bf.Add(line)
		n++
	}
	if err := scanner.Err(); err != nil {
		r.recordErr(err)
		return fmt.Errorf("ncmec hash export scan: %w", err)
	}

	r.backend.SetBloom(bf)
	r.mu.Lock()
	r.lastSync = time.Now()
	r.lastErr = nil
	r.hashCount = n
	r.mu.Unlock()

	r.logger.Info("ncmec_photodna bloom refreshed",
		slog.Uint64("hashes", n),
		slog.Uint64("bits", bf.Size()),
		slog.Uint64("k", bf.Hashes()),
	)
	return nil
}

// recordErr stores the latest refresh error for observability.
func (r *Refresher) recordErr(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastErr = err
}

// Status returns the most recent refresh state.
func (r *Refresher) Status() (lastSync time.Time, hashCount uint64, lastErr error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastSync, r.hashCount, r.lastErr
}
