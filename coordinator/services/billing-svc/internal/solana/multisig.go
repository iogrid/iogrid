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
	// TODO(#439): build vault_transaction_create + proposal_create
	// instructions, submit with bot signer, return proposal ID. Phase-2
	// follow-up to (closed) #98; tracked in detail under iogrid#439.
	return errors.New("solana: Squads multisig proposal not yet implemented (Phase 2)")
}

// ErrSquadsNotConfigured is the sentinel for callers that want to gracefully
// fall back to single-sig submission.
var ErrSquadsNotConfigured = errors.New("solana: Squads multisig not configured")
