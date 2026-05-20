# iogrid — Repo-specific Notes

> This is a product repo (iogrid distributed compute + bandwidth mesh). Generic OpenOva platform working principles live in `~/.claude/CLAUDE.md` (user-global).

## What this is

iogrid is a peer-to-peer network where home PC/Mac owners share idle compute and bandwidth with enterprise customers, in exchange for cash, free unlimited VPN, or charity contributions. Customer workloads cover residential-IP proxy, Docker compute, GPU inference, and macOS-native iOS-build CI. Mobile users (iOS/Android) are consume-only (VPN). Differentiators: per-byte transparency for providers, multi-currency payouts, first-class iOS-build workload at ~50% of GitHub Actions pricing.

## What lives in this repo

| Concern | Path |
|---|---|
| Rust provider daemon (single static binary) | `daemon/` |
| Go microservices control plane | `coordinator/` (identity-svc, providers-svc, workloads-svc, antiabuse-svc, billing-svc, telemetry-svc, gateway-bff, proxy-gateway, build-gateway) |
| Next.js 15 management web (providers + customers + VPN) | `web/` |
| Protobuf schemas (Buf-managed) | `proto/` |
| Flux-managed K8s manifests | `infra/k8s/`, `gitops/` |
| Customer SDKs (TS, Python, Go, Java) | `sdks/` |
| Docs (architecture, roadmap, incentives, legal, market) | `docs/` |
| E2E tests | `e2e/` |

## Tech stack

- Rust (daemon, cross-compiled darwin/linux/windows × amd64/arm64)
- Go (coordinator microservices, Connect-Go contracts, NATS JetStream eventing, Cilium service mesh)
- Next.js 15 + React Server Components + TypeScript + shadcn/ui + Tailwind 4 (web)
- pnpm workspace (Node >=22, pnpm >=9)
- Buf for protobuf lint/format/generate

## Development workflow

```bash
# Top-level make targets (proto pipeline)
make help
make proto                # buf generate stubs into coordinator/internal/pb + web/src/lib/pb
make proto-check          # CI parity: lint + format-check + generate-and-diff
make sdks                 # build all customer SDKs

# Web
cd web && pnpm install && pnpm dev

# Daemon
cd daemon && cargo build --release

# Coordinator microservice
cd coordinator/<svc> && go run ./cmd/server
```

## Known issues

- (empty for now — populate as discovered)

## Sub-agent cap for this project

Default (per user-global) unless project owner overrides here.
