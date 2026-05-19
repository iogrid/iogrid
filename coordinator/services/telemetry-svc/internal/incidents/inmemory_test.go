package incidents

import (
	"context"
	"testing"
	"time"
)

func TestInMemory_CreateIncident_DefaultsAndSeedUpdate(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()

	// Pin the clock so timestamps are deterministic.
	fixed := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return fixed })

	inc, err := s.CreateIncident(ctx, CreateIncidentInput{Title: "Proxy slowness"})
	if err != nil {
		t.Fatalf("CreateIncident: %v", err)
	}
	if inc.ID.String() == "" {
		t.Error("ID not populated")
	}
	if inc.Status != StatusInvestigating {
		t.Errorf("Status = %q, want investigating (default)", inc.Status)
	}
	if inc.Impact != ImpactMinor {
		t.Errorf("Impact = %q, want minor (default)", inc.Impact)
	}
	if !inc.StartedAt.Equal(fixed) {
		t.Errorf("StartedAt = %v, want %v", inc.StartedAt, fixed)
	}
	if len(inc.Updates) != 1 {
		t.Fatalf("want 1 seed update, got %d", len(inc.Updates))
	}
	if inc.Updates[0].Status != StatusInvestigating {
		t.Errorf("seed update status = %q", inc.Updates[0].Status)
	}
}

func TestInMemory_CreateIncident_Validation(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()

	cases := []struct {
		name string
		in   CreateIncidentInput
	}{
		{"empty title", CreateIncidentInput{}},
		{"whitespace title", CreateIncidentInput{Title: "   "}},
		{"invalid status", CreateIncidentInput{Title: "x", Status: Status("bogus")}},
		{"invalid impact", CreateIncidentInput{Title: "x", Impact: Impact("nuclear")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.CreateIncident(ctx, tc.in); err == nil {
				t.Errorf("want error, got nil for %+v", tc.in)
			}
		})
	}
}

func TestInMemory_AppendUpdate_AdvancesStatusAndResolves(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()

	t0 := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return t0 })

	inc, err := s.CreateIncident(ctx, CreateIncidentInput{Title: "X"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Advance the clock for each update.
	s.SetClock(func() time.Time { return t0.Add(time.Minute) })
	if _, err := s.AppendUpdate(ctx, inc.ID, UpdateIncidentInput{Status: StatusIdentified, Body: "Root cause found"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	s.SetClock(func() time.Time { return t0.Add(2 * time.Minute) })
	if _, err := s.AppendUpdate(ctx, inc.ID, UpdateIncidentInput{Status: StatusResolved, Body: "Mitigated"}); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	got, err := s.GetIncident(ctx, inc.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != StatusResolved {
		t.Errorf("Status = %q, want resolved", got.Status)
	}
	if got.ResolvedAt == nil {
		t.Error("ResolvedAt not stamped")
	}
	// 1 seed + 2 appended.
	if len(got.Updates) != 3 {
		t.Errorf("updates = %d, want 3", len(got.Updates))
	}
	// Updates are sorted newest first.
	if got.Updates[0].Status != StatusResolved {
		t.Errorf("newest update status = %q", got.Updates[0].Status)
	}
}

func TestInMemory_AppendUpdate_Validation(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	inc, _ := s.CreateIncident(ctx, CreateIncidentInput{Title: "X"})

	if _, err := s.AppendUpdate(ctx, inc.ID, UpdateIncidentInput{Status: "bogus", Body: "x"}); err == nil {
		t.Error("invalid status accepted")
	}
	if _, err := s.AppendUpdate(ctx, inc.ID, UpdateIncidentInput{Status: StatusIdentified, Body: " "}); err == nil {
		t.Error("empty body accepted")
	}
}

func TestInMemory_ListActiveExcludesResolved(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	t0 := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return t0 })

	a, _ := s.CreateIncident(ctx, CreateIncidentInput{Title: "A", Impact: ImpactMajor})
	s.SetClock(func() time.Time { return t0.Add(time.Hour) })
	b, _ := s.CreateIncident(ctx, CreateIncidentInput{Title: "B", Impact: ImpactCritical})
	_, _ = s.AppendUpdate(ctx, a.ID, UpdateIncidentInput{Status: StatusResolved, Body: "fixed"})

	active, err := s.ListActive(ctx)
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	if len(active) != 1 || active[0].ID != b.ID {
		t.Errorf("ListActive = %+v, want only B", active)
	}
}

func TestInMemory_ListRecent_HonoursWindow(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now.Add(-10 * 24 * time.Hour) })
	old, _ := s.CreateIncident(ctx, CreateIncidentInput{Title: "OLD"})
	s.SetClock(func() time.Time { return now.Add(-2 * 24 * time.Hour) })
	recent, _ := s.CreateIncident(ctx, CreateIncidentInput{Title: "RECENT"})
	s.SetClock(func() time.Time { return now })

	got, err := s.ListRecent(ctx, 7*24*time.Hour)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(got) != 1 || got[0].ID != recent.ID {
		t.Errorf("ListRecent = %+v, want only RECENT (old=%v)", got, old.ID)
	}
}

func TestInMemory_UpsertSubscription_IdempotentAndValidates(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	a, err := s.UpsertSubscription(ctx, SubscribeInput{Email: "alice@example.com"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	b, err := s.UpsertSubscription(ctx, SubscribeInput{Email: "ALICE@example.com"})
	if err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	if a.ID != b.ID {
		t.Errorf("idempotency broken: %v != %v", a.ID, b.ID)
	}
	if _, err := s.UpsertSubscription(ctx, SubscribeInput{Email: "not-an-email"}); err == nil {
		t.Error("invalid email accepted")
	}
}

func TestInMemory_UptimeForService_FillsGaps(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	now := time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC)
	s.SetClock(func() time.Time { return now })

	// Record 2 days of the 90-day window.
	if err := s.RecordSample(ctx, UptimeSample{Service: "proxy-gateway", Day: "2026-05-19", State: "op", SLIPct: 99.99}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := s.RecordSample(ctx, UptimeSample{Service: "proxy-gateway", Day: "2026-05-18", State: "deg", SLIPct: 95.0}); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	got, err := s.UptimeForService(ctx, "proxy-gateway", 90)
	if err != nil {
		t.Fatalf("UptimeForService: %v", err)
	}
	if len(got) != 90 {
		t.Errorf("want 90 entries, got %d", len(got))
	}
	if got[len(got)-1].Day != "2026-05-19" || got[len(got)-1].State != "op" {
		t.Errorf("today entry wrong: %+v", got[len(got)-1])
	}
	if got[len(got)-2].Day != "2026-05-18" || got[len(got)-2].State != "deg" {
		t.Errorf("yesterday entry wrong: %+v", got[len(got)-2])
	}
	// A day far back should be a gap.
	if got[0].State != "" {
		t.Errorf("oldest entry should be empty gap, got %+v", got[0])
	}
}
