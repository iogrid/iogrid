package metering

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// capturingPublisher records the last (subject, data) so a test can inspect the
// billing wire envelope the NATS emitter produced.
type capturingPublisher struct {
	subject string
	data    []byte
}

func (c *capturingPublisher) Publish(subject string, data []byte) error {
	c.subject = subject
	c.data = data
	return nil
}

// #744: a finished build must publish a billing wire envelope that billing-svc
// can persist as a provider-attributed, priced usage_event — otherwise the
// /provide earnings card never moves off 0 for builds. Asserts the field
// mapping (build_id→workload_id), provider attribution, workload_type, and the
// provider-share cost.
func TestNATSEmitterPublishesBillingWireEnvelope(t *testing.T) {
	cap := &capturingPublisher{}
	em := &NATSEmitter{Pub: cap}

	finished := time.Date(2026, 6, 12, 9, 0, 0, 0, time.UTC)
	err := em.Emit(context.Background(), Event{
		BuildID:         "bld-xyz",
		WorkspaceID:     "ws-1",
		ProviderID:      "prov-hatice",
		BillableMinutes: 4,
		FinishedAt:      finished,
		TerminalStatus:  "succeeded",
	})
	if err != nil {
		t.Fatal(err)
	}

	if cap.subject != Subject {
		t.Fatalf("subject = %q, want %q", cap.subject, Subject)
	}
	var got BillingWireEvent
	if err := json.Unmarshal(cap.data, &got); err != nil {
		t.Fatalf("published bytes are not valid JSON: %v", err)
	}
	if got.WorkloadID != "bld-xyz" {
		t.Errorf("workload_id = %q, want the build id", got.WorkloadID)
	}
	if got.ProviderID != "prov-hatice" {
		t.Errorf("provider_id = %q, want prov-hatice (else earnings credit nobody)", got.ProviderID)
	}
	if got.WorkloadType != WireWorkloadType {
		t.Errorf("workload_type = %q, want %q", got.WorkloadType, WireWorkloadType)
	}
	if got.Quantity != 4 {
		t.Errorf("quantity = %d, want 4 minutes", got.Quantity)
	}
	if want := int64(4) * ProviderShareCentsPerMinute; got.CostCents != want {
		t.Errorf("cost_cents = %d, want %d (provider share of 4 min)", got.CostCents, want)
	}
	if got.CostCents == 0 {
		t.Error("cost_cents is 0 — a real build must price > 0 or the card stays at 0")
	}
}

// A nil publisher (NATS_URL unset path mid-construction) must not panic.
func TestNATSEmitterNilPublisherIsNoop(t *testing.T) {
	em := &NATSEmitter{}
	if err := em.Emit(context.Background(), Event{BuildID: "b"}); err != nil {
		t.Fatalf("nil-publisher emit should be a no-op, got %v", err)
	}
}
