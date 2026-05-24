// Package alerter scans providers periodically and emits a structured
// ALERT log line when any active provider's last_seen_at exceeds the
// configured staleness threshold. This closes the architectural gap
// behind #479: today when a residential provider's daemon goes silent,
// the bastion has no way to notice — the row keeps `status=active`
// indefinitely, the operator UI keeps showing the daemon as paired,
// and downstream walks (vcard LinkedIn proxy) silently fail because
// the only "live" daemon left is the bastion's datacenter IP.
//
// What this ships:
//   - A goroutine that wakes every ScanInterval, paginates through
//     active providers, and per-row checks (NOW() - last_seen_at)
//     vs StalenessThreshold.
//   - When a row first crosses the threshold, emits one
//     `provider.heartbeat_loss` log entry (slog WARN level) with
//     {provider_id, owner_user_id, display_name, last_seen_at,
//     staleness}. Subsequent scans don't re-emit for the same row
//     until it heartbeats again (de-dup via in-memory seen-set).
//   - When a previously-stale row sends a fresh heartbeat
//     (last_seen_at moves forward past threshold), emits a
//     `provider.heartbeat_recovered` log entry + drops the row from
//     the seen-set.
//
// Why this is the right shape for the iogridd-side architectural fix:
//   - It runs entirely on the coordinator side; ships independently
//     of any daemon change. So a fix lands even when the broken
//     daemon's host (Hatice's Mac) is unreachable.
//   - The structured log line is the alert primitive — log-ingest
//     (Loki / Mimir) routes WARN-level `provider.heartbeat_loss`
//     events to operator notification channels in a follow-up wire-up.
//   - The same scan output feeds the eventual billing-svc → Stalwart
//     email path ("your daemon stopped — here's what to check") in
//     a sibling change.
//
// Refs #479. Out of scope here: the email wire-up (separate package),
// the iogridd-side self-diagnose / reverse-channel feature (separate
// EPIC), and the operator-UI heartbeat-staleness chip.
package alerter

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

// Config parameterises the alerter loop.
type Config struct {
	// ScanInterval is how often the goroutine wakes to scan. Default 60s.
	// Faster means more accurate alerts at the cost of more DB scans;
	// slower means alerts lag by up to ScanInterval. 60s is a sane
	// balance for Phase-0 (we don't have SLOs that tighter alerting
	// would unlock).
	ScanInterval time.Duration
	// StalenessThreshold is the (NOW() - last_seen_at) cutoff above
	// which a provider is considered offline. Default 5 min. This is
	// well above the iogridd heartbeat cadence (5s per daemon today),
	// so legitimate network blips don't trip it, but well below the
	// "operator should have noticed by now" multi-hour horizon we'd
	// otherwise wait.
	StalenessThreshold time.Duration
	// PageSize is the inner per-scan ListProviders page size. Default
	// 200. The scan is best-effort; if there are >PageSize active
	// providers we still process the first page (sorted by
	// last_seen_at DESC so the most-recently-stale rows come first).
	// Multi-page pagination is a follow-up when fleet size grows.
	PageSize int
}

// defaults applied by Run when the caller leaves a field zero.
const (
	defaultScanInterval       = 60 * time.Second
	defaultStalenessThreshold = 5 * time.Minute
	defaultPageSize           = 200
)

// applyDefaults fills in any zero-valued fields with the package defaults.
// Mutates c in place + returns it for ergonomics in callers like
// `cfg := Config{}.applyDefaults()`.
func (c *Config) applyDefaults() {
	if c.ScanInterval == 0 {
		c.ScanInterval = defaultScanInterval
	}
	if c.StalenessThreshold == 0 {
		c.StalenessThreshold = defaultStalenessThreshold
	}
	if c.PageSize == 0 {
		c.PageSize = defaultPageSize
	}
}

// Alerter wraps the periodic scan loop. Construct via New + drive via Run.
type Alerter struct {
	store store.Store
	log   *slog.Logger
	cfg   Config

	// seen tracks providers we've already emitted a heartbeat_loss for
	// since their last recovery. Keyed by provider id. Empty value (the
	// last_seen_at observed when we first alerted) lets us emit one
	// recovery line when the row's last_seen_at moves forward past
	// that observation. Bounded by active-provider count; no eviction
	// needed for the Phase-0 fleet size.
	seenMu sync.Mutex
	seen   map[string]time.Time

	// now is injected so unit tests can drive the loop without
	// real-clock dependency. Defaults to time.Now in New.
	now func() time.Time
}

// New constructs an Alerter. The Logger is required (alerter writes
// structured events through it); the Store is required (alerter
// queries ListProviders + reads each Provider's LastSeenAt). cfg is
// optional — zero-valued fields fall back to package defaults.
func New(st store.Store, lg *slog.Logger, cfg Config) *Alerter {
	cfg.applyDefaults()
	return &Alerter{
		store: st,
		log:   lg,
		cfg:   cfg,
		seen:  make(map[string]time.Time),
		now:   time.Now,
	}
}

// Run blocks until ctx is cancelled, scanning every ScanInterval.
// Intended to be launched as `go alerter.Run(ctx)` from main; the
// shared-server graceful-shutdown path cancels ctx on SIGTERM and
// the goroutine exits cleanly within one ScanInterval. The first
// scan fires immediately so operators see the initial sweep result
// at boot instead of waiting ScanInterval seconds for the first tick.
func (a *Alerter) Run(ctx context.Context) {
	a.log.Info("providers heartbeat-loss alerter starting",
		slog.Duration("scan_interval", a.cfg.ScanInterval),
		slog.Duration("staleness_threshold", a.cfg.StalenessThreshold))
	// Immediate scan so the initial state is visible without waiting
	// for the first ticker tick.
	a.scanOnce(ctx)
	t := time.NewTicker(a.cfg.ScanInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			a.log.Info("providers heartbeat-loss alerter stopping")
			return
		case <-t.C:
			a.scanOnce(ctx)
		}
	}
}

// scanOnce runs one pass: list active providers + emit loss/recovery
// events vs the seen-set. Errors are logged + swallowed (the goroutine
// keeps running — a transient DB hiccup shouldn't kill the alerter).
func (a *Alerter) scanOnce(ctx context.Context) {
	providers, _, err := a.store.ListProviders(ctx, store.ListOptions{
		Status:   store.StatusActive,
		PageSize: a.cfg.PageSize,
	})
	if err != nil {
		a.log.Warn("providers alerter scan failed (continuing)",
			slog.String("error", err.Error()))
		return
	}
	now := a.now()
	threshold := a.cfg.StalenessThreshold
	a.seenMu.Lock()
	defer a.seenMu.Unlock()
	for _, p := range providers {
		// LastSeenAt zero = never heartbeated (fresh-paired, no /heartbeat
		// yet). We don't alert on those — the daemon may be in the gap
		// between pair RPC and first heartbeat post.
		if p.LastSeenAt.IsZero() {
			continue
		}
		staleness := now.Sub(p.LastSeenAt)
		prev, alreadyAlerted := a.seen[p.ID]
		if staleness > threshold {
			if !alreadyAlerted {
				a.log.Warn("provider.heartbeat_loss",
					slog.String("provider_id", p.ID),
					slog.String("owner_user_id", p.OwnerUserID),
					slog.String("display_name", p.DisplayName),
					slog.Time("last_seen_at", p.LastSeenAt),
					slog.Duration("staleness", staleness),
					slog.Duration("threshold", threshold))
				a.seen[p.ID] = p.LastSeenAt
			}
			continue
		}
		// Row is fresh. If we'd previously alerted + last_seen_at has
		// moved forward, emit recovery + clear the seen entry.
		if alreadyAlerted && p.LastSeenAt.After(prev) {
			a.log.Info("provider.heartbeat_recovered",
				slog.String("provider_id", p.ID),
				slog.String("owner_user_id", p.OwnerUserID),
				slog.String("display_name", p.DisplayName),
				slog.Time("last_seen_at", p.LastSeenAt))
			delete(a.seen, p.ID)
		}
	}
}
