# Technical architecture

This document specifies the technology stack and component design for iogrid's three deliverables: the **provider daemon** (runs on every supply-side PC/Mac), the **coordinator** (server-side microservices), and the **management plane** (web app for providers and customers).

End-state architecture. We build modules in sequence toward this target — we do not iterate the architecture itself.

---

## Stack summary

| Component | Language | Runtime | Why |
|-----------|----------|---------|-----|
| **Provider daemon** | Rust (stable) | tokio async, single static binary | Smallest binary, lowest RAM/CPU footprint, no GC pauses, memory-safe. ~5MB statically linked, ~30MB peak RSS for the supervisor process. |
| **Coordinator microservices** | Go 1.25+ | grpc-go + connect-go, k8s-native | Matches OpenOva stack patterns, fastest iteration loop, excellent observability ecosystem. |
| **Management plane** | TypeScript 5.x + Next.js 15 | React Server Components + Edge runtime | Server-first rendering, SEO, instant hot reload. shadcn/ui on Radix primitives, Tailwind 4. |
| **Data plane** | Postgres 16 (CNPG) + Redis 7 + NATS JetStream | k8s | Postgres-per-service for strong isolation. Redis for hot session/routing state. NATS for cross-service events. |
| **Object storage** | S3-compatible (Hetzner Object Storage initially) | | Build artifacts, audit log archives. |
| **Observability** | OpenTelemetry + Grafana + Loki + Tempo | k8s | Existing OpenOva mothership stack — federated in. |
| **Service mesh** | Cilium (existing) | k8s | mTLS via SPIFFE-style identities, network policy isolation per microservice. |
| **CI/CD** | GitHub Actions → ghcr.io → Flux GitOps | k8s | SHA-pinned image deploys, Flux reconciles cluster state from `iogrid/iogrid-ops` manifests. |

---

## Provider daemon (Rust)

### Why Rust, not Go, on the edge

Go is excellent for the coordinator (microservices, observability ergonomics, fast iteration). On the provider's PC, Go's runtime overhead matters:

| Metric | Go static binary | Rust static binary |
|--------|------------------|--------------------|
| Cold-start RSS | ~12 MB | ~3 MB |
| Idle CPU (8h) | ~0.3 % | ~0.05 % |
| Binary size | ~18 MB stripped | ~5 MB stripped |
| GC pause | ~100µs spikes | none |
| Battery impact on laptop | noticeable on M1 Air | negligible |

For a daemon that should be invisible to a non-technical user, the Rust delta matters. Apple's Activity Monitor will show ~3 MB of memory — won't trigger any "this app is using too much memory" warning.

### Crate workspace structure

```
daemon/
├── Cargo.toml          [workspace]
├── crates/
│   ├── core/           supervisor process, IPC, state machine
│   ├── transport/      gRPC client + bidirectional streaming
│   ├── routing/        WireGuard tunnel + SOCKS5/HTTP CONNECT relay
│   ├── workload-docker/ Docker workload runner (via bollard)
│   ├── workload-gpu/   GPU workload runner (CUDA + MLX bindings)
│   ├── workload-ios/   macOS Tart VM driver (objc2 + Virtualization.framework)
│   ├── anti-abuse/     local pre-flight filters (the SAME filters that run server-side, mirrored locally so provider can audit)
│   ├── scheduler/      cap + calendar + idle-detect logic (combined)
│   ├── ui-bridge/      localhost HTTP server for the management plane to talk to the daemon
│   └── platform-{mac,linux,windows}/ OS-specific bits (idle detection, install location, service registration)
└── installer/          per-platform installer scripts + signing manifests
```

### Async runtime: tokio

- Single-threaded scheduler by default (it's a daemon, not a server)
- Switches to multi-threaded only when iOS-build or GPU workload is active
- All I/O is non-blocking, no thread-per-task overhead

### Inter-process communication

Daemon exposes a localhost-only HTTP+SSE API on `127.0.0.1:7777` (dynamic port if taken). The web management plane (running locally or remote) connects here to:
- Read current state (workload activity, earnings, schedule status)
- Mutate configuration (caps, calendar, opt-ins)
- Stream real-time events (every byte categorized, every container started)

mTLS between daemon and management plane uses a one-time-displayed pairing code on first connection.

### Workload execution security

| Workload | Isolation primitive |
|----------|---------------------|
| Bandwidth | WireGuard tunnel; daemon never decrypts customer's HTTPS payload |
| Docker | Linux: gVisor or Kata Containers for kernel-level isolation. Windows: Hyper-V isolated containers. Mac: Docker Desktop's lightweight VM |
| GPU | Same as Docker but `--gpus`/MLX scoped; vRAM limited per workload |
| iOS build | Tart-spawned ephemeral macOS VM, hypervisor isolation, destroyed at end of build |

Anti-abuse pre-flight filters (CSAM hash, PhishTank lookup) run locally in the daemon BEFORE traffic is relayed. We mirror the server's filter so the provider can verify their daemon is filtering — provider can dump the daemon's filter rules from the local UI bridge.

### Scheduling state machine

Three independent signals combine via AND logic:

```rust
pub enum SchedulerState {
    Active,        // workload requests are accepted
    Paused(reason),
}

fn current_state(cfg: &Config, now: DateTime, idle_secs: u64) -> SchedulerState {
    // Caps check
    if cfg.bandwidth_used_gb > cfg.bandwidth_cap_gb {
        return Paused(BandwidthCapReached);
    }
    if current_cpu_pct() > cfg.cpu_cap_pct {
        return Paused(CpuCapReached);
    }
    // Calendar window check
    if !cfg.calendar.is_active_at(now) {
        return Paused(OutsideCalendarWindow);
    }
    // Idle detection check
    if cfg.idle_only && idle_secs < cfg.idle_threshold_secs {
        return Paused(UserActive);
    }
    Active
}
```

Provider configures any combination. Defaults for first-time setup:
- Bandwidth cap: 50 GB/month
- CPU cap: 30%
- Calendar: always active (no time restriction)
- Idle-only: ON (only activates when user inactive for 5 minutes)

This default makes grandma's PC contribute only when she's not using it.

---

## Coordinator (Go microservices)

### Bounded contexts → microservices

Each microservice is a separate Go module, deployed as separate k8s Deployments. Communication via gRPC (internal) + NATS JetStream events (cross-context).

| Service | Bounded context | Responsibilities |
|---------|----------------|------------------|
| **identity-svc** | Identity | Google OAuth, magic-link issuance via Stalwart SMTP, identity merging on verified-email match, JWT issuance |
| **providers-svc** | Providers | Registration, capability inventory, scheduling state, opt-ins, transparency dashboard backend, audit log |
| **workloads-svc** | Workloads | Customer workload submission, scheduling, dispatch, retry/failover, result delivery |
| **antiabuse-svc** | Anti-abuse | Pre-flight filtering (CSAM, fraud, port restrictions, rate limits), abuse detection, customer flagging |
| **billing-svc** | Billing | Customer subscriptions (Stripe), provider payouts (Stripe Connect), metering aggregation, invoice generation |
| **telemetry-svc** | Observability | Metric collection, log/trace ingestion, alerting rules |
| **gateway-bff** | Web BFF | Backend-for-Frontend for the management plane: aggregates calls across services, handles real-time SSE/WebSocket streams to web clients |
| **proxy-gateway** | Customer-facing proxy | SOCKS5/HTTP CONNECT entry on `proxy.iogrid.org:443`, TLS termination, dispatches to providers via providers-svc |
| **build-gateway** | Customer-facing iOS-CI | Receives build jobs, schedules to Mac providers, manages S3 artifact bucket |

### Why separate services rather than monolith

- **Independent scaling:** proxy-gateway sustains 10K+ RPS, billing-svc sustains 10/min — different replica counts and resource shapes
- **Independent deploys:** anti-abuse rule updates roll out without touching billing
- **Fault isolation:** telemetry-svc outage doesn't kill the proxy data plane
- **Team scaling:** services can be owned by independent teams as the org grows

### Transport

- Service-to-service: **gRPC over mTLS** via Cilium service-mesh policies. Each service has a SPIFFE-style identity (`spiffe://iogrid/ns/iogrid/sa/billing-svc`). Identity-aware policy + SPIRE-backed mutual auth is implemented as `CiliumNetworkPolicy` per service with `authentication.mode: required` — see [SECURITY-mTLS](./SECURITY-mTLS.md) (issue #35).
- Service-to-daemon: **Connect-Go** (gRPC-compatible, HTTP/2 with JSON fallback for easier debugging)
- Daemon-to-coordinator: persistent bi-directional gRPC stream over mTLS (re-establish on disconnect with exponential backoff, max 60s)
- Inter-service async: **NATS JetStream** for events (provider-came-online, workload-completed, abuse-flag-raised, payout-eligible)

### Database strategy

- **Postgres per service** (logical separation; physical = same CNPG cluster for now). No cross-service joins; cross-service reads via gRPC API. Outbox pattern for transactional event emission.
- **Redis Cluster** for hot routing state (active sessions, provider capability snapshots, rate-limit counters)
- **Object storage** (S3-compatible) for build artifacts, audit log archives, large transient blobs

### Buf for protobuf management

```
proto/
├── buf.yaml
├── buf.gen.yaml
├── iogrid/
│   ├── identity/v1/{identity.proto, auth.proto}
│   ├── providers/v1/{registration.proto, scheduling.proto, dashboard.proto}
│   ├── workloads/v1/{submit.proto, dispatch.proto}
│   ├── antiabuse/v1/filters.proto
│   ├── billing/v1/{subscription.proto, payout.proto}
│   └── common/v1/{types.proto, errors.proto}
```

Buf lints + breaking-change detection runs in CI. Generated Go bindings to `coordinator/internal/pb/`, TypeScript bindings to `web/lib/pb/`, Rust bindings to `daemon/crates/transport/src/pb/`.

---

## Management plane (Next.js)

### Audience split

The web management plane serves **two distinct user types** through a single Next.js app, route-segmented:

| Route prefix | Audience | Surface |
|--------------|----------|---------|
| `/provide/*` | Providers | Earnings dashboard, schedule editor, opt-in categories, transparency feed, payout settings |
| `/customer/*` | B2B customers | API keys, usage metrics, billing, audit logs, support |
| `/account/*` | Both | Identity management (linked emails / OAuth providers), preferences |
| `/vpn/*` | Consumer VPN | Download links, account upgrade, server selection (for paid Plus/Pro tiers) |
| `/admin/*` | iogrid staff | Abuse review, customer KYC, financial ops |

Single sign-in flow (Google OAuth or magic-link) → user lands on context-appropriate dashboard based on their primary role. Role-switching available in nav (a provider who is also a customer can toggle).

### Stack

- **Next.js 15** with App Router, React Server Components by default
- **TypeScript 5.x** strict mode
- **shadcn/ui** (Radix primitives + Tailwind utilities), customized to iogrid design tokens
- **Tailwind 4** with CSS variables for theming
- **TanStack Query** for client-side data fetching where needed (real-time updates)
- **Zustand** for client state (sparingly)
- **React Hook Form + Zod** for form validation
- **Server-Sent Events (SSE)** for real-time transparency dashboard updates
- **WebSocket** for the in-product chat support widget
- **Playwright** for E2E tests
- **Vitest** for unit tests
- **Storybook** for component library development

### Data fetching pattern

- **Server Components** for initial page render (SEO + fast first paint)
- **Server Actions** for mutations (no separate API route boilerplate)
- **TanStack Query** only for real-time-updating widgets (transparency feed, earnings counter)
- All data flows through `gateway-bff` — the Next.js app never talks directly to a microservice

### Real-time transparency feed

The killer differentiator: providers see what's happening through their IP in real-time.

```
[ Right now ]                                              Updated 0.3s ago
┌──────────────────────────────────────────────────────────────┐
│ 📊 e-commerce price monitoring  →  amazon.com               │
│ ⏱ 12s ago · 4.2 MB · MyShopMonitor (customer)               │
│                                                              │
│ 🔍 SEO rank check  →  google.com                            │
│ ⏱ 23s ago · 0.8 MB · SerpTracker (customer)                 │
│                                                              │
│ 🛡 ad verification  →  facebook.com                         │
│ ⏱ 47s ago · 1.1 MB · AdAuditPro (customer)                  │
└──────────────────────────────────────────────────────────────┘
[ Block this category ]  [ Block this customer ]  [ Block this destination ]
```

Backed by `providers-svc.AuditStream` gRPC → NATS JetStream consumer → SSE to browser.

### Accessibility

- WCAG 2.2 AA target from day 1 (not bolted on)
- Keyboard-only navigation tested
- Screen-reader semantics via Radix primitives
- High-contrast + reduced-motion preference support
- All forms have explicit labels, error messages tied to fields

### Internationalization

- Next.js native i18n routing
- Languages day-1: English, Spanish, Portuguese, German, French, Italian, Turkish
- Right-to-left support (Arabic, Hebrew) infrastructure even if translations land later

---

## Authentication & identity model

### Identity primitives

```
User (canonical, immutable)
  └── Identifier (binding, can have many per user)
       ├── kind: google | magic-link | apple | github
       ├── verified_email: string (when applicable)
       └── created_at, last_used_at
```

Auth NEVER stores passwords. Two paths only:

1. **Google OAuth** — standard authorization-code flow. We pull `email`, `email_verified`, `name`, `picture`, `sub`. Additionally, Google's `id_token` may include `hd` (hosted domain — for Google Workspace users) which feeds enterprise routing.

2. **Magic link** — user types email; we send a single-use, 10-minute, signed token via Stalwart SMTP. They click → authenticated. Same as Notion, Vercel, Linear.

### Account merging — auto when safe

When a user authenticates via path B (e.g., magic-link to `john@company.com`), the identity-svc checks:

```
on authenticated event:
  found_identifier = look up by current identity (e.g., google:sub-12345)
  if found_identifier:
    proceed as existing user
  else:
    check_for_merge_candidate:
      for each existing user:
        if user has identifier of kind=google AND that Google account
           lists current_email as a VERIFIED email in its verified_emails list:
          AUTO-MERGE — add the new identifier to existing user, no prompt
        if user has identifier of kind=magic-link with email = current_email:
          require explicit confirmation via second magic-link to other email
    if no merge:
      create new user
```

The Google verified-emails trick: Google APIs expose all emails verified on a Google account (primary + secondary). If `john@gmail.com` (Google) has `john@company.com` as a verified secondary, a magic-link from `john@company.com` auto-merges into the Google account. **Same human, no friction.**

### JWT issuance

- Short-lived access token: 15 minutes, RS256-signed, includes user ID + active roles
- Refresh token: 30 days, opaque, stored server-side (rotation on use)
- Session revocation: users can revoke any session from `/account/sessions`

### Privileged operations require step-up auth

Payout changes, identity merging, account deletion → require fresh auth within last 5 minutes. Initiates an additional magic-link or OAuth re-auth.

### B2B customer authentication

Same identity primitives. Workspace concept layered on top:

```
User ← member-of → Workspace ← owns → APIKey, ResourceSpend, Settings
```

A workspace can have multiple users with role-based permissions (owner / admin / billing-only / read-only).

---

## Installation UX — grandma-proof

### Mac install

```bash
# Single command, no dependencies pre-installed expected
curl -fsSL https://iogrid.org/install/mac | sh
```

What this runs:
1. Detects macOS version, Apple Silicon vs Intel
2. Checks if Docker Desktop is installed; if not, downloads + auto-installs the signed .dmg
3. Checks if Tart is installed (only if user opts into iOS-build later); if not, `brew install cirruslabs/cli/tart` (installs Homebrew first if needed)
4. Downloads signed daemon binary (Sparkle-style auto-update on subsequent runs)
5. Installs daemon as a LaunchAgent (auto-start on login, runs as user not root)
6. Opens browser to `https://app.iogrid.org/onboard/<token>` with a one-time pairing token
7. User signs in with Google → daemon pairs → user sees first dashboard

Time from `curl` to "your PC is earning": ~2 minutes (mostly Docker Desktop download).

### Windows install

```powershell
iwr -useb https://iogrid.org/install/win | iex
```

1. Detects Windows version, x64 vs ARM64
2. Installs Docker Desktop (with WSL2 backend) if missing
3. Installs signed daemon as a Windows Service
4. Opens browser to onboarding flow

### Linux install

```bash
curl -fsSL https://iogrid.org/install/linux | sudo sh
```

1. Detects distro (apt / dnf / pacman / apk)
2. Installs Docker if missing (uses distro's official Docker package)
3. Installs daemon as systemd user service
4. Prints URL for browser onboarding (since headless servers can't auto-open)

### What grandma sees

She doesn't run `curl`. She visits **iogrid.org**, clicks "Install for Mac" / "Install for Windows" — a **signed installer** (.dmg / .msi / .deb / .rpm) downloads. Double-click. Click "Continue" 3 times. Browser opens. Sign in with Google. Done.

The `curl | sh` is for developers / power users who prefer terminal. Same end state.

### What gets installed where

| Component | Mac | Linux | Windows |
|-----------|-----|-------|---------|
| Daemon binary | `/usr/local/iogrid/iogridd` | `/usr/local/bin/iogridd` | `C:\Program Files\iogrid\iogridd.exe` |
| Service registration | LaunchAgent `~/Library/LaunchAgents/org.iogrid.plist` | systemd user unit `~/.config/systemd/user/iogridd.service` | Windows Service `iogridd` |
| Config | `~/Library/Application Support/iogrid/config.toml` | `~/.config/iogrid/config.toml` | `%APPDATA%\iogrid\config.toml` |
| Logs | `~/Library/Logs/iogrid/*.log` | `~/.local/share/iogrid/logs/*.log` | `%LOCALAPPDATA%\iogrid\logs\*.log` |
| Tart VMs (Mac only, if iOS-build enabled) | `~/.tart/vms/` | — | — |
| Docker containers | (managed by Docker Desktop / Docker Engine) | | |

### Uninstall

`iogridd uninstall` — single command, removes service registration, daemon binary, config, logs. Bandwidth/compute data is purged from the server-side after a 7-day grace period.

---

## Scheduling — combined cap + calendar + idle

(Implementation detail; UX surfaced in management plane)

### Defaults out-of-the-box (first install)

| Setting | Default | Why |
|---------|---------|-----|
| Bandwidth cap | 50 GB/month | Conservative for non-power-user; ~80% of US home plans have >1 TB total bandwidth, this uses 5% |
| CPU cap | 30% | Allows user's other apps to feel responsive even under load |
| Memory cap | 25% of system RAM | Same logic |
| GPU cap | 100% (when system idle) / 0% (when user active) | GPU workloads benefit most from full power |
| Calendar | Always-on (no restriction) | Sensible default; users with thermal concerns can restrict to nights |
| Idle-only mode | ON, 5-minute threshold | Activates only when user is away. Zero perceived performance impact. |
| Categories allowed | E-commerce, SEO, Ad-verification, AI-training-data, Iogrid-internal | Excludes: Lead-gen scraping (LinkedIn-ish), Social-media intelligence — provider can opt in if they want |
| Destination blocklist | (Empty by default) | Provider can add their employer's domain, banking domains, etc. |

### Power-user customization

The management plane's `/provide/schedule` page exposes everything:

```
┌─────────────────────────────────────────────────────────────┐
│ Resource caps                                               │
│   Bandwidth:   [50] GB/month   [████████░░░░] 32/50 used    │
│   CPU:         [30] % cap       Currently using: 8%         │
│   Memory:      [25] % cap                                   │
│   GPU:         [100] % when idle                            │
│                                                             │
│ Active hours                                                │
│   ◉ Always active (with caps above)                         │
│   ○ Only during these windows:                              │
│       [+ Add window]                                        │
│                                                             │
│ Idle detection                                              │
│   ✓ Only activate when I'm away from my computer            │
│     [5] minutes of inactivity                               │
│                                                             │
│ Categories I allow                                          │
│   ✓ E-commerce price monitoring     [view 247 customers]    │
│   ✓ SEO rank tracking                [view 89 customers]    │
│   ✓ Ad verification                  [view 41 customers]    │
│   ✓ AI training data collection      [view 12 customers]    │
│   ○ Lead generation                  [LinkedIn, Indeed, ...] │
│   ○ Social media intelligence        [Twitter, IG, TikTok]  │
│   ○ Adult content scraping           [requires confirm]     │
│                                                             │
│ Destination blocklist                                       │
│   [+ Add domain pattern]                                    │
│                                                             │
│ Per-destination rate limit                                  │
│   No single customer uses my IP for more than               │
│   [10] minutes consecutively (auto-rotate after)            │
└─────────────────────────────────────────────────────────────┘
```

---

## Observability

### Metrics

- All services emit OpenTelemetry metrics with consistent labels: `service`, `customer_id`, `provider_id`, `workload_type`, `region`
- Pushed to existing OpenOva mothership Mimir / VictoriaMetrics
- Dashboards in Grafana: per-service health, per-customer usage, per-provider earnings, anti-abuse hit rates

### Logging

- Structured JSON via slog (Go) / tracing (Rust)
- Shipped to Loki
- Sensitive fields (customer URLs, provider IPs) hashed at log boundary; raw values accessible only to authorized humans via audit-grant flow

### Tracing

- W3C Trace Context propagation across services and from daemon
- Tempo backing store, sampled at 1% in production, 100% for flagged-abuse requests

### SLOs (Phase 2 commitments)

- Proxy-gateway availability: 99.9% / month
- Workload dispatch latency p95: 100ms
- Web management plane TTI: <2 seconds on cold cache, <500ms warm
- Magic-link delivery: 95% < 30 seconds

### Alerting

- SLO burn rate > 2× → page on-call
- Anti-abuse hit rate spike → notify abuse team
- Anomalous provider behavior (sudden bandwidth spike, IP reputation drop) → automatic temp-suspend + human review

Routing is defined in `infra/k8s/base/telemetry-svc/alertmanager-config.yaml`. Three receiver tiers:

- `iogrid-page` — PagerDuty + `#iogrid-incidents` Slack (severity=page, e.g. 14× burn rate, synthetic-probe DOWN)
- `iogrid-warn` — `#iogrid-warnings` Slack only (severity=warn, e.g. 2× burn rate, capacity warnings)
- `iogrid-abuse` — `#iogrid-abuse-team` Slack (category=abuse, separate moderation team)

Inhibition rules suppress lower-severity alerts when a page-tier alert is already firing for the same service.

### Public status page

**`status.iogrid.org`** is the customer-facing rollup of the above signals. It's served as a static export from `marketing/app/status/` (Next.js, Tailwind) and consumes three public, world-readable JSON endpoints on `telemetry-svc`:

| Endpoint | Purpose |
|----------|---------|
| `GET /status/posture` | One JSON envelope — `overall: up\|degraded\|down`, per-service grid, active incidents, last-7-days history |
| `GET /status/uptime?service=<name>&days=90` | Per-service 90-day uptime ledger that feeds the calendar heatmap |
| `POST /status/subscribe` | Email subscription registry (rate-limited per-IP) |

Operator-curated incidents are managed via admin-token-gated mutations: `POST /status/incidents`, `POST /status/incidents/{id}/updates`. Incidents follow the StatusPage.io lifecycle (`investigating → identified → monitoring → resolved`) with four impact tiers (`none | minor | major | critical`). The runbook lives at [`docs/RUNBOOK_STATUS.md`](RUNBOOK_STATUS.md).

Storage is Postgres-backed (`incidents`, `incident_updates`, `status_subscriptions`, `uptime_samples` tables in the telemetry-svc CNPG database) with an in-memory fallback so the public `/status/*` endpoints stay responsive even when the DB is unavailable — a status page that itself looks broken is the worst possible UX during a real outage.

The page polls `/status/posture` every 60 seconds and degrades gracefully:

- If `/status/posture` is unreachable, the page shows a "stale data" pill and keeps rendering the last good payload.
- If telemetry-svc has been unreachable since first load, the page falls back to a baseline frame from `marketing/content/status/incidents-static.json` — operators edit that file as a backstop during a status-svc outage.

### Grafana dashboard provisioning

iogrid does NOT run its own Grafana. Dashboards live as JSON under `coordinator/services/telemetry-svc/dashboards/`, are copied into `infra/k8s/base/telemetry-svc/assets/dashboards/` (single source of truth, kustomize load-restrictor workaround), and ship as a single ConfigMap with `grafana_dashboard: "1"` label. The mothership Grafana's `kiwigrid/k8s-sidecar` auto-discovers it and provisions dashboards into the "iogrid" folder. Six dashboards in total:

- `iogrid-overview` — global RPS / error rate / P99
- `iogrid-services` — per-service SLI rollups
- `iogrid-customers` — per-customer usage panels
- `iogrid-providers` — per-provider earnings + workload mix
- `iogrid-abuse` — anti-abuse spike detector
- `iogrid-revenue` — GMV, payout queue, $GRID emission

The `infra/k8s/base/grafana/` directory ships the auxiliary "iogrid" Grafana Folder + the tenant-scoped Mimir datasource (`X-Scope-OrgID: iogrid` header) the dashboards reference. Dashboard edits must go through git — UI-side edits are reverted by the next sidecar reconcile.

---

## Federated deployment plan

### Initial (Phase 0–1)

- Single coordinator deployment on existing Contabo k8s mothership
- All microservices in `iogrid` namespace
- Cilium ClusterMesh not needed (single region)

### Mid (Phase 2)

- Migration to dedicated OpenOva instance (larger Contabo box or AWS/Hetzner cluster)
- All k8s manifests in `iogrid/iogrid-ops` repo, Flux-managed
- Zero-downtime migration via dual-coordinator phase (old + new accept traffic, providers reconnect to new, old decommissioned)

### Long-term (Phase 3)

- Multi-region coordinator (US-East, EU-West, APAC) for latency
- Cilium ClusterMesh across regions
- Eventually-consistent provider registry replicated cross-region (CRDT or similar)
- Customer chooses region for proxy traffic (some workloads — geo-targeting — explicitly route by destination region anyway)

---

## Bleeding-edge tech choices

These are explicit "we use the modern thing" decisions, not "we'll pick later":

- **Connect-Go** over raw gRPC for the customer-facing API — HTTP/2 compatible, JSON-fallback for debugging, single-binary code-gen
- **Buf** for protobuf management instead of `protoc` — lint + breaking-change detection + reproducible builds
- **NATS JetStream** for cross-service events instead of Kafka — operates on a 50MB pod (vs Kafka's 4GB JVM minimum)
- **Cilium** instead of kube-proxy — Linux eBPF kernel-bypass, lower latency, native L7 mTLS
- **SPIFFE / SPIRE-style** workload identities instead of Vault for service mTLS
- **Bun** alongside Node.js for build tooling on Next.js side — faster cold builds
- **Drizzle ORM** instead of Prisma — closer to SQL, no migration black box
- **Rust tonic + h2** for gRPC on daemon — single async runtime stack, no Python/Node interop
- **Tart** for macOS VM provisioning — open source from Cirrus Labs, performance-tuned for Apple Silicon
- **gVisor** or **Kata Containers** for stronger Docker isolation on Linux providers
- **Stripe Connect** + **Stripe Tax** for international tax compliance out of the box
- **PostHog** (self-hostable, OpenOva-deployable) for product analytics instead of GA4
- **Sentry** (self-hostable) for error tracking
- **Playwright** for E2E + **MSW** for API mocking instead of legacy Cypress

---

## What this enables for Phase 0 (immediate)

The architecture above IS the end state. Phase 0 implementation ships a subset:

- Daemon: `core` + `transport` + `routing` (bandwidth only) crates
- Coordinator: `identity-svc` + `providers-svc` + `workloads-svc` + `antiabuse-svc` + `proxy-gateway` + `gateway-bff`
- Web plane: `/account/*` + `/provide/*` (basic dashboard) only; `/customer/*` deferred until first paying customer
- One internal customer (vcard-api) consuming the proxy-gateway for LinkedIn enrichment

Subsequent modules (Docker workload, GPU workload, iOS-build, VPN gateway, customer-facing dashboard, billing) plug into the same architecture without redesign.
