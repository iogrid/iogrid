package store

import (
	"context"
	"testing"
	"time"
)

func TestMemStore_PairingTokenLifecycle(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()

	tok, err := s.IssuePairingToken(ctx, "owner-1", time.Minute)
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
	if got.OwnerUserID != "owner-1" {
		t.Fatalf("wrong owner: %q", got.OwnerUserID)
	}
	if _, err := s.ConsumePairingToken(ctx, tok); err == nil {
		t.Fatalf("expected double-consume to fail")
	}
	if _, err := s.ConsumePairingToken(ctx, "does-not-exist"); err == nil {
		t.Fatalf("expected unknown-token failure")
	}
}

func TestMemStore_PairingTokenExpiry(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	tok, err := s.IssuePairingToken(ctx, "owner", -time.Second)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := s.ConsumePairingToken(ctx, tok); err == nil {
		t.Fatalf("expected expired token to be rejected")
	}
}

func TestMemStore_ProviderCRUD(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()

	p := &Provider{
		OwnerUserID: "owner-1",
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

	got.DisplayName = "renamed"
	if err := s.UpdateProvider(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := s.GetProvider(ctx, p.ID)
	if after.DisplayName != "renamed" {
		t.Fatalf("update did not stick: %q", after.DisplayName)
	}

	if _, err := s.GetProvider(ctx, "nope"); err == nil {
		t.Fatalf("expected ErrNotFound")
	}
}

func TestMemStore_ListProvidersFilter(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	_ = s.CreateProvider(ctx, &Provider{OwnerUserID: "a", Status: StatusActive})
	_ = s.CreateProvider(ctx, &Provider{OwnerUserID: "a", Status: StatusOffline})
	_ = s.CreateProvider(ctx, &Provider{OwnerUserID: "b", Status: StatusActive})

	all, _, _ := s.ListProviders(ctx, ListOptions{})
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}
	bo, _, _ := s.ListProviders(ctx, ListOptions{OwnerUserID: "a"})
	if len(bo) != 2 {
		t.Fatalf("expected 2, got %d", len(bo))
	}
	off, _, _ := s.ListProviders(ctx, ListOptions{Status: StatusOffline})
	if len(off) != 1 {
		t.Fatalf("expected 1, got %d", len(off))
	}
}

func TestMemStore_SchedulingConfigDefaults(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	cfg, err := s.GetSchedulingConfig(ctx, "p1")
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

func TestMemStore_AuditAppendListSubscribe(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()

	sub, cancel := s.SubscribeAuditEvents("p1")
	defer cancel()

	for i := 0; i < 3; i++ {
		_ = s.AppendAuditEvent(ctx, AuditEvent{ProviderID: "p1", Kind: "EVENT_KIND_WORKLOAD_DISPATCHED"})
	}
	_ = s.AppendAuditEvent(ctx, AuditEvent{ProviderID: "other", Kind: "x"})

	events, _, err := s.ListAuditEvents(ctx, "p1", AuditQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("expected 3 events for p1, got %d", len(events))
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

func TestMemStore_EarningsSummary(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()

	now := time.Now()
	_ = s.CreditEarnings(ctx, EarningsEntry{ProviderID: "p1", WorkloadType: "bandwidth", OccurredAt: now, Currency: "GRID", Micros: 100})
	_ = s.CreditEarnings(ctx, EarningsEntry{ProviderID: "p1", WorkloadType: "docker", OccurredAt: now, Currency: "GRID", Micros: 250})
	_ = s.CreditEarnings(ctx, EarningsEntry{ProviderID: "p2", WorkloadType: "bandwidth", OccurredAt: now, Currency: "GRID", Micros: 999})

	total, byType, currency, err := s.SumEarnings(ctx, "p1", time.Time{}, time.Time{})
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
}

// TestMemStore_EarningsSummary_EmptyProvider asserts that a provider
// with zero credited entries still gets the Phase-0 native ledger
// currency back from SumEarnings — NOT "USD". This is what makes the
// /provide/earnings headline render "0 $GRID" instead of "$0.00" or
// "—" (#312).
func TestMemStore_EarningsSummary_EmptyProvider(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	total, byType, currency, err := s.SumEarnings(ctx, "no-such-provider", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if total != 0 {
		t.Fatalf("total: got %d want 0", total)
	}
	if len(byType) != 0 {
		t.Fatalf("byType should be empty, got %+v", byType)
	}
	if currency != "GRID" {
		t.Fatalf("currency: got %q want GRID", currency)
	}
}

// TestMemStore_CreditEarnings_DefaultsGrid asserts the credit-time
// default matches the pgStore — an entry with Currency:"" gets stored
// as "GRID", not "USD" (#312). Keeps the in-memory test backend
// faithful to the production Postgres backend.
func TestMemStore_CreditEarnings_DefaultsGrid(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	_ = s.CreditEarnings(ctx, EarningsEntry{ProviderID: "p1", WorkloadType: "bandwidth", OccurredAt: time.Now(), Currency: "", Micros: 42})
	_, _, currency, err := s.SumEarnings(ctx, "p1", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("sum: %v", err)
	}
	if currency != "GRID" {
		t.Fatalf("expected empty-Currency credit to default to GRID, got %q", currency)
	}
}
