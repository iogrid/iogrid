# Tokenomics — $GRID

**Major architectural decision (2026-05-18):** provider payouts and customer payments shift from fiat-only (Stripe Connect / Stripe Subscriptions) to a hybrid model where **$GRID**, an iogrid-minted deflationary token, is the **native unit of account**. Fiat remains a supported on-ramp / off-ramp but is no longer the primary medium.

The founder's intent: providers benefit from token value appreciation as the network grows, aligning their incentives with iogrid's long-term success. The deflationary mechanism is designed so that long-term holders capture upside without active trading.

---

## $GRID vs $CASH — token positioning

**$GRID and $CASH are two distinct tokens, issued by two distinct legal entities, with non-overlapping utility.** They are **NOT merged**, **NOT cross-equity**, and **NOT renamed forms of each other**. The fact that Sociable Cash is the founder's preferred off-ramp partner for $GRID providers does NOT change this separation — Cash is a tenant-neutral rail, iogrid is one tenant among many.

| | **$GRID** (iogrid's token) | **$CASH** (Sociable Cash's future token) |
|---|---|---|
| **Issued by** | iogrid Foundation (Cayman) | Sociable Cash Foundation (separate entity, jurisdiction TBD by Cash team) |
| **Project scope** | Distributed compute + bandwidth mesh | Multi-tenant stablecoin off-ramp rail |
| **Primary utility** | Work-token: paid to compute providers; 20% pay-in-$GRID discount for customers | Platform fee-discount token: held by Cash users to get cheaper off-ramps (Binance BNB model) |
| **Supply curve** | 1B cap, halving every 2 years, 2% revenue buyback-burn | TBD — Cash team owns the design |
| **Audience** | B2B compute (customers + providers) | B2C remittance (Cash users) |
| **TGE timing** | Coincides with iogrid mainnet launch | On Cash's own timeline (likely Year 2 once product-market fit proven) |
| **Regulatory posture** | Cayman Foundation, geo-blocks US persons at launch, Reg D/S for strategic raise | Cash's own MTL + KYC stack (out of iogrid scope) |
| **In-scope for this repo** | Yes — designed, audited, shipped by iogrid | **No** — out of scope, owned by Cash team |

### What "not merged" means concretely

- **Different SPL mints, different tickers, different supply curves.** $GRID is `$GRID` on Solana. $CASH is `$CASH` on Solana. Neither is a wrapped, bridged, or rebranded form of the other.
- **Different legal entities.** iogrid Foundation does not control Cash Foundation, and vice-versa. Token-holder rights, governance votes, and treasury policies are scoped to each Foundation independently. This is a deliberate regulatory-isolation choice — neither project's compliance posture contaminates the other.
- **Different audiences.** iogrid markets $GRID to compute providers and B2B customers. Sociable Cash markets $CASH to remittance senders/receivers and tenant projects. The marketing surfaces never cross-sell as if they were one product.

### Mutual-incentive cross-investments allowed (NOT cross-equity)

- iogrid Foundation **may** hold a small treasury position in $CASH at Cash's TGE (aligns incentives, signals trust). This is a discretionary treasury investment, not a merger or equity stake.
- Sociable Cash **may** LP into the Raydium $GRID/USDC pool (deeper liquidity benefits iogrid users who off-ramp via Cash).
- Either Foundation may run cross-token incentive programs (e.g. "hold both $GRID + $CASH for stacked discount") **without** that constituting a token merger — the discount is a marketing rule, not a contract change.

### Cross-references

- [`docs/MULTI_TENANT_MATRIX.md`](./MULTI_TENANT_MATRIX.md) — full capability matrix proving iogrid and AcmeMesh are symmetric tenants of Cash; iogrid's special status is "first tenant to integrate," not "owner."
- Issue #167 — EPIC: Off-ramp partnership model with Sociable Cash
- Issue #172 — this section

---

## Canonical $GRID liquidity venue — Raydium CLMM

**The $GRID/USDC Raydium CLMM pool is the authoritative DEX-first liquidity source for $GRID.** All off-ramp routing — whether via Sociable Cash, MoonPay, Coinbase, or any future partner — discovers liquidity through this pool via the Jupiter swap aggregator. iogrid never routes provider payouts or customer swaps through a centralized exchange's order book; the Raydium pool is the venue.

### Pool parameters at TGE

```
Pair: $GRID / USDC
Venue: Raydium CLMM (Solana — concentrated liquidity AMM, Uniswap v3 equivalent)

Seed:
- 5,000,000 $GRID (5% initial liquidity allocation from token-allocation table above)
- $250,000 USDC (from pre-TGE strategic raise proceeds)

Range: $0.05 – $5.00 (100× price-discovery range)
Fee tier: 0.25% (Raydium standard for new pairs)
LP tokens: locked for 4 years via Streamflow vesting contract
```

### LP lock — 4-year vest, then permanent burn

LP tokens are deposited into a Streamflow vesting contract on TGE day with a **4-year linear vest** to the iogrid Foundation Squads multisig. **At end of vest the LP tokens are permanently burned**, locking the seeded liquidity in the pool forever.

This means:
- iogrid cannot rug-pull the pool — the LP is provably non-removable for 4 years, then non-removable forever.
- Anyone can verify on-chain by inspecting the Streamflow stream and the eventual LP-token burn tx.
- The 4-year horizon matches the team-vesting curve — incentive alignment by design.

### Jupiter routing — the canonical swap path

All off-ramp partners route $GRID swaps through **Jupiter** (Solana's primary DEX aggregator), which always discovers the best-priced route. In practice the Raydium $GRID/USDC pool is the deepest venue, so Jupiter routes through it. As secondary venues emerge (Orca Whirlpools, Meteora DLMM, etc.) Jupiter discovers them automatically — no integration work for off-ramp partners.

**Critically: off-ramp partners NEVER swap directly on a CEX order book.** The flow is always:

```
Provider $GRID  ──swap via Jupiter──▶  USDC  ──off-ramp partner──▶  Fiat bank deposit
                       (Raydium CLMM)         (Cash / MoonPay / etc.)
```

This keeps three properties intact:
1. **DEX-first price discovery.** The pool's mid-price is the canonical $GRID price; no CEX listing can manipulate it without first arbitraging the pool.
2. **Permissionless integration.** Any future off-ramp partner integrates by pointing at Jupiter — no per-partner CEX listing negotiation needed.
3. **Tenant-neutral liquidity.** Sociable Cash, MoonPay, and any 3rd-party off-ramp see the same pool, same prices, same depth. iogrid's preferred-partner status doesn't entitle Cash to special pricing — Cash earns its routing fee on the off-ramp leg, not on the swap leg.

See `docs/MULTI_TENANT_MATRIX.md` for how this multi-tenant property generalises to any other token issuer (e.g. AcmeMesh's hypothetical $ACME) that wants to off-ramp via Cash.

### LP-lock verification procedure (operator runbook)

To prove on-chain that the pool is locked:

1. Open `raydium.io/pools/<pool-id>` — pool-id published in `docs/TRACKER.md` at TGE.
2. Click "LP tokens" → "Holders". The Streamflow vesting contract should be the sole non-trivial holder.
3. Open Streamflow at `app.streamflow.finance/contract/<stream-id>` — stream-id published alongside pool-id.
4. Verify: recipient = iogrid Foundation Squads multisig; cliff/vest = 0/4 years from TGE; cancellable = false; transferable-by-sender = false.
5. After Year 4: verify the LP-token burn tx (sent to `1nc1nerator1111...`). Pool liquidity then locked permanently.

### Pool-concentration adjustment protocol

As price discovers within the $0.05–$5.00 range, the LP range may be narrowed to concentrate liquidity for tighter spreads. Procedure:

1. **Proposal** by any Squads multisig signer with rationale (current price, current effective range, proposed new range).
2. **3-of-5 Squads vote** required to approve. Vote published on-chain.
3. **Atomic re-range tx** through Raydium CLMM's `decrease-liquidity` + `increase-liquidity` pair within a single Solana transaction. Slippage cap 0.5%.
4. Adjustments capped at one per 90 days to prevent active-management drift.
5. Each adjustment logged in a public registry (`burn.iogrid.org/lp-adjustments`).

### CEX listings — aspirational, not blocking

Tier-1 CEX listings (Binance Spot, Coinbase, Kraken) are tracked as aspirational milestones, **not** prerequisites for $GRID utility or off-ramp functionality. Bonk, Jupiter, Wormhole, Pyth, and Helium all launched DEX-first on Solana without waiting for CEX listings; iogrid follows the same playbook. CEX listings, when they arrive, are additive distribution — they do not move the canonical price (Jupiter arbitrage keeps CEX prices pegged to the Raydium pool's mid).

### Cross-references

- [`docs/MULTI_TENANT_MATRIX.md`](./MULTI_TENANT_MATRIX.md) — multi-tenant rail model; Raydium pool is the shared liquidity venue every tenant routes through.
- `docs/whitepaper.md` — public-facing Jupiter routing narrative.
- PR #177 — off-ramp adapter implementation (provider-side `Withdraw` → Jupiter swap → off-ramp partner redirect).
- Issue #168 — this section.

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

> **Transparency.** Treasury balance, emission progress, burns, staking
> participation, LP health, and foundation activity are published quarterly
> in [`docs/transparency/`](./transparency/README.md). See the
> [report template](./transparency/TEMPLATE.md) for the canonical shape and
> [`2026-Q2.md`](./transparency/2026-Q2.md) for the first scheduled
> publication.

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

### Layer 3 — Mandatory provider-earnings lockup (the alignment mechanic)

**Every $GRID earned by a provider is auto-locked the moment it's distributed.** Providers cannot sell freshly-earned tokens; they earn into a vesting position that releases on a schedule.

Base lockup applied to ALL earned $GRID:

| Time since earned | % unlocked |
|-------------------|------------|
| Day 0 – 30 | 0% (cliff) |
| Day 30 – 90 | Linear vest 0% → 100% |
| Day 90+ | 100% unlocked (provider can sell, transfer, withdraw) |

This is **rolling per payout** — each weekly distribution starts its own 30/90-day clock. A provider who has been earning continuously will always have most of their balance in some stage of vesting.

#### Why it works

1. **Stops day-1 dump.** Without lockup, every provider would convert $GRID → USDC the moment they receive it, crashing the price. With lockup, only ~33% of any month's earnings are sellable at any time.
2. **Compounds with deflation.** Locked tokens count toward "circulating supply removed" — they're untradable. As more providers join, the lockup pool grows, supply pressure shrinks.
3. **Skin in the game.** Providers care about $GRID price even after they stop providing — their vesting position keeps appreciating.

#### Optional bonus lockup tiers (provider's choice, opt-in)

Providers can elect a LONGER lockup at signup for a rewards multiplier:

| Lockup tier | Cliff + vest schedule | Rewards multiplier |
|-------------|----------------------|--------------------|
| **Standard** (default) | 30-day cliff + 60-day linear vest | **1.0×** |
| **Loyalty** | 90-day cliff + 180-day linear vest | **1.25×** |
| **Conviction** | 180-day cliff + 365-day linear vest | **1.5×** |
| **Maximum** | 365-day cliff + 730-day linear vest | **2.0×** |

A provider who picks the "Maximum" tier earns 2× the $GRID for the same work — but cannot touch it for 1 year, with another 2 years of linear vest. Designed for true believers / long-term holders.

Tier is set per-provider at onboarding; can be UPGRADED any time (locks more, never less), but cannot be downgraded.

#### Early-unlock with penalty (escape hatch, not free)

A provider in genuine financial need can early-unlock locked $GRID, but:

- **50% penalty** on the locked portion
- The penalty is **burned** (not retained by iogrid) — strengthens deflation
- One early-unlock event per 12 months per provider (anti-gaming)

So a provider with 10,000 locked $GRID who early-unlocks gets 5,000 unlocked tokens + 5,000 burned forever. Painful enough that few will use it, soft enough that we're not "trapping" anyone.

#### Stake-while-locked

Locked tokens automatically count toward the provider's **routing-priority stake weight.** Providers don't lose yield/priority just because their tokens are vesting — they get the alignment benefit for free.

#### Customer-side staking (separate, voluntary)

Customers may also stake $GRID for **volume discounts** (up to 25% off list price). Minimum 30 days, customer's choice.

#### Target supply lockup

By Year 2: **60%+** of circulating supply locked across provider earnings + voluntary customer stakes. This effectively halves the trading float, amplifying both upside volatility (good for holders) and the deflationary buy-and-burn impact (each $1 of burn removes 2× more % of circulating supply).

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
2. **Foundation jurisdiction** — Cayman vs BVI vs Liechtenstein vs Wyoming DAO LLC
3. **Initial supply** — 1B (this doc) vs 100M vs 10B
4. **Halving period** — 2 years (this doc) vs 4 years (full Bitcoin parity)
5. **Burn percentage** — 2% of revenue (this doc) vs higher
6. **Pre-TGE strategic raise** — yes/no, target amount

(Chain choice locked: **Solana primary + Base bridge** — see chain-rationale section.)

---

## Chain choice rationale — Solana primary, Base as bridge

Founder asked which L1 best fits iogrid's needs. Decision:

| Factor | Solana | Base (Eth L2) | Polygon | Eth L1 | Verdict |
|--------|--------|---------------|---------|--------|---------|
| Cost / tx | $0.0005 | $0.005–0.05 | $0.001–0.01 | $5–50 | Solana 10× cheaper than Base |
| Finality | <1 sec | ~2 sec | ~2 sec | ~12 sec | Solana wins |
| Throughput sustained | 3000+ TPS | ~50 TPS | ~50 TPS | ~15 TPS | Solana wins |
| Native DEX depth | Raydium / Orca / Jupiter | Uniswap (bridged) | Quickswap (declining) | Uniswap | Solana wins |
| Token primitives | Token-2022 with transfer hooks (burn / fee / limit) | ERC-20 + ERC-4626 | ERC-20 | ERC-20 | Solana wins |
| Vesting | Streamflow (audited, production) | Sablier / Llamapay | Sablier | Sablier | Tie |
| Multisig | Squads Protocol (Solana-native) | Safe (Gnosis) | Safe | Safe | Tie |
| CEX listing path | Strong: Bybit, Binance, OKX, Coinbase, Kraken | Coinbase-native (parent) | Weakening | Universal | Solana strong; Base parent-advantage |
| 2026 ecosystem momentum | Highest among non-Eth | Fastest-growing L2 | Declining | Stable | Solana wins |
| Concentrated liquidity AMM | Raydium CLMM, Orca Whirlpools | Uniswap v3/v4 | Quickswap | Uniswap | Tie |

**Decision:** Solana primary. Daily payout swaps + thousands of monthly provider payouts + 2% buy-and-burn at Solana cost = ~$5/month gas. Same volume on Base = $50–500/month. Same volume on Ethereum mainnet = $5K–50K/month. Solana is the only chain whose economics scale to 100K providers without architecture rework.

**Bridge to Base** via [Wormhole NTT](https://wormhole.com/products/ntt) within 30 days of TGE: this enables $GRID-on-Base, which is what Coinbase (parent of Base) lists. For providers who prefer Coinbase off-ramp over Solana-native off-ramps, the bridge is one-click.

## Launch sequence — modern DEX-first playbook (no IEO dependency)

| Phase | Action | Why |
|-------|--------|-----|
| **Pre-TGE strategic raise (Months 1–3)** | Reg D / Reg S, ~$2M @ $20M FDV → 10M tokens (1% supply) sold to accredited investors. CoinList private rounds or direct. | Funds legal + audit + initial liquidity. |
| **Smart contract dev + audit (Months 1–4)** | Anchor program development + audit by OtterSec or Halborn ($30–80K). | Required before TGE — no shortcuts. |
| **Mainnet TGE on Solana (Month 6–9)** | Token mint. Streamflow vesting contracts activated. Squads treasury multisig live. | TGE = official token genesis event. |
| **Bootstrap own liquidity pool (TGE Day 0)** | Seed Raydium CLMM with 5M $GRID + $250K USDC in concentrated range ($0.05–$5.00 = 100× price discovery range). | **THIS is our primary trading venue. No exchange required.** Anyone can swap USDC↔$GRID immediately. |
| **Jupiter Launchpad public sale (TGE Day 0–7)** | 5M tokens at fixed price via Jupiter Launchpad (decentralized, KYC for >$1K). | Public access, no 5–10% IEO fee that CEX launchpads charge. |
| **Bridge to Base (TGE Day 30)** | Wormhole NTT deploys $GRID on Base | Coinbase off-ramp accessible for non-crypto-native providers |
| **Tier-2 CEX listings (Month 6+)** | Bybit Launchpad, KuCoin, Gate.io | Adds visibility; not mission-critical thanks to DEX-first launch |
| **Tier-1 CEX listings (Year 1+)** | Binance Spot, Coinbase, Kraken | Requires 6+ months organic volume, full legal review, audit reports. Aspirational. |

**Key insight:** by seeding our own Raydium CLMM pool at TGE, we eliminate the "we need to get listed somewhere" risk that early-stage tokens face. Bonk, Jupiter, Wormhole, Pyth, Helium — all launched DEX-first on Solana, none waited for CEX listings. Tier-1 CEX listings followed organically once volume + liquidity proved out.

## Concentrated liquidity strategy — Raydium CLMM

Raydium CLMM is the Solana equivalent of Uniswap v3 — providers pin liquidity to a price range and earn fees only when the pair trades in that range. Mechanics:

```
Seed pool:
- 5,000,000 $GRID (from 5% initial liquidity allocation)
- $250,000 USDC (from pre-TGE raise proceeds)

Range: $0.05 – $5.00 (100× price discovery)
Implied launch price: ~$0.05 (low end of range)
Implied FDV at launch: $50M
Implied FDV ceiling: $5B (top of range)

Fee tier: 0.25% (standard for new pairs)
LP tokens: locked in 4-year-vest Streamflow contract → cannot be rugged
```

**Liquidity defense layers:**
1. **Locked LP tokens** — anyone can verify on-chain that we cannot rug the pool
2. **Permanent burn of LP token at end of vesting** — locks liquidity forever, signals trust
3. **MEV protection** — use Jupiter swap routing (built-in sandwich-attack mitigation)
4. **Pool concentration adjustments** — as price discovers, narrow the range to concentrate liquidity for tighter spreads. Done via 3-of-5 Squads multisig vote.

## Strategic raise — terms sketch

For founder reference if pursuing the $2M pre-TGE round:

- Tokens: 10M $GRID (1% of supply) at $0.20/token = $2M raised
- FDV at strategic round: $200M
- TGE launch FDV (DEX implied): $50M (4× discount for strategic investors)
- Vesting: 12-month cliff, 24-month linear vest after TGE (3 years total)
- Investor rights: information rights, no board seat, no veto, governance vote weighted equal to community
- Use of proceeds:
  - Legal + counsel — $200K
  - Smart contract audit — $50K
  - Foundation setup — $100K
  - Initial liquidity seed — $250K
  - Tokenomics-specific engineering — $400K (1 senior dev × 12 mo)
  - Reserve / runway — $1M

