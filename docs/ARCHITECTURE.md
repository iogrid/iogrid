# Architecture

## One platform, four workload types

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

---

## Components

### 1. Coordinator (Go, k8s)

The control plane. Single service running on Kubernetes (initially mothership Contabo, future federation possible). Components:

- **Provider registry** — connected daemons, their capabilities (OS, CPU, RAM, GPU, Mac+Xcode availability), opt-in categories, current state (online/offline/draining), quality score.
- **Workload scheduler** — receives customer workload submissions, matches to eligible providers using a multi-dimensional fit function (capability + geo + quality + opt-ins + load).
- **Customer API** — REST and gRPC, OpenAPI spec'd, API-key authenticated. Endpoints for proxy session creation, Docker workload submission, GPU job dispatch, iOS build trigger.
- **Auth + billing** — provider Stripe Connect, customer Stripe Subscriptions + usage metering.
- **Anti-abuse** — pre-flight filters per workload type: CSAM hash (PhotoDNA / NCMEC), fraud blocklists (PhishTank, OpenPhish), per-target rate limits, port restrictions on outbound bandwidth (no SMTP, no IRC).
- **Transparency dashboard** — provider-facing real-time view of "what's flowing through your IP," with category labels (e-commerce / SEO / ad-verify / lead-gen / AI-training-data / other) and per-destination breakdowns. Audit log per provider.
- **Customer dashboard** — usage, billing, API keys, geo targets, session-stickiness controls.

The coordinator is stateful (provider registry, audit logs, billing). Postgres backing store (existing CNPG cluster on mothership). Redis for ephemeral session/routing state.

### 2. Daemon (Go, cross-compiled single binary)

Installed on provider devices. One binary, OS-conditional behavior:

| Target triple | Workload types enabled by default |
|---------------|-----------------------------------|
| `darwin/arm64` (Apple Silicon Mac) | bandwidth, docker (via Colima), gpu (MLX), ios-build (Tart) |
| `darwin/amd64` (Intel Mac) | bandwidth, docker, ios-build (Tart Intel mode) |
| `linux/amd64` | bandwidth, docker, gpu (CUDA if NVIDIA present) |
| `linux/arm64` | bandwidth, docker (limited container image availability) |
| `windows/amd64` | bandwidth, docker (Docker Desktop / WSL2 backend) |

Provider controls (via local CLI or simple web UI on `localhost:7777`):
- Enable/disable individual workload types
- Set bandwidth cap (e.g., max 50 GB/month)
- Set compute caps (max CPU%, max RAM, max GPU memory)
- Block specific destinations (regex), block specific categories
- Choose payout currency (cash / VPN credits / OpenOva premium tier / charity)
- View real-time and historical activity

Daemon establishes a single long-lived gRPC stream to the coordinator. WireGuard tunnel for bandwidth workload is layered on top (no separate connection). Heartbeats every 15 s. Auto-reconnect with exponential backoff.

### 3. Proto (gRPC + protobuf)

Three protobuf schema files in `proto/`:

- `coordinator.proto` — daemon ↔ coordinator (RegisterProvider, Heartbeat, ReceiveWorkload, ReportResult, etc.)
- `customer.proto` — customer-facing public API
- `internal.proto` — coordinator-internal services (auth, billing, anti-abuse)

Generated Go bindings live in `coordinator/internal/pb/` and `daemon/internal/pb/`.

### 4. Infra (Kubernetes manifests)

Flux-managed deployment to Contabo mothership k8s. Phase 0 footprint:

- 1 coordinator Pod (Go binary)
- 1 Postgres DB (shared with existing CNPG cluster, `iogrid` namespace)
- 1 Redis instance for ephemeral state
- Ingress on `api.iogrid.org` (REST + gRPC)
- Ingress on `dashboard.iogrid.org` (provider + customer UIs, served from coordinator)

Phase 2+ federation: multiple coordinator regions (US-East, EU, APAC), eventually-consistent provider registry.

---

## Workload deep-dives

### Bandwidth proxy

- Coordinator accepts customer HTTP CONNECT or SOCKS5 from `proxy.iogrid.org:443` (TLS-encapsulated)
- Customer specifies geo (country) and session ID
- Scheduler picks an eligible provider matching geo + opt-in + load
- Coordinator establishes a 3-hop route: customer → coordinator → WireGuard tunnel → provider → destination
- Anti-abuse pre-flight: destination domain checked against CSAM hash, fraud blocklist, customer's allowed-categories
- Bandwidth metered at coordinator (out + in), attributed to customer (for billing) and provider (for payout)
- Session sticky to same provider for up to 30 minutes (configurable)

### Docker compute

- Customer submits container image reference (must come from approved registry: ghcr.io, docker.io official-images, custom registry with provider-pre-pull)
- Container spec includes resource caps (CPU, RAM, time limit) and category tag (ML-inference, batch, build, etc.)
- Scheduler picks provider with capacity + opt-in for category
- Coordinator pushes container ref + run spec to provider daemon
- Provider's local Docker runs the container with cgroup limits + read-only filesystem + no host network access (only via routed iogrid tunnel)
- Logs streamed back to coordinator, then to customer via signed URL
- Resource usage metered, billed CPU-hour and RAM-hour

### GPU / AI inference

- Same as Docker compute but coordinator schedules to provider with available GPU
- NVIDIA (Linux): `--gpus all` + NVIDIA Container Toolkit
- Apple Silicon (Mac): MLX-based runtime, custom GPU-shaped container (Apple's Virtualization framework + MLX bindings)
- Customer specifies VRAM requirement; scheduler matches to providers with that much GPU memory

### iOS builds (Mac providers only)

- Customer pushes their source repo + Xcode build command to coordinator's S3-compatible bucket
- Coordinator generates short-lived signed URL
- Scheduler picks Mac provider with Tart + suitable Xcode version installed
- Provider daemon spawns ephemeral macOS VM via Tart (Cirrus Labs, open source) — VM has Xcode, build tools, no provider host filesystem access
- VM clones source, runs `xcodebuild`, archives artifacts, uploads to coordinator's S3
- VM destroyed at end of build
- Build time metered, billed per minute

Reference: Tart is the same tech [Cirrus CI](https://cirrus-ci.org) uses for their macOS runners. Open source, well-maintained, performant on Apple Silicon.

---

## Security model

### Threat: malicious customer

A customer submits malicious code to be executed (Docker workload) or pushes traffic to harmful destinations (bandwidth).

**Mitigations:**
- Pre-flight URL/domain check via CSAM hash, PhishTank, OpenPhish (every bandwidth request)
- Container submission requires approved image source OR sandboxed signed-by-iogrid container builder
- Outbound port restrictions: no SMTP (25, 465, 587), no IRC (6667), no SSH brute force patterns
- Per-customer rate limits, abuse-flag escalation
- Provider isolation: containers run in user-namespaced cgroups, no host filesystem access, no host network namespace
- iOS builds run in Tart VMs (Apple's Virtualization framework, hypervisor isolation)
- Customer KYC for high-volume accounts (>$500/month)

### Threat: malicious provider

A provider intercepts customer traffic, alters responses, or attempts to inject malicious content.

**Mitigations:**
- All customer-to-coordinator traffic is TLS (customer terminates at coordinator)
- Bandwidth workload: coordinator does not decrypt customer's HTTPS; provider only sees encrypted bytes. Customer's certificate validation happens client-side.
- Docker workload: customer specifies container image hash, daemon verifies before run; outputs uploaded to coordinator's S3 with checksum verification by customer
- iOS builds: artifact integrity verified by customer post-upload (hash + signed build manifest)
- Provider reputation scoring: customers can report failed/altered jobs; bad providers downweighted then ejected

### Threat: provider IP gets blamed for bad traffic

This is the biggest legal risk. Anti-abuse must be aggressive enough that the case for the provider being a passive intermediary is defensible.

**Mitigations:**
- Aggressive pre-flight filtering (above)
- Per-provider audit log retained 90 days, identifying the specific customer behind every request through their IP
- Provider notification on every law-enforcement contact (we cover legal fees from defense fund)
- Provider ToS includes consent language matching common-carrier doctrine: "I agree to act as a passive intermediary; I have no knowledge of content; operator commits to filter content."
- Customer ToS makes them liable for their requests with explicit indemnity clause

### Threat: a provider's LinkedIn / banking account gets flagged for unusual traffic

If a customer scrapes LinkedIn through a provider's IP, LinkedIn might temporarily flag that IP (including the provider's own normal LinkedIn session).

**Mitigations:**
- Per-provider per-destination rate limits (no single provider IP serves more than N requests/hour to LinkedIn or other top targets)
- Provider can blocklist specific destinations they care about (e.g., banking domains)
- IP rotation: provider serves a destination for at most 10 minutes before being swapped to a different provider for that customer session
- Real-time alerts to provider: "your IP just served linkedin.com for 8 minutes" with one-click categorical blocklist

---

## Failure modes

### Provider goes offline mid-workload

- Bandwidth: scheduler re-routes to another provider, customer experiences brief reset (~1s)
- Docker: scheduler retries on another provider, job restarts (idempotency is the customer's responsibility)
- GPU: same as Docker
- iOS build: retried on another Mac provider, partial work lost (build is restarted from clean VM)

### Coordinator goes offline

- Phase 0/1: single coordinator, downtime is an outage. Acceptable for closed beta.
- Phase 2+: regional coordinator failover, eventually-consistent provider registry. Customer connections retry to backup coordinator.

### Network partition between customer and coordinator

- Customer SDK retries with exponential backoff up to 60s
- Beyond that, customer's job fails-fast and returns error

### Customer overruns rate limit

- Coordinator returns 429 with `Retry-After` header
- Persistent overrun → account flagged for KYC verification + throttle

---

## What's NOT in scope (decided)

- **Cryptocurrency / tokens** — adds regulatory complexity, our target users aren't crypto-native
- **Real-time audio/video** (meeting recording bots) — legal nightmare around recording consent, latency-sensitive to home connection jitter, providers' Zoom/Teams accounts would get banned
- **General consumer VPN feature ON THE iOS APP** that uses the same iOS device as a provider — Apple App Store Guideline 5.4.6 forbids this; iOS users are client-only
- **Built-in cryptocurrency wallet** for customers — Stripe / wire transfers suffice
- **Mobile SDK for customers** — coordinator is API-only, customers integrate from their own infra

---

## Open questions for Phase 1

- Whether to open-source the daemon (AGPL) for trust-via-transparency, while keeping coordinator proprietary
- Whether to support a federated model (multiple coordinator instances run by different parties) at Phase 2 or defer to Phase 3
- Whether to offer an embedded provider SDK for game-engine / desktop-app developers to bundle iogrid into their app (with explicit user consent at install time) — could massively accelerate provider acquisition but adds review complexity
