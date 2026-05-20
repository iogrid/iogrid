# Business Strategy

> **WHAT:** Canonical product, market, economics, currency, partner-integration, and legal-risk strategy for iogrid.
> **AUTHORITY:** Canon. Supersedes (now-removed) `COMPETITORS.md`, `MARKET.md`, `INCENTIVES.md`, `LEGAL.md`, `OFFRAMP_PROVIDERS.md`, `TOKENOMICS.md`.
> **POINTER:** Regulatory whitepaper lives at [`docs/whitepaper.md`](./whitepaper.md). User-global engineering principles at [`~/.claude/CLAUDE.md`](../CLAUDE.md). The Sociable-Cash multi-tenant capability matrix lives at [`docs/MULTI_TENANT_MATRIX.md`](./MULTI_TENANT_MATRIX.md) (will be promoted to `docs/adr/0001-loose-coupling-with-sociable-cash.md` in a follow-up consolidation PR tracked in #337).

---

## 1. Market

> Source: previously `docs/MARKET.md` (merged here on 2026-05-20).

### 1.1 Total addressable markets

#### Residential proxy

- ~**$1.5B annual market**, growing ~20%/year.
- Customer segments: e-commerce monitoring (40%), SEO/SERP scraping (15%), ad verification (10%), lead-gen scraping (10%), social media intelligence (10%), brand protection (5%), AI training data (5%), travel aggregation (3%), threat intel (2%).
- Customer price points: **$5–15 per GB** retail.
- Provider payout share: $0.30–0.60 per GB (margin is the spread).

#### Distributed compute / GPU inference

- **~$25M/yr** captured by Salad alone (the leader in gamer-GPU compute).
- Vast.ai, io.net, Akash, Render Network combined: another ~$50M.
- Market growing fast with LLM/AI demand.
- Customer price points: $0.20–2.00 per GPU-hour.
- Provider payout: $0.05–0.50 per GPU-hour.

#### iOS build CI

- **~$500M–$1B/yr market**, growing ~20%/year (every mobile app needs iOS CI).
- Underserved segment with high prices and limited supply:

| Provider | Cost/min | Notes |
|----------|----------|-------|
| **GitHub Actions macos-latest** | $0.08 | Most popular, 14-day rate quota |
| **GitHub Actions macos-latest-large** | $0.16 | Faster M-series, higher rate |
| **Bitrise** | $0.10–0.30 | App-focused, integrations |
| **Codemagic** | $0.10–0.20 | Flutter/RN-focused |
| **CircleCI macOS** | $0.10–0.15 | |
| **AWS EC2 Mac (mac1.metal)** | $0.018 effective | BUT 24-hour minimum lease ($26 minimum) |
| **MacStadium dedicated** | $0.05–0.20 effective | Long-term lease model |

iogrid's offer at **$0.04 / minute** is the cheapest non-leased option in the market. The provider's Mac sitting idle 4 hours/day earns ~$145/month (vs $9/month bandwidth-only economics).

#### Consumer VPN

- ~**$50B annual market** (broad consumer VPN, including free + paid tiers).
- Top players: NordVPN, ExpressVPN, Surfshark, ProtonVPN, Mullvad.
- Pricing: $3–13/month subscription.
- iogrid's wedge: **free forever** for users (bandwidth-swap subsidises); $2.99 Plus tier for unlimited / Pro at $4.99 with privacy features.

### 1.2 3-year revenue trajectory (conservative)

| Year | Active providers | B2B MRR | VPN MRR (Plus/Pro) | Net margin |
|------|------------------|---------|--------------------|-----------|
| **Year 1 (Phase 0/1)** | 100 | $1K | $200 | -$30K (lawyer + infra investment) |
| **Year 2 (Phase 2)** | 5K | $50K | $5K | +$300K |
| **Year 3 (Phase 3)** | 50K | $500K | $50K | +$3.5M |

iogrid breaks even mid-Year 2 (after the Phase 1 → Phase 2 transition). Year 3 ARR target: $6.5M ($550K × 12). Aggressive case (Phase 2 lands a marquee enterprise customer at $50K/month): $10M+ ARR by Year 3.

---

## 2. Competitive landscape

> Source: previously `docs/COMPETITORS.md` (merged here on 2026-05-20).
> Source: previously `docs/MARKET.md` competitive matrices (merged here on 2026-05-20).

iogrid sits at the intersection of three historically separate markets:

1. Residential proxy networks (~$1.5B/yr)
2. Distributed compute / GPU marketplaces (~$200M/yr)
3. Consumer VPN (~$50B/yr)

Plus an emerging wedge into iOS-build CI (~$500M–1B/yr).

No competitor bundles all of these. Existing players occupy one quadrant each. iogrid's strategic position is to be the first **horizontally integrated mesh** — which is also why multi-currency provider payout (cash / VPN / tokens) and the radical transparency dashboard differentiate against all of them.

### 2.1 Master comparison table

Legend: ✅ = strong / yes · 🟡 = weak / partial · ❌ = absent / no

| Player | Category | Residential proxy | Docker compute | GPU inference | iOS build | VPN consumer | Provider payout | Transparency | Crypto token | Mainstream UX | iogrid relative position |
|--------|----------|-------------------|---------------|---------------|-----------|--------------|-----------------|--------------|--------------|---------------|--------------------------|
| **Bright Data** | Proxy | ✅ ~72M devices | ❌ | ❌ | ❌ | ❌ (Hola is parent brand) | ✅ Cash | ❌ Opaque (Hola legacy) | ❌ | 🟡 Enterprise only | We undercut on price, win on transparency |
| **Honeygain** | Proxy | ✅ ~1M devices | ❌ | ❌ | ❌ | ❌ | ✅ Cash | ❌ Opaque | ❌ | ✅ Polished | We add 3 more workloads + free VPN tier |
| **Pawns.app (IPRoyal)** | Proxy | ✅ ~500K | ❌ | ❌ | ❌ | ❌ | ✅ Cash | 🟡 Limited | ❌ | ✅ Polished | Same wedge as vs Honeygain |
| **EarnApp** | Proxy | ✅ ~10M devices | ❌ | ❌ | ❌ | ❌ | ✅ Cash | ❌ Opaque (Bright Data subsidiary) | ❌ | 🟡 Sketchy | Cleaner brand, more incentive options |
| **PacketStream** | Proxy | ✅ ~100K | ❌ | ❌ | ❌ | ❌ | ✅ Cash | ❌ | ❌ | 🟡 Dated | Same wedge |
| **Salad Technologies** | Compute | ❌ | ✅ Docker | ✅ GPU (gamer rigs) | ❌ | ❌ | ✅ Cash + gift cards | 🟡 Limited | ❌ | ✅ Polished | We add bandwidth + iOS-build, GPU work overlaps |
| **Vast.ai** | Compute | ❌ | 🟡 Cloud-style | ✅ GPU | ❌ | ❌ | ✅ Cash | 🟡 Marketplace fee | ❌ | 🟡 Power-user | We're cheaper, more workload variety, provider-first not customer-first |
| **io.net** | Compute | ❌ | 🟡 | ✅ GPU | ❌ | ❌ | ✅ Crypto (IO) | ❌ | ❌ | 🟡 Crypto-native | Same vertical, we add residential proxy + iOS build |
| **Akash Network** | Compute | ❌ | ✅ Docker | 🟡 | ❌ | ❌ | ✅ Crypto (AKT) | 🟡 | ✅ AKT | ❌ Crypto-only | Decentralized but crypto-locked, niche audience |
| **Render Network** | Compute | ❌ | ❌ | ✅ GPU rendering only | ❌ | ❌ | ✅ Crypto (RNDR) | 🟡 | ✅ RNDR | 🟡 Niche (3D artists) | Different vertical |
| **Mysterium Network** | VPN+proxy | 🟡 Some proxy | ❌ | ❌ | ❌ | ✅ Mesh VPN | ✅ Crypto (MYST) | 🟡 | ✅ MYST | ❌ Crypto-native | Same mesh-VPN philosophy, we ship better UX + more workloads |
| **Sentinel** | VPN | 🟡 | ❌ | ❌ | ❌ | ✅ VPN marketplace | ✅ Crypto (DVPN) | ❌ | ✅ DVPN | ❌ Crypto-native | Same — niche audience |
| **Orchid Protocol** | VPN | ❌ | ❌ | ❌ | ❌ | ✅ VPN | ✅ Crypto (OXT) | ❌ | ✅ OXT | ❌ Crypto-native | VPN only |
| **HOPR** | VPN/mixnet | ❌ | ❌ | ❌ | ❌ | ✅ Privacy mixnet | ✅ Crypto (HOPR) | 🟡 | ✅ HOPR | ❌ Crypto-native | Privacy mixnet niche |
| **Anyone / Nym** | VPN/mixnet | ❌ | ❌ | ❌ | ❌ | ✅ VPN | ✅ Crypto (NYM) | 🟡 | ✅ NYM | ❌ Crypto-native | Same |
| **Hola VPN** | VPN | ✅ via Bright Data | ❌ | ❌ | ❌ | ✅ Free VPN | 🟡 None (bandwidth-only) | ❌ Opaque (2015 scandal) | ❌ | ✅ Polished | We are the "ethical Hola" — same model, transparent consent |
| **NordVPN / ExpressVPN / Surfshark** | VPN | ❌ | ❌ | ❌ | ❌ | ✅ VPN | ❌ N/A | ❌ N/A | ❌ | ✅ Polished | Different business model (datacenter VPN, not mesh) |
| **ProtonVPN** | VPN | ❌ | ❌ | ❌ | ❌ | ✅ Free + paid | ❌ N/A | 🟡 (open source, audits) | ❌ | ✅ Polished | Same — different model |
| **GitHub Actions Mac** | iOS CI | ❌ | ❌ | ❌ | ✅ Mac CI | ❌ | ❌ N/A | ✅ Microsoft-owned | ❌ | ✅ Polished | We undercut 50% on per-minute pricing |
| **Bitrise** | iOS CI | ❌ | ❌ | ❌ | ✅ Mac CI | ❌ | ❌ N/A | ✅ | ❌ | ✅ Polished | We undercut, broader workload portfolio |
| **Codemagic** | iOS CI | ❌ | ❌ | ❌ | ✅ Mac CI | ❌ | ❌ N/A | ✅ | ❌ | ✅ Polished | Same |
| **MacStadium** | iOS CI | ❌ | ❌ | ❌ | ✅ Dedicated lease | ❌ | ❌ N/A | ✅ | ❌ | 🟡 Lease commitment | We're no-commitment per-minute |
| **AWS EC2 Mac** | iOS CI | ❌ | ❌ | ❌ | ✅ Bare metal | ❌ | ❌ N/A | ✅ | ❌ | 🟡 24-hr lease minimum | We're per-minute, no minimum |
| **iogrid** | Mesh | ✅ Residential | ✅ Docker | ✅ GPU (NVIDIA + Apple Silicon MLX) | ✅ Mac via Tart | ✅ Free mesh VPN | ✅ Cash + Free VPN + $GRID + charity | ✅ Live audit dashboard | ✅ $GRID (deflationary, locked) | ✅ Polished | The only horizontally integrated mesh |

### 2.2 Three-way positioning matrices

#### vs Residential proxy incumbents (Bright Data, Honeygain, Pawns, IPRoyal)

| Dimension | Bright Data ecosystem | Honeygain / Pawns | **iogrid** |
|-----------|----------------------|--------------------|-----------|
| Provider count | 72M+ (Hola legacy) | 1M / 500K | 0 today → 10K target Year 1 |
| Customer market | Enterprise scrapers | Mid-market scrapers | Same + iOS-CI buyers + general compute |
| Pricing to customer | $5–20/GB | $5–15/GB | **$0.30–0.60/GB** (10–30× cheaper) |
| Provider payout | Cash via PayPal/Stripe | Cash via PayPal/Stripe | Cash OR free VPN OR $GRID OR charity |
| Provider consent | Vague ToS | Vague ToS, no real-time visibility | **Live audit dashboard, per-byte category labels, one-click block** |
| Brand reputation | Hola scandal legacy | Clean but commodity | Anti-Hola transparency-first |
| Geo-targeting | ✅ Full | ✅ Full | ✅ Full |
| Session stickiness | ✅ Premium tier | 🟡 Limited | ✅ Default |
| API maturity | Excellent | Good | New (Phase 1+) |

**Our wedge:** transparency + multi-currency payouts (especially free VPN, which Honeygain cannot offer without their own VPN product). The $GRID alignment is a Year-2 amplifier, not a launch differentiator.

#### vs Distributed compute (Salad, Vast.ai, io.net)

| Dimension | Salad | Vast.ai | io.net | **iogrid** |
|-----------|-------|---------|--------|-----------|
| Provider hardware | Gamer GPUs | Mixed | Mixed | Home PC/Mac (any spec) |
| Workload types | Docker only | Docker (GPU) | GPU-focused | **Docker + GPU + bandwidth + iOS build** |
| iOS build | ❌ | ❌ | ❌ | ✅ — **first to market** |
| Bandwidth proxy | ❌ | ❌ | ❌ | ✅ |
| Provider acquisition pitch | "Earn from your gaming PC" | Cloud marketplace | Crypto-yield | "Same idle PC, 4 income streams" |
| Customer market | AI/ML researchers, video processors | AI researchers | Crypto-AI projects | Broader (any business workload) |
| Provider payout | $5/mo cash | Cash (USDC option) | Crypto IO | Cash + $GRID + free VPN + charity |
| Mainstream UX | ✅ | 🟡 power-user | 🟡 crypto-native | ✅ |

**Our wedge:** the four-workload bundle radically improves per-provider economics for Mac owners ($150/mo vs Honeygain's $9). Salad cannot add bandwidth without rebuilding their network. io.net cannot add mainstream UX without alienating their crypto base.

#### vs Consumer VPN (NordVPN, ProtonVPN, Mysterium, Hola)

| Dimension | NordVPN | ProtonVPN | Mysterium | Hola | **iogrid** |
|-----------|---------|-----------|-----------|------|-----------|
| Price (consumer) | $3–13/mo | Free + $4–10 paid | "Free" if you provide | "Free" but you ARE the product | **$0 / $2.99 Plus / $4.99 Pro** |
| How they fund free tier | Paid users | Paid users + grants | Token bandwidth swap | Reselling your bandwidth (no consent) | **B2B proxy revenue subsidises free consumer** |
| Logging | "No-log" claimed | "No-log" audited | "No-log" | ❌ Heavy | Provider audit log; coordinator can't decrypt customer HTTPS |
| Mesh network | ❌ Datacenter VPN | ❌ Datacenter VPN | ✅ P2P mesh | ✅ But abusive | ✅ Consensual mesh |
| Mobile app store | ✅ All platforms | ✅ All platforms | ❌ iOS only client | ✅ Some platforms | ✅ All platforms (consumer side only — provider PC/Mac via direct install) |
| Marketing budget | $50M+/yr | $5M+/yr | <$1M | <$1M | $0 (organic / cross-sell) |

**Our wedge:** truly free consumer VPN funded by enterprise customers, with cryptographically verifiable transparency (provider audit log proves no abuse). Mysterium has the right architecture but loses on UX. Hola has the audience but lost trust.

#### vs iOS-build CI (GitHub Actions, Bitrise, Codemagic, MacStadium, AWS EC2 Mac)

| Dimension | GitHub Actions | Bitrise | Codemagic | MacStadium | AWS EC2 Mac | **iogrid** |
|-----------|----------------|---------|-----------|-------------|--------------|-----------|
| Price / minute | $0.08 (Mac) / $0.16 (M-series) | $0.10–0.30 | $0.10–0.20 | $0.05–0.20 effective | $0.018 (lease) | **$0.04** |
| Lease minimum | None (per-min) | None | None | Monthly | **24 hours ($26 floor)** | None (per-min) |
| Mac hardware | M2 (rented) | Mixed | Mixed | M-series dedicated | M2 bare metal | Home Mac providers (any year, M-series favored) |
| Xcode versions | Latest 2 | Latest 3 | All versions | Latest 5 | Latest 3 | Provider-installed (any) |
| Spot capacity | ❌ Best-effort | ❌ | ❌ | ❌ | ✅ Limited | Plentiful (home Macs idle 16+ hr/day) |
| Network egress cost | Free (limit) | Free (limit) | Free (limit) | Bandwidth tax | $0.09/GB | Routed through iogrid mesh (covered) |
| Setup overhead | Minimal | Minimal | Minimal | Substantial | Substantial (VPC, EBS, security groups) | Minimal (Tart-spawned ephemeral VM) |

**Our wedge:** 50% cheaper than the cheapest no-commit option, with no minimum spend. Indie iOS devs save $30–80/mo immediately. Bonus: their CI runs on home hardware contributing to a network they can also use (free VPN). MacStadium's leases never lose, but the comparable price/minute requires 24+ months of consistent monthly spend.

### 2.3 Crypto-native sub-category

The token model puts iogrid into a sub-category of crypto-native mesh networks:

| Player | Token | Network type | Market cap (May 2026) | Daily active users | $GRID-relative position |
|--------|-------|--------------|------------------------|---------------------|-------------------------|
| **Helium** | HNT | IoT + 5G mesh | $1.5B | ~600K hotspots | Mature; we're not in IoT |
| **io.net** | IO | GPU compute | $400M | ~1K compute providers | Adjacent vertical, no mainstream UX |
| **Render Network** | RNDR | GPU rendering | $1.8B | ~12K rendering nodes | Specialised vertical |
| **Akash Network** | AKT | Cloud compute (Kubernetes-style) | $300M | ~300 hosts | Decentralised hosting niche |
| **Mysterium** | MYST | VPN mesh | $40M | ~50K | Same architecture, dying ecosystem |
| **Sentinel** | DVPN | VPN marketplace | $40M | ~70K | Same — niche |
| **Bonk / Wormhole / Jupiter** | (Various Solana tokens) | DEX / aggregators | $200M–4B | (Different vertical) | Token model reference (DEX-first launch validation) |

**Strategic takeaway:** the crypto-mesh-network field is real but currently small + crypto-native. iogrid's bet is that **mainstream-UX-first + crypto-as-payout-option** breaks out of that niche. We deliberately aren't "another crypto project" — the daemon runs without any wallet for the cash-payout tier; tokens are an OPT-IN payout currency, not a mandatory mechanic.

### 2.4 Defensible moats (what competitors cannot replicate easily)

| iogrid advantage | Why incumbent can't copy |
|------------------|--------------------------|
| **Transparency dashboard** (live per-byte category labels) | Honeygain/Pawns built on opaque-by-default infra; retrofitting = exposing customer behaviour to providers, which their customers would refuse. |
| **Multi-currency provider payouts** | Bright Data has no VPN product to use as currency. Salad has no VPN. Mysterium has the swap but no $GRID-equivalent yield-curve. |
| **iOS-build workload** | Requires Apple Silicon Macs in network. None of the proxy networks have Mac-disproportionate provider mix. |
| **DEX-first $GRID launch** | Established networks already raised in private rounds with VCs who want exit liquidity → forced into CEX listings → SEC exposure. We launch fresh, no legacy lockup. |
| **Anti-Hola brand positioning** | Bright Data IS Hola's lineage. They can't credibly claim transparency-first. |
| **OpenOva ecosystem cross-sell** (deferred, not advertised) | Behind-the-scenes synergy for provider acquisition. Founders/freelancers respond to "earn free email + storage" pitch even if we publicly emphasise cash. |
| **Mesh-VPN economics** | A "free VPN" funded by B2B revenue is the Mysterium / Hola model. Incumbents either don't have a VPN (Honeygain) or have lost trust (Hola). |

### 2.5 Strategic risks (competitive scenario)

1. **A well-funded entrant copies the transparency dashboard.** Possible. We respond by deepening the trust moat: independent audits, open-source the daemon (AGPL), publish quarterly transparency reports with real numbers.
2. **Bright Data acquires us.** Possible exit, not necessarily bad. Phase 4 consideration.
3. **A crypto-native player matches our mainstream UX.** Mysterium tries this every 18 months and fails. The crypto-native skill set rarely overlaps with consumer UX excellence.
4. **A regulator classifies $GRID as a security.** Mitigations in §6.4. Cayman Foundation + geo-restrictions + DEX-first reduce but don't eliminate risk.
5. **AI providers eat into B2B compute demand.** OpenAI's Atlas, Anthropic's Computer Use — automated agents may dramatically increase compute demand, not decrease it. Tailwind for us, not headwind.

### 2.6 Pricing wedge summary

For a single-Mac provider sharing 30 GB/mo bandwidth + 4 idle hr/day Xcode CI:

| Network | Provider monthly earnings | Trust posture |
|---------|---------------------------|---------------|
| Honeygain (bandwidth-only) | $9 | Opaque |
| Salad (idle GPU only) | $20–50 (GPU-dependent) | Limited transparency |
| iogrid Mac provider (bandwidth + iOS-build + free VPN tier) | $145–180 effective value | Live audit, multi-currency |

**For the same hardware contribution, iogrid pays 15–20× more in perceived value** because we're stacking 3–4 workload types AND offering ecosystem cross-sell as a payout currency. This is the per-provider economics moat.

---

## 3. Unit economics & provider incentives

> Source: previously `docs/INCENTIVES.md` (merged here on 2026-05-20).

### 3.1 The two-sided market

iogrid is a two-sided market with three rate-setting axes:

- **Provider side:** how much value providers receive per GB / CPU-hour / GPU-hour / Mac-minute they share.
- **Customer side:** how much customers pay for the same units of supply.
- **Margin:** the spread between, which funds infrastructure, anti-abuse, legal defence, customer support, and (eventually) profit.

Providers can be paid three different ways, but customers only pay one way (USD via Stripe, or $GRID directly per §4.5). The non-cash payout currencies are where margin compounds.

### 3.2 Provider payout tiers

When a provider installs iogrid, they pick how they want to be paid. They can switch between tiers any time. Most providers will pick the **highest face-value tier** they're eligible for, which is typically **OpenOva premium** or **free VPN** — both of which cost us pennies but feel like real value.

#### Tier 1 — Cash (Honeygain-equivalent)

- $0.30 per GB of bandwidth shared (industry standard ~$0.30–0.60/GB).
- Typical home device sharing 30 GB/month = **~$9 per month**.
- Stripe Connect payout, $10 minimum threshold.
- 1099 issued at year-end for >$600 (US tax compliance).
- Audience: people who actively google "passive income at home" — pure mercenary, brand-agnostic.
- **Our cost: $9/month per provider. Margin to coordinator: 0%.**

#### Tier 2 — Free VPN (the mesh-swap advantage)

- Their bandwidth share pays for unlimited iogrid VPN access on all their devices (iOS, Android, Mac, Windows, Linux).
- Marginal cost to iogrid: ~$0.20/month (VPN gateway infrastructure + DNS resolution).
- Provider perceived value: equivalent to NordVPN's $5/month plan.
- Audience: tech-savvy users who already pay $60–130/year for a VPN; they realise this is the same thing for free.
- **Our cost: $0.20/month. Margin: ~98%.**

#### Tier 3 — Charity / mission

- Provider's bandwidth-share revenue is donated to a chosen cause: EFF, Tor Project, Wikipedia, Doctors Without Borders, etc.
- Marginal cost equivalent to cash tier (we forward the donation).
- Audience: ideologically-motivated providers; small but PR-strong.
- Useful as a brand signal: "we're not extracting from users, we're enabling them to support causes they care about."
- **Our cost: $9/month per provider (donated). Margin: 0% but huge brand value.**

#### Tier 4 — $GRID (token, see §4)

The native payout currency for the network. Tier 4 supersedes the deferred-crypto framing in the older doc; $GRID is canonical (§4).

### 3.3 Customer-side pricing

| Workload | Pricing model | Per-unit | Notes |
|----------|---------------|----------|-------|
| **Bandwidth proxy** | Per GB | $0.30 – $0.60 | Geo-targeted, session-sticky |
| **Docker compute** | Per CPU-hour | $0.02 – $0.10 | Per RAM-GB-hour same rate |
| **GPU inference** | Per GPU-hour | $0.20 – $2.00 | Tiered by GPU class (consumer / pro / data-center) |
| **iOS builds** | Per Xcode-minute | **$0.04** | **50% under GitHub Actions** ($0.08/min) |
| **Mobile VPN (consumer)** | Per month | **$0 free / $2.99 Plus / $4.99 Pro** | Free tier has bandwidth cap |

Customer rates are deliberately ~30% under market for B2B workloads — gives us a clean wedge against Bright Data and Honeygain who charge $0.50–1.50/GB. We can underprice because:

1. Provider payouts are partially in non-cash currency (Tier 2/3 → 95%+ margin).
2. Marginal infrastructure cost is low (k8s on Contabo / Hetzner, no per-customer dedicated infra).
3. Anti-abuse and support are amortised across all customers.

### 3.4 Per-provider unit economics

#### Typical home Linux/Windows PC, sharing 30 GB/month

| Component | Provider takes | Our cost | Margin |
|-----------|----------------|----------|--------|
| Cash tier | $9 | $9 | 0% |
| Free VPN tier | "$5/mo perceived" | $0.20 | 96% |
| Charity tier | $9 to cause | $9 | 0% |

iogrid revenue per provider from B2B customers (sold at ~$0.50/GB): $15/month.
Net margin per provider (assuming 40% pick Tier 2): **~$5–7/month / provider**.

#### Typical Mac provider, 30 GB bandwidth + 4 hours/day Xcode CI

| Component | Provider takes | Our revenue | Net |
|-----------|----------------|-------------|-----|
| Bandwidth (30 GB cash) | $9 | $15 | +$6 |
| iOS builds (4hr × 30 × $1.20 take) | $145 | $290 (4hr × 30 × $2.40) | +$145 |
| **Total per Mac provider** | **$154 cash** | **$305 revenue** | **+$151 / month** |

Mac providers are 15× more valuable to iogrid than bandwidth-only providers, because iOS builds are a premium workload with thin competition (only GitHub Actions, MacStadium, Bitrise, AWS EC2 Mac in the market).

#### Tier mix example

100 providers, mix: 50 cash, 40 free-VPN, 10 charity.

| Provider count | Their take | Our cost | Net margin / month |
|----------------|-----------|----------|--------------------|
| 50 × cash | $9 each = $450 | $450 | $0 |
| 40 × free VPN | "$5 each = $200 perceived" | $8 | $192 |
| 10 × charity (donated) | $9 each = $90 | $90 | $0 (brand-value) |
| **Net coordinator margin** | | | **$192 / month** |

Revenue from these 100 providers at $15/month each: $1500.
Cost: $450 cash + $8 VPN + $90 charity = $548.
**Net margin: $952/month from a 100-provider base.**

At 1000 providers: ~$10K/month net. At 10K providers: ~$100K/month net.

Provider acquisition strategy lives or dies on Tier 2 (free VPN). The user who would pay $130/year for NordVPN saves real money and we save ~$9/month per such provider vs cash payouts. Marketing message: *"Why pay for VPN? Share your idle bandwidth instead. You see exactly what flows through. You control everything."*

### 3.5 Customer acquisition cost (CAC) targets

Provider-side:

- **YouTube passive-income sponsor:** $2–3 per install (Honeygain benchmark).
- **Reddit organic / SEO:** $0 per install but slow.
- **Referral program (30 days free VPN per referral):** ~$0.40/install (viral coefficient ~1.4 in this market).
- **OpenOva ecosystem cross-sell:** $0 (organic, our existing audience).

Customer-side (B2B):

- **Direct outbound to scraping companies:** $200–800 per signed customer.
- **Content marketing (technical blog: "iOS CI 50% cheaper"):** SEO play, slow ramp.
- **OpenOva ecosystem cross-sell:** $0.
- **HackerNews launch + ProductHunt:** spike, then taper.

LTV target:

- B2B customer LTV (3-year retention): $3K–30K depending on segment.
- Provider LTV: depends on payout tier (low for cash, high for OpenOva premium since they're tied to ecosystem).

### 3.6 Pricing strategy summary

1. **Underprice the market on customer side** — $0.30/GB vs Bright Data's $1+/GB, $0.04/Xcode-min vs GitHub's $0.08.
2. **Pay providers in non-cash currency where possible** — Tier 2/3 give them more perceived value, give us more margin.
3. **Lock in OpenOva ecosystem incentive** — competitors can't match this; provider stickiness is the moat.
4. **iOS-build workload is the secret weapon** — 50% under market in an undersupplied segment; Mac providers earn 15× bandwidth-only economics.
5. **Free consumer VPN is a feature, not a product** — it acquires providers (mesh-swap) and acquires users (free clients); B2B revenue subsidises both.

---

## 4. Currency model — $GRID + fiat hybrid

> Source: previously `docs/TOKENOMICS.md` (merged here on 2026-05-20).

**Architectural decision (2026-05-18):** provider payouts and customer payments shift from fiat-only (Stripe Connect / Stripe Subscriptions) to a hybrid model where **$GRID**, an iogrid-minted deflationary token, is the **native unit of account**. Fiat remains a supported on-ramp / off-ramp but is no longer the primary medium.

Founder intent: providers benefit from token value appreciation as the network grows, aligning their incentives with iogrid's long-term success. The deflationary mechanism is designed so that long-term holders capture upside without active trading.

### 4.1 $GRID vs $CASH — token positioning

**$GRID and $CASH are two distinct tokens, issued by two distinct legal entities, with non-overlapping utility.** They are **NOT merged**, **NOT cross-equity**, and **NOT renamed forms of each other**. The fact that Sociable Cash is the preferred off-ramp partner for $GRID providers does NOT change this separation — Cash is a tenant-neutral rail, iogrid is one tenant among many.

| | **$GRID** (iogrid's token) | **$CASH** (Sociable Cash's future token) |
|---|---|---|
| **Issued by** | iogrid Foundation (Cayman) | Sociable Cash Foundation (separate entity, jurisdiction TBD by Cash team) |
| **Project scope** | Distributed compute + bandwidth mesh | Multi-tenant stablecoin off-ramp rail |
| **Primary utility** | Work-token: paid to compute providers; 20% pay-in-$GRID discount for customers | Platform fee-discount token: held by Cash users to get cheaper off-ramps (Binance BNB model) |
| **Supply curve** | 1B cap, halving every 2 years, 2% revenue buyback-burn | Owned by Cash team |
| **Audience** | B2B compute (customers + providers) | B2C remittance (Cash users) |
| **TGE timing** | Coincides with iogrid mainnet launch | On Cash's own timeline (Year 2 once product-market fit proven) |
| **Regulatory posture** | Cayman Foundation, geo-blocks US persons at launch, Reg D/S for strategic raise | Cash's own MTL + KYC stack (out of iogrid scope) |
| **In-scope for this repo** | Yes — designed, audited, shipped by iogrid | **No** — owned by Cash team |

#### What "not merged" means concretely

- **Different SPL mints, different tickers, different supply curves.** $GRID is `$GRID` on Solana. $CASH is `$CASH` on Solana. Neither is a wrapped, bridged, or rebranded form of the other.
- **Different legal entities.** iogrid Foundation does not control Cash Foundation, and vice-versa. Token-holder rights, governance votes, and treasury policies are scoped to each Foundation independently. This is a deliberate regulatory-isolation choice — neither project's compliance posture contaminates the other.
- **Different audiences.** iogrid markets $GRID to compute providers and B2B customers. Sociable Cash markets $CASH to remittance senders/receivers and tenant projects. The marketing surfaces never cross-sell as if they were one product.

#### Mutual-incentive cross-investments allowed (NOT cross-equity)

- iogrid Foundation **may** hold a small treasury position in $CASH at Cash's TGE (aligns incentives, signals trust). This is a discretionary treasury investment, not a merger or equity stake.
- Sociable Cash **may** LP into the Raydium $GRID/USDC pool (deeper liquidity benefits iogrid users who off-ramp via Cash).
- Either Foundation may run cross-token incentive programs (e.g. "hold both $GRID + $CASH for stacked discount") **without** that constituting a token merger — the discount is a marketing rule, not a contract change.

#### Cross-references

- [`docs/MULTI_TENANT_MATRIX.md`](./MULTI_TENANT_MATRIX.md) — full capability matrix proving iogrid and AcmeMesh are symmetric tenants of Cash; iogrid's special status is "first tenant to integrate," not "owner."
- Issue [#167](https://github.com/iogrid/iogrid/issues/167) — EPIC: Off-ramp partnership model with Sociable Cash.
- Issue [#172](https://github.com/iogrid/iogrid/issues/172) — this section.

### 4.2 Canonical $GRID liquidity venue — Raydium CLMM

**The $GRID/USDC Raydium CLMM pool is the authoritative DEX-first liquidity source for $GRID.** All off-ramp routing — whether via Sociable Cash, MoonPay, Coinbase, or any future partner — discovers liquidity through this pool via the Jupiter swap aggregator. iogrid never routes provider payouts or customer swaps through a centralised exchange's order book; the Raydium pool is the venue.

#### Pool parameters at TGE

```
Pair: $GRID / USDC
Venue: Raydium CLMM (Solana — concentrated liquidity AMM, Uniswap v3 equivalent)

Seed:
- 5,000,000 $GRID (5% initial liquidity allocation from token-allocation table below)
- $250,000 USDC (from pre-TGE strategic raise proceeds)

Range: $0.05 – $5.00 (100× price-discovery range)
Fee tier: 0.25% (Raydium standard for new pairs)
LP tokens: locked for 4 years via Streamflow vesting contract
```

#### LP lock — 4-year vest, then permanent burn

LP tokens are deposited into a Streamflow vesting contract on TGE day with a **4-year linear vest** to the iogrid Foundation Squads multisig. **At end of vest the LP tokens are permanently burned**, locking the seeded liquidity in the pool forever.

This means:

- iogrid cannot rug-pull the pool — the LP is provably non-removable for 4 years, then non-removable forever.
- Anyone can verify on-chain by inspecting the Streamflow stream and the eventual LP-token burn tx.
- The 4-year horizon matches the team-vesting curve — incentive alignment by design.

#### Jupiter routing — the canonical swap path

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

#### LP-lock verification procedure (operator runbook)

To prove on-chain that the pool is locked:

1. Open `raydium.io/pools/<pool-id>` — pool-id published in `docs/ledger/TRACKER.md` at TGE.
2. Click "LP tokens" → "Holders". The Streamflow vesting contract should be the sole non-trivial holder.
3. Open Streamflow at `app.streamflow.finance/contract/<stream-id>` — stream-id published alongside pool-id.
4. Verify: recipient = iogrid Foundation Squads multisig; cliff/vest = 0/4 years from TGE; cancellable = false; transferable-by-sender = false.
5. After Year 4: verify the LP-token burn tx (sent to `1nc1nerator1111...`). Pool liquidity then locked permanently.

#### Pool-concentration adjustment protocol

As price discovers within the $0.05–$5.00 range, the LP range may be narrowed to concentrate liquidity for tighter spreads. Procedure:

1. **Proposal** by any Squads multisig signer with rationale (current price, current effective range, proposed new range).
2. **3-of-5 Squads vote** required to approve. Vote published on-chain.
3. **Atomic re-range tx** through Raydium CLMM's `decrease-liquidity` + `increase-liquidity` pair within a single Solana transaction. Slippage cap 0.5%.
4. Adjustments capped at one per 90 days to prevent active-management drift.
5. Each adjustment logged in a public registry (`burn.iogrid.org/lp-adjustments`).

#### CEX listings — aspirational, not blocking

Tier-1 CEX listings (Binance Spot, Coinbase, Kraken) are tracked as aspirational milestones, **not** prerequisites for $GRID utility or off-ramp functionality. Bonk, Jupiter, Wormhole, Pyth, and Helium all launched DEX-first on Solana without waiting for CEX listings; iogrid follows the same playbook. CEX listings, when they arrive, are additive distribution — they do not move the canonical price (Jupiter arbitrage keeps CEX prices pegged to the Raydium pool's mid).

### 4.3 Headline parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| **Symbol** | `$GRID` | Short, memorable, evokes the project name |
| **Network** | Solana (SPL token) | Sub-second finality, ~$0.0005 per tx, deepest non-EVM DEX liquidity, no per-action gas surprises for providers |
| **Initial supply** | 1,000,000,000 (1 billion) | Standard supply for SPL ecosystem tokens |
| **Decimals** | 9 (Solana SPL standard) | |
| **Emission curve** | Halving every 2 years | Bitcoin-style scarcity baked into the protocol |
| **Year-1 emission** | 50 million $GRID (5% of supply) | Bootstrap providers + ecosystem |
| **Year-10 cumulative** | ~485M emitted (~48.5% of supply) | Provider rewards pool exhausted; only burns remove tokens thereafter |
| **Burn rate target** | ≥2% of monthly revenue → market-buy → burn | Continuously reduces circulating supply as network scales |
| **Treasury custody** | Multisig (3-of-5 Squads Protocol) | Programmatic governance with founder + key contributors |

> **Transparency.** Treasury balance, emission progress, burns, staking participation, LP health, and foundation activity are published quarterly in [`docs/transparency/`](./transparency/README.md). See the [report template](./transparency/TEMPLATE.md) for the canonical shape and [`2026-Q2.md`](./transparency/2026-Q2.md) for the first scheduled publication.

### 4.4 Token allocation

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

Distribution proportions are settled defaults — material revisions go through an ADR. Locked at TGE (Token Generation Event).

### 4.5 Deflationary mechanism — multi-layered

#### Layer 1 — Buyback-and-burn from customer revenue (continuous)

- Customers pay in USD (Stripe) or USDC (on-chain) or $GRID directly.
- Daily automated process: **2% of all revenue** is converted to $GRID via Jupiter swap → burned to `1nc1nerator1111...` (Solana well-known burn address).
- Burns are public, on-chain, verifiable. Live dashboard at `burn.iogrid.org`.

#### Layer 2 — Emission halving (every 24 months)

| Year | Provider emission rate |
|------|------------------------|
| 0–2 | 50M / year |
| 2–4 | 25M / year |
| 4–6 | 12.5M / year |
| 6–8 | 6.25M / year |
| 8–10 | 3.125M / year |
| 10+ | 0 new emissions; only burns reduce supply |

Hard-coded into the SPL emission program; no governance can override.

#### Layer 3 — Mandatory provider-earnings lockup (the alignment mechanic)

**Every $GRID earned by a provider is auto-locked the moment it's distributed.** Providers cannot sell freshly-earned tokens; they earn into a vesting position that releases on a schedule.

Base lockup applied to ALL earned $GRID:

| Time since earned | % unlocked |
|-------------------|------------|
| Day 0 – 30 | 0% (cliff) |
| Day 30 – 90 | Linear vest 0% → 100% |
| Day 90+ | 100% unlocked (provider can sell, transfer, withdraw) |

This is **rolling per payout** — each weekly distribution starts its own 30/90-day clock. A provider who has been earning continuously will always have most of their balance in some stage of vesting.

**Why it works:**

1. **Stops day-1 dump.** Without lockup, every provider would convert $GRID → USDC the moment they receive it, crashing the price. With lockup, only ~33% of any month's earnings are sellable at any time.
2. **Compounds with deflation.** Locked tokens count toward "circulating supply removed" — they're untradable. As more providers join, the lockup pool grows, supply pressure shrinks.
3. **Skin in the game.** Providers care about $GRID price even after they stop providing — their vesting position keeps appreciating.

**Optional bonus lockup tiers (provider's choice, opt-in):**

| Lockup tier | Cliff + vest schedule | Rewards multiplier |
|-------------|----------------------|--------------------|
| **Standard** (default) | 30-day cliff + 60-day linear vest | **1.0×** |
| **Loyalty** | 90-day cliff + 180-day linear vest | **1.25×** |
| **Conviction** | 180-day cliff + 365-day linear vest | **1.5×** |
| **Maximum** | 365-day cliff + 730-day linear vest | **2.0×** |

A provider who picks the "Maximum" tier earns 2× the $GRID for the same work — but cannot touch it for 1 year, with another 2 years of linear vest. Designed for long-term holders.

Tier is set per-provider at onboarding; can be UPGRADED any time (locks more, never less), but cannot be downgraded.

**Early-unlock with penalty (escape hatch, not free):**

A provider in genuine financial need can early-unlock locked $GRID, but:

- **50% penalty** on the locked portion.
- The penalty is **burned** (not retained by iogrid) — strengthens deflation.
- One early-unlock event per 12 months per provider (anti-gaming).

So a provider with 10,000 locked $GRID who early-unlocks gets 5,000 unlocked tokens + 5,000 burned forever. Painful enough that few will use it, soft enough that we're not "trapping" anyone.

**Stake-while-locked:**

Locked tokens automatically count toward the provider's **routing-priority stake weight.** Providers don't lose yield/priority just because their tokens are vesting — they get the alignment benefit for free.

**Customer-side staking (separate, voluntary):**

Customers may also stake $GRID for **volume discounts** (up to 25% off list price). Minimum 30 days, customer's choice.

**Target supply lockup:**

By Year 2: **60%+** of circulating supply locked across provider earnings + voluntary customer stakes. This effectively halves the trading float, amplifying both upside volatility (good for holders) and the deflationary buy-and-burn impact (each $1 of burn removes 2× more % of circulating supply).

#### Layer 4 — Customer-pays-in-$GRID discount

- Customers paying in $GRID directly (no USD conversion) get **20% off list price**.
- The $GRID they pay flows back to providers + 2% burn.
- Creates persistent buy-pressure on the token as customers swap USD → $GRID to capture the discount.

### 4.6 Provider payout flow

```
┌──────────────────────────────────────────────────────────────┐
│ Customer payment ($1000 USD via Stripe)                      │
│                                                              │
│ Step 1: 2% buyback-and-burn                                  │
│   $20 USD → swap to $GRID on Jupiter → burn                  │
│                                                              │
│ Step 2: 98% provider rewards                                 │
│   $980 USD → swap to $GRID on Jupiter (TWAP over 1 hour)     │
│   → distribute to providers proportional to their contribution│
│   for that billing period                                    │
│                                                              │
│ Step 3: Provider on-ramp choice                              │
│   Provider holds $GRID in their connected wallet             │
│   Optionally: swap to USDC → off-ramp via partner (1% fee)   │
│   Optionally: stake $GRID for routing priority + yield       │
└──────────────────────────────────────────────────────────────┘
```

**Wallet requirement:**

- Providers MUST connect a Solana wallet (Phantom / Solflare / Backpack) during onboarding.
- iogrid never custodies provider tokens.
- "Cash payout" tier is replaced by "auto-convert to USDC and off-ramp" tier (additional 1% fee).

**Tax compliance:**

- $GRID earned is taxable as ordinary income at receipt (US treatment).
- iogrid emits a quarterly 1099-MISC equivalent based on token's USD price at time of receipt.
- Providers are responsible for capital gains on subsequent disposal.

### 4.7 Customer payment options

| Method | Discount | Settlement |
|--------|----------|------------|
| Stripe USD | List price | Instant |
| Stripe USDC | List price | Instant |
| On-chain USDC (Solana) | 5% off | <1 second |
| On-chain $GRID | 20% off | <1 second |

KYC requirement applies to fiat (Stripe AML) and to large on-chain USDC payments (>$10K/month via Sumsub or Persona). $GRID-only customers face per-wallet limits (analytics-based, sanctions-list checked).

### 4.8 Smart contract architecture

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

**Tech stack additions:**

- **Anchor** (Rust) — smart contract framework on Solana.
- **Streamflow** — token vesting + cliff schedules (audited, production-grade).
- **Squads** — multisig treasury.
- **Jupiter** — DEX aggregator for USD ↔ $GRID swaps.
- **Helius** — Solana RPC + indexing (existing fast lane).
- **Pyth** — price oracle (USD/$GRID) for fair-value swaps.
- **MoonPay** — fiat off-ramp for providers who want USDC → bank transfer (see §5).

### 4.9 Chain choice rationale — Solana primary, Base as bridge

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

### 4.10 Launch sequence — modern DEX-first playbook

| Phase | Action | Why |
|-------|--------|-----|
| **Pre-TGE strategic raise (Months 1–3)** | Reg D / Reg S, ~$2M @ $20M FDV → 10M tokens (1% supply) sold to accredited investors. CoinList private rounds or direct. | Funds legal + audit + initial liquidity. |
| **Smart contract dev + audit (Months 1–4)** | Anchor program development + audit by OtterSec or Halborn ($30–80K). | Required before TGE — no shortcuts. |
| **Mainnet TGE on Solana (Month 6–9)** | Token mint. Streamflow vesting contracts activated. Squads treasury multisig live. | TGE = official token genesis event. |
| **Bootstrap own liquidity pool (TGE Day 0)** | Seed Raydium CLMM with 5M $GRID + $250K USDC in concentrated range ($0.05–$5.00 = 100× price discovery range). | **THIS is our primary trading venue. No exchange required.** Anyone can swap USDC↔$GRID immediately. |
| **Jupiter Launchpad public sale (TGE Day 0–7)** | 5M tokens at fixed price via Jupiter Launchpad (decentralised, KYC for >$1K). | Public access, no 5–10% IEO fee that CEX launchpads charge. |
| **Bridge to Base (TGE Day 30)** | Wormhole NTT deploys $GRID on Base. | Coinbase off-ramp accessible for non-crypto-native providers. |
| **Tier-2 CEX listings (Month 6+)** | Bybit Launchpad, KuCoin, Gate.io. | Adds visibility; not mission-critical thanks to DEX-first launch. |
| **Tier-1 CEX listings (Year 1+)** | Binance Spot, Coinbase, Kraken. | Requires 6+ months organic volume, full legal review, audit reports. Aspirational. |

**Key insight:** by seeding our own Raydium CLMM pool at TGE, we eliminate the "we need to get listed somewhere" risk that early-stage tokens face. Bonk, Jupiter, Wormhole, Pyth, Helium — all launched DEX-first on Solana, none waited for CEX listings. Tier-1 CEX listings followed organically once volume + liquidity proved out.

### 4.11 Strategic raise — terms sketch

For founder reference if pursuing the $2M pre-TGE round:

- Tokens: 10M $GRID (1% of supply) at $0.20/token = $2M raised.
- FDV at strategic round: $200M.
- TGE launch FDV (DEX implied): $50M (4× discount for strategic investors).
- Vesting: 12-month cliff, 24-month linear vest after TGE (3 years total).
- Investor rights: information rights, no board seat, no veto, governance vote weighted equal to community.
- Use of proceeds:
  - Legal + counsel — $200K.
  - Smart contract audit — $50K.
  - Foundation setup — $100K.
  - Initial liquidity seed — $250K.
  - Tokenomics-specific engineering — $400K (1 senior dev × 12 mo).
  - Reserve / runway — $1M.

### 4.12 What changes (and doesn't) in the rest of the architecture

**Changes:**

- **`coordinator/billing-svc`** operates a Solana hot wallet (multisig 2-of-3), runs daily Jupiter swaps, manages provider payout queue, emits 1099-equivalent tax reports.
- **`coordinator/identity-svc`** gains a wallet-binding flow: providers connect Phantom / Solflare via SIWS (Sign-In-With-Solana) signature, bind it to their identity.
- **`web/`** management plane gains: wallet-connect button (Solana Wallet Adapter), $GRID balance display, staking UI (stake / unstake / claim rewards), off-ramp flow (USDC → bank via partner per §5), burn dashboard at `/burn` (public, no auth).
- **Customer-side payments:** Stripe stays primary (KYC-easier for enterprise), plus on-chain SDK for $GRID-paying customers.

**Unchanged:**

- The provider daemon (Rust) — never touches tokens directly. Coordinator handles all token logic.
- Anti-abuse, scheduling, transparency dashboard.
- The iOS-build / Docker / GPU / bandwidth workload split.
- The Honeygain-comparable provider UX — slightly higher friction (wallet connection step) but the daemon itself runs the same.
- Consumer VPN free in exchange for bandwidth share — same mesh-swap economics, no token requirement for consumer-side users.

---

## 5. Off-ramp partner integrations

> Source: previously `docs/OFFRAMP_PROVIDERS.md` (merged here on 2026-05-20).

This section is the partner-integration contract surface. The Go-level architecture lives at [`coordinator/services/billing-svc/internal/offramp/README.md`](../coordinator/services/billing-svc/internal/offramp/README.md). Architectural decision recorded in [#167](https://github.com/iogrid/iogrid/issues/167); the implementation PRs close [#169](https://github.com/iogrid/iogrid/issues/169) (web flow) and [#170](https://github.com/iogrid/iogrid/issues/170) (webhook receiver).

### 5.1 Why loose coupling

iogrid providers earn `$GRID` for compute / bandwidth / iOS-build work. When they want bank-deposit-able fiat, they need a partner who:

1. Takes custody of the `$GRID` (briefly, atomically).
2. Swaps to USDC (Jupiter / Raydium CLMM).
3. Settles fiat via local rails (ACH / wire / SEPA / GCash / M-Pesa).

Per founder direction 2026-05-19, iogrid does NOT do any of those three steps in-house. Every off-ramp is a partner integration with a **REST contract** — no shared code, no shared treasury, no shared legal entity. The contract surface is exactly four functions:

```go
type Provider interface {
    Name() string
    BuildRedirectURL(req OffRampRequest) (string, error)
    VerifyWebhookSignature(payload []byte, signature string) bool
    ParseWebhook(payload []byte) (*OffRampStatus, error)
}
```

The catalogue is registry-driven (env var `OFFRAMP_PROVIDERS`), so operators can enable/disable partners per environment without code changes.

### 5.2 Current catalogue

| Provider          | Status               | Real impl?                                                                                          |
|-------------------|----------------------|-----------------------------------------------------------------------------------------------------|
| **MoonPay**       | Default              | Yes — HMAC-SHA256 signed redirect URLs, `Moonpay-Signature-V2` webhooks.                            |
| **Sociable Cash** | Documented contract  | Stub — real adapter lives at `sociable-cloud/cash`. iogrid maintains the contract surface.          |
| **Coinbase**      | Placeholder          | Not yet wired — activates after the Wormhole NTT bridge to Base goes live.                          |

### 5.3 Provider redirect URL contracts

#### MoonPay

```
https://sell.moonpay.com/?apiKey=<key>
  &defaultBaseCurrencyCode=grid
  &baseCurrencyAmount=<decimal GRID>
  &quoteCurrencyCode=<usd|eur|...>
  &refundWalletAddress=<solana pubkey>
  &externalCustomerId=<iogrid user id>
  &externalTransactionId=<iogrid request_id>
  &redirectURL=<our return url>
  &signature=<HMAC-SHA256 base64 of the query-string under MOONPAY_WEBHOOK_SECRET>
```

#### Sociable Cash

```
https://cash.sociable.cloud/off-ramp?from=GRID
  &amount=<lamports>
  &signer=<provider wallet pubkey>
  &return_url=<our return url>
  &ref=<iogrid request_id>
  [&currency=<USD|EUR|PHP|...>]
```

(Unsigned today — Cash's KYC pipeline re-confirms swap amount with the user client-side. When Cash adds signed redirects we'll extend `OffRampRequest` with a `Secret` field; the interface is already designed for that.)

#### Coinbase (planned)

Coinbase Pay's redirect spec will be added once the Wormhole NTT bridge is live. The interface won't change — only the adapter body.

### 5.4 Webhook payload contracts

Every adapter implements `VerifyWebhookSignature` + `ParseWebhook`, but the wire shapes differ per partner. The canonical `OffRampStatus` shape the adapters produce is identical:

```ts
type OffRampStatus = {
  request_id: string;
  provider_id: string;
  status: "pending" | "swapping" | "off-ramping" | "completed" | "failed";
  grid_amount: number;       // lamports (uint64)
  fiat_amount: string;       // "150.00"
  fiat_currency: string;     // ISO-4217
  completed_at: string | null;
  txn_signature: string;     // on-chain GRID→USDC swap signature
  provider_ref_id: string;   // partner's internal id
};
```

#### MoonPay

- Header: `Moonpay-Signature-V2: t=<unix>,s=<hex hmac-sha256>`
- Signed payload: `<timestamp>.<raw body>`
- Body envelope:
  ```json
  {
    "type": "transaction_updated",
    "data": {
      "id": "<moonpay txn id>",
      "externalTransactionId": "<our request_id>",
      "externalCustomerId": "<our user id>",
      "status": "completed|waitingForSwap|...",
      "baseCurrencyAmount": 1.5,
      "quoteCurrencyAmount": 150.00,
      "quoteCurrencyCode": "usd",
      "cryptoTransactionId": "<solana sig>",
      "updatedAt": "<RFC3339>"
    }
  }
  ```

#### Sociable Cash

- Header: `Cash-Signature: <hex hmac-sha256>` over the raw body, under `CASH_WEBHOOK_SECRET`.
- Body envelope:
  ```json
  {
    "offramp_id":    "<cash internal id>",
    "ref":           "<our request_id>",
    "provider_id":   "<our user id>",
    "status":        "pending|swapping|off-ramping|completed|failed",
    "grid_amount":   "<decimal GRID, 9 dp>",
    "fiat_amount":   "150.00",
    "fiat_currency": "USD",
    "txn_signature": "<solana sig>",
    "completed_at":  "<RFC3339>"
  }
  ```

### 5.5 Env-var surface

| Env var                  | Required when                     | Description                                       |
|--------------------------|-----------------------------------|---------------------------------------------------|
| `OFFRAMP_PROVIDERS`      | Always                            | Comma-separated provider names in display order.  |
| `MOONPAY_API_KEY`        | `moonpay` in `OFFRAMP_PROVIDERS`  | MoonPay publishable key.                          |
| `MOONPAY_WEBHOOK_SECRET` | `moonpay` in `OFFRAMP_PROVIDERS`  | Signs redirect URLs + verifies webhooks.          |
| `MOONPAY_BASE_URL`       | Optional                          | Defaults to `https://sell.moonpay.com`.           |
| `CASH_WEBHOOK_SECRET`    | `sociable-cash` in `OFFRAMP_PROVIDERS` | Shared secret with the Sociable Cash team.   |
| `CASH_BASE_URL`          | Optional                          | Defaults to `https://cash.sociable.cloud`.        |

### 5.6 Contract gaps tracked for the Sociable Cash team

Outstanding items in [#167](https://github.com/iogrid/iogrid/issues/167):

1. **Quote endpoint** — Cash has not yet published a pre-redirect quote API. Until then iogrid renders `Estimated fiat: ~$X.XX` client-side from Pyth `$GRID/USD` × 0.97 (3% slippage buffer).
2. **Multi-rail routing** — when Cash adds GCash / M-Pesa rails, the `fiat_currency` string will gain values like `"PHP-GCASH"` so rail-aware reporting in billing-svc works.
3. **Atomic custody / refund flow** — today a failed off-ramp leaves the `$GRID` in the provider's wallet (Cash never custodied it). When Cash adds atomic custody they'll emit a `"refunded"` status that we'll map to `StatusFailed` with a non-nil completion timestamp.
4. **Signed redirects** — current redirect contract is unsigned. When Cash adds redirect signing, extend `offramp.OffRampRequest` with a `Secret` field and HMAC the query string. Interface is already designed for that.
5. **JWT-based webhook auth** — Cash may prefer JWTs over raw HMAC. The `VerifyWebhookSignature` interface accepts an opaque string so either scheme is compatible without changes upstream.

### 5.7 Adding a new partner

See [`coordinator/services/billing-svc/internal/offramp/README.md`](../coordinator/services/billing-svc/internal/offramp/README.md) for the step-by-step ("how to add a new provider"). The interface is small enough that a new partner integration is typically ~150 lines of Go + a config block + a README entry. We deliberately resist adding methods to `Provider` — every new method makes every adapter heavier. If a new partner needs a feature that doesn't fit (e.g. asynchronous quote generation, multi-step KYC handoff), prefer extending `OffRampStatus` or adding a new dedicated route, NOT widening the four-method core contract.

---

## 6. Legal risk landscape & mitigation

> Source: previously `docs/LEGAL.md` (merged here on 2026-05-20).
> Source: previously `docs/TOKENOMICS.md` §"Legal risk + mitigation strategy" (merged here on 2026-05-20).

### 6.1 Risk landscape

iogrid sits in the same regulatory neighbourhood as Bright Data, Honeygain, Pawns.app, IPRoyal, Proxycurl, and Salad. Public-data scraping itself is **not** illegal in the US per the *hiQ Labs v. LinkedIn* (CA9, 2017–2022, eventually settled) decision — but every node in the supply chain has potential exposure:

- **Providers** — their IP gets blamed for traffic they didn't initiate.
- **Coordinator** (us) — secondary-liability theories under copyright (DMCA), trespass-to-chattels, ToS violations of the destination services.
- **Customers** — primary liability for whatever they're doing.

Active litigation in 2024–2025 includes Meta v. Bright Data (Bright Data won, scraping public data ruled lawful), X v. Bright Data (still pending), and various CFAA cases involving smaller scrapers.

**Historical precedent for providers (where it's gone wrong):**

- **William Weber (Austria, 2014):** Ran Tor exit relay; CSAM transited through his IP; **convicted, 4 years probation.** No anti-abuse filtering, no commercial intermediary, no defence fund.
- **Moritz Bartl, Zwiebelfreunde e.V. (Germany):** Operates Tor exits via nonprofit, multiple home raids since 2012; charges dropped each time, but each raid = months of legal stress, lawyer fees.
- **Nolan King (US, 2007):** FBI raid for CSAM allegedly distributed via Tor exit; 2 years of legal hell; charges eventually dropped.
- **Honeygain & Bright Data providers:** No personal lawsuits known. Bright Data's TOS makes them the legal target; providers are shielded.

The reason commercial intermediaries take the legal hit is: deeper pockets, stronger anti-abuse defences, central audit logs that pinpoint customers. We have to maintain those defences or we lose the liability shield.

### 6.2 Mandatory anti-abuse before any external provider joins

These are blockers for Phase 1. They must be functional and verified before we onboard the first external provider.

#### Bandwidth workload pre-flight filters

For every outbound destination, before iogrid relays traffic:

1. **CSAM filter** — destination URL host + hash check against NCMEC's PhotoDNA database (free for registered orgs) and INTERPOL's hash list.
2. **Phishing / fraud filter** — check destination against PhishTank, OpenPhish, Google Safe Browsing (free APIs).
3. **Outbound port restrictions:**
   - No SMTP outbound (port 25, 465, 587, 2525) — no spam.
   - No IRC (port 6667, 6697) — no DDoS coordination.
   - No Tor exit ports (9001, 9030) — don't be a Tor exit ourselves.
   - No SSH brute-force patterns (rate-limit per-target SSH).
4. **High-risk target list:**
   - Banking domains: customer must explicitly request, KYC verified.
   - Government domains (.gov, .mil): block unconditionally.
   - Adult content domains: provider must explicitly opt-in.
5. **Per-customer rate limits:**
   - Default: 100 RPS aggregate.
   - Premium tier: 1000 RPS aggregate, KYC required.
6. **Per-provider rate limits per destination:**
   - No single provider IP serves more than 100 RPS to any one destination.
   - Hot destinations (LinkedIn, Facebook, Twitter, Google): max 10 RPS per provider per destination.

#### Docker workload filters

1. Container image must come from approved registry (default: ghcr.io, docker.io official-images, Dockerhub-verified-publisher namespace).
2. Coordinator scans image's published vulnerability list before scheduling.
3. Network namespace inside container: only outbound through iogrid bandwidth router (same filters above apply).
4. No privileged containers, no host filesystem mount, no host network namespace.
5. Resource caps enforced via cgroups.
6. Per-customer container submission rate limit.

#### iOS-build workload

1. Source code must come from a Git URL the customer authenticates with their token (we don't store the repo).
2. Tart VMs are ephemeral — destroyed after build, no state carries across customers.
3. Build output uploaded to coordinator's S3 with per-customer encryption keys.
4. Build time-boxed (default 30 min, max 4 hours).

#### Customer KYC thresholds

| Customer monthly spend | KYC requirement |
|------------------------|-----------------|
| <$100 | Email verification only |
| $100–500 | Business email + LinkedIn / corporate confirmation |
| $500–5K | Manual review, government ID for principal |
| >$5K | Stripe Identity + business registration verification + AML check |

### 6.3 Required documents (Phase 1 prerequisites)

These must be drafted by qualified counsel before external onboarding. Total cost expected: $5–10K.

#### 6.3.1 Provider Terms of Service

Must include:

- **Consent statement:** "I authorize iogrid to make my device act as a network exit for third-party traffic. I understand my IP will be visible to those third parties. I understand my IP may be flagged or temporarily blocked by some services as a result."
- **Common-carrier defence language:** "I act as a passive bandwidth intermediary. I have no knowledge of the content of the traffic I relay. iogrid operates pre-flight filters to block illegal content."
- **Indemnification clause:** "iogrid will defend, indemnify, and hold you harmless from claims arising from third-party use of your bandwidth, EXCEPT where you have violated this Agreement (e.g., disabled anti-abuse filters, knowingly routed illegal traffic)."
- **Audit-cooperation clause:** "iogrid retains 90 days of audit logs identifying the customer behind each request through your IP. We will use these to respond to law-enforcement inquiries and direct investigations away from you."
- **Revocation rights:** "You may pause or uninstall at any time. Pausing kills traffic instantly. Uninstalling deletes all telemetry from your device."
- **Tax compliance:** US providers earning >$600/year receive a 1099-NEC. EU providers handle local tax. We collect W-9 / W-8BEN on signup.

#### 6.3.2 Privacy Policy

Must include:

- What we log (bandwidth volume per device, uptime, approximate location for geo-targeting — never traffic content).
- Retention period: 90 days for audit logs, anonymised aggregates indefinitely.
- GDPR / CCPA / Brazilian LGPD lawful basis declarations.
- Data subject rights (access, deletion, portability).
- Sub-processor list (Stripe, NCMEC, PhishTank, Google Safe Browsing).

#### 6.3.3 Data Processing Agreement (DPA)

EU-required addendum to ToS. Specifies iogrid as a data processor when the provider is acting as a data subject, and specifies iogrid's processor obligations.

#### 6.3.4 Acceptable Use Policy (AUP)

What providers and customers cannot do via iogrid. Covers:

- No CSAM (zero tolerance).
- No human trafficking, exploitation, or harassment.
- No critical-infrastructure attacks (utilities, healthcare, finance).
- No election interference.
- No DDoS, including stress-testing without owner consent.
- No carding, fraud, or financial-crime facilitation.
- No mass-credential testing (credential stuffing).
- No bypass of anti-spam (no email-spam relay, no SMS-pumping).

Violations: immediate termination, audit-log forensics shared with law enforcement.

#### 6.3.5 Customer Terms of Service + AUP

Customer-side analog, with:

- Liability cap (we cap our liability at the customer's most-recent monthly spend × 12).
- Indemnity from customer (they hold us harmless for legal claims arising from their requests).
- Right of refusal (we can refuse to serve any customer at any time, for any reason).
- Audit rights (we may audit a customer's usage on suspicion of policy violation).

### 6.4 Token-specific legal risk ($GRID Howey analysis)

A token whose value providers "get impacted by" — exactly the founder's framing — sits squarely inside the **Howey test** for an investment contract:

1. Investment of money — yes (providers expect compensation).
2. Common enterprise — yes (network effect).
3. Reasonable expectation of profit — yes (deflationary mechanism is marketed as accruing value).
4. From the efforts of others — yes (iogrid's operations drive value).

A US court is likely to classify $GRID as a security if no mitigations are in place. Recent SEC actions (Coinbase, Binance, Kraken) confirm this trajectory.

#### Mitigations — required before TGE

1. **Geographic restrictions at launch.** No sales / airdrops to US persons. Geo-block US IPs from the token-purchase flow. Standard practice (Solana ecosystem norms).
2. **Token utility primacy in marketing.** Brand $GRID as the network's unit of work, not an investment. Never marketing-promise price appreciation. Treat it like AWS credits with a market.
3. **Foundation structure.** Establish a Cayman Foundation (zero income tax, non-profit form) or BVI Limited to hold treasury and govern the network. iogrid Inc. (Dynolabs's operating entity) licenses tech to the Foundation; Foundation issues tokens. Separates equity owners from token holders.
4. **Liechtenstein TVTG token-issuance license** OR **EU MiCA registration** (whichever cheaper at TGE time) for European market.
5. **Reg D / Reg S exempt offering** for any pre-TGE strategic raise. Accredited investors only.
6. **Counsel.** Top-tier crypto lawyer (Cooley, Fenwick, Davis Polk, Latham — pick by partner expertise) for at least:
   - Token legal opinion ($25–75K).
   - Foundation structuring ($30–80K).
   - Provider ToS amended for token economics ($10–20K).
   - Regulator outreach (no-action requests) ($variable).
7. **No "earn yield by holding" language.** Yield comes from staking work (routing priority), not from passive holding.
8. **Token whitepaper** published pre-TGE with clear utility narrative, risk factors, no forward-looking statements about price. The whitepaper itself lives at [`docs/whitepaper.md`](./whitepaper.md).

#### Risks we cannot fully mitigate

- SEC could classify $GRID as a security regardless of structure. Outcome: forced delisting from US exchanges, mandatory rescission offer to US holders, possible fines.
- Provider tax confusion (earning a volatile asset is harder than earning fixed USD) hurts adoption among non-crypto-native users.
- DEX liquidity could be inadequate at launch — large provider claims could crash the price short-term, eroding trust.
- $GRID price volatility makes B2B customer billing hard ("how much will this cost in USD next month?"). Discount-pegged-to-spot mechanism only partially addresses.

### 6.5 Legal defence fund

Phase 1 starts with **$10K initial pool**, replenished by **5–10% of B2B revenue** monthly.

Purpose:

- Cover provider legal fees if their IP is subpoenaed or LEO contacts them.
- Cover our own ToS-defence litigation costs (Section 230, common-carrier arguments).
- Retain outside counsel on a recurring basis (a partner-level tech attorney, ~$2K/month retainer once Phase 2).

Disbursement criteria:

- Provider received subpoena, LEO contact, or civil claim arising from iogrid traffic → fund pays their reasonable lawyer fees up to $25K.
- Provider violated AUP → fund pays nothing.
- Customer subpoenaed → fund pays nothing (customer is on their own per their ToS).

### 6.6 Insurance (Phase 2 prerequisite)

- **Cyber liability:** $1M coverage, ~$3K/year (covers data breach, ransomware, business interruption from cyber-attack).
- **E&O / Tech E&O:** $1M coverage, ~$5K/year (covers professional negligence claims by customers).
- **D&O:** $1M coverage, ~$2K/year (covers Dynolabs directors).

### 6.7 Jurisdiction & corporate structure

**Operating entity:** Dynolabs (existing). iogrid is a product line within Dynolabs, not a separate subsidiary at Phase 0/1. Phase 2 decision whether to spin out as separate company for liability isolation.

**Customer contracts governed by:** Delaware law (if Dynolabs is DE-incorporated; otherwise wherever Dynolabs is registered).

**Disputes:** binding arbitration in our home jurisdiction, customer waives class-action rights. Standard SaaS pattern.

**Provider contracts:** governed by provider's home jurisdiction (GDPR / regional consumer-protection laws are mandatory regardless).

### 6.8 Cooperation with law enforcement

- **Subpoena response:** standard, redirect to customer who owns the relevant audit log entry.
- **MLAT / international requests:** process via outside counsel.
- **Transparency report:** publish quarterly Phase 2 onward (number of requests received, jurisdictions, compliance percentage).
- **Warrant canary:** Phase 3 consideration (some networks use these; legal value debated).

### 6.9 What we won't do

- **Provider data sale** — bandwidth usage data is the provider's. We never sell it.
- **Backdoor access** — we won't insert backdoors at any government's request. Audit logs are accessible via subpoena, but no special-access channel.
- **Traffic content interception** — we relay encrypted bytes; we do not decrypt customer's HTTPS, ever.
- **Crypto-money-laundering services** — banking, mixer, sanctioned-jurisdiction targets are blocked.

### 6.10 Open items for the Phase 1 counsel brief

These items remain tracked for counsel review at Phase 1 onboarding, not deferred indefinitely:

1. Whether iogrid forms a separate LLC for liability isolation from Dynolabs core.
2. EU AI Act considerations for GPU/AI workload (we're "compute provider" not "model provider," but the line is unclear).
3. SOC 2 Type II target date — required for any enterprise customer at Phase 3.
4. Provider 1099 / VAT collection automation (currently manual via Stripe Connect).
5. Drafting of warrant canary policy.
