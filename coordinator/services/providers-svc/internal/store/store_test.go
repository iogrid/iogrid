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

// #311: heartbeat hot-path bumps last_seen_at without rewriting the rest
// of the row. Asserts the lean UPDATE actually moves the timestamp and
// leaves other columns alone.
func TestMemStore_UpdateLastSeen(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()

	p := &Provider{
		OwnerUserID: "owner-1",
		DisplayName: "hatice mbp",
		HostInfo:    HostInfo{Platform: PlatformMacOS},
	}
	if err := s.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	before, _ := s.GetProvider(ctx, p.ID)
	originalLastSeen := before.LastSeenAt
	originalDisplayName := before.DisplayName

	// Sleep a small slice so the timestamp comparison is unambiguous.
	target := originalLastSeen.Add(42 * time.Second)
	if err := s.UpdateLastSeen(ctx, p.ID, target); err != nil {
		t.Fatalf("update last_seen: %v", err)
	}

	after, err := s.GetProvider(ctx, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !after.LastSeenAt.Equal(target) {
		t.Fatalf("last_seen_at not updated: got %v want %v", after.LastSeenAt, target)
	}
	if after.DisplayName != originalDisplayName {
		t.Fatalf("UpdateLastSeen clobbered display_name: got %q want %q",
			after.DisplayName, originalDisplayName)
	}

	if err := s.UpdateLastSeen(ctx, "no-such-provider", target); err == nil {
		t.Fatalf("expected ErrNotFound for unknown id")
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

// --- #325 — per-owner primary provider ---------------------------------

// TestMemStore_CreateProvider_AutoPromotesFirst asserts the first row
// inserted for an owner is auto-marked is_primary=true.
func TestMemStore_CreateProvider_AutoPromotesFirst(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	p := &Provider{OwnerUserID: "owner-A", DisplayName: "first"}
	if err := s.CreateProvider(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !p.IsPrimary {
		t.Fatalf("first daemon should be auto-primary")
	}
	got, _ := s.GetProvider(ctx, p.ID)
	if !got.IsPrimary {
		t.Fatalf("persisted row should be primary")
	}
}

// TestMemStore_CreateProvider_SecondNotPrimary mirrors the
// PairDaemon-side contract: subsequent rows default to non-primary.
func TestMemStore_CreateProvider_SecondNotPrimary(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	_ = s.CreateProvider(ctx, &Provider{OwnerUserID: "owner-B", DisplayName: "first"})
	second := &Provider{OwnerUserID: "owner-B", DisplayName: "second"}
	_ = s.CreateProvider(ctx, second)
	if second.IsPrimary {
		t.Fatalf("second daemon must NOT be primary on insert")
	}
}

// TestMemStore_SetPrimaryProvider_AtomicSwap verifies the prior primary
// is cleared in the same call that promotes the new one.
func TestMemStore_SetPrimaryProvider_AtomicSwap(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	first := &Provider{OwnerUserID: "owner-C", DisplayName: "first"}
	second := &Provider{OwnerUserID: "owner-C", DisplayName: "second"}
	_ = s.CreateProvider(ctx, first)
	_ = s.CreateProvider(ctx, second)
	if !first.IsPrimary || second.IsPrimary {
		t.Fatalf("setup invariant: first primary, second not — got first=%v second=%v", first.IsPrimary, second.IsPrimary)
	}

	out, err := s.SetPrimaryProvider(ctx, "owner-C", second.ID)
	if err != nil {
		t.Fatalf("set primary: %v", err)
	}
	if out.ID != second.ID || !out.IsPrimary {
		t.Fatalf("returned row should be promoted second: %+v", out)
	}
	gotFirst, _ := s.GetProvider(ctx, first.ID)
	if gotFirst.IsPrimary {
		t.Fatalf("prior primary should be cleared")
	}
	gotSecond, _ := s.GetProvider(ctx, second.ID)
	if !gotSecond.IsPrimary {
		t.Fatalf("new primary should be set")
	}
}

// TestMemStore_SetPrimaryProvider_NotOwner asserts that an attempt to
// promote a provider not owned by the caller returns ErrNotFound (the
// handler turns this into PermissionDenied).
func TestMemStore_SetPrimaryProvider_NotOwner(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	p := &Provider{OwnerUserID: "owner-D"}
	_ = s.CreateProvider(ctx, p)
	if _, err := s.SetPrimaryProvider(ctx, "owner-E", p.ID); err == nil {
		t.Fatalf("expected error when caller doesn't own the provider")
	}
}

// TestMemStore_SelectProviderForOwner_PrimaryWins asserts is_primary=true
// always wins, regardless of registered_at/last_seen ordering.
func TestMemStore_SelectProviderForOwner_PrimaryWins(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	now := time.Now().UTC()
	first := &Provider{OwnerUserID: "owner-F", DisplayName: "first", LastSeenAt: now.Add(-time.Hour), RegisteredAt: now.Add(-time.Hour)}
	second := &Provider{OwnerUserID: "owner-F", DisplayName: "second", LastSeenAt: now, RegisteredAt: now}
	_ = s.CreateProvider(ctx, first)  // auto-primary
	_ = s.CreateProvider(ctx, second) // not primary even though more recent

	got, err := s.SelectProviderForOwner(ctx, "owner-F")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got == nil || got.ID != first.ID {
		t.Fatalf("primary should win, got %+v", got)
	}
}

// TestMemStore_SelectProviderForOwner_LastSeenTiebreaker asserts that
// when no provider is primary (legacy data shape pre-migration), the
// most-recently-heartbeated row wins.
func TestMemStore_SelectProviderForOwner_LastSeenTiebreaker(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	now := time.Now().UTC()
	// Force is_primary=false on both by inserting then clearing.
	p1 := &Provider{OwnerUserID: "owner-G", DisplayName: "p1", LastSeenAt: now.Add(-time.Hour), RegisteredAt: now.Add(-time.Hour)}
	p2 := &Provider{OwnerUserID: "owner-G", DisplayName: "p2", LastSeenAt: now, RegisteredAt: now.Add(-30 * time.Minute)}
	_ = s.CreateProvider(ctx, p1)
	_ = s.CreateProvider(ctx, p2)
	// Clear the auto-primary so we exercise pure timestamp ordering.
	p1.IsPrimary = false
	_ = s.UpdateProvider(ctx, p1)

	got, err := s.SelectProviderForOwner(ctx, "owner-G")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if got == nil || got.ID != p2.ID {
		t.Fatalf("most-recent last_seen should win, got %+v", got)
	}
}

// TestMemStore_SelectProviderForOwner_EmptyOwnerReturnsNil documents the
// (nil, nil) empty-state contract — caller renders the "Install daemon"
// CTA, not an error banner.
func TestMemStore_SelectProviderForOwner_EmptyOwnerReturnsNil(t *testing.T) {
	ctx := context.Background()
	s := NewInMemory()
	got, err := s.SelectProviderForOwner(ctx, "ghost-owner")
	if err != nil {
		t.Fatalf("expected nil error for empty owner, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty owner, got %+v", got)
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
