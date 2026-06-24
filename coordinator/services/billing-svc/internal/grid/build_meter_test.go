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

func TestBuildMeter_Settle_SelfPayClearsProviderWallet(t *testing.T) {
	// #818: when the build submitter (customer) and the Mac provider owner
	// resolve to the SAME wallet — the dogfood case where one identity both
	// submits the build and owns the provider — the settlement must NOT be a
	// payable provider row. Paying it would move treasury $GRID to the party
	// who "spent" it, manufacturing fake earnings. The row is still persisted
	// (audit + idempotency) but with provider_wallet cleared so the worker
	// (which drains only provider_wallet <> '') never transfers.
	const sameWallet = "3TuRAZPs7YUpcigdjoZsyQ7f7iLf6ZGbjcpsMfwhxbmT"
	m := &BuildMeter{St: newFakeBuildStore()}
	row, err := m.Settle(context.Background(), BuildInput{
		BuildID:        uuid.New(),
		AttemptID:      uuid.New(),
		CustomerWallet: sameWallet,
		ProviderWallet: sameWallet, // self-pay
		ProviderID:     uuid.New(),
		EscrowedAtomic: 10_000_000_000,
		ConsumedAtomic: 10_000_000_000,
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if row.ProviderWallet != "" {
		t.Errorf("self-pay row keeps a payable provider_wallet %q — worker would pay treasury back to the spender", row.ProviderWallet)
	}
	if row.CustomerWallet != sameWallet {
		t.Errorf("customer_wallet should be preserved for audit, got %q", row.CustomerWallet)
	}
	// Shares are still computed (the ledger records the build), but with an
	// empty provider_wallet the worker's query excludes the row.
	if row.ProviderShare == 0 {
		t.Errorf("provider_share should still be computed for audit, got 0")
	}
}

func TestBuildMeter_Settle_DistinctWalletsArePayable(t *testing.T) {
	// The real-economy happy path: customer ≠ provider → the provider_wallet
	// is preserved so the worker pays the provider on-chain.
	m := &BuildMeter{St: newFakeBuildStore()}
	row, err := m.Settle(context.Background(), BuildInput{
		BuildID:        uuid.New(),
		AttemptID:      uuid.New(),
		CustomerWallet: "Cust1111111111111111111111111111111111111111",
		ProviderWallet: "Prov2222222222222222222222222222222222222222",
		ProviderID:     uuid.New(),
		EscrowedAtomic: 10_000_000_000,
		ConsumedAtomic: 10_000_000_000,
	})
	if err != nil {
		t.Fatalf("Settle: %v", err)
	}
	if row.ProviderWallet != "Prov2222222222222222222222222222222222222222" {
		t.Errorf("distinct provider_wallet was wrongly cleared: %q", row.ProviderWallet)
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
