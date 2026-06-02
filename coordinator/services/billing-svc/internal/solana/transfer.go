// transfer.go — real Token-2022 (or legacy SPL) TransferChecked execution.
//
// One public entrypoint:
//
//	TransferGRID(ctx, dest, lamports) -> (signature, error)
//
// dest is the recipient's *wallet* address; we derive the recipient ATA
// (associated token account) from (wallet, mint, token-program). The
// upstream blocto SDK's `token.TransferChecked` instruction is reused with a
// surgically-patched `ProgramID` so the same builder serves both legacy SPL
// Token and Token-2022 mints.
//
// Decimals are baked into the TransferChecked encoding (per SPL spec),
// matching $GRID's 9 decimals (TOKENOMICS §"Token primitives").

package solana

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/blocto/solana-go-sdk/common"
	"github.com/blocto/solana-go-sdk/program/token"
	"github.com/blocto/solana-go-sdk/types"
)

// GRIDDecimals is hard-coded to 9 to match the SPL/Token-2022 convention
// captured in TOKENOMICS §"Token mechanics" (Initial supply / Decimals row).
// The mint authority MUST set the same value at TGE; we cross-check at
// boot when live mode is on (see solana.go).
const GRIDDecimals uint8 = 9

// TransferConfig is what TransferGRID needs from the surrounding Service.
// Keeping it explicit makes the transfer helper testable without dragging
// the whole config struct.
type TransferConfig struct {
	HotWallet    *types.Account
	MintAddress  common.PublicKey
	TokenProgram common.PublicKey // Token2022 or legacy SPL Token
}

// resolveTokenProgram parses GRID_TOKEN_MINT_PROGRAM (Token-2022 default).
//
// The legacy SPL Token program (Tokenkeg…) is supported for tests / dev
// because devnet faucets sometimes mint via the legacy program. Production
// (per TOKENOMICS) uses Token-2022 exclusively.
func resolveTokenProgram(name string) common.PublicKey {
	switch name {
	case "", "token-2022", "Token2022":
		return Token2022ProgramID
	case "token", "legacy", "SPL":
		return LegacyTokenProgID
	default:
		return common.PublicKeyFromString(name)
	}
}

// findATA returns the wallet's associated-token-account for `mint` under
// `tokenProgram`. The blocto SDK only ships the legacy-Token ATA derivation
// (FindAssociatedTokenAddress), so we re-derive directly when using
// Token-2022 to keep behaviour symmetric.
func findATA(wallet, mint, tokenProgram common.PublicKey) (common.PublicKey, error) {
	seeds := [][]byte{
		wallet.Bytes(),
		tokenProgram.Bytes(),
		mint.Bytes(),
	}
	pk, _, err := common.FindProgramAddress(seeds, common.SPLAssociatedTokenAccountProgramID)
	if err != nil {
		return common.PublicKey{}, fmt.Errorf("derive ATA: %w", err)
	}
	return pk, nil
}

// buildTransferChecked returns a TransferChecked instruction targeting
// `cfg.TokenProgram` (Token-2022 by default). The blocto helper is reused
// for the bincode encoding, then the program-id is rewritten to point at
// Token-2022 — both programs use the same wire format for this op.
func buildTransferChecked(cfg TransferConfig, sourceATA, destATA common.PublicKey, amount uint64) types.Instruction {
	ins := token.TransferChecked(token.TransferCheckedParam{
		From:     sourceATA,
		To:       destATA,
		Mint:     cfg.MintAddress,
		Auth:     cfg.HotWallet.PublicKey,
		Amount:   amount,
		Decimals: GRIDDecimals,
	})
	// Swap the program id: the helper hard-codes legacy SPL Token. Token-2022
	// is a drop-in replacement at the on-wire layer for this instruction.
	ins.ProgramID = cfg.TokenProgram
	return ins
}

// GRIDAtomicTreasuryBalance returns the treasury ATA balance via a single
// `getTokenAccountBalance` RPC call. Used by the settlement-worker
// pre-flight + dashboards. Returns 0 + no error when the ATA does not
// exist yet (e.g. treasury just-deployed before the first mint).
func (s *Service) GRIDAtomicTreasuryBalance(ctx context.Context) (uint64, error) {
	if !s.Enabled() {
		return 0, errors.New("solana: treasury balance in stub mode")
	}
	if s.chain == nil {
		return 0, errors.New("solana: chain client unset")
	}
	cfg := TransferConfig{
		HotWallet:    s.wallet,
		MintAddress:  common.PublicKeyFromString(s.cfg.GRIDTokenMint),
		TokenProgram: s.tokenProgramID,
	}
	ata, err := findATA(s.wallet.PublicKey, cfg.MintAddress, cfg.TokenProgram)
	if err != nil {
		return 0, err
	}
	res, err := s.chain.rpc.GetTokenAccountBalance(ctx, ata.ToBase58())
	if err != nil {
		// blocto returns the well-known "could not find account" / "Invalid
		// param: not a Token account" as a plain error; treat as zero.
		msg := err.Error()
		if isAccountNotFound(msg) {
			return 0, nil
		}
		return 0, fmt.Errorf("solana: getTokenAccountBalance: %w", err)
	}
	return res.Amount, nil
}

func isAccountNotFound(s string) bool {
	for _, sub := range []string{
		"could not find account",
		"Invalid param: not a Token account",
		"AccountNotFound",
	} {
		if substring(s, sub) {
			return true
		}
	}
	return false
}

func substring(haystack, needle string) bool {
	hlen := len(haystack)
	nlen := len(needle)
	if nlen == 0 || nlen > hlen {
		return nlen == 0
	}
	for i := 0; i <= hlen-nlen; i++ {
		if haystack[i:i+nlen] == needle {
			return true
		}
	}
	return false
}

// TransferGRID transfers `amount` lamports of $GRID from the hot wallet's
// ATA to the recipient wallet's ATA, signed by the hot wallet.
//
// Caller must have verified that the destination ATA exists (either created
// upstream at provider-binding time, or via a separate
// `CreateAssociatedTokenAccount` instruction prepended to the message). For
// Phase 0/1 we *fail* if the destination ATA does not exist; auto-create is
// a Phase-2 hardening pass.
func (s *Service) TransferGRID(ctx context.Context, destWallet common.PublicKey, amount uint64) (string, error) {
	if !s.Enabled() {
		return "", errors.New("solana: transfer attempted in stub mode")
	}
	if s.chain == nil {
		return "", errors.New("solana: chain client unset (programmer error)")
	}
	if amount == 0 {
		return "", errors.New("solana: amount=0")
	}
	cfg := TransferConfig{
		HotWallet:    s.wallet,
		MintAddress:  common.PublicKeyFromString(s.cfg.GRIDTokenMint),
		TokenProgram: s.tokenProgramID,
	}

	sourceATA, err := findATA(s.wallet.PublicKey, cfg.MintAddress, cfg.TokenProgram)
	if err != nil {
		return "", err
	}
	destATA, err := findATA(destWallet, cfg.MintAddress, cfg.TokenProgram)
	if err != nil {
		return "", err
	}

	ins := buildTransferChecked(cfg, sourceATA, destATA, amount)

	s.logger.Info("solana: submitting $GRID transfer",
		slog.String("source_ata", sourceATA.ToBase58()),
		slog.String("dest_ata", destATA.ToBase58()),
		slog.String("dest_wallet", destWallet.ToBase58()),
		slog.Uint64("amount_lamports", amount),
		slog.String("token_program", cfg.TokenProgram.ToBase58()),
	)
	sig, err := s.chain.SubmitAndConfirm(ctx,
		[]types.Instruction{ins},
		[]types.Account{*s.wallet},
		s.wallet.PublicKey,
	)
	if err != nil {
		return sig, err
	}
	s.logger.Info("solana: $GRID transfer confirmed",
		slog.String("signature", sig),
		slog.Uint64("amount_lamports", amount))
	return sig, nil
}
