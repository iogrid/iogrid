# iogrid — UAT (canonical test-case index)

Living test-case ledger. Rule: a TC passes only with **hard evidence** (a committed
screenshot, a backend receipt, or a timestamped wire/log capture). No evidence = not ✅.
Symbols: ✅ proven · ⚠️ fix shipped, validation pending · ⏳ unconfirmed · ❌ broken.

> **2026-06-19 correction (gap analysis):** rows previously marked ✅ for G2/G3 and the #701 native
> gate were OVER-CLAIMED — corrected below to verified runtime reality. See
> [`gap-analysis-2026-06-19.md`](./gap-analysis-2026-06-19.md). The repeated failure mode was reporting
> server-side / handshake / bookkeeping wins as end-user success while the data plane carries nothing.

Deep-walk records: [`UAT-iogrid-2026-06-15.md`](./UAT-iogrid-2026-06-15.md),
[`UAT-iogrid-2026-06-14.md`](./UAT-iogrid-2026-06-14.md).
VPN wire evidence: [`evidence/G1-vpn-live-wire-2026-06-14.md`](./evidence/G1-vpn-live-wire-2026-06-14.md).

| TC | Surface / capability | Status | Evidence |
|---|---|---|---|
| WEB-1…7 | Web surfaces (provider, customer, account, vpn, welcome, admin) | ✅ **7/7** | `UAT-iogrid-2026-06-15.md` (`709532cb`) — live walk + backend receipts |
| G3 | Admin $GRID earnings — provider settlement actually pays + headline reconciles | ❌ **BROKEN at runtime** (was ✅, over-claimed) | settlement-worker **1434+ consecutive failed ticks** (`treasury 10 < 25.925 GRID`, abort-whole-tick; no settle since 2026-06-14); headline **12.325/17 ≠ ledger 13.600/19** (`grid_build_settlement`); **all 20 rows self-pay** (provider==customer). Tracking: epic **#814** (#818 dead-lock, #819 reconcile, #820 RPC) |
| G2 | iOS build through the public API on a real Mac, exit 0, on-chain $GRID settle | ⚠️ **routing PROVEN; no green build ever** (was ✅, over-claimed) | API zero-SSH routing to Mac 808ce330 is real, BUT **0 real iOS builds completed green** — the 2 "succeeded" `build_gateway_builds` rows are ~2s `octocat/Hello-World` probes (`record->>'artifacts'`=null); #806 dogfood `37cb8fa9` = rejected/-1 (provider ENOSPC + #811 split-brain). Tracking: **#806** (green build), **#811** (status), **#821** (runner honesty), **#728** (disk) |
| **VPN-DP** | VPN data plane — founder's iPhone establishes a real WireGuard **handshake** | ✅ **handshake only** (NOT traffic) | `evidence/G1-vpn-live-wire-2026-06-14.md` (`099f9c78`): 3 handshakes 19:12–19:17, 0 decap-drops in that window. **Caveat (2026-06-19):** this proves the HANDSHAKE, not data — the "~6 min bidirectional" was WG keepalives. **0 payload bytes** have ever flowed (see VPN-E2E) |
| **#701** | NE gates "Connected" on a *real* handshake (no fake/black-hole) | ❌ **NOT fixed on main** (was ⚠️ "shipped") | `origin/main:.../PacketTunnelProvider.swift:212` is still a **bare `completionHandler(nil)`** after "tunnel established" — the fake-connection bug is LIVE in shipping native code. The fix is only in **OPEN PR #812** (unmerged) + the working tree. TS half (#786) is merged but doesn't cover the native path. Tracking: **#701 / #813** |
| **#789** | iOS NE self-heals on stale WG client-key drift | ⏳ **still firing in prod** (TS #790 merged; on-device unconfirmed) | `/var/log/iogridd.log`: founder's iPhone (212.72.24.20:51549) last record **2026-06-14T06:21:27 is a DROP** ("did not decapsulate against any known peer"); decap-fails live now from 188.135.27.125. Tracking: **#789 / #813** |
| VPN-E2E | End-user actually browses through the tunnel (egress IP = server) | ⏳ **UNCONFIRMED — never done** | `grep -c 'TUN sink routing-table entry added' /var/log/iogridd.log` = **0 lifetime**; `vpn_sessions` 927/927 zero-byte, max bytes = 0. No customer inner packet has EVER egressed. Tracking: **#815** |
| VPN-DUR | Tunnel survives a daemon restart without manual rebind | ⏳ **UNCONFIRMED** | restart-durability never re-proven on a real device reconnect; recent sessions get dropped at peer-lookup. Tracking: **#816** |
| SAFETY | Abuse pre-flight (CSAM/fraud/rate-limit) runs for proxy + compute | ❌ **OFF in prod** | `antiabuse-svc` scaled **0/0 (35d)**; PhotoDNA backend is a fail-open stub (returns ALLOW); container scanning = NoopScanner. Tracking: **#823** |
| DEPLOY | Merged→prod path rolls every service to latest CI-green digest | ✅ **prefix-collision FIXED + proven** | Root cause: unanchored `infra.${svc}.* deploy` substring grep matched the newer `antiabuse-svc-transparency-report` marker → harbor-path extraction failed → silent `skip antiabuse-svc: no deploy marker found` while antiabuse stayed on stale `f6ae6dc1`. Fix (PR #823): anchored `infra(${svc}): ` scope (literal parens, `grep -F`) + exact `…/iogrid/${svc}@sha256:` path. Same-bastion before/after DRY_RUN: OLD `skip antiabuse-svc: no deploy marker found` (0 roll) → FIXED `ROLL antiabuse-svc: f6ae6dc1 → 8e893046` (1 roll). Reconciled image-only (`kubectl set image` → `8e893046`); post-DRY_RUN `ok antiabuse-svc` (idempotent). Unit test `scripts/test-reroll-resolver.sh` 5/5 (antiabuse-svc/-transparency-report + vpn-svc/vpn-gateway prefix pairs). antiabuse-svc replicas stay 0/0 (re-enable = separate ticket). Tracking: **#822** |
| CI-E2E | Cross-service e2e harness gates merges | ⏳ **can boot, doesn't gate** | fixture committed (PR #803 merged, closed #800), but `e2e-ci.yml:53` has `continue-on-error:true` + `pull_request` commented out. Tracking: **#824** |
| CI-IOS | Swift/native changes validated on PR before merge | ❌ **no PR validation** | mobile-ios-ci is push:[main]+workflow_dispatch only; `gh pr checks 812` = "no checks reported". Tracking: **#825** |

**Honest summary (2026-06-19):** Web (WEB-1..7) = ✅ with committed evidence. **Zero of the three
north-star goals is end-user-real:** G1 VPN has carried **0 payload bytes ever** (927/927 zero-byte
sessions, TUN sink count 0, iPhone silent since 06-14, native fake-connection fix unmerged in PR #812);
G2 has **never produced a green iOS build** (only 2s probe "successes", #811 split-brain unfixed); G3
settlement is **dead-locked** (1434+ consecutive failures, self-pay wallets, headline ≠ ledger). The
VPN handshake (VPN-DP) is genuinely proven; the VPN as a usable product is not. Full analysis +
13-issue backlog: [`gap-analysis-2026-06-19.md`](./gap-analysis-2026-06-19.md).
