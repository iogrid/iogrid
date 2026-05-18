package sse

import (
	"bytes"
	"context"
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
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "event: tick") {
		t.Fatalf("missing event: %q", body)
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
	buf := make([]byte, 1024)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "event: error") {
		t.Fatalf("expected error event in body: %q", body)
	}
}

var errBoom = boomError("boom")

type boomError string

func (e boomError) Error() string { return string(e) }
