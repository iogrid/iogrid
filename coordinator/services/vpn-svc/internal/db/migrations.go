package db

import (
	"context"
	"fmt"

	"github.com/pressly/goose/v3"
)

// Apply runs all embedded migrations.
func Apply(ctx context.Context, databaseURL string) error {
	// Goose will use the embedded FS from migrations/
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	db, err := goose.OpenDBWithDriver("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("up: %w", err)
	}
	return nil
}
