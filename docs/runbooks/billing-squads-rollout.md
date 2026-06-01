# billing-svc Phase-2 cutover — Squads multisig rollout (Closes #439)

> Migrates the hot-wallet signing path from a single keypair to a 2-of-3
> Squads Protocol multisig. The code (`internal/solana/multisig.go`) is
> shipped and tested. This runbook is the operator-side procedure for
> creating the multisig + flipping billing-svc to use it.
>
> Critical safety property: the bot signer NEVER reaches threshold
> alone. Every payout / burn / transfer requires a human approval via
> the Squads UI before execution. A bot-private-key compromise can only
> queue proposals — it cannot move funds.

## Why this is not full automation

Squads v4 instruction encoding (Borsh-tagged discriminators inside
nested `vault_transaction_create` accounts) is precise to a degree that
shipping speculative byte layouts would risk bricking mainnet payouts
on the first proposal. The shipped code therefore:

- Implements + tests the three PDA derivations (vault, transaction,
  proposal) — these are pure and verifiable offline.
- Implements the operator startup sanity check
  (`SquadsConfigSanityCheck`) that cross-validates the configured
  `SQUADS_MULTISIG_PUBKEY` against its derived vault-0 PDA.
- Routes the proposal-build path through the operator's `squads-cli`
  / Squads web UI instead of constructing instructions in-process.
- Reads on-chain proposal state to surface "pending approval" in
  billing-svc dashboards once the proposal lands.

In-process proposal construction can be added later as polish without
changing the security posture; the current design is intentionally
biased toward "no bot can authorise spending without a human in the
loop, period."

## Phase 2 cutover — one-time setup

1. **Create the multisig.** Founder opens https://app.squads.so on a
   hardware wallet (Ledger), connects, picks "Create multisig":
   - Threshold: 2-of-3.
   - Members:
     - Founder hardware wallet (Ledger seed).
     - Ops hardware wallet (Ledger seed, separate device).
     - Bot signer — the *existing* billing-svc hot wallet pubkey.
   - Time lock: 24 hours on the funded vault (mainnet only; devnet 0).
   - The web UI returns the multisig PDA address (32-byte base58).

2. **Cross-check vault-0 address.** Before transferring any funds, in
   a billing-svc dev container:

   ```
   SQUADS_MULTISIG_PUBKEY=<multisig-pda> \
   go run ./cmd/billing-svc/healthz --solana-sanity
   ```

   The health log line emits both the multisig PDA and the derived
   vault-0 PDA. The vault-0 PDA must match what the Squads UI shows
   under "Vault 0" address; if not, abort — the configured multisig
   isn't the one the operator thinks it is.

3. **Fund the vault.** Transfer the existing single-key hot-wallet
   balance to the vault-0 PDA. From here on, only Squads-approved
   transactions move funds out.

4. **Flip billing-svc config.** Set the `SQUADS_MULTISIG_PUBKEY` env
   var in the production Deployment. The pod restart picks it up,
   logs `multisig_mode=squads`, and the proposal-routing path becomes
   active.

5. **Drain the single keypair.** The old single-keypair address keeps
   ~0.05 SOL for transaction fees (proposing costs SOL on the bot
   signer). Everything else moves to the vault.

## Day-2 operations

- **Provider payouts.** billing-svc batches the daily payout, hands the
  instruction bundle to operator tooling (`squads-cli vault-tx
  propose --from billing-svc-batch-<date>.json`), waits for a founder
  approval to land. On approval, anyone (including the bot) can call
  `squads-cli vault-tx execute`.
- **Stop-the-world.** A founder revokes the bot signer member from the
  multisig (Squads UI → Settings → Remove member) and the proposal
  path stops working immediately, no code change.
- **Treasury vault vs hot-wallet vault.** Phase-2 topology recommends
  two multisigs: a 3-of-5 treasury (founder + 2 ops + 2 advisors)
  holding bulk $GRID + USDC, and the 2-of-3 hot wallet documented
  here that holds at most one day's payout float. Drains from
  treasury → hot-wallet vault are themselves multisig proposals.

## Code map

| Concern | File |
|---|---|
| Multisig mode flag | `internal/solana/multisig.go` (`MultisigMode`, `IsMultisig`) |
| PDA derivations | `internal/solana/multisig.go` (`DeriveSquadsVaultPDA`, `…TransactionPDA`, `…ProposalPDA`) |
| Startup sanity check | `internal/solana/multisig.go` (`SquadsConfigSanityCheck`) |
| Single-key fallback | `internal/solana/transfer.go` (callers route through `ProposeViaSquads` first, fall back on `ErrSquadsNotConfigured`) |
| Tests | `internal/solana/multisig_test.go` |
