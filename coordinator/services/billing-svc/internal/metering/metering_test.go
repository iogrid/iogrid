package metering

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEventToStoreEvent_Happy(t *testing.T) {
	workloadID := uuid.New().String()
	workspaceID := uuid.New().String()
	providerID := uuid.New().String()
	now := time.Now().UTC().Format(time.RFC3339)
	e := &Event{
		WorkloadID:   workloadID,
		WorkspaceID:  workspaceID,
		ProviderID:   providerID,
		WorkloadType: "DOCKER",
		Quantity:     1024,
		CostCents:    250,
		RecordedAt:   now,
	}
	row, err := e.ToStoreEvent()
	if err != nil {
		t.Fatalf("ToStoreEvent: %v", err)
	}
	if row.WorkloadID.String() != workloadID {
		t.Errorf("workload id round-trip failed")
	}
	if row.ProviderID == nil || row.ProviderID.String() != providerID {
		t.Errorf("provider id round-trip failed")
	}
	if row.Currency != "USD" {
		t.Errorf("currency default failed, got %q", row.Currency)
	}
	if row.WorkloadType != "DOCKER" {
		t.Errorf("workload_type round-trip failed")
	}
	if row.Quantity != 1024 || row.CostCents != 250 {
		t.Errorf("quantity / cost round-trip failed")
	}
}

func TestEventToStoreEvent_BadUUIDs(t *testing.T) {
	cases := []struct {
		name string
		e    *Event
	}{
		{"bad workload", &Event{WorkloadID: "nope", WorkspaceID: uuid.New().String(), WorkloadType: "DOCKER", RecordedAt: time.Now().Format(time.RFC3339)}},
		{"bad workspace", &Event{WorkloadID: uuid.New().String(), WorkspaceID: "nope", WorkloadType: "DOCKER", RecordedAt: time.Now().Format(time.RFC3339)}},
		{"bad provider", &Event{WorkloadID: uuid.New().String(), WorkspaceID: uuid.New().String(), ProviderID: "nope", WorkloadType: "DOCKER", RecordedAt: time.Now().Format(time.RFC3339)}},
		{"missing type", &Event{WorkloadID: uuid.New().String(), WorkspaceID: uuid.New().String(), RecordedAt: time.Now().Format(time.RFC3339)}},
		{"bad time", &Event{WorkloadID: uuid.New().String(), WorkspaceID: uuid.New().String(), WorkloadType: "DOCKER", RecordedAt: "not-rfc3339"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := c.e.ToStoreEvent(); err == nil {
				t.Errorf("expected error")
			}
		})
	}
}

func TestEventToStoreEvent_NoProvider(t *testing.T) {
	e := &Event{
		WorkloadID:   uuid.New().String(),
		WorkspaceID:  uuid.New().String(),
		WorkloadType: "BANDWIDTH",
		Quantity:     1 << 30,
		CostCents:    10,
		RecordedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	row, err := e.ToStoreEvent()
	if err != nil {
		t.Fatalf("ToStoreEvent: %v", err)
	}
	if row.ProviderID != nil {
		t.Errorf("expected nil ProviderID for bandwidth pool workload")
	}
}
