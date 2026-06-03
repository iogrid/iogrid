# $GRID Tokenomics — canonical source of truth

> This file is the **machine-and-partner-facing source of truth** for the
> `$GRID` token's on-chain identity. The full economic design (emission,
> lockups, buyback-and-burn, tiers) lives in [`whitepaper.md`](./whitepaper.md);
> this file exists so integration partners (notably **Ping**, per
> `ping-cash/ping-cash:docs/coordination/iogrid-ping-integration.md`) have a
> single, stable place to track the mint address and wire contract.

## On-chain identity

| Field | Value |
|---|---|
| Symbol | `$GRID` |
| Network | Solana (Token-2022 SPL) |
| Decimals | `9` |
| **Mainnet mint address** | **PRE-LAUNCH — not yet deployed.** See "Mint address contract" below. |
| Devnet mint address | `BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR` (Token-2022, 9 decimals) — see "Devnet" below. |

The **9-decimal** convention (authoritative: `whitepaper.md` — "SPL Token-2022,
9 decimals, hard cap 1B"; `initialize_mint` creates the mint with decimals=9;
and `billing-svc` meters all settlement in 9-decimal atomic units) is
load-bearing for partners: an amount of `N` $GRID is expressed atomically as
`N * 10^9`. Example: 250 $GRID → `250000000000`. Ping's `approve` Universal Link
expects the atomic form.

> ⚠️ **Decimal-mismatch alert (2026-06-03):** an earlier draft of this file and
> Ping's contract example (`amount=250000000` for 250 $GRID) used **6** decimals.
> The canonical mint is **9** decimals per the whitepaper + billing-svc. Ping's
> `approve` `amount` and any client building atomic amounts MUST use 9 — a 6 vs 9
> mismatch is a silent **1000×** error on every payment. Tracked for Ping in #629.

## Mint address contract

The token is not minted yet (the iogrid Foundation is pre-incorporation per
`whitepaper.md` / `docs/BUSINESS-STRATEGY.md`). Until mainnet deployment,
**no canonical base58 mint exists** and this section must NOT be populated
with a placeholder — a wrong mint would route real funds to a dead ATA.

Consumers MUST resolve the mint via indirection, never by hard-coding:

| Consumer | Indirection key |
|---|---|
| iogrid mobile (Expo) | `EXPO_PUBLIC_GRID_TOKEN_MINT` |
| coordinator billing-svc | `GRID_TOKEN_MINT_ADDRESS` (in `billing-svc-secrets`) |
| **Ping mobile** | `expoConfig.extra.iogridTokenMint` — Ping tracks THIS file as the upstream value |

**Cutover rule (owed to Ping, per their contract §"iogrid side owes Ping"):**
the mainnet mint is published HERE first, then propagated to the env keys
above through a coordinated cutover. Any future mint change (e.g. an upgrade)
is a coordinated cutover, never a silent swap.

## Devnet

A **devnet** `$GRID` mint is deployed so the Ping integration's token half
(balance reads + SPL-Approve `amount` math) is testable end-to-end **before**
the mainnet launch. The devnet mint is disposable test infrastructure — it
carries no economic meaning and is **not** the mainnet token. Refs #629.

| Field | Value |
|---|---|
| Network | Solana **devnet** |
| Mint address | `BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR` |
| Token program | `TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb` (**Token-2022**) |
| Decimals | `9` (matches the canonical 9-decimal convention above) |
| Freeze authority | `null` (non-negotiable transparency property, same as mainnet spec) |
| Mint authority | devnet treasury `8EaS5sf4dT7SzEFNYKC1PD5C3PcKhPnEnHCYjSZ1hcpb` (retained for re-mint on devnet) |
| Treasury ATA | `4u9JDrLVBkBL2vLx691egahEDrzNMaQxNZ6B3M334dM3` (1,000,000 $GRID test supply minted) |

Devnet env wiring (clearly labeled, never replacing a mainnet value):

| Consumer | Devnet default location |
|---|---|
| iogrid mobile (Expo) | `EXPO_PUBLIC_GRID_TOKEN_MINT` in `mobile/ios/.env.example`; app.json `extra.iogridTokenMintDevnet` |
| web | commented devnet block in `web/.env.example` (`NEXT_PUBLIC_GRID_MINT_ADDRESS`) |
| coordinator billing-svc | set `GRID_TOKEN_MINT_ADDRESS` to the devnet mint in staging only |

> ⚠️ **Mainnet is still a founder decision.** The mainnet `$GRID` mint is an
> outward-facing, irreversible financial action and is NOT created by this
> devnet work. The "Mainnet mint address" / `extra.iogridTokenMint` keys stay
> empty until the founder-gated mainnet deploy. See `docs/SOLANA-ADDRESSES.md`.

## Ping integration pointer

The product-layer integration (how providers off-ramp $GRID and how VPN
users pay $GRID via Ping's wallet) is governed by Ping's canonical contract:

- Off-ramp: `$GRID → USDC` auto-swap on receipt via Jupiter (ADR 0007),
  no swap spread on the inbound leg.
- Pay-for-service: **SPL Approve (delegate)** initiated from iogrid via the
  Universal Link `https://ping.cash/approve?token=GRID&delegate=<vault>&amount=<atomic>&memo=<schema>&return_url=iogrid://vpn/activated`.
- Memo schema: `iogrid.v1:vpn:<region>:<days>`.
- Separate Foundations, separate utility: `$GRID` and `$PING` never merge.

iogrid-side conformance to that contract is tracked in iogrid issue **#629**.

### Multi-tenant matrix + Universal-Link groundwork (C-6)

The tenant-routing shape that sits on top of this token identity (the
per-tenant `return_url` scheme table, the memo schema, and the
bidirectional handshake) is documented in
[`MULTI_TENANT_MATRIX.md`](./MULTI_TENANT_MATRIX.md), which Ping's contract
cross-references. As part of coordination item **C-6** (Ping → iogrid
"Direction B" handoff), iogrid now also ships an Apple App Site Association
(AASA) file so `iogrid.org` is a Universal-Link target:

- Static body: `web/public/.well-known/apple-app-site-association`
- Route handler (pins `application/json`):
  `web/src/app/.well-known/apple-app-site-association/route.ts`

The Apple **Team ID is a placeholder** (`PLACEHOLDER_TEAMID`, overridable
via `NEXT_PUBLIC_APPLE_TEAM_ID`) pending real iogrid Foundation Apple
credentials — see MULTI_TENANT_MATRIX.md "What remains blocked".

## Liquidity obligation

iogrid owes Ping `$GRID → USDC` Jupiter liquidity within a 0.5% slippage
budget so Ping's auto-swap off-ramp does not fail closed. Liquidity
provisioning is a launch-gated action (depends on mint deployment above).
