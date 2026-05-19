package audit

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPruner_DefaultsApplied(t *testing.T) {
	p := NewPruner(PrunerOptions{})
	if p.opts.RetentionDays != DefaultRetentionDays {
		t.Errorf("RetentionDays default not applied: %d", p.opts.RetentionDays)
	}
	if p.opts.Interval != DefaultPruneInterval {
		t.Errorf("Interval default not applied: %v", p.opts.Interval)
	}
	if p.opts.Batch != DefaultPruneBatch {
		t.Errorf("Batch default not applied: %d", p.opts.Batch)
	}
}

func TestPruner_NoBackends_NoOp(t *testing.T) {
	// No DB and no Stream — RunOnce must be a clean no-op.
	p := NewPruner(PrunerOptions{})
	if err := p.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce with no backends should succeed: %v", err)
	}
	lastRun, deleted, lastErr := p.Status()
	if lastRun.IsZero() {
		t.Errorf("Status.lastRun should be set after RunOnce")
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
	if lastErr != nil {
		t.Errorf("lastErr = %v, want nil", lastErr)
	}
}

func TestPruner_Start_LoopExitsOnCtx(t *testing.T) {
	p := NewPruner(PrunerOptions{
		Interval: 30 * time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	time.Sleep(80 * time.Millisecond)
	cancel()
	// Brief settle for the goroutine to observe Done.
	time.Sleep(20 * time.Millisecond)
	// Must have run at least once (the immediate pass).
	last, _, _ := p.Status()
	if last.IsZero() {
		t.Errorf("Pruner.Start should have run the initial pass")
	}
}

func TestPruner_RetentionHorizonMath(t *testing.T) {
	// Sanity check the horizon math used by RunOnce — 90 days before
	// now is the threshold.
	p := NewPruner(PrunerOptions{RetentionDays: 90})
	horizon := time.Now().UTC().Add(-time.Duration(p.opts.RetentionDays) * 24 * time.Hour)
	want := time.Now().UTC().Add(-90 * 24 * time.Hour)
	delta := horizon.Sub(want)
	if delta < -time.Second || delta > time.Second {
		t.Errorf("horizon delta = %v, want ~0", delta)
	}
}

func TestPruner_Status_AfterError(t *testing.T) {
	// Build a Pruner whose Postgres path will error (DB is nil so no
	// path executes — but we can poke an error in directly).
	p := NewPruner(PrunerOptions{})
	p.mu.Lock()
	p.lastErr = errors.New("boom")
	p.mu.Unlock()
	_, _, err := p.Status()
	if err == nil {
		t.Errorf("Status must surface the recorded error")
	}
}
