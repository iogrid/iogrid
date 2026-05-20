//go:build integration
// +build integration

// Integration tests for the Postgres-backed Store. Spins up a one-shot
// Postgres container via ory/dockertest (same harness as identity-svc),
// applies the embedded migrations, and exercises the critical paths:
//
//   - pairing token: issue → consume → double-consume rejected
//   - pairing token: expired token rejected
//   - provider CRUD: create → get → update → list with filters
//   - scheduling config: defaults when row missing, upsert round-trip
//   - audit events: append + list + in-process subscribe fan-out
//   - earnings: credit + sum/byType breakdown
//   - persistence: data survives reconnect (the bug this issue closes)
//
// Run via: go test -tags=integration ./internal/store/...
//
// Skips automatically when docker is unavailable so CI runs without it.
package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"

	pdb "github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/db"
)

// pgFixture brings up a one-shot Postgres container, applies migrations,
// and hands back the pool + dsn. Caller defers cleanup.
func pgFixture(t *testing.T) (*pgxpool.Pool, string, func()) {
	t.Helper()
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Skipf("dockertest pool unavailable: %v", err)
	}
	if err := pool.Client.Ping(); err != nil {
		t.Skipf("docker daemon unavailable: %v", err)
	}
	resource, err := pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "postgres",
		Tag:        "16-alpine",
		Env: []string{
			"POSTGRES_PASSWORD=secret",
			"POSTGRES_DB=providers",
			"listen_addresses='*'",
		},
	}, func(cfg *docker.HostConfig) {
		cfg.AutoRemove = true
		cfg.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		t.Fatalf("docker run postgres: %v", err)
	}
	_ = resource.Expire(120)

	dsn := fmt.Sprintf("postgres://postgres:secret@%s/providers?sslmode=disable", resource.GetHostPort("5432/tcp"))
	var pgxPool *pgxpool.Pool
	if err := pool.Retry(func() error {
		p, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			return err
		}
		if err := p.Ping(context.Background()); err != nil {
			p.Close()
			return err
		}
		pgxPool = p
		return nil
	}); err != nil {
		t.Fatalf("postgres ready: %v", err)
	}
	if err := pdb.Apply(context.Background(), dsn); err != nil {
		t.Fatalf("migrations: %v", err)
	}
	cleanup := func() {
		pgxPool.Close()
		_ = pool.Purge(resource)
	}
	return pgxPool, dsn, cleanup
}

func TestPgStore_PairingTokenLifecycle(t *testing.T) {
	pool, _, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()
	s := NewPostgres(pool)

	owner := uuid.NewString()
	tok, err := s.IssuePairingToken(ctx, owner, time.Minute)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if tok == "" {
		t.Fatalf("expected non-empty token")
	}
	got, err := s.ConsumePairingToken(ctx, tok)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got.OwnerUserID != owner {
		t.Fatalf("wrong owner: got %q want %q", got.OwnerUserID, owner)
	}
	if _, err := s.ConsumePairingToken(ctx, tok); err == nil {
		t.Fatalf("expected double-consume to fail")
	}
	if _, err := s.ConsumePairingToken(ctx, "does-not-exist"); err == nil {
		t.Fatalf("expected unknown-token failure")
	}
}

func TestPgStore_PairingTokenExpiry(t *testing.T) {
	pool, _, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()
	s := NewPostgres(pool)

	tok, err := s.IssuePairingToken(ctx, uuid.NewString(), -time.Second)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := s.ConsumePairingToken(ctx, tok); err == nil {
		t.Fatalf("expected expired token to be rejected")
	}
}

func TestPgStore_ProviderCRUD(t *testing.T) {
	pool, _, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()
	s := NewPostgres(pool)

	owner := uuid.NewString()
	p := &Provider{
		OwnerUserID: owner,
		DisplayName: "my mac",
		HostInfo:    HostInfo{Platform: PlatformMacOS, CPULogicalCores: 8},
		Capabilities: Capability{
			SupportedTypes:  []string{"bandwidth", "ios_build"},
			IOSBuildEnabled: true,
		},
	}
	if err := s.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == "" {
		t.Fatalf("expected ID to be assigned")
	}

	got, err := s.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.DisplayName != "my mac" {
		t.Fatalf("wrong display name %q", got.DisplayName)
	}
	if got.HostInfo.Platform != PlatformMacOS || got.HostInfo.CPULogicalCores != 8 {
		t.Fatalf("host info round-trip: %+v", got.HostInfo)
	}
	if len(got.Capabilities.SupportedTypes) != 2 || !got.Capabilities.IOSBuildEnabled {
		t.Fatalf("capabilities round-trip: %+v", got.Capabilities)
	}

	got.DisplayName = "renamed"
	if err := s.UpdateProvider(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := s.GetProvider(ctx, p.ID)
	if after.DisplayName != "renamed" {
		t.Fatalf("update did not stick: %q", after.DisplayName)
	}

	if _, err := s.GetProvider(ctx, uuid.NewString()); err == nil {
		t.Fatalf("expected ErrNotFound for unknown UUID")
	}
	if _, err := s.GetProvider(ctx, "not-a-uuid"); err == nil {
		t.Fatalf("expected ErrNotFound for malformed id")
	}
}

func TestPgStore_ListProvidersFilter(t *testing.T) {
	pool, _, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()
	s := NewPostgres(pool)

	ownerA := uuid.NewString()
	ownerB := uuid.NewString()
	_ = s.CreateProvider(ctx, &Provider{OwnerUserID: ownerA, Status: StatusActive, DisplayName: "a1"})
	_ = s.CreateProvider(ctx, &Provider{OwnerUserID: ownerA, Status: StatusOffline, DisplayName: "a2"})
	_ = s.CreateProvider(ctx, &Provider{OwnerUserID: ownerB, Status: StatusActive, DisplayName: "b1"})

	all, _, err := s.ListProviders(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}

	bo, _, _ := s.ListProviders(ctx, ListOptions{OwnerUserID: ownerA})
	if len(bo) != 2 {
		t.Fatalf("expected 2 for owner A, got %d", len(bo))
	}
	off, _, _ := s.ListProviders(ctx, ListOptions{Status: StatusOffline})
	if len(off) != 1 {
		t.Fatalf("expected 1 offline, got %d", len(off))
	}
}

func TestPgStore_SchedulingConfigDefaults(t *testing.T) {
	pool, _, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()
	s := NewPostgres(pool)

	pid := uuid.NewString()
	cfg, err := s.GetSchedulingConfig(ctx, pid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cfg.BandwidthCapGB != 50 || cfg.CPUCapPct != 30 || cfg.MemoryCapPct != 25 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if !cfg.IdleEnabled || cfg.IdleThresholdSecs != 300 {
		t.Fatalf("expected idle defaults: %+v", cfg)
	}
	if len(cfg.AllowedCategories) == 0 {
		t.Fatalf("expected default categories")
	}
}

func TestPgStore_SchedulingConfigUpsert(t *testing.T) {
	pool, _, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()
	s := NewPostgres(pool)

	pid := uuid.NewString()
	updatedBy := uuid.NewString()
	cfg := &SchedulingConfig{
		ProviderID:           pid,
		BandwidthCapGB:       99,
		CPUCapPct:            70,
		MemoryCapPct:         60,
		GPUCapWhenIdlePct:    80,
		GPUCapWhenActivePct:  10,
		IdleEnabled:          false,
		IdleThresholdSecs:    600,
		AllowedCategories:    []string{"e_commerce", "seo"},
		DisallowedCategories: []string{"adult"},
		DestinationBlocklist: []string{"evil.com"},
		PerCustomerMinutesCap: 30,
		UpdatedByUserID:      updatedBy,
		CalendarWindows: []CalendarWindow{
			{DaysOfWeek: []uint32{1, 2, 3}, StartLocal: "09:00", EndLocal: "17:00", Timezone: "America/Los_Angeles"},
		},
	}
	if _, err := s.UpdateSchedulingConfig(ctx, cfg); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	got, err := s.GetSchedulingConfig(ctx, pid)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.BandwidthCapGB != 99 || got.IdleEnabled || len(got.CalendarWindows) != 1 {
		t.Fatalf("upsert round-trip mismatch: %+v", got)
	}
	if got.CalendarWindows[0].StartLocal != "09:00" {
		t.Fatalf("calendar window not preserved: %+v", got.CalendarWindows)
	}

	// Update again — verify the upsert overwrites.
	cfg.BandwidthCapGB = 200
	if _, err := s.UpdateSchedulingConfig(ctx, cfg); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	got2, _ := s.GetSchedulingConfig(ctx, pid)
	if got2.BandwidthCapGB != 200 {
		t.Fatalf("second upsert did not stick: %d", got2.BandwidthCapGB)
	}
}

func TestPgStore_AuditAppendListSubscribe(t *testing.T) {
	pool, _, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()
	s := NewPostgres(pool)

	pid := uuid.NewString()

	sub, cancelSub := s.SubscribeAuditEvents(pid)
	defer cancelSub()

	for i := 0; i < 3; i++ {
		_ = s.AppendAuditEvent(ctx, AuditEvent{ProviderID: pid, Kind: "EVENT_KIND_WORKLOAD_DISPATCHED"})
	}
	_ = s.AppendAuditEvent(ctx, AuditEvent{ProviderID: uuid.NewString(), Kind: "x"})

	events, _, err := s.ListAuditEvents(ctx, pid, AuditQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events for pid, got %d", len(events))
	}

	got := 0
	timeout := time.After(2 * time.Second)
	for got < 3 {
		select {
		case <-sub:
			got++
		case <-timeout:
			t.Fatalf("only received %d/3 events via subscription", got)
		}
	}
}

func TestPgStore_EarningsSummary(t *testing.T) {
	pool, _, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()
	s := NewPostgres(pool)

	p1 := uuid.NewString()
	p2 := uuid.NewString()
	now := time.Now().UTC()
	_ = s.CreditEarnings(ctx, EarningsEntry{ProviderID: p1, WorkloadType: "bandwidth", OccurredAt: now, Currency: "GRID", Micros: 100})
	_ = s.CreditEarnings(ctx, EarningsEntry{ProviderID: p1, WorkloadType: "docker", OccurredAt: now, Currency: "GRID", Micros: 250})
	_ = s.CreditEarnings(ctx, EarningsEntry{ProviderID: p2, WorkloadType: "bandwidth", OccurredAt: now, Currency: "GRID", Micros: 999})

	total, byType, currency, err := s.SumEarnings(ctx, p1, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if total != 350 {
		t.Fatalf("total: got %d want 350", total)
	}
	if byType["bandwidth"] != 100 || byType["docker"] != 250 {
		t.Fatalf("breakdown: %+v", byType)
	}
	if currency != "GRID" {
		t.Fatalf("currency: %q", currency)
	}

	// Empty-provider path: a provider with zero credited entries must
	// still get the Phase-0 native ledger currency (GRID) back, NOT the
	// legacy "USD" default — that's what makes /provide/earnings render
	// "0 $GRID" instead of "$0.00" / "—" (#312).
	noEarnings := uuid.NewString()
	total, byType, currency, err = s.SumEarnings(ctx, noEarnings, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("empty sum: %v", err)
	}
	if total != 0 || len(byType) != 0 {
		t.Fatalf("empty provider should have zero earnings, got total=%d byType=%+v", total, byType)
	}
	if currency != "GRID" {
		t.Fatalf("empty-provider currency: got %q want GRID", currency)
	}

	// Credit-time default: an entry with Currency:"" must be persisted
	// as "GRID" (not "USD"); regression for the future when workloads-svc
	// starts crediting earnings without explicitly setting currency.
	pDefault := uuid.NewString()
	_ = s.CreditEarnings(ctx, EarningsEntry{ProviderID: pDefault, WorkloadType: "gpu", OccurredAt: now, Currency: "", Micros: 7})
	_, _, currency, err = s.SumEarnings(ctx, pDefault, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("default sum: %v", err)
	}
	if currency != "GRID" {
		t.Fatalf("CreditEarnings empty-currency default: got %q want GRID", currency)
	}
}

// TestPgStore_SurvivesReconnect is the regression test for #242 — the whole
// reason the in-memory store had to go. Provider record persists across
// pool teardown, exactly like a pod restart.
func TestPgStore_SurvivesReconnect(t *testing.T) {
	_, dsn, cleanup := pgFixture(t)
	defer cleanup()
	ctx := context.Background()

	owner := uuid.NewString()
	p := &Provider{
		OwnerUserID: owner,
		DisplayName: "haticeyildiz-mac",
		HostInfo:    HostInfo{Platform: PlatformMacOS},
	}

	// Phase 1: write with a fresh pool.
	pool1, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool1: %v", err)
	}
	s1 := NewPostgres(pool1)
	if err := s1.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	pool1.Close()

	// Phase 2: simulate pod restart with a fresh pool and confirm the row is still there.
	pool2, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pool2: %v", err)
	}
	defer pool2.Close()
	s2 := NewPostgres(pool2)
	got, err := s2.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("get after reconnect: %v", err)
	}
	if got.DisplayName != "haticeyildiz-mac" {
		t.Fatalf("provider did not survive reconnect: %+v", got)
	}
}
