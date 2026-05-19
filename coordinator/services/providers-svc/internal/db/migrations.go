// Package db embeds the goose migration files into the providers-svc binary
// so production deployments don't need to ship a separate migrations
// directory. Migrations are applied at startup before the readiness latch
// flips, exactly like identity-svc.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Migrations is the embedded SQL bundle. Each new schema change adds a
// numbered file under migrations/.
//
//go:embed migrations/*.sql
var Migrations embed.FS

// Apply runs every pending Up migration against the supplied URL. Idempotent
// across pod restarts because goose maintains its own bookkeeping table
// (goose_db_version).
func Apply(ctx context.Context, databaseURL string) error {
	if databaseURL == "" {
		return fmt.Errorf("providers-svc: DATABASE_URL is empty")
	}
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("providers-svc: open db for migrations: %w", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(Migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("providers-svc: goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "migrations"); err != nil {
		return fmt.Errorf("providers-svc: goose up: %w", err)
	}
	return nil
}

// Ensure the pgx stdlib driver is linked so sql.Open("pgx") works.
var _ = stdlib.GetDefaultDriver
