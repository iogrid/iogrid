package tax

import (
	"bytes"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPeriodBounds(t *testing.T) {
	cases := []struct {
		year, q  int
		startStr string
		endStr   string
	}{
		{2026, 1, "2026-01-01T00:00:00Z", "2026-04-01T00:00:00Z"},
		{2026, 2, "2026-04-01T00:00:00Z", "2026-07-01T00:00:00Z"},
		{2026, 3, "2026-07-01T00:00:00Z", "2026-10-01T00:00:00Z"},
		{2026, 4, "2026-10-01T00:00:00Z", "2027-01-01T00:00:00Z"},
	}
	for _, c := range cases {
		p := Period{Year: c.year, Quarter: c.q}
		start, end := p.Bounds()
		wantS, _ := time.Parse(time.RFC3339, c.startStr)
		wantE, _ := time.Parse(time.RFC3339, c.endStr)
		if !start.Equal(wantS) || !end.Equal(wantE) {
			t.Errorf("Q%d: got %v..%v, want %v..%v",
				c.q, start, end, wantS, wantE)
		}
	}
}

func TestPeriodBoundsInvalidQuarter(t *testing.T) {
	p := Period{Year: 2026, Quarter: 5}
	s, e := p.Bounds()
	if !s.IsZero() || !e.IsZero() {
		t.Errorf("expected zero bounds for invalid quarter, got %v..%v", s, e)
	}
}

func TestGenerate1099NEC_RendersValidPDF(t *testing.T) {
	g := New(nil)
	pdf, err := g.Generate1099NEC(uuid.New(), Period{Year: 2026, Quarter: 1}, 123456)
	if err != nil {
		t.Fatalf("Generate1099NEC: %v", err)
	}
	if len(pdf) < 200 {
		t.Errorf("pdf seems empty, %d bytes", len(pdf))
	}
	// PDF magic header
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("not a PDF: %q", string(pdf[:5]))
	}
}

func TestGenerateGRID1099Equivalent_RendersValidPDF(t *testing.T) {
	g := New(nil)
	pdf, err := g.GenerateGRID1099Equivalent(uuid.New(), Period{Year: 2026, Quarter: 4}, 800000)
	if err != nil {
		t.Fatalf("GenerateGRID1099Equivalent: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("not a PDF: %q", string(pdf[:5]))
	}
}

func TestQuarterlyTrigger(t *testing.T) {
	cases := []struct {
		now string
		p   Period
	}{
		// 1st of April → previous month = March → Q1
		{"2026-04-01T00:00:00Z", Period{Year: 2026, Quarter: 1}},
		// 1st of July → June → Q2
		{"2026-07-01T00:00:00Z", Period{Year: 2026, Quarter: 2}},
		// 1st of January → December previous year → Q4
		{"2026-01-01T00:00:00Z", Period{Year: 2025, Quarter: 4}},
	}
	for _, c := range cases {
		t.Run(c.now, func(t *testing.T) {
			now, _ := time.Parse(time.RFC3339, c.now)
			got := QuarterlyTrigger(now)
			if got != c.p {
				t.Errorf("got %+v want %+v", got, c.p)
			}
		})
	}
}
