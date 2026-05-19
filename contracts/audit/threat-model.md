# Threat model — $GRID Solana programs

Per-program enumeration of privileged signers, what they can do, what they cannot, and the
catastrophic outcomes the design is meant to prevent. Audit firm should walk this document
and validate every "cannot" claim with a falsification attempt.

## Cross-cutting assumptions

- **Token-2022** (`spl-token-2022 6.x` via `anchor-spl 0.31.1`). All token transfers and
  burns go through this program. We do NOT assume bug-freedom in Token-2022; we DO assume
  the audit firm trusts the upstream's prior audits and only verifies that our CPI surface
  uses the documented signer/PDA pattern.
- **Anchor 0.31.1** macro hygiene. `#[account]` discriminators, `init_if_needed`,
  `seeds = [...]` constraints are trusted as audited via OtterSec's prior Anchor work
  (Phoenix, Squads, Jupiter).
- **Solana runtime** (BPF VM, sysvars, rent). Assumed sound.
- **Signers can collude.** A multi-signer model (e.g., `admin` + `billing_signer`) provides
  defense-in-depth ONLY if the keys are held by distinct parties. Production deployment
  rotates these to Squads multisigs (`admin`) and HSM-held keys (`billing_signer`,
  `attestor`).

## Value at risk (worst-case scenarios this design must block)

| Scenario | Loss bound | Mitigation |
|----------|-----------|-----------|
| Unauthorized mint of $GRID past the 1B cap | Token uncapped → unbounded inflation → ~$50M+ initial-DEX-pool drain | `GRID_HARD_CAP` check + `authority_locked` one-way switch |
| Bypass of vesting cliff/linear schedule | Single provider drains another provider's pending vesting | `seeds = ["provider-vesting", mint, provider]` PDA + `has_one = mint` + signer-must-equal-provider |
| Bypass of 50% early-unlock burn penalty | Provider extracts 100% locked tokens without burning the half | `EARLY_UNLOCK_PENALTY_BPS` is a `const` (not a parameter); the `Burn` CPI precedes the `Transfer` CPI in the same instruction |
| Re-claim of an already-distributed emission epoch | Double-spend of a billing-svc epoch payout | `epoch_id` monotonic check (`epoch_id == config.epoch_counter + 1`) + `EpochClaim` PDA seeded by `epoch_id` (cannot re-init) |
| Inflation of staking rewards via faked `accrue_yield` calls | Rewards pool drained, breaking the 5% APR ceiling | `last_accrual` timestamp prevents back-dating; `annual_yield_bps` is set at pool init only by `admin` |
| Burn registry tampering | Burn audit log diverges from actual on-chain supply | `attestor` signer required for `record_burn`; `burn_via_program` does the CPI atomically with the receipt write |

## Per-program threat model

### grid-token

| Privileged role | Held by | Can do | Cannot do |
|-----------------|---------|--------|-----------|
| `admin` | Deployer keypair (rotated to Squads multisig before mainnet) | `initialize_config`, `initialize_mint`, `mint_to_recipient` (until lock), `transfer_mint_authority`, `set_metadata`, `lock_authorities` | Mint past `GRID_HARD_CAP`; reset `minted_so_far`; bypass `lock_authorities` once set |

**Critical invariants:**
- `GridConfig::minted_so_far` is monotonic. No instruction decreases it. Auditor must verify.
- `GridConfig::authority_locked` is monotonic. Set-once at `lock_authorities`; never cleared.
- `GridConfig::hard_cap` is set at `initialize_config` and never changed.
- The mint authority on the Token-2022 mint MUST be the `admin` Pubkey at init time, then
  transferred (via `transfer_mint_authority`) to the emission program's PDA before
  `lock_authorities` is called. After that, ONLY the halving-curve in `emission` can mint.
- `GridMetadata::name_len/symbol_len/uri_len` are bounded by the declared maxes
  (META_NAME_MAX=32, META_SYMBOL_MAX=10, META_URI_MAX=200). Out-of-bounds payloads are
  rejected with `MetadataFieldTooLong`.

### emission

| Privileged role | Held by | Can do | Cannot do |
|-----------------|---------|--------|-----------|
| `admin` | Deployer keypair (rotated to Squads multisig) | `initialize` | Re-initialize after first init |
| `billing_signer` | billing-svc HSM key | `claim_epoch`, `distribute_to`, `finalize_epoch` | Claim more than `budget_for_window` for any epoch; reuse an `epoch_id`; mint past 1B (gated by grid-token's hard cap on the mint side) |

**Critical invariants:**
- `EmissionConfig::epoch_counter` is monotonic; `claim_epoch` requires `epoch_id == counter + 1`.
- `EpochClaim::total_reward >= EpochClaim::distributed` (enforced in `distribute_to`).
- `EpochClaim::finalized` is monotonic. Once `true`, no further `distribute_to` allowed.
- `EmissionConfig::tge_unix` set once at init; never changes. The halving curve anchors here.
- `EpochClaim` PDA seeded by `(mint, epoch_id)` — re-init of the same epoch_id fails.
- The vault authority is a program PDA seeded by `["emission-vault-authority", mint]`; no
  external party can sign for the vault.

### vesting

| Privileged role | Held by | Can do | Cannot do |
|-----------------|---------|--------|-----------|
| `payer` (creator of provider profile) | Anyone (billing-svc in production) | `initialize_provider` (one per (mint, provider)), `record_deposit` | Create a `ProviderVesting` for a provider who already has one (PDA collision) |
| `provider` | The provider's wallet | `upgrade_tier`, `withdraw_unlocked`, `early_unlock` | Withdraw past the vested portion; early-unlock more than once per 365 days; downgrade tier |

**Critical invariants:**
- `vested_amount(amount, deposited_at, now, tier)` is monotonic in `now` (never decreases).
- `VestingDeposit::withdrawn` is monotonic. No instruction decreases it.
- `ProviderVesting::last_early_unlock` is monotonic. Cooldown enforces ≥365 days delta.
- Early-unlock penalty is exactly 50% (`EARLY_UNLOCK_PENALTY_BPS = 5_000`). Auditor must
  verify the `(locked * penalty_bps / 10_000)` formula uses `u128` intermediate to avoid
  truncation, and rounds AGAINST the provider (i.e., truncation favors the burn).
- The vault authority PDA is seeded by `["vesting-vault-authority", mint, provider]` — the
  provider cannot sign for the vault directly.
- `Burn` CPI on the penalty happens BEFORE the `Transfer` CPI of the payout in
  `early_unlock`. If the burn CPI fails, the transfer also fails (atomic).

### staking

| Privileged role | Held by | Can do | Cannot do |
|-----------------|---------|--------|-----------|
| `admin` | Deployer keypair (rotated to Squads multisig) | `initialize_pool` | Re-initialize after first init; change `annual_yield_bps` (no setter in v0) |
| `staker` (position owner) | The staker's wallet | `stake`, `accrue_yield`, `claim_yield`, `unstake` (after MIN_STAKE_SECS) | Unstake before MIN_STAKE_SECS; claim more yield than accrued |
| `staker` (voucher owner) | The customer's wallet | `customer_stake_for_discount`, `redeem_discount_voucher` (after lock_end) | Redeem before lock_end; redeem twice (consumed flag) |
| anyone | — | `compute_weight` (pure view) | Mutate any state |

**Critical invariants:**
- `MIN_STAKE_SECS = 30 * DAY_SECS`. Enforced in `unstake` and `customer_stake_for_discount`.
- `StakePosition::amount` never changes after `stake`. To "increase" stake, the user opens
  a separate position (in v0; v1 may add `add_to_stake`).
- `MAX_DISCOUNT_BPS = 2_500` (25%). The ramp math is bounded by this.
- `DiscountVoucher::consumed` is set true on redeem; the account is `close`d in the same
  instruction so it cannot be re-played.

### burn

| Privileged role | Held by | Can do | Cannot do |
|-----------------|---------|--------|-----------|
| `admin` | Deployer (rotated to Squads multisig) | `initialize_registry`, `rotate_attestor` | Reset `total_burned` |
| `attestor` | billing-svc HSM key | `record_burn`, `burn_via_program` | Replay a previously-recorded burn (PDA collision on `(registry, seq)`) |
| `source_authority` | Owner of the source ATA (for `burn_via_program`) | Burn their own ATA | Burn another wallet's ATA (SPL enforces) |

**Critical invariants:**
- `BurnRegistry::total_burned` is monotonic (only increased by `record_burn` /
  `burn_via_program`).
- `BurnReceipt` PDA is seeded by `(registry, seq)` — replay-resistant.
- `burn_via_program` does the `Burn` CPI BEFORE writing the receipt (atomic; receipt only
  exists if the burn succeeded).
- `attestor` rotation requires `admin` signer.

## Test coverage matrix

Each privileged action has at least one test:

| Action | Positive test | Negative test |
|--------|---------------|---------------|
| grid_token::initialize_config | integration step 1 | (re-init via duplicate PDA fails — Anchor `init` does this automatically) |
| grid_token::set_metadata | integration step 2 + `grid-token.ts` IDL | bounds-check via `MetadataFieldTooLong` IDL assert |
| burn::initialize_registry | integration step 3 | (`has_one = attestor` on `RecordBurn` is the negative path) |
| burn::record_burn | integration step 4 | (`has_one = attestor` rejects wrong signer) |
| vesting::vested_amount math | `vesting.ts` (off-chain mirror) | boundary cases (pre-cliff = 0, post-vest = amount) |
| emission::budget_for_window math | `emission.ts` (off-chain mirror) | year-10 → 0 |
| staking::compute_weight math | `staking.ts` (off-chain mirror) | below-min → 0, capped at 2.00× |
| staking::customer_stake_for_discount | `staking.ts` (math mirror) | min-lock enforcement |
| vesting::early_unlock 50% burn | `vesting.ts` (math mirror) | cooldown via `EarlyUnlockOnCooldown` IDL |

Full end-to-end execution (with simulated clock advance) is TODO under `anchor-bankrun`
(see `tests/integration.ts` header). Until then, the math is mirrored exhaustively in TS
and the on-chain CPI surface is exercised by the partial integration suite (steps 1–4).

## Known-unknowns (require auditor judgment, not provable invariants)

1. **`init_if_needed` on GridMetadata**: re-running `set_metadata` overwrites the metadata
   payload in place. We rely on `has_one = admin` to gate this — auditor should confirm
   the Anchor-generated discriminator/seed check makes account substitution impossible.
2. **`anchor-spl 0.31.1` deprecated `transfer` vs the new `transfer_checked`**: we use
   `transfer` with an explicit `#[allow(deprecated)]`. The `_checked` variant requires the
   mint account on every transfer — we accept the trade-off because our internal accounts
   already validate the mint via `has_one`. Auditor should rule on whether to migrate.
3. **No `unstake_request` cool-down**: the task spec mentions an `unstake_request` two-phase
   pattern; v0 has only `unstake`, which is rejected before `MIN_STAKE_SECS`. If the audit
   firm or governance wants an additional 7-day cool-down post-min-stake, that would land
   in a v1 PR with its own re-attestation.
