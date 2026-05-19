# $GRID audit — test vectors

Known-good signatures and expected state transitions per program. Auditors should be able
to verify, byte-for-byte, that the on-chain Anchor instructions produce the documented
state changes from the given inputs.

Each vector has:
- **Inputs** (instruction arg + account roles)
- **Expected on-chain state delta**
- **Expected events / logs**
- **Negative paths** (rejected with what error code)

The instruction encodings follow Anchor 0.31 IDL conventions (8-byte discriminator + LE
borsh-encoded args). Discriminators are computed from `sighash("global:<instruction_name>")[..8]`.

> Important: discriminator values below are **placeholders** computed from the function
> names listed. Auditors should regenerate them from the actual IDL after `anchor build`.
> The format is correct; the specific bytes depend on Anchor's discriminator-hashing of the
> exact instruction names in the published IDL.

---

## grid-token program

### Vector 1 — `initialize_config`

**Goal:** bootstrap the `GridConfig` PDA at seeds `["grid-config", mint]`.

| Input | Value |
|-------|-------|
| `admin` (signer) | `Adm1n...` (deployer keypair) |
| `mint` | `GRiD...` (caller-created Token-2022 mint account, uninitialized) |
| `config` PDA | derived from `["grid-config", mint]` |
| Instruction args | none |

**Expected `GridConfig` after:**

```rust
GridConfig {
  admin:           admin.key(),
  mint:            mint.key(),
  hard_cap:        1_000_000_000_000_000_000, // 1B * 10^9
  decimals:        9,
  minted_so_far:   0,
  authority_locked: false,
  bump:            <derived bump>,
}
```

**Expected event:** `ConfigInitialized { admin, mint, hard_cap }`.

**Negative paths:**

| Modification | Expected failure |
|--------------|------------------|
| Re-run with same `mint` | Anchor `init` constraint rejects (account already in use) |
| `mint` decimals != 9 | not enforced here (decimals come from `initialize_mint`, NOT verified at config init) — note for auditor |

---

### Vector 2 — `initialize_mint` (after `initialize_config`)

**Goal:** initialize the Token-2022 mint with 9 decimals + admin as mint/freeze authority.

| Input | Value |
|-------|-------|
| `admin` (signer) | `Adm1n...` |
| `mint` (mut) | `GRiD...` (created with right space + owner = Token-2022 program) |
| `token_program` | `TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb` (Token-2022 program) |
| Instruction args | none |

**Expected mint state after:**

```rust
Mint {
  mint_authority:   Some(admin.key()),
  supply:           0,
  decimals:         9,
  is_initialized:   true,
  freeze_authority: Some(admin.key()),
}
```

**Expected event:** `MintInitialized { mint, decimals: 9 }`.

---

### Vector 3 — `mint_to_recipient` (pre-handoff distribution)

**Goal:** mint 100M $GRID (= treasury allocation) to a recipient ATA, within hard cap.

| Input | Value |
|-------|-------|
| `admin` (signer) | `Adm1n...` |
| `config` (mut) | `GridConfig` PDA |
| `mint` (mut) | `GRiD...` |
| `recipient` (mut) | recipient's ATA for `mint` |
| Instruction args | `amount = 100_000_000 * 10^9` (= 100M $GRID with decimals) |

**Expected delta:**

- `mint.supply` += 100_000_000_000_000_000
- `config.minted_so_far` += 100_000_000_000_000_000
- `recipient.amount` += 100_000_000_000_000_000

**Negative paths:**

| Modification | Expected failure |
|--------------|------------------|
| Attempt 901_000_000 $GRID after this | `MintWouldExceedCap` (would push minted_so_far past 1B) |
| Wrong `admin` signer | `ConstraintHasOne` from `has_one = admin` |
| Run after `lock_authorities` was called | `AuthoritiesLocked` |

---

### Vector 4 — `transfer_mint_authority` then `lock_authorities`

**Goal:** rotate mint authority to the `emission` program's PDA, then permanently lock.

| Step | Action | Expected state |
|------|--------|----------------|
| 1 | `transfer_mint_authority(new = emission_pda)` | Token-2022 mint's mint_authority = `emission_pda`; config emits `MintAuthorityTransferred` event |
| 2 | `lock_authorities()` | `config.authority_locked = true` |
| 3 | Attempt `mint_to_recipient` | `AuthoritiesLocked` |
| 4 | Attempt `transfer_mint_authority` again | `AuthoritiesLocked` |

After step 2, ONLY the emission program's halving curve can mint new $GRID. This is the
single most important invariant in the audit. Falsification target: find any code path
that toggles `authority_locked` back to `false` or that mints without going through the
emission PDA.

---

## emission program

### Vector 5 — `initialize` (one-time)

| Input | Value |
|-------|-------|
| `admin` (signer) | `Adm1n...` |
| `mint` | `GRiD...` |
| Instruction args | `tge_unix = 1748563200` (UTC anchor for halving curve); `billing_signer = BiL1ing...` |

**Expected `EmissionConfig` after:**

```rust
EmissionConfig {
  admin:           admin.key(),
  mint:            mint.key(),
  tge_unix:        1748563200,
  billing_signer:  BiL1ing.pubkey(),
  epoch_counter:   0,
  vault:           emission_vault_pda,
  bump:            <derived>,
}
```

**Negative paths:**

| Modification | Expected failure |
|--------------|------------------|
| Re-run | Anchor `init` rejects (account exists) |
| `tge_unix` < 1 (sentinel) | `InvalidTgeAnchor` |

---

### Vector 6 — `claim_epoch` (week 1, year 0)

**Goal:** billing-svc claims week-1 emission budget.

| Input | Value |
|-------|-------|
| `billing_signer` (signer) | `BiL1ing...` |
| `config` (mut) | `EmissionConfig` PDA |
| `vault` (mut) | emission vault ATA |
| `mint` (mut) | `GRiD...` |
| Instruction args | `epoch_id = 1`, `epoch_start_unix = tge_unix`, `epoch_end_unix = tge_unix + 7*86400` |

**Halving curve math (off-chain reference):**

`budget_for_window(start, end)` = ∫ on the piecewise emission curve.

For year 0 (rate = 50M/year = 50,000,000 * 10^9 base units / 31,536,000 seconds), 7 days =
604,800 seconds:

```
budget = (50_000_000 * 10^9) * (604_800 / 31_536_000)
       = (50_000_000 * 10^9) * 0.01918...
       ≈ 958_904_109_589_041
```

**Expected delta:**

- `config.epoch_counter` = 1
- `EpochClaim {id:1, total_reward: 958_904_109_589_041, distributed: 0, finalized: false}` PDA created
- `mint.supply` += 958_904_109_589_041
- `vault.amount` += 958_904_109_589_041

**Negative paths:**

| Modification | Expected failure |
|--------------|------------------|
| `epoch_id = 2` (skipping 1) | `InvalidEpochId` (must equal counter + 1) |
| Re-run with `epoch_id = 1` | Anchor `init` rejects on `EpochClaim` PDA |
| `epoch_start_unix < tge_unix` | `EpochWindowOutOfRange` |
| Wrong `billing_signer` | `ConstraintHasOne` |
| `epoch_end_unix` > 32 years post-TGE | `EpochWindowOutOfRange` (curve returns 0 anyway, but explicit reject) |

---

### Vector 7 — `distribute_to` (week 1, provider A gets 40% share)

| Input | Value |
|-------|-------|
| `billing_signer` (signer) | `BiL1ing...` |
| `epoch_claim` (mut) | week-1 `EpochClaim` PDA |
| `provider` | `ProV1der...` (the wallet, not a signer) |
| `provider_vesting` (mut) | derived from `["provider-vesting", mint, provider]` |
| Instruction args | `amount = 958_904 * 0.40 = 383_561_643_835_616` |

**Expected delta:**

- `epoch_claim.distributed` += 383_561_643_835_616
- `provider_vesting`'s vault ATA receives 383_561_643_835_616 (via CPI to `vesting::record_deposit`)
- `VestingDeposit` PDA created with `deposit_id = provider_vesting.deposit_counter + 1`, `amount = 383_561...`, `deposited_at = now`, `tier_at_deposit = provider's current tier`

**Negative paths:**

| Modification | Expected failure |
|--------------|------------------|
| `amount > epoch_claim.total_reward - epoch_claim.distributed` | `InsufficientEpochBudget` |
| Epoch already finalized | `EpochFinalized` |
| `provider_vesting` belongs to wrong provider | `ConstraintSeeds` from PDA derivation |

---

## vesting program

### Vector 8 — `vested_amount` math (boundary cases)

The function: `vested_amount(amount, deposited_at, now, tier) -> u64`.

Mathematical definition (Standard tier example: cliff=30d, vest=60d):

```
let dt = now - deposited_at
if dt < cliff { 0 }
else if dt >= cliff + vest { amount }
else {
  // Linear vest. u128 intermediate to avoid u64 overflow.
  let elapsed = dt - cliff
  ((amount as u128) * (elapsed as u128) / (vest as u128)) as u64
}
```

| Case | `dt` | Tier | Expected |
|------|------|------|----------|
| pre-cliff | 0 | Standard | 0 |
| at cliff | cliff = 30d | Standard | 0 (boundary: `dt < cliff` is `<`, but `dt == cliff` should be 0 since elapsed = 0; auditor verify) |
| mid-vest | cliff + vest/2 = 60d | Standard | amount / 2 |
| at full vest | cliff + vest = 90d | Standard | amount |
| post-vest | cliff + vest + 1y | Standard | amount |
| pre-cliff Maximum tier | 364d | Maximum | 0 |
| mid-vest Maximum tier | 365 + 730/2 = 730d | Maximum | amount / 2 |

**Test vector:** `amount = 1_000_000_000_000`, `deposited_at = 0`, `now = 5_184_000` (60 days), tier = Standard.

Expected: `1_000_000_000_000 * (5_184_000 - 2_592_000) / 5_184_000 = 1_000_000_000_000 * 2_592_000 / 5_184_000 = 500_000_000_000`.

**Negative paths (audit must verify):**

- `now == deposited_at` → returns 0 (no integer division by 0; `dt < cliff` short-circuits).
- `amount = u64::MAX, now = deposited_at + cliff + vest`: result must equal `amount` exactly (no overflow). Achieved via u128 intermediate.
- `now < deposited_at`: caller responsibility; in practice the call sites use the clock sysvar so `now >= deposited_at` is guaranteed.

---

### Vector 9 — `early_unlock` (50% burn penalty)

**Goal:** provider with 10,000 $GRID locked early-unlocks; receives 5,000, burns 5,000.

| Input | Value |
|-------|-------|
| `provider` (signer) | `ProV1der...` |
| `provider_vesting` (mut) | `["provider-vesting", mint, provider]` |
| `vesting_deposit` (mut) | one deposit with amount = 10,000 * 10^9, withdrawn = 0 |
| `vesting_vault` (mut) | PDA-owned ATA |
| `provider_ata` (mut) | provider's destination ATA |
| `mint` (mut) | `GRiD...` |

**Expected delta:**

- `vesting_deposit.withdrawn` += 10,000 * 10^9
- `mint.supply` -= 5,000 * 10^9 (the burn)
- `vesting_vault.amount` -= 10,000 * 10^9
- `provider_ata.amount` += 5,000 * 10^9
- `provider_vesting.last_early_unlock` = now

**Atomicity check:** the `Burn` CPI for the penalty MUST be ordered BEFORE the `Transfer`
CPI for the payout. If the Burn fails, the transaction reverts and the Transfer is undone.
Auditor verify by reading the lib.rs source: the CPIs appear in this order inside the
`early_unlock` handler.

**Negative paths:**

| Modification | Expected failure |
|--------------|------------------|
| Re-run within 365 days | `EarlyUnlockOnCooldown` |
| Wrong `provider` signer | `ConstraintHasOne` (provider_vesting has_one = provider) |
| `withdrawn == amount` (already withdrawn) | `NothingToUnlock` |
| Penalty bps != 5_000 in account state | impossible — `EARLY_UNLOCK_PENALTY_BPS` is a Rust `const`, not stored in any account |

---

### Vector 10 — `upgrade_tier` ratcheting (no downgrades)

| Step | Action | Expected |
|------|--------|----------|
| Init | provider on Standard (tier=0) | `provider_vesting.tier = 0` |
| 1 | `upgrade_tier(new_tier = 2 /* Conviction */)` | `tier = 2` |
| 2 | `upgrade_tier(new_tier = 3 /* Maximum */)` | `tier = 3` |
| 3 | `upgrade_tier(new_tier = 1 /* Loyalty */)` | rejected with `TierDowngradeNotAllowed` |
| 4 | `upgrade_tier(new_tier = 3)` (no-op same) | accepted, no-op |

Subsequent `record_deposit` calls use the new tier at the time of the call (per-deposit
tier snapshot, see `VestingDeposit::tier_at_deposit`).

---

## staking program

### Vector 11 — `compute_weight` (pure view)

```
fn compute_weight(staked_amount: u64) -> u32 {
  if staked_amount < MIN_STAKE_AMOUNT { return 0; }
  // Linear ramp from 1.00x at MIN_STAKE to 2.00x at MAX_STAKE_AMOUNT
  // Encoded as fixed-point: 10000 = 1.00x, 20000 = 2.00x
  let bonus = (staked_amount as u128).saturating_sub(MIN_STAKE_AMOUNT as u128)
              * 10000u128
              / (MAX_STAKE_AMOUNT - MIN_STAKE_AMOUNT) as u128;
  let weight = 10000u128 + bonus.min(10000u128);
  weight as u32
}
```

| Case | `staked_amount` | Expected weight |
|------|-----------------|-----------------|
| zero | 0 | 0 |
| below min | MIN_STAKE_AMOUNT - 1 | 0 |
| at min | MIN_STAKE_AMOUNT | 10000 (= 1.00x) |
| half-ramp | (MIN + MAX) / 2 | 15000 (= 1.50x) |
| at max | MAX_STAKE_AMOUNT | 20000 (= 2.00x) |
| above max | MAX_STAKE_AMOUNT * 2 | 20000 (capped) |

**Mutation test:** if `compute_weight`'s `Context::accounts` ever becomes `mut`, the audit
must flag it. This function is intended to be a pure view.

---

### Vector 12 — `customer_stake_for_discount` (30-day lock)

| Input | Value |
|-------|-------|
| `customer` (signer) | `Cu5tomer...` |
| `pool` (mut) | `StakingPool` PDA |
| `voucher` (init) | `DiscountVoucher` |
| Instruction args | `amount = 5,000 * 10^9`, `lock_secs = 30 * 86400` |

**Expected delta:**

- `voucher.owner = customer.key()`, `voucher.stake_amount = 5,000 * 10^9`, `voucher.lock_end = now + 30 days`, `voucher.discount_bps = compute_discount(5,000 * 10^9)`, `voucher.consumed = false`
- `pool.stake_vault.amount` += 5,000 * 10^9

**Negative paths:**

| Modification | Expected failure |
|--------------|------------------|
| `lock_secs < MIN_STAKE_SECS (30 days)` | `StakeLockTooShort` |
| `redeem_discount_voucher` before `lock_end` | `VoucherStillLocked` |
| Re-redeem same voucher | account already closed; PDA fetch returns AccountNotInitialized |

---

### Vector 13 — discount cap

| `amount` | Expected `discount_bps` |
|----------|-------------------------|
| 0 | 0 (no voucher created) |
| MIN_STAKE_AMOUNT | small (linear ramp lower bound) |
| (MIN + MAX) / 2 | mid-ramp |
| MAX_STAKE_AMOUNT | 2500 (= 25%, MAX_DISCOUNT_BPS) |
| MAX * 2 | 2500 (capped) |

---

## burn program

### Vector 14 — `initialize_registry`

| Input | Value |
|-------|-------|
| `admin` (signer) | `Adm1n...` |
| `mint` | `GRiD...` |
| Instruction args | `attestor = Atte5tor...` |

**Expected `BurnRegistry` after:**

```rust
BurnRegistry {
  admin:         admin.key(),
  mint:          mint.key(),
  attestor:      attestor,
  total_burned:  0,
  seq:           0,
  bump:          <derived>,
}
```

---

### Vector 15 — `record_burn` (write-only receipt, no CPI)

Used when a burn happened off-chain (e.g., legacy burn to incinerator before `burn_via_program`
landed) and we want a receipt for audit traceability.

| Input | Value |
|-------|-------|
| `attestor` (signer) | `Atte5tor...` |
| `registry` (mut) | `BurnRegistry` PDA |
| `receipt` (init) | `BurnReceipt` PDA seeded by `(registry, seq)` |
| Instruction args | `amount = 1_000_000_000_000` (= 1,000 $GRID), `audit_hash = [u8; 32]` |

**Expected delta:**

- `registry.total_burned` += 1_000_000_000_000
- `registry.seq` += 1
- `BurnReceipt { seq, amount, audit_hash, block_height, bump }` created

**Negative paths:**

| Modification | Expected failure |
|--------------|------------------|
| Re-run with same effective `seq` | Anchor `init` rejects on `BurnReceipt` PDA (seeded by `seq`) |
| Wrong `attestor` | `ConstraintHasOne` |

---

### Vector 16 — `burn_via_program` (atomic burn + receipt)

The canonical "billing-svc daily burn" path.

| Input | Value |
|-------|-------|
| `attestor` (signer) | `Atte5tor...` (billing-svc HSM) |
| `source_authority` (signer) | hot-wallet authority |
| `source_ata` (mut) | hot-wallet ATA for `mint` |
| `mint` (mut) | `GRiD...` |
| `registry` (mut) | `BurnRegistry` PDA |
| `receipt` (init) | `BurnReceipt` PDA seeded by `(registry, seq)` |
| Instruction args | `amount = 50_000_000_000` (50 $GRID daily carve, example), `audit_hash = sha256(billing_svc_emission_log_entry)` |

**Atomicity:** the `Burn` CPI to Token-2022 happens BEFORE the `BurnReceipt` PDA is
written. If the `Burn` reverts (e.g., insufficient balance in `source_ata`), the entire
transaction reverts and no receipt is written. Auditor must inspect the source ordering.

**Expected delta:**

- `mint.supply` -= 50_000_000_000
- `source_ata.amount` -= 50_000_000_000
- `registry.total_burned` += 50_000_000_000
- `registry.seq` += 1
- `BurnReceipt` PDA created

---

## Audit-time invariants checklist (machine-checkable)

For each invariant, the auditor should run a brief negation attempt and confirm the
constraint holds:

1. `GridConfig.minted_so_far` monotonic increasing (never decreased).
2. `GridConfig.authority_locked` once `true`, never `false` again.
3. `mint.supply <= GRID_HARD_CAP` for all reachable states.
4. `EmissionConfig.epoch_counter` increases by exactly 1 per `claim_epoch`.
5. `EpochClaim.total_reward >= EpochClaim.distributed` after every `distribute_to`.
6. `EpochClaim.finalized` once `true`, never `false`.
7. `VestingDeposit.withdrawn` monotonic increasing.
8. `vested_amount(_, _, now, _)` monotonic non-decreasing in `now`.
9. `EARLY_UNLOCK_PENALTY_BPS == 5_000` (compile-time const).
10. `ProviderVesting.last_early_unlock` cooldown ≥ 365 days enforced in `early_unlock`.
11. `BurnRegistry.total_burned` monotonic increasing.
12. `BurnReceipt` PDAs unique per `(registry, seq)`.
13. `MIN_STAKE_SECS == 30 days` enforced in `unstake` and `customer_stake_for_discount`.
14. `MAX_DISCOUNT_BPS == 2_500` cap on `compute_discount`.
15. `DiscountVoucher.consumed = true` AND account closed in same redemption instruction.
16. `StakingPool.annual_yield_bps` not user-settable post-init.
17. `compute_weight` accounts read-only (`mut` would be a regression).
18. `transfer_mint_authority` → mint authority Pubkey == `emission` PDA before `lock_authorities`.

---

## Reproducible test bundle

The full set of vectors is encoded as TS test cases in `contracts/tests/audit-vectors.ts`
(forthcoming — auditor can drive the encoding in their own framework if preferred). The
TS bundle:

1. Boots a local validator with `scripts/local-validator.sh --clean`.
2. Executes each vector as a real on-chain transaction.
3. Reads back the resulting account state via Anchor's typed client.
4. Asserts every expected delta in this document.

When new programs ship or instructions change, this document and the TS test bundle are
updated together. The audit firm should ensure the bundle's CI is green on the
audit-target commit.
