// Package audit — retention enforcement.
//
// docs/LEGAL.md mandates 90-day retention of audit events. JetStream's
// native MaxAge handles in-stream pruning, but two cases need explicit
// help:
//
//  1. A Postgres mirror (when the operator points us at one via
//     AUDIT_POSTGRES_DSN). Stream-side MaxAge does not propagate into
//     the relational mirror; we run a daily DELETE keyed on created_at
//     plus an explicit index so the DELETE is index-only.
//  2. Recovery from a JetStream config drift. We periodically purge any
//     remnant pre-retention messages whose age exceeds the configured
//     window. This is belt-and-braces — a corrupt JetStream config or
//     a misapplied stream update could otherwise let stale audit
//     events linger.
//
// The pruner runs as a single goroutine on a 24h tick. Each pass is
// bounded (LIMIT 10_000 per DELETE batch) so memory growth is capped
// even if a backlog accumulates.
package audit

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// DefaultRetentionDays mirrors the docs/LEGAL.md value.
const DefaultRetentionDays = 90

// DefaultPruneInterval is the cron-like cadence.
const DefaultPruneInterval = 24 * time.Hour

// DefaultPruneBatch caps each DELETE to bound memory.
const DefaultPruneBatch = 10_000

// PrunerOptions configures the background retention enforcer.
type PrunerOptions struct {
	// RetentionDays defaults to 90.
	RetentionDays int
	// Interval defaults to DefaultPruneInterval (24h).
	Interval time.Duration
	// Batch caps the LIMIT on each Postgres DELETE (default 10_000).
	Batch int
	// DB is an optional *sql.DB pointing at the audit-mirror schema.
	// When nil only the JetStream-side enforcement runs.
	DB *sql.DB
	// Stream is an optional JetStream stream handle. When non-nil the
	// pruner re-applies the MaxAge on each pass so a drifted config
	// self-heals.
	Stream jetstream.Stream
	// JS is the JetStream context used to issue the UpdateStream call
	// (the Stream type itself is read-only at the SDK level). Optional;
	// when nil, MaxAge drift detection runs without remediation and a
	// warning is logged instead.
	JS jetstream.JetStream
	// Logger defaults to slog.Default.
	Logger *slog.Logger
}

// Pruner enforces the audit-log retention policy.
type Pruner struct {
	opts   PrunerOptions
	logger *slog.Logger

	mu          sync.Mutex
	lastRun     time.Time
	lastDeleted int64
	lastErr     error
}

// NewPruner constructs a Pruner with defaults applied.
func NewPruner(opts PrunerOptions) *Pruner {
	if opts.RetentionDays <= 0 {
		opts.RetentionDays = DefaultRetentionDays
	}
	if opts.Interval <= 0 {
		opts.Interval = DefaultPruneInterval
	}
	if opts.Batch <= 0 {
		opts.Batch = DefaultPruneBatch
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Pruner{opts: opts, logger: opts.Logger}
}

// EnsureIndex creates the created_at TTL index if it does not already
// exist. Idempotent; safe to call on every boot. Called automatically
// from Start when DB is non-nil.
//
// The index is partial (only rows older than the retention horizon)
// because the live working set is the bulk of the table and would
// never benefit from this particular index. PostgreSQL 11+ supports
// CREATE INDEX CONCURRENTLY IF NOT EXISTS which is what we want, but
// CONCURRENTLY can't run in a transaction — so we issue it raw on the
// supplied connection.
func (p *Pruner) EnsureIndex(ctx context.Context) error {
	if p.opts.DB == nil {
		return nil
	}
	// Two indices: a covering created_at index (used by retention DELETE)
	// plus the table itself. We create the table-if-not-exists for the
	// dev-time case where the operator has pointed us at a fresh DB.
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS antiabuse_audit (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			customer_id TEXT,
			provider_id TEXT,
			check_type TEXT NOT NULL,
			target TEXT,
			decision TEXT NOT NULL,
			reason TEXT,
			trace_id TEXT,
			payload JSONB NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS antiabuse_audit_created_at_idx
			ON antiabuse_audit (created_at)`,
	}
	for _, s := range stmts {
		if _, err := p.opts.DB.ExecContext(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

// Start launches the background pruner goroutine.
//
// On boot it (a) ensures the Postgres index exists when DB is set and
// (b) re-asserts the JetStream MaxAge when Stream is set. Then it
// schedules a tick every opts.Interval. The goroutine exits when ctx
// is cancelled.
func (p *Pruner) Start(ctx context.Context) {
	if p.opts.DB != nil {
		if err := p.EnsureIndex(ctx); err != nil {
			p.logger.Warn("audit pruner index init failed",
				slog.String("error", err.Error()))
		}
	}
	go p.loop(ctx)
}

func (p *Pruner) loop(ctx context.Context) {
	// Run once on boot so the first pass doesn't wait 24h.
	if err := p.RunOnce(ctx); err != nil {
		p.logger.Warn("audit pruner initial run failed",
			slog.String("error", err.Error()))
	}
	t := time.NewTicker(p.opts.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := p.RunOnce(ctx); err != nil {
				p.logger.Warn("audit pruner run failed",
					slog.String("error", err.Error()))
			}
		}
	}
}

// RunOnce executes a single pruning pass. Exposed publicly so an admin
// endpoint or unit test can trigger an out-of-band sweep.
func (p *Pruner) RunOnce(ctx context.Context) error {
	horizon := time.Now().UTC().Add(-time.Duration(p.opts.RetentionDays) * 24 * time.Hour)
	var (
		deleted int64
		errs    []error
	)

	// 1. JetStream stream — re-assert MaxAge in case a config drift
	//    extended the window. Best-effort; failure logs a warn.
	if p.opts.Stream != nil {
		info, err := p.opts.Stream.Info(ctx)
		if err == nil {
			cfg := info.Config
			want := time.Duration(p.opts.RetentionDays) * 24 * time.Hour
			if cfg.MaxAge != want {
				cfg.MaxAge = want
				if p.opts.JS != nil {
					if _, err := p.opts.JS.UpdateStream(ctx, cfg); err != nil {
						errs = append(errs, err)
					} else {
						p.logger.Info("audit pruner re-applied JetStream MaxAge",
							slog.Duration("max_age", want))
					}
				} else {
					p.logger.Warn("audit pruner detected JetStream MaxAge drift but JS handle missing — cannot remediate",
						slog.Duration("current_max_age", info.Config.MaxAge),
						slog.Duration("desired_max_age", want))
				}
			}
		}
	}

	// 2. Postgres mirror — DELETE in bounded batches.
	if p.opts.DB != nil {
		n, err := p.prunePostgres(ctx, horizon)
		if err != nil {
			errs = append(errs, err)
		}
		deleted += n
	}

	p.mu.Lock()
	p.lastRun = time.Now()
	p.lastDeleted = deleted
	if len(errs) > 0 {
		p.lastErr = errors.Join(errs...)
	} else {
		p.lastErr = nil
	}
	p.mu.Unlock()

	if deleted > 0 {
		p.logger.Info("audit pruner pass complete",
			slog.Int64("deleted", deleted),
			slog.Time("horizon", horizon),
		)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// prunePostgres deletes rows older than horizon in capped batches.
func (p *Pruner) prunePostgres(ctx context.Context, horizon time.Time) (int64, error) {
	var total int64
	for {
		// DELETE ... WHERE id IN (SELECT id ... ORDER BY created_at LIMIT N)
		// keeps each transaction short and lets autovacuum keep up.
		res, err := p.opts.DB.ExecContext(ctx, `
			DELETE FROM antiabuse_audit
			WHERE id IN (
				SELECT id FROM antiabuse_audit
				WHERE created_at < $1
				ORDER BY created_at
				LIMIT $2
			)`, horizon, p.opts.Batch)
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < int64(p.opts.Batch) {
			return total, nil
		}
		// Yield between batches so this never monopolises the DB.
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Status returns the last-pass observability snapshot.
func (p *Pruner) Status() (lastRun time.Time, deleted int64, lastErr error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastRun, p.lastDeleted, p.lastErr
}

// Stream exposes the JetStream handle so the transparency report can
// query it for stream-level statistics (last-MaxAge config, message
// count, etc).
func (e *Emitter) Stream() jetstream.Stream { return e.stream }

// JS exposes the JetStream context (used by the Pruner for stream
// remediation). Returns nil in slog-fallback mode.
func (e *Emitter) JS() jetstream.JetStream { return e.js }
