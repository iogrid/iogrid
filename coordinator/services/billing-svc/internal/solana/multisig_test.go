package solana

import (
	"context"
	"errors"
	"testing"
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
}
