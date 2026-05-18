// Package audit emits the structured audit events docs/LEGAL.md requires
// to be retained for 90 days, and the metering events the billing-svc
// consumes to invoice customers + pay providers.
//
// Two JetStream streams are used:
//
//   - AUDIT — every customer auth, every abuse-filter decision, every
//     connection accept/reject. Mirror of antiabuse-svc's stream so
//     ops queries can join them. Subject prefix: iogrid.audit.proxy.
//
//   - BILLING — per-relay metering events. Subject prefix:
//     iogrid.billing.bandwidth.
//
// When NATS_URL is unset both emitters fall back to slog so unit tests
// and local dev work offline.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// AuditStream is the JetStream stream name for legal-retention events.
const AuditStream = "AUDIT"

// AuditSubject is the subject prefix used by the proxy-gateway. A dotted
// suffix (event_kind) is appended at publish time.
const AuditSubject = "iogrid.audit.proxy"

// BillingStream is the JetStream stream name for billing meter events.
const BillingStream = "BILLING"

// BillingSubject is the subject prefix for bandwidth metering events.
const BillingSubject = "iogrid.billing.bandwidth"

// AuditEvent is the legal-retention audit payload. Mirrors the antiabuse
// event shape so downstream consumers can use a single schema.
type AuditEvent struct {
	Timestamp   time.Time         `json:"timestamp"`
	CustomerID  string            `json:"customer_id,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	ProviderID  string            `json:"provider_id,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	ClientAddr  string            `json:"client_addr,omitempty"`
	Destination string            `json:"destination,omitempty"`
	Protocol    string            `json:"protocol,omitempty"` // socks5 | http_connect
	EventKind   string            `json:"event_kind"`         // accepted | rejected | relay_started | relay_ended | failover
	Decision    string            `json:"decision,omitempty"` // allow | block | review
	Reason      string            `json:"reason,omitempty"`
	Categories  []string          `json:"categories,omitempty"`
	TraceID     string            `json:"trace_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// BillingEvent is the metering payload. Schema matches the task spec.
type BillingEvent struct {
	Timestamp    time.Time `json:"timestamp"`
	CustomerID   string    `json:"customer_id"`
	WorkspaceID  string    `json:"workspace_id,omitempty"`
	ProviderID   string    `json:"provider_id"`
	WorkloadType string    `json:"workload_type"` // always "bandwidth" here.
	BytesIn      uint64    `json:"bytes_in"`
	BytesOut     uint64    `json:"bytes_out"`
	SessionID    string    `json:"session_id,omitempty"`
	WorkloadID   string    `json:"workload_id,omitempty"`
}

// Emitter publishes AuditEvent + BillingEvent records to JetStream.
type Emitter struct {
	logger *slog.Logger

	mu sync.Mutex
	nc *nats.Conn
	js jetstream.JetStream
}

// Options configures the Emitter.
type Options struct {
	NATSURL string
	Logger  *slog.Logger
	// AuditRetentionDays for the AUDIT stream max-age (default 90).
	AuditRetentionDays int
	// BillingRetentionDays for the BILLING stream max-age (default 90).
	BillingRetentionDays int
}

// New constructs an Emitter. NATS errors fall back to slog-only mode.
func New(ctx context.Context, opts Options) *Emitter {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	e := &Emitter{logger: logger}
	if opts.NATSURL == "" {
		logger.Info("proxy audit/billing emitter using slog fallback (NATS_URL unset)")
		return e
	}
	nc, err := nats.Connect(opts.NATSURL,
		nats.Name("iogrid-proxy-gateway"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		logger.Warn("proxy emitter NATS connect failed; falling back to slog",
			slog.String("error", err.Error()))
		return e
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		logger.Warn("proxy emitter JetStream init failed; falling back to slog",
			slog.String("error", err.Error()))
		return e
	}
	auditRet := opts.AuditRetentionDays
	if auditRet <= 0 {
		auditRet = 90
	}
	billRet := opts.BillingRetentionDays
	if billRet <= 0 {
		billRet = 90
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      AuditStream,
		Subjects:  []string{AuditSubject + ".>"},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    time.Duration(auditRet) * 24 * time.Hour,
		Storage:   jetstream.FileStorage,
	}); err != nil {
		nc.Close()
		logger.Warn("proxy AUDIT stream init failed; falling back to slog",
			slog.String("error", err.Error()))
		return e
	}
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      BillingStream,
		Subjects:  []string{BillingSubject + ".>"},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    time.Duration(billRet) * 24 * time.Hour,
		Storage:   jetstream.FileStorage,
	}); err != nil {
		nc.Close()
		logger.Warn("proxy BILLING stream init failed; falling back to slog",
			slog.String("error", err.Error()))
		return e
	}
	e.nc = nc
	e.js = js
	logger.Info("proxy audit/billing emitter using NATS JetStream",
		slog.Int("audit_retention_days", auditRet),
		slog.Int("billing_retention_days", billRet))
	return e
}

// Close releases the NATS connection.
func (e *Emitter) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.nc != nil {
		e.nc.Drain() //nolint:errcheck
		e.nc = nil
	}
}

// EmitAudit publishes an AuditEvent. Always logs at INFO so even if
// JetStream is unavailable the operator gets a structured line.
func (e *Emitter) EmitAudit(ctx context.Context, ev AuditEvent) error {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	e.logger.Info("proxy_audit",
		slog.String("event_kind", ev.EventKind),
		slog.String("protocol", ev.Protocol),
		slog.String("destination", ev.Destination),
		slog.String("decision", ev.Decision),
		slog.String("reason", ev.Reason),
		slog.String("customer_id", ev.CustomerID),
		slog.String("provider_id", ev.ProviderID),
		slog.String("session_id", ev.SessionID),
		slog.String("trace_id", ev.TraceID),
	)
	if e.js == nil {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	subj := AuditSubject + "." + sanitiseSubjectToken(ev.EventKind)
	_, err = e.js.Publish(ctx, subj, body)
	if err != nil && !errors.Is(err, context.Canceled) {
		e.logger.Warn("proxy audit publish failed", slog.String("error", err.Error()))
		return err
	}
	return nil
}

// EmitBilling publishes a BillingEvent. Bills are best-effort durable —
// the relay loop continues forwarding bytes even on emit error so a
// transient NATS hiccup never blocks customer traffic.
func (e *Emitter) EmitBilling(ctx context.Context, ev BillingEvent) error {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	if ev.WorkloadType == "" {
		ev.WorkloadType = "bandwidth"
	}
	e.logger.Debug("proxy_billing",
		slog.String("customer_id", ev.CustomerID),
		slog.String("provider_id", ev.ProviderID),
		slog.Uint64("bytes_in", ev.BytesIn),
		slog.Uint64("bytes_out", ev.BytesOut),
		slog.String("session_id", ev.SessionID),
	)
	if e.js == nil {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	subj := BillingSubject + "." + sanitiseSubjectToken(ev.WorkloadType)
	_, err = e.js.Publish(ctx, subj, body)
	if err != nil && !errors.Is(err, context.Canceled) {
		e.logger.Warn("proxy billing publish failed", slog.String("error", err.Error()))
		return err
	}
	return nil
}

// sanitiseSubjectToken keeps NATS subject tokens lowercase and dot-free.
func sanitiseSubjectToken(s string) string {
	if s == "" {
		return "unknown"
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= '0' && r <= '9',
			r == '_', r == '-':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
