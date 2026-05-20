// Package sse implements the Server-Sent-Events transport used by
// gateway-bff to push real-time updates to the Next.js web app.
//
// Wire format follows the HTML5 SSE spec exactly:
//
//	id: <event id>\n
//	event: <event kind>\n
//	data: <json payload>\n
//	\n
//
// Reconnect-safe: clients that resume a dropped connection MAY send a
// Last-Event-ID header. We forward that to the producer so it can
// resume from the right point. Producers that don't support resume can
// ignore the header.
//
// We DO NOT pull in an external SSE library (r3labs/sse) — the protocol
// is too small to justify a dependency, and our event filtering
// (per-user JetStream consumers, kind filters) is bespoke anyway.
package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Event is one server-side event ready to be flushed to the client.
// Either Data or DataJSON is set; if both are set, DataJSON wins.
type Event struct {
	// ID is the SSE `id:` field — clients echo the latest as
	// Last-Event-ID on reconnect.
	ID string
	// Kind populates the `event:` field. Empty for "message".
	Kind string
	// Data is the raw payload. NEWLINES INSIDE MUST BE ESCAPED — the
	// Write helper handles multi-line correctly by emitting multiple
	// `data:` lines.
	Data string
	// DataJSON is a more convenient alternative: marshalled to JSON
	// before write. Cheaper than carrying byte slices through the
	// stream API.
	DataJSON any
	// Retry, if non-zero, sets the `retry:` reconnect-budget the
	// browser uses when the connection drops.
	Retry time.Duration
}

// Producer is the interface BFF route-handlers implement to feed an
// SSE stream. The handler invokes Produce once per request; Produce
// returns when (a) the downstream source is exhausted, (b) the request
// context is cancelled, or (c) it hits a non-recoverable error.
type Producer interface {
	Produce(ctx context.Context, lastEventID string, emit func(Event) error) error
}

// ProducerFunc adapts an ordinary function to the Producer interface.
type ProducerFunc func(ctx context.Context, lastEventID string, emit func(Event) error) error

// Produce implements Producer.
func (f ProducerFunc) Produce(ctx context.Context, lastEventID string, emit func(Event) error) error {
	return f(ctx, lastEventID, emit)
}

// Handler returns an http.HandlerFunc that runs the producer. Handlers
// MUST set Cache-Control: no-cache, X-Accel-Buffering: no (for nginx),
// and set Content-Type before any write happens. We do all three.
//
// keepAlive sends a `:keep-alive` comment line every keepAlive duration
// to defeat idle proxy timeouts. Zero disables keep-alives.
//
// The keep-alive runs in a separate goroutine concurrent with the
// producer's emit() calls, so all writes to the underlying
// http.ResponseWriter MUST be serialised through a mutex. Without the
// mutex (the previous implementation, fixed in #292) concurrent writes
// can corrupt the SSE frame boundary or — worse — race the response
// writer into a half-written state that nginx/traefik treat as a
// premature close, causing the browser's EventSource to reconnect.
// That was the root cause of the 30+ reconnects/10s storm reported in
// #292.
func Handler(p Producer, keepAlive time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ctx := r.Context()
		lastEventID := r.Header.Get("Last-Event-ID")

		// Single writer mutex: every byte that hits w must pass through
		// here. Both the producer's emit() and the keep-alive ticker
		// goroutine share this lock.
		var writeMu sync.Mutex

		// On entry, immediately emit a comment so the response body has
		// at least one chunk on the wire. Without this, downstream
		// proxies (BFF Route Handler → traefik → client) may not flush
		// any bytes until the first real event, leaving the browser
		// stuck in "connecting" state. With the comment, EventSource's
		// `open` listener fires within ms, which lets the frontend
		// flip "connecting" → "live (no events yet)" immediately.
		writeMu.Lock()
		_ = writeComment(w, "open")
		flusher.Flush()
		writeMu.Unlock()

		// Keep-alive ticker — feeds a serialised write under the same
		// mutex as the producer. Defeats idle proxy timeouts AND keeps
		// the EventSource happy on streams that have zero real events
		// for long stretches (e.g. a freshly paired provider with no
		// workloads yet — the #292 trigger).
		kaCtx, cancelKA := context.WithCancel(ctx)
		defer cancelKA()
		if keepAlive > 0 {
			go func() {
				t := time.NewTicker(keepAlive)
				defer t.Stop()
				for {
					select {
					case <-kaCtx.Done():
						return
					case <-t.C:
						writeMu.Lock()
						err := writeComment(w, "keep-alive")
						if err == nil {
							flusher.Flush()
						}
						writeMu.Unlock()
						if err != nil {
							// Client gone — bail; the producer's
							// ctx will fire next and unwind too.
							return
						}
					}
				}
			}()
		}

		emit := func(e Event) error {
			writeMu.Lock()
			defer writeMu.Unlock()
			if err := WriteEvent(w, e); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}
		if err := p.Produce(ctx, lastEventID, emit); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			// Terminal error — emit a final error event so the client
			// can show a banner before the stream closes. (SSE has no
			// status-code path once headers are sent.)
			_ = emit(Event{Kind: "error", Data: err.Error()})
		}
	}
}

// WriteEvent serialises one Event to the SSE wire format.
func WriteEvent(w io.Writer, e Event) error {
	if e.Retry > 0 {
		if _, err := fmt.Fprintf(w, "retry: %d\n", e.Retry.Milliseconds()); err != nil {
			return err
		}
	}
	if e.ID != "" {
		if _, err := fmt.Fprintf(w, "id: %s\n", e.ID); err != nil {
			return err
		}
	}
	if e.Kind != "" {
		if _, err := fmt.Fprintf(w, "event: %s\n", e.Kind); err != nil {
			return err
		}
	}
	body := e.Data
	if e.DataJSON != nil {
		b, err := json.Marshal(e.DataJSON)
		if err != nil {
			return err
		}
		body = string(b)
	}
	if body == "" {
		body = "{}"
	}
	for _, line := range strings.Split(body, "\n") {
		if _, err := fmt.Fprintf(w, "data: %s\n", line); err != nil {
			return err
		}
	}
	if _, err := w.Write([]byte("\n")); err != nil {
		return err
	}
	return nil
}

// writeComment writes a `: <text>\n\n` SSE comment, ignored by clients
// but useful to keep the connection alive.
func writeComment(w io.Writer, text string) error {
	_, err := fmt.Fprintf(w, ": %s\n\n", text)
	return err
}
