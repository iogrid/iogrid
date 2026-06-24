# iogrid — Gap Analysis & Backlog Rebuild (2026-06-19)

> Honest, evidence-backed audit. Every "done"/"PROVEN"/"100%" claim was treated as UNVERIFIED
> until checked against the actual code (origin/main), the deployed/running state, and on-wire/runtime
> reality. Over-claiming (server-side wins reported as end-user success) has been the repeated failure.
> Citations: file:line, command output, issue/PR numbers.

## Top line

iogrid's control plane, build-gateway routing, settlement bookkeeping, web surface, and reroll-deploy
path are real and largely work — but **none of the three north-star goals is met at the DoD bar**
(a real person sees it work; traffic/builds/money actually flow).

Verified live (2026-06-24):
- All **927 prod VPN sessions are zero-byte** and **0 inner packets have ever egressed** (`vpn_sessions`:
  927 total / 0 with bytes / max(bytes_in+bytes_out)=0; `grep -c 'TUN sink routing-table entry added'
  /var/log/iogridd.log` = 0 lifetime).
- **No real iOS app build has ever completed green** — the only 2 "succeeded" `build_gateway_builds`
  rows are ~2s shell probes against `octocat/Hello-World` (`record->>'artifacts'` = null).
- The **$GRID settlement worker has been hard-failing for 1434+ consecutive ticks** (~5 days) on an
  underfunded treasury (`treasury balance 10 GRID < required 25.925 GRID — needs manual top-up`), with
  **provider==customer on every one of 20 settlement rows** (self-pay, not a customer→provider economy).

The recurring pattern the founder flagged is confirmed: server-side / handshake / DB wins are being
reported as end-user success while the data plane carries nothing. Docs are honest on the VPN data
plane and web, but over-claim the native #701 fix as "shipped" (it is in OPEN PR #812), the all-3-goals
deliverable, and live G3 health.

**UAT verdict: FAIL at the founder's DoD bar.**

---

## G1 — VPN connects AND carries traffic on the founder's real iPhone

**Real state: NOT MET.** The server data-plane code is engineering-correct (boringtun decap +
`peer_binder` auto-bind + #788 restart-reconcile + `tun_forward` NAT, all unit-tested; iptables
MASQUERADE/FORWARD + `iogrid-tun0` 10.66.0.1/16 live on the bastion), and auto-bind genuinely posts
provider keys (270/927 sessions carry a daemon-posted `provider_wg_public_key` — refuting the "always
manual bind" worry).

BUT verified live:
- `vpn_sessions` = 927 total / 927 zero-byte / max(bytes_in+bytes_out) = **0** — not one session has
  ever moved a payload byte.
- `grep -c 'TUN sink routing-table entry added' /var/log/iogridd.log` = **0** across the full history —
  no customer inner packet has EVER reached the forward sink, so browsing/egress provably never happened
  (the 06-14 "32B bidirectional" proof was WG keepalives, not payload).
- The founder's iPhone (212.72.24.20:51549) last touched the daemon **2026-06-14T06:21:27** and the
  record is a **DROP** ("did not decapsulate against any known peer"); current live traffic is dominated
  by decap-fails from a stale-key desktop client (188.135.27.125).
- The #701 native handshake gate is **NOT on main**: `origin/main:mobile/ios/native/ios/PacketTunnelProvider/PacketTunnelProvider.swift:212`
  is still a bare `completionHandler(nil)` after "tunnel established". The fix sits only in OPEN PR #812
  + the uncommitted working tree.
- #789 stale-WG-client-key class is still firing in prod today.

### Gaps
- Merge PR #812 (native NE gates "connected" on a real handshake) — currently OPEN; fix only in working tree. → **#813 / #701**
- On-device confirmation: real handshake + actual traffic routing (egress IP == server) — NEVER done; TUN sink has carried 0 packets ever. → **#815**
- Fix #789 stale-WG-client-key self-heal on-device (TS #790 merged, on-device adoption unconfirmed). → **#813 / #789**
- Auto-bind durability: recent sessions get dropped at peer-lookup; restart-durability never re-proven on a real device reconnect. → **#816**
- Non-Linux/macOS VPN provider registers `egress_capable=true` but uses LoggingSink (never NATs) — black-holes any customer routed to a Mac (`vpn_wiring.rs:325-329,397-399`; documented limitation #694). → **#817**

---

## G2 — iOS/compute builds run through the build-gateway API (zero-SSH, $GRID-earning), ping as first customer

**Real state: PARTIALLY MET (routing only).** API submit + union-capabilities routing (#810) +
multi-static-key (#807) merged; live SSE routing to the Mac provider over the API zero-SSH is proven.

BUT no real iOS build has EVER completed green: of 11 lifetime `build_gateway_builds` records the only
2 "succeeded" are ~2-second shell probes against `octocat/Hello-World` (`record->>'exit_code'`=0,
`record->>'artifacts'`=null); every actual iOS build failed (exit 1) or rejected (scheduler_paused).
The #811 split-brain is real and unfixed (`git grep scheduler_paused origin/main -- coordinator/`
returns nothing) — the #806 dogfood build 37cb8fa9 is recorded `rejected` / `exit_code`=-1 while the
poll path ran it. Throughput is near-zero (11 rows lifetime; stale `running`/`dispatched` rows never
reaped). The default-built daemon also runs Docker (`ScaffoldDockerRunner`) and GPU (`NoopGpuRunner`)
workloads as FAKE exit-0 successes (`workload-docker/src/lib.rs:196-216`, `workload-gpu/src/lib.rs:164-180`).

### Gaps
- A real iOS xcodebuild must COMPLETE green (exit 0 + `record->>'artifacts'`) end-to-end — never achieved; blocked by provider ENOSPC (#728 disk) + a stale daemon. → **#806**
- Implement #811 server-side guard (stream scheduler_paused non-terminal for IOS_BUILD; poll running supersedes) + a stale-row reaper. → **#811**
- Docker/GPU runners return fake exit-0 success for jobs they never run — must fail closed (the `MlxStubGpuRunner` `BackendUnimplemented` pattern). NOTE: `capability_report.rs:111` correctly DERIVES `gpu_enabled` from supported_types (the default daemon reports `gpu_enabled=false`; an existing test asserts this) — the bug is the runner faking success, not the flag. → **#821**
- `build_gateway_builds` carries no provider attribution column while settlement attributes to 808ce330 — attribution not traceable through the gateway. → folded into #811 / G2 follow-ons.
- Single Mac build provider (808ce330) is an unmitigated SPOF for the whole G2 line. → platform reliability.

---

## G3 — provider $GRID earnings (Hatice's Mac) visible in admin

**Real state: BROKEN AT RUNTIME + over-claimed.** The settlement code path is wired and admin renders a
number, BUT verified live:
- settlement-worker has **1434+ consecutive failed ticks** (~5 days): `treasury balance 10000000000 <
  required 25925000000 — needs manual top-up`. RunOnce aborts the WHOLE tick on insufficient treasury,
  so one 25.925-GRID unsettled requirement dead-locks all payouts. No $GRID has settled since
  2026-06-14 17:08.
- The admin headline "12.325 GRID / 17 builds" does NOT reconcile with the ledger: `grid_build_settlement`
  = 20 rows / 19 settled / **13.600 GRID** settled provider_share.
- `provider_wallet == customer_wallet` on **all 20 rows** (self-pay dogfood, not a customer→provider economy).
- On-chain settlement runs on an ephemeral in-cluster `solana-validator` (settlement-worker
  `SOLANA_RPC_URL=http://solana-validator.iogrid.svc.cluster.local:8899`); cited "Finalized" devnet tx
  (e.g. 4Zrmyw8oT97…) reportedly return null. billing-svc's read-RPC source is unconfirmed (must be
  proven before any unify — see #820).

### Gaps
- Settlement pipeline dead-locked: replace abort-whole-tick with skip-the-oversized-wallet; seed/parameterize the devnet treasury. → **#818**
- Reconcile the admin headline (12.325/17) with the settled ledger (13.600/19). → **#819**
- No real customer→provider value transfer (provider==customer on every row; amounts are synthetic floors). → **#814**
- On-chain proof not durable/verifiable; cited tx unfindable. Prove the read/write RPC split first, then unify + persist. → **#820**

---

## Platform gaps

- **Reroll cron prefix-collision** (`scripts/reroll-iogrid-deployments.sh:47-52`): `grep -iE
  "infra.${svc}.* deploy "` for `antiabuse-svc` matches `infra(antiabuse-svc-transparency-report):`
  first → harbor-digest extraction fails → silent "no deploy marker found". `antiabuse-svc` is frozen on
  a stale digest while latest CI-green `8e893046` never deploys. Any prefix-named service can silently
  freeze. → **#822**
- **antiabuse-svc scaled 0/0 (35d)** — CSAM/fraud/rate-limit pre-flight is OFF in prod; proxy-gateway
  points at a dead service. PhotoDNA backend is a fail-open stub (returns ALLOW); container scanning
  defaults to NoopScanner. Serious safety hole for a residential-proxy + container-compute product. → **#823**
- **mobile-ios-ci has no `pull_request` trigger** (push:[main] + workflow_dispatch only) — the
  G1-critical Swift NE fix PR #812 gets zero pre-merge validation (`gh pr checks 812` = "no checks
  reported"). → **#825**
- **e2e-ci never gates**: fixture committed by MERGED PR #803 (closed #800), so the harness can now boot,
  BUT `e2e-ci.yml:53` still has `continue-on-error: true` and the `pull_request` trigger is commented
  out (lines ~30-35). No automatic cross-service gate before prod. → **#824**
- **Single K8s node** (`vmi3116389`, control-plane) = unmitigated SPOF for all data-plane + control-plane + DB. → **#682** (blocked-ext).
- **Prod DB backups co-located on the same single node** (barmanObjectStore → in-cluster minio; pg-dump CronJob in-cluster) — a node/disk loss destroys DB AND backups; no offsite DR. → **#652** (parked, needs Hetzner creds).
- **Google sign-in broken in prod**: GOOGLE_CLIENT_ID is a phase0 placeholder; needs an operator Console step. BFF StartGoogleSignIn stub now returns 400 not 501. → **#646** (blocked-ext).
- **Deploy surface is a single brittle bastion cron** (string-grep over `git log`, silent skips, no Flux, no CI cluster creds). It IS installed/running now (last tick 2026-06-24 exit=0, closing the prior ~25-day freeze for normally-named services), but the prefix-collision proves a silent freeze can recur (#822).
- **admin G3 config exists only as live kubectl/DB mutations**, not baked into `infra/k8s/base/admin` — a redeploy/reroll regresses the operator view. → **#794**.

---

## Over-claims corrected

- `docs/ledger/UAT.md` #701 row said the native gate is "shipped — PR #812"; PR #812 is OPEN/unmerged and `origin/main` PacketTunnelProvider.swift:212 is still a bare `completionHandler(nil)`. The #701 fake-connection bug is LIVE in shipping native code. → corrected to ❌ in UAT.
- `docs/deliverables/three-goals-100-percent-2026-06-14.md` declares "All three are met / a real person sees it work in their hands" — contradicted by the SAME-DAY G1 evidence file (browsing + durability "NOT yet confirmed") and by live state (0-byte sessions, failed builds, dead settlement worker).
- UAT-iogrid-2026-06-15 calls the settlement pipeline "stable" and the number "unchanged" — it is unchanged BECAUSE the worker is hard-failing every tick (1434+ consecutive, unactioned treasury-underfunded alert).
- MEMORY/#748 "PROVEN LIVE ON-CHAIN — solana confirm 4Zrmyw8… = Finalized" — that proof did not persist (ephemeral validator).
- G3 headline "12.325 GRID / 17 builds" ≠ the settled ledger it claims to reflect (13.600 GRID / 19 rows). The deliverable doc cites a THIRD number (11.05 / 14) — none match.
- #806 "dogfood PROVEN" conflates "request reached the Mac zero-SSH" with "product works" — the build FAILED on ENOSPC; #806 stays open, status/uat stripped.
- MEMORY "G1 FULLY WORKING + bidirectional + restart-durable" rests on one tcpdump window 10 days ago; device silent since, last daemon record is a drop, 0 inner packets ever egressed.
- build-gateway "succeeded" status is unreliable — #811 split-brain records rejected/scheduler_paused for builds the poll path actually ran; the 2 "succeeded" rows are 2-second probes.

---

## What IS genuinely delivered (honest)

- **Web surface (WEB-1..7)**: #801/#802/#808 merged and deployed via the running reroll cron — UAT web rows are honestly evidenced.
- **VPN server-side data-plane code** is engineering-correct and well-tested: boringtun decap, peer_binder auto-bind, #788 restart-reconcile, tun_forward NAT; iptables MASQUERADE/FORWARD + iogrid-tun0 live on the bastion.
- **Auto-bind genuinely works** (DB shows daemon-posted provider keys on 270/927 sessions) — refutes the "binds always required a manual DB hack" worry.
- **build-gateway API submit + union-capabilities routing (#810) + multi-static-key (#807)** merged and proven to route builds to the Mac provider over the API zero-SSH with live SSE.
- **TS-layer connection gates merged**: #786 (gate Connected on a real handshake, UI/TS) and #790 (force NE recreate on WG client-key drift, TS) — native + on-device halves unproven.
- **The image-reroll cron** (the only merged→prod path) IS installed and running on the bastion (last tick 2026-06-24 exit=0) — the catastrophic ~25-day frozen-prod gap is closed for normally-named services.
- **G3 devnet settlement code** is wired and ran historically (19 settled rows, 13.6 GRID, settled 2026-06-13/14) — the bookkeeping exists, even though it is now dead-locked and self-pay.
- **BFF StartGoogleSignIn native stub** implemented — endpoint returns 400 not 501; only placeholder OAuth creds remain (#646).
- **Ping C-8 sig-verify gate cleared** via ADR 0029 (ping-cash#188 closed); iogrid's verifyApprovalBestEffort poll (#784) merged.
- **UAT.md is honest where it matters most**: VPN-E2E (browsing) and VPN-DUR (restart durability) correctly left ⏳ UNCONFIRMED; the 06-14 on-wire evidence file explicitly says "do not over-claim".

---

## New backlog map (issues created 2026-06-19)

| # | Title | Goal/area |
|---|---|---|
| **#813** | EPIC: G1 — make the VPN genuinely carry traffic on the founder's real iPhone | G1 epic |
| #815 | G1: on-device traffic-routing validation (egress IP == server) — the NEVER-DONE core proof | G1 |
| #816 | G1: auto-bind durability — sessions dropped at peer-lookup; bind live + re-prove on reconnect | G1 |
| #817 | G1: non-Linux/macOS VPN provider advertises egress but uses LoggingSink — black-holes (#694) | G1 |
| **#814** | EPIC: G3 — make $GRID provider settlement actually pay providers | G3 epic |
| #818 | G3: settlement-worker dead-locks the whole tick on insufficient treasury — skip oversized wallet | G3 |
| #819 | G3: admin headline GRID/builds doesn't reconcile with the settled ledger | G3 |
| #820 | G3: unify Solana read/write RPC + make cited tx durable/verifiable (prove the split first) | G3 |
| #821 | G2: Docker/GPU runners fake exit-0 success for jobs they never run — fail closed | G2 |
| #822 | deploy: reroll cron prefix-collision silently freezes prefix-named services (antiabuse stale) | platform |
| #823 | antiabuse-svc scaled 0/0 — CSAM/fraud preflight OFF; PhotoDNA + scanning fail-open stubs | platform |
| #824 | e2e-ci: harness can boot (#803 merged) but continue-on-error:true + no PR trigger — make it gate | platform |
| #825 | mobile-ios-ci has no pull_request trigger — Swift NE changes merge with zero pre-merge validation | platform |

### Triage of the 12 pre-existing open issues
- **#811** edit-rescope (server-side split-brain guard + reaper; DoD SQL corrected to jsonb accessors) — kept open.
- **#806** edit-rescope (real green build via API; status/uat stripped; DoD SQL corrected) — kept open.
- **#800** verifier override: was CLOSED with PR #803 MERGED — left closed; remaining work tracked in #824 (comment posted).
- **#794** edit-rescope (bake admin G3 config into infra/k8s/base/admin) — kept open.
- **#701** keep-open + `status/uat` stripped (native gate NOT on main, only in OPEN PR #812).
- **#789** keep-open + `status/uat` stripped (stale-key decap-fails live in prod).
- **#728** keep-parked (provider-disk root cause behind #806 ENOSPC).
- **#682** keep-blocked-ext (single node SPOF; founder hardware decision).
- **#665** keep-blocked-ext (mainnet $GRID mint; founder financial decision; C-8 cleared).
- **#652** keep-parked (offsite DR; needs Hetzner creds).
- **#646** keep-blocked-ext (Google OAuth client; operator Console step).
- **#574** keep-parked (ASC privacy labels UI-only; needs founder cookie).

### Genuinely shippable autonomous next steps
1. Merge PR #812 (native handshake gate) — then #815 on-device traffic proof.
2. Fix #811 split-brain + #821 runner fail-closed.
3. Fix #822 reroll prefix-collision (antiabuse stale) + #823 antiabuse fail-closed/up.
4. Unblock #818 settlement-worker treasury-abort; #819 reconcile; #820 RPC.
5. Enable #824 e2e gate + #825 mobile PR trigger.

Operator-gated (correctly blocked): #682 (2nd node), #652 (Hetzner DR), #646 (Google OAuth), #665 (mainnet mint), #574 (ASC/TestFlight cookie).

---

## Execution status — updated 2026-06-19 (post session-limit interruption)

A session-limit window killed 4 in-flight delivery agents mid-run; their committed work + opened PRs are captured here so the roadmap stays the single source of truth.

### Merged to main since the gap analysis
- **#812** (`bec19473`) — #701 native NE gates `startTunnel` completion on a REAL handshake; the fake-"connected" branch is dead. (G1 client honesty.)
- **#823** (`dcc490f2`) — antiabuse-svc re-enabled 1/1 + live-proven (CSAM URL → BLOCK, normal → ALLOW). proxy-gateway is **fail-CLOSED**, so the 35-day outage DENIED proxy traffic — it did NOT allow it unchecked. (Safety restored.)
- **#828** (`72b7c322`) — reroll prefix-collision fixed (antiabuse no longer silently frozen).
- **#803** (`610ad778`) — e2e harness boots.

### Open PRs awaiting review / validation
| PR | Goal | What | Gating |
|---|---|---|---|
| **#827** | G1 | full-tunnel captures IPv6 (`::/0`) — the "connects but doesn't browse" fix (IPv6 leaked past the v4-only tunnel on cellular) | Mac build validation (env broken — see blocker) + on-device browse check |
| **#830** | G3 | settlement worker skips an oversized/unfundable settlement instead of dead-locking the whole tick | CI green + review |
| **#831** | safety | antiabuse fail-closed CSAM stub + container hardening | CI green + review |

### Blocker
- **Mac provider build env** — disk 98-99% (the 2026-06-15 cleanup re-filled) + system Ruby 2.6.10 (< 2.7, breaks CocoaPods). Blocks iOS build validation (#827) and the first-ever green build (G2). Durable re-fix in progress (disk cleanup + Ruby ≥2.7).

### Per-goal honest status (now)
- **G1** — server egress PROVEN (evidence doc); client IPv6 route fix in **PR #827** (pending Mac build + device browse check); #812 native honesty gate MERGED; #816 auto-bind durability open. NOT end-user-confirmed.
- **G2** — first green iOS build still blocked on the Mac env; runner-fail-closed #821 open. NOT met.
- **G3** — settlement dead-lock fix in **PR #830**; headline-reconcile #819 + RPC-unify #820 open. NOT met (no real customer→provider settle proven).
- **Safety** — antiabuse RESTORED + proven fail-closed (#823 merged; #831 hardening open).

---

## G3 economy — settlement pipeline FIXED + real payout PROVEN on-chain (2026-06-19)
- **#833 MERGED** — self-pay root cause: `build-gateway/internal/builds/service.go:539-547` (settleGrid) resolved customer (submitter) + provider (Mac owner) wallets independently, but in the dogfood they're the SAME identity → all 32 `grid_build_settlement` rows were `provider==customer` (treasury paying itself = fake earnings). Fix: `provider_wallet` cleared when == customer (kept for audit, non-payable); worker queries add `provider_wallet <> customer_wallet`.
- **First REAL customer→provider settle PROVEN** (devnet, in-cluster test-validator): customer ≠ provider, 0.85 GRID — `solana confirm 5rkpV5FqJ6BQyBSsDe3NGTdu8dTWWwg6gD9TuGbLSvBYi1sqQXEpBVByHBVrE33o5F5VnR64yzLNEVkEM3drkMBq = Finalized`; provider 0 → 0.85, treasury 9 → 8.15.
- **#819 reconciled + closed** — orphan `provider_id IS NULL` row backfilled → per-provider headline == ledger == 13.600 GRID / 19.
- **#835 MERGED** — settlement-worker added to the reroll SERVICES array (it was silently never auto-deployed — the cause of its stale code).
- **HONEST remaining:** the historical 13.600 GRID were self-pay (now non-payable); the first genuine payout is the 0.85 proven settle. A real *build-driven* provider payout (external customer's build → Mac earns) needs an external customer — the dogfood is self-pay, correctly excluded. **G3 pipeline = proven + honest; full build→earn end-to-end = pending an external customer.**
