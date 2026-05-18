// Log streaming for the build-gateway.
//
// Mac providers stream the running build's stdout/stderr back to the gateway
// over the workloads-svc dispatch frames. The gateway buffers the lines in
// memory (capped, ring-buffer style) so customers can both:
//
//  1. Tail a live build via Server-Sent Events on GET /v1/builds/{id}/logs
//  2. Re-fetch from any point — the buffer's Seq lets SSE clients resume
//     on disconnect via Last-Event-ID
//
// The buffer is intentionally small (a few thousand lines max per build) —
// the canonical log is the S3 object the provider uploads at terminal
// state. The in-gateway buffer is only for live tail.
package builds

import (
	"context"
	"sync"
	"time"
)

// LogLine is one structured emission from the build VM.
type LogLine struct {
	// Seq is a monotonically-increasing sequence number, scoped to the
	// build. Used as the SSE id.
	Seq uint64 `json:"seq"`
	// Stream is "stdout" or "stderr".
	Stream string `json:"stream"`
	// Text is the line content (no trailing newline).
	Text string `json:"text"`
	// At is the gateway-side ingress timestamp.
	At time.Time `json:"at"`
}

// LogBroker fans out log lines for ONE build to N live subscribers and
// retains a ring-buffer of recent history for late joiners.
//
// One LogBroker exists per build. The Hub manages the per-build instances.
type LogBroker struct {
	mu       sync.RWMutex
	lines    []LogLine // ring buffer
	capacity int
	nextSeq  uint64
	subs     map[chan LogLine]struct{}
	// closed indicates the build reached a terminal state — no more
	// lines will arrive. Subscribers see their channel closed.
	closed bool
}

// NewLogBroker builds a broker with the given history capacity. capacity<=0
// defaults to 2048 lines.
func NewLogBroker(capacity int) *LogBroker {
	if capacity <= 0 {
		capacity = 2048
	}
	return &LogBroker{
		lines:    make([]LogLine, 0, capacity),
		capacity: capacity,
		subs:     make(map[chan LogLine]struct{}),
	}
}

// Append records a line. Returns the Seq assigned. Lines arriving after
// Close() are silently dropped.
func (b *LogBroker) Append(stream, text string, at time.Time) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0
	}
	if at.IsZero() {
		at = time.Now()
	}
	b.nextSeq++
	line := LogLine{Seq: b.nextSeq, Stream: stream, Text: text, At: at}
	if len(b.lines) >= b.capacity {
		// Drop oldest. Use a copy-shift rather than a true ring to keep
		// snapshot iteration cheap (no wrap-around bookkeeping).
		b.lines = b.lines[1:]
	}
	b.lines = append(b.lines, line)
	for ch := range b.subs {
		select {
		case ch <- line:
		default:
			// Slow consumer — drop to keep producers fast. SSE clients
			// can refetch via fromSeq query.
		}
	}
	return line.Seq
}

// Snapshot returns every line currently in the buffer with seq >= fromSeq.
// Pass fromSeq=0 for the entire buffer.
func (b *LogBroker) Snapshot(fromSeq uint64) []LogLine {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]LogLine, 0, len(b.lines))
	for _, l := range b.lines {
		if l.Seq >= fromSeq {
			out = append(out, l)
		}
	}
	return out
}

// Subscribe registers a channel to receive future lines. The channel MUST
// be buffered — a full channel results in line drops on the producer side.
// Returns a cancel func the caller MUST run when done (typically deferred).
func (b *LogBroker) Subscribe(ch chan LogLine) func() {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		close(ch)
		return func() {}
	}
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			// Don't close ch — the caller owns it.
		}
	}
}

// Close marks the broker terminal and closes every subscriber channel.
// Idempotent.
func (b *LogBroker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subs {
		close(ch)
	}
	b.subs = map[chan LogLine]struct{}{}
}

// IsClosed reports whether Close has been called.
func (b *LogBroker) IsClosed() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.closed
}

// LogHub is the registry of LogBrokers keyed by build id.
type LogHub struct {
	mu       sync.Mutex
	brokers  map[string]*LogBroker
	capacity int
}

// NewLogHub returns a fresh hub. perBuildCap is forwarded to every broker.
func NewLogHub(perBuildCap int) *LogHub {
	return &LogHub{
		brokers:  make(map[string]*LogBroker),
		capacity: perBuildCap,
	}
}

// For returns (and lazily creates) the broker for buildID.
func (h *LogHub) For(buildID string) *LogBroker {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, ok := h.brokers[buildID]
	if !ok {
		b = NewLogBroker(h.capacity)
		h.brokers[buildID] = b
	}
	return b
}

// Drop closes and removes the broker for buildID. Called once the build is
// fully archived and the live tail is no longer needed.
func (h *LogHub) Drop(buildID string) {
	h.mu.Lock()
	b, ok := h.brokers[buildID]
	delete(h.brokers, buildID)
	h.mu.Unlock()
	if ok {
		b.Close()
	}
}

// CloseAll closes every broker. Used during graceful shutdown so live SSE
// streams disconnect cleanly.
func (h *LogHub) CloseAll(_ context.Context) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, b := range h.brokers {
		b.Close()
		delete(h.brokers, id)
	}
}
