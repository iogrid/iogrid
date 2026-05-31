// Package earnings publishes per-session VPN byte usage to the BILLING
// NATS stream so billing-svc can credit residential providers.
//
// Without this loop, providers ship bandwidth for free — closes #547.
//
// Design:
//
//   - Every PollInterval (default 5 min), scan vpn_sessions for rows
//     where terminated_at IS NOT NULL AND billed_at IS NULL.
//   - For each session, compute the provider's earnings share (cents)
//     from total bytes transferred at the regulated VPN rate.
//   - Publish one envelope per session to subject
//     BILLING.metering.vpn_bytes — billing-svc's metering consumer
//     persists it as a usage_event row keyed on session_id (UNIQUE
//     dedupes at-least-once delivery).
//   - Stamp billed_at locally so we don't republish on the next tick.
//
// We intentionally bill on session terminate rather than streaming
// per-tick incremental bytes — terminated sessions have a stable byte
// total, so the workload_id (=session_id) UNIQUE constraint dedupes
// cleanly. Long-lived sessions (>24h) will see a single large credit
// when they end; if that lag turns out to matter we'll add a partial
// "checkpoint" batch separately.
package earnings

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/iogrid/iogrid/coordinator/services/vpn-svc/internal/store"
)

// Subject is the BILLING stream subject billing-svc subscribes to via
// `BILLING.metering.>`. Keep in sync with metering.SubjectMetering in
// billing-svc.
const Subject = "BILLING.metering.vpn_bytes"

// WorkloadType for the usage_event row. billing-svc treats this as an
// opaque string; the web /provide/earnings card groups by it.
const WorkloadType = "BANDWIDTH_VPN"

// DefaultPollInterval — how often the batcher scans for unbilled
// terminated sessions. 5 minutes was the cadence #547 called out;
// shorter cadences would just hammer Postgres for sessions that don't
// terminate often.
const DefaultPollInterval = 5 * time.Minute

// DefaultBatchLimit caps the per-tick scan so a backlog after a
// vpn-svc restart doesn't try to flush thousands of sessions in one
// query.
const DefaultBatchLimit = 200

// providerShareCentsPerGiB is the cents we credit the residential
// provider per GiB of customer traffic. Customer pays 40 ¢/GiB (mid of
// the published 30–60 ¢/GiB band in README.md); provider gets 70 % of
// gross = 28 ¢/GiB. The remaining 12 ¢ funds iogrid platform costs
// (control plane + free-VPN consumers cross-subsidy per
// BUSINESS-STRATEGY).
const providerShareCentsPerGiB = 28

// bytesPerGiB is 1024^3.
const bytesPerGiB = uint64(1024 * 1024 * 1024)

// Publisher abstracts the NATS publish to keep the loop testable
// without spinning up a real broker.
type Publisher interface {
	Publish(subject string, data []byte) error
}

// Config wires the loop together.
type Config struct {
	Store        store.Store
	Publisher    Publisher
	Logger       *slog.Logger
	PollInterval time.Duration
	BatchLimit   int
}

// Event is the JSON envelope billing-svc.metering.Event consumes.
// Field tags MUST match metering.Event verbatim — they're the wire
// contract.
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

// Batcher runs the periodic scan/publish/mark loop. Use Run to block.
type Batcher struct {
	cfg Config
}

// New validates the config and returns a ready-to-Run Batcher.
func New(cfg Config) (*Batcher, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("earnings: nil store")
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("earnings: nil publisher")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	if cfg.BatchLimit <= 0 {
		cfg.BatchLimit = DefaultBatchLimit
	}
	return &Batcher{cfg: cfg}, nil
}

// Run blocks on the periodic tick. Returns when ctx is cancelled.
func (b *Batcher) Run(ctx context.Context) error {
	t := time.NewTicker(b.cfg.PollInterval)
	defer t.Stop()
	// Fire one immediate tick so a fresh-deploy session that's already
	// terminated doesn't have to wait 5 min to be credited.
	if err := b.Tick(ctx); err != nil {
		b.cfg.Logger.Warn("earnings tick failed (will retry)", slog.String("err", err.Error()))
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if err := b.Tick(ctx); err != nil {
				b.cfg.Logger.Warn("earnings tick failed (will retry)", slog.String("err", err.Error()))
			}
		}
	}
}

// Tick runs one scan + publish + mark cycle. Exposed so tests + an
// admin "flush now" handler can drive it directly.
func (b *Batcher) Tick(ctx context.Context) error {
	sessions, err := b.cfg.Store.ListUnbilledTerminatedSessions(ctx, b.cfg.BatchLimit)
	if err != nil {
		return fmt.Errorf("list unbilled: %w", err)
	}
	if len(sessions) == 0 {
		return nil
	}
	var published, failed int
	for _, s := range sessions {
		evt := buildEvent(s)
		body, err := json.Marshal(evt)
		if err != nil {
			b.cfg.Logger.Warn("earnings marshal failed",
				slog.String("session_id", s.ID.String()),
				slog.String("err", err.Error()))
			failed++
			continue
		}
		if err := b.cfg.Publisher.Publish(Subject, body); err != nil {
			b.cfg.Logger.Warn("earnings publish failed (will retry next tick)",
				slog.String("session_id", s.ID.String()),
				slog.String("err", err.Error()))
			failed++
			continue
		}
		if err := b.cfg.Store.MarkSessionBilled(ctx, s.ID); err != nil {
			b.cfg.Logger.Warn("mark billed failed (event published, may double-credit)",
				slog.String("session_id", s.ID.String()),
				slog.String("err", err.Error()))
			// Don't count as failed; the NATS dedupe (workload_id UNIQUE)
			// will catch the next-tick republish.
		}
		published++
	}
	b.cfg.Logger.Info("earnings tick complete",
		slog.Int("scanned", len(sessions)),
		slog.Int("published", published),
		slog.Int("failed", failed))
	return nil
}

// buildEvent converts a terminated session into a wire event. The
// provider share is computed from the higher of bytes_in / bytes_out
// (egress is what the provider's ISP bills them for, so we credit
// against whichever direction was larger — most VPN traffic is download-
// heavy, but a customer running a server through us flips the ratio).
func buildEvent(s *store.Session) Event {
	bytes := s.BytesIn
	if s.BytesOut > bytes {
		bytes = s.BytesOut
	}
	cost := int64(bytes) * providerShareCentsPerGiB / int64(bytesPerGiB)
	if cost < 0 {
		cost = 0
	}
	terminated := time.Now().UTC()
	if s.TerminatedAt != nil {
		terminated = s.TerminatedAt.UTC()
	}
	providerID := ""
	if s.CurrentProvider != uuid.Nil {
		providerID = s.CurrentProvider.String()
	}
	return Event{
		WorkloadID:   s.ID.String(),
		WorkspaceID:  s.CustomerID.String(),
		ProviderID:   providerID,
		WorkloadType: WorkloadType,
		Quantity:     int64(bytes),
		CostCents:    cost,
		Currency:     "USD",
		RecordedAt:   terminated.Format(time.RFC3339),
	}
}

// NATSPublisher adapts *nats.Conn to the Publisher interface.
type NATSPublisher struct {
	NC *nats.Conn
}

// Publish forwards to nats.Conn.Publish + flushes so the call returns
// only after the broker has the bytes (we'd rather be slow than lose
// an event after we mark billed_at).
func (p *NATSPublisher) Publish(subject string, data []byte) error {
	if err := p.NC.Publish(subject, data); err != nil {
		return err
	}
	return p.NC.Flush()
}
