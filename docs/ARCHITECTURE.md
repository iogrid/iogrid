# Architecture

> **WHAT:** Canonical "how iogrid works" — system overview, component design, scheduling, transparency layer, security, DNS + TLS, install UX, observability.
> **AUTHORITY:** Canon. Supersedes (now-removed) `TECH.md`, `DNS_TLS.md`.
> **POINTER:** Business-strategy context (market, pricing, $GRID, legal) lives in [`BUSINESS-STRATEGY.md`](./BUSINESS-STRATEGY.md). Module-ship sequence in [`ROADMAP.md`](./ROADMAP.md). Per-component design notes co-locate with code (e.g. `coordinator/services/<name>/README.md`).

End-state architecture. We build modules in sequence toward this target — we do not iterate the architecture itself.

---

## 1. System overview

### 1.1 One platform, four workload types

iogrid is a single coordinated platform with pluggable workload modules. The same provider daemon, coordinator, auth, billing, and transparency layer serves all four workload types — what changes is the workload-specific execution module.

```
                       ┌──────────────────────────────────────────────────┐
                       │              Customer-facing API                  │
                       │  REST + gRPC, OpenAPI spec, per-key billing       │
                       └──────────────────┬────────────────────────────────┘
                                          │
                       ┌──────────────────▼────────────────────────────────┐
                       │              Workload Scheduler                   │
                       │  Match requests → eligible providers by:          │
                       │   - capability (proxy / docker / gpu / ios-build) │
                       │   - geo (country / region)                        │
                       │   - quality score (uptime, latency, reputation)   │
                       │   - provider opt-ins (category, destination)      │
                       │   - load (current bandwidth / cpu utilization)    │
                       └──────────────────┬────────────────────────────────┘
                                          │
            ┌────────────────────────────┬┴──────────────────────┬─────────────────────────┐
            │                            │                       │                         │
   ┌────────▼─────────┐    ┌────────────▼────────┐  ┌────────────▼─────────┐   ┌──────────▼──────────┐
   │ Bandwidth router │    │ Docker workload    │  │ GPU workload         │   │ iOS-build workload  │
   │ (SOCKS5 + WG)    │    │ scheduler (k8s-    │  │ (CUDA / MLX,         │   │ (Tart macOS VMs +   │
   │                  │    │  lite, no etcd)    │  │  per-provider)       │   │  Xcode env images)  │
   └────────┬─────────┘    └────────────┬────────┘  └────────────┬─────────┘   └──────────┬──────────┘
            │                            │                       │                         │
            └────────────────────────────┴───────────────────────┴─────────────────────────┘
                                          │
                              ┌───────────▼──────────┐
                              │  Provider transport  │
                              │  gRPC over mTLS,     │
                              │  long-lived bidi     │
                              │  streams. WireGuard  │
                              │  tunnel for bandwidth│
                              │  workload only.      │
                              └───────────┬──────────┘
                                          │
              ┌───────────────────────────┴────────────────────────────┐
              │                                                        │
       ┌──────▼──────────────┐                              ┌──────────▼───────────┐
       │  Provider Daemon    │                              │  Provider Daemon     │
       │  (macOS / Apple Si) │                              │  (Linux x86_64)      │
       │                     │                              │                      │
       │  - WireGuard client │                              │  - WireGuard client  │
       │  - Tart (macOS VMs) │                              │  - Docker / Podman   │
       │  - Docker (Colima)  │                              │  - NVIDIA Container  │
       │  - MLX (GPU infer)  │                              │    Toolkit (if GPU)  │
       │  - Local control UI │                              │  - Local control UI  │
       └─────────────────────┘                              └──────────────────────┘
```

### 1.2 Stack summary

> Source: previously `docs/TECH.md` §"Stack summary" (merged here on 2026-05-20).

| Component | Language | Runtime | Why |
|-----------|----------|---------|-----|
| **Provider daemon** | Rust (stable) | tokio async, single static binary | Smallest binary, lowest RAM/CPU footprint, no GC pauses, memory-safe. ~5 MB statically linked, ~30 MB peak RSS for the supervisor process. |
| **Coordinator microservices** | Go 1.25+ | grpc-go + connect-go, k8s-native | Matches OpenOva stack patterns, fastest iteration loop, excellent observability ecosystem. |
| **Management plane** | TypeScript 5.x + Next.js 15 | React Server Components + Edge runtime | Server-first rendering, SEO, instant hot reload. shadcn/ui on Radix primitives, Tailwind 4. |
| **Data plane** | Postgres (CloudNativePG, the `iogrid-pg` cluster) + Redis + NATS JetStream | k8s | Postgres-per-service (logical) for isolation; continuous WAL archiving + PITR to an **in-cluster MinIO** today (offsite Hetzner is a follow-up). Redis for hot session/routing state. NATS for cross-service events. |
| **Object storage** | S3-compatible (in-cluster MinIO; Hetzner Object Storage as a later offsite target) | | Build artifacts, audit log archives, Postgres backups. |
| **Observability** | OpenTelemetry + Grafana + Loki + Tempo | k8s | Existing OpenOva mothership stack — federated in. |
| **Service mesh** | Cilium (existing) | k8s | mTLS via SPIFFE-style identities, network policy isolation per microservice. |
| **CI/CD** | GitHub Actions → ghcr.io + harbor.openova.io (dual-push) → `scripts/reroll-iogrid-deployments.sh` (image-only roll) | k8s | SHA-pinned image deploys. ghcr.io is the public source of truth; `harbor.openova.io/iogrid` is the in-cluster mirror the cluster actually pulls from (bypasses per-package ACLs). The 6h `harbor-mirror-verify` cron catches silent dual-push regressions. **iogrid is NOT Flux-wired yet** — the reference Flux Kustomizations are suspended because the committed manifests drifted from working prod; the only safe live deploy is the image-only reroll script. An off-prod runtime-validation gate runs green in CI. See [`runbooks/2026-05-24-harbor-mirror-bypass.md`](./runbooks/2026-05-24-harbor-mirror-bypass.md). |

---

## 2. Provider daemon (Rust)

> Source: previously `docs/TECH.md` §"Provider daemon" + `docs/ARCHITECTURE.md` §"Daemon" (merged here on 2026-05-20).

Installed on provider devices. One binary, OS-conditional behaviour.

### 2.1 Why Rust, not Go, on the edge

Go is excellent for the coordinator (microservices, observability ergonomics, fast iteration). On the provider's PC, Go's runtime overhead matters:

| Metric | Go static binary | Rust static binary |
|--------|------------------|--------------------|
| Cold-start RSS | ~12 MB | ~3 MB |
| Idle CPU (8h) | ~0.3 % | ~0.05 % |
| Binary size | ~18 MB stripped | ~5 MB stripped |
| GC pause | ~100µs spikes | none |
| Battery impact on laptop | noticeable on M1 Air | negligible |

For a daemon that should be invisible to a non-technical user, the Rust delta matters. Apple's Activity Monitor shows ~3 MB of memory — won't trigger any "this app is using too much memory" warning.

### 2.2 Targets enabled by default

| Target triple | Workload types enabled by default |
|---------------|-----------------------------------|
| `darwin/arm64` (Apple Silicon Mac) | bandwidth, docker (via Colima), gpu (MLX), ios-build (Tart) |
| `darwin/amd64` (Intel Mac) | bandwidth, docker, ios-build (Tart Intel mode) |
| `linux/amd64` | bandwidth, docker, gpu (CUDA if NVIDIA present) |
| `linux/arm64` | bandwidth, docker (limited container image availability) |
| `windows/amd64` | bandwidth, docker (Docker Desktop / WSL2 backend) |

### 2.3 Crate workspace structure

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
│   ├── anti-abuse/     local pre-flight filters (mirror of server-side filters; provider can audit locally)
│   ├── scheduler/      cap + calendar + idle-detect logic (combined)
│   ├── ui-bridge/      localhost HTTP server for the management plane to talk to the daemon
│   └── platform-{mac,linux,windows}/ OS-specific bits (idle detection, install location, service registration)
└── installer/          per-platform installer scripts + signing manifests
```

### 2.4 Async runtime: tokio

- Single-threaded scheduler by default (it's a daemon, not a server).
- Switches to multi-threaded only when iOS-build or GPU workload is active.
- All I/O is non-blocking, no thread-per-task overhead.

### 2.5 Inter-process communication

Daemon exposes a localhost-only HTTP+SSE API on `127.0.0.1:7777` (dynamic port if taken). The web management plane (running locally or remote) connects here to:

- Read current state (workload activity, earnings, schedule status).
- Mutate configuration (caps, calendar, opt-ins).
- Stream real-time events (every byte categorised, every container started).

mTLS between daemon and management plane uses a one-time-displayed pairing code on first connection.

### 2.6 Provider controls

Via the local CLI or simple web UI on `localhost:7777`:

- Enable/disable individual workload types.
- Set bandwidth cap (e.g. max 50 GB/month).
- Set compute caps (max CPU%, max RAM, max GPU memory).
- Block specific destinations (regex), block specific categories.
- Choose payout currency (cash / VPN credits / $GRID / charity).
- View real-time and historical activity.

Daemon establishes a single long-lived gRPC stream to the coordinator. WireGuard tunnel for bandwidth workload is layered on top (no separate connection). Heartbeats every 15 s. Auto-reconnect with exponential backoff.

### 2.7 Workload execution security

| Workload | Isolation primitive |
|----------|---------------------|
| Bandwidth | WireGuard tunnel; daemon never decrypts customer's HTTPS payload |
| Docker | Linux: gVisor or Kata Containers for kernel-level isolation. Windows: Hyper-V isolated containers. Mac: Docker Desktop's lightweight VM |
| GPU | Same as Docker but `--gpus`/MLX scoped; vRAM limited per workload |
| iOS build | Tart-spawned ephemeral macOS VM, hypervisor isolation, destroyed at end of build |

Anti-abuse pre-flight filters (CSAM hash, PhishTank lookup) run locally in the daemon BEFORE traffic is relayed. We mirror the server's filter so the provider can verify their daemon is filtering — provider can dump the daemon's filter rules from the local UI bridge.

---

## 3. Coordinator (Go microservices)

> Source: previously `docs/TECH.md` §"Coordinator" + `docs/ARCHITECTURE.md` §"Coordinator" (merged here on 2026-05-20).

The control plane. Microservices deployed as separate k8s Deployments on Kubernetes (Phase 0 on the OpenOva mothership; Phase 2 federation possible).

### 3.1 Bounded contexts → microservices

Each microservice is a separate Go module. Communication via gRPC (internal) + NATS JetStream events (cross-context).

| Service | Bounded context | Responsibilities |
|---------|----------------|------------------|
| **identity-svc** | Identity | Google OAuth, magic-link issuance via Stalwart SMTP, identity merging on verified-email match, JWT issuance |
| **providers-svc** | Providers | Registration, capability inventory, scheduling state, opt-ins, transparency dashboard backend, audit log |
| **workloads-svc** | Workloads | Customer workload submission, scheduling, dispatch, retry/failover, result delivery |
| **antiabuse-svc** | Anti-abuse | Pre-flight filtering (CSAM, fraud, port restrictions, rate limits), abuse detection, customer flagging |
| **billing-svc** | Billing | Customer subscriptions (Stripe), provider payouts (Stripe Connect / $GRID), metering aggregation, invoice generation |
| **telemetry-svc** | Observability | Metric collection, log/trace ingestion, alerting rules |
| **gateway-bff** | Web BFF | Backend-for-Frontend for the management plane: aggregates calls across services, handles real-time SSE/WebSocket streams to web clients |
| **proxy-gateway** | Customer-facing proxy | SOCKS5/HTTP CONNECT entry on `proxy.iogrid.org:443`, TLS termination, dispatches to providers via providers-svc |
| **build-gateway** | Customer-facing iOS-CI | Receives build jobs, schedules to Mac providers, manages S3 artifact bucket |
| **vpn-svc** | VPN sessions | Consumer-VPN session bring-up (incl. `POST /v1/vpn/sessions/mobile`), per-session inner-IP allocation, quota/entitlement |
| **vpn-gateway** | Customer-facing VPN | VPN data-plane ingress; consumes from the same opted-in residential provider pool |

### 3.2 Why separate services rather than monolith

- **Independent scaling:** proxy-gateway sustains 10K+ RPS; billing-svc sustains 10/min — different replica counts and resource shapes.
- **Independent deploys:** anti-abuse rule updates roll out without touching billing.
- **Fault isolation:** telemetry-svc outage doesn't kill the proxy data plane.
- **Team scaling:** services can be owned by independent teams as the org grows.

### 3.3 Transport

- **Service-to-service:** gRPC over mTLS via Cilium service-mesh policies. Each service has a SPIFFE-style identity (`spiffe://iogrid/ns/iogrid/sa/billing-svc`). Identity-aware policy + SPIRE-backed mutual auth implemented as `CiliumNetworkPolicy` per service with `authentication.mode: required` — see §5.4 below.
- **Service-to-daemon:** Connect-Go (gRPC-compatible, HTTP/2 with JSON fallback for easier debugging).
- **Daemon-to-coordinator:** persistent bi-directional gRPC stream over mTLS (re-establish on disconnect with exponential backoff, max 60 s).
- **Inter-service async:** NATS JetStream for events (provider-came-online, workload-completed, abuse-flag-raised, payout-eligible).

### 3.4 Database strategy

- **Postgres per service** — logical separation; physically shares the same CNPG cluster in Phase 0/1 (graduates to per-service Postgres clusters in Phase 2). No cross-service joins; cross-service reads via gRPC API. Outbox pattern for transactional event emission.
- **Redis Cluster** for hot routing state (active sessions, provider capability snapshots, rate-limit counters).
- **Object storage** (S3-compatible) for build artefacts, audit log archives, large transient blobs.

### 3.5 Proto management (Buf)

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

Buf lints + breaking-change detection runs in CI. Generated Go bindings to `coordinator/internal/pb/`, TypeScript bindings to `web/src/lib/pb/`, Rust bindings to `daemon/crates/transport/src/pb/`.

### 3.6 Federated deployment plan

- **Initial (Phase 0–1):** single coordinator deployment on existing mothership k8s. All microservices in `iogrid` namespace. Cilium ClusterMesh not needed (single region). K8s manifests live in this repo under `infra/k8s/`; they are **not Flux-reconciled yet** (reference Kustomizations suspended), so deploys roll via `scripts/reroll-iogrid-deployments.sh` (image-only) until the manifests are reconciled back to working prod.
- **Mid (Phase 2):** migration to dedicated OpenOva instance (larger Contabo box or AWS/Hetzner cluster), at which point the manifests are wired into Flux GitOps. Zero-downtime migration via dual-coordinator phase (old + new accept traffic, providers reconnect to new, old decommissioned).
- **Long-term (Phase 3):** multi-region coordinator (US-East, EU-West, APAC) for latency. Cilium ClusterMesh across regions. Eventually-consistent provider registry replicated cross-region (CRDT or similar). Customer chooses region for proxy traffic (some workloads — geo-targeting — explicitly route by destination region anyway).

---

## 4. Management plane (Next.js)

> Source: previously `docs/TECH.md` §"Management plane" (merged here on 2026-05-20).

### 4.1 Audience split

The user-facing web management plane serves provider, customer, account, and consumer-VPN surfaces through a single Next.js app on the **apex `iogrid.org`**, route-segmented:

| Route prefix | Audience | Surface |
|--------------|----------|---------|
| `/provide/*` | Providers | Earnings dashboard, schedule editor, opt-in categories, transparency feed, payout settings |
| `/customer/*` | B2B customers | API keys, usage metrics, billing, audit logs, support |
| `/account/*` | Both | Identity management (linked emails / OAuth providers), preferences |
| `/vpn/*` | Consumer VPN | Download links, account upgrade, server selection (for paid Plus/Pro tiers) |

iogrid **staff/admin tooling** (abuse review, customer KYC, financial ops) lives in a **separate, independent admin app** served from `admin.iogrid.org` — its own codebase, Deployment, CI, and auth session. The admin app never renders provider/customer surfaces, and the user app never renders admin surfaces; the same human needs two distinct sessions.

Sign-in is email **magic-link** (working) plus **Google OAuth** (the button is hidden until a real OAuth client is configured). The user lands on a context-appropriate dashboard based on their primary role. Role-switching is available in nav (a provider who is also a customer can toggle).

> **Note on `app.iogrid.org`:** the `app.` subdomain was **dropped** (EPIC #422). It now 301-redirects to the apex; the product app serves `iogrid.org` directly. Do not treat `app.iogrid.org` as a live surface.

### 4.2 Stack

- **Next.js 15** with App Router, React Server Components by default.
- **TypeScript 5.x** strict mode.
- **shadcn/ui** (Radix primitives + Tailwind utilities), customised to iogrid design tokens.
- **Tailwind 4** with CSS variables for theming.
- **TanStack Query** for client-side data fetching where needed (real-time updates).
- **Zustand** for client state (sparingly).
- **React Hook Form + Zod** for form validation.
- **Server-Sent Events (SSE)** for real-time transparency dashboard updates.
- **WebSocket** for the in-product chat support widget.
- **Playwright** for E2E tests.
- **Vitest** for unit tests.
- **Storybook** for component library development.

### 4.3 Data fetching pattern

- **Server Components** for initial page render (SEO + fast first paint).
- **Server Actions** for mutations (no separate API route boilerplate).
- **TanStack Query** only for real-time-updating widgets (transparency feed, earnings counter).
- All data flows through `gateway-bff` — the Next.js app never talks directly to a microservice.

### 4.4 Accessibility

- WCAG 2.2 AA target from day 1 (not bolted on).
- Keyboard-only navigation tested.
- Screen-reader semantics via Radix primitives.
- High-contrast + reduced-motion preference support.
- All forms have explicit labels, error messages tied to fields.

### 4.5 Internationalisation

- Next.js native i18n routing.
- Languages day-1: English, Spanish, Portuguese, German, French, Italian, Turkish.
- Right-to-left support (Arabic, Hebrew) infrastructure even if translations land later.

---

## 5. Authentication & identity model

> Source: previously `docs/TECH.md` §"Authentication & identity model" (merged here on 2026-05-20).

### 5.1 Identity primitives

```
User (canonical, immutable)
  └── Identifier (binding, can have many per user)
       ├── kind: google | magic-link | apple | github | solana_wallet
       ├── verified_email: string (when applicable)
       └── created_at, last_used_at
```

Auth NEVER stores passwords. The web paths:

1. **Magic link** (working today) — user types email; we send a single-use, 10-minute, signed token via Stalwart SMTP. They click → authenticated. Same as Notion, Vercel, Linear.
2. **Google OAuth** — standard authorization-code flow. We pull `email`, `email_verified`, `name`, `picture`, `sub`. Additionally, Google's `id_token` may include `hd` (hosted domain — for Google Workspace users) which feeds enterprise routing. **The Google sign-in button is hidden in the UI until a real OAuth client is configured** — magic-link is the live path until then.

On **mobile (iOS)**, the path is **Sign in with Apple** (native ASAuthorization), which mints/links a `kind=apple` identifier through the same identity-svc.

### 5.2 Account merging — auto when safe

When a user authenticates via path B (e.g. magic-link to `john@company.com`), identity-svc checks:

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

### 5.3 JWT issuance

- Short-lived access token: 15 minutes, RS256-signed, includes user ID + active roles.
- Refresh token: 30 days, opaque, stored server-side (rotation on use).
- Session revocation: users can revoke any session from `/account/sessions`.

### 5.4 Service mTLS (SPIFFE-style identities via Cilium)

Every coordinator microservice carries a SPIFFE-style identity issued from the Cilium service-mesh control plane. Identity-aware `CiliumNetworkPolicy` enforces mutual auth at L7 — a connection from `proxy-gateway` to `billing-svc` is rejected at the kernel unless `proxy-gateway`'s SVID is presented and matches the policy's `selector.matchLabels`. SPIFFE IDs are of the form `spiffe://iogrid/ns/iogrid/sa/<service>`.

The plain Kubernetes `NetworkPolicy` (L3/L4) ships in parallel as defence-in-depth. Operators verify mutual auth via `cilium hubble observe --identity=<svid>`. The detailed mTLS debug walk lives in [`SECURITY.md`](./SECURITY.md) §3.

### 5.5 Privileged operations require step-up auth

Payout changes, identity merging, account deletion → require fresh auth within last 5 minutes. Initiates an additional magic-link or OAuth re-auth.

### 5.6 B2B customer authentication

Same identity primitives. Workspace concept layered on top:

```
User ← member-of → Workspace ← owns → APIKey, ResourceSpend, Settings
```

A workspace can have multiple users with role-based permissions (owner / admin / billing-only / read-only).

---

## 6. Workload deep-dives

> Source: previously `docs/ARCHITECTURE.md` §"Workload deep-dives" (rebased here).

### 6.1 Bandwidth proxy

- Coordinator accepts customer HTTP CONNECT or SOCKS5 from `proxy.iogrid.org:443` (TLS-encapsulated).
- Customer specifies geo (country) and session ID.
- Scheduler picks an eligible provider matching geo + opt-in + load.
- Coordinator establishes a 3-hop route: customer → coordinator → WireGuard tunnel → provider → destination.
- Anti-abuse pre-flight: destination domain checked against CSAM hash, fraud blocklist, customer's allowed-categories.
- Bandwidth metered at coordinator (out + in), attributed to customer (for billing) and provider (for payout).
- Session sticky to same provider for up to 30 minutes (configurable).

### 6.2 Docker compute

- Customer submits container image reference (must come from approved registry: ghcr.io, docker.io official-images, custom registry with provider-pre-pull).
- Container spec includes resource caps (CPU, RAM, time limit) and category tag (ML-inference, batch, build, etc.).
- Scheduler picks provider with capacity + opt-in for category.
- Coordinator pushes container ref + run spec to provider daemon.
- Provider's local Docker runs the container with cgroup limits + read-only filesystem + no host network access (only via routed iogrid tunnel).
- Logs streamed back to coordinator, then to customer via signed URL.
- Resource usage metered, billed CPU-hour and RAM-hour.

### 6.3 GPU / AI inference

- Same as Docker compute but coordinator schedules to provider with available GPU.
- NVIDIA (Linux): `--gpus all` + NVIDIA Container Toolkit.
- Apple Silicon (Mac): MLX-based runtime, custom GPU-shaped container (Apple's Virtualization framework + MLX bindings).
- Customer specifies VRAM requirement; scheduler matches to providers with that much GPU memory.

### 6.4 iOS builds (Mac providers only)

- Customer pushes their source repo + Xcode build command to coordinator's S3-compatible bucket.
- Coordinator generates short-lived signed URL.
- Scheduler picks Mac provider with Tart + suitable Xcode version installed.
- Provider daemon spawns ephemeral macOS VM via Tart (Cirrus Labs, open source) — VM has Xcode, build tools, no provider host filesystem access.
- VM clones source, runs `xcodebuild`, archives artefacts, uploads to coordinator's S3.
- VM destroyed at end of build.
- Build time metered, billed per minute.

Reference: Tart is the same tech [Cirrus CI](https://cirrus-ci.org) uses for their macOS runners. Open source, well-maintained, performant on Apple Silicon.

---

## 7. Scheduling — combined cap + calendar + idle

> Source: previously `docs/TECH.md` §"Scheduling state machine" + §"Scheduling — combined cap + calendar + idle" (merged here on 2026-05-20).

### 7.1 Scheduling state machine

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

### 7.2 Defaults out-of-the-box (first install)

| Setting | Default | Why |
|---------|---------|-----|
| Bandwidth cap | 50 GB/month | Conservative for non-power-user; ~80% of US home plans have >1 TB total bandwidth, this uses 5% |
| CPU cap | 30% | Allows user's other apps to feel responsive even under load |
| Memory cap | 25% of system RAM | Same logic |
| GPU cap | 100% (when system idle) / 0% (when user active) | GPU workloads benefit most from full power |
| Calendar | Always-on (no restriction) | Sensible default; users with thermal concerns can restrict to nights |
| Idle-only mode | ON, 5-minute threshold | Activates only when user is away. Zero perceived performance impact. |
| Categories allowed | E-commerce, SEO, Ad-verification, AI-training-data, iogrid-internal | Excludes lead-gen scraping (LinkedIn-ish), social-media intelligence — provider can opt in if they want |
| Destination blocklist | (Empty by default) | Provider can add their employer's domain, banking domains, etc. |

This default makes grandma's PC contribute only when she's not using it.

### 7.3 Power-user customisation

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

## 8. Transparency layer

> Source: previously `docs/TECH.md` §"Real-time transparency feed" (merged here on 2026-05-20).

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

---

## 9. Security model

> Source: previously `docs/ARCHITECTURE.md` §"Security model" + §"Failure modes" (rebased here).

### 9.1 Threat: malicious customer

A customer submits malicious code to be executed (Docker workload) or pushes traffic to harmful destinations (bandwidth).

**Mitigations:**

- Pre-flight URL/domain check via CSAM hash, PhishTank, OpenPhish (every bandwidth request).
- Container submission requires approved image source OR sandboxed signed-by-iogrid container builder.
- Outbound port restrictions: no SMTP (25, 465, 587), no IRC (6667), no SSH brute-force patterns.
- Per-customer rate limits, abuse-flag escalation.
- Provider isolation: containers run in user-namespaced cgroups, no host filesystem access, no host network namespace.
- iOS builds run in Tart VMs (Apple's Virtualization framework, hypervisor isolation).
- Customer KYC for high-volume accounts (>$500/month).

### 9.2 Threat: malicious provider

A provider intercepts customer traffic, alters responses, or attempts to inject malicious content.

**Mitigations:**

- All customer-to-coordinator traffic is TLS (customer terminates at coordinator).
- Bandwidth workload: coordinator does not decrypt customer's HTTPS; provider only sees encrypted bytes. Customer's certificate validation happens client-side.
- Docker workload: customer specifies container image hash, daemon verifies before run; outputs uploaded to coordinator's S3 with checksum verification by customer.
- iOS builds: artefact integrity verified by customer post-upload (hash + signed build manifest).
- Provider reputation scoring: customers can report failed/altered jobs; bad providers downweighted then ejected.

### 9.3 Threat: provider IP gets blamed for bad traffic

This is the biggest legal risk. Anti-abuse must be aggressive enough that the case for the provider being a passive intermediary is defensible.

**Mitigations:**

- Aggressive pre-flight filtering (above).
- Per-provider audit log retained 90 days, identifying the specific customer behind every request through their IP.
- Provider notification on every law-enforcement contact (we cover legal fees from defence fund — see [`BUSINESS-STRATEGY.md`](./BUSINESS-STRATEGY.md) §6.5).
- Provider ToS includes consent language matching common-carrier doctrine.
- Customer ToS makes them liable for their requests with explicit indemnity clause.

### 9.4 Threat: provider's LinkedIn / banking gets flagged

If a customer scrapes LinkedIn through a provider's IP, LinkedIn might temporarily flag that IP (including the provider's own normal LinkedIn session).

**Mitigations:**

- Per-provider per-destination rate limits (no single provider IP serves more than N requests/hour to LinkedIn or other top targets).
- Provider can blocklist specific destinations they care about (e.g. banking domains).
- IP rotation: provider serves a destination for at most 10 minutes before being swapped to a different provider for that customer session.
- Real-time alerts to provider: "your IP just served linkedin.com for 8 minutes" with one-click categorical blocklist.

### 9.5 Failure modes

#### Provider goes offline mid-workload

- Bandwidth: scheduler re-routes to another provider, customer experiences brief reset (~1s).
- Docker: scheduler retries on another provider, job restarts (idempotency is the customer's responsibility).
- GPU: same as Docker.
- iOS build: retried on another Mac provider, partial work lost (build is restarted from clean VM).

#### Coordinator goes offline

- Phase 0/1: single coordinator, downtime is an outage. Acceptable for closed beta.
- Phase 2+: regional coordinator failover, eventually-consistent provider registry. Customer connections retry to backup coordinator.

#### Network partition between customer and coordinator

- Customer SDK retries with exponential backoff up to 60 s.
- Beyond that, customer's job fails-fast and returns error.

#### Customer overruns rate limit

- Coordinator returns 429 with `Retry-After` header.
- Persistent overrun → account flagged for KYC verification + throttle.

---

## 10. DNS + TLS architecture

> Source: previously `docs/DNS_TLS.md` (merged here on 2026-05-20).

End-state decisions are locked. This section explains *why* each piece is the way it is and how to operate it day-to-day.

### 10.1 TL;DR

| Concern               | Choice                                               | Why                                                                                 |
|-----------------------|------------------------------------------------------|-------------------------------------------------------------------------------------|
| Registrar             | Dynadot                                              | Founder's account; same account as `openova.io` and `dynolabs.io`                  |
| Authoritative DNS     | **Dynadot Hosted DNS** (`ns1.dyna-ns.net`, `ns2.dyna-ns.net`) | Keeps iogrid independent from OpenOva's PowerDNS stack — see §10.3 below |
| Record mutation       | Dynadot API `set_dns2` from a kubectl-execed pod on the OpenOva mothership | Mothership public IP is allowlisted by Dynadot; bastion is not                    |
| Source of truth       | `infra/dynadot/iogrid-org-records.json` in this repo | Code-reviewed, branch-protected, auditable                                          |
| TLS certificates      | Let's Encrypt via cert-manager, HTTP-01 over Cilium Gateway API | Re-uses the existing cluster-wide `letsencrypt-prod` ClusterIssuer                  |
| Ingress / L7 routing  | Gateway API (Cilium gateway class), TLSRoute + HTTPRoute | Matches end-state architecture (commit `f49fe50`)                                  |
| Public ingress IP     | `45.151.123.50` (OpenOva mothership Hetzner LB)       | Single-VM today, will move to a dedicated iogrid LB before public launch           |

### 10.2 DNS record set

All records point at the mothership LB IP `45.151.123.50`. TTL 300 s so we can flip to a dedicated LB IP without operational pain. The exact record set is whatever `infra/dynadot/iogrid-org-records.json` declares (the source of truth) — the table below is illustrative.

| Hostname               | Type | Value           | Backend                                          |
|------------------------|------|-----------------|--------------------------------------------------|
| `iogrid.org`           | A    | 45.151.123.50   | web (Next.js) :3000 — the apex serves the app    |
| `www.iogrid.org`       | A    | 45.151.123.50   | web (Next.js) :3000                              |
| `api.iogrid.org`       | A    | 45.151.123.50   | gateway-bff :8080                                |
| `admin.iogrid.org`     | A    | 45.151.123.50   | admin app (separate Next.js Deployment) :3000    |
| `app.iogrid.org`       | A    | 45.151.123.50   | **301 → apex** (subdomain dropped, EPIC #422; kept during the cert grace window) |
| `proxy.iogrid.org`     | A    | 45.151.123.50   | proxy-gateway :443 (TLS passthrough)             |
| `build.iogrid.org`     | A    | 45.151.123.50   | build-gateway :8080                              |
| `releases.iogrid.org`  | A    | 45.151.123.50   | desktop-installer release artifacts              |
| `updates.iogrid.org`   | A    | 45.151.123.50   | daemon auto-update feed                          |
| `status.iogrid.org`    | A    | 45.151.123.50   | gateway-bff :8080 (URLRewrite to `/status`)      |

Source of truth: [`infra/dynadot/iogrid-org-records.json`](../infra/dynadot/iogrid-org-records.json).

#### Why no wildcard

A wildcard `*.iogrid.org A 45.151.123.50` would cover any new subdomain automatically, but:

1. Wildcards encourage uncontrolled subdomain sprawl ("just spin up `experimental.iogrid.org`") which makes security-scope arguments harder.
2. The matching wildcard TLS cert needs DNS-01 ACME, which couples cert renewal to a Dynadot API key with write access. HTTP-01 over a discrete subdomain list keeps the renewal path read-only on DNS.
3. The subdomain set is small and well-defined — explicit > implicit.

If a new subdomain is needed, edit `infra/dynadot/iogrid-org-records.json` + the certificate SAN list + add an HTTPRoute, one PR.

### 10.3 Independence from OpenOva DNS

OpenOva runs a central PowerDNS in `openova-system` and delegates `omani.works` etc. to `ns1.openova.io`/`ns2.openova.io`. We deliberately do NOT do this for iogrid:

- Per the end-state lock (commit `f49fe50` in this repo), iogrid is an independent brand. Its DNS resolution path must not transit OpenOva control.
- Outages on the OpenOva PowerDNS pods should not blacken iogrid.
- iogrid will eventually move to its own k8s cluster + LB; collapsing the move is easier when DNS is already first-party.

The price we pay: every iogrid DNS change is a Dynadot API call rather than a `kubectl apply -f zone.yaml`. We mitigate via the script in `scripts/dynadot-apply.sh` which makes the change auditable and gated by code review.

### 10.4 Operational procedure — adding/changing a record

1. Edit `infra/dynadot/iogrid-org-records.json`.
2. Run `./scripts/dynadot-apply.sh` (dry-run; prints the URL it *would* call).
3. Open a PR.
4. After merge, on the bastion: `./scripts/dynadot-apply.sh --apply`. Requires kubectl access to the OpenOva mothership cluster (mothership IP is Dynadot-allowlisted; bastion is not, so the script kubectl-execs a one-shot Alpine pod on the mothership).
5. `./scripts/dynadot-apply.sh --verify` polls public resolvers; expect convergence within 5 minutes.

**WARNING:** Dynadot `set_dns2` is a *replace-whole-zone* call. Records not in the JSON file are deleted on apply. Always merge from JSON, never hand-edit via the Dynadot web UI.

### 10.5 TLS certificate lifecycle

#### Issuer

The ClusterIssuer `letsencrypt-prod` is shared with the OpenOva tenant on the mothership. Manifest: [`infra/k8s/cert-manager/cluster-issuer-letsencrypt.yaml`](../infra/k8s/cert-manager/cluster-issuer-letsencrypt.yaml). It carries two HTTP-01 solvers:

- Primary: `gatewayHTTPRoute` targeting the iogrid Gateway. cert-manager auto-creates a transient HTTPRoute on port 80 hitting `/.well-known/acme-challenge` during validation.
- Fallback: `ingress.ingressClassName=traefik`. Used while the mothership is still on Traefik (transitional). Safe to delete once Cilium Gateway is the only ingress.

#### Certificate CRs

[`infra/k8s/certificates/iogrid-org-cert.yaml`](../infra/k8s/certificates/iogrid-org-cert.yaml) declares two `Certificate` objects:

- `iogrid-org-tls` — SAN list of all TLS-terminated hostnames (apex + www + the api/admin/build/status subdomains, plus app during its redirect grace window), ECDSA P-256, 90 d duration, 30 d renewBefore. Stored as `Secret/iogrid-org-tls` in the `iogrid` namespace.
- `iogrid-proxy-tls` — separate single-SAN cert for `proxy.iogrid.org`. Even though that listener does TLS passthrough, we still hold a public cert so the Gateway can do hostname-based SNI routing and so future direct-terminate experiments are one annotation flip away.

#### Renewal

cert-manager auto-renews 30 days before expiry. The renewal HTTPRoute is short-lived (typically < 60 s) and does not interfere with normal traffic — Gateway API listeners on port 80 accept the challenge alongside the http→https redirect routes.

#### Troubleshooting

```bash
# Status of all certs
kubectl -n iogrid get cert,certificaterequest,order,challenge

# Verbose on a failing cert
kubectl -n iogrid describe cert iogrid-org-tls
kubectl -n iogrid describe challenge | tail -50

# cert-manager controller logs
kubectl -n cert-manager logs -l app.kubernetes.io/name=cert-manager --tail=200 | grep -i iogrid
```

Common failures:

- **Pending challenge, `propagation check failed`:** the temporary HTTPRoute hasn't been routed by Cilium yet. Check `kubectl -n iogrid get httproute,gateway` and look for `Programmed=True`.
- **Rate-limit error:** LE allows 50 certs per registered domain per week. If you blew the budget by re-creating, switch the issuer to `letsencrypt-staging` to develop, then flip back.
- **DNS NXDOMAIN at the challenge step:** a record didn't propagate yet. `dig +short <host> A @1.1.1.1`. Wait 5 min after a `dynadot-apply.sh --apply`.

### 10.6 Gateway API routing

[`infra/k8s/gateways/gateway.yaml`](../infra/k8s/gateways/gateway.yaml) declares one Gateway (`iogrid-gateway`) with HTTP listeners on port 80 (ACME + http→https redirect), per-hostname HTTPS listeners on port 443 presenting the matching cert from `iogrid-org-tls`, and one TLS-Passthrough listener for `proxy.iogrid.org`.

Each HTTPRoute targets a listener via `parentRefs[].sectionName`:

| Route file                        | Backend service        | Notes                                          |
|-----------------------------------|------------------------|------------------------------------------------|
| `httproute-apex-www.yaml`         | web :3000              | Apex `iogrid.org` + www serve the Next.js app  |
| `httproute-app.yaml`              | (redirect)             | `app.iogrid.org` → 301 apex (subdomain dropped)|
| `httproute-api.yaml`              | gateway-bff :8080      | REST + gRPC-web BFF                             |
| `httproute-build.yaml`            | build-gateway :8080    | iOS-build orchestrator                         |
| `httproute-status.yaml`           | gateway-bff :8080      | URLRewrite to /status                          |
| `tlsroute-proxy.yaml`             | proxy-gateway :443     | TLS passthrough                                |

`admin.iogrid.org` routes to the independent admin Deployment via its own HTTPRoute.

#### Transitional Traefik bridge

The mothership currently runs Traefik (the OpenOva ingress controller) bound to `45.151.123.50:80/443`. Cilium Gateway is not yet the default IngressController on this cluster. Until it is, the iogrid `Gateway` object is the canonical routing intent — Flux on the iogrid-ops repo materialises it; a separate PR adds a Traefik IngressRoute shim derived from the same JSON record source.

When the mothership migrates to Cilium Gateway (or iogrid moves to its own LB), the Gateway object becomes the live router with zero rewrites.

### 10.7 Provisioning order on a fresh cluster

```bash
# 1. Apply infra/k8s in order (cert-manager assumed already running)
kubectl apply -f infra/k8s/namespaces/iogrid.yaml
kubectl apply -f infra/k8s/cert-manager/cluster-issuer-letsencrypt.yaml
kubectl apply -f infra/k8s/gateways/gateway.yaml
kubectl apply -f infra/k8s/gateways/      # HTTPRoutes + TLSRoute
kubectl apply -f infra/k8s/certificates/  # triggers cert-manager order

# 2. DNS (must come AFTER LB IP allocated; cert solving needs DNS to resolve to the LB)
./scripts/dynadot-apply.sh --apply

# 3. Wait for certs
kubectl -n iogrid wait --for=condition=Ready cert/iogrid-org-tls --timeout=10m
kubectl -n iogrid wait --for=condition=Ready cert/iogrid-proxy-tls --timeout=10m

# 4. Sanity
curl -sI https://iogrid.org/ | head -5
curl -sI https://api.iogrid.org/ | head -5
```

### 10.8 What is intentionally NOT here

- **DNSSEC:** not enabled today. Dynadot supports it; we add it once the registrar→authoritative key-handover is automated. Tracked as a follow-up.
- **CAA records:** not yet set. Add `iogrid.org CAA 0 issue "letsencrypt.org"` once cert issuance is steady-state to prevent rogue CA misissuance.
- **HSTS preload:** cert pinning is enabled per host via the future Cilium L7 policy. HSTS headers are set by the web app, not at the Gateway.
- **Per-region anycast:** single-region today (Hetzner Nuremberg via Contabo VPS). Multi-region rolls in once the second mothership exists.

---

## 11. Installation UX — grandma-proof

> Source: previously `docs/TECH.md` §"Installation UX" (merged here on 2026-05-20).

### 11.1 Mac install

```bash
# Single command, no dependencies pre-installed expected
curl -fsSL https://iogrid.org/install/mac | sh
```

What this runs:

1. Detects macOS version, Apple Silicon vs Intel.
2. Checks if Docker Desktop is installed; if not, downloads + auto-installs the signed `.dmg`.
3. Checks if Tart is installed (only if user opts into iOS-build later); if not, `brew install cirruslabs/cli/tart` (installs Homebrew first if needed).
4. Downloads signed daemon binary (Sparkle-style auto-update on subsequent runs).
5. Installs daemon as a LaunchAgent (auto-start on login, runs as user not root).
6. Opens browser to `https://iogrid.org/onboard/<token>` with a one-time pairing token.
7. User signs in (email magic-link; Google OAuth once configured) → daemon pairs → user sees first dashboard.

Time from `curl` to "your PC is earning": ~2 minutes (mostly Docker Desktop download).

### 11.2 Windows install

```powershell
iwr -useb https://iogrid.org/install/win | iex
```

1. Detects Windows version, x64 vs ARM64.
2. Installs Docker Desktop (with WSL2 backend) if missing.
3. Installs signed daemon as a Windows Service.
4. Opens browser to onboarding flow.

### 11.3 Linux install

```bash
curl -fsSL https://iogrid.org/install/linux | sudo sh
```

1. Detects distro (apt / dnf / pacman / apk).
2. Installs Docker if missing (uses distro's official Docker package).
3. Installs daemon as systemd user service.
4. Prints URL for browser onboarding (since headless servers can't auto-open).

### 11.4 What grandma sees

She doesn't run `curl`. She visits **iogrid.org**, clicks "Install for Mac" / "Install for Windows" — a **signed installer** (`.dmg` / `.msi` / `.deb` / `.rpm`) downloads. Double-click. Click "Continue" 3 times. Browser opens. Sign in with an emailed magic link. Done.

The `curl | sh` is for developers / power users who prefer terminal. Same end state.

### 11.5 What gets installed where

| Component | Mac | Linux | Windows |
|-----------|-----|-------|---------|
| Daemon binary | `/usr/local/iogrid/iogridd` | `/usr/local/bin/iogridd` | `C:\Program Files\iogrid\iogridd.exe` |
| Service registration | LaunchAgent `~/Library/LaunchAgents/org.iogrid.plist` | systemd user unit `~/.config/systemd/user/iogridd.service` | Windows Service `iogridd` |
| Config | `~/Library/Application Support/iogrid/config.toml` | `~/.config/iogrid/config.toml` | `%APPDATA%\iogrid\config.toml` |
| Logs | `~/Library/Logs/iogrid/*.log` | `~/.local/share/iogrid/logs/*.log` | `%LOCALAPPDATA%\iogrid\logs\*.log` |
| Tart VMs (Mac only, if iOS-build enabled) | `~/.tart/vms/` | — | — |
| Docker containers | (managed by Docker Desktop / Docker Engine) | | |

### 11.6 Uninstall

`iogridd uninstall` — single command, removes service registration, daemon binary, config, logs. Bandwidth/compute data is purged from the server-side after a 7-day grace period.

---

## 12. Observability

> Source: previously `docs/TECH.md` §"Observability" (merged here on 2026-05-20).

### 12.1 Metrics

- All services emit OpenTelemetry metrics with consistent labels: `service`, `customer_id`, `provider_id`, `workload_type`, `region`.
- Pushed to existing OpenOva mothership Mimir / VictoriaMetrics.
- Dashboards in Grafana: per-service health, per-customer usage, per-provider earnings, anti-abuse hit rates.

### 12.2 Logging

- Structured JSON via slog (Go) / tracing (Rust).
- Shipped to Loki.
- Sensitive fields (customer URLs, provider IPs) hashed at log boundary; raw values accessible only to authorised humans via audit-grant flow.

### 12.3 Tracing

- W3C Trace Context propagation across services and from daemon.
- Tempo backing store, sampled at 1% in production, 100% for flagged-abuse requests.

### 12.4 SLOs (Phase 2 commitments)

- Proxy-gateway availability: 99.9% / month.
- Workload dispatch latency p95: 100 ms.
- Web management plane TTI: <2 seconds on cold cache, <500 ms warm.
- Magic-link delivery: 95% < 30 seconds.

### 12.5 Alerting

- SLO burn rate > 2× → page on-call.
- Anti-abuse hit rate spike → notify abuse team.
- Anomalous provider behaviour (sudden bandwidth spike, IP reputation drop) → automatic temp-suspend + human review.

Routing is defined in `infra/k8s/base/telemetry-svc/alertmanager-config.yaml`. Three receiver tiers:

- `iogrid-page` — PagerDuty + `#iogrid-incidents` Slack (severity=page, e.g. 14× burn rate, synthetic-probe DOWN).
- `iogrid-warn` — `#iogrid-warnings` Slack only (severity=warn, e.g. 2× burn rate, capacity warnings).
- `iogrid-abuse` — `#iogrid-abuse-team` Slack (category=abuse, separate moderation team).

Inhibition rules suppress lower-severity alerts when a page-tier alert is already firing for the same service.

### 12.6 Public status page

`status.iogrid.org` is the customer-facing rollup. Served from the web app's `web/src/app/status/` route (Next.js, Tailwind) and consumes three public, world-readable JSON endpoints on `telemetry-svc`:

| Endpoint | Purpose |
|----------|---------|
| `GET /status/posture` | One JSON envelope — `overall: up\|degraded\|down`, per-service grid, active incidents, last-7-days history |
| `GET /status/uptime?service=<name>&days=90` | Per-service 90-day uptime ledger that feeds the calendar heatmap |
| `POST /status/subscribe` | Email subscription registry (rate-limited per-IP) |

Operator-curated incidents are managed via admin-token-gated mutations: `POST /status/incidents`, `POST /status/incidents/{id}/updates`. Incidents follow the StatusPage.io lifecycle (`investigating → identified → monitoring → resolved`) with four impact tiers (`none | minor | major | critical`). The status-page operations runbook lives in [`RUNBOOKS.md`](./RUNBOOKS.md).

Storage is Postgres-backed (`incidents`, `incident_updates`, `status_subscriptions`, `uptime_samples` tables in the telemetry-svc CNPG database) with an in-memory fallback so the public `/status/*` endpoints stay responsive even when the DB is unavailable — a status page that itself looks broken is the worst possible UX during a real outage.

The page polls `/status/posture` every 60 seconds and degrades gracefully:

- If `/status/posture` is unreachable, the page shows a "stale data" pill and keeps rendering the last good payload.
- If telemetry-svc has been unreachable since first load, the page falls back to a baseline frame from a static `incidents-static.json` bundled with the web app — operators edit that file as a backstop during a status-svc outage.

### 12.7 Grafana dashboard provisioning

iogrid does NOT run its own Grafana. Dashboards live as JSON under `coordinator/services/telemetry-svc/dashboards/`, are copied into `infra/k8s/base/telemetry-svc/assets/dashboards/` (single source of truth, kustomize load-restrictor workaround), and ship as a single ConfigMap with `grafana_dashboard: "1"` label. The mothership Grafana's `kiwigrid/k8s-sidecar` auto-discovers it and provisions dashboards into the "iogrid" folder. Six dashboards in total:

- `iogrid-overview` — global RPS / error rate / P99.
- `iogrid-services` — per-service SLI rollups.
- `iogrid-customers` — per-customer usage panels.
- `iogrid-providers` — per-provider earnings + workload mix.
- `iogrid-abuse` — anti-abuse spike detector.
- `iogrid-revenue` — GMV, payout queue, $GRID emission.

The `infra/k8s/base/grafana/` directory ships the auxiliary "iogrid" Grafana Folder + the tenant-scoped Mimir datasource (`X-Scope-OrgID: iogrid` header) the dashboards reference. Dashboard edits must go through git — UI-side edits are reverted by the next sidecar reconcile.

---

## 13. Bleeding-edge tech choices

> Source: previously `docs/TECH.md` §"Bleeding-edge tech choices" (merged here on 2026-05-20).

These are explicit "we use the modern thing" decisions, not "we'll pick later":

- **Connect-Go** over raw gRPC for the customer-facing API — HTTP/2 compatible, JSON-fallback for debugging, single-binary code-gen.
- **Buf** for protobuf management instead of `protoc` — lint + breaking-change detection + reproducible builds.
- **NATS JetStream** for cross-service events instead of Kafka — operates on a 50 MB pod (vs Kafka's 4 GB JVM minimum).
- **Cilium** instead of kube-proxy — Linux eBPF kernel-bypass, lower latency, native L7 mTLS.
- **SPIFFE / SPIRE-style** workload identities instead of Vault for service mTLS.
- **Bun** alongside Node.js for build tooling on Next.js side — faster cold builds.
- **Drizzle ORM** instead of Prisma — closer to SQL, no migration black box.
- **Rust tonic + h2** for gRPC on daemon — single async runtime stack, no Python/Node interop.
- **Tart** for macOS VM provisioning — open source from Cirrus Labs, performance-tuned for Apple Silicon.
- **gVisor** or **Kata Containers** for stronger Docker isolation on Linux providers.
- **Stripe Connect** + **Stripe Tax** for international tax compliance out of the box.
- **PostHog** (self-hostable, OpenOva-deployable) for product analytics instead of GA4.
- **Sentry** (self-hostable) for error tracking.
- **Playwright** for E2E + **MSW** for API mocking instead of legacy Cypress.

---

## 14. What's NOT in scope (decided)

> Source: previously `docs/ARCHITECTURE.md` §"What's NOT in scope" (rebased here).

- **Real-time audio/video** (meeting recording bots) — legal nightmare around recording consent, latency-sensitive to home connection jitter, providers' Zoom/Teams accounts would get banned.
- **General consumer VPN feature ON THE iOS APP** that uses the same iOS device as a provider — Apple App Store Guideline 5.4.6 forbids this; iOS users are client-only.
- **Built-in cryptocurrency wallet** for customers — Stripe / wire transfers suffice (provider wallets are the only crypto-touching surface, scoped to $GRID payout — see [`BUSINESS-STRATEGY.md`](./BUSINESS-STRATEGY.md) §4).
- **Mobile SDK for customers** — coordinator is API-only, customers integrate from their own infra.

---

## 15. Phase 0 implementation subset

> Source: previously `docs/TECH.md` §"What this enables for Phase 0" (merged here on 2026-05-20).

The architecture above IS the end state. Phase 0 implementation ships a subset:

- Daemon: `core` + `transport` + `routing` (bandwidth only) crates.
- Coordinator: `identity-svc` + `providers-svc` + `workloads-svc` + `antiabuse-svc` + `proxy-gateway` + `gateway-bff`.
- Web plane: `/account/*` + `/provide/*` (basic dashboard) only; `/customer/*` deferred until first paying customer.
- One internal customer (`vcard-api`) consuming the proxy-gateway for LinkedIn enrichment.

Subsequent modules (Docker workload, GPU workload, iOS-build, VPN gateway, customer-facing dashboard, billing) plug into the same architecture without redesign.
