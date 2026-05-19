// burn.go — daily buyback-and-burn execution (TOKENOMICS §"Layer 1").
//
// Flow for a single day's burn:
//
//  1. Compute USD revenue → atomic USDC amount (1 cent = 10_000 atomic).
//  2. Jupiter swap USDC → $GRID, output to the hot wallet's $GRID ATA.
//  3. Burn the realised $GRID amount via SPL BurnChecked on the Token-2022
//     program — funds vanish from the supply (per SPL `Burn` semantics).
//     We use *real* burn (mint supply decreases) rather than transfer-to-
//     incinerator because Token-2022 supports it natively; the
//     incinerator-transfer is documented as a fallback for legacy mints.
//  4. (Optional) record on-chain via the burn-registry Anchor program — we
//     prepare the instruction here; wiring the actual program call is
//     deferred to Phase 2 (the program is shipped in `contracts/`).
//
// The function is idempotent at the *row* level: each call inserts a new
// `solana_burn` row with a new uuid, so retrying a failed burn always
// produces a fresh record. Duplicate-day protection lives at the cron site
// (it skips a date if there's already a CONFIRMED row).

package solana

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/blocto/solana-go-sdk/program/token"
	"github.com/blocto/solana-go-sdk/types"
)

// BurnResult is what ExecuteBurn returns on success.
type BurnResult struct {
	SwapSignature string
	BurnSignature string
	USDCSpent     uint64
	GRIDBurned    uint64
}

// ExecuteBurn runs the live buyback-and-burn: swap USDC → $GRID → burn.
// Caller must have already verified s.Enabled() and computed usdCents.
//
// `transferToIncinerator` — when true, the burn is implemented as a
// TransferChecked to INCINERATOR_ADDRESS rather than a real BurnChecked.
// Used for legacy-Token mints or when the hot wallet is not the mint's
// burn authority. Default false (real burn).
func (s *Service) ExecuteBurn(ctx context.Context, usdCents int64, transferToIncinerator bool) (*BurnResult, error) {
	if !s.Enabled() {
		return nil, errors.New("solana: ExecuteBurn called in stub mode")
	}
	if usdCents <= 0 {
		return nil, errors.New("solana: ExecuteBurn: usdCents<=0")
	}

	// 1) Swap USDC → $GRID (output goes to hot wallet's $GRID ATA).
	inAmount := uint64(usdCents) * 10_000
	swap, err := s.ExecuteSwap(ctx, SwapRequest{
		InputMint:   USDCMint,
		OutputMint:  s.cfg.GRIDTokenMint,
		Amount:      inAmount,
		SlippageBps: 50,
	})
	if err != nil {
		return nil, fmt.Errorf("burn swap: %w", err)
	}
	gridAmount := swap.OutAmount

	// 2) Burn (or transfer-to-incinerator) the realised $GRID amount.
	mint := common.PublicKeyFromString(s.cfg.GRIDTokenMint)
	hotATA, err := findATA(s.wallet.PublicKey, mint, s.tokenProgramID)
	if err != nil {
		return nil, fmt.Errorf("burn: derive hot wallet ATA: %w", err)
	}

	var burnIns types.Instruction
	if transferToIncinerator {
		incin := common.PublicKeyFromString(s.cfg.IncineratorAddress)
		incinATA, err := findATA(incin, mint, s.tokenProgramID)
		if err != nil {
			return nil, fmt.Errorf("burn: derive incinerator ATA: %w", err)
		}
		burnIns = buildTransferChecked(TransferConfig{
			HotWallet:    s.wallet,
			MintAddress:  mint,
			TokenProgram: s.tokenProgramID,
		}, hotATA, incinATA, gridAmount)
	} else {
		ins := token.BurnChecked(token.BurnCheckedParam{
			Account:  hotATA,
			Mint:     mint,
			Auth:     s.wallet.PublicKey,
			Amount:   gridAmount,
			Decimals: GRIDDecimals,
		})
		ins.ProgramID = s.tokenProgramID
		burnIns = ins
	}

	burnSig, err := s.chain.SubmitAndConfirm(ctx,
		[]types.Instruction{burnIns},
		[]types.Account{*s.wallet},
		s.wallet.PublicKey,
	)
	if err != nil {
		return &BurnResult{
			SwapSignature: swap.Signature,
			USDCSpent:     inAmount,
			GRIDBurned:    0,
			BurnSignature: burnSig,
		}, fmt.Errorf("burn submit: %w", err)
	}
	s.logger.Info("solana: burn confirmed",
		slog.String("swap_signature", swap.Signature),
		slog.String("burn_signature", burnSig),
		slog.Uint64("usdc_in", inAmount),
		slog.Uint64("grid_burned", gridAmount),
		slog.Bool("via_incinerator", transferToIncinerator),
	)

	return &BurnResult{
		SwapSignature: swap.Signature,
		BurnSignature: burnSig,
		USDCSpent:     inAmount,
		GRIDBurned:    gridAmount,
	}, nil
}

// burnRegistryRecordInstruction builds the (unused, Phase-2-only) Anchor
// instruction for the burn-registry CPI. Kept in-tree so the on-chain
// program and the off-chain billing-svc can be tested together once the
// Phase-2 wiring lands.
//
// The Anchor program lives at `contracts/programs/burn/src/lib.rs`. Its
// `record_burn(amount, revenue_cents, source_tag)` instruction is what we
// build here. The actual program ID + discriminator are placeholders pending
// `anchor deploy`.
func buildBurnRegistryRecord(programID common.PublicKey, registry, receipt, attestor common.PublicKey, amount, revenueCents uint64, sourceTag string) types.Instruction {
	// Anchor instruction layout: [8-byte discriminator || args].
	// Discriminator = first 8 bytes of sha256("global:record_burn"). We
	// hard-code the precomputed value rather than re-derive at runtime to
	// keep this file dependency-light.
	disc := []byte{0x6a, 0xb5, 0x3c, 0x32, 0x12, 0x40, 0x95, 0x7d}

	if len(sourceTag) > 32 {
		sourceTag = sourceTag[:32]
	}
	// Anchor string serialization: u32 LE length + bytes.
	tagBytes := []byte(sourceTag)
	data := make([]byte, 0, 8+8+8+4+len(tagBytes))
	data = append(data, disc...)
	data = appendU64LE(data, amount)
	data = appendU64LE(data, revenueCents)
	data = appendU32LE(data, uint32(len(tagBytes)))
	data = append(data, tagBytes...)

	return types.Instruction{
		ProgramID: programID,
		Accounts: []types.AccountMeta{
			{PubKey: registry, IsSigner: false, IsWritable: true},
			{PubKey: receipt, IsSigner: false, IsWritable: true},
			{PubKey: attestor, IsSigner: true, IsWritable: true},
		},
		Data: data,
	}
}

func appendU64LE(b []byte, v uint64) []byte {
	for i := 0; i < 8; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

func appendU32LE(b []byte, v uint32) []byte {
	for i := 0; i < 4; i++ {
		b = append(b, byte(v>>(8*i)))
	}
	return b
}

// SourceTag is a short label that captures *where* the burn came from. Kept
// short so it fits the on-chain registry's 32-byte cap (see MAX_SOURCE_TAG_LEN
// in `contracts/programs/burn/src/lib.rs`).
type SourceTag string

const (
	SourceTagDailyRevenue SourceTag = "daily-revenue-burn"
)

// String makes SourceTag fmt.Stringer-compatible — useful for logging.
func (t SourceTag) String() string { return string(t) }

// stableTagForDay derives a deterministic source tag from a date string —
// kept here so unit tests can assert formatting.
func stableTagForDay(day string, base SourceTag) string {
	out := string(base) + ":" + day
	if len(out) > 32 {
		out = out[:32]
	}
	return strings.TrimSpace(out)
}
