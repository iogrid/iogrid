// Package db embeds the goose migration files into the providers-svc binary
// so production deployments don't need to ship a separate migrations
// directory. Migrations are applied at startup before the readiness latch
// flips, exactly like identity-svc.
package db

import (
	"context"
	"embed"

	shareddb "github.com/iogrid/iogrid/coordinator/shared/db"
)

// Migrations is the embedded SQL bundle. Each new schema change adds a
// numbered file under migrations/.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Apply runs every pending Up migration against the supplied URL.
// Idempotent across pod restarts because goose maintains its own
// bookkeeping table (goose_db_version).
//
// Thin shim over the shared helper — keeps the call sites in main.go
// and the integration tests stable while the actual goose plumbing
// lives in coordinator/shared/db so every coordinator service runs
// identical migration code.
func Apply(ctx context.Context, databaseURL string) error {
	return shareddb.MigrateUpFS(ctx, databaseURL, Migrations, "migrations")
}
