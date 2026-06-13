package db

import (
	"context"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

// migrationsFS embeds the *.sql files in this directory at compile time so the
// running binary doesn't depend on a filesystem path. Each file is run once
// at startup via Apply(). Mirrors the vpn-svc db package (the established
// per-service migration pattern in this repo).
//
//go:embed *.sql
var migrationsFS embed.FS

// Apply runs all embedded migrations against the given DATABASE_URL.
func Apply(ctx context.Context, databaseURL string) error {
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil) // reset so a future caller with no embed.FS doesn't get stuck on ours

	db, err := goose.OpenDBWithDriver("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	// "." means: use the root of the embed.FS we just registered.
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("up: %w", err)
	}
	return nil
}
