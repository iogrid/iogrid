// Package transparency generates the quarterly transparency reports
// that docs/LEGAL.md commits us to publish "Phase 2 onward". The
// generator runs as a Kubernetes CronJob on the first of January /
// April / July / October and writes:
//
//  1. A canonical JSON document to S3 at
//     s3://iogrid-transparency/{year}/Q{quarter}.json
//  2. A Markdown rendering at
//     s3://iogrid-transparency/{year}/Q{quarter}.md (the marketing
//     site fetches this for /transparency)
//  3. A POST to gateway-bff at /api/v1/transparency/publish so the
//     public endpoint /status/transparency/{year}/{quarter} can serve
//     the latest snapshot without going to S3 per request.
//
// The generator is intentionally pure-function with respect to its
// MetricsSource — production wires a NATS / Postgres / Prometheus
// adapter; unit tests pass a deterministic in-memory implementation.
package transparency

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Quarter is 1..4.
type Quarter int

// Window expands (year, quarter) into the [start, end) wall-clock pair
// the metrics source should aggregate over. End is exclusive.
func Window(year int, q Quarter) (start, end time.Time, err error) {
	if q < 1 || q > 4 {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid quarter %d (want 1..4)", q)
	}
	startMonth := time.Month(int(q-1)*3 + 1)
	start = time.Date(year, startMonth, 1, 0, 0, 0, 0, time.UTC)
	end = start.AddDate(0, 3, 0)
	return start, end, nil
}

// Categories of filter blocks. Stable slugs — published verbatim.
var Categories = []string{
	"csam_hash_match",
	"phishtank_listed",
	"openphish_listed",
	"google_safe_browsing_match",
	"banking_blocked",
	"government_blocked",
	"adult_blocked",
	"smtp_port_blocked",
	"irc_port_blocked",
	"tor_port_blocked",
	"rate_limited",
	"destination_blocked",
}

// Report is the canonical transparency-report payload. Marshals to
// stable JSON (ordered keys via the struct fields) and to Markdown via
// (*Report).Markdown.
type Report struct {
	Year             int                       `json:"year"`
	Quarter          Quarter                   `json:"quarter"`
	Window           ReportWindow              `json:"window"`
	GeneratedAt      time.Time                 `json:"generated_at"`
	TotalChecks      int64                     `json:"total_checks"`
	TotalBlocks      int64                     `json:"total_blocks"`
	BlockRate        float64                   `json:"block_rate"`
	BlocksByCategory map[string]int64          `json:"blocks_by_category"`
	BlocksByBackend  map[string]int64          `json:"blocks_by_backend"`
	HitRates         map[string]float64        `json:"hit_rates"`
	LawEnforcement   LawEnforcementBlock       `json:"law_enforcement"`
	AuditRetention   AuditRetentionBlock       `json:"audit_retention"`
	Methodology      string                    `json:"methodology"`
	Notes            []string                  `json:"notes,omitempty"`
}

// ReportWindow is the wall-clock range the report covers.
type ReportWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// LawEnforcementBlock counts inbound LE requests + outbound responses.
type LawEnforcementBlock struct {
	InquiriesReceived int               `json:"inquiries_received"`
	ResponsesSent     int               `json:"responses_sent"`
	ByJurisdiction    map[string]int    `json:"by_jurisdiction,omitempty"`
	ByRequestType     map[string]int    `json:"by_request_type,omitempty"` // subpoena / MLAT / warrant / informal
	NDAReceived       int               `json:"nda_received,omitempty"`
}

// AuditRetentionBlock summarises the 90-day retention compliance.
type AuditRetentionBlock struct {
	RequiredDays      int       `json:"required_days"`
	ConfiguredDays    int       `json:"configured_days"`
	OldestRecord      time.Time `json:"oldest_record"`
	LastPruneAt       time.Time `json:"last_prune_at"`
	LastPruneDeleted  int64     `json:"last_prune_deleted"`
	Compliant         bool      `json:"compliant"`
	CompliantRationale string   `json:"compliant_rationale,omitempty"`
}

// MetricsSource abstracts the underlying telemetry store. Production
// wires a NATS-JetStream consumer that aggregates audit events;
// dev / tests use the In-memory implementation.
type MetricsSource interface {
	TotalChecks(ctx context.Context, start, end time.Time) (int64, error)
	BlocksByCategory(ctx context.Context, start, end time.Time) (map[string]int64, error)
	BlocksByBackend(ctx context.Context, start, end time.Time) (map[string]int64, error)
	ChecksByBackend(ctx context.Context, start, end time.Time) (map[string]int64, error)
	LawEnforcement(ctx context.Context, start, end time.Time) (LawEnforcementBlock, error)
	AuditRetention(ctx context.Context) (AuditRetentionBlock, error)
}

// Generator builds Reports. Constructed once; safe for concurrent use.
type Generator struct {
	Source      MetricsSource
	Methodology string
}

// NewGenerator returns a Generator bound to the given source.
func NewGenerator(src MetricsSource) *Generator {
	return &Generator{
		Source:      src,
		Methodology: defaultMethodology,
	}
}

// Generate builds the Report for (year, quarter). Returns a fully
// populated *Report with all per-category / per-backend maps
// initialised even when the source returns nil maps — downstream
// callers can iterate without nil-checks.
func (g *Generator) Generate(ctx context.Context, year int, quarter Quarter) (*Report, error) {
	if g.Source == nil {
		return nil, errors.New("transparency: MetricsSource not configured")
	}
	start, end, err := Window(year, quarter)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	report := &Report{
		Year:    year,
		Quarter: quarter,
		Window: ReportWindow{
			Start: start,
			End:   end,
		},
		GeneratedAt:      now,
		BlocksByCategory: map[string]int64{},
		BlocksByBackend:  map[string]int64{},
		HitRates:         map[string]float64{},
		Methodology:      g.Methodology,
	}

	checks, err := g.Source.TotalChecks(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("transparency: TotalChecks: %w", err)
	}
	report.TotalChecks = checks

	cats, err := g.Source.BlocksByCategory(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("transparency: BlocksByCategory: %w", err)
	}
	// Always seed every canonical category so the published shape is
	// stable across quarters (zero-rows visible).
	for _, k := range Categories {
		report.BlocksByCategory[k] = cats[k]
	}
	var totalBlocks int64
	for _, v := range cats {
		totalBlocks += v
	}
	report.TotalBlocks = totalBlocks
	if checks > 0 {
		report.BlockRate = float64(totalBlocks) / float64(checks)
	}

	byBackend, err := g.Source.BlocksByBackend(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("transparency: BlocksByBackend: %w", err)
	}
	for k, v := range byBackend {
		report.BlocksByBackend[k] = v
	}

	checksByBackend, err := g.Source.ChecksByBackend(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("transparency: ChecksByBackend: %w", err)
	}
	for k, total := range checksByBackend {
		if total <= 0 {
			report.HitRates[k] = 0
			continue
		}
		hits := byBackend[k]
		report.HitRates[k] = float64(hits) / float64(total)
	}

	le, err := g.Source.LawEnforcement(ctx, start, end)
	if err != nil {
		return nil, fmt.Errorf("transparency: LawEnforcement: %w", err)
	}
	report.LawEnforcement = le

	ar, err := g.Source.AuditRetention(ctx)
	if err != nil {
		return nil, fmt.Errorf("transparency: AuditRetention: %w", err)
	}
	report.AuditRetention = ar

	return report, nil
}

// JSON serialises the report as indented JSON. Stable across runs.
func (r *Report) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// Markdown renders the report as a human-friendly Markdown document
// suitable for publication on the marketing site.
func (r *Report) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# iogrid Transparency Report — Q%d %d\n\n", r.Quarter, r.Year)
	fmt.Fprintf(&b, "Window: `%s` → `%s` (UTC)\n\n",
		r.Window.Start.Format(time.RFC3339),
		r.Window.End.Format(time.RFC3339))
	fmt.Fprintf(&b, "Generated: `%s`\n\n", r.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&b, "## Filter activity\n\n")
	fmt.Fprintf(&b, "- Total checks performed: **%d**\n", r.TotalChecks)
	fmt.Fprintf(&b, "- Total blocks issued:   **%d**\n", r.TotalBlocks)
	fmt.Fprintf(&b, "- Aggregate block rate:  **%.4f%%**\n\n", r.BlockRate*100)

	if len(r.BlocksByCategory) > 0 {
		fmt.Fprintf(&b, "### Blocks by category\n\n| Category | Count |\n|---|--:|\n")
		keys := sortedKeys(r.BlocksByCategory)
		for _, k := range keys {
			fmt.Fprintf(&b, "| `%s` | %d |\n", k, r.BlocksByCategory[k])
		}
		b.WriteString("\n")
	}

	if len(r.HitRates) > 0 {
		fmt.Fprintf(&b, "### Backend hit-rates\n\n| Backend | Hit rate |\n|---|--:|\n")
		keys := sortedKeysFloat(r.HitRates)
		for _, k := range keys {
			fmt.Fprintf(&b, "| `%s` | %.4f%% |\n", k, r.HitRates[k]*100)
		}
		b.WriteString("\n")
	}

	fmt.Fprintf(&b, "## Law-enforcement engagement\n\n")
	fmt.Fprintf(&b, "- Inquiries received: **%d**\n", r.LawEnforcement.InquiriesReceived)
	fmt.Fprintf(&b, "- Responses sent:     **%d**\n", r.LawEnforcement.ResponsesSent)
	if len(r.LawEnforcement.ByJurisdiction) > 0 {
		fmt.Fprintf(&b, "\n#### By jurisdiction\n\n| Jurisdiction | Count |\n|---|--:|\n")
		// sort string keys
		keys := make([]string, 0, len(r.LawEnforcement.ByJurisdiction))
		for k := range r.LawEnforcement.ByJurisdiction {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "| %s | %d |\n", k, r.LawEnforcement.ByJurisdiction[k])
		}
	}
	if len(r.LawEnforcement.ByRequestType) > 0 {
		fmt.Fprintf(&b, "\n#### By request type\n\n| Type | Count |\n|---|--:|\n")
		keys := make([]string, 0, len(r.LawEnforcement.ByRequestType))
		for k := range r.LawEnforcement.ByRequestType {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "| %s | %d |\n", k, r.LawEnforcement.ByRequestType[k])
		}
	}
	if r.LawEnforcement.NDAReceived > 0 {
		fmt.Fprintf(&b, "\nRequests subject to non-disclosure orders: **%d** (counted, not detailed).\n", r.LawEnforcement.NDAReceived)
	}
	b.WriteString("\n")

	fmt.Fprintf(&b, "## Audit retention compliance\n\n")
	fmt.Fprintf(&b, "- Required retention: **%d days** (per `docs/LEGAL.md`)\n", r.AuditRetention.RequiredDays)
	fmt.Fprintf(&b, "- Configured retention: **%d days**\n", r.AuditRetention.ConfiguredDays)
	if !r.AuditRetention.OldestRecord.IsZero() {
		fmt.Fprintf(&b, "- Oldest record in store: `%s`\n", r.AuditRetention.OldestRecord.Format(time.RFC3339))
	}
	if !r.AuditRetention.LastPruneAt.IsZero() {
		fmt.Fprintf(&b, "- Last prune pass: `%s` (deleted %d rows)\n",
			r.AuditRetention.LastPruneAt.Format(time.RFC3339),
			r.AuditRetention.LastPruneDeleted)
	}
	fmt.Fprintf(&b, "- Compliant: **%v**", r.AuditRetention.Compliant)
	if r.AuditRetention.CompliantRationale != "" {
		fmt.Fprintf(&b, " — %s", r.AuditRetention.CompliantRationale)
	}
	b.WriteString("\n\n")

	fmt.Fprintf(&b, "## Methodology\n\n%s\n", r.Methodology)
	if len(r.Notes) > 0 {
		b.WriteString("\n## Notes\n\n")
		for _, n := range r.Notes {
			fmt.Fprintf(&b, "- %s\n", n)
		}
	}
	return b.String()
}

// sortedKeys returns map keys in deterministic order.
func sortedKeys(m map[string]int64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeysFloat(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// defaultMethodology is the canonical published rationale. Kept here
// (not in a config file) so the wording ships atomically with the
// generator code and any change is committed + reviewed.
const defaultMethodology = `Counts are derived from the NATS JetStream AUDIT stream of antiabuse-svc, ` +
	`which records every CheckURL / CheckDomain / CheckContainerImage call. ` +
	`Retention is 90 days per docs/LEGAL.md; per-event records are pruned ` +
	`thereafter and only aggregate counters survive into the report. ` +
	`Block totals are deduplicated per (customer_id, target, decision) inside ` +
	`a 60-second window so a single hostile request that re-enters the ` +
	`pipeline does not inflate the block rate. Law-enforcement inquiries ` +
	`include every inbound subpoena, MLAT, warrant, or informal request ` +
	`processed by counsel during the window. PhotoDNA / PhishTank / ` +
	`OpenPhish hit-rates are computed as (backend blocks) / (backend checks) ` +
	`over the same window.`
