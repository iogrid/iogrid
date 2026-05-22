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

## Autonomy guardrails (project-specific reinforcement, 2026-05-23)

Cross-references user-global `~/.claude/CLAUDE.md` principles 7, 13, 21,
22 + diversion patterns D7, D12, D14, D15. The session log on this repo
keeps surfacing the same anti-patterns; this section restates the rule
in iogrid-vocabulary so pattern-matching catches them at this repo's
keystroke level.

1. **CI-green ≠ DoD.** DoD per CLAUDE.md §2 = operator walks the surface
   on a FRESH provisioned environment + screenshot attached to the issue.
   The 7 PRs being green is not a stopping point. The merge by founder
   + Flux roll + browser walk of `iogrid.org`, `/provider`, `/customer`,
   `/account`, `/vpn`, `/welcome`, `/admin.iogrid.org` is the stopping
   point — and even then, only the founder closes after seeing evidence.

2. **Every assistant message ends with a tool call.** "Status:",
   "Session totals:", "X PRs in flight:", or any closing recap is the
   stop signal. The TRACKER.md commit is the status update. The audit
   comment is the status update. The PR description is the status
   update. If I'm tempted to write a bulleted status block as the
   closing paragraph — kill the block, ship the next commit.

3. **TRACKER.md must be live.** Every PR pushed / issue audited /
   cluster op applied → `docs/ledger/TRACKER.md` row update + commit in
   the same session window. The cron at
   `/home/openova/bin/refresh-dod-dashboard.sh` is the safety net; my
   hand updates are the primary path. Founder 2026-05-23: *"is there no
   progress for the last 50 minutes? why isn't this changed?"* —
   because I shipped 2 PRs + 1 audit without touching TRACKER.

4. **No ScheduleWakeup for CI polling.** CI re-runs on push; check
   inline at natural checkpoints, never via `ScheduleWakeup
   delaySeconds=240`. Founder 2026-05-23: *"who allowed you to stupidly
   estimate build times and allowed you to wait for minutes?"*. The
   answer is: nobody — it's a banned pattern.

5. **The cycle is**: pull next concrete piece from backlog →
   implement → push commit → update TRACKER.md → audit one stale issue
   if backlog momentarily thin → repeat. The cycle has NO summarize
   step. If I think I'm done, the answer is one of:
   - Another stale issue to audit (>50 still open at session start)
   - A test/coverage gap on a PR I just shipped
   - A follow-up commit that closes a piece of a multi-PR chain
   - The next concrete inline fix
   Never: "Status: …".

## Sub-agent cap for this project

Default (per user-global) unless project owner overrides here.
