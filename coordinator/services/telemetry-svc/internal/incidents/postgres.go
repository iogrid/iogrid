package incidents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres implements [Store] against a pgx pool. It assumes the
// schema in migrations/0001_init.sql has been applied — see [Apply].
type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgres returns a Postgres-backed store. The pool is owned by
// the caller (open + close).
func NewPostgres(pool *pgxpool.Pool) *Postgres {
	return &Postgres{Pool: pool}
}

func (p *Postgres) CreateIncident(ctx context.Context, in CreateIncidentInput) (*Incident, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("incidents: title required")
	}
	if in.Status == "" {
		in.Status = StatusInvestigating
	}
	if !in.Status.Valid() {
		return nil, fmt.Errorf("incidents: invalid status %q", in.Status)
	}
	if in.Impact == "" {
		in.Impact = ImpactMinor
	}
	if !in.Impact.Valid() {
		return nil, fmt.Errorf("incidents: invalid impact %q", in.Impact)
	}

	tx, err := p.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inc := Incident{
		Title:            strings.TrimSpace(in.Title),
		Body:             in.Body,
		Status:           in.Status,
		Impact:           in.Impact,
		AffectedServices: append([]string(nil), in.AffectedServices...),
	}
	row := tx.QueryRow(ctx, `
		INSERT INTO incidents (title, body, status, impact, affected_services)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, started_at, created_at, updated_at, resolved_at
	`, inc.Title, inc.Body, string(inc.Status), string(inc.Impact), inc.AffectedServices)
	if err := row.Scan(&inc.ID, &inc.StartedAt, &inc.CreatedAt, &inc.UpdatedAt, &inc.ResolvedAt); err != nil {
		return nil, fmt.Errorf("insert incident: %w", err)
	}

	seedBody := firstNonEmpty(in.Body, fmt.Sprintf("Incident opened (status=%s, impact=%s)", in.Status, in.Impact))
	var seed Update
	seed.IncidentID = inc.ID
	seed.Status = in.Status
	seed.Body = seedBody
	row = tx.QueryRow(ctx, `
		INSERT INTO incident_updates (incident_id, status, body)
		VALUES ($1, $2, $3)
		RETURNING id, created_at
	`, inc.ID, string(in.Status), seedBody)
	if err := row.Scan(&seed.ID, &seed.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert seed update: %w", err)
	}

	if in.Status == StatusResolved {
		if _, err := tx.Exec(ctx, `UPDATE incidents SET resolved_at = now() WHERE id = $1 AND resolved_at IS NULL`, inc.ID); err != nil {
			return nil, fmt.Errorf("stamp resolved_at: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	inc.Updates = []Update{seed}
	return &inc, nil
}

func (p *Postgres) GetIncident(ctx context.Context, id uuid.UUID) (*Incident, error) {
	inc := Incident{ID: id}
	var status, impact string
	row := p.Pool.QueryRow(ctx, `
		SELECT title, body, status, impact, affected_services, started_at, resolved_at, created_at, updated_at
		FROM incidents WHERE id = $1
	`, id)
	if err := row.Scan(&inc.Title, &inc.Body, &status, &impact, &inc.AffectedServices, &inc.StartedAt, &inc.ResolvedAt, &inc.CreatedAt, &inc.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get incident: %w", err)
	}
	inc.Status = Status(status)
	inc.Impact = Impact(impact)
	upds, err := p.listUpdates(ctx, id)
	if err != nil {
		return nil, err
	}
	inc.Updates = upds
	return &inc, nil
}

func (p *Postgres) listUpdates(ctx context.Context, id uuid.UUID) ([]Update, error) {
	rows, err := p.Pool.Query(ctx, `
		SELECT id, status, body, created_at
		FROM incident_updates WHERE incident_id = $1 ORDER BY created_at DESC
	`, id)
	if err != nil {
		return nil, fmt.Errorf("list updates: %w", err)
	}
	defer rows.Close()
	var out []Update
	for rows.Next() {
		var u Update
		u.IncidentID = id
		var status string
		if err := rows.Scan(&u.ID, &status, &u.Body, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan update: %w", err)
		}
		u.Status = Status(status)
		out = append(out, u)
	}
	return out, rows.Err()
}

func (p *Postgres) AppendUpdate(ctx context.Context, id uuid.UUID, in UpdateIncidentInput) (*Update, error) {
	if !in.Status.Valid() {
		return nil, fmt.Errorf("incidents: invalid status %q", in.Status)
	}
	if strings.TrimSpace(in.Body) == "" {
		return nil, fmt.Errorf("incidents: update body required")
	}
	tx, err := p.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Confirm the incident exists with a FOR UPDATE lock so concurrent
	// updates don't race on the resolved_at stamp.
	var exists uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM incidents WHERE id = $1 FOR UPDATE`, id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("lock incident: %w", err)
	}

	var u Update
	u.IncidentID = id
	u.Status = in.Status
	u.Body = in.Body
	row := tx.QueryRow(ctx, `
		INSERT INTO incident_updates (incident_id, status, body)
		VALUES ($1, $2, $3) RETURNING id, created_at
	`, id, string(in.Status), in.Body)
	if err := row.Scan(&u.ID, &u.CreatedAt); err != nil {
		return nil, fmt.Errorf("insert update: %w", err)
	}

	if in.Status == StatusResolved {
		if _, err := tx.Exec(ctx, `UPDATE incidents SET status = $2, updated_at = now(), resolved_at = COALESCE(resolved_at, now()) WHERE id = $1`, id, string(in.Status)); err != nil {
			return nil, fmt.Errorf("resolve incident: %w", err)
		}
	} else {
		if _, err := tx.Exec(ctx, `UPDATE incidents SET status = $2, updated_at = now() WHERE id = $1`, id, string(in.Status)); err != nil {
			return nil, fmt.Errorf("update incident status: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &u, nil
}

func (p *Postgres) ListActive(ctx context.Context) ([]Incident, error) {
	return p.listIncidents(ctx, `
		SELECT id, title, body, status, impact, affected_services, started_at, resolved_at, created_at, updated_at
		FROM incidents WHERE resolved_at IS NULL ORDER BY started_at DESC
	`)
}

func (p *Postgres) ListRecent(ctx context.Context, since time.Duration) ([]Incident, error) {
	cutoff := time.Now().Add(-since)
	return p.listIncidents(ctx, `
		SELECT id, title, body, status, impact, affected_services, started_at, resolved_at, created_at, updated_at
		FROM incidents WHERE started_at >= $1 ORDER BY started_at DESC
	`, cutoff)
}

func (p *Postgres) listIncidents(ctx context.Context, query string, args ...any) ([]Incident, error) {
	rows, err := p.Pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()
	var out []Incident
	for rows.Next() {
		var inc Incident
		var status, impact string
		if err := rows.Scan(&inc.ID, &inc.Title, &inc.Body, &status, &impact, &inc.AffectedServices, &inc.StartedAt, &inc.ResolvedAt, &inc.CreatedAt, &inc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan incident: %w", err)
		}
		inc.Status = Status(status)
		inc.Impact = Impact(impact)
		out = append(out, inc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Hydrate updates per incident. The cardinality is bounded (UI shows
	// at most ~20), so a per-row roundtrip is cheap and keeps the SQL
	// simple. Optimise to a single window query if this ever lights up.
	for i := range out {
		upds, err := p.listUpdates(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Updates = upds
	}
	return out, nil
}

func (p *Postgres) UpsertSubscription(ctx context.Context, in SubscribeInput) (*Subscription, error) {
	email := strings.TrimSpace(strings.ToLower(in.Email))
	if !looksLikeEmail(email) {
		return nil, fmt.Errorf("incidents: invalid email %q", in.Email)
	}
	verifyToken := uuid.NewString()
	var s Subscription
	row := p.Pool.QueryRow(ctx, `
		INSERT INTO status_subscriptions (email, verify_token, services_filter)
		VALUES ($1, $2, $3)
		ON CONFLICT (LOWER(email)) WHERE unsubscribed_at IS NULL
		DO UPDATE SET services_filter = EXCLUDED.services_filter
		RETURNING id, email, verified, verify_token, services_filter, created_at, verified_at, unsubscribed_at
	`, email, verifyToken, in.ServicesFilter)
	if err := row.Scan(&s.ID, &s.Email, &s.Verified, &s.VerifyToken, &s.ServicesFilter, &s.CreatedAt, &s.VerifiedAt, &s.UnsubscribedAt); err != nil {
		return nil, fmt.Errorf("upsert subscription: %w", err)
	}
	return &s, nil
}

func (p *Postgres) RecordSample(ctx context.Context, sample UptimeSample) error {
	if sample.Service == "" || sample.Day == "" {
		return fmt.Errorf("incidents: service + day required")
	}
	_, err := p.Pool.Exec(ctx, `
		INSERT INTO uptime_samples (service, day, state, sli_pct)
		VALUES ($1, $2::date, $3, $4)
		ON CONFLICT (service, day) DO UPDATE
		SET state = EXCLUDED.state, sli_pct = EXCLUDED.sli_pct
	`, sample.Service, sample.Day, sample.State, sample.SLIPct)
	if err != nil {
		return fmt.Errorf("record sample: %w", err)
	}
	return nil
}

func (p *Postgres) UptimeForService(ctx context.Context, service string, days int) ([]UptimeSample, error) {
	if days <= 0 {
		days = 90
	}
	if days > 365 {
		days = 365
	}
	rows, err := p.Pool.Query(ctx, `
		WITH all_days AS (
			SELECT generate_series(
				(now() AT TIME ZONE 'UTC')::date - ($2::int - 1),
				(now() AT TIME ZONE 'UTC')::date,
				'1 day'::interval
			)::date AS day
		)
		SELECT $1::text AS service,
		       to_char(d.day, 'YYYY-MM-DD') AS day,
		       COALESCE(s.state, '') AS state,
		       COALESCE(s.sli_pct, 0) AS sli_pct
		FROM all_days d
		LEFT JOIN uptime_samples s ON s.service = $1 AND s.day = d.day
		ORDER BY d.day ASC
	`, service, days)
	if err != nil {
		return nil, fmt.Errorf("uptime query: %w", err)
	}
	defer rows.Close()
	var out []UptimeSample
	for rows.Next() {
		var s UptimeSample
		if err := rows.Scan(&s.Service, &s.Day, &s.State, &s.SLIPct); err != nil {
			return nil, fmt.Errorf("scan uptime: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
