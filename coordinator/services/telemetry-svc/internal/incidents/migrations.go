// Package incidents implements the operator-curated incident timeline,
// the email-subscription registry, and the daily uptime sample ledger
// behind the public status page at status.iogrid.org.
//
// Storage is split into two implementations behind the [Store]
// interface:
//
//   - [InMemory] — keeps everything in process memory. Used by unit
//     tests, the local-dev binary (no DATABASE_URL), and as a
//     deliberate fallback when Postgres is unavailable so the public
//     /status page never goes dark just because the DB is down.
//   - [Postgres] — pgx-backed implementation against the schema in
//     ./migrations/0001_init.sql. Used in prod.
//
// All public read endpoints (/status, /status/posture, /status/uptime)
// are intentionally world-readable; mutating endpoints
// (/status/incidents, /status/subscribe) gate behind either a
// shared-secret admin token (incident management) or a public
// rate-limited path (subscribe).
package incidents

import (
	"context"
	"embed"

	shareddb "github.com/iogrid/iogrid/coordinator/shared/db"
)

// Migrations is the embedded SQL bundle.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Apply runs every pending Up migration against the supplied URL.
// Idempotent across pod restarts.
//
// Thin shim over the shared helper — keeps the call site in main.go
// stable while the actual goose plumbing lives in
// coordinator/shared/db so every coordinator service runs identical
// migration code.
func Apply(ctx context.Context, databaseURL string) error {
	return shareddb.MigrateUpFS(ctx, databaseURL, Migrations, "migrations")
}
