# Market & competitive landscape

## Total addressable markets

### Residential proxy

- ~**$1.5B annual market**, growing ~20%/year
- Customer segments: e-commerce monitoring (40%), SEO/SERP scraping (15%), ad verification (10%), lead-gen scraping (10%), social media intelligence (10%), brand protection (5%), AI training data (5%), travel aggregation (3%), threat intel (2%)
- Customer price points: **$5–15 per GB** retail
- Provider payout share: $0.30–0.60 per GB (margin is the spread)

### Distributed compute / GPU inference

- **~$25M/yr** captured by Salad alone (the leader in gamer-GPU compute)
- Vast.ai, io.net, Akash, Render Network combined: another ~$50M
- Market growing fast with LLM/AI demand
- Customer price points: $0.20–2.00 per GPU-hour
- Provider payout: $0.05–0.50 per GPU-hour

### iOS build CI

- **~$500M–$1B/yr market**, growing ~20%/year (every mobile app needs iOS CI)
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

iogrid's offer at **$0.04 / minute** would be the cheapest non-leased option in the market. The provider's Mac sitting idle 4 hours/day earns ~$145/month (vs $9/month bandwidth-only economics).

### Consumer VPN

- ~**$50B annual market** (broad consumer VPN, including free + paid tiers)
- Top players: NordVPN, ExpressVPN, Surfshark, ProtonVPN, Mullvad
- Pricing: $3–13/month subscription
- iogrid's wedge: **free forever** for users (bandwidth-swap subsidizes); $2.99 Plus tier for unlimited / Pro for $4.99 with privacy features

---

## Competitive matrix — residential proxy

| Player | Revenue | Provider count | Provider model | Customer model | Strengths | Weaknesses |
|--------|---------|----------------|----------------|----------------|-----------|------------|
| **Bright Data** | ~$300M/yr | ~72M devices | Hola users (free VPN swap) + commercial | Enterprise B2B | Scale, brand, OG | Hola legacy, opaque, expensive, $20+/GB enterprise |
| **Honeygain** | ~$25M/yr | ~1M devices | Cash payouts | B2B + research | Easy to install, clear payouts | Cash-only, no transparency, App Store delisted |
| **Pawns.app (IPRoyal)** | ~$30M/yr | ~500K devices | Cash payouts | B2B + research | Similar to Honeygain | Same limitations |
| **EarnApp (BrightData)** | n/a (part of BrightData) | ~10M devices | Cash payouts | Same as BrightData | Distribution leverage | Hola legacy |
| **PacketStream** | ~$5M/yr | ~100K | Cash | B2B | Niche | Small scale |
| **Mysterium Network** | unknown | ~50K | Crypto tokens + free VPN | B2B + consumer VPN | Decentralized, mesh VPN model | Crypto-native UX, niche audience |

**iogrid's positioning:** "Cleaner Mysterium" — bandwidth-swap mesh VPN with B2B revenue, but mainstream UX (no crypto), transparent consent (anti-Hola), multi-currency payouts (cash OR VPN OR ecosystem premium OR charity).

---

## Competitive matrix — distributed compute

| Player | Revenue | Workload | Provider hardware | Customer market |
|--------|---------|----------|-------------------|------------------|
| **Salad** | ~$25M/yr | Docker workloads, ML inference, rendering | Gamer GPUs, idle PCs | AI / ML, video processing |
| **Vast.ai** | ~$10M/yr | GPU compute marketplace | Mix of consumer + data center | AI researchers, ML practitioners |
| **io.net** | ~$10M/yr | Decentralized GPU for AI inference | Mixed | Crypto-AI projects |
| **Akash Network** | <$5M/yr | Kubernetes-style decentralized | Data center providers | Crypto-native |
| **Render Network** | <$5M/yr | GPU rendering (Blender, Octane) | Specialty rendering rigs | 3D artists / studios |

**iogrid's positioning:** the first network to bundle compute with bandwidth + iOS builds. Salad has the gamer-GPU market; we don't compete there directly. We win in **Mac providers for iOS builds** (where Salad has zero) and **general home-Linux Docker workloads for B2B** (where Salad is gamer-focused).

---

## Competitive matrix — iOS build CI

| Player | Cost/min | Provider model | iogrid wedge |
|--------|----------|----------------|--------------|
| **GitHub Actions** | $0.08 | Microsoft-owned data center Macs | Underprice 50% |
| **Bitrise** | $0.10–0.30 | Self-owned + AWS Mac | Underprice 60–80% |
| **Codemagic** | $0.10–0.20 | Self-owned | Underprice 60–80% |
| **MacStadium** | $0.05–0.20 effective | Dedicated lease | Match price, no lease commitment |
| **AWS EC2 Mac** | $0.018 effective | AWS-owned | We're more expensive per min BUT no 24-hour minimum |

iogrid is the only **pay-per-minute, no-commitment** option that's cheap. AWS EC2 Mac is cheaper per minute but locks users into 24-hour minimums ($26 floor per build session) — not viable for typical indie iOS dev workflows.

---

## What makes iogrid different — defensible moats

1. **Transparency-first brand** (anti-Hola). Provider dashboards show every byte categorized. Hard for incumbents to retrofit this — Bright Data has 10 years of opacity to undo.
2. **OpenOva ecosystem cross-sell.** No competitor can offer "free vCard Pro + free yours@openova.io + free domain" as a payout currency.
3. **Mac iOS-build workload.** Salad has gamer GPUs, but no one has organized home Mac CI. Apple licensing locks builds to Mac hardware → real supply constraint we can exploit.
4. **Bandwidth-swap mesh VPN.** Mysterium proved the model works but stayed crypto-niche. We execute the same model in polished mainstream UX.
5. **Underpricing on customer side.** ~30% below market on B2B workloads, made possible by Tier 2/3 non-cash provider payouts (95%+ margin on those tiers).

---

## What could go wrong (real risks)

- **App Store removal of related Dynolabs products.** Apple has explicit policy 5.4.6 — they could retaliate against vCard if iogrid is too prominently associated. **Mitigation:** independent brand, no co-marketing on App Store, Dynolabs ownership only disclosed in legal records.
- **LinkedIn / Meta sue for facilitating scraping.** Bright Data's wins suggest we're defensible, but legal costs are real. **Mitigation:** strong AUP, customer indemnification, defense fund.
- **CSAM transits through a provider's IP.** Even with PhotoDNA, edge cases happen. **Mitigation:** aggressive pre-flight + provider notification + LE cooperation + defense fund covers their legal fees.
- **Apple deprecates Tart-style VM execution.** Tart relies on Apple's Virtualization framework; if Apple changes terms, our iOS-build workload dies. **Mitigation:** maintain Anka fallback (commercial alternative), keep iOS builds non-exclusive workload.
- **Provider acquisition is hard.** Honeygain spent ~4 years to hit 1M. We need ~10K for Phase 2 viability. **Mitigation:** mesh-swap free VPN angle + OpenOva ecosystem cross-sell + iOS-build economics (15× bandwidth-only).
- **Hola scandal repeat.** If we're sloppy with consent or anti-abuse, journalists will write "iogrid is the new Hola." **Mitigation:** transparency dashboards are the public defense; we publish quarterly transparency reports starting Phase 2.

---

## 3-year revenue trajectory (conservative)

| Year | Active providers | B2B MRR | VPN MRR (Plus/Pro) | Net margin |
|------|------------------|---------|--------------------|-----------|
| **Year 1 (Phase 0/1)** | 100 | $1K | $200 | -$30K (lawyer + infra investment) |
| **Year 2 (Phase 2)** | 5K | $50K | $5K | +$300K |
| **Year 3 (Phase 3)** | 50K | $500K | $50K | +$3.5M |

iogrid breaks even mid-Year 2 (after Phase 1 → Phase 2 transition). Year 3 ARR target: $6.5M ($550K × 12). Aggressive case (Phase 2 lands a marquee enterprise customer at $50K/month): $10M+ ARR by Year 3.
