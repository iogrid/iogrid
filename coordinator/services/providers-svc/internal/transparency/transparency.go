// Package transparency consumes proxy-gateway abuse-decision audit
// events from the AUDIT JetStream and projects them into the
// providers-svc audit_events table so the per-provider transparency
// feed (StreamAuditEvents) surfaces the same EVENT_KIND_ABUSE_FLAGGED
// rows the web management plane already renders for other event kinds.
//
// Bridge subject: "iogrid.audit.proxy.abuse_flagged".
//
// Wire-up:
//
//  1. proxy-gateway emits AuditEvent{EventKind:"abuse_flagged", ...}
//     to NATS at iogrid.audit.proxy.abuse_flagged on every block.
//  2. This package subscribes to that subject with a durable consumer
//     and on each message:
//       - drops events with no provider_id (no sticky binding existed,
//         so no per-provider feed can render them; the AUDIT stream
//         still retains the legal-evidence row).
//       - maps the payload onto store.AuditEvent{Kind:
//         "EVENT_KIND_ABUSE_FLAGGED"}.
//       - calls Store.AppendAuditEvent, which both inserts into
//         audit_events AND fans out to the in-process subscriber set
//         feeding StreamAuditEvents.
//
// Fail mode: bridge errors NEVER block the proxy data plane — the
// proxy's own AUDIT stream is the source of legal-retention truth.
// The bridge's only job is the per-provider projection. On NATS
// outage we log a warning and skip; on Store insertion failure we
// log and continue (single-row gaps are recoverable from the AUDIT
// stream via the antiabuse-svc transparency report pipeline).
//
// Refs #360.
package transparency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

// AuditStream is the JetStream stream name proxy-gateway publishes to.
// MUST match coordinator/services/proxy-gateway/internal/audit.AuditStream.
const AuditStream = "AUDIT"

// AbuseSubject is the proxy-gateway abuse_flagged subject (a NATS
// subject token, lower-cased + dot-free, matches proxy-gateway's
// sanitiseSubjectToken("abuse_flagged")).
const AbuseSubject = "iogrid.audit.proxy.abuse_flagged"

// ConsumerName is the durable consumer name. One per providers-svc
// replica; JetStream load-balances delivery across replicas with the
// same durable name + same filter subject.
const ConsumerName = "providers-svc-abuse-bridge"

// proxyAuditEvent mirrors proxy-gateway's audit.AuditEvent shape
// (decoded from the JetStream payload). Keep the JSON tags in sync
// with coordinator/services/proxy-gateway/internal/audit.AuditEvent.
type proxyAuditEvent struct {
	Timestamp   time.Time         `json:"timestamp"`
	CustomerID  string            `json:"customer_id,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	ProviderID  string            `json:"provider_id,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	ClientAddr  string            `json:"client_addr,omitempty"`
	Destination string            `json:"destination,omitempty"`
	Protocol    string            `json:"protocol,omitempty"`
	EventKind   string            `json:"event_kind"`
	Decision    string            `json:"decision,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	Categories  []string          `json:"categories,omitempty"`
	TraceID     string            `json:"trace_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Bridge subscribes to the AUDIT stream and projects abuse_flagged
// events into the providers-svc Store.
type Bridge struct {
	Store  store.Store
	Logger *slog.Logger

	// AckWait is how long the consumer holds an unacked message before
	// JetStream re-delivers. Default 30s.
	AckWait time.Duration
	// MaxDeliver caps re-delivery attempts before the consumer gives
	// up on a poison message. Default 5.
	MaxDeliver int

	// onMessage is a test seam — when non-nil it's invoked after each
	// successful Append so integration tests can assert without
	// instrumenting the durable consumer state.
	onMessage func(store.AuditEvent)
}

// Options carries the NATS connection info.
type Options struct {
	// NATSURL is the JetStream-enabled NATS URL. Empty → no-op
	// (slog-only fallback; matches proxy-gateway behaviour when
	// NATS_URL is unset).
	NATSURL string
	// Logger is reused for the bridge's structured logs.
	Logger *slog.Logger
	// AckWait + MaxDeliver override the consumer config (see Bridge).
	AckWait    time.Duration
	MaxDeliver int
}

// Cleanup releases the NATS connection + consumer subscription.
type Cleanup func()

// noop is the cleanup used in slog-fallback mode.
var noop Cleanup = func() {}

// Start opens the NATS connection, ensures the AUDIT stream exists,
// creates/updates the durable consumer, and launches the receive
// loop in a goroutine. Returns immediately with a cleanup func.
//
// When NATSURL is empty Start logs a warning + returns the no-op
// cleanup; the providers-svc binary keeps running with the per-
// provider abuse feed simply empty. This matches the local-dev
// path (no NATS infra) and the proxy-gateway's identical fallback.
func Start(ctx context.Context, b *Bridge, opts Options) (Cleanup, error) {
	if b == nil || b.Store == nil {
		return noop, errors.New("transparency: Bridge.Store is required")
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	b.Logger = logger
	if opts.AckWait > 0 {
		b.AckWait = opts.AckWait
	}
	if b.AckWait == 0 {
		b.AckWait = 30 * time.Second
	}
	if opts.MaxDeliver > 0 {
		b.MaxDeliver = opts.MaxDeliver
	}
	if b.MaxDeliver == 0 {
		b.MaxDeliver = 5
	}

	if opts.NATSURL == "" {
		logger.Warn("transparency abuse bridge disabled (NATS_URL unset)",
			slog.String("impact", "per-provider transparency feed will not surface antiabuse blocks until NATS is wired"),
		)
		return noop, nil
	}

	nc, err := nats.Connect(opts.NATSURL,
		nats.Name("iogrid-providers-svc-abuse-bridge"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		logger.Warn("transparency abuse bridge NATS connect failed",
			slog.String("error", err.Error()),
			slog.String("impact", "per-provider abuse feed will be empty until reconnect"),
		)
		return noop, nil
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		logger.Warn("transparency abuse bridge JetStream init failed",
			slog.String("error", err.Error()),
		)
		return noop, nil
	}

	// The proxy-gateway is responsible for creating/configuring the
	// AUDIT stream (subject, retention, storage) — we only consume
	// from it. Fetching the stream confirms our consumer's filter
	// subject is in scope.
	stream, err := js.Stream(ctx, AuditStream)
	if err != nil {
		nc.Close()
		logger.Warn("transparency abuse bridge AUDIT stream not found yet",
			slog.String("error", err.Error()),
			slog.String("hint", "proxy-gateway creates the AUDIT stream on first boot — restart providers-svc once proxy-gateway has been live"),
		)
		return noop, nil
	}
	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       ConsumerName,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: AbuseSubject,
		AckWait:       b.AckWait,
		MaxDeliver:    b.MaxDeliver,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		nc.Close()
		logger.Warn("transparency abuse bridge consumer create failed",
			slog.String("error", err.Error()),
		)
		return noop, nil
	}

	consumeCtx, err := consumer.Consume(b.handleMessage)
	if err != nil {
		nc.Close()
		logger.Warn("transparency abuse bridge consume start failed",
			slog.String("error", err.Error()),
		)
		return noop, nil
	}

	logger.Info("transparency abuse bridge online",
		slog.String("stream", AuditStream),
		slog.String("subject", AbuseSubject),
		slog.String("durable", ConsumerName),
	)
	return func() {
		consumeCtx.Stop()
		_ = nc.Drain()
	}, nil
}

// handleMessage decodes a single proxy-gateway AuditEvent and (when
// it carries a provider_id) appends an EVENT_KIND_ABUSE_FLAGGED row
// to the providers-svc Store.
func (b *Bridge) handleMessage(msg jetstream.Msg) {
	var ev proxyAuditEvent
	if err := json.Unmarshal(msg.Data(), &ev); err != nil {
		b.Logger.Warn("transparency abuse bridge: decode failed",
			slog.String("error", err.Error()),
			slog.Int("len", len(msg.Data())),
		)
		// Bad payloads ack so they don't loop forever on the consumer.
		_ = msg.Ack()
		return
	}

	// Only events that already have a provider_id translate to the
	// per-provider transparency feed — without one the audit_events
	// row has no surface to render against. Legal-retention is the
	// proxy-gateway's AUDIT stream itself; that path is untouched.
	if ev.ProviderID == "" {
		_ = msg.Ack()
		return
	}

	occurred := ev.Timestamp
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	meta := make(map[string]string, len(ev.Metadata)+6)
	for k, v := range ev.Metadata {
		meta[k] = v
	}
	meta["abuse_reason"] = ev.Reason
	if ev.Decision != "" {
		meta["abuse_decision"] = ev.Decision
	}
	if ev.Protocol != "" {
		meta["protocol"] = ev.Protocol
	}
	if ev.SessionID != "" {
		meta["session_id"] = ev.SessionID
	}
	if ev.TraceID != "" {
		meta["trace_id"] = ev.TraceID
	}
	if ev.WorkspaceID != "" {
		meta["workspace_id"] = ev.WorkspaceID
	}

	row := store.AuditEvent{
		ProviderID:         ev.ProviderID,
		Kind:               "EVENT_KIND_ABUSE_FLAGGED",
		OccurredAt:         occurred,
		Category:           firstCategory(ev.Categories),
		DestinationSummary: ev.Destination,
		Metadata:           meta,
	}
	if err := b.Store.AppendAuditEvent(context.Background(), row); err != nil {
		b.Logger.Warn("transparency abuse bridge: store insert failed",
			slog.String("error", err.Error()),
			slog.String("provider_id", ev.ProviderID),
			slog.String("destination", ev.Destination),
			slog.String("reason", ev.Reason),
		)
		// NAK so JetStream re-delivers — likely a transient DB
		// glitch. After MaxDeliver attempts the consumer drops the
		// message and the row stays only in the AUDIT stream.
		_ = msg.Nak()
		return
	}
	if b.onMessage != nil {
		b.onMessage(row)
	}
	if err := msg.Ack(); err != nil {
		b.Logger.Debug("transparency abuse bridge: ack failed",
			slog.String("error", err.Error()),
		)
	}
}

// ApplyEvent is the in-process entrypoint used by unit tests to drive
// the same projection path without standing up a NATS broker. It
// accepts a pre-decoded proxy-gateway audit payload (JSON bytes) and
// returns the projected store.AuditEvent (zero value when the bridge
// would have dropped the message — e.g. no provider_id).
func (b *Bridge) ApplyEvent(ctx context.Context, payload []byte) (store.AuditEvent, error) {
	var ev proxyAuditEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		return store.AuditEvent{}, fmt.Errorf("decode: %w", err)
	}
	if ev.ProviderID == "" {
		return store.AuditEvent{}, nil
	}
	occurred := ev.Timestamp
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	meta := make(map[string]string, len(ev.Metadata)+6)
	for k, v := range ev.Metadata {
		meta[k] = v
	}
	meta["abuse_reason"] = ev.Reason
	if ev.Decision != "" {
		meta["abuse_decision"] = ev.Decision
	}
	if ev.Protocol != "" {
		meta["protocol"] = ev.Protocol
	}
	if ev.SessionID != "" {
		meta["session_id"] = ev.SessionID
	}
	if ev.TraceID != "" {
		meta["trace_id"] = ev.TraceID
	}
	if ev.WorkspaceID != "" {
		meta["workspace_id"] = ev.WorkspaceID
	}
	row := store.AuditEvent{
		ProviderID:         ev.ProviderID,
		Kind:               "EVENT_KIND_ABUSE_FLAGGED",
		OccurredAt:         occurred,
		Category:           firstCategory(ev.Categories),
		DestinationSummary: ev.Destination,
		Metadata:           meta,
	}
	if err := b.Store.AppendAuditEvent(ctx, row); err != nil {
		return store.AuditEvent{}, err
	}
	return row, nil
}

func firstCategory(cats []string) string {
	if len(cats) == 0 {
		return ""
	}
	return cats[0]
}
