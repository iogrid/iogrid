// Package db provides a shared pgx pool factory and a goose-based migration
// runner for iogrid coordinator services. Each service owns its own logical
// database (postgres-per-service) but they all share the same physical CNPG
// cluster.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// Config holds the parameters needed to connect to Postgres.
type Config struct {
	// URL is a libpq-style connection string. When empty, falls back to
	// the DATABASE_URL env var.
	URL string
	// MaxConns caps the pool size. Defaults to 10.
	MaxConns int32
	// ConnectTimeout caps how long Connect() will block. Defaults to 10s.
	ConnectTimeout time.Duration
}

// NewPool opens a pgx connection pool. It does NOT ping the database
// synchronously — call Ping() yourself or rely on the /readyz probe.
func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	url := cfg.URL
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		return nil, fmt.Errorf("no database url configured (set DATABASE_URL or Config.URL)")
	}

	pgxCfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse db url: %w", err)
	}
	if cfg.MaxConns > 0 {
		pgxCfg.MaxConns = cfg.MaxConns
	} else {
		pgxCfg.MaxConns = 10
	}
	pgxCfg.MaxConnLifetime = 30 * time.Minute
	pgxCfg.MaxConnIdleTime = 5 * time.Minute

	timeout := cfg.ConnectTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(dialCtx, pgxCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	return pool, nil
}

// PingProbe returns a ReadinessProbe-compatible function that pings the
// database. Use it via health.Registry.AddProbe.
func PingProbe(pool *pgxpool.Pool) func() error {
	return func() error {
		if pool == nil {
			return fmt.Errorf("db pool is nil")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return pool.Ping(ctx)
	}
}

// MigrateUp runs all pending goose migrations from the given directory.
// goose uses database/sql, so we open a sql.DB over the same connection
// string (pgx stdlib driver) and close it immediately after migration.
func MigrateUp(ctx context.Context, databaseURL, migrationsDir string) error {
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		return fmt.Errorf("no database url configured")
	}
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("sql.Open: %w", err)
	}
	defer sqlDB.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, migrationsDir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// MigrateUpFS runs all pending goose-up migrations from an embedded
// (or otherwise virtual) filesystem rooted at the given directory.
//
// Services own their migrations as `//go:embed migrations/*.sql` so the
// binary is self-contained — no separate migrations directory needs to
// ship with the container. Every coordinator service that owns a Store
// must call this exactly once at startup, AFTER db.NewPool but BEFORE
// store.New, so the schema exists before any request handler can touch
// it.
//
// goose maintains its own bookkeeping table (goose_db_version), so this
// is idempotent across pod restarts.
func MigrateUpFS(ctx context.Context, databaseURL string, fsys fs.FS, dir string) error {
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		return fmt.Errorf("no database url configured")
	}
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("sql.Open: %w", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(fsys)
	defer goose.SetBaseFS(nil)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, dir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	return nil
}

// Ensure pgx stdlib is linked so database/sql can find the "pgx" driver
// when goose opens its sql.DB.
var _ = stdlib.GetDefaultDriver
