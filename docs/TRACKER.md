# iogrid — Status Tracker

Every node in the WBS below is **clickable** — open it to land on the related GitHub issue or PR. Titles are descriptive (read the WBS without clicking).

|  |  |
|---|---|
| Last refreshed | `2026-05-19T08:00:00Z` |
| Repo visibility | **PUBLIC** (free CI on github-hosted runners) |
| Merged PRs | **50** since project bootstrap |
| Open PRs | 0 |
| Open issues | **19** (8 EPICs + 11 sub-issues / chores) |
| EPIC closure | <img alt="DONE" src="https://img.shields.io/badge/-DONE-2ea043?style=flat-square" /> 9 / 17 closed = **53%** (+ 60+ sub-issues closed) |

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

All 17 EPICs shown as **circles** (done + in-flight + open). Every sub-issue ever opened against an EPIC — closed or open — hangs off it as a rectangle. Every node is clickable (opens GitHub).

```mermaid
flowchart LR
  classDef done     fill:#2ea043,stroke:#1a7f37,color:#fff,stroke-width:2px
  classDef flight   fill:#bf8700,stroke:#9a6700,color:#fff,stroke-width:2px
  classDef open     fill:#cf222e,stroke:#a40e26,color:#fff,stroke-width:2px
  classDef deferred fill:#6e7781,stroke:#4f555c,color:#fff,stroke-width:2px
  classDef blocked  fill:#8250df,stroke:#5e1ed1,color:#fff,stroke-width:2px

  %% EPICS as circles
  E1(("E1 Provider daemon")):::flight
  E2(("E2 Coordinator")):::done
  E3(("E3 Web plane")):::flight
  E4(("E4 Identity")):::flight
  E5(("E5 Install UX")):::flight
  E6(("E6 Scheduling")):::done
  E7(("E7 Anti-abuse")):::open
  E73(("E73 Infra k8s")):::done
  E74(("E74 Customer API")):::done
  E75(("E75 Consumer VPN")):::done
  E76(("E76 Observability")):::done
  E77(("E77 Brand site")):::done
  E78(("E78 Legal drafts")):::done
  E87(("E87 GRID token")):::flight
  E106(("E106 iogrid.org")):::flight
  E115(("E115 SDKs published")):::done
  E167(("E167 Sociable Cash")):::flight

  %% EPIC -> EPIC dependencies
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
  E115 --> E74

  %% EPIC 1 daemon children
  I8["8 Cargo workspace + CI"]:::done
  I9["9 daemon core supervisor"]:::done
  I10["10 daemon transport gRPC"]:::done
  I11["11 daemon routing WireGuard"]:::done
  I12["12 daemon workload-docker"]:::done
  I13["13 daemon workload-gpu"]:::done
  I14["14 daemon workload-ios Tart"]:::done
  I15["15 daemon anti-abuse local"]:::done
  I16["16 daemon scheduler"]:::done
  I17["17 daemon ui-bridge"]:::done
  I18["18 platform-mac"]:::done
  I19["19 platform-linux"]:::done
  I20["20 platform-windows"]:::done
  I21["21 Signed installers"]:::done
  I59["59 Daemon auto-update"]:::done
  I60["60 Uninstall command"]:::done
  I61["61 OS idle detection"]:::done
  I79["79 Mac Sonoma to Sequoia"]:::blocked
  I80["80 bun via oven-sh tap"]:::open
  E1 --> I8
  E1 --> I9
  E1 --> I10
  E1 --> I11
  E1 --> I12
  E1 --> I13
  E1 --> I14
  E1 --> I15
  E1 --> I16
  E1 --> I17
  E1 --> I18
  E1 --> I19
  E1 --> I20
  E1 --> I21
  E1 --> I59
  E1 --> I60
  E1 --> I61
  E1 --> I79
  E1 --> I80

  %% EPIC 2 coordinator children
  I22["22 Go workspace + Buf"]:::done
  I23["23 identity-svc bootstrap"]:::done
  I24["24 providers-svc bootstrap"]:::done
  I25["25 workloads-svc bootstrap"]:::done
  I26["26 antiabuse-svc bootstrap"]:::done
  I27["27 billing-svc bootstrap"]:::done
  I28["28 telemetry-svc bootstrap"]:::done
  I29["29 gateway-bff bootstrap"]:::done
  I30["30 proxy-gateway SOCKS5"]:::done
  I31["31 build-gateway iOS-CI"]:::done
  I32["32 Postgres CNPG"]:::done
  I33["33 Redis hot state"]:::done
  I34["34 NATS JetStream"]:::done
  I35["35 Cilium SPIFFE mTLS"]:::open
  I46["46 Identity DB schema"]:::done
  I121["121 API reference docs"]:::done
  I141["141 contract drift fix"]:::done
  I143["143 providers HTTP route"]:::done
  I144["144 ValidateApiKey RPC"]:::done
  I146["146 Workspace API"]:::done
  I147["147 BLOCK_DOMAINS env"]:::done
  I148["148 readOnlyRoot fix"]:::done
  I170["170 Cash webhook receiver"]:::done
  E2 --> I22
  E2 --> I23
  E2 --> I24
  E2 --> I25
  E2 --> I26
  E2 --> I27
  E2 --> I28
  E2 --> I29
  E2 --> I30
  E2 --> I31
  E2 --> I32
  E2 --> I33
  E2 --> I34
  E2 --> I35
  E2 --> I46
  E2 --> I121
  E2 --> I141
  E2 --> I143
  E2 --> I144
  E2 --> I146
  E2 --> I147
  E2 --> I148
  E2 --> I170

  %% EPIC 3 web children
  I36["36 Next.js 15 + shadcn"]:::done
  I37["37 /account route"]:::done
  I38["38 /provide route"]:::done
  I39["39 /provide/audit"]:::done
  I40["40 /customer route"]:::done
  I41["41 /vpn route"]:::done
  I42["42 /admin route"]:::done
  I43["43 i18n routing"]:::flight
  I44["44 WCAG 2.2 AA"]:::flight
  I45["45 Playwright E2E"]:::flight
  I58["58 Onboarding flow"]:::done
  I62["62 Schedule editor UI"]:::done
  I63["63 Categories opt-in"]:::done
  I64["64 Destination blocklist"]:::done
  I65["65 Sensible-defaults wizard"]:::done
  I169["169 Off-ramp redirect"]:::done
  E3 --> I36
  E3 --> I37
  E3 --> I38
  E3 --> I39
  E3 --> I40
  E3 --> I41
  E3 --> I42
  E3 --> I43
  E3 --> I44
  E3 --> I45
  E3 --> I58
  E3 --> I62
  E3 --> I63
  E3 --> I64
  E3 --> I65
  E3 --> I169

  %% EPIC 4 auth children
  I47["47 Google OAuth"]:::done
  I48["48 Magic-link via Stalwart"]:::done
  I49["49 Auto-merge verified"]:::done
  I50["50 Step-up auth"]:::done
  I51["51 Workspace + role B2B"]:::done
  E4 --> I47
  E4 --> I48
  E4 --> I49
  E4 --> I50
  E4 --> I51

  %% EPIC 5 install UX children
  I52["52 install.sh Mac"]:::done
  I53["53 install.sh Linux"]:::done
  I54["54 install.ps1 Windows"]:::done
  I55["55 .dmg Mac installer"]:::done
  I56["56 .msi Windows installer"]:::done
  I57["57 .deb and .rpm Linux"]:::done
  I81["81 docker CLI PATH"]:::open
  I82["82 autossh launchd Mac"]:::flight
  I142["142 WiX 7 vs 4.0.6 clash"]:::open
  E5 --> I52
  E5 --> I53
  E5 --> I54
  E5 --> I55
  E5 --> I56
  E5 --> I57
  E5 --> I81
  E5 --> I82
  E5 --> I142

  %% EPIC 7 anti-abuse children
  I66["66 NCMEC PhotoDNA"]:::done
  I67["67 PhishTank + GSB"]:::done
  I68["68 Outbound port limits"]:::done
  I69["69 Per-customer rate limits"]:::done
  I70["70 Per-provider rate limits"]:::done
  I71["71 Docker registry validate"]:::done
  I72["72 Audit log + transparency"]:::done
  E7 --> I66
  E7 --> I67
  E7 --> I68
  E7 --> I69
  E7 --> I70
  E7 --> I71
  E7 --> I72

  %% EPIC 73 infra children
  I145["145 k8s base CRD docs"]:::done
  I154["154 GH Actions billing fix"]:::done
  I158["158 kustomize commonLabels"]:::open
  E73 --> I145
  E73 --> I154
  E73 --> I158

  %% EPIC 74 + 115 SDK children
  I116["116 OpenAPI 3.1 from buf"]:::done
  I117["117 TypeScript SDK"]:::done
  I118["118 Python SDK"]:::done
  I119["119 Go SDK"]:::done
  I120["120 Java SDK"]:::done
  E74 --> I116
  E115 --> I117
  E115 --> I118
  E115 --> I119
  E115 --> I120

  %% EPIC 76 observability children
  I111["111 status.iogrid.org"]:::done
  E76 --> I111

  %% EPIC 77 brand + EPIC 106 marketing children
  I107["107 Logo + design system"]:::done
  I108["108 Landing page funnel"]:::done
  I109["109 Customer marketing pages"]:::done
  I110["110 Blog technical content"]:::done
  I112["112 docs.iogrid.org"]:::done
  I113["113 SEO baseline"]:::done
  E77 --> I107
  E106 --> I108
  E106 --> I109
  E106 --> I110
  E106 --> I112
  E106 --> I113

  %% EPIC 78 legal children
  I155["155 Counsel review pkg"]:::done
  E78 --> I155

  %% EPIC 87 GRID token children
  I88["88 Anchor scaffold + tooling"]:::done
  I89["89 SPL Token-2022 program"]:::done
  I90["90 Emission halving program"]:::done
  I91["91 Vesting cliff program"]:::done
  I92["92 Staking priority program"]:::done
  I93["93 Burn registry program"]:::done
  I94["94 Raydium CLMM bootstrap"]:::done
  I95["95 Wormhole NTT to Base"]:::done
  I96["96 Squads multisig treasury"]:::done
  I97["97 Smart-contract audit"]:::done
  I98["98 billing-svc Solana wallet"]:::done
  I99["99 SIWS wallet binding"]:::done
  I100["100 web Wallet Adapter"]:::done
  I101["101 MoonPay off-ramp embed"]:::done
  I102["102 Token whitepaper"]:::done
  I103["103 Foundation incorporation"]:::done
  I122["122 Cayman checklist"]:::done
  I104["104 Reg-D + Reg-S raise"]:::deferred
  I105["105 Quarterly transparency"]:::open
  E87 --> I88
  E87 --> I89
  E87 --> I90
  E87 --> I91
  E87 --> I92
  E87 --> I93
  E87 --> I94
  E87 --> I95
  E87 --> I96
  E87 --> I97
  E87 --> I98
  E87 --> I99
  E87 --> I100
  E87 --> I101
  E87 --> I102
  E87 --> I103
  E87 --> I104
  E87 --> I105
  E87 --> I122

  %% EPIC 167 Sociable Cash children
  I168["168 Raydium canonical doc"]:::open
  I172["172 GRID vs CASH positioning"]:::open
  E167 --> I168
  E167 --> I172

  %% Click directives EPICs
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

  %% Click directives sub-issues
  click I8 "https://github.com/iogrid/iogrid/issues/8"
  click I9 "https://github.com/iogrid/iogrid/issues/9"
  click I10 "https://github.com/iogrid/iogrid/issues/10"
  click I11 "https://github.com/iogrid/iogrid/issues/11"
  click I12 "https://github.com/iogrid/iogrid/issues/12"
  click I13 "https://github.com/iogrid/iogrid/issues/13"
  click I14 "https://github.com/iogrid/iogrid/issues/14"
  click I15 "https://github.com/iogrid/iogrid/issues/15"
  click I16 "https://github.com/iogrid/iogrid/issues/16"
  click I17 "https://github.com/iogrid/iogrid/issues/17"
  click I18 "https://github.com/iogrid/iogrid/issues/18"
  click I19 "https://github.com/iogrid/iogrid/issues/19"
  click I20 "https://github.com/iogrid/iogrid/issues/20"
  click I21 "https://github.com/iogrid/iogrid/issues/21"
  click I22 "https://github.com/iogrid/iogrid/issues/22"
  click I23 "https://github.com/iogrid/iogrid/issues/23"
  click I24 "https://github.com/iogrid/iogrid/issues/24"
  click I25 "https://github.com/iogrid/iogrid/issues/25"
  click I26 "https://github.com/iogrid/iogrid/issues/26"
  click I27 "https://github.com/iogrid/iogrid/issues/27"
  click I28 "https://github.com/iogrid/iogrid/issues/28"
  click I29 "https://github.com/iogrid/iogrid/issues/29"
  click I30 "https://github.com/iogrid/iogrid/issues/30"
  click I31 "https://github.com/iogrid/iogrid/issues/31"
  click I32 "https://github.com/iogrid/iogrid/issues/32"
  click I33 "https://github.com/iogrid/iogrid/issues/33"
  click I34 "https://github.com/iogrid/iogrid/issues/34"
  click I35 "https://github.com/iogrid/iogrid/issues/35"
  click I36 "https://github.com/iogrid/iogrid/issues/36"
  click I37 "https://github.com/iogrid/iogrid/issues/37"
  click I38 "https://github.com/iogrid/iogrid/issues/38"
  click I39 "https://github.com/iogrid/iogrid/issues/39"
  click I40 "https://github.com/iogrid/iogrid/issues/40"
  click I41 "https://github.com/iogrid/iogrid/issues/41"
  click I42 "https://github.com/iogrid/iogrid/issues/42"
  click I43 "https://github.com/iogrid/iogrid/issues/43"
  click I44 "https://github.com/iogrid/iogrid/issues/44"
  click I45 "https://github.com/iogrid/iogrid/issues/45"
  click I46 "https://github.com/iogrid/iogrid/issues/46"
  click I47 "https://github.com/iogrid/iogrid/issues/47"
  click I48 "https://github.com/iogrid/iogrid/issues/48"
  click I49 "https://github.com/iogrid/iogrid/issues/49"
  click I50 "https://github.com/iogrid/iogrid/issues/50"
  click I51 "https://github.com/iogrid/iogrid/issues/51"
  click I52 "https://github.com/iogrid/iogrid/issues/52"
  click I53 "https://github.com/iogrid/iogrid/issues/53"
  click I54 "https://github.com/iogrid/iogrid/issues/54"
  click I55 "https://github.com/iogrid/iogrid/issues/55"
  click I56 "https://github.com/iogrid/iogrid/issues/56"
  click I57 "https://github.com/iogrid/iogrid/issues/57"
  click I58 "https://github.com/iogrid/iogrid/issues/58"
  click I59 "https://github.com/iogrid/iogrid/issues/59"
  click I60 "https://github.com/iogrid/iogrid/issues/60"
  click I61 "https://github.com/iogrid/iogrid/issues/61"
  click I62 "https://github.com/iogrid/iogrid/issues/62"
  click I63 "https://github.com/iogrid/iogrid/issues/63"
  click I64 "https://github.com/iogrid/iogrid/issues/64"
  click I65 "https://github.com/iogrid/iogrid/issues/65"
  click I66 "https://github.com/iogrid/iogrid/issues/66"
  click I67 "https://github.com/iogrid/iogrid/issues/67"
  click I68 "https://github.com/iogrid/iogrid/issues/68"
  click I69 "https://github.com/iogrid/iogrid/issues/69"
  click I70 "https://github.com/iogrid/iogrid/issues/70"
  click I71 "https://github.com/iogrid/iogrid/issues/71"
  click I72 "https://github.com/iogrid/iogrid/issues/72"
  click I79 "https://github.com/iogrid/iogrid/issues/79"
  click I80 "https://github.com/iogrid/iogrid/issues/80"
  click I81 "https://github.com/iogrid/iogrid/issues/81"
  click I82 "https://github.com/iogrid/iogrid/issues/82"
  click I88 "https://github.com/iogrid/iogrid/issues/88"
  click I89 "https://github.com/iogrid/iogrid/issues/89"
  click I90 "https://github.com/iogrid/iogrid/issues/90"
  click I91 "https://github.com/iogrid/iogrid/issues/91"
  click I92 "https://github.com/iogrid/iogrid/issues/92"
  click I93 "https://github.com/iogrid/iogrid/issues/93"
  click I94 "https://github.com/iogrid/iogrid/issues/94"
  click I95 "https://github.com/iogrid/iogrid/issues/95"
  click I96 "https://github.com/iogrid/iogrid/issues/96"
  click I97 "https://github.com/iogrid/iogrid/issues/97"
  click I98 "https://github.com/iogrid/iogrid/issues/98"
  click I99 "https://github.com/iogrid/iogrid/issues/99"
  click I100 "https://github.com/iogrid/iogrid/issues/100"
  click I101 "https://github.com/iogrid/iogrid/issues/101"
  click I102 "https://github.com/iogrid/iogrid/issues/102"
  click I103 "https://github.com/iogrid/iogrid/issues/103"
  click I104 "https://github.com/iogrid/iogrid/issues/104"
  click I105 "https://github.com/iogrid/iogrid/issues/105"
  click I107 "https://github.com/iogrid/iogrid/issues/107"
  click I108 "https://github.com/iogrid/iogrid/issues/108"
  click I109 "https://github.com/iogrid/iogrid/issues/109"
  click I110 "https://github.com/iogrid/iogrid/issues/110"
  click I111 "https://github.com/iogrid/iogrid/issues/111"
  click I112 "https://github.com/iogrid/iogrid/issues/112"
  click I113 "https://github.com/iogrid/iogrid/issues/113"
  click I116 "https://github.com/iogrid/iogrid/issues/116"
  click I117 "https://github.com/iogrid/iogrid/issues/117"
  click I118 "https://github.com/iogrid/iogrid/issues/118"
  click I119 "https://github.com/iogrid/iogrid/issues/119"
  click I120 "https://github.com/iogrid/iogrid/issues/120"
  click I121 "https://github.com/iogrid/iogrid/issues/121"
  click I122 "https://github.com/iogrid/iogrid/issues/122"
  click I141 "https://github.com/iogrid/iogrid/issues/141"
  click I142 "https://github.com/iogrid/iogrid/issues/142"
  click I143 "https://github.com/iogrid/iogrid/issues/143"
  click I144 "https://github.com/iogrid/iogrid/issues/144"
  click I145 "https://github.com/iogrid/iogrid/issues/145"
  click I146 "https://github.com/iogrid/iogrid/issues/146"
  click I147 "https://github.com/iogrid/iogrid/issues/147"
  click I148 "https://github.com/iogrid/iogrid/issues/148"
  click I154 "https://github.com/iogrid/iogrid/issues/154"
  click I155 "https://github.com/iogrid/iogrid/issues/155"
  click I158 "https://github.com/iogrid/iogrid/issues/158"
  click I168 "https://github.com/iogrid/iogrid/issues/168"
  click I169 "https://github.com/iogrid/iogrid/issues/169"
  click I170 "https://github.com/iogrid/iogrid/issues/170"
  click I172 "https://github.com/iogrid/iogrid/issues/172"
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
