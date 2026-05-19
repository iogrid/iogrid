# $GRID audit — auditor checklist

A structured falsification checklist for the audit team. Each item is a claim about the
on-chain programs that must be verified (or falsified) before the audit closes. Order is
intentional: high-impact economic invariants first, then per-program invariants, then
cross-cutting concerns.

This document is paired with [`./test-vectors.md`](./test-vectors.md) (positive/negative
cases) and [`./threat-model.md`](./threat-model.md) (privileged roles + value at risk).
Reading order for an incoming auditor: README → threat-model → scope → this checklist →
test-vectors.

---

## How to use this document

- Each item is a single falsifiable claim.
- The auditor MUST attempt to break each claim and report the outcome:
  - **PASS** — claim holds in the audit-target commit (cite source line if helpful).
  - **FAIL** — claim does NOT hold; explain the falsification path; severity-rate it.
  - **AMBIGUOUS** — the claim is not precise enough to verify; ask iogrid for clarification.
- The auditor's working notes against each line item go into the audit report appendix.

---

## A. Headline economic invariants (Critical-severity if violated)

### A.1 Hard cap

**Claim:** No reachable program state allows `mint.supply > GRID_HARD_CAP` (= `1_000_000_000 * 10^9` = 1B with 9 decimals).

**Approach to falsify:**
- Trace every code path that can increase `mint.supply`:
  - `grid_token::mint_to_recipient` — checks `config.minted_so_far + amount <= GRID_HARD_CAP` before CPI to Token-2022 `mint_to`.
  - `emission::claim_epoch` — calls `mint_to` on the vault; the amount comes from `budget_for_window` which is bounded by the per-window halving formula. Confirm the cumulative across all epochs cannot exceed 500M (the provider rewards pool size) when starting from a freshly-init mint, AND when starting from a mint with prior `mint_to_recipient` calls.
- If both paths can co-exist, confirm the SUM of caps holds, not just the per-path cap.

### A.2 Authority lock is one-way

**Claim:** `GridConfig.authority_locked` is monotonic — once set to `true`, no instruction sets it back to `false`.

**Approach:** grep `lib.rs` for every `authority_locked = ` assignment; confirm none assigns `false` (besides the `init`-time default in `initialize_config`).

### A.3 Mint authority transferred before lock

**Claim:** The mint authority on the Token-2022 mint MUST be the `emission` program PDA at the time `lock_authorities` is called. Otherwise, after locking, no further mints (including halving emissions) are possible — irreparable bricking.

**Approach:** the deployment procedure (`migrations/deploy.ts` + `scripts/devnet-deploy.sh` + the operator runbook) calls `transfer_mint_authority(new = emission_pda)` THEN `lock_authorities`. The audit must confirm:
- The `transfer_mint_authority` instruction correctly invokes Token-2022 `set_authority` with `AuthorityType::MintTokens`, `new_authority = emission_pda`.
- `lock_authorities` does NOT verify the current mint authority. If the deployer messes up the order, the lock can be set with the admin still holding mint authority. The audit should flag this as a deployment-procedure risk; consider adding an on-chain check.

### A.4 Emission curve caps cumulative supply

**Claim:** `Σ budget_for_window(epoch_i.start, epoch_i.end)` for all valid epochs <= 500M (the provider rewards pool size). The halving curve cannot exceed the pool over its lifetime.

**Approach:** offline mathematical proof + on-chain assertion. The formula:

```
budget_for_window(s, e) = ∫_s^e emission_rate(t) dt
emission_rate(t) = (50M * 10^9 / 31_536_000) * 0.5^floor((t - tge) / 63_072_000)
```

The cumulative across the full curve = `50M * (2 + 1 + 0.5 + 0.25 + ...) = 100M * 2 = ~200M` from the geometric sum, well below 500M. **BUT** the code must enforce 500M as a hard cap on the emission program separately, in case future governance adjusts the curve. Auditor must confirm.

### A.5 Vesting cliff cannot be bypassed

**Claim:** `vesting::withdraw_unlocked` cannot transfer more than `vested_amount(amount, deposited_at, now, tier) - withdrawn` for any `VestingDeposit`.

**Approach:**
- Read the `withdraw_unlocked` handler. Confirm it computes `available = vested_amount(...) - withdrawn` and uses `available` as the transfer amount (not user-supplied).
- Confirm `withdrawn` is incremented before the Transfer CPI (atomicity).
- Confirm `vested_amount` returns 0 for `now < deposited_at + cliff(tier)`.

### A.6 Early-unlock burns exactly 50%

**Claim:** `vesting::early_unlock` burns exactly `floor((locked * 5000) / 10000)` and transfers exactly `locked - burned` to the provider, where `locked = amount - withdrawn`.

**Approach:**
- Read the `early_unlock` handler. Confirm the math.
- Confirm `EARLY_UNLOCK_PENALTY_BPS = 5000` is a Rust `const`, not a parameter from any account.
- Confirm the truncation in integer division favors the burn (i.e., if locked is odd, the burn gets the rounded-up half).

### A.7 Burn registry monotonic

**Claim:** `BurnRegistry.total_burned` and `BurnRegistry.seq` only increase.

**Approach:** grep for every `total_burned = ` and `seq = ` assignment in `burn::lib.rs`. Confirm no decrement.

### A.8 Burn-via-program atomic

**Claim:** `burn::burn_via_program` succeeds only if the Token-2022 `Burn` CPI succeeds first. The receipt account is not created if the burn fails.

**Approach:** read the handler. Confirm the order: `token_2022::burn(...)` → `**receipt_account_data = ...`. Solana's transaction model ensures full revert if either fails.

---

## B. Per-program invariants

### B.1 grid-token

- [ ] `GridConfig.bump` is the canonical bump (Anchor `seeds = [...]` with `bump` keyword).
- [ ] `GridConfig.minted_so_far` is `u128` or has overflow protection (1B × 10^9 fits in `u64`, so `u64` is OK).
- [ ] `set_metadata` `name_len <= META_NAME_MAX = 32`, `symbol_len <= 10`, `uri_len <= 200`.
- [ ] `transfer_mint_authority` does NOT allow setting the new authority to `None` (would brick the mint).
- [ ] `freeze_authority`: confirm policy — kept at admin/Foundation multisig for sanctions compliance, OR set to `None` for non-custodial guarantee. Document the decision.
- [ ] No `unsafe` Rust code anywhere.

### B.2 emission

- [ ] `claim_epoch` requires `epoch_id == config.epoch_counter + 1` (monotone, no gaps, no replays).
- [ ] `EpochClaim` PDA seeded by `(mint, epoch_id)` → re-init impossible (Anchor `init` enforces).
- [ ] `claim_epoch` requires `epoch_start_unix < epoch_end_unix`.
- [ ] `claim_epoch` requires `epoch_end_unix - epoch_start_unix <= MAX_EPOCH_SECS` (sanity cap, e.g., 14 days, to bound the budget per call).
- [ ] `budget_for_window(s, e)` returns 0 for `e <= tge_unix` (no pre-TGE emission).
- [ ] `distribute_to` rejects if `epoch_claim.finalized`.
- [ ] `distribute_to` rejects if `amount > epoch_claim.total_reward - epoch_claim.distributed`.
- [ ] `finalize_epoch` sets `finalized = true`; subsequent `distribute_to` reject.
- [ ] Vault authority PDA seeded by `["emission-vault-authority", mint]` — external party cannot sign.
- [ ] `claim_epoch` and `distribute_to` both require `billing_signer` (`has_one`).

### B.3 vesting

- [ ] `ProviderVesting` PDA seeded by `["provider-vesting", mint, provider]` — unique per (mint, provider).
- [ ] `initialize_provider` is `init` (not `init_if_needed`) — cannot re-create.
- [ ] `record_deposit` accepts `billing_signer` (called from emission's `distribute_to` CPI in production).
- [ ] `upgrade_tier` requires `new_tier > current_tier` (strict greater, no downgrade or same).
  - Note: vector 10 above says "same is no-op accepted"; the audit must rule on whether `>=` or `>`. Recommend `>`.
- [ ] `withdraw_unlocked` only transfers vested portion; uses `u128` intermediate in `vested_amount`.
- [ ] `early_unlock` requires `now - last_early_unlock >= 365 days`.
- [ ] `early_unlock` Burn CPI precedes Transfer CPI.
- [ ] Vesting vault authority PDA seeded by `["vesting-vault-authority", mint, provider]` — provider cannot sign for vault directly.

### B.4 staking

- [ ] `StakingPool` is `init` (one-time).
- [ ] `annual_yield_bps` is set only in `initialize_pool`; no setter.
- [ ] `stake` enforces `lock_secs >= MIN_STAKE_SECS = 30 days`.
- [ ] `unstake` rejects before `staked_at + MIN_STAKE_SECS`.
- [ ] `claim_yield` cannot exceed `accrued_yield`.
- [ ] `accrue_yield` uses `last_accrual` to compute the delta; cannot be back-dated.
- [ ] `customer_stake_for_discount` discount cap = `MAX_DISCOUNT_BPS = 2500`.
- [ ] `DiscountVoucher.consumed` set true AND account closed in `redeem_discount_voucher`.
- [ ] `compute_weight` is a pure view (`Context::accounts` are NOT `mut`).

### B.5 burn

- [ ] `BurnRegistry` is `init` (one-time).
- [ ] `rotate_attestor` requires `admin` signer.
- [ ] `record_burn` requires `attestor` signer (`has_one`).
- [ ] `burn_via_program` requires BOTH `attestor` and `source_authority` signers.
- [ ] `BurnReceipt` PDA seeded by `(registry, seq)` → replay-resistant.
- [ ] `burn_via_program` Burn CPI precedes receipt write (atomicity).

---

## C. Cross-cutting concerns

### C.1 Token-2022 specifics

- [ ] All SPL CPIs use `anchor_spl::token_2022` (not legacy `token`).
- [ ] The mint is created with Token-2022 program owner (TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb).
- [ ] No Token-2022 extension is used in v0 (transfer hooks, fees, freeze) beyond what's documented.
  - If `freeze` extension is enabled, document the policy.
- [ ] ATA derivation uses the ATA-2022 program (`ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL`).

### C.2 Anchor 0.31 specifics

- [ ] `#[account(init, seeds = ..., bump, payer = ..., space = 8 + ...)]` patterns are consistent.
- [ ] `has_one = X` constraints used for signer/account-key validation (rather than `require!(ctx.accounts.X.key() == ...)`).
- [ ] Discriminator collisions: confirm none of the 5 programs share a struct name that would collide; Anchor 0.31 uses `sighash("account:<TypeName>")` for the 8-byte discriminator.
- [ ] `init_if_needed` is used sparingly; document every usage with a justification.
- [ ] No deprecated APIs without `#[allow(deprecated)]` + comment.

### C.3 Rust hygiene

- [ ] `cargo clippy --workspace --all-targets -- -D warnings` clean.
- [ ] `cargo fmt --all -- --check` clean.
- [ ] No `unsafe` blocks (verify via `grep -rn 'unsafe' programs/`).
- [ ] No `unwrap()` on Result/Option in non-test code (use `?` + proper error codes).
- [ ] `overflow-checks = true` in `[profile.release]` (verified in `Cargo.toml`).
- [ ] All `#[error_code]` variants documented with a 1-line description.

### C.4 PDA seed integrity

- [ ] Every PDA seed list is enumerated in [`../README.md`](../README.md) §"PDA seed table".
- [ ] No two PDAs across the 5 programs use overlapping seeds + same first-seed-string + same program ID prefix (verify by inspecting the deterministic derivation).
- [ ] No seed includes a user-controlled long byte slice that could enable PDA-grinding to collide.

### C.5 Reentrancy / CPI safety

- [ ] No program calls into a user-controlled program (only Token-2022 + System).
- [ ] State updates (account writes) happen BEFORE any CPI where the CPI's success affects the user-visible state (e.g., `withdrawn += amount` before the Transfer CPI in `withdraw_unlocked`).
- [ ] No instruction enables a CPI-chain that could re-enter the same program (Solana's model makes this hard, but verify).

### C.6 Replay protection

- [ ] Every "one-time" instruction uses an `init` PDA (Anchor enforces).
- [ ] Every "monotone" counter is checked before increment.
- [ ] Every receipt / claim PDA is seeded by something deterministic that the operator cannot replay (e.g., `epoch_id`, `seq`, `(mint, provider)`).

### C.7 Signer checks

- [ ] Every privileged action verifies its signer via `Signer<'info>` + (if applicable) `has_one` or explicit `require!(ctx.accounts.x.key() == ...)`.
- [ ] No instruction lets `payer` masquerade as `admin` / `attestor` / `provider`.

---

## D. Deployment and operations

### D.1 Build reproducibility

- [ ] `anchor build` is reproducible across machines using the same Anchor + Solana CLI + Rust pin. Auditor should reproduce the .so files and confirm sha256 parity with iogrid's bundled artefacts.

### D.2 Program upgrade authority

- [ ] At launch, the BPF-Loader-Upgradeable upgrade authority for every program is the deployer keypair (auditor verifies via `solana program show`).
- [ ] Operator runbook says: rotate upgrade authority to Squads 3-of-5 multisig PDA before TGE.
- [ ] Audit should comment on whether `scripts/upgrade.sh` enforces the rotation (it does not in v0 — flag for operator discipline).

### D.3 IDL publish

- [ ] On-chain IDL accounts are populated post-deploy via `scripts/idl-publish.sh`.
- [ ] IDL authority is the deployer keypair (auditor verifies via `anchor idl fetch`); rotate to Squads pre-TGE.

### D.4 Emergency response

- [ ] What happens if a Critical-severity bug is found post-launch?
  - Squads multisig pauses the affected program via `upgrade.sh` to a "no-op stub" (a program with the same ID that rejects every instruction). This is the canonical Solana pause pattern.
  - The audit should comment on whether iogrid has a prepared no-op stub binary for each program (it does NOT in v0 — recommend adding).

### D.5 Bug bounty

- [ ] Immunefi program drafted (not yet open). Tiered payouts:
  - Critical (token-supply manipulation, treasury drain): $50K – $250K
  - High (per-user fund loss): $10K – $50K
  - Medium (DoS, replay) : $1K – $10K
  - Low / Informational: $0 – $1K
- [ ] Bounty scope = all 5 programs on mainnet.

---

## E. Known-acceptances (documented)

These are items where the auditor's recommendation may differ from iogrid's design intent.
Each requires an explicit decision in the engagement letter:

### E.1 Token-2022 freeze authority retention

- **Position:** iogrid retains the freeze authority at launch for sanctions compliance.
- **Auditor concern:** freeze authority enables censorship; some regulators view it as
  a centralization signal.
- **Decision:** retain for compliance. Disclosed in [`legal/token-disclaimer.md`](../../legal/token-disclaimer.md)
  §7. Audit report should note the trade-off explicitly.

### E.2 `init_if_needed` on `GridMetadata`

- **Position:** `set_metadata` uses `init_if_needed` so the same instruction handles
  first-set and updates.
- **Auditor concern:** `init_if_needed` can sometimes be exploited for account-substitution
  attacks (mitigated by `seeds = [...]` + `bump` constraint).
- **Decision:** auditor judgment. Mitigation: `has_one = admin` ensures only the admin
  signer can call.

### E.3 Deprecated `transfer` vs `transfer_checked`

- **Position:** iogrid uses `transfer` with `#[allow(deprecated)]`.
- **Auditor concern:** `transfer_checked` verifies mint + decimals at the SPL layer.
  `transfer` does not. iogrid's internal accounts validate the mint via `has_one`, but
  the SPL-level safety is reduced.
- **Decision:** migrate if auditor recommends. Cost: ~1 day engineering + re-audit of
  the diff.

### E.4 No `unstake_request` two-phase cool-down

- **Position:** v0 has only `unstake`, rejected before `MIN_STAKE_SECS = 30 days`.
- **Auditor concern:** instant `unstake` post-min-stake enables fast yank-and-go which
  could harm pool yield calculations.
- **Decision:** add a 7-day `unstake_request` window in v0.1 if auditor recommends.
  Tracked separately.

### E.5 No emergency-pause stub binary

- **Position:** v0 does not ship a "no-op stub" binary for each program.
- **Auditor concern:** if a Critical bug is found post-launch, iogrid must compile a new
  binary on the spot (under time pressure). Industry best practice is to pre-compile a
  stub for every program and store its sha256 in the operator runbook.
- **Decision:** add no-op stubs in v0.1 (tracked separately).

---

## F. Sign-off

After the audit:

- [ ] Auditor delivers final report.
- [ ] All Critical / High findings: code merged + tests added + re-audit verified.
- [ ] Medium findings: triaged, accepted, or fixed.
- [ ] Low / Informational findings: documented in this checklist's "Known-acceptances" or filed as follow-up issues.
- [ ] Audit-target commit tagged `v0.1.0-audited`.
- [ ] Report published at `iogrid.org/security/audit-2026-ottersec.pdf`.
- [ ] Squads multisig + Foundation governance ready.
- [ ] Mainnet deploy proceeds.

---

*End of auditor checklist.*
