package metering

import (
	"context"
	"testing"
	"time"
)

func TestAddAndMonthToDate(t *testing.T) {
	m := New(nil)
	m.AddBytes("u1", 100, 200)
	m.AddBytes("u1", 50, 75)
	c := m.MonthToDate("u1")
	if c.BytesIn != 150 {
		t.Errorf("BytesIn = %d, want 150", c.BytesIn)
	}
	if c.BytesOut != 275 {
		t.Errorf("BytesOut = %d, want 275", c.BytesOut)
	}
	if c.Total() != 425 {
		t.Errorf("Total = %d, want 425", c.Total())
	}
}

func TestMissReturnsZero(t *testing.T) {
	m := New(nil)
	c := m.MonthToDate("ghost")
	if c.Total() != 0 {
		t.Errorf("miss should return zero, got %+v", c)
	}
}

func TestFlushAll(t *testing.T) {
	var events []Event
	m := New(func(_ context.Context, ev Event) error {
		events = append(events, ev)
		return nil
	})
	m.AddBytes("u1", 1000, 2000)
	m.AddBytes("u2", 500, 0)

	n, err := m.FlushAll(context.Background(), func(id string) string {
		if id == "u1" {
			return "plus"
		}
		return "free"
	})
	if err != nil {
		t.Fatalf("FlushAll: %v", err)
	}
	if n != 2 {
		t.Errorf("FlushAll emitted %d, want 2", n)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	// Counters remain in place (month-to-date accumulator).
	if c := m.MonthToDate("u1"); c.BytesIn != 1000 {
		t.Errorf("counters should persist post-flush, got %+v", c)
	}
}

func TestFlushNoEmitter(t *testing.T) {
	m := New(nil)
	m.AddBytes("u1", 100, 100)
	if _, err := m.FlushAll(context.Background(), func(string) string { return "free" }); err != nil {
		t.Errorf("FlushAll with nil emitter should be a no-op, got %v", err)
	}
}

func TestMonthRollover(t *testing.T) {
	clock := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	m := New(nil).WithClock(func() time.Time { return clock })
	m.AddBytes("u1", 1<<30, 0)
	if m.MonthToDate("u1").Total() != 1<<30 {
		t.Error("january usage missing")
	}
	clock = time.Date(2026, 2, 1, 0, 0, 1, 0, time.UTC)
	// First MonthToDate call after rollover should reset.
	if c := m.MonthToDate("u1"); c.Total() != 0 {
		t.Errorf("february should reset, got %+v", c)
	}
}
