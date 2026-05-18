// Package metering emits per-build billable-time events.
//
// Each finished build produces exactly one event capturing wall-clock
// minutes between start and finish. In production the event is published
// to a NATS JetStream subject ("iogrid.metering.build.v1") that billing-svc
// consumes. The interface here lets us test without standing up NATS.
//
// We deliberately key the event by build_id + attempt_id so retries
// (workloads-svc fails over to a different provider) bill independently —
// the customer pays for each provider's wall-clock; failures don't bill.
package metering

import (
	"context"
	"sync"
	"time"
)

// Event is the wire shape published to NATS.
type Event struct {
	BuildID         string    `json:"build_id"`
	WorkspaceID     string    `json:"workspace_id"`
	AttemptID       string    `json:"attempt_id,omitempty"`
	TerminalStatus  string    `json:"terminal_status"`
	StartedAt       time.Time `json:"started_at"`
	FinishedAt      time.Time `json:"finished_at"`
	BillableMinutes int64     `json:"billable_minutes"`
	Plan            string    `json:"plan,omitempty"`
}

// Emitter publishes billing events.
type Emitter interface {
	// Emit records a single billable event. SHOULD be idempotent on
	// (BuildID, AttemptID) — the dispatcher may resend on reconnect.
	Emit(ctx context.Context, ev Event) error
}

// --- InMemory implementation ------------------------------------------------

// InMemory captures every emitted event in a slice. Used for tests and as
// the default when no NATS connection is wired.
type InMemory struct {
	mu     sync.Mutex
	events []Event
}

// NewInMemory builds an empty emitter.
func NewInMemory() *InMemory {
	return &InMemory{}
}

// Emit implements Emitter.
func (e *InMemory) Emit(_ context.Context, ev Event) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
	return nil
}

// Events returns a defensive copy of every event emitted so far.
func (e *InMemory) Events() []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Event, len(e.events))
	copy(out, e.events)
	return out
}
