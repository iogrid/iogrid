package grid

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// fakeBuildStore is an in-memory BuildStore for the meter's unit tests.
type fakeBuildStore struct {
	rows map[string]*BuildSettlement
}

func newFakeBuildStore() *fakeBuildStore {
	return &fakeBuildStore{rows: map[string]*BuildSettlement{}}
}

func key(buildID, attemptID uuid.UUID) string { return buildID.String() + ":" + attemptID.String() }

func (f *fakeBuildStore) InsertBuildSettlement(_ context.Context, s *BuildSettlement) error {
	f.rows[key(s.BuildID, s.AttemptID)] = s
	return nil
}

func (f *fakeBuildStore) GetBuildSettlement(_ context.Context, buildID, attemptID uuid.UUID) (*BuildSettlement, error) {
	return f.rows[key(buildID, attemptID)], nil
}

func TestBuildMeter_Settle_SplitIs85Provider(t *testing.T) {
	m := &BuildMeter{St: newFakeBuildStore()}
	in := BuildInput{
		BuildID:        uuid.New(),
		AttemptID:      uuid.New(),
		CustomerWallet: "Cust1111111111111111111111111111111111111111",
		ProviderID:     uuid.New(),
		EscrowedAtomic: 100_000_000_000, // 100 GRID escrowed
		ConsumedAtomic: 40_000_000_000,  // 40 GRID consumed by the build
	}
	row, err := m.Settle(context.Background(), in)
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	// Provider gets 85% of consumed, iogrid the residual 15%, sum == consumed.
	if row.ProviderShare != 34_000_000_000 {
		t.Errorf("ProviderShare = %d, want 34e9 (85%% of 40 GRID)", row.ProviderShare)
	}
	if row.IogridShare != 6_000_000_000 {
		t.Errorf("IogridShare = %d, want 6e9 (15%%)", row.IogridShare)
	}
	if row.ProviderShare+row.IogridShare != in.ConsumedAtomic {
		t.Errorf("shares must sum to consumed: %d + %d != %d", row.ProviderShare, row.IogridShare, in.ConsumedAtomic)
	}
	// Refund = escrowed - consumed = 60 GRID.
	if row.RefundAtomic != 60_000_000_000 {
		t.Errorf("RefundAtomic = %d, want 60e9", row.RefundAtomic)
	}
}

func TestBuildMeter_Settle_IdempotentOnAttempt(t *testing.T) {
	m := &BuildMeter{St: newFakeBuildStore()}
	in := BuildInput{
		BuildID:        uuid.New(),
		AttemptID:      uuid.New(),
		CustomerWallet: "Cust1111111111111111111111111111111111111111",
		ConsumedAtomic: 10_000_000_000,
	}
	first, err := m.Settle(context.Background(), in)
	if err != nil {
		t.Fatalf("first Settle: %v", err)
	}
	second, err := m.Settle(context.Background(), in)
	if err != nil {
		t.Fatalf("second Settle: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("re-settle minted a new row (%s != %s) — a build-gateway retry would double-pay", first.ID, second.ID)
	}
}

func TestBuildMeter_Settle_Validations(t *testing.T) {
	m := &BuildMeter{St: newFakeBuildStore()}
	base := BuildInput{
		BuildID: uuid.New(), AttemptID: uuid.New(),
		CustomerWallet: "w", ConsumedAtomic: 1,
	}
	cases := map[string]func(BuildInput) BuildInput{
		"no build_id":   func(i BuildInput) BuildInput { i.BuildID = uuid.Nil; return i },
		"no attempt_id": func(i BuildInput) BuildInput { i.AttemptID = uuid.Nil; return i },
		"no wallet":     func(i BuildInput) BuildInput { i.CustomerWallet = ""; return i },
	}
	for name, mut := range cases {
		if _, err := m.Settle(context.Background(), mut(base)); err == nil {
			t.Errorf("%s: expected an error, got nil", name)
		}
	}
	// Zero consumption is a distinct sentinel, not a hard error.
	z := base
	z.ConsumedAtomic = 0
	if _, err := m.Settle(context.Background(), z); err != ErrNoBuildConsumption {
		t.Errorf("zero consumption: want ErrNoBuildConsumption, got %v", err)
	}
}

func TestBuildMemoPrefix_MatchesPingSchema(t *testing.T) {
	// The mobile side builds `iogrid.v1:build:ios:<spec>`; billing-svc must
	// recognise the same prefix to route a build pull.
	if BuildMemoPrefix != "iogrid.v1:build:ios:" {
		t.Errorf("BuildMemoPrefix = %q, drifted from the ping-pay schema", BuildMemoPrefix)
	}
}
