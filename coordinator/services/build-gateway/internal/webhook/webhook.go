// Package webhook delivers HMAC-signed build status events to a customer's
// configured callback URL.
//
// Delivery is fire-and-forget from the handler's perspective: the gateway
// enqueues an event and returns 202 to the customer immediately. A
// background dispatcher pulls from the queue and POSTs with bounded
// retries.
//
// Signature scheme matches GitHub's webhook style — a SHA-256 HMAC of the
// raw request body, hex-encoded, sent as:
//
//	X-Iogrid-Signature-256: sha256=<hex>
//
// Customers verify with the secret they registered at build-submission
// time. This package never logs the secret.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// Event is the wire shape we POST to the customer.
type Event struct {
	// EventID is a stable per-delivery id so customers can dedupe
	// retries.
	EventID string `json:"event_id"`
	// BuildID is the iogrid build the event pertains to.
	BuildID string `json:"build_id"`
	// WorkspaceID owns the build.
	WorkspaceID string `json:"workspace_id"`
	// Status is the new build status (e.g. "running", "succeeded").
	Status string `json:"status"`
	// Note is the free-form provider-side annotation, if any.
	Note string `json:"note,omitempty"`
	// OccurredAt is the gateway-side timestamp when the status changed.
	OccurredAt time.Time `json:"occurred_at"`
	// AttemptID is the workloads-svc attempt id (set on dispatched or
	// later transitions).
	AttemptID string `json:"attempt_id,omitempty"`
}

// Dispatcher delivers Events to remote webhook URLs.
type Dispatcher interface {
	// Enqueue schedules ev for delivery to url, signed with secret. The
	// call returns immediately; failure to actually reach url is
	// surfaced via logs + metrics, not the return value.
	Enqueue(ctx context.Context, url, secret string, ev Event)
}

// SignBody computes the canonical X-Iogrid-Signature-256 header value for
// body under secret. Exposed for receivers that want to validate locally.
func SignBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// AsyncDispatcher is the production Dispatcher. It runs N worker goroutines
// that drain a buffered channel of pending deliveries with bounded retry.
//
// Backpressure: if the buffer is full when Enqueue is called, the event is
// dropped and a metric is incremented. Webhook delivery is best-effort by
// design — customers MUST also poll GET /v1/builds/{id} for ground truth.
type AsyncDispatcher struct {
	httpClient *http.Client
	logger     *slog.Logger
	queue      chan pending

	// retry policy
	maxAttempts int
	baseBackoff time.Duration

	// drop counter for tests / metrics export
	mu      sync.Mutex
	dropped int
}

type pending struct {
	url    string
	secret string
	body   []byte
	id     string
}

// NewAsyncDispatcher returns a fresh dispatcher with workers already
// running. Cancel ctx to stop them gracefully.
func NewAsyncDispatcher(ctx context.Context, logger *slog.Logger, workers, bufSize int) *AsyncDispatcher {
	if workers <= 0 {
		workers = 4
	}
	if bufSize <= 0 {
		bufSize = 256
	}
	if logger == nil {
		logger = slog.Default()
	}
	d := &AsyncDispatcher{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger:      logger,
		queue:       make(chan pending, bufSize),
		maxAttempts: 5,
		baseBackoff: 200 * time.Millisecond,
	}
	for i := 0; i < workers; i++ {
		go d.worker(ctx)
	}
	return d
}

// Enqueue implements Dispatcher.
func (d *AsyncDispatcher) Enqueue(_ context.Context, url, secret string, ev Event) {
	if url == "" || secret == "" {
		return
	}
	body, err := json.Marshal(ev)
	if err != nil {
		d.logger.Warn("webhook: marshal event failed", slog.String("error", err.Error()))
		return
	}
	p := pending{url: url, secret: secret, body: body, id: ev.EventID}
	select {
	case d.queue <- p:
	default:
		d.mu.Lock()
		d.dropped++
		d.mu.Unlock()
		d.logger.Warn("webhook: queue full, event dropped",
			slog.String("event_id", ev.EventID),
			slog.String("build_id", ev.BuildID),
		)
	}
}

// Dropped returns the cumulative count of events that couldn't be queued
// because the buffer was full. Exposed for tests + ops dashboards.
func (d *AsyncDispatcher) Dropped() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dropped
}

func (d *AsyncDispatcher) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-d.queue:
			if !ok {
				return
			}
			d.deliver(ctx, p)
		}
	}
}

func (d *AsyncDispatcher) deliver(ctx context.Context, p pending) {
	for attempt := 1; attempt <= d.maxAttempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(p.body))
		if err != nil {
			d.logger.Warn("webhook: build request failed", slog.String("error", err.Error()))
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Iogrid-Signature-256", SignBody(p.secret, p.body))
		req.Header.Set("X-Iogrid-Event-Id", p.id)
		req.Header.Set("User-Agent", "iogrid-build-gateway/1.0")
		resp, err := d.httpClient.Do(req)
		if err == nil {
			// Drain body so the connection can be re-used.
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return
			}
		}
		if attempt == d.maxAttempts {
			d.logger.Warn("webhook: delivery giving up",
				slog.String("url", p.url),
				slog.String("event_id", p.id),
				slog.Int("attempts", attempt),
			)
			return
		}
		backoff := d.baseBackoff * time.Duration(1<<uint(attempt-1))
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// Noop is a Dispatcher that drops every event silently. Used as the default
// when no webhook URL was registered for a build.
type Noop struct{}

// Enqueue implements Dispatcher.
func (Noop) Enqueue(_ context.Context, _, _ string, _ Event) {}

// Recorder is a test-only Dispatcher that just appends every event to a
// slice. Use to assert webhook payloads in unit tests.
type Recorder struct {
	mu     sync.Mutex
	events []Event
}

// NewRecorder builds a fresh Recorder.
func NewRecorder() *Recorder {
	return &Recorder{}
}

// Enqueue implements Dispatcher.
func (r *Recorder) Enqueue(_ context.Context, _, _ string, ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

// Events returns a defensive copy of every event Enqueued so far.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Event, len(r.events))
	copy(out, r.events)
	return out
}

// VerifySignatureHeader is a convenience helper for tests that want to
// confirm an incoming X-Iogrid-Signature-256 header matches the body. It
// constant-time compares to defeat timing-leak based forgery attempts.
func VerifySignatureHeader(secret, header string, body []byte) bool {
	expected := SignBody(secret, body)
	return hmac.Equal([]byte(expected), []byte(header))
}

// ErrInvalidWebhookURL is returned by ValidateWebhookURL for non-https
// targets or syntactically broken URLs.
var ErrInvalidWebhookURL = fmt.Errorf("webhook url must be https")
