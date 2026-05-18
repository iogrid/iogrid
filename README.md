# iogrid

**Distributed compute + bandwidth mesh for the post-cloud era.**

iogrid is a peer-to-peer network where home PC/Mac owners share their idle compute and bandwidth with paying enterprise customers. Providers get cash, free unlimited VPN, or free premium services in return. Customers get residential-IP proxy, Docker compute, GPU inference, and macOS-native iOS builds at a fraction of cloud prices.

A Dynolabs initiative, independent of OpenOva/vCard.

---

## What we offer

### To providers (home users with idle PCs/Macs)

A single installer that turns your always-on home device into an earner. Pick how you get paid:

- **Cash** — Stripe payouts, ~$10–50/month per device
- **Free unlimited VPN** for all your devices (mesh-swap economics — costs us pennies)
- **Free OpenOva ecosystem premium** — vCard Pro + custom-domain email + cloud storage (~$25–40/month retail value)
- **Charity donation** — bandwidth share funds EFF, Tor Project, etc.

You see every byte flowing through your IP. You block any category or destination you don't want. You revoke consent and uninstall any time.

### To customers (enterprises that need real residential IPs or affordable compute)

Four workload types, one API:

| Workload | What it's for | Customer pricing |
|----------|---------------|------------------|
| **Bandwidth proxy** | E-commerce scraping, SEO tracking, ad verification, social media intelligence | ~$0.30–0.60 / GB |
| **Docker compute** | Batch processing, ML inference, internal jobs | ~$0.02–0.10 / CPU-hour |
| **GPU / AI inference** | LLM serving, vision models, MLX/CUDA workloads | ~$0.20–2.00 / GPU-hour |
| **iOS builds** | Xcode CI on real Mac hardware (50% under GitHub Actions) | ~$0.04 / minute |

Geo-targeting by country, session stickiness, real-time telemetry, audit logs.

---

## Why this exists

Existing residential-proxy networks (Bright Data / Hola, Honeygain, Pawns, IPRoyal) have a known problem: **opaque consent.** The 2015 Hola scandal — where free-VPN users were turned into a botnet without their knowledge — remains the cautionary tale of the industry. Providers don't see what their IP serves. Bandwidth gets resold for purposes the provider would refuse if asked.

Existing distributed-compute networks (Salad, Vast.ai, io.net) focus on GPUs for gamers or crypto-native users. iOS build CI in particular is wildly undersupplied — every iOS developer pays GitHub Actions ~$0.08/min for macOS runners because Apple's licensing locks builds to Mac hardware. A coordinated network of home Mac providers could undercut by 50%+.

iogrid bundles both — bandwidth + compute — into one platform under one daemon, with three core differentiators:

1. **Radical transparency** — providers see exactly what flows through their IP, categorized and audited
2. **Multi-currency incentives** — providers choose cash, VPN, premium services, or charity
3. **First-class iOS-build workload** — solving a real CI/CD pain point for the world's iOS developers

---

## Status

**Phase 0 (internal pilot, ~2 weeks):** founder's home Mac + 5 phones + 3 home networks. Single coordinator on existing k8s. First internal customer: Dynolabs vCard LinkedIn enrichment, replacing the Proxycurl/Apollo dependency at zero per-lookup cost.

**Phase 1 (closed beta, 6–9 months):** 10–100 invited providers. Lawyer-drafted Provider ToS + DPA + AUP. First 1–3 design-partner B2B customers. Free-VPN and OpenOva-premium incentive tiers active.

**Phase 2 (commercial, +12 months):** Stripe billing, KYC/AML for payouts, anti-abuse v2, iOS-build workload rolls out, support team.

**Phase 3 (scale, +18 months):** marketing, enterprise sales, compliance, $2M+ runway.

See [`docs/ROADMAP.md`](./docs/ROADMAP.md) for the detailed phase plan.

---

## Repository layout

```
iogrid/
├── coordinator/    Go service running on k8s — provider registry,
│                   workload scheduler, customer API, billing,
│                   transparency dashboard, anti-abuse.
├── daemon/         Go binary (cross-compiled for darwin/amd64,
│                   darwin/arm64, linux/amd64, linux/arm64,
│                   windows/amd64). Installed on provider devices.
├── proto/          gRPC + protobuf definitions for daemon ↔ coordinator
│                   and customer-facing API.
├── infra/k8s/      Coordinator k8s manifests (Flux-managed).
└── docs/           Architecture, roadmap, incentive model, legal
                    requirements, anti-abuse design.
```

---

## Key documents

- [`docs/ARCHITECTURE.md`](./docs/ARCHITECTURE.md) — coordinator + daemon + workload modules
- [`docs/ROADMAP.md`](./docs/ROADMAP.md) — Phase 0 → 3 plan, milestones, capital requirements
- [`docs/INCENTIVES.md`](./docs/INCENTIVES.md) — provider payout tiers, unit economics
- [`docs/LEGAL.md`](./docs/LEGAL.md) — Provider ToS / DPA / AUP requirements, anti-abuse, liability shielding
- [`docs/MARKET.md`](./docs/MARKET.md) — competitive landscape, market sizing

---

## License

All source unpublished pending Phase 1. License TBD (likely AGPL-3.0 for daemon, proprietary for coordinator).

---

## Contact

Operations: emrahbaysal@gmail.com · hatiyildiz (GitHub)
