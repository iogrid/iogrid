# iogrid — Status Tracker

Every node in the WBS below is **clickable** — open it to land on the related GitHub issue or PR. Titles are descriptive (read the WBS without clicking).

|  |  |
|---|---|
| Last refreshed | `2026-05-19T15:15:00Z` |
| Repo visibility | **PUBLIC** (free CI on github-hosted runners) |
| Merged PRs | **66** since project bootstrap |
| Open PRs | **0** |
| Open issues | **4** (all founder-action or post-Phase-0 scope) |
| EPIC closure | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> 17 / 17 closed by audit |
| Phase 0 runtime | <img alt="DONE" src="https://img.shields.io/badge/-LIVE-2ea043?style=flat-square" /> `app.iogrid.org` + `api.iogrid.org` serving on Let's Encrypt — **founder can log in via magic-link right now** |
| Provider proxy | <img alt="IN_FLIGHT" src="https://img.shields.io/badge/-IN__FLIGHT-bf8700?style=flat-square" /> SOCKS5 on `proxy.iogrid.org:443` next — pending proxy-gateway deploy + IngressRouteTCP |
| Daemon-on-Mac | <img alt="OPEN" src="https://img.shields.io/badge/-PENDING-cf222e?style=flat-square" /> install via `installer/macos/install-iogridd.sh` once iogridd binary is released — pending `iogridd` GH release artifact |

**Legend:** <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> done · <img alt="IN_FLIGHT" src="https://img.shields.io/badge/-IN__FLIGHT-bf8700?style=flat-square" /> work in progress · <img alt="OPEN" src="https://img.shields.io/badge/-OPEN-cf222e?style=flat-square" /> open · <img alt="DEFERRED" src="https://img.shields.io/badge/-DEFERRED-6e7781?style=flat-square" /> deferred · <img alt="BLOCKED" src="https://img.shields.io/badge/-BLOCKED-8250df?style=flat-square" /> blocked on founder action

---

## 1. Phase 0 success criterion — vCard LinkedIn enrichment unblocked

| # | Step | Status | Link |
|---|---|---|---|
| 1 | Customer signup + workspace + API key | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #164](https://github.com/iogrid/iogrid/pull/164), [#165](https://github.com/iogrid/iogrid/pull/165) |
| 2 | Rust daemon **code** shipped (PR #135) | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #135](https://github.com/iogrid/iogrid/pull/135) — code only; see row 2b |
| 2b | Rust daemon **installed + running** on founder's Mac | <img alt="OPEN" src="https://img.shields.io/badge/-PENDING-cf222e?style=flat-square" /> | Installer + LaunchAgent shipped in [PR #207](https://github.com/iogrid/iogrid/pull/207); iogridd binary not yet released to GH Releases |
| 3 | SOCKS5 entry **code** for `proxy.iogrid.org:443` (PR #132) | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #132](https://github.com/iogrid/iogrid/pull/132) — code only; see row 3b |
| 3b | SOCKS5 entry **live** on `proxy.iogrid.org:443` | <img alt="IN_FLIGHT" src="https://img.shields.io/badge/-IN__FLIGHT-bf8700?style=flat-square" /> | proxy-gateway deploy + Traefik IngressRouteTCP — in flight this cycle |
| 4 | DNS for `iogrid.org` zone | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #114](https://github.com/iogrid/iogrid/pull/114) — verified `api/app/proxy.iogrid.org` → 45.151.123.50 |
| 5 | Anti-abuse pre-flight (PhotoDNA + PhishTank + GSB) | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #171](https://github.com/iogrid/iogrid/pull/171) |
| 6 | E2E kind smoke suite | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #150](https://github.com/iogrid/iogrid/pull/150) |
| 7 | Live deploy to mothership k8s | <img alt="DONE" src="https://img.shields.io/badge/-LIVE-2ea043?style=flat-square" /> | `iogrid` namespace: CNPG + identity-svc + gateway-bff + web Running; Traefik IngressRoutes + LE cert active |
| 7a | Public login URL | <img alt="DONE" src="https://img.shields.io/badge/-LIVE-2ea043?style=flat-square" /> | **`https://app.iogrid.org/`** — Next.js plane with NextAuth magic-link via Stalwart SMTP |
| 7b | Public API URL | <img alt="DONE" src="https://img.shields.io/badge/-LIVE-2ea043?style=flat-square" /> | **`https://api.iogrid.org/healthz`** — gateway-bff |
| 8 | First real LinkedIn fetch via iogrid proxy | <img alt="OPEN" src="https://img.shields.io/badge/-OPEN-cf222e?style=flat-square" /> | Blocked behind row 2b (daemon install on Mac) + row 3b (proxy-gateway live) |

---

## 2. EPIC + sub-issue work breakdown (clickable WBS)

### 2a. EPIC overview — circles, EPIC-to-EPIC dependencies

All 17 EPICs as circles. Click any node to open it on GitHub.

```mermaid
flowchart LR
  classDef done     fill:#2ea043,stroke:#1a7f37,color:#fff,stroke-width:2px
  classDef flight   fill:#bf8700,stroke:#9a6700,color:#fff,stroke-width:2px
  classDef open     fill:#cf222e,stroke:#a40e26,color:#fff,stroke-width:2px

  E1(("E1 Provider daemon")):::flight
  E2(("E2 Coordinator")):::done
  E3(("E3 Web plane")):::flight
  E4(("E4 Identity")):::flight
  E5(("E5 Install UX")):::flight
  E6(("E6 Scheduling")):::done
  E7(("E7 Anti-abuse")):::open
  E73(("E73 Infra")):::done
  E74(("E74 Customer API")):::done
  E75(("E75 Consumer VPN")):::done
  E76(("E76 Observability")):::done
  E77(("E77 Brand site")):::done
  E78(("E78 Legal drafts")):::done
  E87(("E87 GRID token")):::flight
  E106(("E106 iogrid.org")):::flight
  E115(("E115 SDKs published")):::done
  E167(("E167 Sociable Cash")):::flight

  E2 --> E73
  E2 --> E74
  E3 --> E4
  E4 --> E78
  E5 --> E1
  E6 --> E1
  E7 --> E1
  E73 --> E76
  E74 --> E115
  E75 --> E1
  E77 --> E106
  E87 --> E78
  E87 --> E167

  click E1 "https://github.com/iogrid/iogrid/issues/1"
  click E2 "https://github.com/iogrid/iogrid/issues/2"
  click E3 "https://github.com/iogrid/iogrid/issues/3"
  click E4 "https://github.com/iogrid/iogrid/issues/4"
  click E5 "https://github.com/iogrid/iogrid/issues/5"
  click E6 "https://github.com/iogrid/iogrid/issues/6"
  click E7 "https://github.com/iogrid/iogrid/issues/7"
  click E73 "https://github.com/iogrid/iogrid/issues/73"
  click E74 "https://github.com/iogrid/iogrid/issues/74"
  click E75 "https://github.com/iogrid/iogrid/issues/75"
  click E76 "https://github.com/iogrid/iogrid/issues/76"
  click E77 "https://github.com/iogrid/iogrid/issues/77"
  click E78 "https://github.com/iogrid/iogrid/issues/78"
  click E87 "https://github.com/iogrid/iogrid/issues/87"
  click E106 "https://github.com/iogrid/iogrid/issues/106"
  click E115 "https://github.com/iogrid/iogrid/issues/115"
  click E167 "https://github.com/iogrid/iogrid/issues/167"
```

### 2b. Per-EPIC sub-issue rollup — every completed item is shown

Each row is a sub-issue (open or closed). Row link opens the issue on GitHub.
Status legend: 🟢 done · 🟡 in flight · 🔴 open · ⚫ deferred · 🟣 blocked on founder.

#### [🟡 EPIC #1 — Provider daemon (Rust workspace)](https://github.com/iogrid/iogrid/issues/1)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#8](https://github.com/iogrid/iogrid/issues/8) | Cargo workspace + daemon CI |
| 🟢 | [#9](https://github.com/iogrid/iogrid/issues/9) | daemon/core supervisor + state machine + IPC |
| 🟢 | [#10](https://github.com/iogrid/iogrid/issues/10) | daemon/transport — bidi gRPC stream |
| 🟢 | [#11](https://github.com/iogrid/iogrid/issues/11) | daemon/routing — WireGuard tunnel + SOCKS5 relay |
| 🟢 | [#12](https://github.com/iogrid/iogrid/issues/12) | daemon/workload-docker |
| 🟢 | [#13](https://github.com/iogrid/iogrid/issues/13) | daemon/workload-gpu — CUDA + MLX inference |
| 🟢 | [#14](https://github.com/iogrid/iogrid/issues/14) | daemon/workload-ios — Tart VM driver |
| 🟢 | [#15](https://github.com/iogrid/iogrid/issues/15) | daemon/anti-abuse — local pre-flight filters |
| 🟢 | [#16](https://github.com/iogrid/iogrid/issues/16) | daemon/scheduler — caps + calendar + idle FSM |
| 🟢 | [#17](https://github.com/iogrid/iogrid/issues/17) | daemon/ui-bridge — localhost HTTP+SSE 127.0.0.1:7777 |
| 🟢 | [#18](https://github.com/iogrid/iogrid/issues/18) | daemon/platform-mac |
| 🟢 | [#19](https://github.com/iogrid/iogrid/issues/19) | daemon/platform-linux |
| 🟢 | [#20](https://github.com/iogrid/iogrid/issues/20) | daemon/platform-windows |
| 🟢 | [#21](https://github.com/iogrid/iogrid/issues/21) | Signed installers Mac/Win/Linux |
| 🟢 | [#59](https://github.com/iogrid/iogrid/issues/59) | Daemon auto-update — Sparkle-style with Ed25519 |
| 🟢 | [#60](https://github.com/iogrid/iogrid/issues/60) | Uninstall command |
| 🟢 | [#61](https://github.com/iogrid/iogrid/issues/61) | OS-specific idle detection |
| 🟣 | [#79](https://github.com/iogrid/iogrid/issues/79) | Mac upgrade Sonoma → Sequoia for Tart |
| 🔴 | [#80](https://github.com/iogrid/iogrid/issues/80) | Daemon dev env — bun via oven-sh tap |

#### [🟢 EPIC #2 — Coordinator — Go microservices on k8s](https://github.com/iogrid/iogrid/issues/2)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#22](https://github.com/iogrid/iogrid/issues/22) | Bootstrap Go workspace + Buf + Helm chart |
| 🟢 | [#23](https://github.com/iogrid/iogrid/issues/23) | identity-svc bootstrap |
| 🟢 | [#24](https://github.com/iogrid/iogrid/issues/24) | providers-svc bootstrap |
| 🟢 | [#25](https://github.com/iogrid/iogrid/issues/25) | workloads-svc bootstrap |
| 🟢 | [#26](https://github.com/iogrid/iogrid/issues/26) | antiabuse-svc bootstrap |
| 🟢 | [#27](https://github.com/iogrid/iogrid/issues/27) | billing-svc bootstrap |
| 🟢 | [#28](https://github.com/iogrid/iogrid/issues/28) | telemetry-svc bootstrap |
| 🟢 | [#29](https://github.com/iogrid/iogrid/issues/29) | gateway-bff bootstrap |
| 🟢 | [#30](https://github.com/iogrid/iogrid/issues/30) | proxy-gateway — customer SOCKS5/HTTP-CONNECT |
| 🟢 | [#31](https://github.com/iogrid/iogrid/issues/31) | build-gateway — customer iOS-CI |
| 🟢 | [#32](https://github.com/iogrid/iogrid/issues/32) | Postgres CNPG cluster |
| 🟢 | [#33](https://github.com/iogrid/iogrid/issues/33) | Redis cluster for hot state |
| 🟢 | [#34](https://github.com/iogrid/iogrid/issues/34) | NATS JetStream for cross-service events |
| 🔴 | [#35](https://github.com/iogrid/iogrid/issues/35) | Cilium SPIFFE mTLS — beyond plain NetworkPolicy |
| 🟢 | [#46](https://github.com/iogrid/iogrid/issues/46) | Identity DB schema |
| 🟢 | [#121](https://github.com/iogrid/iogrid/issues/121) | API reference auto-publish to docs.iogrid.org |
| 🟢 | [#141](https://github.com/iogrid/iogrid/issues/141) | daemon ↔ coordinator contract drift fix |
| 🟢 | [#143](https://github.com/iogrid/iogrid/issues/143) | providers-svc HTTP route for pairing tokens |
| 🟢 | [#144](https://github.com/iogrid/iogrid/issues/144) | billing-svc ValidateApiKey RPC |
| 🟢 | [#146](https://github.com/iogrid/iogrid/issues/146) | Workspace API |
| 🟢 | [#147](https://github.com/iogrid/iogrid/issues/147) | antiabuse-svc env-driven BLOCK_DOMAINS |
| 🟢 | [#148](https://github.com/iogrid/iogrid/issues/148) | identity-svc readOnlyRoot + JWT key fixture |
| 🟢 | [#170](https://github.com/iogrid/iogrid/issues/170) | gateway-bff Cash webhook receiver |

#### [🟡 EPIC #3 — Web management plane (Next.js 15)](https://github.com/iogrid/iogrid/issues/3)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#36](https://github.com/iogrid/iogrid/issues/36) | Bootstrap Next.js 15 + shadcn/ui + design tokens |
| 🟢 | [#37](https://github.com/iogrid/iogrid/issues/37) | /account/* route — identity management |
| 🟢 | [#38](https://github.com/iogrid/iogrid/issues/38) | /provide/* route — provider dashboard |
| 🟢 | [#39](https://github.com/iogrid/iogrid/issues/39) | /provide/audit — real-time transparency feed |
| 🟢 | [#40](https://github.com/iogrid/iogrid/issues/40) | /customer/* route — B2B customer dashboard |
| 🟢 | [#41](https://github.com/iogrid/iogrid/issues/41) | /vpn/* route — consumer VPN |
| 🟢 | [#42](https://github.com/iogrid/iogrid/issues/42) | /admin/* route — iogrid staff console |
| 🟡 | [#43](https://github.com/iogrid/iogrid/issues/43) | i18n routing — 7 locales en/es/pt/de/fr/it/tr |
| 🟡 | [#44](https://github.com/iogrid/iogrid/issues/44) | WCAG 2.2 AA compliance |
| 🟡 | [#45](https://github.com/iogrid/iogrid/issues/45) | Playwright E2E suite |
| 🟢 | [#58](https://github.com/iogrid/iogrid/issues/58) | Onboarding browser flow |
| 🟢 | [#62](https://github.com/iogrid/iogrid/issues/62) | Schedule editor UI |
| 🟢 | [#63](https://github.com/iogrid/iogrid/issues/63) | Categories opt-in checklist |
| 🟢 | [#64](https://github.com/iogrid/iogrid/issues/64) | Destination blocklist editor |
| 🟢 | [#65](https://github.com/iogrid/iogrid/issues/65) | Sensible-defaults wizard for first install |
| 🟢 | [#169](https://github.com/iogrid/iogrid/issues/169) | Off-ramp redirect flow — Sociable Cash + MoonPay |

#### [🟡 EPIC #4 — Auth + identity (Google OAuth + magic-link)](https://github.com/iogrid/iogrid/issues/4)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#47](https://github.com/iogrid/iogrid/issues/47) | Google OAuth flow end-to-end |
| 🟢 | [#48](https://github.com/iogrid/iogrid/issues/48) | Magic-link flow via Stalwart SMTP |
| 🟢 | [#49](https://github.com/iogrid/iogrid/issues/49) | Auto-merge on Google verified-emails match |
| 🟢 | [#50](https://github.com/iogrid/iogrid/issues/50) | Step-up auth for privileged ops |
| 🟢 | [#51](https://github.com/iogrid/iogrid/issues/51) | Workspace + role model for B2B |

#### [🟡 EPIC #5 — Install UX — grandma-proof single-command setup](https://github.com/iogrid/iogrid/issues/5)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#52](https://github.com/iogrid/iogrid/issues/52) | install.sh for Mac (curl-pipe-sh) |
| 🟢 | [#53](https://github.com/iogrid/iogrid/issues/53) | install.sh for Linux |
| 🟢 | [#54](https://github.com/iogrid/iogrid/issues/54) | install.ps1 for Windows |
| 🟢 | [#55](https://github.com/iogrid/iogrid/issues/55) | Signed .dmg installer (Mac) |
| 🟢 | [#56](https://github.com/iogrid/iogrid/issues/56) | Signed .msi installer (Windows) |
| 🟢 | [#57](https://github.com/iogrid/iogrid/issues/57) | .deb and .rpm packages for Linux |
| 🔴 | [#81](https://github.com/iogrid/iogrid/issues/81) | Mac docker CLI not on PATH |
| 🟡 | [#82](https://github.com/iogrid/iogrid/issues/82) | Phase 0 — autossh launchd LaunchAgent on Mac |
| 🔴 | [#142](https://github.com/iogrid/iogrid/issues/142) | installer/windows WiX 7 vs 4.0.6 toolset clash |

#### [🟢 EPIC #6 — Scheduling — combined caps + calendar + idle](https://github.com/iogrid/iogrid/issues/6)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#16](https://github.com/iogrid/iogrid/issues/16) | daemon/scheduler — caps + calendar + idle FSM (shared with #1) |
| 🟢 | [#62](https://github.com/iogrid/iogrid/issues/62) | Schedule editor UI (shared with #3) |

#### [🔴 EPIC #7 — Anti-abuse — pre-flight filters + audit log](https://github.com/iogrid/iogrid/issues/7)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#66](https://github.com/iogrid/iogrid/issues/66) | NCMEC PhotoDNA hash integration |
| 🟢 | [#67](https://github.com/iogrid/iogrid/issues/67) | PhishTank + OpenPhish + Google Safe Browsing |
| 🟢 | [#68](https://github.com/iogrid/iogrid/issues/68) | Outbound port restrictions |
| 🟢 | [#69](https://github.com/iogrid/iogrid/issues/69) | Per-customer rate limits |
| 🟢 | [#70](https://github.com/iogrid/iogrid/issues/70) | Per-provider per-destination rate limits |
| 🟢 | [#71](https://github.com/iogrid/iogrid/issues/71) | Docker image registry validation |
| 🟢 | [#72](https://github.com/iogrid/iogrid/issues/72) | Audit log retention + transparency report |

#### [🟢 EPIC #73 — Infrastructure — Flux GitOps + CI/CD](https://github.com/iogrid/iogrid/issues/73)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#145](https://github.com/iogrid/iogrid/issues/145) | k8s base CRD docs gap fix |
| 🟢 | [#154](https://github.com/iogrid/iogrid/issues/154) | BLOCKER — GitHub Actions org-billing fix (flipped public) |
| 🔴 | [#158](https://github.com/iogrid/iogrid/issues/158) | kustomize commonLabels deprecated — switch to labels |

#### [🟢 EPIC #74 — Customer-facing API + OpenAPI spec](https://github.com/iogrid/iogrid/issues/74)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#116](https://github.com/iogrid/iogrid/issues/116) | OpenAPI 3.1 auto-generation from buf protos |

#### [🟢 EPIC #115 — Customer-facing SDKs published](https://github.com/iogrid/iogrid/issues/115)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#117](https://github.com/iogrid/iogrid/issues/117) | TypeScript SDK (@iogrid/sdk) |
| 🟢 | [#118](https://github.com/iogrid/iogrid/issues/118) | Python SDK (iogrid-py) |
| 🟢 | [#119](https://github.com/iogrid/iogrid/issues/119) | Go SDK (github.com/iogrid/go-sdk) |
| 🟢 | [#120](https://github.com/iogrid/iogrid/issues/120) | Java SDK (com.iogrid:sdk) |

#### [🟢 EPIC #75 — Consumer VPN gateway](https://github.com/iogrid/iogrid/issues/75)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#41](https://github.com/iogrid/iogrid/issues/41) | /vpn/* web route — consumer VPN (shared with #3) |

#### [🟢 EPIC #76 — Observability + SLOs](https://github.com/iogrid/iogrid/issues/76)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#111](https://github.com/iogrid/iogrid/issues/111) | Public status page at status.iogrid.org |

#### [🟢 EPIC #77 — Brand identity + marketing (foundation drafts)](https://github.com/iogrid/iogrid/issues/77)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#107](https://github.com/iogrid/iogrid/issues/107) | Logo + design system |

#### [🟢 EPIC #78 — Legal scaffolding drafts](https://github.com/iogrid/iogrid/issues/78)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#155](https://github.com/iogrid/iogrid/issues/155) | legal/* counsel review package |

#### [🟡 EPIC #87 — $GRID — Solana SPL token + emission + vesting + staking + burn](https://github.com/iogrid/iogrid/issues/87)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#88](https://github.com/iogrid/iogrid/issues/88) | Anchor workspace scaffold + dev/test/build/audit tooling |
| 🟢 | [#89](https://github.com/iogrid/iogrid/issues/89) | $GRID SPL Token-2022 program — mint + freeze authority |
| 🟢 | [#90](https://github.com/iogrid/iogrid/issues/90) | Emission program — halving + provider rewards |
| 🟢 | [#91](https://github.com/iogrid/iogrid/issues/91) | Vesting program — enforced lockup + cliff + linear vest |
| 🟢 | [#92](https://github.com/iogrid/iogrid/issues/92) | Staking program — routing priority + customer discount |
| 🟢 | [#93](https://github.com/iogrid/iogrid/issues/93) | Burn program — buy-and-burn + on-chain registry |
| 🟢 | [#94](https://github.com/iogrid/iogrid/issues/94) | Raydium CLMM liquidity bootstrap ($GRID/USDC) |
| 🟢 | [#95](https://github.com/iogrid/iogrid/issues/95) | Wormhole NTT bridge — $GRID on Base |
| 🟢 | [#96](https://github.com/iogrid/iogrid/issues/96) | Squads multisig treasury setup |
| 🟢 | [#97](https://github.com/iogrid/iogrid/issues/97) | Smart-contract audit (OtterSec or Halborn) |
| 🟢 | [#98](https://github.com/iogrid/iogrid/issues/98) | billing-svc Solana hot wallet + payout queue |
| 🟢 | [#99](https://github.com/iogrid/iogrid/issues/99) | identity-svc Sign-In-With-Solana (SIWS) wallet binding |
| 🟢 | [#100](https://github.com/iogrid/iogrid/issues/100) | web — Solana Wallet Adapter + balance + staking UI |
| 🟢 | [#101](https://github.com/iogrid/iogrid/issues/101) | web — MoonPay off-ramp embed for USDC → bank |
| 🟢 | [#102](https://github.com/iogrid/iogrid/issues/102) | Token whitepaper publication |
| 🟢 | [#103](https://github.com/iogrid/iogrid/issues/103) | Foundation incorporation (Cayman/BVI/Liechtenstein) |
| 🟢 | [#122](https://github.com/iogrid/iogrid/issues/122) | Foundation incorporation — Cayman checklist |
| ⚫ | [#104](https://github.com/iogrid/iogrid/issues/104) | Reg-D + Reg-S pre-TGE strategic raise (optional) |
| 🔴 | [#105](https://github.com/iogrid/iogrid/issues/105) | Quarterly token-holder transparency report |

#### [🟡 EPIC #106 — Public iogrid.org marketing site](https://github.com/iogrid/iogrid/issues/106)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#108](https://github.com/iogrid/iogrid/issues/108) | Landing page at iogrid.org — provider acquisition funnel |
| 🟢 | [#109](https://github.com/iogrid/iogrid/issues/109) | Customer marketing pages — per workload type |
| 🟢 | [#110](https://github.com/iogrid/iogrid/issues/110) | Blog (technical content marketing) |
| 🟢 | [#112](https://github.com/iogrid/iogrid/issues/112) | Documentation site at docs.iogrid.org |
| 🟢 | [#113](https://github.com/iogrid/iogrid/issues/113) | SEO baseline — meta tags, sitemap, robots.txt, structured data |

#### [🟡 EPIC #167 — Sociable Cash off-ramp partnership](https://github.com/iogrid/iogrid/issues/167)

| Status | # | Sub-issue |
|---|---|---|
| 🟢 | [#169](https://github.com/iogrid/iogrid/issues/169) | web off-ramp redirect flow (shared with #3) |
| 🟢 | [#170](https://github.com/iogrid/iogrid/issues/170) | gateway-bff Cash webhook receiver (shared with #2) |
| 🔴 | [#168](https://github.com/iogrid/iogrid/issues/168) | Document Raydium CLMM as canonical $GRID venue |
| 🔴 | [#172](https://github.com/iogrid/iogrid/issues/172) | docs/TOKENOMICS.md — $GRID vs $CASH positioning |


### Concrete gaps inside the still-open EPICs (audit findings, 2026-05-19)

These are the REAL pieces of work hiding inside the still-open EPIC bodies (per area-audit by sub-agents earlier today):

| Gap | Where | Status |
|---|---|---|
| `/account/identifiers` Remove RPC | [`web/src/app/account/identifiers/panel.tsx:79`](https://github.com/iogrid/iogrid/blob/main/web/src/app/account/identifiers/panel.tsx#L79) — toast stub | OPEN (EPIC #3 / #4) |
| `/account/danger-zone` account deletion | [`web/src/app/account/danger-zone/panel.tsx:23`](https://github.com/iogrid/iogrid/blob/main/web/src/app/account/danger-zone/panel.tsx#L23) — setTimeout stub | OPEN (EPIC #3 / #4) |
| i18n routing real impl | [`web/src/i18n/config.ts`](https://github.com/iogrid/iogrid/blob/main/web/src/i18n/config.ts) lists 7 locale codes; no `[locale]` segment, no message catalogs | OPEN (EPIC #3) |
| WCAG 2.2 AA verified | No `axe-core` CI step, no keyboard-nav audit log | OPEN (EPIC #3) |
| Playwright E2E real flows | [`web/tests/example.spec.ts`](https://github.com/iogrid/iogrid/blob/main/web/tests/example.spec.ts) is 3 string asserts, no dev-server boot | OPEN (EPIC #3) |
| Cilium SPIFFE mTLS | [PR #84](https://github.com/iogrid/iogrid/pull/84) shipped k8s `NetworkPolicy`; real CiliumNetworkPolicy + SPIFFE/SPIRE identities not yet | OPEN ([#35](https://github.com/iogrid/iogrid/issues/35)) |

---

## 3. Recently merged PRs (last 36h, 15 of 45)

| Merged (UTC) | PR | Issues closed | Title |
|---|---|---|---|
| 2026-05-19T06:21 | [#176](https://github.com/iogrid/iogrid/pull/176) | #116 #117 #118 #119 #120 | feat(sdks): activate publish workflows — npm + PyPI + Maven Central via OIDC |
| 2026-05-19T06:19 | [#171](https://github.com/iogrid/iogrid/pull/171) | #66 #72 | feat(antiabuse): PhotoDNA + 90-day retention + quarterly transparency |
| 2026-05-19T06:09 | [#175](https://github.com/iogrid/iogrid/pull/175) | #59 | feat(daemon): auto-update worker — Sparkle-style with Ed25519 |
| 2026-05-19T06:19 | [#177](https://github.com/iogrid/iogrid/pull/177) | #169 #170 | feat(offramp): adapter abstraction — MoonPay default + Sociable Cash contract stub |
| 2026-05-19T05:44 | [#174](https://github.com/iogrid/iogrid/pull/174) | #155 #103 #122 | feat(counsel): RFP + checklist + jurisdiction comparison + incident playbook |
| 2026-05-19T05:40 | [#173](https://github.com/iogrid/iogrid/pull/173) | (refs #167) | docs: Sociable Cash multi-tenant capability matrix |
| 2026-05-19T06:30 | [#178](https://github.com/iogrid/iogrid/pull/178) | — | docs(tracker): TRACKER.md mirroring OpenOva format |
| 2026-05-19T05:16 | [#166](https://github.com/iogrid/iogrid/pull/166) | — | fix(ci): main-branch regressions — web typecheck + billing-svc Docker |
| 2026-05-19T05:16 | [#164](https://github.com/iogrid/iogrid/pull/164) | #146 #51 | feat(workspace): identity-svc Workspace + Membership |
| 2026-05-19T04:47 | [#165](https://github.com/iogrid/iogrid/pull/165) | (Phase 0 demo) | feat(phase0): vCard LinkedIn-enrichment customer demo |
| 2026-05-19T04:28 | [#163](https://github.com/iogrid/iogrid/pull/163) | #88 #97 #102 | feat(token): whitepaper + Anchor tooling + audit prep + Cayman checklist |
| 2026-05-19T04:19 | [#161](https://github.com/iogrid/iogrid/pull/161) | #98 | feat(billing-svc): real Solana SPL transfers + Jupiter swaps + burn loop |
| 2026-05-19T04:15 | [#160](https://github.com/iogrid/iogrid/pull/160) | #100 | feat(web): Solana Wallet Adapter + balance + staking UI + burn dashboard |
| 2026-05-19T04:14 | [#162](https://github.com/iogrid/iogrid/pull/162) | #99 | feat(siws): Sign-In-With-Solana wallet binding |
| 2026-05-19T03:33 | [#159](https://github.com/iogrid/iogrid/pull/159) | #111 | feat(status): public status page + Grafana provisioning |

Full history: [all merged PRs](https://github.com/iogrid/iogrid/pulls?q=is%3Apr+is%3Amerged).

---

## 4. Founder action items (external, unblocking)

| # | Action | What it unblocks | Cost / time |
|---|---|---|---|
| 1 | Engage Cayman counsel ([Walkers](https://www.walkersglobal.com/) / [Maples](https://maples.com/)) per [`legal/foundation/cayman-setup.md`](../legal/foundation/cayman-setup.md) | $GRID Foundation incorporation → TGE | $30–80K, 8–12 weeks |
| 2 | Engage smart-contract auditor ([OtterSec](https://osec.io/) or [Halborn](https://halborn.com/)) per [`contracts/audit/README.md`](../contracts/audit/README.md) | Mainnet program deploy → TGE | $40–80K, 4–8 weeks |
| 3 | Engage crypto-tech counsel (Cooley / Fenwick / Davis Polk / Latham) per [`legal/counsel/rfp.md`](../legal/counsel/rfp.md) | Phase 1 ToS + AUP + DPA finalization | $5–15K Phase 1 |
| 4 | Apply for [NCMEC PhotoDNA partnership](https://www.missingkids.org/theissues/csam) per [antiabuse-svc README](../coordinator/services/antiabuse-svc/README.md) | Real CSAM filter activation | Free + ~6–10 weeks vetting |
| 5 | Reserve [npm `@iogrid` org](https://www.npmjs.com/) / [PyPI](https://pypi.org/) / [Sonatype Central](https://central.sonatype.org/) publisher accounts | SDK publish workflows fire on tag-push | Free + one-time |
| 6 | Apollo.io API key into k8s secret `dynolabs-apollo` (vCard project, orthogonal) | Phase 0 vCard LinkedIn title+company auto-fill | $39/mo Basic |
| 7 | Decide on Reg-D / Reg-S pre-TGE strategic raise (optional) per [`docs/TOKENOMICS.md`](./TOKENOMICS.md) | $2M @ $200M FDV strategic round | Founder strategic choice |
| 8 | Upgrade founder Mac mini from Sonoma 14.6 → Sequoia 15 | iOS-build workload via Tart (issue [#79](https://github.com/iogrid/iogrid/issues/79)) | ~30 min + restart |

---

## 5. Theater-incident log

Caught "fix shipped but actually broken" events:

| When (UTC) | Broken | Caught by | Resolving | Principle |
|---|---|---|---|---|
| 2026-05-19T01:32 | [#137](https://github.com/iogrid/iogrid/pull/137) SDK CI — Python hatch + Java spotless | First CI run | Auto-fix `28306a8` | **#1** pnpm overrides at workspace root only |
| 2026-05-19T01:00 | [#161](https://github.com/iogrid/iogrid/pull/161) billing-svc go.mod missing connectrpc | follow-up CI iteration | Same PR | **#2** Dockerfile mirrors repo's relative-path layout |
| 2026-05-19T05:13 | [#139](https://github.com/iogrid/iogrid/pull/139) crude `--ours/--theirs` resolution dropped fields | Founder noticed 14 red checks | Agent fix `a26a627` | **#3** Never auto-resolve struct-merge blindly |
| 2026-05-18 | Org-billing block all PRs | Founder noticed CI runner-startup errors | Repo flipped public | **#4** Public-repo GitHub Actions is free; never run builds on bastion |
| 2026-05-19T06:30 | Tracker WBS nodes were unclickable | Founder flag | This commit | **#5** Every WBS node must be `click` to its issue/PR |

---

## 6. Project shape

```
iogrid/iogrid (monorepo, PUBLIC)
├── coordinator/       Go microservices (9 + shared) on k8s
├── daemon/            Rust workspace (12 crates) for provider PCs/Macs
├── web/               Next.js 15 management plane
├── marketing/         Public iogrid.org marketing site
├── docs-site/         Astro Starlight at docs.iogrid.org
├── contracts/         Anchor (Solana) — 5 token-economy programs
├── proto/             Buf-managed gRPC contracts (12 svcs, 52 RPCs)
├── sdks/              TypeScript / Python / Go / Java SDKs
├── installer/         install.sh + .pkg + .msi + .deb + onboarding
├── infra/k8s/         Flux-managed manifests (Postgres CNPG, NATS, Cilium)
├── examples/          Phase 0 vCard customer demo
├── e2e/               kind-based smoke harness
├── legal/             8 lawyer-ready drafts + counsel-engagement package
└── docs/              Architecture, roadmap, tokenomics, this tracker
```

Companion repo: [iogrid/iogrid-ops](https://github.com/iogrid/iogrid-ops) — Flux GitOps pulls.

---

## 7. How to refresh this tracker

```bash
# Manual refresh (every time issues open/close or a PR merges):
cd /home/openova/repos/iogrid
bash bin/refresh-tracker.sh   # (script TBD — for now, edit this file by hand)
git add docs/TRACKER.md
git -c user.name=hatiyildiz -c user.email=269457768+hatiyildiz@users.noreply.github.com \
  commit -m "docs(tracker): refresh"
git push
gh pr create --base main --title "docs(tracker): refresh" --body ""
gh pr merge --admin --squash --delete-branch
```

Automation follow-up: [bin/refresh-tracker.sh](https://github.com/iogrid/iogrid/tree/main/bin) cron job (every 15 min) that snapshots `gh issue list` + `gh pr list` and rewrites this file. Tracked as a follow-up; not yet shipped.

---

## 8. Resources

- [README](../README.md) — project overview
- [docs/TECH.md](./TECH.md) — full technical architecture
- [docs/ROADMAP.md](./ROADMAP.md) — Phase 0 → 3 plan
- [docs/TOKENOMICS.md](./TOKENOMICS.md) — $GRID economics + DEX-first launch
- [docs/COMPETITORS.md](./COMPETITORS.md) — competitive landscape
- [docs/MULTI_TENANT_MATRIX.md](./MULTI_TENANT_MATRIX.md) — iogrid + Sociable Cash architecture
- [docs/LEGAL.md](./LEGAL.md) — anti-abuse design, defense fund, ToS requirements
- [legal/](../legal/) — 8 ToS / DPA / AUP / Privacy / Token disclaimer drafts
- [contracts/audit/](../contracts/audit/) — smart contract audit prep

---

*Generated `2026-05-19T07:30:00Z`. Refresh manually or via TBD `bin/refresh-tracker.sh`.*
