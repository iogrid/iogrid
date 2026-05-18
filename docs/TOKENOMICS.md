# Tokenomics — $GRID

**Major architectural decision (2026-05-18):** provider payouts and customer payments shift from fiat-only (Stripe Connect / Stripe Subscriptions) to a hybrid model where **$GRID**, an iogrid-minted deflationary token, is the **native unit of account**. Fiat remains a supported on-ramp / off-ramp but is no longer the primary medium.

The founder's intent: providers benefit from token value appreciation as the network grows, aligning their incentives with iogrid's long-term success. The deflationary mechanism is designed so that long-term holders capture upside without active trading.

---

## Headline parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| **Symbol** | `$GRID` | Short, memorable, evokes the project name |
| **Network** | Solana (SPL token) | Sub-second finality, ~$0.0005 per tx, deepest non-EVM DEX liquidity, no per-action gas surprises for providers |
| **Initial supply** | 1,000,000,000 (1 billion) | Standard supply for SPL ecosystem tokens |
| **Decimals** | 9 (Solana SPL standard) | |
| **Emission curve** | Halving every 2 years | Bitcoin-style scarcity baked into the protocol |
| **Year-1 emission** | 50 million $GRID (5% of supply) | Bootstrap providers + ecosystem |
| **Year-10 cumulative** | ~485M emitted (~48.5% of supply) | Provider rewards pool exhausted, only burns remove tokens from circulation thereafter |
| **Burn rate target** | ≥2% of monthly revenue → market-buy → burn | Continuously reduces circulating supply as network scales |
| **Treasury custody** | Multisig (3-of-5 Squads Protocol) | Programmatic governance with founder + key contributors |

---

## Token allocation

```
┌─ 50% — Provider rewards pool ────────── 500M $GRID
│       Vested linear over 10 years (halving schedule baked in)
│       Distributed continuously per workload contributed
│
├─ 15% — Team ──────────────────────────── 150M $GRID
│       4-year vesting with 1-year cliff
│       Founder + core engineering + ops
│
├─ 10% — Treasury / Governance ─────────── 100M $GRID
│       Multisig-controlled
│       Funds: legal, audits, infra, community grants
│
├─ 10% — Strategic investors (if any) ──── 100M $GRID
│       12-month cliff, 24-month linear vest after
│       Held for runway during pre-revenue phase
│
├─ 10% — Community / ecosystem ─────────── 100M $GRID
│       Airdrops to early adopters, bug bounties,
│       integration grants, validator rewards (Phase 3)
│
└─  5% — Initial DEX liquidity ────────────  50M $GRID
        Seed Raydium / Jupiter pools
        Paired with USDC at launch
```

Distribution proportions configurable up to mainnet launch; locked at TGE (Token Generation Event).

---

## Deflationary mechanism — multi-layered

### Layer 1 — Buyback-and-burn from customer revenue (continuous)

- Customers pay in USD (Stripe) or USDC (on-chain) or $GRID directly.
- Daily automated process: **2% of all revenue** is converted to $GRID via Jupiter swap → burned to `1nc1nerator1111...` (Solana well-known burn address).
- Burns are public, on-chain, verifiable. Live dashboard at `burn.iogrid.org`.

### Layer 2 — Emission halving (every 24 months)

| Year | Provider emission rate |
|------|------------------------|
| 0–2 | 50M / year |
| 2–4 | 25M / year |
| 4–6 | 12.5M / year |
| 6–8 | 6.25M / year |
| 8–10 | 3.125M / year |
| 10+ | 0 new emissions; only burns reduce supply |

Hard-coded into the SPL emission program; no governance can override.

### Layer 3 — Staking-induced supply lockup

- Providers can stake $GRID to earn **routing priority** (their devices are preferred for premium workloads, earning ~30% more).
- Customers can stake $GRID to earn **volume discounts** (up to 25% off list price).
- Staked tokens are illiquid for the staking duration (minimum 30 days). Effectively reduces circulating supply.
- Target: 40%+ of circulating supply locked in staking by Year 2.

### Layer 4 — Customer-pays-in-$GRID discount

- Customers paying in $GRID directly (no USD conversion) get **20% off list price**.
- The $GRID they pay flows back to providers + 2% burn.
- Creates persistent buy-pressure on the token as customers swap USD → $GRID to capture the discount.

---

## Provider payout flow

```
┌──────────────────────────────────────────────────────────────┐
│ Customer payment ($1000 USD via Stripe)                      │
│                                                              │
│ Step 1: 2% buyback-and-burn                                  │
│   $20 USD → swap to $GRID on Jupiter → burn                  │
│                                                              │
│ Step 2: 98% provider rewards                                 │
│   $980 USD → swap to $GRID on Jupiter (TWAP over 1 hour)    │
│   → distribute to providers proportional to their contribution│
│   for that billing period                                    │
│                                                              │
│ Step 3: Provider on-ramp choice                              │
│   Provider holds $GRID in their connected wallet             │
│   Optionally: swap to USDC → off-ramp via MoonPay (1% fee)   │
│   Optionally: stake $GRID for routing priority + yield       │
└──────────────────────────────────────────────────────────────┘
```

### Wallet requirement

- Providers MUST connect a Solana wallet (Phantom / Solflare / Backpack) during onboarding
- iogrid never custodies provider tokens
- "Cash payout" tier is replaced by "auto-convert to USDC and off-ramp" tier (additional 1% fee)

### Tax compliance

- $GRID earned is taxable as ordinary income at receipt (US treatment).
- iogrid emits a quarterly 1099-MISC equivalent based on token's USD price at time of receipt.
- Providers are responsible for capital gains on subsequent disposal.

---

## Customer payment options

| Method | Discount | Settlement |
|--------|----------|------------|
| Stripe USD | List price | Instant |
| Stripe USDC | List price | Instant |
| On-chain USDC (Solana) | 5% off | <1 second |
| On-chain $GRID | 20% off | <1 second |

KYC requirement applies to fiat (Stripe AML) and to large on-chain USDC payments (>$10k/month via Sumsub or Persona). $GRID-only customers face per-wallet limits (analytics-based, sanctions-list checked).

---

## Smart contract architecture

```
                ┌────────────────────────────────────────────┐
                │  Solana Mainnet                            │
                │                                            │
                │  ┌──────────────────┐ ┌──────────────────┐ │
                │  │ $GRID SPL Token  │ │ Vesting program  │ │
                │  │ (Token-2022 ext) │ │ (Streamflow)     │ │
                │  └──────────────────┘ └──────────────────┘ │
                │                                            │
                │  ┌──────────────────┐ ┌──────────────────┐ │
                │  │ Emission program │ │ Staking program  │ │
                │  │ (halving curve)  │ │ (lockup + yield) │ │
                │  └──────────────────┘ └──────────────────┘ │
                │                                            │
                │  ┌──────────────────┐ ┌──────────────────┐ │
                │  │ Burn registry    │ │ DEX pools        │ │
                │  │ (on-chain log)   │ │ (Raydium/Orca)   │ │
                │  └──────────────────┘ └──────────────────┘ │
                └────────────────────────────────────────────┘
                                ▲
                                │ deposit/withdraw
                                │
                ┌───────────────┴────────────────────────────┐
                │  Coordinator billing-svc (Go)              │
                │  - Hot wallet (multisig 2-of-3) for daily  │
                │    swap+distribute                         │
                │  - Provider payout queue                   │
                │  - Burn audit log                          │
                │  - Quarterly tax report generator          │
                └────────────────────────────────────────────┘
```

### Tech stack additions

- **Anchor** (Rust) — smart contract framework on Solana
- **Streamflow** — token vesting + cliff schedules (audited, production-grade)
- **Squads** — multisig treasury
- **Jupiter** — DEX aggregator for USD ↔ $GRID swaps
- **Helius** — Solana RPC + indexing (existing fast lane)
- **Pyth** — price oracle (USD/$GRID) for fair-value swaps
- **MoonPay** — fiat off-ramp for providers who want USDC → bank transfer

---

## Legal risk + mitigation strategy

> **This is the highest-impact section. Read carefully.**

A token whose value providers "get impacted by" — exactly the founder's framing — sits squarely inside the **Howey test** for an investment contract:
1. Investment of money — yes (providers expect compensation)
2. Common enterprise — yes (network effect)
3. Reasonable expectation of profit — yes (deflationary mechanism is marketed as accruing value)
4. From the efforts of others — yes (iogrid's operations drive value)

A US court is likely to classify $GRID as a security if no mitigations are in place. Recent SEC actions (Coinbase, Binance, Kraken) confirm this trajectory.

### Mitigations — required before TGE

1. **Geographic restrictions at launch.** No sales / airdrops to US persons. Geo-block US IPs from the token-purchase flow. Standard practice (Solana ecosystem norms).
2. **Token utility primacy in marketing.** Brand $GRID as the network's unit of work, not an investment. Never marketing-promise price appreciation. Treat it like AWS credits with a market.
3. **Foundation structure.** Establish a Cayman Foundation (zero income tax, non-profit form) or BVI Limited to hold treasury and govern the network. iogrid Inc. (Dynolabs's operating entity) licenses tech to the Foundation; Foundation issues tokens. Separates equity owners from token holders.
4. **Liechtenstein TVTG token-issuance license** OR **EU MiCA registration** (whichever cheaper at TGE time) for European market.
5. **Reg D / Reg S exempt offering** for any pre-TGE strategic raise. Accredited investors only.
6. **Counsel.** Top-tier crypto lawyer (Cooley, Fenwick, Davis Polk, Latham — pick by partner expertise) for at least:
   - Token legal opinion ($25–75K)
   - Foundation structuring ($30–80K)
   - Provider ToS amended for token economics ($10–20K)
   - Regulator outreach (no-action requests) ($variable)
7. **No "earn yield by holding" language.** Yield comes from staking work (routing priority), not from passive holding.
8. **Token whitepaper** published pre-TGE with clear utility narrative, risk factors, no forward-looking statements about price.

### Risks we cannot fully mitigate

- SEC could classify $GRID as a security regardless of structure. Outcome: forced delisting from US exchanges, mandatory rescission offer to US holders, possible fines.
- Provider tax confusion (earning a volatile asset is harder than earning fixed USD) hurts adoption among non-crypto-native users.
- DEX liquidity could be inadequate at launch — large provider claims could crash the price short-term, eroding trust.
- $GRID price volatility makes B2B customer billing hard ("how much will this cost in USD next month?"). Discount-pegged-to-spot mechanism only partially addresses.

### Decision the founder needs to make explicitly

**Are these mitigations worth the cost (~$200K legal + ~12 months runway extension) vs the alignment value of token-aligned providers?**

If **yes** → token model proceeds as documented.
If **no** → fall back to fiat-only payouts (the model in `docs/INCENTIVES.md` pre-amendment). The token model could be revisited in Phase 4+ once $5M+ ARR justifies the legal investment.

This document **assumes yes** and proceeds. If you want to reverse, say so.

---

## Token launch sequence

| Milestone | Trigger | Timing |
|-----------|---------|--------|
| **Foundation incorporated** | Founder + counsel select jurisdiction | Month 1 |
| **Smart contracts written + audited** | Anchor program ready, audit by OtterSec / Halborn | Months 1–3 |
| **Devnet token deployed** | Internal provider testing | Month 3 |
| **Testnet token + dual payout** | Phase 1 closed-beta providers receive both fiat AND testnet tokens | Months 4–6 |
| **Mainnet TGE** | Token launches on mainnet, DEX liquidity seeded, initial airdrop to Phase 0/1 providers | Month 6–9 |
| **Halving #1** | Year 2 anniversary of TGE | Year 2 |
| **Governance handoff to DAO** | Treasury multisig migrates to community-elected signers; protocol parameters become DAO-votable | Year 3+ |

---

## What changes in the rest of the architecture

- **`coordinator/billing-svc`** now also operates a Solana hot wallet (multisig 2-of-3), runs daily Jupiter swaps, manages provider payout queue, emits 1099-equivalent tax reports.
- **`coordinator/identity-svc`** gains a wallet-binding flow: providers connect Phantom / Solflare via SIWS (Sign-In-With-Solana) signature, bind it to their identity.
- **`web/`** management plane gains:
  - Wallet-connect button (Solana Wallet Adapter)
  - $GRID balance display
  - Staking UI (stake / unstake / claim rewards)
  - Off-ramp flow (USDC → bank via MoonPay embed)
  - Burn dashboard at `/burn` (public, no auth)
- **Customer-side payments**: Stripe stays primary (KYC-easier for enterprise), plus on-chain SDK for $GRID-paying customers.

---

## What this does NOT change

- The provider daemon (Rust) remains identical — it never touches tokens directly. Coordinator handles all token logic.
- Anti-abuse, scheduling, transparency dashboard — unchanged.
- The iOS-build / Docker / GPU / bandwidth workload split — unchanged.
- The Honeygain-comparable provider UX — slightly higher friction (wallet connection step) but the daemon itself runs the same.
- Consumer VPN remains free in exchange for bandwidth share — same mesh-swap economics, no token requirement for consumer-side users.

---

## Open decisions (founder to confirm)

1. **Final token symbol** — `$GRID` vs `$IOG` vs other (this doc assumes `$GRID`)
2. **Chain choice** — Solana (this doc) vs Base (Ethereum L2) vs Polygon
3. **Foundation jurisdiction** — Cayman vs BVI vs Liechtenstein vs Wyoming DAO LLC
4. **Initial supply** — 1B (this doc) vs 100M vs 10B
5. **Halving period** — 2 years (this doc) vs 4 years (full Bitcoin parity)
6. **Burn percentage** — 2% of revenue (this doc) vs higher
7. **Pre-TGE strategic raise** — yes/no, target amount
