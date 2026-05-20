package sse

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWriteEvent_AllFields(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteEvent(&buf, Event{
		ID:    "abc",
		Kind:  "audit_event",
		Data:  "first\nsecond",
		Retry: 5 * time.Second,
	}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"retry: 5000\n", "id: abc\n", "event: audit_event\n", "data: first\n", "data: second\n", "\n\n"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestWriteEvent_JSONPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteEvent(&buf, Event{
		Kind:     "msg",
		DataJSON: map[string]any{"a": 1, "b": "x"},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "data: {") {
		t.Fatalf("expected JSON line: %s", buf.String())
	}
}

func TestHandler_StreamsAndStopsOnCancel(t *testing.T) {
	p := ProducerFunc(func(ctx context.Context, _ string, emit func(Event) error) error {
		for i := 0; i < 3; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := emit(Event{Kind: "tick", Data: "x"}); err != nil {
				return err
			}
		}
		return nil
	})
	srv := httptest.NewServer(Handler(p, 0))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type: %s", got)
	}
	// Drain the full body — the handler emits the initial ": open\n\n"
	// comment in its own flush before the producer runs (see #293), and
	// the producer's three ticks land in subsequent flushes. A single
	// Read can return only the first chunk, so accumulate until EOF
	// (the handler closes the stream when Produce returns).
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := string(body)
	if !strings.Contains(out, ": open") {
		t.Fatalf("missing initial open comment: %q", out)
	}
	if !strings.Contains(out, "event: tick") {
		t.Fatalf("missing event: %q", out)
	}
}

func TestHandler_ProducerErrorEmitsErrorEvent(t *testing.T) {
	p := ProducerFunc(func(_ context.Context, _ string, emit func(Event) error) error {
		return errBoom
	})
	srv := httptest.NewServer(Handler(p, 0))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Drain the full body — the handler flushes the initial open
	// comment (#293) in its own chunk before the error event, so a
	// single Read can return only the open comment.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	out := string(body)
	if !strings.Contains(out, "event: error") {
		t.Fatalf("expected error event in body: %q", out)
	}
}

// TestHandler_EmitsInitialOpenComment guards #292 — without an upfront
// flush, EventSource stays in "connecting" until the first real event.
// On a freshly paired provider with zero events that's forever, and
// the React layer's option-prop instability churned the EventSource
// every render. Both ends of that bug needed fixing; this test pins
// the server half.
func TestHandler_EmitsInitialOpenComment(t *testing.T) {
	// Producer that blocks forever (simulates "provider with no events").
	p := ProducerFunc(func(ctx context.Context, _ string, _ func(Event) error) error {
		<-ctx.Done()
		return ctx.Err()
	})
	srv := httptest.NewServer(Handler(p, 0))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	n, err := resp.Body.Read(buf)
	if err != nil && err.Error() != "EOF" {
		// First read may block briefly but should NOT hang forever.
	}
	body := string(buf[:n])
	if !strings.Contains(body, ": open") {
		t.Fatalf("expected initial `: open` comment, got: %q", body)
	}
}

// TestHandler_KeepAliveConcurrentWithProducerNoRace exercises the new
// write mutex. With race detector on (go test -race), the previous
// implementation would flag concurrent writes to w from the keep-alive
// goroutine and the producer's emit(). After #292's fix the mutex
// serialises both.
func TestHandler_KeepAliveConcurrentWithProducerNoRace(t *testing.T) {
	ready := make(chan struct{})
	stop := make(chan struct{})
	p := ProducerFunc(func(ctx context.Context, _ string, emit func(Event) error) error {
		close(ready)
		// Hammer emit() so it overlaps with the keep-alive ticker.
		ticker := time.NewTicker(2 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; i < 50; i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-stop:
				return nil
			case <-ticker.C:
				if err := emit(Event{Kind: "tick", Data: "x"}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	// 5ms keep-alive => guaranteed overlap with the 2ms emits.
	srv := httptest.NewServer(Handler(p, 5*time.Millisecond))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	<-ready
	// Drain a chunk to ensure both write paths fire.
	buf := make([]byte, 8192)
	_, _ = resp.Body.Read(buf)
	close(stop)
}

var errBoom = boomError("boom")

type boomError string

func (e boomError) Error() string { return string(e) }
