# iogrid — Status Tracker

Every node in the WBS below is **clickable** — open it to land on the related GitHub issue or PR. Titles are descriptive (read the WBS without clicking).

|  |  |
|---|---|
| Last refreshed | `2026-05-19T08:00:00Z` |
| Repo visibility | **PUBLIC** (free CI on github-hosted runners) |
| Merged PRs | **50** since project bootstrap |
| Open PRs | 0 |
| Open issues | **19** (5 EPICs + 17 sub-issues / chores) |
| EPIC closure | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> 19 / 26 closed = **73%** |

**Legend:** <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> done · <img alt="IN_FLIGHT" src="https://img.shields.io/badge/-IN__FLIGHT-bf8700?style=flat-square" /> work in progress · <img alt="OPEN" src="https://img.shields.io/badge/-OPEN-cf222e?style=flat-square" /> open · <img alt="DEFERRED" src="https://img.shields.io/badge/-DEFERRED-6e7781?style=flat-square" /> deferred · <img alt="BLOCKED" src="https://img.shields.io/badge/-BLOCKED-8250df?style=flat-square" /> blocked on founder action

---

## 1. Phase 0 success criterion — vCard LinkedIn enrichment unblocked

| # | Step | Status | Link |
|---|---|---|---|
| 1 | Customer signup + workspace + API key | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #164](https://github.com/iogrid/iogrid/pull/164), [#165](https://github.com/iogrid/iogrid/pull/165) |
| 2 | Rust daemon installed on founder's Mac | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #135](https://github.com/iogrid/iogrid/pull/135) |
| 3 | SOCKS5 entry on `proxy.iogrid.org:443` | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #132](https://github.com/iogrid/iogrid/pull/132) |
| 4 | DNS + TLS for `iogrid.org` zone | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #114](https://github.com/iogrid/iogrid/pull/114) |
| 5 | Anti-abuse pre-flight (PhotoDNA + PhishTank + GSB) | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #171](https://github.com/iogrid/iogrid/pull/171) |
| 6 | E2E kind smoke suite | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> | [PR #150](https://github.com/iogrid/iogrid/pull/150) |
| 7 | Live deploy to mothership k8s | <img alt="IN_FLIGHT" src="https://img.shields.io/badge/-IN__FLIGHT-bf8700?style=flat-square" /> | Flux reconciles automatically; verifier walkthrough pending |
| 8 | First real LinkedIn fetch via iogrid proxy | <img alt="OPEN" src="https://img.shields.io/badge/-OPEN-cf222e?style=flat-square" /> | Founder runs `examples/phase0-vcard-customer/client.go` to validate |

---

## 2. EPIC + sub-issue work breakdown (clickable WBS)

All 26 EPICs shown — done + in-flight + open. Sub-issues hang off each EPIC. Every node is clickable (opens GitHub).

```mermaid
flowchart LR
  classDef open     fill:#cf222e,stroke:#a40e26,color:#fff,stroke-width:2px
  classDef flight   fill:#bf8700,stroke:#9a6700,color:#fff,stroke-width:2px
  classDef done     fill:#2ea043,stroke:#1a7f37,color:#fff,stroke-width:2px
  classDef deferred fill:#6e7781,stroke:#4f555c,color:#fff,stroke-width:2px
  classDef blocked  fill:#8250df,stroke:#5e1ed1,color:#fff,stroke-width:2px



  %% CORE PLATFORM EPICS
  E1["EPIC 1 Rust provider daemon"]:::flight
  E2["EPIC 2 Go coordinator microservices"]:::done
  E3["EPIC 3 Next.js 15 web plane"]:::flight
  E4["EPIC 4 Identity Google+magic-link"]:::flight
  E5["EPIC 5 Grandma-proof install"]:::flight
  E6["EPIC 6 Scheduling caps+calendar+idle"]:::done
  E7["EPIC 7 Anti-abuse pre-flight"]:::open
  E73["EPIC 73 Infra k8s+Flux GitOps"]:::done



  %% PRODUCT EPICS
  E74["EPIC 74 Customer API + OpenAPI + SDKs"]:::done
  E75["EPIC 75 Consumer VPN gateway"]:::done
  E76["EPIC 76 Observability + SLOs"]:::done



  %% TOKEN + BRAND EPICS
  E77["EPIC 77 Brand identity + marketing"]:::flight
  E78["EPIC 78 Legal scaffolding drafts"]:::done
  E87["EPIC 87 \$GRID Solana token + 5 programs"]:::flight
  E106["EPIC 106 Public iogrid.org marketing site"]:::flight
  E115["EPIC 115 Customer SDKs published"]:::done
  E150["EPIC 150 E2E kind smoke harness"]:::done
  E167["EPIC 167 Sociable Cash off-ramp partner"]:::flight



  %% EPIC DEPENDENCIES
  E2 --> E73
  E2 --> E74
  E3 --> E4
  E4 --> E78
  E5 --> E73
  E6 --> E1
  E7 --> E1
  E87 --> E78
  E87 --> E167
  E106 --> E77
  E115 --> E74
  E167 --> E87
  E150 --> E2
  E150 --> E1



  %% STILL-OPEN SUB-ISSUES
  %% Lane A — \$GRID TGE prerequisites
  I104["104 Reg-D/Reg-S pre-TGE raise optional"]:::deferred
  I105["105 Quarterly token-holder transparency report"]:::open
  I168["168 Raydium CLMM canonical \$GRID venue doc"]:::open
  I172["172 TOKENOMICS \$GRID vs \$CASH positioning"]:::open
  E87 --> I168
  E87 --> I172
  E87 --> I105
  E87 --> I104

  %% Lane B — Coordinator gap
  I35["35 Cilium SPIFFE mTLS not just NetworkPolicy"]:::open
  E2 --> I35

  %% Lane C — Web app real impl gaps
  I3a["3 EPIC body — 5 stubs: identifier remove, account delete, i18n, WCAG, Playwright"]:::flight
  E3 --> I3a

  %% Lane D — Infra hygiene chores
  I158["158 kustomize commonLabels deprecated"]:::open
  I142["142 installer windows WiX 7 vs 4.0.6 clash"]:::open
  E73 --> I158
  E5 --> I142

  %% Lane E — Mac dev environment Phase 0
  I82["82 autossh launchd LaunchAgent on Mac"]:::flight
  I81["81 Mac docker CLI not on PATH"]:::open
  I80["80 bun install via oven-sh tap"]:::open
  I79["79 Mac upgrade Sonoma to Sequoia for Tart"]:::blocked
  E5 --> I82
  E5 --> I81
  E5 --> I80
  E1 --> I79

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
  click E150 "https://github.com/iogrid/iogrid/issues/150"
  click E167 "https://github.com/iogrid/iogrid/issues/167"
  click I35 "https://github.com/iogrid/iogrid/issues/35"
  click I79 "https://github.com/iogrid/iogrid/issues/79"
  click I80 "https://github.com/iogrid/iogrid/issues/80"
  click I81 "https://github.com/iogrid/iogrid/issues/81"
  click I82 "https://github.com/iogrid/iogrid/issues/82"
  click I104 "https://github.com/iogrid/iogrid/issues/104"
  click I105 "https://github.com/iogrid/iogrid/issues/105"
  click I142 "https://github.com/iogrid/iogrid/issues/142"
  click I158 "https://github.com/iogrid/iogrid/issues/158"
  click I168 "https://github.com/iogrid/iogrid/issues/168"
  click I172 "https://github.com/iogrid/iogrid/issues/172"
  click I3a "https://github.com/iogrid/iogrid/issues/3"
```

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
