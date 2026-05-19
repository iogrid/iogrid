package solana

import (
	"testing"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/blocto/solana-go-sdk/types"
)

// TestResolveTokenProgram covers the env-string → program id table.
func TestResolveTokenProgram(t *testing.T) {
	cases := []struct {
		in   string
		want common.PublicKey
	}{
		{"", Token2022ProgramID},
		{"token-2022", Token2022ProgramID},
		{"Token2022", Token2022ProgramID},
		{"token", LegacyTokenProgID},
		{"legacy", LegacyTokenProgID},
		{"SPL", LegacyTokenProgID},
	}
	for _, c := range cases {
		got := resolveTokenProgram(c.in)
		if got != c.want {
			t.Errorf("resolveTokenProgram(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestFindATA exercises the canonical SPL associated-token derivation.
// Values: a fake but deterministic wallet + mint pair under Token-2022.
func TestFindATA_Deterministic(t *testing.T) {
	wallet := common.PublicKeyFromString("11111111111111111111111111111112") // arbitrary valid base58
	mint := common.PublicKeyFromString("So11111111111111111111111111111111111111112")
	ataA, err := findATA(wallet, mint, Token2022ProgramID)
	if err != nil {
		t.Fatalf("findATA: %v", err)
	}
	ataB, err := findATA(wallet, mint, Token2022ProgramID)
	if err != nil {
		t.Fatalf("findATA: %v", err)
	}
	if ataA != ataB {
		t.Errorf("findATA must be deterministic; got %v vs %v", ataA, ataB)
	}
	// Legacy program produces a different ATA — proves the derivation
	// actually depends on the program id (which is how Token-2022 isolates
	// its accounts from legacy SPL Token).
	ataLegacy, err := findATA(wallet, mint, LegacyTokenProgID)
	if err != nil {
		t.Fatalf("findATA legacy: %v", err)
	}
	if ataLegacy == ataA {
		t.Errorf("legacy ATA must differ from Token-2022 ATA")
	}
}

// TestBuildTransferChecked_TargetsTokenProgram asserts the instruction's
// program id is rewritten to Token-2022 (the blocto helper hard-codes
// legacy by default). This is the central correctness check for our
// "Token-2022 SPL transfer" claim.
func TestBuildTransferChecked_TargetsToken2022(t *testing.T) {
	wallet := newTestAccount(t)
	mint := common.PublicKeyFromString("So11111111111111111111111111111111111111112")
	src := common.PublicKeyFromString("11111111111111111111111111111112")
	dst := common.PublicKeyFromString("11111111111111111111111111111113")
	cfg := TransferConfig{
		HotWallet:    wallet,
		MintAddress:  mint,
		TokenProgram: Token2022ProgramID,
	}
	ins := buildTransferChecked(cfg, src, dst, 1_000_000_000)

	if ins.ProgramID != Token2022ProgramID {
		t.Errorf("ProgramID = %v, want %v", ins.ProgramID, Token2022ProgramID)
	}
	// Data layout: [opcode=12 (TransferChecked) || u64 LE amount || u8 decimals]
	if len(ins.Data) != 1+8+1 {
		t.Fatalf("Data length = %d, want %d", len(ins.Data), 10)
	}
	if ins.Data[0] != 12 { // SPL token InstructionTransferChecked
		t.Errorf("opcode = %d, want 12", ins.Data[0])
	}
	if ins.Data[9] != GRIDDecimals {
		t.Errorf("decimals = %d, want %d", ins.Data[9], GRIDDecimals)
	}
}

// TestTransferGRID_StubModeRejected ensures the live-only path fails fast in
// stub mode instead of attempting to derive ATAs with an empty mint.
func TestTransferGRID_StubModeRejected(t *testing.T) {
	svc := newStubService(t)
	dest := common.PublicKeyFromString("11111111111111111111111111111113")
	if _, err := svc.TransferGRID(nil, dest, 1); err == nil {
		t.Fatalf("expected error in stub mode")
	}
}

// ── helpers ────────────────────────────────────────────────────────

func newTestAccount(t *testing.T) *types.Account {
	t.Helper()
	a := types.NewAccount()
	return &a
}

// newStubService — convenience: a Service in stub mode (no wallet, no RPC).
func newStubService(t *testing.T) *Service {
	t.Helper()
	svc, err := New(testConfig(""), nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return svc
}
