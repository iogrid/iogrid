package solana

import (
	"context"
	"errors"
	"testing"

	"github.com/blocto/solana-go-sdk/common"
)

// TestMultisigMode_FallbackToSingleSig — the most important property in
// Phase 0/1: when SQUADS_MULTISIG_PUBKEY is unset, ProposeViaSquads returns
// the canonical sentinel so callers can fall back to single-sig submit
// without parsing the error string.
func TestMultisigMode_FallbackToSingleSig(t *testing.T) {
	cfg := testConfig("")
	cfg.SquadsMultisigPubkey = ""
	svc, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if svc.IsMultisig() {
		t.Errorf("IsMultisig should be false when env unset")
	}
	if svc.MultisigMode() != MultisigModeSingleSig {
		t.Errorf("mode = %s, want %s", svc.MultisigMode(), MultisigModeSingleSig)
	}
	if _, ok := svc.SquadsVaultPubkey(); ok {
		t.Errorf("SquadsVaultPubkey ok = true in single-sig mode")
	}
	if err := svc.ProposeViaSquads(context.Background(), "noop"); !errors.Is(err, ErrSquadsNotConfigured) {
		t.Errorf("ProposeViaSquads err = %v, want ErrSquadsNotConfigured", err)
	}
}

// TestMultisigMode_SquadsConfigured — when the env is set the mode flips,
// the vault pubkey is exposed, and the sentinel ErrSquadsNotConfigured is
// no longer returned (we get the Phase-2-NotYetImplemented placeholder).
func TestMultisigMode_SquadsConfigured(t *testing.T) {
	cfg := testConfig("")
	cfg.SquadsMultisigPubkey = "SQUADSv4r54mDmXrPRpAhAFCYNvWdCRyHN8izyDhB7L"
	svc, err := New(cfg, nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !svc.IsMultisig() {
		t.Errorf("IsMultisig should be true")
	}
	if svc.MultisigMode() != MultisigModeSquads {
		t.Errorf("mode = %s, want %s", svc.MultisigMode(), MultisigModeSquads)
	}
	_, ok := svc.SquadsVaultPubkey()
	if !ok {
		t.Errorf("SquadsVaultPubkey ok = false")
	}
	err = svc.ProposeViaSquads(context.Background(), "noop")
	if err == nil {
		t.Errorf("expected NotYetImplemented error from Phase-2 stub")
	}
	if errors.Is(err, ErrSquadsNotConfigured) {
		t.Errorf("should NOT return ErrSquadsNotConfigured when env is set")
	}
	if !errors.Is(err, ErrSquadsProposalNotShipped) {
		t.Errorf("expected ErrSquadsProposalNotShipped, got %v", err)
	}
}

// TestSquadsPDAs_Deterministic — the three PDA derivations are pure
// functions of (multisigPubkey, programID). Same inputs must always
// produce the same output. Catches regressions where seed ordering or
// little-endian encoding of the txIndex drifts. The exact addresses
// don't need to be checked against the on-chain reality here — the
// cross-check in SquadsConfigSanityCheck does that at startup against
// the operator's actual multisig.
func TestSquadsPDAs_Deterministic(t *testing.T) {
	ms := mustParsePubkey(t, "SQUADSv4r54mDmXrPRpAhAFCYNvWdCRyHN8izyDhB7L")

	v0a, err := DeriveSquadsVaultPDA(ms, 0)
	if err != nil {
		t.Fatalf("DeriveSquadsVaultPDA: %v", err)
	}
	v0b, err := DeriveSquadsVaultPDA(ms, 0)
	if err != nil {
		t.Fatalf("DeriveSquadsVaultPDA: %v", err)
	}
	if v0a != v0b {
		t.Errorf("vault PDA non-deterministic: %s vs %s", v0a.ToBase58(), v0b.ToBase58())
	}

	v1, err := DeriveSquadsVaultPDA(ms, 1)
	if err != nil {
		t.Fatalf("DeriveSquadsVaultPDA: %v", err)
	}
	if v1 == v0a {
		t.Errorf("vault index 0 and 1 must derive distinct PDAs (got both = %s)", v0a.ToBase58())
	}

	tx0, err := DeriveSquadsTransactionPDA(ms, 0)
	if err != nil {
		t.Fatalf("DeriveSquadsTransactionPDA: %v", err)
	}
	tx1, err := DeriveSquadsTransactionPDA(ms, 1)
	if err != nil {
		t.Fatalf("DeriveSquadsTransactionPDA: %v", err)
	}
	if tx0 == tx1 {
		t.Errorf("tx PDA must change with index (got both = %s)", tx0.ToBase58())
	}

	prop0, err := DeriveSquadsProposalPDA(ms, 0)
	if err != nil {
		t.Fatalf("DeriveSquadsProposalPDA: %v", err)
	}
	if prop0 == tx0 {
		t.Errorf("proposal PDA must differ from transaction PDA at same index")
	}
}

func mustParsePubkey(t *testing.T, s string) common.PublicKey {
	t.Helper()
	return common.PublicKeyFromString(s)
}
