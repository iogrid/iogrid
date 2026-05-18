# Competitive landscape

iogrid sits at the intersection of three markets that have historically been separate:
1. **Residential proxy networks** (~$1.5B/yr, growing 20%/yr)
2. **Distributed compute / GPU marketplaces** (~$200M/yr, growing 50%/yr)
3. **Consumer VPN** (~$50B/yr, mature)

Plus an emerging wedge into iOS-build CI (~$500M-1B/yr, growing 20%/yr).

No competitor bundles all of these. Existing players occupy one quadrant each. iogrid's strategic position is to be the first horizontally integrated mesh — which is also why the multi-currency provider payout (cash / VPN / tokens) and the radical transparency dashboard differentiate against all of them.

---

## Master comparison table

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

---

## Three-way positioning matrix

### vs Residential proxy incumbents (Bright Data, Honeygain, Pawns, IPRoyal)

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

### vs Distributed compute (Salad, Vast.ai, io.net)

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

### vs Consumer VPN (NordVPN, ProtonVPN, Mysterium, Hola)

| Dimension | NordVPN | ProtonVPN | Mysterium | Hola | **iogrid** |
|-----------|---------|-----------|-----------|------|-----------|
| Price (consumer) | $3–13/mo | Free + $4–10 paid | "Free" if you provide | "Free" but you ARE the product | **$0 / $2.99 Plus / $4.99 Pro** |
| How they fund free tier | Paid users | Paid users + grants | Token bandwidth swap | Reselling your bandwidth (no consent) | **B2B proxy revenue subsidizes free consumer** |
| Logging | "No-log" claimed | "No-log" audited | "No-log" | ❌ Heavy | Provider audit log; coordinator can't decrypt customer HTTPS |
| Mesh network | ❌ Datacenter VPN | ❌ Datacenter VPN | ✅ P2P mesh | ✅ But abusive | ✅ Consensual mesh |
| Mobile app store | ✅ All platforms | ✅ All platforms | ❌ iOS only client | ✅ Some platforms | ✅ All platforms (consumer side only — provider PC/Mac via direct install) |
| Marketing budget | $50M+/yr | $5M+/yr | <$1M | <$1M | $0 (organic / cross-sell) |

**Our wedge:** truly free consumer VPN funded by enterprise customers, with cryptographically verifiable transparency (provider audit log proves no abuse). Mysterium has the right architecture but loses on UX. Hola has the audience but lost trust.

### vs iOS-build CI (GitHub Actions, Bitrise, Codemagic, MacStadium, AWS EC2 Mac)

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

---

## Crypto-native competitors specifically

The token model puts iogrid into a sub-category of crypto-native mesh networks. Detail:

| Player | Token | Network type | Market cap (May 2026) | Daily active users | $GRID-relative position |
|--------|-------|--------------|------------------------|---------------------|-------------------------|
| **Helium** | HNT | IoT + 5G mesh | $1.5B | ~600K hotspots | Mature; we're not in IoT |
| **io.net** | IO | GPU compute | $400M | ~1K compute providers | Adjacent vertical, no mainstream UX |
| **Render Network** | RNDR | GPU rendering | $1.8B | ~12K rendering nodes | Specialized vertical |
| **Akash Network** | AKT | Cloud compute (Kubernetes-style) | $300M | ~300 hosts | Decentralized hosting niche |
| **Mysterium** | MYST | VPN mesh | $40M | ~50K | Same architecture, dying ecosystem |
| **Sentinel** | DVPN | VPN marketplace | $40M | ~70K | Same — niche |
| **Bonk / Wormhole / Jupiter** | (Various Solana tokens) | DEX / aggregators | $200M–4B | (Different vertical) | Token model reference (DEX-first launch validation) |

**Strategic takeaway:** the crypto-mesh-network field is real but currently small + crypto-native. iogrid's bet is that **mainstream-UX-first + crypto-as-payout-option** breaks out of that niche. We deliberately aren't "another crypto project" — the daemon runs without any wallet for the cash-payout tier; tokens are an OPT-IN payout currency, not a mandatory mechanic.

---

## What competitors can NOT replicate easily

| iogrid advantage | Why incumbent can't copy |
|------------------|--------------------------|
| **Transparency dashboard** (live per-byte category labels) | Honeygain/Pawns built on opaque-by-default infra; retrofitting = exposing customer behavior to providers, which their customers would refuse. |
| **Multi-currency provider payouts** | Bright Data has no VPN product to use as currency. Salad has no VPN. Mysterium has the swap but no $GRID-equivalent yield-curve. |
| **iOS-build workload** | Requires Apple Silicon Macs in network. None of the proxy networks have Mac-disproportionate provider mix. |
| **DEX-first $GRID launch** | Established networks already raised in private rounds with VCs who want exit liquidity → forced into CEX listings → SEC exposure. We launch fresh, no legacy lockup. |
| **Anti-Hola brand positioning** | Bright Data IS Hola's lineage. They can't credibly claim transparency-first. |
| **OpenOva ecosystem cross-sell** (deferred, not advertised) | Behind-the-scenes synergy for provider acquisition (founders/freelancers respond to "earn free email + storage" pitch even if we publicly emphasize cash). |
| **Mesh-VPN economics** | A "free VPN" funded by B2B revenue is the Mysterium / Hola model. Incumbents either don't have a VPN (Honeygain) or have lost trust (Hola). |

---

## Strategic risks

1. **A well-funded entrant copies the transparency dashboard.** Possible. We respond by deepening the trust moat: independent audits, open-source the daemon (AGPL), publish quarterly transparency reports with real numbers.
2. **Bright Data acquires us.** Possible exit, not necessarily bad. Phase 4 consideration.
3. **A crypto-native player matches our mainstream UX.** Mysterium tries this every 18 months and fails. The crypto-native skill set rarely overlaps with consumer UX excellence.
4. **A regulator classifies $GRID as a security.** See `docs/TOKENOMICS.md` and `docs/LEGAL.md`. Cayman Foundation + geo-restrictions + DEX-first reduce but don't eliminate risk.
5. **AI providers eat into B2B compute demand.** OpenAI's Atlas, Anthropic's Computer Use — automated agents may dramatically increase compute demand, not decrease it. Tailwind for us, not headwind.

---

## Pricing wedge summary

For a single-Mac provider sharing 30 GB/mo bandwidth + 4 idle hr/day Xcode CI:

| Network | Provider monthly earnings | Trust posture |
|---------|---------------------------|---------------|
| Honeygain (bandwidth-only) | $9 | Opaque |
| Salad (idle GPU only) | $20–50 (GPU-dependent) | Limited transparency |
| iogrid Mac provider (bandwidth + iOS-build + free VPN tier) | $145–180 effective value | Live audit, multi-currency |

**For the same hardware contribution, iogrid pays 15–20× more in perceived value** because we're stacking 3–4 workload types AND offering ecosystem cross-sell as a payout currency. This is the per-provider economics moat.
