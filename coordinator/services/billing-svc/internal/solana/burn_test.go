package solana

import (
	"strings"
	"testing"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/blocto/solana-go-sdk/program/token"
)

// TestStableTagForDay covers the 32-byte cap that the on-chain registry
// enforces. The combined "<base>:<day>" must NEVER exceed MAX_SOURCE_TAG_LEN.
func TestStableTagForDay(t *testing.T) {
	tag := stableTagForDay("2026-05-17", SourceTagDailyRevenue)
	if got := len(tag); got > 32 {
		t.Errorf("len(tag) = %d, want ≤32", got)
	}
	if !strings.HasPrefix(tag, "daily-revenue-burn") {
		t.Errorf("missing base prefix: %q", tag)
	}
}

// TestBuildBurnRegistryRecord_DiscPlusArgs verifies the instruction data has
// the precomputed Anchor discriminator + the bincode-shaped args.
func TestBuildBurnRegistryRecord_DiscPlusArgs(t *testing.T) {
	prog := common.PublicKeyFromString("11111111111111111111111111111112")
	reg := common.PublicKeyFromString("11111111111111111111111111111113")
	rcpt := common.PublicKeyFromString("11111111111111111111111111111114")
	att := common.PublicKeyFromString("11111111111111111111111111111115")
	ins := buildBurnRegistryRecord(prog, reg, rcpt, att, 42, 100, "tag")
	if ins.ProgramID != prog {
		t.Errorf("ProgramID = %v, want %v", ins.ProgramID, prog)
	}
	if len(ins.Accounts) != 3 {
		t.Errorf("Accounts len = %d, want 3", len(ins.Accounts))
	}
	if !ins.Accounts[2].IsSigner {
		t.Errorf("attestor must be signer")
	}
	// 8 disc + 8 amount + 8 revenue + 4 len + len("tag")
	wantLen := 8 + 8 + 8 + 4 + 3
	if len(ins.Data) != wantLen {
		t.Errorf("Data len = %d, want %d", len(ins.Data), wantLen)
	}
	// Discriminator first byte
	if ins.Data[0] != 0x6a {
		t.Errorf("disc[0] = 0x%x, want 0x6a", ins.Data[0])
	}
}

// TestBurnChecked_OpcodeOnToken2022 — sanity-check that BurnChecked under
// Token-2022 carries the SPL standard opcode (15 = InstructionBurnChecked).
func TestBurnChecked_OpcodeOnToken2022(t *testing.T) {
	acct := newTestAccount(t)
	mint := common.PublicKeyFromString("So11111111111111111111111111111111111111112")
	hotATA, err := findATA(acct.PublicKey, mint, Token2022ProgramID)
	if err != nil {
		t.Fatalf("findATA: %v", err)
	}
	ins := token.BurnChecked(token.BurnCheckedParam{
		Account:  hotATA,
		Mint:     mint,
		Auth:     acct.PublicKey,
		Amount:   1_000_000_000,
		Decimals: GRIDDecimals,
	})
	ins.ProgramID = Token2022ProgramID
	if ins.ProgramID != Token2022ProgramID {
		t.Errorf("ProgramID = %v", ins.ProgramID)
	}
	if ins.Data[0] != 15 { // InstructionBurnChecked
		t.Errorf("opcode = %d, want 15", ins.Data[0])
	}
}
