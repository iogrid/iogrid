//go:build integration

// Postgres integration tests for the #771 workloads-svc assignment store.
//
// The bug: assignments lived ONLY in process memory (NewInMemory). With
// multiple replicas (HPA maxReplicas:10) the poll-dispatch path (#705) is
// split-brain — a long iOS build's terminal-status POST can land on a
// different replica than the one that created the assignment → GetAssignment
// 404 → the build-gateway ForwardStatus never fires → the build stays
// "running", metering / $GRID settle never run. ping's #770 Ping.app built but
// never settled.
//
// These tests pin the Postgres path on a real database so the SQL-level
// contract (column names, JSONB spec/result round-trip, NULL handling on the
// nullable timestamp columns, the ListPendingAssignments filter) is verified —
// the in-memory mirror can't surface SQL drift. The headline test
// (TestPostgres_CrossReplicaAssignmentSurvives) builds TWO independent store
// instances over the SAME database (= two replicas) and proves the terminal
// status resolves on the OTHER instance: the exact boundary that 404'd before.
//
// Run with:
//
//	docker run --rm -d -p 55434:5432 \
//	    -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=workloads_svc_test \
//	    postgres:16
//	DATABASE_URL=postgres://postgres:postgres@localhost:55434/workloads_svc_test?sslmode=disable \
//	    go test -tags=integration ./internal/store/...
//
// Port 55434 avoids clashing with #608's vpn-svc fixture (55432) and #597's
// billing-svc fixture (55433) so all three suites can co-tenant on a dev box.
//
// If DATABASE_URL is unset and the default localhost:55434 DSN is unreachable
// the tests skip cleanly — same pattern as billing-svc/grid and vpn-svc/store.
//
// Refs #771, #770, #740.

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	// stdlib registers the "pgx" sql.Driver name goose (via db.Apply) uses.
	_ "github.com/jackc/pgx/v5/stdlib"

	wdb "github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/db"
)

const defaultPostgresDSN = "postgres://postgres:postgres@localhost:55434/workloads_svc_test?sslmode=disable"

func postgresDSN() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultPostgresDSN
}

// newPostgresPool brings up a pool against the configured DATABASE_URL and
// skips cleanly if unreachable. It does NOT migrate — call wipeAndMigrate once
// per test (it's destructive by design).
func newPostgresPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	dsn := postgresDSN()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("postgres pool create failed (DATABASE_URL=%q): %v", dsn, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres ping failed (DATABASE_URL=%q): %v", dsn, err)
	}
	return pool, func() { pool.Close() }
}

// wipeAndMigrate drops the workloads-svc schema (incl. the goose marker) and
// re-applies the embedded migrations so every run starts clean.
func wipeAndMigrate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, s := range []string{
		`DROP TABLE IF EXISTS workload_assignments CASCADE`,
		`DROP TABLE IF EXISTS workloads CASCADE`,
		`DROP TABLE IF EXISTS goose_db_version CASCADE`,
	} {
		if _, err := pool.Exec(ctx, s); err != nil {
			t.Fatalf("wipe (%s): %v", s, err)
		}
	}
	if err := wdb.Apply(context.Background(), postgresDSN()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
}

// fullIOSWorkload builds a Workload with EVERY field populated (the
// field-complete round-trip requirement) — a gateway-originated iOS build with
// a build_id label (the routing key the settle path depends on).
func fullIOSWorkload() *Workload {
	now := time.Now().UTC().Truncate(time.Microsecond) // pg timestamptz µs resolution
	return &Workload{
		ID:                uuid.NewString(),
		WorkspaceID:       "ws-ping",
		SubmittedByUserID: "user-ping-ci",
		Type:              TypeIOSBuild,
		Priority:          "high",
		Status:            StatusQueued,
		SubmittedAt:       now,
		Labels: map[string]string{
			"build_id":   "bld_f1afa5da",
			"repo":       "ping-cash/ping-cash",
			"git_commit": "f1afa5da9c",
		},
		IOSBuild: &IOSBuildSpec{
			SourceTarballS3Key: "s3://iogrid-src/ping/f1afa5da.tar.gz",
			TartImage:          "ghcr.io/cirruslabs/macos-sequoia-xcode:26",
			BuildCommands:      []string{"pod install", "xcodebuild -scheme Ping"},
			ArtifactBucket:     "iogrid-artifacts",
			ArtifactPrefix:     "ping/f1afa5da",
			RepoURL:            "https://github.com/ping-cash/ping-cash",
			GitRef:             "refs/heads/main",
			BuildCommand:       "fastlane build",
			UploadURL:          "https://upload.iogrid.org/ping/f1afa5da",
			ArtifactGuestPath:  "/tmp/Ping.app",
			CPU:                8,
			MemoryMiB:          16384,
			BootTimeoutSecs:    600,
		},
	}
}

// TestPostgres_WorkloadRoundTripFieldComplete asserts every persisted Workload
// field (incl. the full IOSBuildSpec + labels) survives Create → Get.
func TestPostgres_WorkloadRoundTripFieldComplete(t *testing.T) {
	pool, cleanup := newPostgresPool(t)
	defer cleanup()
	wipeAndMigrate(t, pool)
	s := NewPostgres(pool)
	ctx := context.Background()

	want := fullIOSWorkload()
	if err := s.CreateWorkload(ctx, want); err != nil {
		t.Fatalf("CreateWorkload: %v", err)
	}

	got, err := s.GetWorkload(ctx, want.ID)
	if err != nil {
		t.Fatalf("GetWorkload: %v", err)
	}

	if got.ID != want.ID || got.WorkspaceID != want.WorkspaceID ||
		got.SubmittedByUserID != want.SubmittedByUserID || got.Type != want.Type ||
		got.Priority != want.Priority || got.Status != want.Status {
		t.Errorf("scalar mismatch:\n got=%+v\nwant=%+v", got, want)
	}
	if !got.SubmittedAt.Equal(want.SubmittedAt) {
		t.Errorf("SubmittedAt = %v, want %v", got.SubmittedAt, want.SubmittedAt)
	}
	if got.Labels["build_id"] != "bld_f1afa5da" || got.Labels["repo"] != "ping-cash/ping-cash" {
		t.Errorf("labels not round-tripped: %+v", got.Labels)
	}
	if got.IOSBuild == nil {
		t.Fatalf("IOSBuild spec lost on round-trip")
	}
	gb, wb := got.IOSBuild, want.IOSBuild
	if gb.SourceTarballS3Key != wb.SourceTarballS3Key || gb.TartImage != wb.TartImage ||
		gb.ArtifactBucket != wb.ArtifactBucket || gb.ArtifactPrefix != wb.ArtifactPrefix ||
		gb.RepoURL != wb.RepoURL || gb.GitRef != wb.GitRef || gb.BuildCommand != wb.BuildCommand ||
		gb.UploadURL != wb.UploadURL || gb.ArtifactGuestPath != wb.ArtifactGuestPath ||
		gb.CPU != wb.CPU || gb.MemoryMiB != wb.MemoryMiB || gb.BootTimeoutSecs != wb.BootTimeoutSecs {
		t.Errorf("IOSBuildSpec mismatch:\n got=%+v\nwant=%+v", gb, wb)
	}
	if len(gb.BuildCommands) != len(wb.BuildCommands) {
		t.Errorf("BuildCommands len = %d, want %d", len(gb.BuildCommands), len(wb.BuildCommands))
	}
	// The other specs MUST be nil (type-discriminated invariant preserved).
	if got.Bandwidth != nil || got.Docker != nil || got.GPU != nil {
		t.Errorf("non-iOS specs should be nil: bw=%v dk=%v gpu=%v", got.Bandwidth, got.Docker, got.GPU)
	}
}

// TestPostgres_AssignmentRoundTrip asserts the full Assignment survives
// Create → Get and that UpdateAssignment + SetWorkloadResult persist.
func TestPostgres_AssignmentRoundTrip(t *testing.T) {
	pool, cleanup := newPostgresPool(t)
	defer cleanup()
	wipeAndMigrate(t, pool)
	s := NewPostgres(pool)
	ctx := context.Background()

	w := fullIOSWorkload()
	if err := s.CreateWorkload(ctx, w); err != nil {
		t.Fatalf("CreateWorkload: %v", err)
	}
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	a := &Assignment{
		ID:           uuid.NewString(),
		WorkloadID:   w.ID,
		ProviderID:   "808ce330-prov",
		Deadline:     deadline,
		LatestStatus: StatusDispatched,
	}
	if err := s.CreateAssignment(ctx, a); err != nil {
		t.Fatalf("CreateAssignment: %v", err)
	}

	got, err := s.GetAssignment(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetAssignment: %v", err)
	}
	if got.WorkloadID != a.WorkloadID || got.ProviderID != a.ProviderID ||
		got.LatestStatus != StatusDispatched || !got.Deadline.Equal(deadline) {
		t.Errorf("assignment mismatch:\n got=%+v\nwant=%+v", got, a)
	}

	// Drain it (the terminal path): move off "dispatched".
	got.LatestStatus = StatusSucceeded
	if err := s.UpdateAssignment(ctx, got); err != nil {
		t.Fatalf("UpdateAssignment: %v", err)
	}
	reread, _ := s.GetAssignment(ctx, a.ID)
	if reread.LatestStatus != StatusSucceeded {
		t.Errorf("latest_status after update = %q, want succeeded", reread.LatestStatus)
	}

	// Result blob persists.
	if err := s.SetWorkloadResult(ctx, w.ID, &Result{
		TerminalStatus: "succeeded", ExitCode: 0, ArtifactS3Keys: []string{"ping/f1afa5da/Ping.app"},
	}); err != nil {
		t.Fatalf("SetWorkloadResult: %v", err)
	}
	wl, _ := s.GetWorkload(ctx, w.ID)
	if wl.Result == nil || wl.Result.TerminalStatus != "succeeded" ||
		len(wl.Result.ArtifactS3Keys) != 1 {
		t.Errorf("result not round-tripped: %+v", wl.Result)
	}
}

// TestPostgres_CrossReplicaAssignmentSurvives is the headline #771 test. It
// builds TWO independent store instances over the SAME database — modelling
// replica A (handles the daemon's poll, creates the assignment) and replica B
// (handles the daemon's terminal-status POST 5.5 min later). With the OLD
// in-memory store, replica B's GetAssignment 404s and the build never settles.
// With the shared Postgres store, B resolves the assignment, drains it, and
// flips the workload terminal — exactly what unblocks ForwardStatus → metering
// → $GRID settle.
func TestPostgres_CrossReplicaAssignmentSurvives(t *testing.T) {
	// Replica A's pool.
	poolA, cleanupA := newPostgresPool(t)
	defer cleanupA()
	wipeAndMigrate(t, poolA)
	replicaA := NewPostgres(poolA)

	// Replica B: a SEPARATE pool (distinct connections) over the same DB. This
	// is the crux — a fresh process-state store that never saw A's writes
	// except through Postgres.
	ctxB, cancelB := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelB()
	poolB, err := pgxpool.New(ctxB, postgresDSN())
	if err != nil {
		t.Fatalf("replica B pool: %v", err)
	}
	defer poolB.Close()
	replicaB := NewPostgres(poolB)

	ctx := context.Background()

	// --- Replica A: submit + dispatch (poll lands here) ---
	w := fullIOSWorkload()
	if err := replicaA.CreateWorkload(ctx, w); err != nil {
		t.Fatalf("A CreateWorkload: %v", err)
	}
	attemptID := uuid.NewString()
	if err := replicaA.CreateAssignment(ctx, &Assignment{
		ID:           attemptID,
		WorkloadID:   w.ID,
		ProviderID:   "808ce330-prov",
		Deadline:     time.Now().UTC().Add(time.Hour),
		LatestStatus: StatusDispatched,
	}); err != nil {
		t.Fatalf("A CreateAssignment: %v", err)
	}
	// A also marks the workload dispatched + the daemon reported RUNNING via A.
	_ = replicaA.UpdateWorkloadStatus(ctx, w.ID, StatusDispatched, "dispatched to provider")

	// The daemon polls A and gets exactly this one assignment.
	pending, err := replicaA.ListPendingAssignments(ctx, "808ce330-prov")
	if err != nil || len(pending) != 1 || pending[0].ID != attemptID {
		t.Fatalf("A ListPendingAssignments = %+v (err %v), want the one attempt %s", pending, err, attemptID)
	}

	// --- Replica B: the terminal-status POST lands here 5.5 min later ---
	// This is the call that 404'd with the in-memory store.
	a, err := replicaB.GetAssignment(ctx, attemptID)
	if err != nil {
		t.Fatalf("B GetAssignment(%s) FAILED — this is the #771 404: %v", attemptID, err)
	}
	if a.ProviderID != "808ce330-prov" {
		t.Errorf("B resolved wrong provider: %q", a.ProviderID)
	}

	// B drives the exact drain the handler does: mark assignment + workload
	// terminal, set the result.
	a.LatestStatus = StatusSucceeded
	if err := replicaB.UpdateAssignment(ctx, a); err != nil {
		t.Fatalf("B UpdateAssignment: %v", err)
	}
	if err := replicaB.UpdateWorkloadStatus(ctx, a.WorkloadID, StatusSucceeded, "exit 0"); err != nil {
		t.Fatalf("B UpdateWorkloadStatus: %v", err)
	}
	if err := replicaB.SetWorkloadResult(ctx, a.WorkloadID, &Result{TerminalStatus: "succeeded"}); err != nil {
		t.Fatalf("B SetWorkloadResult: %v", err)
	}

	// B MUST be able to resolve the build_id label (the ForwardStatus routing
	// key) from the workload it never created — the metering/settle hop.
	wlB, err := replicaB.GetWorkload(ctx, a.WorkloadID)
	if err != nil {
		t.Fatalf("B GetWorkload: %v", err)
	}
	if wlB.Labels["build_id"] != "bld_f1afa5da" {
		t.Fatalf("B could not resolve build_id (settle key) cross-replica: %+v", wlB.Labels)
	}

	// --- Replica A observes the terminal state (shared store) ---
	wlA, err := replicaA.GetWorkload(ctx, w.ID)
	if err != nil {
		t.Fatalf("A GetWorkload after B drain: %v", err)
	}
	if wlA.Status != StatusSucceeded {
		t.Errorf("A sees status %q after B's drain, want succeeded", wlA.Status)
	}
	if wlA.FinishedAt.IsZero() {
		t.Errorf("A sees no FinishedAt after B's terminal transition")
	}

	// And the assignment has drained off the poll list (no re-run on restart).
	pendingAfter, _ := replicaA.ListPendingAssignments(ctx, "808ce330-prov")
	if len(pendingAfter) != 0 {
		t.Errorf("assignment still pending after terminal drain: %+v", pendingAfter)
	}
}

// TestPostgres_GetAssignmentNotFound mirrors memStore's ErrNotFound contract:
// the handler returns 404 only for a genuinely-absent attempt, never a SQL
// error.
func TestPostgres_GetAssignmentNotFound(t *testing.T) {
	pool, cleanup := newPostgresPool(t)
	defer cleanup()
	wipeAndMigrate(t, pool)
	s := NewPostgres(pool)

	_, err := s.GetAssignment(context.Background(), uuid.NewString())
	if err != ErrNotFound {
		t.Errorf("GetAssignment(missing) = %v, want ErrNotFound", err)
	}
	// A non-UUID id is also "not found", not a parse-error leak.
	if _, err := s.GetAssignment(context.Background(), "not-a-uuid"); err != ErrNotFound {
		t.Errorf("GetAssignment(non-uuid) = %v, want ErrNotFound", err)
	}
}
