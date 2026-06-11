package builds_test

import (
	"context"
	"testing"
	"time"

	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/builds"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/gridsettle"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/metering"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/s3artifact"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/store"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/webhook"
	"github.com/iogrid/iogrid/coordinator/services/build-gateway/internal/workloadclient"
)

// recordingSettler captures the BuildSettleInput the Service hands the
// settler, so we can assert the terminal hook fired with the right shape.
type recordingSettler struct{ calls []gridsettle.BuildSettleInput }

func (r *recordingSettler) SettleBuild(_ context.Context, in gridsettle.BuildSettleInput) error {
	r.calls = append(r.calls, in)
	return nil
}

type fixedWallet struct{ wallet string }

func (f fixedWallet) ResolveWallet(context.Context, string) (string, error) { return f.wallet, nil }

// TestService_TerminalStatus_FiresGridSettle is the G3 wiring test (#718):
// gridsettle_test covers the Settler in isolation and grid_build_end_test
// covers billing-svc's endpoint, but nothing proved the build-gateway
// Service LIFECYCLE actually drives the settle — i.e. that submitting a
// build (which resolves the customer wallet, #723) and then transitioning
// it to a terminal status fires settleGrid with the resolved wallet + the
// consumed amount computed from billable minutes. This closes that gap so
// a regression in the terminal hook (service.go:354) fails CI rather than
// silently dropping provider $GRID settlement.
func TestService_TerminalStatus_FiresGridSettle(t *testing.T) {
	clock := newClock(time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC))
	rec := &recordingSettler{}
	const wallet = "7gWxQ3iogridDevnetWalletXXXXXXXXXXXXXXXXXXXX"

	svc := builds.NewService(builds.Options{
		Store:      store.NewInMemory(clock.Now),
		Dispatcher: workloadclient.NewInMemory(clock.Now),
		Storage:    s3artifact.NewInMemory(clock.Now, ""),
		Webhooks:   webhook.NewRecorder(),
		Metering:   metering.NewInMemory(),
		Logs:       builds.NewLogHub(64),
		Now:        clock.Now,
		GridSettle: rec,
		Wallets:    fixedWallet{wallet: wallet},
	})
	ctx := context.Background()

	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// Wallet must be resolved at submit (#723) so the settle isn't a no-op.
	if b.CustomerWallet != wallet {
		t.Fatalf("wallet not resolved at submit: got %q", b.CustomerWallet)
	}

	clock.Advance(5 * time.Second)
	if _, err := svc.UpdateStatus(ctx, b.ID, builds.StatusRunning, "vm-booted", 0); err != nil {
		t.Fatalf("running update: %v", err)
	}
	// Not terminal yet → no settle.
	if len(rec.calls) != 0 {
		t.Fatalf("settle fired before terminal: %d calls", len(rec.calls))
	}

	clock.Advance(7 * time.Minute)
	if _, err := svc.UpdateStatus(ctx, b.ID, builds.StatusSucceeded, "ok", 0); err != nil {
		t.Fatalf("succeed update: %v", err)
	}

	if len(rec.calls) != 1 {
		t.Fatalf("expected exactly 1 settle on terminal, got %d", len(rec.calls))
	}
	got := rec.calls[0]
	if got.BuildID != b.ID {
		t.Errorf("settle build_id = %q, want %q", got.BuildID, b.ID)
	}
	if got.CustomerWallet != wallet {
		t.Errorf("settle wallet = %q, want %q", got.CustomerWallet, wallet)
	}
	if got.AttemptID != b.ProviderAttemptID {
		t.Errorf("settle attempt_id = %q, want %q", got.AttemptID, b.ProviderAttemptID)
	}
	wantAtomic := gridsettle.BillableToAtomic(7, gridsettle.DefaultRatePerMinuteAtomic)
	if got.ConsumedAtomic != wantAtomic {
		t.Errorf("settle consumed = %d, want %d (7 min × rate)", got.ConsumedAtomic, wantAtomic)
	}
	if got.EscrowedAtomic != wantAtomic {
		t.Errorf("settle escrowed = %d, want %d", got.EscrowedAtomic, wantAtomic)
	}
}

// TestService_NonTerminal_NoSettle guards the inverse: a running build that
// never reaches terminal must never settle.
func TestService_NonTerminal_NoSettle(t *testing.T) {
	clock := newClock(time.Date(2026, 5, 19, 12, 0, 0, 0, time.UTC))
	rec := &recordingSettler{}
	svc := builds.NewService(builds.Options{
		Store:      store.NewInMemory(clock.Now),
		Dispatcher: workloadclient.NewInMemory(clock.Now),
		Storage:    s3artifact.NewInMemory(clock.Now, ""),
		Webhooks:   webhook.NewRecorder(),
		Metering:   metering.NewInMemory(),
		Logs:       builds.NewLogHub(64),
		Now:        clock.Now,
		GridSettle: rec,
		Wallets:    fixedWallet{wallet: "w"},
	})
	ctx := context.Background()
	b, err := svc.Submit(ctx, "ws-1", "u-1", "free", validSubmit())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	clock.Advance(5 * time.Second)
	if _, err := svc.UpdateStatus(ctx, b.ID, builds.StatusRunning, "vm-booted", 0); err != nil {
		t.Fatalf("running update: %v", err)
	}
	if len(rec.calls) != 0 {
		t.Fatalf("settle fired for a non-terminal build: %d calls", len(rec.calls))
	}
}
