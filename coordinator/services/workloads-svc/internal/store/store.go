// Package store is the workloads-svc persistence layer. The default
// implementation is in-memory; a pgx-backed implementation lives in
// store_pg.go behind the `postgres` build tag.
//
// All public types here are server-side projections — they intentionally
// do NOT embed proto messages so the layer is easy to mock and the
// scheduler can reason about plain Go structs.
package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound      = errors.New("store: not found")
	ErrInvalidState  = errors.New("store: invalid state transition")
)

// WorkloadType — slug form (matches docs/TECH.md).
const (
	TypeBandwidth = "bandwidth"
	TypeDocker    = "docker"
	TypeGPU       = "gpu"
	TypeIOSBuild  = "ios_build"
)

// Status mirrors workloads/v1.WorkloadStatus.
type Status string

const (
	StatusQueued     Status = "queued"
	StatusDispatched Status = "dispatched"
	StatusRunning    Status = "running"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusTimedOut   Status = "timed_out"
	StatusCancelled  Status = "cancelled"
	StatusRejected   Status = "rejected"
)

// Workload is the persisted projection. Spec is type-discriminated by Type
// + the corresponding *Spec fields — exactly one is non-nil per row.
type Workload struct {
	ID                 string
	WorkspaceID        string
	SubmittedByUserID  string
	Type               string // slug
	Priority           string // "low"/"normal"/"high"
	Status             Status
	SubmittedAt        time.Time
	StartedAt          time.Time
	FinishedAt         time.Time
	Labels             map[string]string

	Bandwidth *BandwidthSpec
	Docker    *DockerSpec
	GPU       *GPUSpec
	IOSBuild  *IOSBuildSpec

	Result *Result
}

type BandwidthSpec struct {
	TargetURL        string
	Method           string
	SessionID        string
	PreferredRegion  string
	Category         string
	MaxSpendCurrency string
	MaxSpendMicros   int64
}

type DockerSpec struct {
	Image          string
	Command        []string
	Env            map[string]string
	Timeout        time.Duration
	MinCPUCores    uint32
	MinMemoryMiB   uint64
	MinGPUMemoryMiB uint64
}

type GPUSpec struct {
	Image          string
	Command        []string
	Env            map[string]string
	Timeout        time.Duration
	MinVRAMMiB     uint64
	AllowedVendors []string
}

type IOSBuildSpec struct {
	SourceTarballS3Key string
	TartImage          string
	BuildCommands      []string
	ArtifactBucket     string
	ArtifactPrefix     string

	// git-based dispatch (preferred; drives the daemon Tart driver). A
	// submission sets EITHER RepoURL or SourceTarballS3Key.
	RepoURL           string
	GitRef            string
	BuildCommand      string
	UploadURL         string
	ArtifactGuestPath string
	CPU               uint32
	MemoryMiB         uint32
	BootTimeoutSecs   uint32
}

type Result struct {
	TerminalStatus string // succeeded|failed|timed_out|cancelled
	ExitCode       int32
	LogsS3Key      string
	BytesIn        uint64
	BytesOut       uint64
	ArtifactS3Keys []string
	Currency       string
	CostMicros     int64
	CompletedAt    time.Time
}

// Assignment is one dispatch attempt against a specific provider.
type Assignment struct {
	ID         string
	WorkloadID string
	ProviderID string
	CreatedAt  time.Time
	Deadline   time.Time
	Accepted   bool
	LatestStatus Status
	RejectionReason string
}

// Event captures every workload-status transition; tail of the list is
// the canonical timeline for StreamWorkloadEvents.
type Event struct {
	WorkloadID string
	NewStatus  string
	OccurredAt time.Time
	Note       string
}

// ListOptions parameterises ListWorkloads.
type ListOptions struct {
	WorkspaceID  string
	Type         string
	Status       Status
	From         time.Time
	To           time.Time
	PageSize     int
	PageToken    string
}

// Store is the workloads-svc persistence interface.
type Store interface {
	CreateWorkload(ctx context.Context, w *Workload) error
	GetWorkload(ctx context.Context, id string) (*Workload, error)
	ListWorkloads(ctx context.Context, opts ListOptions) ([]*Workload, string, error)
	UpdateWorkloadStatus(ctx context.Context, id string, s Status, note string) error
	SetWorkloadResult(ctx context.Context, id string, r *Result) error
	CancelWorkload(ctx context.Context, id, reason string) error

	CreateAssignment(ctx context.Context, a *Assignment) error
	GetAssignment(ctx context.Context, id string) (*Assignment, error)
	UpdateAssignment(ctx context.Context, a *Assignment) error
	AssignmentsForWorkload(ctx context.Context, workloadID string) ([]*Assignment, error)
	// ListPendingAssignments returns assignments for a provider that have
	// been dispatched but not yet picked up (LatestStatus==dispatched) —
	// the poll-based delivery path (#705) for daemons whose server→client
	// push half is dropped by the edge. Mirrors the VPN binder's
	// /assigned-sessions.
	ListPendingAssignments(ctx context.Context, providerID string) ([]*Assignment, error)

	AppendEvent(ctx context.Context, e Event) error
	SubscribeWorkloadEvents(workloadID string) (<-chan Event, func())
}

// --- in-memory implementation -----------------------------------------------

type memStore struct {
	mu          sync.RWMutex
	workloads   map[string]*Workload
	assignments map[string]*Assignment
	events      []Event

	subsMu sync.Mutex
	subs   map[string][]chan Event
}

func NewInMemory() Store {
	return &memStore{
		workloads:   make(map[string]*Workload),
		assignments: make(map[string]*Assignment),
		subs:        make(map[string][]chan Event),
	}
}

func (m *memStore) CreateWorkload(_ context.Context, w *Workload) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if w.ID == "" {
		w.ID = uuid.NewString()
	}
	if w.SubmittedAt.IsZero() {
		w.SubmittedAt = time.Now().UTC()
	}
	if w.Status == "" {
		w.Status = StatusQueued
	}
	cp := *w
	m.workloads[w.ID] = &cp
	return nil
}

func (m *memStore) GetWorkload(_ context.Context, id string) (*Workload, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.workloads[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *w
	return &cp, nil
}

func (m *memStore) ListWorkloads(_ context.Context, opts ListOptions) ([]*Workload, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Workload, 0, len(m.workloads))
	for _, w := range m.workloads {
		if opts.WorkspaceID != "" && w.WorkspaceID != opts.WorkspaceID {
			continue
		}
		if opts.Type != "" && w.Type != opts.Type {
			continue
		}
		if opts.Status != "" && w.Status != opts.Status {
			continue
		}
		if !opts.From.IsZero() && w.SubmittedAt.Before(opts.From) {
			continue
		}
		if !opts.To.IsZero() && !w.SubmittedAt.Before(opts.To) {
			continue
		}
		cp := *w
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SubmittedAt.Before(out[j].SubmittedAt) })

	size := opts.PageSize
	if size <= 0 || size > 200 {
		size = 50
	}
	start := 0
	if opts.PageToken != "" {
		for i, w := range out {
			if w.ID > opts.PageToken {
				start = i
				break
			}
		}
	}
	end := start + size
	next := ""
	if end < len(out) {
		next = out[end-1].ID
	} else {
		end = len(out)
	}
	return out[start:end], next, nil
}

func (m *memStore) UpdateWorkloadStatus(ctx context.Context, id string, s Status, note string) error {
	m.mu.Lock()
	w, ok := m.workloads[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	w.Status = s
	switch s {
	case StatusRunning:
		if w.StartedAt.IsZero() {
			w.StartedAt = time.Now().UTC()
		}
	case StatusSucceeded, StatusFailed, StatusTimedOut, StatusCancelled, StatusRejected:
		if w.FinishedAt.IsZero() {
			w.FinishedAt = time.Now().UTC()
		}
	}
	m.mu.Unlock()
	return m.AppendEvent(ctx, Event{WorkloadID: id, NewStatus: string(s), Note: note})
}

func (m *memStore) SetWorkloadResult(_ context.Context, id string, r *Result) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.workloads[id]
	if !ok {
		return ErrNotFound
	}
	w.Result = r
	if r != nil && r.CompletedAt.IsZero() {
		r.CompletedAt = time.Now().UTC()
	}
	return nil
}

func (m *memStore) CancelWorkload(ctx context.Context, id, reason string) error {
	return m.UpdateWorkloadStatus(ctx, id, StatusCancelled, reason)
}

func (m *memStore) CreateAssignment(_ context.Context, a *Assignment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	cp := *a
	m.assignments[a.ID] = &cp
	return nil
}

func (m *memStore) GetAssignment(_ context.Context, id string) (*Assignment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.assignments[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *memStore) UpdateAssignment(_ context.Context, a *Assignment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.assignments[a.ID]; !ok {
		return ErrNotFound
	}
	cp := *a
	m.assignments[a.ID] = &cp
	return nil
}

func (m *memStore) AssignmentsForWorkload(_ context.Context, workloadID string) ([]*Assignment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*Assignment{}
	for _, a := range m.assignments {
		if a.WorkloadID == workloadID {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *memStore) ListPendingAssignments(_ context.Context, providerID string) ([]*Assignment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*Assignment{}
	for _, a := range m.assignments {
		// Dispatched-but-not-yet-running: the daemon hasn't reported back
		// (no RUNNING/terminal update). Once it polls + starts the work it
		// reports RUNNING, which moves LatestStatus off "dispatched" and
		// drops the row from this list.
		if a.ProviderID == providerID && a.LatestStatus == StatusDispatched {
			cp := *a
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *memStore) AppendEvent(_ context.Context, e Event) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	m.mu.Lock()
	m.events = append(m.events, e)
	m.mu.Unlock()

	m.subsMu.Lock()
	subs := append([]chan Event(nil), m.subs[e.WorkloadID]...)
	m.subsMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
	return nil
}

func (m *memStore) SubscribeWorkloadEvents(workloadID string) (<-chan Event, func()) {
	ch := make(chan Event, 32)
	m.subsMu.Lock()
	m.subs[workloadID] = append(m.subs[workloadID], ch)
	m.subsMu.Unlock()
	cancel := func() {
		m.subsMu.Lock()
		defer m.subsMu.Unlock()
		remaining := m.subs[workloadID][:0]
		for _, c := range m.subs[workloadID] {
			if c != ch {
				remaining = append(remaining, c)
			}
		}
		m.subs[workloadID] = remaining
		close(ch)
	}
	return ch, cancel
}
