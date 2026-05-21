package transparency

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

// TestBridge_ApplyEvent_ProjectsToAuditEvent — the canonical happy
// path. Inputs a proxy-gateway abuse_flagged AuditEvent (carrying a
// provider_id from a sticky binding) and asserts the bridge writes
// an EVENT_KIND_ABUSE_FLAGGED row into the providers Store with the
// abuse_reason metadata carried through.
func TestBridge_ApplyEvent_ProjectsToAuditEvent(t *testing.T) {
	st := store.NewInMemory()
	pid := uuid.NewString()

	payload, err := json.Marshal(map[string]any{
		"timestamp":    time.Now().UTC().Format(time.RFC3339Nano),
		"customer_id":  "cust-1",
		"workspace_id": "ws-1",
		"provider_id":  pid,
		"session_id":   "sess-1",
		"destination":  "malware.testing.google.test:443",
		"protocol":     "socks5",
		"event_kind":   "abuse_flagged",
		"decision":     "block",
		"reason":       "phishtank_listed",
		"trace_id":     "trace-1",
		"metadata":     map[string]string{"client_addr": "203.0.113.7:55001"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	b := &Bridge{Store: st}
	row, err := b.ApplyEvent(context.Background(), payload)
	if err != nil {
		t.Fatalf("ApplyEvent: %v", err)
	}
	if row.Kind != "EVENT_KIND_ABUSE_FLAGGED" {
		t.Fatalf("kind = %q, want EVENT_KIND_ABUSE_FLAGGED", row.Kind)
	}
	if row.ProviderID != pid {
		t.Fatalf("provider_id = %q, want %q", row.ProviderID, pid)
	}
	if row.DestinationSummary != "malware.testing.google.test:443" {
		t.Fatalf("destination_summary = %q", row.DestinationSummary)
	}
	if row.Metadata["abuse_reason"] != "phishtank_listed" {
		t.Fatalf("metadata[abuse_reason] = %q", row.Metadata["abuse_reason"])
	}
	if row.Metadata["protocol"] != "socks5" {
		t.Fatalf("metadata[protocol] = %q", row.Metadata["protocol"])
	}
	if row.Metadata["trace_id"] != "trace-1" {
		t.Fatalf("metadata[trace_id] = %q", row.Metadata["trace_id"])
	}
	if row.Metadata["client_addr"] != "203.0.113.7:55001" {
		t.Fatalf("metadata[client_addr] dropped from passthrough; got %v", row.Metadata)
	}

	// And the store must surface it on List.
	events, _, err := st.ListAuditEvents(context.Background(), pid, store.AuditQuery{})
	if err != nil {
		t.Fatalf("ListAuditEvents: %v", err)
	}
	if len(events) != 1 || events[0].Kind != "EVENT_KIND_ABUSE_FLAGGED" {
		t.Fatalf("store list = %+v", events)
	}
}

// TestBridge_ApplyEvent_DropsEventsWithoutProviderID — events with
// no provider_id (no sticky binding) MUST be dropped from the per-
// provider feed. The AUDIT stream in NATS still retains the row for
// legal evidence; this bridge's sole job is the per-provider
// projection. Dropping returns a zero-value row + no error.
func TestBridge_ApplyEvent_DropsEventsWithoutProviderID(t *testing.T) {
	st := store.NewInMemory()
	payload, _ := json.Marshal(map[string]any{
		"event_kind":  "abuse_flagged",
		"reason":      "outbound_port_blocked",
		"destination": "mail.example:25",
	})

	b := &Bridge{Store: st}
	row, err := b.ApplyEvent(context.Background(), payload)
	if err != nil {
		t.Fatalf("ApplyEvent: %v", err)
	}
	if row.Kind != "" {
		t.Fatalf("expected zero row when provider_id is empty; got %+v", row)
	}
	// And no row landed in the store.
	events, _, _ := st.ListAuditEvents(context.Background(), "any", store.AuditQuery{})
	if len(events) != 0 {
		t.Fatalf("expected zero rows; got %+v", events)
	}
}

// TestBridge_ApplyEvent_BadPayload — malformed JSON must return an
// error so the NATS layer can NAK (and after MaxDeliver, drop). The
// in-process path surfaces that as a decode error.
func TestBridge_ApplyEvent_BadPayload(t *testing.T) {
	b := &Bridge{Store: store.NewInMemory()}
	if _, err := b.ApplyEvent(context.Background(), []byte("not-json")); err == nil {
		t.Fatal("expected decode error")
	}
}

// TestBridge_ApplyEvent_StreamFanout — when the bridge writes a row
// the in-process StreamAuditEvents subscriber gets the same row on
// its channel. This is the load-bearing wire-up between proxy-gateway
// abuse decisions and the provider's live transparency feed.
func TestBridge_ApplyEvent_StreamFanout(t *testing.T) {
	st := store.NewInMemory()
	pid := uuid.NewString()
	sub, cancel := st.SubscribeAuditEvents(pid)
	defer cancel()

	payload, _ := json.Marshal(map[string]any{
		"provider_id": pid,
		"event_kind":  "abuse_flagged",
		"reason":      "phishtank_listed",
		"destination": "phishing-test.iogrid.org:443",
	})
	b := &Bridge{Store: st}
	if _, err := b.ApplyEvent(context.Background(), payload); err != nil {
		t.Fatalf("ApplyEvent: %v", err)
	}

	select {
	case got := <-sub:
		if got.Kind != "EVENT_KIND_ABUSE_FLAGGED" {
			t.Fatalf("subscriber received kind=%q, want EVENT_KIND_ABUSE_FLAGGED", got.Kind)
		}
		if got.DestinationSummary != "phishing-test.iogrid.org:443" {
			t.Fatalf("subscriber dest=%q", got.DestinationSummary)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive event in 1s")
	}
}
