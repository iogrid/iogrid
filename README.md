# iogrid

**Distributed compute + bandwidth mesh for the post-cloud era.**

iogrid is a peer-to-peer network where home PC and Mac owners share their idle compute and bandwidth with paying enterprise customers. Providers earn cash, free unlimited VPN, or charity contributions in return. Customers get residential-IP proxy, Docker compute, GPU inference, and macOS-native iOS builds at a fraction of cloud prices.

Mobile users (iOS / Android) are VPN consumers only — they never share resources, only consume them.

---

## What we offer

### To providers — home PC and Mac owners

A single one-command install turns your always-on home device into an earner. Pick how you get paid:

- **Cash** — Stripe Connect payouts, ~$10–50/month per device
- **Free unlimited VPN** for all your devices (iOS, Android, Mac, Windows, Linux) — mesh-swap economics
- **Charity donation** — bandwidth-share revenue is forwarded to EFF, Tor Project, Wikipedia, etc.

You see every byte flowing through your IP in real-time, categorized. You block any category, destination, or customer you don't want. You revoke consent and uninstall any time.

Mobile devices cannot be providers (iOS / Android platform policy prohibits background bandwidth-sharing).

### To customers — businesses that need residential IPs or affordable compute

Four workload types, one API:

| Workload | Use cases | Price |
|----------|-----------|-------|
| **Bandwidth proxy** | E-commerce monitoring, SEO tracking, ad verification, social intelligence | $0.30–0.60 / GB |
| **Docker compute** | Batch processing, ML inference, internal jobs | $0.02–0.10 / CPU-hour |
| **GPU / AI inference** | LLM serving, vision models | $0.20–2.00 / GPU-hour |
| **iOS builds** | Xcode CI on real Mac hardware | **$0.04 / minute** (50% under GitHub Actions) |

Geo-targeting by country, session stickiness, real-time telemetry, audit logs.

### To consumers — iOS, Android, Mac, Windows, Linux users

Free unlimited VPN. No subscription required, no ads, no data sale. We make money from the B2B side, not from you.

- **Free tier** — 2 GB/month, 1 server location
- **Plus** — $2.99/month — unlimited bandwidth, 30 countries
- **Pro** — $4.99/month — Plus + ad/tracker blocking, kill switch

#### Quick start (Linux/macOS)

```bash
curl -fsSL https://iogrid.org/install-cli.sh | sh         # 1. install
# Mint a key at https://iogrid.org/customer/vpn, then:
iogrid login --api-key=iog_YOUR_KEY --customer-id=YOUR_ID  # 2. login
iogrid vpn regions                                         # 3. pick a region
iogrid vpn run --region us-east-1                          # 4. tunnel up (Ctrl-C to exit)
# in another shell:
curl ifconfig.me                                           # 5. verify exit IP changed
```

Windows users grab the `.msi` from [releases.iogrid.org](https://releases.iogrid.org). Mobile users install the
iogrid app from the App Store / Play Store.

`iogrid vpn doctor` runs a connectivity self-check; paste the output to a GitHub issue
if anything fails. The control plane is at `https://api.iogrid.org/v1/vpn/*`; the WG
data plane is direct peer-to-peer between your machine and an opted-in residential
provider — iogrid never sees your decrypted traffic.

If a step fails, [docs/runbooks/vpn/customer-onboarding.md](./docs/runbooks/vpn/customer-onboarding.md)
documents what each command does, the expected output, and the operator-side fix for
every failure mode. Operators standing up provider daemons should see
[docs/runbooks/vpn/operator-paired-daemon.md](./docs/runbooks/vpn/operator-paired-daemon.md).

---

## Why iogrid exists

Existing residential-proxy networks have a known opacity problem. Providers don't see what their IP serves. Bandwidth gets resold for purposes the provider would refuse if asked. The 2015 Hola scandal — free-VPN users turned into a botnet without their knowledge — remains the cautionary tale.

Existing distributed-compute networks focus narrowly on GPUs for gamers or crypto-native users. iOS-build CI in particular is wildly undersupplied — every iOS developer pays GitHub Actions ~$0.08/min for Mac runners because Apple's licensing locks builds to Mac hardware.

iogrid bundles both into one platform with three explicit differentiators:

1. **Radical transparency** — providers see every byte flowing through their IP, categorized, in real-time, in a dashboard. Block any category, destination, or customer with one click.
2. **Multi-currency incentives** — providers choose cash, free VPN, or charity. We don't lock you into a payment model.
3. **First-class iOS-build workload** — solving a real CI/CD pain point for the world's iOS developers, at half the going rate.

---

## Repository layout

```
iogrid/
├── daemon/         Rust workspace — provider-side binary, cross-compiled for
│                   darwin/{arm64,amd64}, linux/{amd64,arm64}, windows/amd64.
│                   Single static binary, ~5 MB, ~3 MB RSS at idle.
├── coordinator/    Go microservices — k8s-native control plane.
│                   identity-svc, providers-svc, workloads-svc, antiabuse-svc,
│                   billing-svc, telemetry-svc, gateway-bff, proxy-gateway,
│                   build-gateway.
├── web/            Next.js 15 management plane — providers + customers + VPN.
│                   TypeScript, shadcn/ui, Tailwind 4, real-time SSE.
├── proto/          Buf-managed protobuf schemas for all services.
├── infra/k8s/      Flux-managed k8s manifests (deployed on OpenOva ecosystem).
└── docs/           Architecture, roadmap, tech specs, incentive model, legal.
```

---

## Documentation

### 📐 Canon (read in this order)

- [ARCHITECTURE](./docs/ARCHITECTURE.md) — how iogrid works: system overview, components, scheduling, transparency layer, DNS + TLS, install UX, observability
- [BUSINESS-STRATEGY](./docs/BUSINESS-STRATEGY.md) — market, competitive landscape, unit economics, currency model ($GRID), multi-tenant fiat-rail relationship with Sociable Cash, off-ramp partners, legal risk
- [SECURITY](./docs/SECURITY.md) — threat model, trust boundaries, service mTLS, secrets policy, identity, supply chain
- [ROADMAP](./docs/ROADMAP.md) — module shipping sequence toward the end state
- [Whitepaper](./docs/whitepaper.md) — $GRID token whitepaper (regulatory artifact, PDF-exported by CI for counsel review)

### 🔧 Build + operate

- [RUNBOOKS](./docs/RUNBOOKS.md) — operator how-tos (Solana devnet bootstrap, status-page incident handling)

### 🟢 Live state ([ledger/](./docs/ledger/))

- [Ledger overview](./docs/ledger/README.md)
- [TRUST](./docs/ledger/TRUST.md) — verification ledger (UNVERIFIED / VERIFIED-PASS / VERIFIED-FAIL / VERIFIED-PARTIAL per surface)
- [TRACKER](./docs/ledger/TRACKER.md) — open work + DoD progress board

### 🏛️ Decision records ([adr/](./docs/adr/))

- [ADR index](./docs/adr/README.md) (none yet — decisions still being captured in BUSINESS-STRATEGY / ARCHITECTURE)

### 💡 In-flight design ([proposals/](./docs/proposals/))

- [Proposal index](./docs/proposals/README.md) (none yet)

### 📚 Operator notes

- [Lessons learned](./docs/lessons-learned/README.md) — field notes (none yet)
- [Per-incident playbooks](./docs/runbooks/README.md) — date-stamped one-offs (none yet)
- [Sessions](./docs/sessions/README.md) — transient artifacts (auto-archive at 30 days)
- [Archive](./docs/archive/README.md) — frozen / historical (see contents below)
  - [Phase 0 setup](./docs/archive/2026-05-21-phase0-setup.md) — superseded bastion setup procedure
  - [Phase 0 unblock](./docs/archive/2026-05-21-phase0-unblock.md) — superseded Phase 0 unblock chain
  - [Phase 0 first-customer playbook](./docs/archive/2026-05-21-phase0-first-customer.md) — superseded vCard onboarding

### 🌐 Domain subdirs

- [Transparency reports](./docs/transparency/README.md) — quarterly public-facing transparency artifacts
  - [Template](./docs/transparency/TEMPLATE.md) — quarterly report skeleton
  - [2026-Q2](./docs/transparency/2026-Q2.md) — current quarter

---

## Engineering principles

- **End-state architecture, not iterative MVPs.** We design for the target and ship modules toward it. We do not redesign architecture between releases.
- **Bleeding-edge stack.** Rust on the edge (smallest binary, zero GC), Go microservices on k8s, Next.js 15 with React Server Components on the web plane. Connect-Go + Buf for service contracts. NATS JetStream for events. Cilium for service mesh.
- **Microservices with strict bounded contexts.** Each service owns one bounded context (identity, providers, workloads, anti-abuse, billing, telemetry). No cross-service joins.
- **Quality is non-negotiable.** WCAG 2.2 AA from day 1. SLOs defined before launch. SOC 2 Type II roadmap.
- **Strict ticketing discipline.** All work tracked as GitHub Issues on this org. Kanban: `status/in-progress` → `status/uat` → `status/completed`. No untracked work.

---

## Status

End-state architecture documented. Building toward it. See [`docs/ROADMAP.md`](./docs/ROADMAP.md) for the module ship sequence and current focus.

---

## License

All source unpublished pending public launch. License TBD (likely AGPL-3.0 for daemon, proprietary for coordinator).
