// Package audit emits the structured audit events docs/LEGAL.md
// requires us to retain for 90 days.
//
// When NATS_URL is set, every Event is published to the "AUDIT"
// JetStream stream so Loki / a downstream consumer can persist it.
// When NATS_URL is empty (local dev, tests) the emitter falls back
// to slog at INFO level — operators still get a structured line and
// observability tooling can pick it up.
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

// StreamName is the JetStream stream we publish to.
const StreamName = "AUDIT"

// Subject is the canonical subject prefix.
const Subject = "iogrid.audit.antiabuse"

// Event is the canonical audit-log payload.
type Event struct {
	Timestamp   time.Time         `json:"timestamp"`
	CustomerID  string            `json:"customer_id,omitempty"`
	ProviderID  string            `json:"provider_id,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	WorkloadID  string            `json:"workload_id,omitempty"`
	CheckType   string            `json:"check_type"`
	Target      string            `json:"target"`
	Decision    string            `json:"decision"`
	Reason      string            `json:"reason"`
	Explanation string            `json:"explanation,omitempty"`
	TraceID     string            `json:"trace_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// Emitter publishes Events.
type Emitter struct {
	logger *slog.Logger

	mu     sync.Mutex
	nc     *nats.Conn
	js     jetstream.JetStream
	stream jetstream.Stream
}

// Options configure the Emitter.
type Options struct {
	NATSURL string
	Logger  *slog.Logger
	// RetentionDays is the JetStream max-age for AUDIT messages
	// (default 90 days per docs/LEGAL.md).
	RetentionDays int
}

// New constructs an Emitter. If NATSURL is empty the emitter operates
// in slog-only mode. If NATS is reachable a JetStream "AUDIT" stream
// is created if it doesn't exist, with the configured retention.
func New(ctx context.Context, opts Options) (*Emitter, error) {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	e := &Emitter{logger: logger}
	if opts.NATSURL == "" {
		logger.Info("audit emitter using slog fallback (NATS_URL unset)")
		return e, nil
	}
	nc, err := nats.Connect(opts.NATSURL,
		nats.Name("iogrid-antiabuse-svc"),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
	)
	if err != nil {
		// Failing closed on audit is too aggressive — the legal
		// requirement is "we tried"; log + fall back to slog.
		logger.Warn("audit emitter NATS connect failed; falling back to slog",
			slog.String("error", err.Error()))
		return e, nil
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		logger.Warn("audit emitter JetStream init failed; falling back to slog",
			slog.String("error", err.Error()))
		return e, nil
	}
	retention := opts.RetentionDays
	if retention <= 0 {
		retention = 90
	}
	stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{Subject + ".>"},
		Retention: jetstream.LimitsPolicy,
		MaxAge:    time.Duration(retention) * 24 * time.Hour,
		Storage:   jetstream.FileStorage,
	})
	if err != nil {
		nc.Close()
		logger.Warn("audit emitter stream init failed; falling back to slog",
			slog.String("error", err.Error()))
		return e, nil
	}
	e.nc = nc
	e.js = js
	e.stream = stream
	logger.Info("audit emitter using NATS JetStream",
		slog.String("stream", StreamName),
		slog.Int("retention_days", retention),
	)
	return e, nil
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

// Emit publishes an Event. It logs at INFO level first (so even if
// JetStream is down the line is in stdout) and then attempts the
// NATS publish.
func (e *Emitter) Emit(ctx context.Context, ev Event) error {
	if ev.Timestamp.IsZero() {
		ev.Timestamp = time.Now()
	}
	e.logger.Info("antiabuse_audit",
		slog.String("check_type", ev.CheckType),
		slog.String("target", ev.Target),
		slog.String("decision", ev.Decision),
		slog.String("reason", ev.Reason),
		slog.String("customer_id", ev.CustomerID),
		slog.String("provider_id", ev.ProviderID),
		slog.String("trace_id", ev.TraceID),
	)
	if e.js == nil {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	subj := Subject + "." + sanitiseSubjectToken(ev.CheckType)
	_, err = e.js.Publish(ctx, subj, body)
	if err != nil && !errors.Is(err, context.Canceled) {
		e.logger.Warn("audit emit failed",
			slog.String("error", err.Error()),
			slog.String("subject", subj),
		)
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
