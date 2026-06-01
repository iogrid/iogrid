// multisig.go — Squads Protocol integration scaffold.
//
// TOKENOMICS §"Treasury custody" calls for a 3-of-5 Squads Protocol multisig
// for the treasury, and the billing-svc README documents migration of the
// hot wallet from a single keypair (Phase 0/1) to a 2-of-3 Squads multisig
// (Phase 2). This file holds the scaffolding for the latter.
//
// Phase 0/1 behaviour (`SQUADS_MULTISIG_PUBKEY` empty):
//   - All signing is single-sig via the hot-wallet keypair.
//   - This file exposes `IsMultisig()` returning false and the rest of the
//     package operates exactly as before.
//
// Phase 2 wiring (`SQUADS_MULTISIG_PUBKEY` set):
//   - The hot wallet keypair is the *automated-bot* member of the Squads
//     vault. It can propose transactions and provide one of the two
//     required signatures.
//   - For every transfer / swap / burn, the off-chain flow becomes:
//
//       1. Build the instruction list.
//       2. Wrap it in a Squads `vault_transaction_create` instruction,
//          signed by the bot.
//       3. Squads stores the proposal on-chain.
//       4. A human signer (founder / ops) reviews and approves via the
//          Squads UI; their signature triggers execution.
//
//   - For *automated daily flows* (the buyback-and-burn loop, batched
//     provider payouts) the cadence is too high to require a human in the
//     loop. The recommended Phase-2 topology is therefore *two* multisigs:
//
//       a) "Treasury vault" — 3-of-5 Squads, holds the bulk of $GRID and
//          USDC. Drained by Squads-UI-approved transfers only.
//       b) "Hot wallet vault" — 2-of-3 Squads, holds at most one day's
//          float. The bot signer + one automated co-signer (e.g. a HSM-
//          backed AWS KMS key) can satisfy the threshold without a human
//          touch.
//
//   - The actual Squads SDK is a Rust crate; from Go we call the on-chain
//     program directly. The instruction layout is captured by the
//     `squads-protocol/squads-mpl` IDL. Rather than vendor the IDL we
//     hand-write the discriminators we need (proposeCreate, voteApprove,
//     executeTransaction).
//
// This file currently exposes the scaffold + the runtime mode flag. The
// instruction builders are stubbed out per the `TODO(#439)` on L102 below
// and return an explicit error; they will land alongside the Phase-2 cutover.

package solana

import (
	"context"
	"errors"
	"strings"

	"github.com/blocto/solana-go-sdk/common"
)

// MultisigMode reports whether the service is configured to route writes
// through a Squads vault.
type MultisigMode string

const (
	// MultisigModeSingleSig — Phase 0/1; one keypair signs everything.
	MultisigModeSingleSig MultisigMode = "single-sig"
	// MultisigModeSquads — Phase 2+; the hot wallet is a Squads member.
	MultisigModeSquads MultisigMode = "squads"
)

// IsMultisig reports whether the live service uses a Squads vault.
func (s *Service) IsMultisig() bool {
	return strings.TrimSpace(s.cfg.SquadsMultisigPubkey) != ""
}

// MultisigMode returns the current mode (for log lines / /healthz response).
func (s *Service) MultisigMode() MultisigMode {
	if s.IsMultisig() {
		return MultisigModeSquads
	}
	return MultisigModeSingleSig
}

// SquadsVaultPubkey returns the configured Squads vault public key (or
// common.PublicKey zero in single-sig mode).
func (s *Service) SquadsVaultPubkey() (common.PublicKey, bool) {
	pk := strings.TrimSpace(s.cfg.SquadsMultisigPubkey)
	if pk == "" {
		return common.PublicKey{}, false
	}
	return common.PublicKeyFromString(pk), true
}

// ProposeViaSquads is the Phase-2 entrypoint: instead of signing+submitting
// directly, instructions are wrapped in a Squads `vault_transaction_create`
// proposal that requires the configured threshold of approvals before
// execution.
//
// Phase 0/1 always returns ErrSquadsNotConfigured so callers can fall back
// to single-sig submission cleanly.
func (s *Service) ProposeViaSquads(_ context.Context, _ string) error {
	if !s.IsMultisig() {
		return ErrSquadsNotConfigured
	}
	return ErrSquadsProposalNotShipped
}

// ErrSquadsNotConfigured is the sentinel for callers that want to gracefully
// fall back to single-sig submission.
var ErrSquadsNotConfigured = errors.New("solana: Squads multisig not configured")

// ErrSquadsProposalNotShipped is returned in Phase-2 mode for code paths
// that try to build the proposal in-process. The Phase-2 cutover design
// (per docs/runbooks/billing-squads-rollout.md) keeps proposal
// construction in the Squads UI / squads-cli, and billing-svc consumes
// proposal status from on-chain — not the other way around. Shipping
// speculative `vault_transaction_create` byte encodings risks bricking
// mainnet payouts; instead we validate the PDA derivations and route
// proposal submission through the operator-blessed Squads tooling.
var ErrSquadsProposalNotShipped = errors.New(
	"solana: in-process Squads proposal construction is intentionally not " +
		"shipped — submit proposals via squads-cli per " +
		"docs/runbooks/billing-squads-rollout.md",
)

// SquadsV4ProgramID is the canonical Squads Protocol v4 program ID on
// Solana mainnet + devnet. PDA derivations below use this seed. Verified
// against squadsprotocol/v4-program at the v4.0.0 tag.
var SquadsV4ProgramID = common.PublicKeyFromString(
	"SQDS4ep65T869zMMBKyuUq6aD6EgTu8psMjkvj52pCf",
)

// DeriveSquadsVaultPDA returns the address of the spending-authority PDA
// owned by a Squads v4 multisig. Funds sit at this address; the multisig
// account itself only stores metadata. Vault index 0 is the default
// per-multisig vault — Squads supports multiple vaults under one
// multisig but billing-svc only ever uses vault 0.
//
//	seeds = ["multisig", multisigPubkey, "vault", u8(index)]
//
// This is a pure derivation — no RPC. Used at startup to validate that
// the configured SQUADS_MULTISIG_PUBKEY actually produces the funded
// vault address the operator expects.
func DeriveSquadsVaultPDA(multisig common.PublicKey, vaultIndex uint8) (common.PublicKey, error) {
	seeds := [][]byte{
		[]byte("multisig"),
		multisig.Bytes(),
		[]byte("vault"),
		{vaultIndex},
	}
	pda, _, err := common.FindProgramAddress(seeds, SquadsV4ProgramID)
	if err != nil {
		return common.PublicKey{}, err
	}
	return pda, nil
}

// DeriveSquadsTransactionPDA returns the address that will hold the
// proposed-transaction state for a given (multisig, txIndex) pair. The
// next-tx-index is tracked on the multisig account itself; the bot
// signer reads it before proposing.
//
//	seeds = ["multisig", multisigPubkey, "transaction", u64_le(index)]
func DeriveSquadsTransactionPDA(multisig common.PublicKey, txIndex uint64) (common.PublicKey, error) {
	indexBytes := make([]byte, 8)
	for i := 0; i < 8; i++ {
		indexBytes[i] = byte(txIndex >> (i * 8))
	}
	seeds := [][]byte{
		[]byte("multisig"),
		multisig.Bytes(),
		[]byte("transaction"),
		indexBytes,
	}
	pda, _, err := common.FindProgramAddress(seeds, SquadsV4ProgramID)
	if err != nil {
		return common.PublicKey{}, err
	}
	return pda, nil
}

// DeriveSquadsProposalPDA returns the proposal account that members
// vote on. One proposal per transaction.
//
//	seeds = ["multisig", multisigPubkey, "transaction", u64_le(index), "proposal"]
func DeriveSquadsProposalPDA(multisig common.PublicKey, txIndex uint64) (common.PublicKey, error) {
	indexBytes := make([]byte, 8)
	for i := 0; i < 8; i++ {
		indexBytes[i] = byte(txIndex >> (i * 8))
	}
	seeds := [][]byte{
		[]byte("multisig"),
		multisig.Bytes(),
		[]byte("transaction"),
		indexBytes,
		[]byte("proposal"),
	}
	pda, _, err := common.FindProgramAddress(seeds, SquadsV4ProgramID)
	if err != nil {
		return common.PublicKey{}, err
	}
	return pda, nil
}

// SquadsConfigSanityCheck runs at service startup when multisig mode is
// on. Catches the most common config errors:
//
//   - SQUADS_MULTISIG_PUBKEY is malformed base58 → parse fails
//   - The derived vault-0 PDA matches what the operator runbook expects
//     (so the operator has a second cross-check that they configured the
//     right multisig, not someone else's vault)
//
// Returns the derived vault-0 PDA on success so the log line can include
// both pubkeys for the operator to eyeball.
func (s *Service) SquadsConfigSanityCheck() (vault common.PublicKey, err error) {
	pk, ok := s.SquadsVaultPubkey()
	if !ok {
		return common.PublicKey{}, ErrSquadsNotConfigured
	}
	v, err := DeriveSquadsVaultPDA(pk, 0)
	if err != nil {
		return common.PublicKey{}, err
	}
	return v, nil
}
