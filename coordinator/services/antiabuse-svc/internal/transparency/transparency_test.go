package transparency

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWindow_AllQuarters(t *testing.T) {
	cases := []struct {
		q             Quarter
		wantStartMon  time.Month
		wantEndYear   int
		wantEndMonth  time.Month
	}{
		{1, time.January, 2026, time.April},
		{2, time.April, 2026, time.July},
		{3, time.July, 2026, time.October},
		{4, time.October, 2027, time.January},
	}
	for _, c := range cases {
		start, end, err := Window(2026, c.q)
		if err != nil {
			t.Fatalf("Q%d: %v", c.q, err)
		}
		if start.Month() != c.wantStartMon || start.Year() != 2026 || start.Day() != 1 {
			t.Errorf("Q%d start = %v, want first of %v 2026", c.q, start, c.wantStartMon)
		}
		if end.Year() != c.wantEndYear || end.Month() != c.wantEndMonth || end.Day() != 1 {
			t.Errorf("Q%d end = %v, want first of %v %d", c.q, end, c.wantEndMonth, c.wantEndYear)
		}
	}
}

func TestWindow_BadQuarter(t *testing.T) {
	if _, _, err := Window(2026, 0); err == nil {
		t.Errorf("Window(0) must error")
	}
	if _, _, err := Window(2026, 5); err == nil {
		t.Errorf("Window(5) must error")
	}
}

func TestGenerate_ProducesStableReport(t *testing.T) {
	src := NewInMemory()
	src.SetChecks(1_000_000)
	src.AddCategory("csam_hash_match", 12)
	src.AddCategory("phishtank_listed", 250)
	src.AddCategory("destination_blocked", 100)
	src.AddBackendBlocks("ncmec_photodna", 12)
	src.AddBackendBlocks("phishtank", 250)
	src.SetBackendChecks("ncmec_photodna", 50_000)
	src.SetBackendChecks("phishtank", 200_000)
	src.SetLawEnforcement(LawEnforcementBlock{
		InquiriesReceived: 4,
		ResponsesSent:     4,
		ByJurisdiction: map[string]int{
			"US": 3,
			"DE": 1,
		},
		ByRequestType: map[string]int{
			"subpoena": 3,
			"mlat":     1,
		},
	})
	src.SetAuditRetention(AuditRetentionBlock{
		RequiredDays:   90,
		ConfiguredDays: 90,
		OldestRecord:   time.Now().Add(-89 * 24 * time.Hour),
		LastPruneAt:    time.Now().Add(-2 * time.Hour),
		Compliant:      true,
	})

	g := NewGenerator(src)
	r, err := g.Generate(context.Background(), 2026, 1)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if r.TotalChecks != 1_000_000 {
		t.Errorf("TotalChecks = %d, want 1_000_000", r.TotalChecks)
	}
	if r.TotalBlocks != 362 {
		t.Errorf("TotalBlocks = %d, want 362", r.TotalBlocks)
	}
	wantRate := 362.0 / 1_000_000.0
	if r.BlockRate != wantRate {
		t.Errorf("BlockRate = %v, want %v", r.BlockRate, wantRate)
	}
	for _, k := range Categories {
		if _, ok := r.BlocksByCategory[k]; !ok {
			t.Errorf("BlocksByCategory missing canonical key %q", k)
		}
	}
	if got := r.BlocksByCategory["csam_hash_match"]; got != 12 {
		t.Errorf("BlocksByCategory[csam_hash_match] = %d, want 12", got)
	}
	if got := r.HitRates["ncmec_photodna"]; got <= 0 {
		t.Errorf("HitRates[ncmec_photodna] = %v, want > 0", got)
	}
}

func TestGenerate_JSONIsParseable(t *testing.T) {
	src := NewInMemory()
	src.SetChecks(0)
	g := NewGenerator(src)
	r, err := g.Generate(context.Background(), 2026, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := r.JSON()
	if err != nil {
		t.Fatalf("JSON: %v", err)
	}
	var into map[string]any
	if err := json.Unmarshal(raw, &into); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if into["year"].(float64) != 2026 {
		t.Errorf("year not round-tripped")
	}
}

func TestGenerate_MarkdownHasExpectedSections(t *testing.T) {
	src := NewInMemory()
	src.SetChecks(100)
	src.AddCategory("csam_hash_match", 1)
	src.SetLawEnforcement(LawEnforcementBlock{InquiriesReceived: 2, ResponsesSent: 2})
	src.SetAuditRetention(AuditRetentionBlock{RequiredDays: 90, ConfiguredDays: 90, Compliant: true})

	g := NewGenerator(src)
	r, err := g.Generate(context.Background(), 2026, 2)
	if err != nil {
		t.Fatal(err)
	}
	md := r.Markdown()
	for _, must := range []string{
		"# iogrid Transparency Report — Q2 2026",
		"## Filter activity",
		"## Law-enforcement engagement",
		"## Audit retention compliance",
		"## Methodology",
		"csam_hash_match",
	} {
		if !strings.Contains(md, must) {
			t.Errorf("Markdown missing %q", must)
		}
	}
}

func TestGenerate_RequiresSource(t *testing.T) {
	g := &Generator{}
	if _, err := g.Generate(context.Background(), 2026, 1); err == nil {
		t.Errorf("expected error when MetricsSource is nil")
	}
}

func TestGenerate_PropagatesQuarterValidation(t *testing.T) {
	src := NewInMemory()
	g := NewGenerator(src)
	if _, err := g.Generate(context.Background(), 2026, 5); err == nil {
		t.Errorf("expected error for invalid quarter")
	}
}

func TestGenerate_ZeroChecks_NoNaNBlockRate(t *testing.T) {
	src := NewInMemory()
	// Checks left as 0
	src.AddCategory("phishtank_listed", 0)
	g := NewGenerator(src)
	r, err := g.Generate(context.Background(), 2026, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.BlockRate != 0 {
		t.Errorf("BlockRate must be 0 when TotalChecks=0, got %v", r.BlockRate)
	}
}
