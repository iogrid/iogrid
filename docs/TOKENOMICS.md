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
| Decimals | `6` |
| **Mainnet mint address** | **PRE-LAUNCH — not yet deployed.** See "Mint address contract" below. |
| Devnet mint address | PRE-LAUNCH — not yet deployed. |

The 6-decimal convention is load-bearing for partners: an amount of
`N` $GRID is expressed atomically as `N * 10^6`. Example: 250 $GRID →
`250000000`. Ping's `approve` Universal Link expects the atomic form.

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

## Liquidity obligation

iogrid owes Ping `$GRID → USDC` Jupiter liquidity within a 0.5% slippage
budget so Ping's auto-swap off-ramp does not fail closed. Liquidity
provisioning is a launch-gated action (depends on mint deployment above).
