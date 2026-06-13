//go:build integration

// #771 end-to-end cross-replica test for the poll-dispatch status route.
//
// This exercises the EXACT HTTP handler that 404'd in prod
// (assignedWorkloadStatusHandler — POST
// /v1/providers/{id}/assigned-workloads/{attempt}/status) but with a
// Postgres-backed store split across two pools, so the assignment is created
// by "replica A" and the terminal-status POST is served by "replica B" — the
// boundary the in-memory store could not cross. The assertion is binary: the
// route returns 200 (not 404) AND the build-gateway ForwardStatus fires, which
// is what unblocks metering / $GRID settle for every completed build.
//
// Run with its OWN throwaway DB (distinct from the store suite's
// workloads_svc_test so the two suites can run in parallel against one
// postgres server without colliding on DROP/CREATE TABLE):
//
//	docker run --rm -d -p 55434:5432 \
//	    -e POSTGRES_PASSWORD=postgres postgres:16
//	# create both throwaway DBs once:
//	psql "postgres://postgres:postgres@localhost:55434/postgres?sslmode=disable" \
//	    -c 'CREATE DATABASE workloads_svc_test' -c 'CREATE DATABASE workloads_svc_srv_test'
//	go test -tags=integration ./internal/server/...
//
// When WORKLOADS_SVC_SRV_DATABASE_URL is set it wins; else DATABASE_URL with
// its dbname swapped to workloads_svc_srv_test; else the localhost default.
// Skips cleanly if the DB is unreachable.
//
// Refs #771, #740, #770.

package server

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	wdb "github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/db"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/dispatcher"
	"github.com/iogrid/iogrid/coordinator/services/workloads-svc/internal/store"
)

const pgDSNDefault = "postgres://postgres:postgres@localhost:55434/workloads_svc_srv_test?sslmode=disable"

// pgDSN resolves a dedicated DB for the server suite so it never shares the
// store suite's database (concurrent DROP/CREATE on one DB collides on
// pg_type — a test-harness race, not a product bug). Precedence:
//  1. WORKLOADS_SVC_SRV_DATABASE_URL (explicit override),
//  2. DATABASE_URL with its dbname rewritten to workloads_svc_srv_test,
//  3. the localhost default.
func pgDSN() string {
	if v := os.Getenv("WORKLOADS_SVC_SRV_DATABASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return swapDBName(v, "workloads_svc_srv_test")
	}
	return pgDSNDefault
}

// swapDBName replaces the path component (database name) of a libpq URL,
// preserving the query string. Falls back to the input unchanged if it doesn't
// parse as a URL with a path.
func swapDBName(dsn, db string) string {
	q := ""
	if i := strings.IndexByte(dsn, '?'); i >= 0 {
		q = dsn[i:]
		dsn = dsn[:i]
	}
	if i := strings.LastIndexByte(dsn, '/'); i >= 0 {
		return dsn[:i+1] + db + q
	}
	return dsn + q
}

func pgPoolOrSkip(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, pgDSN())
	if err != nil {
		t.Skipf("postgres pool create failed (DATABASE_URL=%q): %v", pgDSN(), err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres ping failed (DATABASE_URL=%q): %v", pgDSN(), err)
	}
	return pool
}

// TestCrossReplicaTerminalStatusForwards proves the #771 fix at the HTTP layer:
// assignment created on replica A's pool, terminal status POSTed to a router
// wired to replica B's pool → 200 + ForwardStatus fires (was 404 + no forward).
func TestCrossReplicaTerminalStatusForwards(t *testing.T) {
	const prov = "808ce330-0000-0000-0000-0000000000aa"

	// Replica A: create + migrate, then write the workload + assignment.
	poolA := pgPoolOrSkip(t)
	defer poolA.Close()

	wipeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, s := range []string{
		`DROP TABLE IF EXISTS workload_assignments CASCADE`,
		`DROP TABLE IF EXISTS workloads CASCADE`,
		`DROP TABLE IF EXISTS goose_db_version CASCADE`,
	} {
		if _, err := poolA.Exec(wipeCtx, s); err != nil {
			t.Fatalf("wipe (%s): %v", s, err)
		}
	}
	if err := wdb.Apply(context.Background(), pgDSN()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	replicaA := store.NewPostgres(poolA)
	ctx := context.Background()
	attemptID := uuid.NewString()
	wl := &store.Workload{
		ID:     uuid.NewString(),
		Type:   store.TypeIOSBuild,
		Status: store.StatusRunning,
		Labels: map[string]string{"build_id": "bld_f1afa5da"},
		IOSBuild: &store.IOSBuildSpec{
			RepoURL: "https://github.com/ping-cash/ping-cash", GitRef: "main", BuildCommand: "fastlane build",
		},
	}
	if err := replicaA.CreateWorkload(ctx, wl); err != nil {
		t.Fatalf("A CreateWorkload: %v", err)
	}
	if err := replicaA.CreateAssignment(ctx, &store.Assignment{
		ID: attemptID, WorkloadID: wl.ID, ProviderID: prov,
		Deadline: time.Now().UTC().Add(time.Hour), LatestStatus: store.StatusDispatched,
	}); err != nil {
		t.Fatalf("A CreateAssignment: %v", err)
	}

	// Replica B: a SEPARATE pool over the same DB → a router that never saw
	// A's in-process state. This is the replica the LB lands the terminal POST
	// on.
	poolB := pgPoolOrSkip(t)
	defer poolB.Close()
	replicaB := store.NewPostgres(poolB)

	fwd := &recordingForwarder{}
	r := chi.NewRouter()
	r.Group(Mount(Deps{
		Store:        replicaB,
		Dispatcher:   dispatcher.New(replicaB, nil),
		BuildGateway: fwd,
		Log:          slog.Default(),
	}))
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(
		srv.URL+"/v1/providers/"+prov+"/assigned-workloads/"+attemptID+"/status",
		"application/json",
		strings.NewReader(`{"status":"succeeded","exit_code":0,"note":"iOS build finished"}`),
	)
	if err != nil {
		t.Fatalf("status POST: %v", err)
	}
	defer resp.Body.Close()

	// The #771 assertion: 200, not the 404 the in-memory store returned when
	// the POST landed on a replica that never created the assignment.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("terminal status POST on replica B: want 200, got %d (this is the #771 cross-replica 404)", resp.StatusCode)
	}

	// And the build-gateway forward fired → metering / $GRID settle proceeds.
	if len(fwd.calls) != 1 {
		t.Fatalf("want exactly 1 ForwardStatus to build-gateway, got %d", len(fwd.calls))
	}
	if c := fwd.calls[0]; c.buildID != "bld_f1afa5da" || c.providerID != prov ||
		c.status != "succeeded" || c.exitCode != 0 {
		t.Fatalf("unexpected forward: %+v", c)
	}

	// The workload is terminal in the shared store (replica A would now see
	// succeeded too).
	got, err := replicaA.GetWorkload(ctx, wl.ID)
	if err != nil {
		t.Fatalf("A GetWorkload: %v", err)
	}
	if got.Status != store.StatusSucceeded {
		t.Errorf("workload status = %q after cross-replica terminal POST, want succeeded", got.Status)
	}
}
