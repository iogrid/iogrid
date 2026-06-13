//go:build integration

// Field-complete round-trip tests for the telemetry-svc incidents store the
// #726 audit verified CLEAN by inspection but left without a regression
// guard: incidents + incident_updates (the operator status-page timeline)
// and status_subscriptions (the email-notify registry).
//
// The bug class (#709 / #725 / #732): the in-memory impl keeps the whole
// struct, so unit tests are green, while the Postgres INSERT/SELECT silently
// drops a column — lost only in prod. These tests write a row populating
// EVERY caller-set field, read it back through the real getter the handlers
// use against a REAL Postgres, and assert each field survives. A future
// column added to the Incident / Update / Subscription structs but not to the
// SQL fails here.
//
// Fixture mirrors billing-svc/internal/store's money_roundtrip test:
// DATABASE_URL (default a local throwaway Postgres), skip-if-unreachable,
// wipe + re-apply the embedded incidents migrations per run. Point
// DATABASE_URL at a throwaway postgres:16 (testcontainers/podman); the
// fixture is destructive by design.
package incidents

import (
	"context"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

const defaultIncidentsDSN = "postgres://postgres:postgres@localhost:55435/telemetry_test?sslmode=disable"

func incidentsDSN() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return defaultIncidentsDSN
}

func newIncidentsFixture(t *testing.T) (*Postgres, func()) {
	t.Helper()
	dsn := incidentsDSN()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("pg pool create failed (DATABASE_URL=%q): %v", dsn, err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("pg ping failed (DATABASE_URL=%q): %v", dsn, err)
	}
	wipeCtx, wipeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer wipeCancel()
	if _, err := pool.Exec(wipeCtx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		pool.Close()
		t.Fatalf("wipe schema: %v", err)
	}
	// Apply is the package's own embedded-migration runner (Migrations FS).
	if err := Apply(context.Background(), dsn); err != nil {
		pool.Close()
		t.Fatalf("apply migrations: %v", err)
	}
	return NewPostgres(pool), func() { pool.Close() }
}

// TestIncident_RoundTrip locks every caller-set field of an incident (plus its
// seed update + a follow-up update) through CreateIncident → GetIncident. A
// dropped column (e.g. impact, affected_services, body) silently corrupts the
// public status page.
func TestIncident_RoundTrip(t *testing.T) {
	p, cleanup := newIncidentsFixture(t)
	defer cleanup()
	ctx := context.Background()

	in := CreateIncidentInput{
		Title:            "Edge latency elevated in eu-central-1",
		Body:             "Investigating elevated p99 on the proxy gateway.",
		Status:           StatusInvestigating,
		Impact:           ImpactMajor,
		AffectedServices: []string{"proxy-gateway", "vpn-gateway"},
	}
	created, err := p.CreateIncident(ctx, in)
	if err != nil {
		t.Fatalf("CreateIncident: %v", err)
	}
	if created.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("CreateIncident did not stamp ID")
	}
	if created.StartedAt.IsZero() || created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("CreateIncident did not stamp started_at/created_at/updated_at")
	}

	got, err := p.GetIncident(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetIncident: %v", err)
	}
	if got.Title != in.Title {
		t.Errorf("Title = %q, want %q", got.Title, in.Title)
	}
	if got.Body != in.Body {
		t.Errorf("Body = %q, want %q — the body column was dropped", got.Body, in.Body)
	}
	if got.Status != in.Status {
		t.Errorf("Status = %q, want %q", got.Status, in.Status)
	}
	if got.Impact != in.Impact {
		t.Errorf("Impact = %q, want %q — the impact column was dropped", got.Impact, in.Impact)
	}
	if !equalStrSet(got.AffectedServices, in.AffectedServices) {
		t.Errorf("AffectedServices = %v, want %v — the array column was dropped", got.AffectedServices, in.AffectedServices)
	}
	if got.ResolvedAt != nil {
		t.Errorf("non-resolved incident ResolvedAt = %v, want nil", got.ResolvedAt)
	}
	// A fresh incident carries exactly its seed update with the opening status.
	if len(got.Updates) != 1 {
		t.Fatalf("seed updates len = %d, want 1", len(got.Updates))
	}
	if got.Updates[0].Status != in.Status {
		t.Errorf("seed update Status = %q, want %q", got.Updates[0].Status, in.Status)
	}
	if got.Updates[0].IncidentID != created.ID {
		t.Errorf("seed update IncidentID = %v, want %v", got.Updates[0].IncidentID, created.ID)
	}
	if got.Updates[0].Body == "" || got.Updates[0].CreatedAt.IsZero() {
		t.Errorf("seed update dropped body/created_at: %+v", got.Updates[0])
	}

	// AppendUpdate must round-trip its body + status, and resolving must
	// stamp resolved_at on the parent (the lifecycle the status page draws).
	upd, err := p.AppendUpdate(ctx, created.ID, UpdateIncidentInput{
		Status: StatusResolved,
		Body:   "Root cause mitigated; p99 back within SLO.",
	})
	if err != nil {
		t.Fatalf("AppendUpdate: %v", err)
	}
	if upd.Body != "Root cause mitigated; p99 back within SLO." {
		t.Errorf("update Body = %q (dropped)", upd.Body)
	}
	resolved, err := p.GetIncident(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetIncident after resolve: %v", err)
	}
	if resolved.Status != StatusResolved {
		t.Errorf("Status after resolve = %q, want %q", resolved.Status, StatusResolved)
	}
	if resolved.ResolvedAt == nil {
		t.Error("ResolvedAt nil after resolving update — the status-page 'resolved' state was dropped")
	}
	if len(resolved.Updates) != 2 {
		t.Errorf("updates after append len = %d, want 2", len(resolved.Updates))
	}
}

// TestSubscription_RoundTrip locks every caller-set field of a
// status_subscriptions row through UpsertSubscription. services_filter and
// verify_token are the fields most at risk of a silent drop (the filter
// scopes which incidents the subscriber is emailed about; the token gates
// double-opt-in).
func TestSubscription_RoundTrip(t *testing.T) {
	p, cleanup := newIncidentsFixture(t)
	defer cleanup()
	ctx := context.Background()

	in := SubscribeInput{
		Email:          "status-rt@example.com",
		ServicesFilter: []string{"vpn-gateway", "build-gateway"},
	}
	got, err := p.UpsertSubscription(ctx, in)
	if err != nil {
		t.Fatalf("UpsertSubscription: %v", err)
	}
	if got.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Fatal("UpsertSubscription did not stamp ID")
	}
	if got.Email != in.Email {
		t.Errorf("Email = %q, want %q", got.Email, in.Email)
	}
	if got.Verified {
		t.Error("fresh subscription Verified = true, want false")
	}
	if got.VerifyToken == "" {
		t.Error("VerifyToken empty — the double-opt-in token was dropped")
	}
	if !equalStrSet(got.ServicesFilter, in.ServicesFilter) {
		t.Errorf("ServicesFilter = %v, want %v — the filter array was dropped", got.ServicesFilter, in.ServicesFilter)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt not stamped")
	}
	if got.VerifiedAt != nil || got.UnsubscribedAt != nil {
		t.Errorf("fresh subscription has verified_at/unsubscribed_at set: %v %v", got.VerifiedAt, got.UnsubscribedAt)
	}

	// Re-upsert with a different filter must update services_filter in place
	// (the ON CONFLICT path) — proving the column is written on update too.
	in2 := SubscribeInput{Email: in.Email, ServicesFilter: []string{"proxy-gateway"}}
	got2, err := p.UpsertSubscription(ctx, in2)
	if err != nil {
		t.Fatalf("UpsertSubscription (update): %v", err)
	}
	if got2.ID != got.ID {
		t.Errorf("upsert created a new row: id %v != %v", got2.ID, got.ID)
	}
	if !equalStrSet(got2.ServicesFilter, in2.ServicesFilter) {
		t.Errorf("ServicesFilter after update = %v, want %v", got2.ServicesFilter, in2.ServicesFilter)
	}
}

// TestUptimeSample_RoundTrip locks the (service, day, state, sli_pct) cell
// through RecordSample → UptimeForService.
func TestUptimeSample_RoundTrip(t *testing.T) {
	p, cleanup := newIncidentsFixture(t)
	defer cleanup()
	ctx := context.Background()

	today := time.Now().UTC().Format("2006-01-02")
	want := UptimeSample{Service: "vpn-gateway", Day: today, State: "deg", SLIPct: 97.5}
	if err := p.RecordSample(ctx, want); err != nil {
		t.Fatalf("RecordSample: %v", err)
	}
	// UptimeForService fills a contiguous day range; find today's cell.
	cells, err := p.UptimeForService(ctx, want.Service, 7)
	if err != nil {
		t.Fatalf("UptimeForService: %v", err)
	}
	var todayCell *UptimeSample
	for i := range cells {
		if cells[i].Day == today {
			todayCell = &cells[i]
			break
		}
	}
	if todayCell == nil {
		t.Fatalf("today's cell (%s) not in range %d days", today, 7)
	}
	if todayCell.Service != want.Service {
		t.Errorf("Service = %q, want %q", todayCell.Service, want.Service)
	}
	if todayCell.State != want.State {
		t.Errorf("State = %q, want %q — the state column was dropped", todayCell.State, want.State)
	}
	if todayCell.SLIPct != want.SLIPct {
		t.Errorf("SLIPct = %v, want %v — the sli_pct column was dropped", todayCell.SLIPct, want.SLIPct)
	}

	// Re-record must update state + sli_pct in place (ON CONFLICT path).
	want2 := UptimeSample{Service: want.Service, Day: today, State: "down", SLIPct: 41.0}
	if err := p.RecordSample(ctx, want2); err != nil {
		t.Fatalf("RecordSample (update): %v", err)
	}
	cells2, err := p.UptimeForService(ctx, want.Service, 7)
	if err != nil {
		t.Fatalf("UptimeForService (after update): %v", err)
	}
	for _, c := range cells2 {
		if c.Day == today {
			if c.State != "down" || c.SLIPct != 41.0 {
				t.Errorf("after update cell = (%q, %v), want (down, 41)", c.State, c.SLIPct)
			}
		}
	}
}

// equalStrSet compares two string slices as sets (order-independent), so a
// Postgres array round-trip that reorders elements doesn't false-fail.
func equalStrSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	ac := append([]string(nil), a...)
	bc := append([]string(nil), b...)
	sort.Strings(ac)
	sort.Strings(bc)
	for i := range ac {
		if ac[i] != bc[i] {
			return false
		}
	}
	return true
}
