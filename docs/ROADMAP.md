# Roadmap

## Phase 0 — Internal pilot (2 weeks)

**Goal:** prove the architecture works end-to-end. Bandwidth workload only. Single internal customer: Dynolabs vCard's LinkedIn enrichment.

**Scope:**
- 1 coordinator running on existing Contabo mothership k8s
- 1 provider daemon binary, installed on founder's home Mac via SSH
- Optional: 2–4 additional provider devices (founder's phones, home Linux/Windows PCs)
- Single workload type: SOCKS5 bandwidth proxy
- Static API key auth (no Stripe billing yet)
- Minimal "is anything live" dashboard

**Internal customer (vCard LinkedIn enrichment):**
- vcard-api's `/v1/enrich/email` handler currently calls Apollo (returns empty without paid plan)
- Phase 0 makes vcard-api route LinkedIn-profile-scraping requests through iogrid bandwidth proxy
- Resolves the Build-170 "title/company not imported" issue at zero per-lookup cost
- Validates: routing works, anti-abuse filter is sound, latency is acceptable

**Out of scope for Phase 0:**
- Docker / GPU / iOS-build workloads (Phase 1)
- Provider billing or payouts (just one provider, the founder)
- Public dashboard / web UI
- Provider transparency dashboard (single internal use, no need yet)
- Cross-platform installers (only Mac via SSH for now)

**Engineering deliverables:**
- `coordinator/` — Go microservices (identity-svc, providers-svc, workloads-svc, antiabuse-svc, proxy-gateway, gateway-bff) over Connect-Go + a long-lived provider transport; SOCKS5 customer entry on `proxy.iogrid.org:443`
- `daemon/` — **Rust** single static binary cross-compiled `darwin/arm64` and `linux/amd64`. Provides outbound TCP via WireGuard tunnel + sandboxed relay.
- `infra/k8s/` — coordinator Deployments, Services, Gateway API routes, ConfigMaps, DB CR (CloudNativePG `iogrid-pg`). Note: manifests are **not Flux-wired yet** — live deploy via `scripts/reroll-iogrid-deployments.sh` (image-only).
- Provider Mac install instructions (folded into the install runbooks under `docs/runbooks/`)

**Success criteria:**
- vCard LinkedIn enrichment calls succeed via iogrid bandwidth, return title + company + photo
- 99%+ uptime over a 5-day soak test
- Per-request latency p95 < 600ms (versus Proxycurl's ~1s)
- Zero LinkedIn account flags (founder's home IP, single provider)

**Timeline:** 2 weeks from name confirmation.

---

## Phase 1 — Closed beta (6–9 months from Phase 0)

**Goal:** validate the multi-provider model + multi-workload platform. Onboard external providers and customers.

**Scope:**
- 10–100 invited providers (founder's network, friends-of-friends, opportunistic crypto/passive-income community recruitment)
- 1–3 design-partner B2B customers (likely Dynolabs-portfolio adjacent at first)
- All 4 workload types live: bandwidth, Docker, GPU, iOS build
- Provider transparency dashboard live
- Anti-abuse v1: PhotoDNA CSAM hash check, PhishTank URL blocklist, port restrictions
- Free-VPN incentive tier live (provider gets unlimited VPN in exchange for bandwidth share)
- OpenOva-premium incentive tier live (provider gets free vCard Pro + Stalwart email + custom domain)
- Lawyer-drafted Provider ToS + DPA + AUP + Customer ToS + AUP
- $10K legal defense fund seeded

**Capital required:** ~$50K–100K (lawyer ~$10K, infra ~$20K/yr, provider hardware experiments ~$5K, partner support effort, defense fund ~$10K)

**Engineering deliverables:**
- All 4 workload modules production-ready
- Cross-platform daemon binaries: macOS Intel/AS, Windows x64, Linux x64/ARM64
- Direct-download installers (NOT in app stores per Apple Guideline 5.4.6)
- Provider self-service web UI (settings, payout method, opt-ins, audit log)
- Customer self-service web UI (API keys, usage, billing, audit log)
- Stripe Connect for provider payouts (cash tier)
- Stripe Subscriptions for customer billing
- VPN gateway (consumes from same provider pool, separate ingress)

**Success criteria:**
- Provider retention >50% at 90 days
- Customer MRR >$1K
- Zero CSAM hits leaving the network (filter audit)
- iOS-build pilot live with at least one external iOS dev customer

**Timeline:** Phase 1 begins immediately after Phase 0 ships. Closed beta runs 6–9 months.

---

## Phase 2 — Commercial launch (12 months from Phase 1)

**Goal:** open public registration. Scale provider base 10–100× (1K–10K active devices). First major customer wins.

**Scope:**
- Public marketing site (`iogrid.org`) — provider acquisition funnel, customer pricing page
- Crypto-skeptical brand: no tokens, no governance — straightforward SaaS model with payouts
- Cross-product cross-sell from OpenOva portfolio (Dynolabs vCard "earn free Pro" upsell, etc.)
- KYC/AML for provider payouts >$600/year (US 1099, EU equivalents)
- Anti-abuse v2: ML-based traffic classification, behavioral fingerprinting, per-customer abuse pattern detection
- Cyber liability insurance ($1M coverage, ~$3K/yr)
- Support team (1–2 humans, mostly async chat + email)
- Per-country geo-targeting at customer side
- Sticky session quotas (premium tier feature)
- iOS-build marketing push to indie iOS dev community (50% under GitHub Actions pricing is the lede)

**Capital required:** ~$400K–700K (team of 3 engineers + 1 support + marketing budget)

**Engineering deliverables:**
- Multi-region coordinator federation (US-East, EU-West)
- Provider acquisition mobile-friendly site (`https://provide.iogrid.org`)
- Referral program: 30 days free VPN per referred provider
- Customer Slack/Discord communities
- OpenTelemetry stack (Grafana mothership integration)

**Success criteria:**
- 1K+ active provider devices
- $20K+ MRR
- Net Promoter Score >40 on provider side
- LinkedIn / Twitter / TikTok review trail confirms transparency claims

**Timeline:** Phase 2 begins after Phase 1 has 3+ months stable. Commercial launch milestone in month 18 overall.

---

## Phase 3 — Scale (18+ months from Phase 2)

**Goal:** become a meaningful player in residential proxy + distributed compute. Profitability inflection.

**Scope:**
- $2M+ runway (potential funding or self-funded via revenue)
- Enterprise sales (named contracts >$5K/month)
- SOC 2 Type II certification
- 24/7 oncall + support
- Federated coordinator (multiple operating parties, possibly open-source the federation protocol)
- iOS-build product split into separate brand (e.g., `mac.iogrid.org`) if it grows large enough
- B2B partnerships: white-label iogrid proxy for SaaS scraping companies

**Revenue trajectory benchmarks (existing players for context):**
- Honeygain — ~$25M/yr ARR, ~1M devices, bandwidth-only, opaque consent
- IPRoyal — ~$30M/yr, similar profile
- Bright Data (residential proxy line) — ~$300M/yr, gold standard, has Hola legacy
- Salad — ~$25M/yr, gamer GPU compute, the "Honeygain of compute"

iogrid's target by year 3: $5–15M ARR (proxy + iOS-build + compute combined). Achievable if Phase 2 hits the >$20K MRR mark and we maintain unit economics.

**Strategic decisions deferred to Phase 3:**
- Open-source the daemon? (AGPL increases trust, especially post-Hola; lets us argue defensibility to LE)
- Tokenize provider payouts? (Probably no — defer to Phase 4 if at all)
- Pursue Marketing Developer Platform partnership with LinkedIn? (Would let us drop residential-proxy fallback for LinkedIn enrichment specifically)
- Federation: open the protocol so partners can run their own coordinators against shared provider pool

---

## Critical-path dependencies

```
Phase 0 ─┬─ vCard LinkedIn enrichment works
         │
         ▼
Phase 1 ─┬─ Provider ToS lawyer-drafted ($10K, ~6 weeks lead time)
         ├─ Anti-abuse v1 production (CSAM hash, PhishTank, port filters)
         ├─ Stripe Connect onboarding (KYC for first paying providers)
         ├─ Free-VPN gateway built (consumes same provider pool)
         │
         ▼
Phase 2 ─┬─ Provider acquisition funnel proven (>50% retention at 90d)
         ├─ At least one B2B customer >$1K MRR for 3+ months
         ├─ iOS-build workload validated with external iOS dev customer
         │
         ▼
Phase 3 ─┬─ Public marketing launch (would-be PR risk — needs all anti-abuse working)
         ├─ Multi-region federation operational
```

## What we will NOT pursue (decided)

- **Bundling iogrid into the Dynolabs vCard iOS app** — Apple's Guideline 5.4.6 forbids it; would risk vCard's App Store status
- **Crypto tokens for provider payouts** — regulatory complexity, our target users aren't crypto-native
- **Real-time audio/video meeting recording workload** — legal exposure around consent, latency intolerant to home jitter
- **Decentralized blockchain coordinator** — adds operational complexity without product benefit for our customers
- **iOS-app providers** — single biggest workload-type miss, but unavoidable per Apple platform policy
