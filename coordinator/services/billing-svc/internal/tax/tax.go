// Package tax generates quarterly tax-report PDFs for providers.
//
// US providers earning >$600/year in cash payouts receive a 1099-NEC.
// US providers earning $GRID receive a 1099-equivalent PDF stating the
// USD fair-value at receipt (computed from the SolanaPayout rows that
// recorded the swap-time USD value).
//
// PDF generation uses github.com/jung-kurt/gofpdf. The PDF is a simple
// rendering — final IRS-compliant forms are produced by the partner
// tax provider (TaxBandits / Track1099) from CSV exports. The PDF here
// is the provider's personal record.
package tax

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jung-kurt/gofpdf"

	"github.com/iogrid/iogrid/coordinator/services/billing-svc/internal/store"
)

// Threshold: US 1099-NEC kicks in at $600 / year in non-employee comp.
const ThresholdCents = 60000

// FormType constants.
const (
	Form1099NEC   = "1099-NEC"
	FormGRID1099E = "GRID-1099-equiv"
)

// Generator owns the store handle.
type Generator struct {
	store *store.Store
}

// New constructs a Generator.
func New(st *store.Store) *Generator { return &Generator{store: st} }

// Period describes a quarter.
type Period struct {
	Year    int
	Quarter int // 1..4
}

// Bounds returns the (start, end) timestamps for the quarter.
func (p Period) Bounds() (time.Time, time.Time) {
	if p.Quarter < 1 || p.Quarter > 4 {
		return time.Time{}, time.Time{}
	}
	startMonth := time.Month((p.Quarter-1)*3 + 1)
	start := time.Date(p.Year, startMonth, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 3, 0)
	return start, end
}

// CashEarnings is the aggregate cash income for one provider in a period.
type CashEarnings struct {
	UserID    uuid.UUID
	CashCents int64
}

// TokenEarnings is the aggregate $GRID income (USD fair-value at receipt).
type TokenEarnings struct {
	UserID     uuid.UUID
	TokenCents int64
}

// Generate1099NEC renders a 1099-NEC PDF for a single provider's quarter.
// Returns the PDF bytes — the caller persists via store.UpsertTaxReport.
func (g *Generator) Generate1099NEC(userID uuid.UUID, period Period, cashCents int64) ([]byte, error) {
	if cashCents < ThresholdCents {
		// Below threshold — no 1099 required by IRS, but we still emit
		// a personal-record version so providers have a single source
		// of truth.
	}
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("1099-NEC — iogrid Provider Earnings", false)
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 16)
	pdf.Cell(0, 10, "Form 1099-NEC — Nonemployee Compensation")
	pdf.Ln(12)

	pdf.SetFont("Helvetica", "", 10)
	pdf.Cell(0, 6, fmt.Sprintf("Tax year: %d   Quarter: Q%d", period.Year, period.Quarter))
	pdf.Ln(6)
	start, end := period.Bounds()
	pdf.Cell(0, 6, fmt.Sprintf("Period: %s — %s",
		start.Format("2006-01-02"), end.AddDate(0, 0, -1).Format("2006-01-02")))
	pdf.Ln(6)
	pdf.Cell(0, 6, fmt.Sprintf("Recipient (provider id): %s", userID.String()))
	pdf.Ln(10)

	pdf.SetFont("Helvetica", "B", 12)
	pdf.Cell(0, 8, "Box 1 — Nonemployee compensation (cash payouts)")
	pdf.Ln(8)
	pdf.SetFont("Helvetica", "", 11)
	pdf.Cell(0, 8, fmt.Sprintf("USD %.2f", float64(cashCents)/100))
	pdf.Ln(12)

	pdf.SetFont("Helvetica", "I", 9)
	pdf.MultiCell(0, 5,
		"This document is a personal record produced by iogrid. The "+
			"IRS-filed 1099-NEC is transmitted by our tax-services partner "+
			"(TaxBandits / Track1099). Annual cash earnings under $600 are "+
			"not reported to the IRS; you remain responsible for declaring "+
			"all income.",
		"", "L", false)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GenerateGRID1099Equivalent renders a 1099-equivalent PDF for $GRID earnings.
// The USD column uses fair-value at receipt (the SolanaPayout's USDValueCents).
func (g *Generator) GenerateGRID1099Equivalent(userID uuid.UUID, period Period, tokenCents int64) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetTitle("$GRID 1099 Equivalent — iogrid Provider Earnings", false)
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 16)
	pdf.Cell(0, 10, "$GRID Earnings Statement (1099 equivalent)")
	pdf.Ln(12)

	pdf.SetFont("Helvetica", "", 10)
	pdf.Cell(0, 6, fmt.Sprintf("Tax year: %d   Quarter: Q%d", period.Year, period.Quarter))
	pdf.Ln(6)
	start, end := period.Bounds()
	pdf.Cell(0, 6, fmt.Sprintf("Period: %s — %s",
		start.Format("2006-01-02"), end.AddDate(0, 0, -1).Format("2006-01-02")))
	pdf.Ln(6)
	pdf.Cell(0, 6, fmt.Sprintf("Recipient (provider id): %s", userID.String()))
	pdf.Ln(10)

	pdf.SetFont("Helvetica", "B", 12)
	pdf.Cell(0, 8, "Total $GRID earned (USD fair-value at receipt)")
	pdf.Ln(8)
	pdf.SetFont("Helvetica", "", 11)
	pdf.Cell(0, 8, fmt.Sprintf("USD %.2f", float64(tokenCents)/100))
	pdf.Ln(12)

	pdf.SetFont("Helvetica", "I", 9)
	pdf.MultiCell(0, 5,
		"$GRID earnings are taxable as ordinary income at receipt under "+
			"current US guidance. Capital gains on subsequent disposal are "+
			"your responsibility. iogrid recommends a tax-professional review "+
			"of these figures before filing.",
		"", "L", false)

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GenerateAndPersist generates both 1099-NEC and $GRID-1099-equivalent
// reports for a provider in a quarter, stores them, and returns the rows.
//
// The CashCents and TokenCents are pulled from the store; the caller is
// the cron job that fires on the 1st of Jan / Apr / Jul / Oct.
func (g *Generator) GenerateAndPersist(ctx context.Context, userID uuid.UUID, period Period, cashCents, tokenCents int64) ([]store.TaxReport, error) {
	if g.store == nil {
		return nil, errors.New("store not configured")
	}
	out := []store.TaxReport{}
	if cashCents > 0 {
		bytesPDF, err := g.Generate1099NEC(userID, period, cashCents)
		if err != nil {
			return nil, err
		}
		row := store.TaxReport{
			UserID:    userID,
			TaxYear:   period.Year,
			Quarter:   period.Quarter,
			FormType:  Form1099NEC,
			CashCents: cashCents,
			PDFBytes:  bytesPDF,
		}
		if err := g.store.UpsertTaxReport(ctx, row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if tokenCents > 0 {
		bytesPDF, err := g.GenerateGRID1099Equivalent(userID, period, tokenCents)
		if err != nil {
			return nil, err
		}
		row := store.TaxReport{
			UserID:     userID,
			TaxYear:    period.Year,
			Quarter:    period.Quarter,
			FormType:   FormGRID1099E,
			TokenCents: tokenCents,
			PDFBytes:   bytesPDF,
		}
		if err := g.store.UpsertTaxReport(ctx, row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// QuarterlyTrigger returns the Period the cron should run for, given
// the current time. The convention is to run on the 1st of Jan/Apr/Jul/Oct
// for the PREVIOUS calendar quarter.
func QuarterlyTrigger(now time.Time) Period {
	prev := now.AddDate(0, -1, 0)
	q := (int(prev.Month())-1)/3 + 1
	return Period{Year: prev.Year(), Quarter: q}
}
