package handlers

import (
	"context"
	"testing"
	"time"

	"connectrpc.com/connect"

	commonv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/common/v1"
	providersv1 "github.com/iogrid/iogrid/coordinator/internal/pb/iogrid/providers/v1"
	"github.com/iogrid/iogrid/coordinator/services/providers-svc/internal/store"
)

func TestListAuditEvents_FilterByKind(t *testing.T) {
	s := store.NewInMemory()
	ctx := context.Background()
	_ = s.AppendAuditEvent(ctx, store.AuditEvent{ProviderID: "p1", Kind: "EVENT_KIND_WORKLOAD_DISPATCHED"})
	_ = s.AppendAuditEvent(ctx, store.AuditEvent{ProviderID: "p1", Kind: "EVENT_KIND_WORKLOAD_BLOCKED"})
	_ = s.AppendAuditEvent(ctx, store.AuditEvent{ProviderID: "p1", Kind: "EVENT_KIND_WORKLOAD_DISPATCHED"})

	h := NewDashboardHandler(s, nil)
	resp, err := h.ListAuditEvents(ctx, connect.NewRequest(&providersv1.ListAuditEventsRequest{
		ProviderId:  &commonv1.UUID{Value: "p1"},
		KindFilter:  []providersv1.EventKind{providersv1.EventKind_EVENT_KIND_WORKLOAD_DISPATCHED},
	}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(resp.Msg.Events) != 2 {
		t.Fatalf("expected 2 dispatched events, got %d", len(resp.Msg.Events))
	}
}

// TestGetEarningsSummary_Empty is the regression test for #312: when a
// caller's provider has zero earnings_entries (the Phase-0 zero-workload
// state), the headline Money must carry currency="GRID" (not "USD") so
// the web layer renders "0 $GRID" — not "$0.00" and not "—" (which is
// what proto3 zero-omission + Intl.NumberFormat("USD") would produce).
func TestGetEarningsSummary_Empty(t *testing.T) {
	s := store.NewInMemory()
	h := NewDashboardHandler(s, nil)
	resp, err := h.GetEarningsSummary(context.Background(), connect.NewRequest(&providersv1.GetEarningsSummaryRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.Msg.Summary.TotalEarned == nil {
		t.Fatal("TotalEarned must be set even on empty state — frontend reads currencyCode off it")
	}
	if resp.Msg.Summary.TotalEarned.Micros != 0 {
		t.Fatalf("expected 0 micros")
	}
	if resp.Msg.Summary.TotalEarned.Currency != "GRID" {
		t.Fatalf("expected GRID default (Phase-0 native ledger currency), got %q", resp.Msg.Summary.TotalEarned.Currency)
	}
}

func TestGetEarningsSummary_Breakdown(t *testing.T) {
	s := store.NewInMemory()
	ctx := context.Background()
	now := time.Now()
	_ = s.CreditEarnings(ctx, store.EarningsEntry{ProviderID: "p1", WorkloadType: "bandwidth", OccurredAt: now, Currency: "GRID", Micros: 100})
	_ = s.CreditEarnings(ctx, store.EarningsEntry{ProviderID: "p1", WorkloadType: "docker", OccurredAt: now, Currency: "GRID", Micros: 250})

	h := NewDashboardHandler(s, nil)
	resp, err := h.GetEarningsSummary(ctx, connect.NewRequest(&providersv1.GetEarningsSummaryRequest{
		ProviderId: &commonv1.UUID{Value: "p1"},
	}))
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.Msg.Summary.TotalEarned.Micros != 350 {
		t.Fatalf("total: got %d want 350", resp.Msg.Summary.TotalEarned.Micros)
	}
	if resp.Msg.Summary.TotalEarned.Currency != "GRID" {
		t.Fatalf("currency: got %q want GRID", resp.Msg.Summary.TotalEarned.Currency)
	}
	if resp.Msg.Summary.ByWorkloadType["bandwidth"].Micros != 100 {
		t.Fatalf("bandwidth: %v", resp.Msg.Summary.ByWorkloadType["bandwidth"])
	}
}

func TestListAuditEvents_MissingProviderID(t *testing.T) {
	h := NewDashboardHandler(store.NewInMemory(), nil)
	_, err := h.ListAuditEvents(context.Background(), connect.NewRequest(&providersv1.ListAuditEventsRequest{}))
	if err == nil {
		t.Fatalf("expected error")
	}
}
