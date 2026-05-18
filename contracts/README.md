# `$GRID` Solana programs

Anchor workspace implementing the five on-chain programs for the `$GRID` token economy. The
canonical economic parameters live in [`../docs/TOKENOMICS.md`](../docs/TOKENOMICS.md); this
directory is the contract surface that enforces them on Solana.

## Programs

| Program       | Crate path                | Purpose                                                                                          |
|---------------|---------------------------|--------------------------------------------------------------------------------------------------|
| `grid-token`  | `programs/grid-token`     | Token-2022 mint + authority management (hard cap 1B, decimals 9).                                |
| `emission`    | `programs/emission`       | Halving emission curve (50M / 25M / 12.5M ... year), epoch-based payout batches.                 |
| `vesting`     | `programs/vesting`        | Mandatory provider-earnings lockup: 30-day cliff + 60-day linear vest. 4 tiers. 50% burn-penalty early-unlock. |
| `staking`     | `programs/staking`        | Routing-priority stake (provider) + volume-discount stake (customer). 30-day minimum. Yield accrual. |
| `burn`        | `programs/burn`           | Public on-chain registry of buyback-and-burn events.                                             |

All five programs are independent on-chain — they communicate by Token-2022 `transfer` /
`burn` CPIs against a shared `$GRID` mint plus client-side composition. There is no
program-to-program CPI dependency, which keeps each program independently auditable.

## Build

Requires Anchor CLI 0.31+. Easiest install:

```bash
cargo install --git https://github.com/coral-xyz/anchor avm --locked --force
avm install 0.31.0
avm use 0.31.0
anchor --version   # anchor-cli 0.31.0
```

Then from this directory:

```bash
yarn install
anchor build       # produces target/deploy/*.so + target/idl/*.json + target/types/*.ts
```

The first `anchor build` takes 5–10 minutes (a fresh Cargo + BPF toolchain compile).

## Test

```bash
anchor test                              # spawns a local validator, runs ts-mocha
anchor test --skip-build --skip-deploy   # if you've already built and have a validator
```

Tests intentionally split into two layers:

1. **IDL / math assertions** (default in `tests/*.ts`) — assert that the IDL exposes the
   expected instructions, account fields, and error codes; verify the off-chain math matches
   the on-chain implementation (vested_amount, halving curve, yield accrual). These run fast
   and require no funded keys.
2. **Integration suite** (forthcoming; tracked separately) — spin up a fresh Token-2022 mint,
   call every instruction, assert post-conditions. Will live in `tests/_integration.ts` and
   gate via env `RUN_INTEGRATION=1` so they can be skipped in normal CI.

## Deploy

### Localnet

```bash
solana-test-validator               # in another terminal
anchor deploy --provider.cluster localnet
```

### Devnet

```bash
solana airdrop 5 --url devnet       # fund the deployer
anchor deploy --provider.cluster devnet
```

CI **does not** deploy to devnet (no funded wallet) — it builds + runs the unit/IDL suite
only. Devnet deploys are performed manually by the founder + tech lead from a key controlled
by the eventual Cayman Foundation's hot wallet (see `migrations/deploy.ts` header).

### Mainnet-beta (TGE)

Mainnet deployment is gated on:

1. **Cayman Foundation incorporated** (open GitHub issue #103 "Foundation incorporation
   (Cayman/BVI/Liechtenstein)" + the chore issue this scaffold opened). The foundation,
   not iogrid Inc., is the legal mint authority holder.
2. **Smart contract audit complete** — OtterSec or Halborn (issue #97). $30–80K, ~4–6 weeks.
3. **Squads 3-of-5 multisig live** (issue #96), holding the eventual mint authority post-
   `transfer_mint_authority`.
4. **DEX liquidity ready** — Raydium CLMM pool seeded with 5M $GRID + $250K USDC (issue #94).
5. **Reg D/Reg S strategic raise closed** (optional, issue #104) if pursuing the pre-TGE
   $2M strategic round to fund items 1–4.

Until those gates are green, this workspace is dev/devnet-only.

## Audit preparation

Recommended pre-mainnet audit firms (per `../docs/TOKENOMICS.md` § "Token launch sequence"):

- **OtterSec** — leading Solana-native audit firm, audited Phoenix, Squads, Jupiter, MarginFi.
  Strong reputation in the Solana ecosystem; deep Anchor expertise. Typical engagement
  $30–60K for a 5-program workspace this size.
- **Halborn** — multi-chain audit firm with strong Solana practice; also offers a "audit
  retainer" model that fits a project that will keep iterating post-launch.
- **Neodyme** — boutique, very deep on Solana primitives. Lower throughput, longer queue.

The recommendation is OtterSec as primary auditor with Neodyme as a second-pair-of-eyes spot
review on the vesting and emission programs (the highest-impact economic logic). Halborn is
the strong fallback if OtterSec is queue-blocked at the time of submission.

Pre-audit checklist:
- [ ] `anchor build` clean (no warnings)
- [ ] `anchor test` 100% green (unit + integration)
- [ ] `cargo clippy --workspace --all-targets -- -D warnings` clean
- [ ] All `#[error_code]` enums documented with a 1-line description per variant
- [ ] PDA seed table documented in this README
- [ ] Threat model document at `docs/AUDIT-THREAT-MODEL.md` enumerates the assumed
      attacker, the value at risk, and the privileged signers (admin, billing_signer,
      attestor) for each program
- [ ] CI green on `main` for at least 2 weeks before audit start

## PDA seed table

| Program       | PDA                                | Seeds                                                                       |
|---------------|------------------------------------|-----------------------------------------------------------------------------|
| `grid-token`  | `GridConfig`                       | `["grid-config", mint]`                                                     |
| `emission`    | `EmissionConfig`                   | `["emission-config", mint]`                                                 |
| `emission`    | vault authority                    | `["emission-vault-authority", mint]`                                        |
| `emission`    | `EpochClaim`                       | `["emission-epoch", mint, epoch_id]`                                        |
| `vesting`     | `ProviderVesting`                  | `["provider-vesting", mint, provider]`                                      |
| `vesting`     | vesting vault                      | `["vesting-vault", mint, provider]`                                         |
| `vesting`     | vesting vault authority            | `["vesting-vault-authority", mint, provider]`                               |
| `vesting`     | `VestingDeposit`                   | `["vesting-deposit", provider_vesting, deposit_id]`                         |
| `staking`     | `StakingPool`                      | `["staking-pool", mint]`                                                    |
| `staking`     | stake vault                        | `["staking-stake-vault", mint]`                                             |
| `staking`     | reward vault                       | `["staking-reward-vault", mint]`                                            |
| `staking`     | staking vault authority            | `["staking-vault-authority", mint]`                                         |
| `staking`     | `StakePosition`                    | `["stake-position", pool, owner]`                                           |
| `burn`        | `BurnRegistry`                     | `["burn-registry", mint]`                                                   |
| `burn`        | `BurnReceipt`                      | `["burn-receipt", registry, seq]`                                           |

## CI

`.github/workflows/contracts-ci.yml` runs on every push that touches `contracts/`:

1. Installs Solana CLI + Anchor CLI 0.31.0
2. Caches Cargo registry + `target/` between runs
3. `anchor build` (build all 5 programs)
4. `anchor test --skip-deploy` (runs ts-mocha against the local validator)

Expect 5–10 minutes for a cold cache, 1–2 minutes warm. Deploying to devnet is **not** part
of CI (no funded wallet); promote-to-devnet is a manual workflow.

## Open governance questions

These are tracked as GitHub issues; this README pins them so anyone reading is reminded:

- Foundation jurisdiction (Cayman vs BVI vs Liechtenstein vs Wyoming DAO LLC) — issue #103.
- Whether to do the Reg D / Reg S $2M pre-TGE raise — issue #104.
- Burn rate floor (2% in this scaffold; founder may raise it pre-TGE).
- Staking annual yield (configurable; pool initializer sets `annual_yield_bps`).
- Whether to enable Token-2022 transfer hooks (currently no hooks; can add via mint extension
  pre-TGE if the audit recommends a fee/limit hook).
