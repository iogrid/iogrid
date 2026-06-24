package builds_test

import (
	"context"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
)

// TestSplitBrain_StreamRejectionDoesNotDowngradeRunning reproduces the #811
// (and #742) split-brain: the dispatch STREAM forwards a scheduler_paused
// rejection AFTER the POLL path already moved the same build to `running`. The
// gateway must keep the build `running` (the path that executed work wins) — a
// build cannot be both 'never started' and 'running'. The non-authoritative
// rejection is dropped, NOT 409'd, and metering/settle do not fire on it.
func TestSplitBrain_StreamRejectionDoesNotDowngradeRunning(t *testing.T) {
	t.Parallel()
	svc, fx := newTestService(t)
	ctx := context.Background()

	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	// POLL path: the daemon claimed the assignment and reported running.
	fx.clock.Advance(3 * time.Second)
	if _, err := svc.UpdateStatus(ctx, b.ID, builds.StatusRunning, "vm-booted", "prov-mac-1", 0); err != nil {
		t.Fatalf("running update: %v", err)
	}

	// STREAM path: the late scheduler_paused rejection races in (exit_code -1).
	updated, err := svc.UpdateStatus(ctx, b.ID, builds.StatusRejected, "scheduler_paused", "", -1)
	if err != nil {
		// MUST NOT 409 — workloads-svc forwards best-effort; the gateway
		// silently drops the downgrade.
		t.Fatalf("stream rejection should be dropped silently, got error: %v", err)
	}
	if updated.Status != builds.StatusRunning {
		t.Fatalf("split-brain: expected status to stay running, got %s", updated.Status)
	}
	if updated.ExitCode != 0 {
		t.Fatalf("split-brain: exit code must not be clobbered to -1, got %d", updated.ExitCode)
	}
	if updated.StatusNote == "scheduler_paused" {
		t.Fatalf("split-brain: status note must not be overwritten by the rejection")
	}

	// No terminal fired → no metering event from the dropped rejection.
	if events := fx.metering.Events(); len(events) != 0 {
		t.Fatalf("dropped rejection must not meter; got %d events", len(events))
	}

	// The real poll-path terminal still wins.
	fx.clock.Advance(6 * time.Minute)
	final, err := svc.UpdateStatus(ctx, b.ID, builds.StatusSucceeded, "ok", "prov-mac-1", 0)
	if err != nil {
		t.Fatalf("succeed update after dropped rejection: %v", err)
	}
	if final.Status != builds.StatusSucceeded {
		t.Fatalf("expected the running build to reach succeeded, got %s", final.Status)
	}
}

// TestSplitBrain_StreamRejectionDoesNotDowngradeDispatched covers the earlier
// window: the rejection arrives while the build is still `dispatched` (the poll
// path owns iOS-build dispatch). It must also be dropped, not recorded.
func TestSplitBrain_StreamRejectionDoesNotDowngradeDispatched(t *testing.T) {
	t.Parallel()
	svc, _ := newTestService(t)
	ctx := context.Background()

	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if b.Status != builds.StatusDispatched {
		t.Fatalf("expected dispatched after submit, got %s", b.Status)
	}

	updated, err := svc.UpdateStatus(ctx, b.ID, builds.StatusRejected, "scheduler_paused", "", -1)
	if err != nil {
		t.Fatalf("stream rejection on dispatched should be dropped, got: %v", err)
	}
	if updated.Status != builds.StatusDispatched {
		t.Fatalf("expected status to stay dispatched, got %s", updated.Status)
	}
}

// TestNeverDispatched_GenuineRejectionStillRecords is the no-regression guard:
// a build that genuinely never started must still record `rejected`. The
// dispatcher-rejection path (workloads-svc has no eligible provider) carries a
// note that is NOT scheduler_paused, so it stays authoritative.
func TestNeverDispatched_GenuineRejectionStillRecords(t *testing.T) {
	t.Parallel()
	svc, fx := newTestService(t)
	ctx := context.Background()

	// Make the dispatcher reject the submission outright.
	fx.disp.FailNextSubmit(context.DeadlineExceeded)
	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit should still persist the record: %v", err)
	}
	if b.Status != builds.StatusRejected {
		t.Fatalf("dispatcher-rejected build must record rejected, got %s", b.Status)
	}

	// And a scheduler_paused rejection on a *queued* (never-running) build is
	// also recorded — there is no poll progress to protect.
	b3, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit b3: %v", err)
	}
	// Force it back to queued to model "accepted but never dispatched/run".
	queued, err := forceStatus(ctx, svc, b3.ID, builds.StatusQueued)
	if err != nil {
		t.Fatalf("force queued: %v", err)
	}
	if queued.Status != builds.StatusQueued {
		t.Fatalf("setup: expected queued, got %s", queued.Status)
	}
	rej, err := svc.UpdateStatus(ctx, b3.ID, builds.StatusRejected, "scheduler_paused", "", -1)
	if err != nil {
		t.Fatalf("queued→rejected should be allowed: %v", err)
	}
	if rej.Status != builds.StatusRejected {
		t.Fatalf("a never-running build must record rejected, got %s", rej.Status)
	}
}

// TestReaper_FailsStaleRunningBuild proves the TTL reaper fails a build stuck
// `running` with no daemon heartbeat past the TTL, and leaves a fresh running
// build untouched.
func TestReaper_FailsStaleRunningBuild(t *testing.T) {
	t.Parallel()
	svc, fx := newTestService(t)
	ctx := context.Background()

	// Stale build: started, then the daemon went silent.
	stale, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit stale: %v", err)
	}
	if _, err := svc.UpdateStatus(ctx, stale.ID, builds.StatusRunning, "vm-booted", "prov-1", 0); err != nil {
		t.Fatalf("running stale: %v", err)
	}

	// Fresh build started just now — must survive the sweep.
	fx.clock.Advance(90 * time.Minute) // push the stale build well past TTL
	fresh, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit fresh: %v", err)
	}
	if _, err := svc.UpdateStatus(ctx, fresh.ID, builds.StatusRunning, "vm-booted", "prov-2", 0); err != nil {
		t.Fatalf("running fresh: %v", err)
	}

	reaped, err := svc.ReapStale(ctx, builds.DefaultStaleTTL)
	if err != nil {
		t.Fatalf("ReapStale: %v", err)
	}
	if len(reaped) != 1 || reaped[0] != stale.ID {
		t.Fatalf("expected to reap exactly the stale build %s, got %v", stale.ID, reaped)
	}

	got, err := svc.Get(ctx, "ws-1", stale.ID)
	if err != nil {
		t.Fatalf("get stale: %v", err)
	}
	if got.Status != builds.StatusFailed {
		t.Fatalf("stale build should be failed, got %s", got.Status)
	}
	if got.FinishedAt == nil {
		t.Fatalf("reaped build should have a FinishedAt")
	}

	gotFresh, err := svc.Get(ctx, "ws-1", fresh.ID)
	if err != nil {
		t.Fatalf("get fresh: %v", err)
	}
	if gotFresh.Status != builds.StatusRunning {
		t.Fatalf("fresh build must stay running, got %s", gotFresh.Status)
	}
}

// TestReaper_FailsStaleDispatchedBuild proves a build stuck `dispatched`
// (poll assignment never picked up) past the TTL is reaped too — timed from
// SubmittedAt since it never started.
func TestReaper_FailsStaleDispatchedBuild(t *testing.T) {
	t.Parallel()
	svc, fx := newTestService(t)
	ctx := context.Background()

	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if b.Status != builds.StatusDispatched {
		t.Fatalf("expected dispatched, got %s", b.Status)
	}

	// Before the TTL elapses: nothing reaped.
	fx.clock.Advance(30 * time.Minute)
	if reaped, err := svc.ReapStale(ctx, builds.DefaultStaleTTL); err != nil || len(reaped) != 0 {
		t.Fatalf("nothing should reap before TTL; reaped=%v err=%v", reaped, err)
	}

	// Past the TTL: reaped to failed.
	fx.clock.Advance(40 * time.Minute)
	reaped, err := svc.ReapStale(ctx, builds.DefaultStaleTTL)
	if err != nil {
		t.Fatalf("ReapStale: %v", err)
	}
	if len(reaped) != 1 || reaped[0] != b.ID {
		t.Fatalf("expected to reap %s, got %v", b.ID, reaped)
	}
	got, err := svc.Get(ctx, "ws-1", b.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != builds.StatusFailed {
		t.Fatalf("expected failed, got %s", got.Status)
	}
}

// forceStatus is a test helper that drives a build to an arbitrary (legal)
// status via the store so a test can model intermediate lifecycle states
// (e.g. a build accepted-then-queued) the public API doesn't otherwise reach.
func forceStatus(ctx context.Context, svc *builds.Service, id string, s builds.Status) (*builds.Build, error) {
	// UpdateStatus enforces AllowedTransition; queued is reachable from
	// dispatched (backwards transitions are allowed while non-terminal).
	return svc.UpdateStatus(ctx, id, s, "", "", 0)
}
