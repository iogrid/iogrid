# Incentives & unit economics

## The two-sided market

iogrid is a two-sided market with three rate-setting axes:

- **Provider side:** how much value providers receive per GB / CPU-hour / GPU-hour / Mac-minute they share
- **Customer side:** how much customers pay for the same units of supply
- **Margin:** the spread between, which funds infrastructure, anti-abuse, legal defense, customer support, and (eventually) profit

Providers can be paid three different ways, but customers only pay one way (USD via Stripe). The non-cash payout currencies are where margin compounds.

---

## Provider payout tiers

When a provider installs iogrid, they pick how they want to be paid. They can switch between tiers any time. Most providers will pick the **highest face-value tier** they're eligible for, which is typically **OpenOva premium** or **free VPN** — both of which cost us pennies but feel like real value.

### Tier 1 — Cash (Honeygain-equivalent)

- $0.30 per GB of bandwidth shared (industry standard ~$0.30–0.60/GB)
- Typical home device sharing 30 GB/month = **~$9 per month**
- Stripe Connect payout, $10 minimum threshold
- 1099 issued at year-end for >$600 (US tax compliance)
- Audience: people who actively google "passive income at home" — pure mercenary, brand-agnostic
- **Our cost: $9/month per provider. Margin to coordinator: 0%.**

### Tier 2 — Free VPN (the mesh-swap advantage)

- Their bandwidth share pays for unlimited iogrid VPN access on all their devices (iOS, Android, Mac, Windows, Linux)
- Marginal cost to iogrid: ~$0.20/month (VPN gateway infrastructure + DNS resolution)
- Provider perceived value: equivalent to NordVPN's $5/month plan
- Audience: tech-savvy users who already pay $60–130/year for a VPN; they realize this is the same thing for free
- **Our cost: $0.20/month. Margin: ~98%.**

### Tier 3 — Charity / mission

- Provider's bandwidth-share revenue is donated to a chosen cause: EFF, Tor Project, Wikipedia, Doctors Without Borders, etc.
- Marginal cost equivalent to cash tier (we forward the donation)
- Audience: ideologically-motivated providers; small but PR-strong
- Useful as a brand signal: "we're not extracting from users, we're enabling them to support causes they care about"
- **Our cost: $9/month per provider (donated). Margin: 0% but huge brand value.**

### Tier 4 (deferred) — Crypto tokens

- Stablecoin or governance token payouts
- Regulatory complexity is real (Howey test, AML for token sales)
- Our audience is not crypto-native — defer to Phase 4 if ever

---

## Customer-side pricing

| Workload | Pricing model | Per-unit | Notes |
|----------|---------------|----------|-------|
| **Bandwidth proxy** | Per GB | $0.30 – $0.60 | Geo-targeted, session-sticky |
| **Docker compute** | Per CPU-hour | $0.02 – $0.10 | Per RAM-GB-hour same rate |
| **GPU inference** | Per GPU-hour | $0.20 – $2.00 | Tiered by GPU class (consumer / pro / data-center) |
| **iOS builds** | Per Xcode-minute | **$0.04** | **50% under GitHub Actions** ($0.08/min) |
| **Mobile VPN (consumer)** | Per month | **$0 free / $2.99 Plus / $4.99 Pro** | Free tier has bandwidth cap |

Customer rates are deliberately ~30% under market for B2B workloads — gives us a clean wedge against Bright Data and Honeygain who charge $0.50–1.50/GB. We can underprice because:

1. Provider payouts are partially in non-cash currency (Tier 2/3 → 95%+ margin)
2. Marginal infrastructure cost is low (k8s on Contabo, no per-customer dedicated infra)
3. Anti-abuse and support are amortized across all customers

---

## Per-provider unit economics

### Typical home Linux/Windows PC, sharing 30 GB/month

| Component | Provider takes | Our cost | Margin |
|-----------|----------------|----------|--------|
| Cash tier | $9 | $9 | 0% |
| Free VPN tier | "$5/mo perceived" | $0.20 | 96% |
| Charity tier | $9 to cause | $9 | 0% |

iogrid revenue per provider from B2B customers (sold at ~$0.50/GB): $15/month
Net margin per provider (assuming 40% pick Tier 2): **~$5–7/month / provider**

### Typical Mac provider, 30 GB bandwidth + 4 hours/day Xcode CI

| Component | Provider takes | Our revenue | Net |
|-----------|----------------|-------------|-----|
| Bandwidth (30 GB cash) | $9 | $15 | +$6 |
| iOS builds (4hr × 30 × $1.20 take) | $145 | $290 (4hr × 30 × $2.40) | +$145 |
| **Total per Mac provider** | **$154 cash** | **$305 revenue** | **+$151 / month** |

Mac providers are 15× more valuable to iogrid than bandwidth-only providers, because iOS builds are a premium workload with thin competition (only GitHub Actions, MacStadium, Bitrise, AWS EC2 Mac in the market).

### Tier mix example

100 providers, mix: 50 cash, 40 free-VPN, 10 charity

| Provider count | Their take | Our cost | Net margin / month |
|----------------|-----------|----------|--------------------|
| 50 × cash | $9 each = $450 | $450 | $0 |
| 40 × free VPN | "$5 each = $200 perceived" | $8 | $192 |
| 10 × charity (donated) | $9 each = $90 | $90 | $0 (brand-value) |
| **Net coordinator margin** | | | **$192 / month** |

Revenue from these 100 providers at $15/month each: $1500
Cost: $450 cash + $8 VPN + $90 charity = $548
**Net margin: $952/month from a 100-provider base.**

At 1000 providers: ~$10K/month net. At 10K providers: ~$100K/month net.

Provider acquisition strategy lives or dies on Tier 2 (free VPN). The user who would pay $130/year for NordVPN saves real money and we save ~$9/month per such provider vs cash payouts. Marketing message: *"Why pay for VPN? Share your idle bandwidth instead. You see exactly what flows through. You control everything."*

---

## Customer acquisition cost (CAC) targets

Provider-side:
- **YouTube passive-income sponsor:** $2–3 per install (Honeygain benchmark)
- **Reddit organic / SEO:** $0 per install but slow
- **Referral program (30 days free VPN per referral):** ~$0.40/install (viral coefficient ~1.4 in this market)
- **OpenOva ecosystem cross-sell:** $0 (organic, our existing audience)

Customer-side (B2B):
- **Direct outbound to scraping companies:** $200–800 per signed customer
- **Content marketing (technical blog: "iOS CI 50% cheaper"):** SEO play, slow ramp
- **OpenOva ecosystem cross-sell:** $0
- **HackerNews launch + ProductHunt:** spike, then taper

LTV target:
- B2B customer LTV (3-year retention): $3K–30K depending on segment
- Provider LTV: depends on payout tier (low for cash, high for OpenOva premium since they're tied to ecosystem)

---

## Pricing strategy summary

1. **Underprice the market on customer side** — $0.30/GB vs Bright Data's $1+/GB, $0.04/Xcode-min vs GitHub's $0.08
2. **Pay providers in non-cash currency where possible** — Tier 2/3 give them more perceived value, give us more margin
3. **Lock in OpenOva ecosystem incentive** — competitors can't match this; provider stickiness is the moat
4. **iOS-build workload is the secret weapon** — 50% under market in an undersupplied segment; Mac providers earn 15× bandwidth-only economics
5. **Free consumer VPN is a feature, not a product** — it acquires providers (mesh-swap) and acquires users (free clients); B2B revenue subsidizes both
