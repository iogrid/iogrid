# Audit scope — $GRID Solana programs

This document defines what is **in scope** and what is **out of scope** for the pre-mainnet
audit (target firm: OtterSec; fallback: Halborn or Neodyme — see
[`docs/TOKENOMICS.md`](../../docs/TOKENOMICS.md) §"Token launch sequence").

## In scope

Five Anchor programs at `contracts/programs/`:

| Program        | Crate                | Audited surface                                         |
|----------------|----------------------|---------------------------------------------------------|
| `grid-token`   | `programs/grid-token`| Token-2022 mint init + authority management + metadata  |
| `emission`     | `programs/emission`  | Halving curve + per-epoch mint + paginated distribution |
| `vesting`      | `programs/vesting`   | Cliff + linear vest, 4 tiers, early-unlock w/ 50% burn  |
| `staking`      | `programs/staking`   | Provider routing-priority weight + customer discount    |
| `burn`         | `programs/burn`      | Buyback-and-burn registry + atomic burn CPI             |

All Rust source under `contracts/programs/*/src/**` and the corresponding test/IDL surfaces
under `contracts/tests/**`. The TypeScript test harness (`tests/*.ts`) is in scope for
**parity** — auditors should verify the off-chain math mirrors the on-chain implementation
exactly.

### Specific surfaces requiring extra scrutiny

1. **`emission::claim_epoch` budget math** — integer arithmetic that integrates the halving
   curve over an arbitrary `(start, end)` window. Off-by-one or rounding bugs here directly
   over/under-mint $GRID against the documented 500M provider rewards pool.
2. **`vesting::vested_amount`** — the linear-vest formula `amount * (now - deposited_at - cliff) / linear`
   uses `u128` intermediate to avoid `u64` overflow on large deposits. Boundary cases (now < cliff,
   now == cliff, now == cliff + linear, now > cliff + linear) all need explicit coverage.
3. **`vesting::early_unlock`** — 50% burn penalty + 12-month cooldown. Critical that the
   penalty CANNOT be set to 0% via account-injection (the bps is a `const` in the program,
   not a parameter). Also critical: the cooldown timer cannot be reset by closing/recreating
   the `ProviderVesting` account.
4. **PDA seed determinism** — the entire authority model depends on PDAs deriving uniquely
   from the mint (and provider) so a hostile party cannot create a "shadow" PDA that
   matches the expected one but carries different state. See the PDA seed table in
   `contracts/README.md` for the complete inventory.
5. **`grid_token::mint_to_recipient` hard cap** — the on-chain `minted_so_far` counter must
   never be re-zeroed by any code path; if it can, an attacker could mint past the 1B cap.
6. **`burn::burn_via_program` atomicity** — the Token-2022 `Burn` CPI MUST succeed in the
   same transaction as the receipt PDA write, otherwise the registry drifts from the actual
   on-chain supply.
7. **`staking::compute_weight`** — pure-view function; should NEVER mutate state. If a
   `Context::accounts` field becomes `mut` here in a future change, the auditor should flag
   that as a regression.

## Out of scope

- Off-chain coordinator code (`coordinator/billing-svc/**`, `coordinator/identity-svc/**`).
  The audit assumes a hostile billing-svc and validates that the on-chain programs
  survive a malicious signer for `billing_signer` / `attestor`.
- Web frontend (`web/**`).
- Brand assets (`brand/**`).
- Anchor framework itself (anchor-lang 0.31.1) — assumed audited via OtterSec's prior work
  on Phoenix / Squads / Jupiter (all Anchor 0.30+).
- `anchor-spl::token_2022` — same: covered by upstream Token-2022 audits.
- Streamflow vesting program (third-party, separate audit).
- Squads multisig program (third-party, separate audit).
- Raydium CLMM pool seeding (third-party, separate audit).
- Wormhole NTT bridge (third-party, separate audit).
- Deploy/upgrade scripts (`migrations/deploy.ts`, `scripts/upgrade.sh`) — these run from
  the deployer's machine; auditors should review them for foot-guns but the result of a
  bug here is a deploy mishap, not a token-supply or fund-loss bug.

## Versioning + reproducibility

- Anchor pinned to **0.31.1** (workspace `Cargo.toml`).
- Solana Agave CLI pinned to **v4.0.0** in CI (`.github/workflows/contracts-ci.yml`).
- Rust toolchain pinned to **1.85.0** (first stable with edition2024 support, required by
  transitive deps).
- Auditors should reproduce the build inside the same containerized environment used in CI
  to ensure bytecode parity with the version they sign off on.

## Out-of-band concerns the audit should NOT bless

- The on-chain programs cannot block a legal/regulatory shutdown (SEC reclassification,
  geo-blocking enforcement). Those are addressed in `docs/TOKENOMICS.md` §"Legal risk +
  mitigation strategy" and are an operational/legal concern, not a smart-contract concern.
- Provider tax obligations are not enforced on-chain. The billing-svc emits
  1099-MISC-equivalent reports — that is an off-chain process.

## Deliverables expected from the audit firm

1. Written report with severity-rated findings (Critical / High / Medium / Low / Informational).
2. Confirmation that the `vested_amount` and `budget_for_window` integer math is correct
   across `[0, u64::MAX]` and `[tge_unix, tge_unix + 32 years]` respectively.
3. Confirmation that no instruction can mint past the 1B hard cap (`GRID_HARD_CAP`).
4. Confirmation that no instruction can bypass the 50% early-unlock burn penalty.
5. Confirmation that the `authority_locked` flag is monotonically true (set-once,
   never-cleared) and gates every privileged action.
6. Re-attestation after any post-audit changes (typically a "fix-up" round).

## Cost + timing

- Budget: $30–60K (OtterSec) per `docs/TOKENOMICS.md` §"Token launch sequence".
- Calendar: 4–6 weeks from the engagement signing, including a fix-up round.
- Engagement must complete BEFORE `transfer_mint_authority` is called on mainnet (issue #97).
