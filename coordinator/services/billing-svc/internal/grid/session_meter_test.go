package grid

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestComputeShares_Splits85_15(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		consumed          uint64
		wantProvider      uint64
		wantIogrid        uint64
	}{
		{"zero", 0, 0, 0},
		{"100", 100, 85, 15},
		{"one_grid_atomic", 1_000_000_000, 850_000_000, 150_000_000},
		{"odd_99", 99, 84, 15}, // 99*85/100=84; residual 99-84=15
		{"odd_101", 101, 85, 16}, // 101*85/100=85; residual=16
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, i := ComputeShares(c.consumed)
			if p != c.wantProvider || i != c.wantIogrid {
				t.Fatalf("ComputeShares(%d) = (%d, %d), want (%d, %d)",
					c.consumed, p, i, c.wantProvider, c.wantIogrid)
			}
			if p+i != c.consumed {
				t.Fatalf("provider+iogrid must equal consumed: %d+%d=%d != %d",
					p, i, p+i, c.consumed)
			}
		})
	}
}

func TestComputeRefund_EdgeCases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		escrowed, consumed, want uint64
	}{
		{"unused_full_refund", 1_000_000, 0, 1_000_000},
		{"partial", 1_000_000, 200_000, 800_000},
		{"exactly_zero", 1_000_000, 1_000_000, 0},
		{"overconsumed_clamps_to_zero", 1_000_000, 1_500_000, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ComputeRefund(c.escrowed, c.consumed); got != c.want {
				t.Fatalf("ComputeRefund(%d,%d)=%d want=%d", c.escrowed, c.consumed, got, c.want)
			}
		})
	}
}

// ── SessionMeter.Settle ────────────────────────────────────────────

type fakeMeterStore struct {
	mu      sync.Mutex
	rows    map[uuid.UUID]*Settlement
}

func newFakeMeterStore() *fakeMeterStore {
	return &fakeMeterStore{rows: map[uuid.UUID]*Settlement{}}
}
func (f *fakeMeterStore) InsertSettlement(ctx context.Context, s *Settlement) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[s.SessionID]; ok {
		return nil // idempotent
	}
	clone := *s
	f.rows[s.SessionID] = &clone
	return nil
}
func (f *fakeMeterStore) GetSettlementBySession(ctx context.Context, sid uuid.UUID) (*Settlement, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.rows[sid]; ok {
		clone := *s
		return &clone, nil
	}
	return nil, errors.New("not found")
}

type fakeMetrics struct {
	mu        sync.Mutex
	consumed  uint64
	providerQ uint64
	iogridC   uint64
}

func (f *fakeMetrics) RecordConsumed(a uint64)              { f.mu.Lock(); f.consumed += a; f.mu.Unlock() }
func (f *fakeMetrics) RecordProviderPayoutQueued(a uint64)  { f.mu.Lock(); f.providerQ += a; f.mu.Unlock() }
func (f *fakeMetrics) RecordIogridCommission(a uint64)      { f.mu.Lock(); f.iogridC += a; f.mu.Unlock() }

func TestSessionMeter_Settle(t *testing.T) {
	t.Parallel()
	st := newFakeMeterStore()
	m := &SessionMeter{St: st, Metrics: &fakeMetrics{}}
	in := Input{
		SessionID:      uuid.New(),
		CustomerWallet: "Cw1",
		ProviderWallet: "Pw1",
		ProviderID:     uuid.New(),
		BytesIn:        500_000_000,
		BytesOut:       500_000_000,
		EscrowedAtomic: 5_000_000,
		ConsumedAtomic: 1_000_000, // exactly 1 GB worth
	}
	row, err := m.Settle(context.Background(), in)
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if row.ProviderShare != 850_000 {
		t.Fatalf("provider share: %d want 850_000", row.ProviderShare)
	}
	if row.IogridShare != 150_000 {
		t.Fatalf("iogrid share: %d want 150_000", row.IogridShare)
	}
	if row.RefundAtomic != 4_000_000 {
		t.Fatalf("refund: %d want 4_000_000", row.RefundAtomic)
	}
	// Idempotency: re-call returns the same row.
	row2, err := m.Settle(context.Background(), in)
	if err != nil {
		t.Fatalf("Settle (idempotent): %v", err)
	}
	if row2.ID != row.ID {
		t.Fatalf("expected idempotent re-call to return same row id")
	}
}

func TestSessionMeter_Settle_RejectsMissingFields(t *testing.T) {
	t.Parallel()
	m := &SessionMeter{St: newFakeMeterStore(), Metrics: &fakeMetrics{}}
	if _, err := m.Settle(context.Background(), Input{CustomerWallet: "Cw"}); err == nil {
		t.Fatal("expected error for missing session_id")
	}
	if _, err := m.Settle(context.Background(), Input{SessionID: uuid.New()}); err == nil {
		t.Fatal("expected error for missing customer wallet")
	}
}

func TestSessionMeter_Settle_ZeroConsumptionStillPersists(t *testing.T) {
	t.Parallel()
	st := newFakeMeterStore()
	m := &SessionMeter{St: st, Metrics: &fakeMetrics{}}
	in := Input{
		SessionID:      uuid.New(),
		CustomerWallet: "Cw",
		EscrowedAtomic: 1_000_000,
		ConsumedAtomic: 0,
	}
	row, err := m.Settle(context.Background(), in)
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if row.ProviderShare != 0 || row.IogridShare != 0 || row.RefundAtomic != 1_000_000 {
		t.Fatalf("zero-consumption shape wrong: %+v", row)
	}
}
