// Package store defines the persistence interface for the build-gateway and
// ships a goroutine-safe in-memory implementation.
//
// The interface is the seam we'll plug Postgres into once the schema lands
// in iogrid/iogrid-ops (per docs/TECH.md "Postgres per service"). Until
// then, the in-memory impl is what the service runs against — it survives
// process restarts only insofar as the gateway is wired in front of a
// dispatcher that re-enqueues lost jobs (workloads-svc).
package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
)

// ErrNotFound is returned when a lookup misses. The HTTP layer maps this to
// 404 — callers MUST treat it as a not-found, not a transient failure.
var ErrNotFound = errors.New("build not found")

// Store is the persistence abstraction. All methods are safe for concurrent
// use. Mirrors the narrower builds.Store interface so an *InMemory can be
// handed straight into builds.NewService.
type Store interface {
	// Create persists a brand-new build. b.SubmittedAt is filled in if zero.
	Create(ctx context.Context, b *builds.Build) error
	// Get returns the build by id, scoped to workspaceID for tenancy.
	// Returns ErrNotFound if it doesn't exist OR belongs to a different
	// workspace (we never disclose existence cross-tenant).
	Get(ctx context.Context, workspaceID, id string) (*builds.Build, error)
	// GetByIDInternal returns the build by id WITHOUT a workspace check —
	// used only by internal handlers (provider artifact upload, dispatch
	// callbacks) where the caller has already proved authority via
	// dispatch token / mTLS.
	GetByIDInternal(ctx context.Context, id string) (*builds.Build, error)
	// Update applies mutator under the store's lock. The mutator MUST NOT
	// retain references to b across the call boundary.
	Update(ctx context.Context, id string, mutator func(*builds.Build) error) (*builds.Build, error)
	// List returns builds for workspaceID matching the optional status,
	// newest-first. limit<=0 means no cap.
	List(ctx context.Context, workspaceID string, status builds.Status, limit int) ([]*builds.Build, error)
	// ListNonTerminal returns every build across all workspaces whose status is
	// not terminal — consulted by the stale-build reaper (#811).
	ListNonTerminal(ctx context.Context) ([]*builds.Build, error)
}

// InMemory is a goroutine-safe Store backed by a map. Suitable for
// development, tests, and as the runtime impl until Postgres lands.
type InMemory struct {
	mu    sync.RWMutex
	items map[string]*builds.Build
	now   func() time.Time
}

// NewInMemory builds an empty in-memory store. now is the clock source —
// pass nil for time.Now.
func NewInMemory(now func() time.Time) *InMemory {
	if now == nil {
		now = time.Now
	}
	return &InMemory{
		items: make(map[string]*builds.Build),
		now:   now,
	}
}

// Create implements Store.
func (s *InMemory) Create(_ context.Context, b *builds.Build) error {
	if b == nil || b.ID == "" {
		return errors.New("build: id required")
	}
	if b.WorkspaceID == "" {
		return errors.New("build: workspace_id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.items[b.ID]; exists {
		return errors.New("build: id collision")
	}
	if b.SubmittedAt.IsZero() {
		b.SubmittedAt = s.now()
	}
	if b.Status == "" {
		b.Status = builds.StatusQueued
	}
	// Store a defensive clone so mutations to the caller's copy don't
	// leak in.
	stored := *b
	if b.EnvVars != nil {
		stored.EnvVars = make(map[string]string, len(b.EnvVars))
		for k, v := range b.EnvVars {
			stored.EnvVars[k] = v
		}
	}
	if b.Webhook != nil {
		w := *b.Webhook
		stored.Webhook = &w
	}
	if b.Artifacts != nil {
		stored.Artifacts = append([]builds.Artifact(nil), b.Artifacts...)
	}
	s.items[b.ID] = &stored
	return nil
}

// Get implements Store.
func (s *InMemory) Get(_ context.Context, workspaceID, id string) (*builds.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.items[id]
	if !ok || b.WorkspaceID != workspaceID {
		return nil, ErrNotFound
	}
	clone := *b
	if b.Artifacts != nil {
		clone.Artifacts = append([]builds.Artifact(nil), b.Artifacts...)
	}
	return &clone, nil
}

// GetByIDInternal implements Store.
func (s *InMemory) GetByIDInternal(_ context.Context, id string) (*builds.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	clone := *b
	if b.Artifacts != nil {
		clone.Artifacts = append([]builds.Artifact(nil), b.Artifacts...)
	}
	return &clone, nil
}

// Update implements Store.
func (s *InMemory) Update(_ context.Context, id string, mutator func(*builds.Build) error) (*builds.Build, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.items[id]
	if !ok {
		return nil, ErrNotFound
	}
	if err := mutator(b); err != nil {
		return nil, err
	}
	clone := *b
	if b.Artifacts != nil {
		clone.Artifacts = append([]builds.Artifact(nil), b.Artifacts...)
	}
	return &clone, nil
}

// List implements Store.
func (s *InMemory) List(_ context.Context, workspaceID string, status builds.Status, limit int) ([]*builds.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*builds.Build, 0)
	for _, b := range s.items {
		if workspaceID != "" && b.WorkspaceID != workspaceID {
			continue
		}
		if status != "" && b.Status != status {
			continue
		}
		clone := *b
		out = append(out, &clone)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SubmittedAt.After(out[j].SubmittedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListNonTerminal implements Store — every non-terminal build across all
// workspaces, oldest-first so the reaper handles the longest-stuck rows first.
func (s *InMemory) ListNonTerminal(_ context.Context) ([]*builds.Build, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*builds.Build, 0)
	for _, b := range s.items {
		if b.Status.Terminal() {
			continue
		}
		clone := *b
		if b.Artifacts != nil {
			clone.Artifacts = append([]builds.Artifact(nil), b.Artifacts...)
		}
		out = append(out, &clone)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].SubmittedAt.Before(out[j].SubmittedAt)
	})
	return out, nil
}
