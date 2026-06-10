// Postgres-backed Store implementation for the build-gateway.
//
// Builds MUST survive a pod restart in production: a customer who submits an
// iOS build and then polls GET /v1/builds/{id} must not get a 404 because the
// gateway pod rolled. The InMemory store (store.go) loses everything on
// restart, so production wires this Postgres-backed store via DATABASE_URL
// (see cmd/build-gateway/main.go).
//
// Schema is created idempotently on construction (EnsureSchema) — a single
// `builds` table whose JSONB column carries the full build record. We do NOT
// shard the build's sub-objects (artifacts, webhook, env) into separate
// columns/tables: the gateway only ever reads/writes whole-build records, and
// JSONB keeps the schema in lockstep with the Go struct without a migration
// per field. The few columns we DO project (workspace_id, status,
// submitted_at) exist purely so the tenancy-scoped List query can filter and
// order in the database.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
)

// Postgres is a durable Store backed by a single `builds` table. Safe for
// concurrent use (pgxpool is concurrency-safe; per-build mutations run inside
// a serialisable-by-row UPDATE … RETURNING transaction).
type Postgres struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// persistedBuild is the on-disk JSONB shape. It mirrors builds.Build but
// serialises EVERY field — including the ones builds.Build hides from the
// customer-facing HTTP JSON via `json:"-"` (ArtifactBucket/ArtifactPrefix/
// ProviderAttemptID, Artifact.S3Key, Webhook.Secret). Persisting via the
// public json tags would silently drop those, breaking artifact uploads,
// pre-signed downloads, and webhook HMAC signing after a pod restart. We keep
// this DTO private to the store so the redaction guarantees on the public
// model stay intact.
type persistedBuild struct {
	ID                string              `json:"id"`
	WorkspaceID       string              `json:"workspace_id"`
	SubmittedByUserID string              `json:"submitted_by_user_id"`
	GitURL            string              `json:"git_url"`
	GitRef            string              `json:"git_ref"`
	XcodeVersion      string              `json:"xcode_version"`
	BuildCommand      string              `json:"build_command"`
	SigningTeamID     string              `json:"signing_team_id"`
	EnvVars           map[string]string   `json:"env_vars"`
	Status            builds.Status       `json:"status"`
	StatusNote        string              `json:"status_note"`
	ExitCode          int32               `json:"exit_code"`
	ArtifactBucket    string              `json:"artifact_bucket"`
	ArtifactPrefix    string              `json:"artifact_prefix"`
	Artifacts         []persistedArtifact `json:"artifacts"`
	Webhook           *persistedWebhook   `json:"webhook"`
	SubmittedAt       time.Time           `json:"submitted_at"`
	StartedAt         *time.Time          `json:"started_at"`
	FinishedAt        *time.Time          `json:"finished_at"`
	ProviderAttemptID string              `json:"provider_attempt_id"`
}

type persistedArtifact struct {
	Name        string    `json:"name"`
	SizeBytes   int64     `json:"size_bytes"`
	S3Key       string    `json:"s3_key"`
	ContentType string    `json:"content_type"`
	SHA256      string    `json:"sha256"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

type persistedWebhook struct {
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

func toPersisted(b *builds.Build) *persistedBuild {
	p := &persistedBuild{
		ID:                b.ID,
		WorkspaceID:       b.WorkspaceID,
		SubmittedByUserID: b.SubmittedByUserID,
		GitURL:            b.GitURL,
		GitRef:            b.GitRef,
		XcodeVersion:      b.XcodeVersion,
		BuildCommand:      b.BuildCommand,
		SigningTeamID:     b.SigningTeamID,
		EnvVars:           b.EnvVars,
		Status:            b.Status,
		StatusNote:        b.StatusNote,
		ExitCode:          b.ExitCode,
		ArtifactBucket:    b.ArtifactBucket,
		ArtifactPrefix:    b.ArtifactPrefix,
		SubmittedAt:       b.SubmittedAt,
		StartedAt:         b.StartedAt,
		FinishedAt:        b.FinishedAt,
		ProviderAttemptID: b.ProviderAttemptID,
	}
	for _, a := range b.Artifacts {
		p.Artifacts = append(p.Artifacts, persistedArtifact{
			Name:        a.Name,
			SizeBytes:   a.SizeBytes,
			S3Key:       a.S3Key,
			ContentType: a.ContentType,
			SHA256:      a.SHA256,
			UploadedAt:  a.UploadedAt,
		})
	}
	if b.Webhook != nil {
		p.Webhook = &persistedWebhook{URL: b.Webhook.URL, Secret: b.Webhook.Secret}
	}
	return p
}

func (p *persistedBuild) toBuild() *builds.Build {
	b := &builds.Build{
		ID:                p.ID,
		WorkspaceID:       p.WorkspaceID,
		SubmittedByUserID: p.SubmittedByUserID,
		GitURL:            p.GitURL,
		GitRef:            p.GitRef,
		XcodeVersion:      p.XcodeVersion,
		BuildCommand:      p.BuildCommand,
		SigningTeamID:     p.SigningTeamID,
		EnvVars:           p.EnvVars,
		Status:            p.Status,
		StatusNote:        p.StatusNote,
		ExitCode:          p.ExitCode,
		ArtifactBucket:    p.ArtifactBucket,
		ArtifactPrefix:    p.ArtifactPrefix,
		SubmittedAt:       p.SubmittedAt,
		StartedAt:         p.StartedAt,
		FinishedAt:        p.FinishedAt,
		ProviderAttemptID: p.ProviderAttemptID,
	}
	for _, a := range p.Artifacts {
		b.Artifacts = append(b.Artifacts, builds.Artifact{
			Name:        a.Name,
			SizeBytes:   a.SizeBytes,
			S3Key:       a.S3Key,
			ContentType: a.ContentType,
			SHA256:      a.SHA256,
			UploadedAt:  a.UploadedAt,
		})
	}
	if p.Webhook != nil {
		b.Webhook = &builds.Webhook{URL: p.Webhook.URL, Secret: p.Webhook.Secret}
	}
	return b
}

// NewPostgres wires a Postgres-backed store over an existing pool. Callers
// SHOULD call EnsureSchema once at boot before serving traffic. now is the
// clock source — pass nil for time.Now.
func NewPostgres(pool *pgxpool.Pool, now func() time.Time) *Postgres {
	if now == nil {
		now = time.Now
	}
	return &Postgres{pool: pool, now: now}
}

// EnsureSchema creates the `builds` table if it does not already exist. It is
// idempotent and safe to call on every boot.
func (s *Postgres) EnsureSchema(ctx context.Context) error {
	const ddl = `
CREATE TABLE IF NOT EXISTS build_gateway_builds (
    id            TEXT PRIMARY KEY,
    workspace_id  TEXT NOT NULL,
    status        TEXT NOT NULL,
    submitted_at  TIMESTAMPTZ NOT NULL,
    record        JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_build_gateway_builds_ws_submitted
    ON build_gateway_builds (workspace_id, submitted_at DESC);
`
	if _, err := s.pool.Exec(ctx, ddl); err != nil {
		return fmt.Errorf("build-gateway: ensure schema: %w", err)
	}
	return nil
}

// Create implements Store.
func (s *Postgres) Create(ctx context.Context, b *builds.Build) error {
	if b == nil || b.ID == "" {
		return errors.New("build: id required")
	}
	if b.WorkspaceID == "" {
		return errors.New("build: workspace_id required")
	}
	if b.SubmittedAt.IsZero() {
		b.SubmittedAt = s.now()
	}
	if b.Status == "" {
		b.Status = builds.StatusQueued
	}
	raw, err := json.Marshal(toPersisted(b))
	if err != nil {
		return fmt.Errorf("build: marshal: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
INSERT INTO build_gateway_builds (id, workspace_id, status, submitted_at, record)
VALUES ($1, $2, $3, $4, $5)
`, b.ID, b.WorkspaceID, string(b.Status), b.SubmittedAt, raw)
	if err != nil {
		// pgx surfaces a unique_violation as a *pgconn.PgError; we keep the
		// error message stable with the InMemory store ("id collision") so
		// the service layer's branching stays backend-agnostic.
		return fmt.Errorf("build: insert (possible id collision): %w", err)
	}
	return nil
}

// Get implements Store — workspace-scoped for tenancy.
func (s *Postgres) Get(ctx context.Context, workspaceID, id string) (*builds.Build, error) {
	row := s.pool.QueryRow(ctx, `
SELECT record FROM build_gateway_builds WHERE id = $1 AND workspace_id = $2
`, id, workspaceID)
	return scanBuild(row)
}

// GetByIDInternal implements Store — no workspace check (dispatch-token /
// mTLS callers only).
func (s *Postgres) GetByIDInternal(ctx context.Context, id string) (*builds.Build, error) {
	row := s.pool.QueryRow(ctx, `
SELECT record FROM build_gateway_builds WHERE id = $1
`, id)
	return scanBuild(row)
}

// Update implements Store. The mutator runs inside a transaction that selects
// the current record FOR UPDATE, applies the mutation in Go, and writes the
// new record back — giving the same read-modify-write atomicity the InMemory
// store gets from its mutex.
func (s *Postgres) Update(ctx context.Context, id string, mutator func(*builds.Build) error) (*builds.Build, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("build: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `
SELECT record FROM build_gateway_builds WHERE id = $1 FOR UPDATE
`, id)
	b, err := scanBuild(row)
	if err != nil {
		return nil, err
	}
	if err := mutator(b); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(toPersisted(b))
	if err != nil {
		return nil, fmt.Errorf("build: marshal: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE build_gateway_builds
   SET workspace_id = $2, status = $3, record = $4
 WHERE id = $1
`, b.ID, b.WorkspaceID, string(b.Status), raw); err != nil {
		return nil, fmt.Errorf("build: update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("build: commit: %w", err)
	}
	clone := *b
	if b.Artifacts != nil {
		clone.Artifacts = append([]builds.Artifact(nil), b.Artifacts...)
	}
	return &clone, nil
}

// List implements Store — newest-first, optionally filtered by status.
func (s *Postgres) List(ctx context.Context, workspaceID string, status builds.Status, limit int) ([]*builds.Build, error) {
	q := `SELECT record FROM build_gateway_builds WHERE ($1 = '' OR workspace_id = $1)`
	args := []any{workspaceID}
	if status != "" {
		q += ` AND status = $2`
		args = append(args, string(status))
	}
	q += ` ORDER BY submitted_at DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("build: list: %w", err)
	}
	defer rows.Close()
	out := make([]*builds.Build, 0)
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("build: list rows: %w", err)
	}
	return out, nil
}

// scanRow is the minimal surface shared by pgx.Row and pgx.Rows.
type scanRow interface {
	Scan(dest ...any) error
}

// scanBuild decodes a single record JSONB column into a *builds.Build,
// translating pgx's no-rows sentinel into the store's ErrNotFound so the HTTP
// layer keeps mapping it to 404.
func scanBuild(row scanRow) (*builds.Build, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("build: scan: %w", err)
	}
	var p persistedBuild
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("build: unmarshal: %w", err)
	}
	return p.toBuild(), nil
}
