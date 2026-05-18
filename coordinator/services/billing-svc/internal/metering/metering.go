// Package metering consumes per-workload completion events from the
// `BILLING` NATS JetStream stream and persists them to the usage_event
// table. A scheduled job rolls today's events into usage_aggregate_daily
// at 00:05 UTC.
//
// The event schema is intentionally minimal:
//
//	type MeteringEvent struct {
//	    WorkloadID    string  // UUID — dedupe key
//	    WorkspaceID   string  // UUID — customer side
//	    ProviderID    string  // UUID — provider side (optional for
//	                          //   workloads that don't have a single
//	                          //   provider, e.g. bandwidth pools)
//	    WorkloadType  string  // DOCKER | GPU | IOS_BUILD | BANDWIDTH
//	    Quantity      int64   // bytes or seconds
//	    CostCents     int64   // computed at submission time
//	    Currency      string  // ISO 4217 — defaults to USD
//	    RecordedAt    string  // RFC3339
//	}
//
// At-least-once delivery is expected; the (workload_id) UNIQUE constraint
// in usage_event dedupes on the database side.
package metering

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// StreamName is the JetStream stream containing metering events.
const StreamName = "BILLING"

// SubjectMetering is the wildcard subject pattern: BILLING.<workload_type>.
const SubjectMetering = "BILLING.metering.>"

// ConsumerName is the durable consumer used by billing-svc.
const ConsumerName = "billing-svc-metering"

// Event is the on-wire JSON envelope.
type Event struct {
	WorkloadID   string `json:"workload_id"`
	WorkspaceID  string `json:"workspace_id"`
	ProviderID   string `json:"provider_id,omitempty"`
	WorkloadType string `json:"workload_type"`
	Quantity     int64  `json:"quantity"`
	CostCents    int64  `json:"cost_cents"`
	Currency     string `json:"currency,omitempty"`
	RecordedAt   string `json:"recorded_at"`
}

// ToStoreEvent converts the wire envelope to a store.UsageEvent. Returns
// an error if the required UUID fields fail to parse.
func (e *Event) ToStoreEvent() (store.UsageEvent, error) {
	if e == nil {
		return store.UsageEvent{}, errors.New("nil event")
	}
	workloadID, err := uuid.Parse(e.WorkloadID)
	if err != nil {
		return store.UsageEvent{}, fmt.Errorf("workload_id: %w", err)
	}
	workspaceID, err := uuid.Parse(e.WorkspaceID)
	if err != nil {
		return store.UsageEvent{}, fmt.Errorf("workspace_id: %w", err)
	}
	var providerID *uuid.UUID
	if e.ProviderID != "" {
		pid, err := uuid.Parse(e.ProviderID)
		if err != nil {
			return store.UsageEvent{}, fmt.Errorf("provider_id: %w", err)
		}
		providerID = &pid
	}
	if e.WorkloadType == "" {
		return store.UsageEvent{}, errors.New("workload_type empty")
	}
	if e.Currency == "" {
		e.Currency = "USD"
	}
	t, err := time.Parse(time.RFC3339, e.RecordedAt)
	if err != nil {
		return store.UsageEvent{}, fmt.Errorf("recorded_at: %w", err)
	}
	return store.UsageEvent{
		WorkloadID:   workloadID,
		WorkspaceID:  workspaceID,
		ProviderID:   providerID,
		WorkloadType: e.WorkloadType,
		Quantity:     e.Quantity,
		CostCents:    e.CostCents,
		Currency:     e.Currency,
		RecordedAt:   t,
	}, nil
}

// Consumer wraps the JetStream consumer + store mutator.
type Consumer struct {
	nc     *nats.Conn
	js     jetstream.JetStream
	cons   jetstream.Consumer
	store  *store.Store
	logger *slog.Logger
}

// NewConsumer connects to NATS, ensures the BILLING stream + durable
// consumer exists, and returns a Consumer ready for Run().
func NewConsumer(ctx context.Context, url string, st *store.Store, logger *slog.Logger) (*Consumer, error) {
	if logger == nil {
		logger = slog.Default()
	}
	nc, err := nats.Connect(url)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("jetstream new: %w", err)
	}
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{SubjectMetering},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    14 * 24 * time.Hour,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("create stream: %w", err)
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       ConsumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		FilterSubject: SubjectMetering,
	})
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("create consumer: %w", err)
	}
	return &Consumer{nc: nc, js: js, cons: cons, store: st, logger: logger}, nil
}

// Run blocks on the consumer pull loop. Returns when ctx is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	iter, err := c.cons.Messages()
	if err != nil {
		return err
	}
	defer iter.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		msg, err := iter.Next()
		if err != nil {
			if errors.Is(err, jetstream.ErrMsgIteratorClosed) {
				return nil
			}
			return err
		}
		c.handle(ctx, msg)
	}
}

// HandleEvent persists a single event. Exported so unit tests can call
// it without spinning up a real NATS stream.
func (c *Consumer) HandleEvent(ctx context.Context, e *Event) error {
	row, err := e.ToStoreEvent()
	if err != nil {
		return err
	}
	return c.store.RecordUsageEvent(ctx, row)
}

func (c *Consumer) handle(ctx context.Context, msg jetstream.Msg) {
	var e Event
	if err := json.Unmarshal(msg.Data(), &e); err != nil {
		c.logger.Warn("metering: invalid JSON, acking and dropping",
			slog.String("error", err.Error()))
		_ = msg.Ack()
		return
	}
	if err := c.HandleEvent(ctx, &e); err != nil {
		c.logger.Error("metering: persist failed, will redeliver",
			slog.String("workload_id", e.WorkloadID),
			slog.String("error", err.Error()))
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}

// Close releases the NATS connection.
func (c *Consumer) Close() {
	if c.nc != nil {
		c.nc.Drain() //nolint:errcheck
	}
}
