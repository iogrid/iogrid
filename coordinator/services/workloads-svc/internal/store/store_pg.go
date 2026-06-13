package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgStore is the Postgres-backed Store. Unlike memStore (process-local, lost on
// restart AND not shared across replicas), pgStore persists workloads +
// assignments in the shared CNPG `workloads` database so ANY replica can
// resolve GetWorkload / GetAssignment.
//
// This is the #771 fix: the poll-dispatch path (#705) is split-brain across
// replicas with the in-memory store — a build's terminal-status POST can land
// on a different replica than the one that created the assignment, 404ing the
// GetAssignment lookup and skipping the build-gateway ForwardStatus (→ build
// stuck "running", no metering / $GRID settle). With a shared Postgres store
// the terminal POST resolves the assignment on any replica and the build
// settles.
//
// Workload-event SUBSCRIPTIONS (SSE live tail) stay process-local: they are a
// best-effort within-replica convenience, not part of the assignment→settle
// durability contract. A subscriber only sees events appended by the replica
// it is connected to — same behaviour the dispatch-stream path already has.
type pgStore struct {
	pool *pgxpool.Pool

	subsMu sync.Mutex
	subs   map[string][]chan Event
}

// NewPostgres builds a Postgres-backed Store over the given pool.
func NewPostgres(pool *pgxpool.Pool) Store {
	return &pgStore{
		pool: pool,
		subs: make(map[string][]chan Event),
	}
}

// --- workloads ---------------------------------------------------------------

func (p *pgStore) CreateWorkload(ctx context.Context, w *Workload) error {
	if w.ID == "" {
		w.ID = uuid.NewString()
	}
	if w.SubmittedAt.IsZero() {
		w.SubmittedAt = time.Now().UTC()
	}
	if w.Status == "" {
		w.Status = StatusQueued
	}
	if w.Priority == "" {
		w.Priority = "normal"
	}

	labels, err := marshalJSON(w.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	bw, err := marshalSpec(w.Bandwidth)
	if err != nil {
		return fmt.Errorf("marshal bandwidth spec: %w", err)
	}
	dk, err := marshalSpec(w.Docker)
	if err != nil {
		return fmt.Errorf("marshal docker spec: %w", err)
	}
	gp, err := marshalSpec(w.GPU)
	if err != nil {
		return fmt.Errorf("marshal gpu spec: %w", err)
	}
	ios, err := marshalSpec(w.IOSBuild)
	if err != nil {
		return fmt.Errorf("marshal ios_build spec: %w", err)
	}
	res, err := marshalSpec(w.Result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}

	const q = `
		INSERT INTO workloads (
			id, workspace_id, submitted_by_user_id, type, priority, status,
			submitted_at, started_at, finished_at, labels,
			bandwidth_spec, docker_spec, gpu_spec, ios_build_spec, result
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14, $15
		)
		ON CONFLICT (id) DO NOTHING
	`
	_, err = p.pool.Exec(ctx, q,
		w.ID, w.WorkspaceID, w.SubmittedByUserID, w.Type, w.Priority, string(w.Status),
		w.SubmittedAt, nullTime(w.StartedAt), nullTime(w.FinishedAt), labels,
		bw, dk, gp, ios, res,
	)
	if err != nil {
		return fmt.Errorf("insert workload: %w", err)
	}
	return nil
}

const workloadCols = `
	id, workspace_id, submitted_by_user_id, type, priority, status,
	submitted_at, started_at, finished_at, labels,
	bandwidth_spec, docker_spec, gpu_spec, ios_build_spec, result`

func (p *pgStore) GetWorkload(ctx context.Context, id string) (*Workload, error) {
	wid, err := uuid.Parse(id)
	if err != nil {
		// Non-UUID id can never be in the table → mirror memStore's
		// not-found rather than surfacing a SQL error.
		return nil, ErrNotFound
	}
	row := p.pool.QueryRow(ctx, `SELECT `+workloadCols+` FROM workloads WHERE id = $1`, wid)
	return scanWorkload(row)
}

func (p *pgStore) ListWorkloads(ctx context.Context, opts ListOptions) ([]*Workload, string, error) {
	// Build a parameterised filter mirroring memStore's predicates.
	args := []any{}
	where := "WHERE 1=1"
	add := func(clause string, v any) {
		args = append(args, v)
		where += fmt.Sprintf(" AND %s $%d", clause, len(args))
	}
	if opts.WorkspaceID != "" {
		add("workspace_id =", opts.WorkspaceID)
	}
	if opts.Type != "" {
		add("type =", opts.Type)
	}
	if opts.Status != "" {
		add("status =", string(opts.Status))
	}
	if !opts.From.IsZero() {
		add("submitted_at >=", opts.From)
	}
	if !opts.To.IsZero() {
		add("submitted_at <", opts.To)
	}

	size := opts.PageSize
	if size <= 0 || size > 200 {
		size = 50
	}
	// Keyset pagination on id (memStore sorts by submitted_at but pages by
	// id > PageToken; we page by id for a stable cursor and fetch size+1 to
	// compute the next token).
	if opts.PageToken != "" {
		add("id >", opts.PageToken)
	}
	args = append(args, size+1)
	q := `SELECT ` + workloadCols + ` FROM workloads ` + where +
		fmt.Sprintf(" ORDER BY id ASC LIMIT $%d", len(args))

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query workloads: %w", err)
	}
	defer rows.Close()

	out := []*Workload{}
	for rows.Next() {
		w, err := scanWorkload(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, w)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	next := ""
	if len(out) > size {
		next = out[size-1].ID
		out = out[:size]
	}
	return out, next, nil
}

func (p *pgStore) UpdateWorkloadStatus(ctx context.Context, id string, s Status, note string) error {
	wid, err := uuid.Parse(id)
	if err != nil {
		return ErrNotFound
	}
	// COALESCE the timestamp columns so we only stamp started_at/finished_at
	// on the FIRST transition into running/terminal — same as memStore's
	// IsZero() guards.
	var startedAt, finishedAt any
	switch s {
	case StatusRunning:
		startedAt = time.Now().UTC()
	case StatusSucceeded, StatusFailed, StatusTimedOut, StatusCancelled, StatusRejected:
		finishedAt = time.Now().UTC()
	}
	const q = `
		UPDATE workloads
		SET status = $1,
		    started_at  = COALESCE(started_at,  $2::timestamptz),
		    finished_at = COALESCE(finished_at, $3::timestamptz)
		WHERE id = $4
	`
	tag, err := p.pool.Exec(ctx, q, string(s), startedAt, finishedAt, wid)
	if err != nil {
		return fmt.Errorf("update workload status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return p.AppendEvent(ctx, Event{WorkloadID: id, NewStatus: string(s), Note: note})
}

func (p *pgStore) SetWorkloadResult(ctx context.Context, id string, r *Result) error {
	wid, err := uuid.Parse(id)
	if err != nil {
		return ErrNotFound
	}
	if r != nil && r.CompletedAt.IsZero() {
		r.CompletedAt = time.Now().UTC()
	}
	blob, err := marshalSpec(r)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	tag, err := p.pool.Exec(ctx, `UPDATE workloads SET result = $1 WHERE id = $2`, blob, wid)
	if err != nil {
		return fmt.Errorf("set workload result: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *pgStore) CancelWorkload(ctx context.Context, id, reason string) error {
	return p.UpdateWorkloadStatus(ctx, id, StatusCancelled, reason)
}

// --- assignments -------------------------------------------------------------

func (p *pgStore) CreateAssignment(ctx context.Context, a *Assignment) error {
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.LatestStatus == "" {
		a.LatestStatus = StatusDispatched
	}
	const q = `
		INSERT INTO workload_assignments (
			id, workload_id, provider_id, created_at, deadline,
			accepted, latest_status, rejection_reason
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING
	`
	_, err := p.pool.Exec(ctx, q,
		a.ID, a.WorkloadID, a.ProviderID, a.CreatedAt, nullTime(a.Deadline),
		a.Accepted, string(a.LatestStatus), a.RejectionReason,
	)
	if err != nil {
		return fmt.Errorf("insert assignment: %w", err)
	}
	return nil
}

const assignmentCols = `
	id, workload_id, provider_id, created_at, deadline,
	accepted, latest_status, rejection_reason`

func (p *pgStore) GetAssignment(ctx context.Context, id string) (*Assignment, error) {
	aid, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrNotFound
	}
	row := p.pool.QueryRow(ctx, `SELECT `+assignmentCols+` FROM workload_assignments WHERE id = $1`, aid)
	return scanAssignment(row)
}

func (p *pgStore) UpdateAssignment(ctx context.Context, a *Assignment) error {
	aid, err := uuid.Parse(a.ID)
	if err != nil {
		return ErrNotFound
	}
	const q = `
		UPDATE workload_assignments
		SET workload_id = $2, provider_id = $3, deadline = $4,
		    accepted = $5, latest_status = $6, rejection_reason = $7
		WHERE id = $1
	`
	tag, err := p.pool.Exec(ctx, q,
		aid, a.WorkloadID, a.ProviderID, nullTime(a.Deadline),
		a.Accepted, string(a.LatestStatus), a.RejectionReason,
	)
	if err != nil {
		return fmt.Errorf("update assignment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *pgStore) AssignmentsForWorkload(ctx context.Context, workloadID string) ([]*Assignment, error) {
	wid, err := uuid.Parse(workloadID)
	if err != nil {
		return []*Assignment{}, nil
	}
	rows, err := p.pool.Query(ctx,
		`SELECT `+assignmentCols+` FROM workload_assignments WHERE workload_id = $1 ORDER BY created_at ASC`, wid)
	if err != nil {
		return nil, fmt.Errorf("query assignments: %w", err)
	}
	defer rows.Close()
	return scanAssignments(rows)
}

func (p *pgStore) ListPendingAssignments(ctx context.Context, providerID string) ([]*Assignment, error) {
	// Dispatched-but-not-yet-running: the daemon hasn't reported back. Once it
	// polls + starts the work it reports RUNNING (latest_status off
	// 'dispatched'), dropping the row from this list. Mirrors memStore.
	rows, err := p.pool.Query(ctx,
		`SELECT `+assignmentCols+`
		   FROM workload_assignments
		  WHERE provider_id = $1 AND latest_status = $2
		  ORDER BY created_at ASC`,
		providerID, string(StatusDispatched))
	if err != nil {
		return nil, fmt.Errorf("query pending assignments: %w", err)
	}
	defer rows.Close()
	return scanAssignments(rows)
}

// --- events ------------------------------------------------------------------

func (p *pgStore) AppendEvent(ctx context.Context, e Event) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	// Events are advisory timeline rows; the SSE tail reads from the
	// in-process subscriber fanout (per-replica, best-effort — see the type
	// doc). We do not persist them: the durable record the settle path needs
	// is the workload status + result + assignment, all already persisted
	// above. Keeping the fanout matches memStore's observable behaviour for a
	// subscriber attached to THIS replica.
	p.subsMu.Lock()
	subs := append([]chan Event(nil), p.subs[e.WorkloadID]...)
	p.subsMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
	return nil
}

func (p *pgStore) SubscribeWorkloadEvents(workloadID string) (<-chan Event, func()) {
	ch := make(chan Event, 32)
	p.subsMu.Lock()
	p.subs[workloadID] = append(p.subs[workloadID], ch)
	p.subsMu.Unlock()
	cancel := func() {
		p.subsMu.Lock()
		defer p.subsMu.Unlock()
		remaining := p.subs[workloadID][:0]
		for _, c := range p.subs[workloadID] {
			if c != ch {
				remaining = append(remaining, c)
			}
		}
		p.subs[workloadID] = remaining
		close(ch)
	}
	return ch, cancel
}

// --- scan helpers ------------------------------------------------------------

type scannable interface {
	Scan(dest ...any) error
}

func scanWorkload(row scannable) (*Workload, error) {
	w := &Workload{}
	var (
		startedAt, finishedAt           *time.Time
		labels                          []byte
		bwBlob, dkBlob, gpBlob, iosBlob []byte
		resBlob                         []byte
		statusStr                       string
	)
	err := row.Scan(
		&w.ID, &w.WorkspaceID, &w.SubmittedByUserID, &w.Type, &w.Priority, &statusStr,
		&w.SubmittedAt, &startedAt, &finishedAt, &labels,
		&bwBlob, &dkBlob, &gpBlob, &iosBlob, &resBlob,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan workload: %w", err)
	}
	w.Status = Status(statusStr)
	if startedAt != nil {
		w.StartedAt = *startedAt
	}
	if finishedAt != nil {
		w.FinishedAt = *finishedAt
	}
	if err := unmarshalJSON(labels, &w.Labels); err != nil {
		return nil, fmt.Errorf("unmarshal labels: %w", err)
	}
	if err := unmarshalInto(bwBlob, &w.Bandwidth); err != nil {
		return nil, fmt.Errorf("unmarshal bandwidth spec: %w", err)
	}
	if err := unmarshalInto(dkBlob, &w.Docker); err != nil {
		return nil, fmt.Errorf("unmarshal docker spec: %w", err)
	}
	if err := unmarshalInto(gpBlob, &w.GPU); err != nil {
		return nil, fmt.Errorf("unmarshal gpu spec: %w", err)
	}
	if err := unmarshalInto(iosBlob, &w.IOSBuild); err != nil {
		return nil, fmt.Errorf("unmarshal ios_build spec: %w", err)
	}
	if err := unmarshalInto(resBlob, &w.Result); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}
	return w, nil
}

func scanAssignment(row scannable) (*Assignment, error) {
	a := &Assignment{}
	var (
		deadline  *time.Time
		statusStr string
	)
	err := row.Scan(
		&a.ID, &a.WorkloadID, &a.ProviderID, &a.CreatedAt, &deadline,
		&a.Accepted, &statusStr, &a.RejectionReason,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scan assignment: %w", err)
	}
	a.LatestStatus = Status(statusStr)
	if deadline != nil {
		a.Deadline = *deadline
	}
	return a, nil
}

// rowsScanner is the subset of pgx.Rows the multi-row scanners use.
type rowsScanner interface {
	scannable
	Next() bool
	Err() error
}

func scanAssignments(rows rowsScanner) ([]*Assignment, error) {
	out := []*Assignment{}
	for rows.Next() {
		a, err := scanAssignment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// --- json helpers ------------------------------------------------------------

// marshalSpec marshals a possibly-nil *Spec / *Result pointer to a JSONB value,
// returning a nil interface (→ SQL NULL) when the pointer is nil so the
// type-discriminated columns stay NULL for the specs that don't apply.
func marshalSpec(v any) (any, error) {
	if isNilPtr(v) {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// marshalJSON marshals a value to bytes (used for labels — always written, as
// the column is NOT NULL DEFAULT '{}').
func marshalJSON(v any) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(v)
}

func unmarshalJSON(b []byte, dst any) error {
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, dst)
}

// unmarshalInto unmarshals a JSONB blob into **T, leaving the pointer nil when
// the column was SQL NULL (len 0) — preserving the "exactly one spec non-nil"
// invariant on read-back.
func unmarshalInto[T any](b []byte, dst **T) error {
	if len(b) == 0 {
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*dst = &v
	return nil
}

// isNilPtr reports whether v is a typed-nil pointer (so a nil *Spec passed
// through the `any` interface still maps to SQL NULL).
func isNilPtr(v any) bool {
	switch p := v.(type) {
	case nil:
		return true
	case *BandwidthSpec:
		return p == nil
	case *DockerSpec:
		return p == nil
	case *GPUSpec:
		return p == nil
	case *IOSBuildSpec:
		return p == nil
	case *Result:
		return p == nil
	default:
		return false
	}
}

// nullTime maps a zero time.Time to nil (SQL NULL) so the nullable timestamp
// columns stay NULL rather than storing 0001-01-01.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
