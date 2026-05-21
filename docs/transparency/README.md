# $GRID transparency program

iogrid commits to publishing a **quarterly transparency report** covering the
on-chain economics of the $GRID token. The intent is to mirror the disclosure
cadence of regulated web2 financial institutions — quarterly, public, durable
in git history — without inheriting the gatekeeping. Anyone can audit the
treasury, the burn ledger, the emission curve, and the foundation's activity
against what we publish.

## Why this exists

Closes the visibility gap between the on-chain reality (which is technically
public but practically unreadable without tooling) and the audience that needs
to assess it: token-holders, providers earning $GRID, prospective customers
evaluating the unit-of-account risk, and regulators. The report is the
authoritative human-readable view; raw on-chain data remains the source of
truth.

## Cadence

- **Quarterly.** Publish dates target the 1st business day of the month after
  quarter close:
  - Q1 (Jan–Mar) → published by **April 1**
  - Q2 (Apr–Jun) → published by **July 1**
  - Q3 (Jul–Sep) → published by **October 1**
  - Q4 (Oct–Dec) → published by **January 15** (longer window for year-end
    treasury and tax reconciliation)
- A **draft** version of each report is committed at the start of the
  reporting quarter with `Status: DRAFT` and `Publish: YYYY-MM-DD` in the
  frontmatter, then progressively filled as the quarter unfolds.
- The draft flips to `Status: PUBLISHED` on the publish date. The
  corresponding git tag `transparency-YYYY-Qn` is cut at that point.

## Where reports live

Each report is a single Markdown file in this directory:

```
docs/transparency/
├── README.md          ← this file (program overview)
├── TEMPLATE.md        ← the canonical report shape
├── 2026-Q2.md         ← first report (DRAFT)
├── 2026-Q3.md
└── ...
```

Naming: `YYYY-Qn.md` (zero-pad not required; quarter is single digit 1–4).

## What every report covers

The shape is fixed (see [`TEMPLATE.md`](./TEMPLATE.md)). Top-level sections:

1. **Treasury balance** — Squads multisig holdings (USDC, $GRID, other)
2. **Emission** — programmatic emission this quarter, actual vs. curve
3. **Burns** — buy-and-burn event count, total $GRID burned, USD value
4. **Staking** — % of supply staked, validator/provider participation
5. **Liquidity** — Raydium CLMM pool depth, 30-day volume
6. **Foundation activity** — grants, partnerships, governance proposals
7. **Compliance / legal updates**
8. **Known issues / forward look**

Numbers are sourced from on-chain queries against the SPL emission program,
the Squads multisig, the burn-address balance delta, and Raydium pool state.
The query commands used are appended to each report under "Methodology" so
anyone can reproduce the figures.

## How holders subscribe to notifications

Three channels, lowest-friction first:

- **GitHub watch.** Watch this repo with "Custom → Releases" enabled. Each
  published report gets a release tagged `transparency-YYYY-Qn` with the
  report rendered in the release body.
- **RSS.** GitHub releases expose an RSS feed at
  `https://github.com/iogrid/iogrid/releases.atom`. Drop that into any
  reader.
- **Email list.** A low-volume announce-only list (`transparency@iogrid.org`)
  fires one message per report. Subscribe by emailing
  `transparency-subscribe@iogrid.org` (handled by the foundation's listserv).
  Unsubscribe link is in every message.

There is **no Discord/Telegram-only channel** for transparency reports — the
canonical surface is git, and every downstream channel mirrors what's in this
directory.

## Methodology disclosure

Each report ends with an explicit "Methodology" section listing:

- The RPC endpoints queried
- The exact `solana` / `spl-token` CLI commands run
- The block height / slot range covered
- Any manual reconciliation (e.g. CEX-held treasury balances that don't show
  on-chain)

If a figure cannot be sourced on-chain (e.g. fiat-denominated grants paid out
of a non-multisig operating account), it is labelled `[off-chain]` and the
counterparty + amount is disclosed in plain text.

## Editorial policy

- **No predictions.** Reports cover the quarter that closed, not forward
  guidance. The "Forward look" section names known events (planned
  governance proposals, upcoming audits) but assigns no price expectations.
- **Errors corrected in-place** with a `Corrections` section at the bottom of
  the affected report. Git history is the audit trail; we do not silently
  rewrite.
- **No marketing language** in transparency reports. Tone is auditor-neutral.

## Related

- [`../BUSINESS-STRATEGY.md` §4 (Currency model — $GRID + fiat hybrid)](../BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) — the underlying economics being
  reported on
- [`../BUSINESS-STRATEGY.md` §3 (Unit economics & provider incentives)](../BUSINESS-STRATEGY.md#3-unit-economics--provider-incentives) — provider incentive design
- [`../BUSINESS-STRATEGY.md` §6 (Legal risk landscape & mitigation)](../BUSINESS-STRATEGY.md#6-legal-risk-landscape--mitigation) — legal posture that this disclosure cadence
  supports
