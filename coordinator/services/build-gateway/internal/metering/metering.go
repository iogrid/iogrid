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
	"encoding/json"
	"sync"
	"time"
)

// Event is the build-domain metering record. The NATS emitter maps it onto
// the billing-svc wire envelope (BillingWireEvent) at publish time, so this
// stays readable in build terms (build_id, billable_minutes) while the wire
// carries what billing-svc's consumer expects (workload_id, cost_cents, …).
type Event struct {
	BuildID         string    `json:"build_id"`
	WorkspaceID     string    `json:"workspace_id"`
	AttemptID       string    `json:"attempt_id,omitempty"`
	// ProviderID is the daemon that ran the build — the earnings the metered
	// minutes credit against (#744). Empty → billing-svc writes a usage_event
	// with no provider, so it never shows on any /provide earnings card.
	ProviderID      string    `json:"provider_id,omitempty"`
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

// --- NATS / billing-svc wire ------------------------------------------------

// Subject is the BILLING JetStream subject billing-svc subscribes to via
// `BILLING.metering.>` (metering.SubjectMetering). Keep the prefix in sync.
const Subject = "BILLING.metering.ios_build"

// WireWorkloadType is the workload_type slug billing-svc stores on the
// usage_event row and that providers-svc buckets earnings by.
const WireWorkloadType = "IOS_BUILD"

// ProviderShareCentsPerMinute is the provider's cut of one billed build-minute,
// in cents. Mirrors vpn-svc's provider-share rating (the earnings card sums the
// PROVIDER's share, not gross). Devnet placeholder until the shared price book
// lands; iogrid targets ~50% of GitHub Actions macOS (~8¢/min) → ~4¢/min gross,
// 85% provider share ≈ 3¢/min.
const ProviderShareCentsPerMinute int64 = 3

// BillingWireEvent is the exact JSON envelope billing-svc's metering consumer
// decodes (internal/metering.Event in billing-svc). Field tags MUST match it
// verbatim — workload_id, not build_id.
type BillingWireEvent struct {
	WorkloadID   string `json:"workload_id"`
	WorkspaceID  string `json:"workspace_id"`
	ProviderID   string `json:"provider_id,omitempty"`
	WorkloadType string `json:"workload_type"`
	Quantity     int64  `json:"quantity"`
	CostCents    int64  `json:"cost_cents"`
	Currency     string `json:"currency,omitempty"`
	RecordedAt   string `json:"recorded_at"`
}

// toWire projects a build-domain Event onto the billing wire envelope. The
// build id doubles as the workload id (billing dedupes on workload_id), the
// quantity is billed minutes, and the cost is the provider's share of those
// minutes — the number /provide earnings sums.
func toWire(ev Event) BillingWireEvent {
	return BillingWireEvent{
		WorkloadID:   ev.BuildID,
		WorkspaceID:  ev.WorkspaceID,
		ProviderID:   ev.ProviderID,
		WorkloadType: WireWorkloadType,
		Quantity:     ev.BillableMinutes,
		CostCents:    ev.BillableMinutes * ProviderShareCentsPerMinute,
		Currency:     "USD",
		RecordedAt:   ev.FinishedAt.UTC().Format(time.RFC3339),
	}
}

// Publisher abstracts the NATS publish so the emitter is unit-testable.
type Publisher interface {
	Publish(subject string, data []byte) error
}

// NATSEmitter is the production Emitter: it marshals the billing wire envelope
// and publishes it to the BILLING stream so billing-svc writes a usage_event
// (provider-attributed) that surfaces on the /provide earnings card (#744).
type NATSEmitter struct {
	Pub Publisher
}

// Emit implements Emitter.
func (n *NATSEmitter) Emit(_ context.Context, ev Event) error {
	if n == nil || n.Pub == nil {
		return nil
	}
	body, err := json.Marshal(toWire(ev))
	if err != nil {
		return err
	}
	return n.Pub.Publish(Subject, body)
}
