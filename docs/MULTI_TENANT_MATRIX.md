# Sociable Cash multi-tenant capability matrix

**Purpose:** validate the architectural decision that iogrid (with $GRID) and Sociable Cash (with future $CASH) should be **loosely coupled** — independent products, independent tokens, independent legal entities — with Cash serving as a multi-tenant fiat-rail platform that any token-issuing project can plug into.

To make the pattern concrete, this matrix compares four platforms:
- **iogrid** — our distributed compute + bandwidth mesh
- **Sociable Cash** — the planned multi-tenant stablecoin off-ramp rail
- **Binance** — a centralized exchange (reference point — what Cash is NOT)
- **AcmeMesh** — hypothetical 3rd-party tenant of Sociable Cash (e.g. a decentralized-storage project with its own $ACME token), shown so the multi-tenant symmetry is visible

---

## Capability matrix

Legend: ✅ owned/operated · 🟡 partial / consumes-from-partner · ❌ explicitly not in scope · 🔌 plugs into another platform · ❓ TBD by that project's team

| Capability / Building block | **iogrid** ($GRID) | **Sociable Cash** ($CASH) | **Binance** ($BNB) | **AcmeMesh** ($ACME — hypothetical tenant) |
|---|---|---|---|---|
| **Primary product** | Distributed compute + bandwidth mesh | Stablecoin remittance + fiat off-ramp rail | Centralized crypto exchange | (Anything — e.g. decentralized storage) |
| **Native token** | $GRID — deflationary work-token | $CASH — fee-discount platform token | $BNB — fee-discount + chain gas | $ACME — whatever AcmeMesh designs |
| **Token utility design** | Pay for compute + provider rewards + 20% pay-in-GRID discount | Hold for off-ramp fee discount + governance | Hold for trading fee discount + BNB Chain gas | Whatever AcmeMesh designs |
| **Token emission model** | Halving every 2 years, capped 1B | TBD (Cash team owns) | Initial 200M, quarterly burns | Whatever AcmeMesh designs |
| **Legal entity issuing** | iogrid Foundation (Cayman) | Sociable Cash Foundation (separate, jurisdiction TBD) | Binance Holdings (private co.) | AcmeMesh's own entity |
| **Token holders' relationship** | Customers + providers (B2B) | Cash users (B2C remittance) | Traders (B2C) | AcmeMesh's user base |
| **Where token trades** | Raydium CLMM $GRID/USDC (DEX-first) → CEX later | Raydium CLMM $CASH/USDC → CEX later | Binance Exchange itself + ~all CEXs | Raydium CLMM $ACME/USDC → CEX later |
| **Liquidity pool authority** | ✅ Owns Raydium pool, locked LP 4y | ✅ Owns its own pool | ✅ Binance IS the venue | ✅ Owns its own pool |
| **Onchain SPL transfer infra** | ✅ `billing-svc/internal/solana/` (Go) | ✅ Own implementation (likely TypeScript) | N/A — they ARE the venue | ✅ Own implementation |
| **KYC / AML pipeline** | 🟡 Stripe Identity for customers | ✅ Full Sumsub + 50-state MTL coverage | ✅ Full | 🔌 Plugs into Sociable Cash KYC |
| **Money Transmitter Licenses (US)** | ❌ Not in scope (no fiat handling) | ✅ Required + acquired | ✅ Operates around (no US retail) | ❌ Not in scope |
| **Fiat off-ramp (USDC → bank)** | 🔌 Redirects to MoonPay / Sociable Cash | ✅ TransFi / GCash / M-Pesa / SEPA / Wire | ✅ via Binance Fiat partners | 🔌 Plugs into Sociable Cash |
| **Fiat on-ramp (bank → USDC/token)** | 🟡 Stripe Checkout for customers paying USD | ✅ Same partner network | ✅ via Binance Fiat | 🔌 Plugs into Sociable Cash |
| **Stablecoin acceptance for payment** | ✅ USDC for customer billing | ✅ USDC for remittance | ✅ | ✅ |
| **Multi-token tenant routing** | ❌ Not in scope (we ARE a tenant) | ✅ Core feature — any project's token routes through Cash | ✅ All tokens listed on Binance | ❌ Same as iogrid — we're a tenant |
| **Token discovery / registration API** | ❌ N/A | ✅ `/v1/tokens/register` (multi-tenant) | ✅ Internal listings team | ❌ N/A |
| **Off-ramp redirect contract** | 🔌 Provider clicks 'Withdraw' → redirects to Cash | ✅ Receives redirects from any tenant | 🟡 Withdraw to Binance address only | 🔌 Same as iogrid |
| **Webhook to tenant on completion** | 🔌 Receives Cash webhooks | ✅ Sends to any tenant | 🟡 Email only | 🔌 Same as iogrid |
| **Smart contracts on Solana** | ✅ 5 Anchor programs (token + emission + vesting + staking + burn) | ❓ Probably 1-2 (token + treasury) | N/A (CEX) | ✅ Own contracts |
| **Multisig treasury** | ✅ Squads 3-of-5 (Cayman Foundation) | ✅ Own Squads multisig | ✅ Centralized custody | ✅ Own multisig |
| **Audit firm engaged** | OtterSec (planned) | Cash's choice | (centralized, not audited like DeFi) | AcmeMesh's choice |
| **Smart-contract audit responsibility** | ✅ iogrid Foundation pays | ✅ Cash Foundation pays | N/A | ✅ AcmeMesh pays |
| **Regulatory compliance burden** | Provider/customer ToS + AUP + DPA | Heavy — MTLs, KYC, AML, FinCEN, FCA, MAS, etc. | Heaviest — full exchange license stack | Same shape as iogrid |
| **Governance** | $GRID holders vote on protocol params (post-DAO handoff Y3) | $CASH holders vote on Cash params | $BNB holders vote on BSC validators | $ACME holders vote |
| **Cross-token incentive (hold tenant token AND $CASH for stacked discount)** | 🟡 Possible — Cash could offer "iogrid customer holding both $GRID + $CASH get 30% off" | ✅ Designs the rule | N/A | 🟡 Same as iogrid |
| **Native VPN product** | ✅ Consumer mesh VPN (free) | ❌ Not in scope | ❌ Not in scope | ❌ |
| **iOS-build CI** | ✅ Phase 2 product | ❌ | ❌ | ❌ |
| **Customer-facing developer SDKs** | ✅ TS/Python/Go/Java | ✅ TS-first | ✅ Many | ✅ Own |
| **Public Status page** | ✅ status.iogrid.org | ✅ status.sociable.cash | ✅ | ✅ |

---

## Three takeaways the matrix surfaces

### 1. iogrid and AcmeMesh are symmetric tenants

They consume the same Sociable Cash interface. Whether you're a compute mesh ($GRID) or a decentralized-storage mesh ($ACME), Cash treats you identically. This is the **multi-tenant property** — exactly what makes Cash a *platform* not a *service*. iogrid's special status is just "we were the first tenant to integrate."

### 2. Cash captures value from EVERY tenant

Every $GRID off-ramp pays Cash a fee. Every $ACME off-ramp pays Cash a fee. $CASH token captures buyback-burn from ALL tenant volume. iogrid's success grows Cash's revenue → grows $CASH token value → iogrid Foundation (if it holds $CASH) benefits indirectly.

**Aligned incentives without merger.** This is the BNB-of-rails pattern.

### 3. Binance is what Cash is NOT

Binance is a **centralized exchange** — you bring your token to them and they custody it. Cash is a **rail** — token issuers keep their token, Cash just routes the off-ramp transaction. Cash is more like **Stripe** (payment rail for merchants) than Binance (custody + trading venue). This is the cleanest model for a regulator to look at: Cash never custodies user tokens, just routes them.

| | Binance | Sociable Cash |
|---|---|---|
| Custody model | Holds user assets | Never holds — pure routing |
| Listing process | Internal review board | Permissionless tenant registration |
| Pricing | Binance sets fees | Tenant + LP fee structure |
| Risk model | Centralized failure point | Distributed routing, per-tenant isolation |
| Regulatory shape | Exchange license stack | Money-rail license (MTL) per state |

---

## Commit-level decisions this matrix locks in

- ✅ $GRID stays iogrid-exclusive (not renamed, not merged into Cash)
- ✅ $CASH is Cash's own token (we don't design it, Cash team does)
- ✅ iogrid never directly handles fiat at the user level — always redirects to a rail
- ✅ Off-ramp adapter abstraction makes Cash one of N providers (MoonPay default + Cash + Coinbase + ...), never the only one
- ✅ iogrid Foundation MAY hold a $CASH treasury position to align incentives, but this is investment-not-equity (a separate decision)
- ✅ The Raydium CLMM $GRID/USDC pool is the authoritative liquidity venue. Cash routes through it via Jupiter aggregator. CEX listings are aspirational, not blocking.

---

## Cross-references

- [`docs/BUSINESS-STRATEGY.md` §4 (Currency model — $GRID + fiat hybrid)](./BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid) — full $GRID economics
- [`docs/BUSINESS-STRATEGY.md` §2 (Competitive landscape)](./BUSINESS-STRATEGY.md#2-competitive-landscape) — competitive landscape
- Issue #167 — EPIC: Off-ramp partnership model with Sociable Cash
- Issue #168 — Document Raydium CLMM as canonical $GRID liquidity venue
- Issue #169 — web off-ramp redirect flow
- Issue #170 — gateway-bff Cash webhook receiver
- Issue #172 — $GRID vs $CASH positioning section in docs/BUSINESS-STRATEGY.md §4 (Currency model — $GRID + fiat hybrid)
