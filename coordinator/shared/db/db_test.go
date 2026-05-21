// Package db unit tests. These focus on argument validation so they
// can run in CI without a live Postgres; the actual goose-up execution
// is covered by per-service integration tests under
// internal/auth/integration_test.go and internal/store/store_pg_test.go.
package db

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
)

func TestMigrateUpFS_RejectsEmptyURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	fsys := fstest.MapFS{
		"migrations/0001_noop.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n-- +goose Down\n")},
	}
	err := MigrateUpFS(context.Background(), "", fsys, "migrations")
	if err == nil {
		t.Fatalf("expected error when DATABASE_URL is empty, got nil")
	}
	if !strings.Contains(err.Error(), "no database url") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestMigrateUpFS_RejectsBadDSN(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0001_noop.sql": &fstest.MapFile{Data: []byte("-- +goose Up\n-- +goose Down\n")},
	}
	// Use a DSN that parses but can't dial — sql.Open returns nil error
	// (it's lazy), but goose.UpContext will fail on the actual query.
	// What we really want to check is that the function returns *some*
	// error rather than panic'ing or hanging.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already-cancelled context guarantees a fast fail
	err := MigrateUpFS(ctx, "postgres://nobody:nothing@127.0.0.1:1/none?connect_timeout=1", fsys, "migrations")
	if err == nil {
		t.Fatalf("expected an error against an unreachable DSN, got nil")
	}
}

func TestMigrateUp_RejectsEmptyURL(t *testing.T) {
	// The original dir-based helper must keep working too — it's still
	// exported for any caller that ships migrations as a real directory.
	t.Setenv("DATABASE_URL", "")
	err := MigrateUp(context.Background(), "", "/nonexistent")
	if err == nil {
		t.Fatalf("expected error when DATABASE_URL is empty, got nil")
	}
}
