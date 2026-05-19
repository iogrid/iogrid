# $GRID smart contract audit

This directory holds the artifacts an external smart-contract audit firm needs to scope,
quote, and execute a pre-mainnet review of the five `$GRID` Anchor programs.

**Status:** open. Audit not yet engaged. Tracking issue: [#97](https://github.com/iogrid/iogrid/issues/97).

---

## TL;DR for auditors

| Item | Value |
|------|-------|
| Project | `$GRID` token on Solana (SPL Token-2022) |
| Programs | 5 (`grid-token`, `emission`, `vesting`, `staking`, `burn`) |
| Framework | Anchor 0.31.1 |
| Solana CLI | Agave 4.0.0 (CI-pinned) |
| Rust toolchain | 1.85.0 |
| Lines of Rust (approx) | ~3,000 across the 5 programs |
| Tokenomics reference | [`../../docs/TOKENOMICS.md`](../../docs/TOKENOMICS.md) |
| Whitepaper | [`../../docs/whitepaper.md`](../../docs/whitepaper.md) |
| Risk-factor narrative | [`../../legal/token-disclaimer.md`](../../legal/token-disclaimer.md) |
| Threat model | [`./threat-model.md`](./threat-model.md) |
| Audit scope | [`./scope.md`](./scope.md) |
| Auditor checklist | [`./checklist.md`](./checklist.md) |
| Test vectors | [`./test-vectors.md`](./test-vectors.md) |
| Budget estimate | [`./budget.md`](./budget.md) |
| Build instructions | [`../README.md`](../README.md) + `make build && make test` |

A single-command bundle of everything an auditor needs (source, IDLs, tests, audit docs,
tokenomics, sha256 manifest) is produced via:

```bash
cd contracts/
make audit-export
# → audit-bundle-<timestamp>.tar.gz with MANIFEST.sha256 inside
```

---

## Recommended audit firms (decision matrix)

After surveying the Solana audit landscape:

| Firm | Strengths | Weaknesses | Typical quote (5 programs) | Calendar |
|------|-----------|------------|----------------------------|----------|
| **OtterSec** (otter.xyz) | Deepest Anchor expertise. Audited Phoenix, Squads, Jupiter, MarginFi, Drift, Tensor. Largest Solana audit firm. | Long queue (4–8 weeks lead time); higher cost. | $40–80K | 4–6 weeks |
| **Halborn** (halborn.com) | Multi-chain; strong Solana practice. Offers "retainer" model for iterative re-audit. Faster intake than OtterSec. | Slightly less Solana-native than OtterSec; some Anchor 0.31 gaps reported. | $30–60K | 4–6 weeks |
| **Neodyme** (neodyme.io) | Boutique; very deep on Solana primitives (write-up of Wormhole exploit). | Lower throughput; longer queue. Smaller team. | $30–50K | 6–10 weeks |
| **Sec3** (sec3.dev) | Automated + manual review; runs the X-Ray tool. Cheaper. | More tooling-driven, less narrative writeup. | $20–40K | 3–5 weeks |
| **Trail of Bits** (trailofbits.com) | Industry leader, deep formal-methods chops. | Most expensive; Solana less of a specialty than other firms. | $80–150K | 6–10 weeks |

**Recommendation:** **OtterSec as primary** auditor for all 5 programs, with **Neodyme** for a
second-pair-of-eyes spot review on `vesting` and `emission` (the highest-impact
economic logic). **Halborn** as the strong fallback if OtterSec queue blocks at submission
time.

The total target spend: $30–80K (primary) + optional $10–20K (Neodyme spot review).
Falls within the [`docs/TOKENOMICS.md`](../../docs/TOKENOMICS.md) budget envelope.

---

## How to engage an audit firm

### Step 1 — Pre-engagement (founder + tech lead, ~2 weeks before submission)

1. **Lock the code.** Freeze the `contracts/` tree on a tagged commit (`v0.1.0-audit`).
   No further changes until the audit completes (except critical fixes from CI / clippy).
2. **Green CI.** Last 5 CI runs on the audit-target commit green. `make build`, `make test`,
   `make clippy` all clean.
3. **Build the audit bundle.** `cd contracts && make audit-export`. SHA256 of the bundle
   and the manifest go into the engagement letter so the auditor can confirm parity.
4. **Open the kickoff issue.** A GitHub issue on this repo titled
   "Audit: OtterSec engagement — $GRID v0.1.0-audit" with the bundle hash, the auditor's
   contact, and a 2-week response SLA. Link to issue #97.

### Step 2 — Auditor onboarding email (template)

```
Subject: $GRID smart contract audit — OtterSec engagement request

Hi [contact],

We are iogrid Foundation, building a Solana-native work marketplace with a $GRID utility
token. We would like to engage OtterSec for a pre-mainnet audit of our 5 Anchor programs.

Project context:
  * Whitepaper:        https://github.com/iogrid/iogrid/blob/main/docs/whitepaper.md
  * Tokenomics:        https://github.com/iogrid/iogrid/blob/main/docs/TOKENOMICS.md
  * Source:            https://github.com/iogrid/iogrid/tree/v0.1.0-audit/contracts
  * Audit bundle:      attached, sha256 <hash>, manifest <hash>
  * Threat model:      contracts/audit/threat-model.md (in the bundle)
  * Audit scope:       contracts/audit/scope.md
  * Auditor checklist: contracts/audit/checklist.md
  * Test vectors:      contracts/audit/test-vectors.md

Scope: 5 Anchor programs (grid-token, emission, vesting, staking, burn). Anchor 0.31.1.
Approx. 3,000 lines of Rust + 2,000 lines of TS test/IDL harness. PDA-only architecture
(no third-party CPI dependencies on our side — Token-2022 + system program only).

Timeline ask: kickoff within 4 weeks of contract signing, with a 4–6 week audit window
plus a 2-week fix-up round. Mainnet TGE is gated on the audit report.

Budget envelope: $30–80K all-in, per our internal budget. If the work warrants a higher
number, please write it up — we can have that conversation.

We would also welcome a Neodyme spot review of the `vesting` and `emission` programs as a
second-pair-of-eyes exercise; happy to coordinate that introduction.

Please confirm:
  1. Availability + kickoff date
  2. Quote + scope (variance from the bundle scoped above)
  3. Engagement letter draft

Thank you,
[Founder name]
iogrid Foundation
```

### Step 3 — Engagement letter must-haves

- **Scope locked to the bundle hash.** Any change to the source between engagement and
  delivery is out of scope unless re-quoted.
- **Deliverable:** written report with severity-rated findings (Critical / High / Medium /
  Low / Informational), reproduction steps per finding, recommended fixes, fix verification.
- **Fix-up round included.** Auditor commits to a 1-week re-audit after iogrid lands fixes,
  at no additional cost (up to 2 fix-up cycles).
- **Public report.** Auditor agrees to a public report at iogrid's discretion (with a
  reasonable embargo window for critical fixes).
- **Liability cap and IP.** Standard audit-firm contract terms. iogrid retains all code IP.
- **Re-audit retainer.** Optional clause: $5–15K per upgrade audit if iogrid ships a
  program upgrade post-launch. We expect to use this.

### Step 4 — Auditor work (4–6 weeks)

- Auditor reads source + threat model + checklist + test vectors.
- Daily Slack / email checkpoints (lightweight; auditor leads cadence).
- Mid-audit findings shared in draft so iogrid can fix critical-severity items early.
- Final report delivered.

### Step 5 — Fix-up + re-audit (2 weeks)

- iogrid lands fixes for every Critical / High finding.
- Medium findings: triage and land in `main`; non-blocking.
- Low / Informational: documented in [`./checklist.md`](./checklist.md) as known-acceptances or follow-ups.
- Auditor re-runs verification on the fix-target commit.
- Final report includes "all Critical/High findings fixed" sign-off.

### Step 6 — Publish + mainnet readiness

- Final audit report published at `https://iogrid.org/security/audit-2026.pdf` and pinned
  in `contracts/audit/`.
- Mainnet deploy proceeds: `transfer_mint_authority` to the `emission` program PDA,
  `lock_authorities` on `grid-token`, Squads multisig becomes upgrade authority.
- Bug bounty program opens at [Immunefi](https://immunefi.com) with a tiered payout
  ($1K – $250K depending on severity).

---

## Auditor onboarding doc (technical)

### Codebase layout

```
contracts/
├── Anchor.toml               # workspace config (anchor 0.31.1)
├── Cargo.toml                # rust workspace (resolver=2, overflow-checks=true)
├── Makefile                  # convenience targets (make build/test/audit-export)
├── README.md                 # build + PDA inventory
├── programs/
│   ├── grid-token/src/lib.rs   # Token-2022 mint init + hard cap
│   ├── emission/src/lib.rs     # halving curve + epoch payouts
│   ├── vesting/src/lib.rs      # provider lockup + early-unlock
│   ├── staking/src/lib.rs      # routing-priority + discount staking
│   └── burn/src/lib.rs         # buyback-and-burn registry
├── tests/                    # ts-mocha test harness (IDL + off-chain math mirror)
├── migrations/deploy.ts      # post-build deploy bootstrap (devnet/local only)
├── scripts/                  # operator tooling (see ./scripts/README)
│   ├── local-validator.sh    # boot a fresh local validator with programs deployed
│   ├── devnet-deploy.sh      # interactive devnet deploy
│   ├── upgrade.sh            # re-deploy one program (rotates bytecode, keeps program id)
│   ├── idl-publish.sh        # `anchor idl init/upgrade`
│   ├── airdrop.sh            # devnet SOL helper
│   └── burn-replay.sh        # admin: replay a missed burn from emission log
└── audit/                    # this directory
```

### Build environment

```bash
# Anchor + Solana CLI
cargo install --git https://github.com/coral-xyz/anchor avm --locked --force
avm install 0.31.0 && avm use 0.31.0
sh -c "$(curl -sSfL https://release.anza.xyz/v2.0.0/install)"   # Agave (formerly solana-cli)

# Rust toolchain (pinned via rust-toolchain.toml if present, else)
rustup toolchain install 1.85.0
rustup default 1.85.0
rustup target add wasm32-unknown-unknown  # not strictly required for BPF; harmless

# Node + Yarn (for tests)
nvm use 22

# Build
cd contracts/
yarn install
anchor build       # first time: 5–10 min cold compile
```

### Running the tests

```bash
make test                                # spawns local validator, runs ts-mocha
anchor test --skip-build --skip-deploy   # reuse existing validator
```

The TS test suite splits into two layers:

1. **Off-chain math mirror** — TS reimplements `vested_amount`, `budget_for_window`,
   `compute_weight` and asserts they match the on-chain implementation across boundary
   cases. Fast (no validator needed for assertion logic).
2. **IDL parity** — TS reads `target/idl/*.json` and asserts the expected instruction set,
   account fields, and error codes are present.

A full integration suite (with simulated clock advance) is staged under `tests/integration.ts`
and gated by `RUN_INTEGRATION=1`. **Auditors should run with `RUN_INTEGRATION=1` enabled.**

### Devnet program IDs

`grid_token` / `emission` / `vesting` / `staking` / `burn` use placeholder IDs in
`Anchor.toml` (`GR1Dtoken...`, `GR1Demission...`, etc.). These are vanity prefixes that
will be regenerated for real-keypair-backed deployments. Auditors should treat the IDs as
strings to validate, not real on-chain accounts (no on-chain state to inspect on devnet
until we redeploy).

### Open questions for the auditor

These are listed verbatim from [`./threat-model.md`](./threat-model.md) under
"Known-unknowns":

1. **`init_if_needed` on `GridMetadata`**: re-running `set_metadata` overwrites in place.
   We rely on `has_one = admin` to gate this. Confirm the Anchor-generated discriminator/
   seed check makes account substitution impossible.
2. **`anchor-spl 0.31.1` `transfer` vs `transfer_checked`**: we use `transfer` with
   explicit `#[allow(deprecated)]`. `transfer_checked` requires the mint account on every
   transfer; we accept the trade-off because our internal accounts validate the mint via
   `has_one`. Rule on whether to migrate.
3. **No `unstake_request` two-phase cool-down**: v0 has only `unstake`, rejected before
   `MIN_STAKE_SECS`. Should we add a 7-day post-min-stake request window?

### Out-of-band concerns

- iogrid coordinator code (`coordinator/billing-svc/**`) is OUT of scope. Audit must
  validate that the on-chain programs survive a malicious `billing_signer` / `attestor`.
- Streamflow vesting (used for LP-token lock) is third-party, separately audited.
- Squads multisig (used for the Foundation treasury) is third-party, separately audited.
- Wormhole NTT (used for the Base bridge post-TGE) is third-party, separately audited.

---

## Post-audit follow-up

- **Bug bounty program** opens at Immunefi with $1K – $250K tiered payouts based on
  Immunefi's standard severity scale.
- **Re-audit on upgrade**: every program upgrade (via `scripts/upgrade.sh`) requires a
  re-audit covering the diff. The retainer clause in the engagement letter covers this at
  a discounted rate.
- **Internal continuous review**: `cargo clippy --workspace --all-targets -- -D warnings`
  in CI ensures no new warnings sneak in; the auditor's recommended invariants get pinned
  as TS test assertions where feasible.

---

## Related issues

- [#97](https://github.com/iogrid/iogrid/issues/97) — Smart contract audit (this directory)
- [#88](https://github.com/iogrid/iogrid/issues/88) — Anchor workspace scaffold + dev tooling
- [#96](https://github.com/iogrid/iogrid/issues/96) — Squads 3-of-5 multisig setup
- [#103](https://github.com/iogrid/iogrid/issues/103) — Foundation jurisdiction selection
- [#122](https://github.com/iogrid/iogrid/issues/122) — Cayman Foundation incorporation
- [#102](https://github.com/iogrid/iogrid/issues/102) — Token whitepaper publication
