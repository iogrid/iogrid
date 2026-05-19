package session

import (
	"testing"
	"time"
)

func TestBindAndGet(t *testing.T) {
	s := New(15 * time.Minute)
	now := time.Now()
	s.WithClock(func() time.Time { return now })

	b := s.Bind("u1", "p1", "US")
	if b.ProviderID != "p1" {
		t.Errorf("bind provider = %s", b.ProviderID)
	}
	got, ok := s.Get("u1")
	if !ok || got.ProviderID != "p1" {
		t.Error("Get miss after Bind")
	}
}

func TestExpiry(t *testing.T) {
	now := time.Now()
	clock := now
	s := New(time.Minute)
	s.WithClock(func() time.Time { return clock })
	s.Bind("u1", "p1", "DE")

	clock = now.Add(30 * time.Second)
	if _, ok := s.Get("u1"); !ok {
		t.Error("should still be bound at 30s")
	}
	clock = now.Add(2 * time.Minute)
	if _, ok := s.Get("u1"); ok {
		t.Error("should be expired at 2 minutes")
	}
}

func TestStickyExtend(t *testing.T) {
	now := time.Now()
	clock := now
	s := New(time.Minute)
	s.WithClock(func() time.Time { return clock })
	s.Bind("u1", "p1", "DE")

	clock = now.Add(45 * time.Second)
	s.Bind("u1", "p1", "DE") // same country -> extend
	clock = now.Add(90 * time.Second)
	if _, ok := s.Get("u1"); !ok {
		t.Error("extended binding should still be live at 90s (45s + 60s window)")
	}
}

func TestCountryChangeRebinds(t *testing.T) {
	now := time.Now()
	clock := now
	s := New(time.Minute)
	s.WithClock(func() time.Time { return clock })
	s.Bind("u1", "p1", "DE")

	clock = now.Add(10 * time.Second)
	b2 := s.Bind("u1", "p2", "JP")
	if b2.Country != "JP" || b2.ProviderID != "p2" {
		t.Errorf("country change should overwrite: %+v", b2)
	}
}

func TestSweep(t *testing.T) {
	now := time.Now()
	clock := now
	s := New(time.Minute)
	s.WithClock(func() time.Time { return clock })
	for i := 0; i < 5; i++ {
		s.Bind(string(rune('a'+i)), "p", "US")
	}
	if s.Len() != 5 {
		t.Errorf("Len = %d, want 5", s.Len())
	}
	clock = now.Add(2 * time.Minute)
	n := s.Sweep()
	if n != 5 {
		t.Errorf("Sweep removed %d, want 5", n)
	}
}

func TestDrop(t *testing.T) {
	s := New(time.Minute)
	s.Bind("u1", "p1", "US")
	s.Drop("u1")
	if _, ok := s.Get("u1"); ok {
		t.Error("Get should miss after Drop")
	}
}
