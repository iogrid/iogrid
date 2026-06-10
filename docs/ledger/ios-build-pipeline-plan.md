Confirmed. The forwarder is a TCP-over-DispatchFrame bridge (for proxy/bandwidth), NOT a status-push to build-gateway. The status updates the daemon sends are *persisted in workloads-svc store* (dispatch.go step 4), but nothing forwards them to build-gateway, which uses an InMemory dispatcher that never connected. This is the central architectural gap.

I now have everything needed. Producing the final plan.

---

# iOS-Build Milestone — End-to-End Devnet Delivery Plan

**Honest verdict up front:** the customer-facing build-gateway HTTP surface is real and tested; the daemon's Tart-based macOS runner is real; the VPN $GRID devnet payment path is real and live. But the **provider half of the build pipeline is a stub end-to-end**: build-gateway runs `workloadclient.NewInMemory(nil)` in production `main.go:82` (it never dials workloads-svc), the daemon's `convert.rs:262-267` drops every workload payload to `String::new()` (so a real assignment arrives empty and is rejected), and there is **no build-metering→$GRID-settlement bridge** (build-gateway emits a NATS event nobody consumes). The plan below closes exactly those gaps, in order.

## 1. End-to-End Architecture (target state)

```
CUSTOMER (CI or `iogrid build` CLI)
  │  POST https://build.iogrid.org/v1/builds  {git_url, git_ref, xcode_version, build_command, env_vars}
  │  Auth: X-Iogrid-Api-Key
  ▼
BUILD-GATEWAY (coordinator/services/build-gateway)
  │  • Build record (Postgres, NOT in-mem) → status=queued
  │  • Tarball/source ref handed off; dispatch via REAL workloadclient (Connect-Go → workloads-svc)
  │  • Listens for runner status/log callbacks on /internal/v1/builds/{id}/{status,logs,heartbeat}
  ▼
WORKLOADS-SVC (coordinator/services/workloads-svc)  [dispatch stream server ALREADY EXISTS]
  │  • Submission → store.Workload{IOSBuild}, scheduler picks the macOS provider (cap: ios_build)
  │  • Pushes WorkloadAssignment down the long-lived bidi Dispatch stream
  ▼  (mTLS, over the reverse-SSH tunnel: bastion :2223 → Hatice's Mac)
DAEMON `iogridd` on Hatice's Mac  (daemon/crates/*)
  │  • convert.rs serializes IosBuildRequest → payload_json  [FIX: today it's empty]
  │  • WorkloadRouter → workload-ios TartDriver:
  │       tart clone → set → run → tart ip → sshpass ssh → git clone+checkout+xcodebuild
  │       → simctl boot + install + Maestro 00-all.yaml walkthrough  [native macOS, NOT Podman]
  │       → scp .ipa/.app + maestro junit/screenshots out → PUT to presigned S3
  │  • Streams status/logs back up the Dispatch stream (running→succeeded/failed + exit_code)
  ▼
WORKLOADS-SVC persists updates → forwards to BUILD-GATEWAY  [FIX: forwarder is proxy-only today]
  │  build-gateway UpdateStatus()/AppendLog() → customer SSE tail + webhook + metering event
  ▼
METERING → billing-svc  (NATS iogrid.metering.build.v1)
  │  • NEW build_meter.go consumer → grid_settlement row (provider_share = billable_min × rate × 85%)
  ▼
$GRID DEVNET PAYMENT
  │  CUSTOMER PAYS: Ping approve URL w/ memo "iogrid.v1:build:ios:<spec>"  [NEW buildBuildMemo()]
  │       → ed25519 sig-verify (reuse vpn-svc sig path) + RPC getTransaction confirm (devnet mint
  │         BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR, Token-2022, 9-dec)
  │  PROVIDER EARNS: settlement_cron batches grid_settlement → SPL TransferChecked → Hatice's wallet
  ▼
DOGFOOD: .github/workflows/mobile-ios-dogfood.yml submits iogrid's OWN app build to the gateway,
         polls, downloads .ipa + maestro artifacts — replacing the 1401-line mobile-ios-ci.yml.
```

## 2. Ordered Implementation Steps (specific files)

### Phase A — Make the dispatch path carry payload (daemon) — *unblocks everything*
1. **`daemon/crates/transport/src/convert.rs:261-278`** — replace `payload_json: String::new()` with a real `serialize_workload_payload(&w)`. Add a fn that matches `w.payload` oneof and `serde_json::to_string()` the matching Rust type. **Blocker:** proto/Rust field mismatch (proto `IosBuildRequest` has `source_tarball_s3_key`/`build_commands[]`/`artifact_s3_bucket`+`prefix`; Rust `IosBuildWorkload` wants `repo_url`/`git_ref`/`build_command`/`upload_url`). **Decision: align proto → Rust** (the git-based flow is what TartDriver actually runs).
2. **`proto/iogrid/workloads/v1/submit.proto:59-70`** — extend `IosBuildRequest`: add `repo_url`, `git_ref`, `build_command` (singular), `upload_url`, `artifact_guest_path`, `cpu`, `memory_mib`, `boot_timeout_secs`. Keep `tart_image`. Run `make proto`.
3. **`coordinator/services/workloads-svc/internal/handlers/convert.go:144-152` + `:214-221`** and **`internal/store/store.go` `IOSBuildSpec`** — carry the new fields. **`submission.go:270`** — relax the `source_tarball_s3_key required` validation to accept `repo_url` OR tarball.
4. **`daemon/crates/core/src/workloads.rs:411-415`** — verify `serde_json::from_str::<IosBuildWorkload>` succeeds against the new payload (it will, once 1+2 land).

### Phase B — Wire build-gateway to the REAL provider path
5. **`coordinator/services/build-gateway/internal/workloadclient/workloadclient.go`** — add a `ConnectClient` implementing `Dispatcher` that submits to workloads-svc over Connect-Go (`workloadsv1connect`), translating the Build record → `IosBuildRequest`.
6. **`coordinator/services/build-gateway/cmd/build-gateway/main.go:81-83`** — env-switch: if `WORKLOADS_SVC_URL` set, use `ConnectClient`; if `DATABASE_URL` set, use a Postgres `store` instead of `NewInMemory` (builds must survive restart). Keep in-mem as the test default.
7. **`coordinator/services/build-gateway/internal/server/routes.go:90-94`** (new internal routes, dispatch-token guarded) — add:
   - `POST /internal/v1/builds/{id}/status` → `Service.UpdateStatus()` (`builds/service.go:300-338`, already exists)
   - `POST /internal/v1/builds/{id}/logs` → `Service.AppendLog()` (`builds/service.go:342-344`, already exists)
   - `POST /internal/v1/builds/{id}/heartbeat` → metering liveness (Blocker-5 mitigation)
   This is the **runner-facing claim/stream/report API** the findings call CRITICAL. It is the pragmatic alternative to a full workloads-svc→build-gateway forwarder.
8. **`coordinator/services/workloads-svc/internal/handlers/dispatch.go:52-59`** — on each daemon `WorkloadStatusUpdate`, in addition to persisting, POST to build-gateway's new `/internal/v1/builds/{id}/status` (env `BUILD_GATEWAY_INTERNAL_URL` + dispatch token). One new outbound HTTP call in the persist branch.

### Phase C — Native macOS runner: register a 'build' capability + Maestro walkthrough
9. **`daemon/crates/workload-ios/src/lib.rs:374-388`** — the `build_command` assembly already does git clone+checkout+xcodebuild. Append the **Maestro walkthrough** post-build inside the same VM: `simctl create/boot/install`, `maestro --device $UDID test .maestro/00-all.yaml --format junit`, scp `maestro-junit.xml` + screenshots out as additional artifacts. (CI sequence in `mobile-ios-ci.yml:737-922` is the source of truth; encode the gotchas from `mobile/ios/CONTRIBUTING.md`.)
10. **Daemon capability advertisement** — in the `DaemonHello` path (daemon transport + `dispatch.proto:63 DaemonHello`), advertise `ios_build` capability when `target_os="macos"` + Sequoia 15+ so workloads-svc scheduler routes iOS builds only to Macs. Verify the scheduler in workloads-svc filters on it.

### Phase D — Payment layer (build memo + sig-verify + provider earn)
11. **`mobile/ios/src/lib/wallets/ping-pay.ts:78`** — add `buildBuildMemo(platform, spec)` → `iogrid.v1:build:ios:<spec>` next to `buildVpnMemo()`; add a `buildBuildApproveUrl()` parallel to `buildVpnApproveUrl():115`.
12. **NEW `coordinator/services/billing-svc/internal/grid/build_meter.go`** — mirror `session_meter.go`; parse the build memo, compute `provider_share = billable_minutes × rate × 85%` (reuse `ProviderSharePct=85`), write a `grid_settlement` row (UNIQUE on `(build_id, attempt_id)` for idempotency).
13. **`coordinator/services/billing-svc/cmd/settlement-worker/main.go`** — subscribe to `iogrid.metering.build.v1`; on event → `build_meter.go`. `settlement_cron.go` already batches `grid_settlement`→`TransferChecked` (provider earn, no new code).
14. **Sig-verify:** reuse `vpn-svc/internal/payment/sig_verify.go` + `solana_balance.go` (devnet mint already wired). For Ping C-8, keep the client-side `verifyApprovalBestEffort` (ping-pay.ts:310-350) and add **`POST /v1/grid/approve-verify`** in `billing-svc/internal/server/grid_handlers.go` that does an RPC `getTransaction` confirm — devnet, no webhook dependency.
15. **`coordinator/services/billing-svc/internal/store/earnings.go:104-131`** — UNION `grid_settlement.provider_share` into `SumProviderEarnings` so Hatice's build earnings show up.

### Phase E — Dogfood wiring
16. **NEW `.github/workflows/mobile-ios-dogfood.yml`** — `ubuntu-latest` job: `curl POST` build to gateway, poll `GET /v1/builds/{id}` until terminal, download artifacts via presigned URLs, tail SSE logs. Gated on a `dogfood` branch first, then cut `main` over from `mobile-ios-ci.yml`.
17. **Trigger command** (also the manual smoke test):
```bash
curl -sS -X POST https://build.iogrid.org/v1/builds \
  -H "X-Iogrid-Api-Key: ${IOGRID_API_KEY}" -H "Content-Type: application/json" \
  -d '{"git_url":"https://github.com/iogrid/iogrid.git","git_ref":"main","xcode_version":"latest",
       "build_command":"cd mobile/ios && npm install --legacy-peer-deps && npx expo prebuild --platform ios --no-install --clean && ruby scripts/add-network-extension-target.rb && cd ios && pod install && xcodebuild -workspace *.xcworkspace -scheme iogrid -configuration Release -destination generic/platform=iOS -archivePath /tmp/app.xcarchive archive"}' \
  | tee /tmp/sub.json
BUILD_ID=$(jq -r .build_id /tmp/sub.json)
curl -sN https://build.iogrid.org/v1/builds/$BUILD_ID/logs -H "X-Iogrid-Api-Key: ${IOGRID_API_KEY}"
```

## 3. Smallest First Deliverable Demonstrable on Devnet TODAY

**"iogrid's own mobile app, built on Hatice's Mac through the build-gateway, artifacts returned, one devnet $GRID charge settled."**

Minimum cut to get a *real* build through (skip Maestro/payment polish first):
- **Phase A (steps 1-4):** un-break the payload — proto align + `convert.rs` serialize. Without this, no assignment ever reaches TartDriver. (~½ day)
- **Phase B (steps 5-8):** point build-gateway at workloads-svc + the 2 internal callback routes + the dispatch.go status push-back. (~1 day)
- **Run the trigger command (step 17)** against a build-gateway deployed with `WORKLOADS_SVC_URL` set, with Hatice's `iogridd` paired and advertising `ios_build`. Watch SSE logs show `xcodebuild` running on the Mac; download the `.ipa`.
- **One $GRID charge (thin slice of Phase D):** before submit, generate a Ping approve URL with `buildBuildMemo("ios","dogfood-smoke")`, pay devnet $GRID from the test wallet, confirm via RPC `getTransaction`. Even if `build_meter.go` settlement lags, the **customer-pays leg is provable today** because the devnet mint + sig-verify + RPC poll already exist.

That is the honest "real iOS build runs on a registered Mac THROUGH iogrid + a devnet $GRID charge" demo. Provider-earn (settlement_cron transfer to Hatice's wallet) and the Maestro walkthrough land in the immediate follow-up (Phases C+D full).

## 4. Podman pod vs Native macOS

| Workload | Execution | Why |
|---|---|---|
| **iOS build (this milestone)** | **NATIVE macOS** — Tart VM via `tart` CLI + `sshpass` ssh, `xcodebuild`, `simctl`, Maestro, all inside an ephemeral `iogridd-ios-<id>` VM on the Mac. **No container, no Podman.** | Xcode/Simulator require real macOS; cannot containerize. `workload-ios/src/lib.rs:225-327`. macOS 15 Sequoia min enforced. |
| Docker compute / GPU-CUDA / bandwidth-proxy (other lanes) | Container via **bollard** (Docker daemon socket), feature `docker-real`. **Podman is NOT implemented** — no `workload-podman` crate, no `podman-real` feature exists today. | `workload-docker/src/lib.rs:218-400`. The "Podman pods (rootless)" framing is the *founder ideal*, not shipped code. |

**Honest note:** the constraint "containerizable workloads run as Podman pods" is **aspirational for the Linux/compute lanes and out of scope for the iOS milestone**. iOS is the native-macOS exception by physics. If Podman is required for the compute lanes, that's a separate `daemon/crates/workload-podman/` crate (mirror `workload-docker`, add `podman-real` feature) — do **not** block the iOS milestone on it.

## 5. Exact Mac-Access Hop (bastion → Hatice's Mac)

```bash
# Reverse-SSH tunnel: terminates at bastion 144.91.121.182, binds 127.0.0.1:2223 → Hatice's Mac sshd.
# Maintained by macOS LaunchAgent org.iogrid.tunnel (installer/macos/install-tunnel.sh, REMOTE_PORT=2223).
ssh -i ~/.ssh/openova_migration openova@144.91.121.182 \
  "ssh -p 2223 -i ~/.ssh/claude_offload emrah@localhost '<command on Hatices-Mac-mini-2>'"

# Examples:
#   ... 'iogridd diag --json'                 # health
#   ... 'tart list'                           # confirm Tart present
#   ... 'which xcodebuild sshpass maestro'    # confirm toolchain
# Provider record: provider_id 808ce330-79c1-4390-8cc6-87c5ce5a94d8, daemon user `emrah`.
```
For the milestone, the **daemon's mTLS Dispatch stream to workloads-svc rides this same tunnel** — you do not ssh to run builds; you submit via the gateway and the paired `iogridd` pulls the assignment. The ssh hop is for provisioning/diagnostics only.

## 6. Real Blockers + Concrete Workaround for Each (nothing parked)

1. **Daemon drops payload to empty string** (`convert.rs:262-267` → every assignment rejected `payload_decode_failed`). **Workaround/fix:** implement `serialize_workload_payload()` (Phase A step 1). This is a 1-function fix, not external — do it.
2. **Proto/Rust iOS field mismatch** (proto tarball-based vs Rust git-based). **Fix:** extend proto to git fields + regen (Phase A step 2), the lower-risk direction since TartDriver's `build_command` already does git clone.
3. **build-gateway never dials workloads-svc** (`main.go:82` InMemory in prod). **Fix:** `ConnectClient` + env switch (Phase B steps 5-6). Self-contained.
4. **Build-gateway has no runner status/log ingest** (UpdateStatus/AppendLog are internal, unexposed; forwarder is proxy-only). **Workaround (chosen):** 3 dispatch-token-guarded `/internal/v1/builds/{id}/*` endpoints + a single outbound POST from `workloads-svc/dispatch.go` — avoids building a full event-stream subscriber. (Phase B steps 7-8.)
5. **Builds vanish on pod restart** (InMemory store in prod). **Fix:** env-driven Postgres store (Phase B step 6). If a build-gateway Postgres schema doesn't exist yet, ship a migration mirroring vpn-svc's pattern; until then, gate the dogfood on a single long-lived pod.
6. **No build→$GRID settlement bridge** (NATS event emitted, no consumer). **Fix:** `build_meter.go` + settlement-worker subscriber (Phase D steps 12-13). Reuses live VPN settlement_cron.
7. **Ping C-8 (RPC-poll vs webhook) undecided.** **Workaround:** don't wait on Ping — implement client-side `verifyApprovalBestEffort` (exists) + a coordinator `POST /v1/grid/approve-verify` doing devnet RPC `getTransaction`. Webhook can be added later without rework. (Phase D step 14.)
8. **Maestro stale-XCTest-handle crashes** (per MEMORY `feedback_maestro_stale_xctest_handle`). **Workaround:** outer restart loop (fresh maestro session, 3 attempts) gated on the `App crashed` signature — port the CI loop into the daemon's runner stage. (Phase C step 9.)
9. **Shared dispatch-token (no per-provider auth on artifact upload).** **Workaround for devnet:** accept the shared token (S3 buckets are per-workspace; only the assigned Mac is in flight). Per-attempt token is a Phase-2 hardening, not a milestone blocker.
10. **Mainnet-zero-exception guard:** every Solana call must target devnet mint `BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR` / devnet RPC. **Fix:** assert `GRID_TOKEN_MINT_ADDRESS` == devnet mint in `build_meter.go` and the approve-verify handler; fail closed if a mainnet mint is configured.

**Scaffold vs real, no false victories:**
- REAL: customer build-gateway HTTP API; daemon Tart macOS runner; VPN $GRID devnet payment (authorize/heartbeat/settle); settlement_cron SPL transfer; Mac tunnel + paired `iogridd`.
- STUB/BROKEN: build-gateway→workloads-svc dispatch (InMemory in prod); daemon payload serialization (empty); runner→gateway status/log ingest (nonexistent HTTP path); proto/Rust iOS schema (mismatched); build memo + build→settlement bridge (absent); build-gateway store (in-mem, non-durable); Maestro-in-daemon (only in CI YAML, not extracted); Podman runner (does not exist).

Key files to CREATE: `coordinator/services/billing-svc/internal/grid/build_meter.go`, `.github/workflows/mobile-ios-dogfood.yml`, `ConnectClient` in `workloadclient.go`, three internal routes in `build-gateway/internal/server/routes.go`. Key files to CHANGE: `daemon/crates/transport/src/convert.rs`, `proto/iogrid/workloads/v1/submit.proto`, `coordinator/services/workloads-svc/internal/handlers/{convert.go,submission.go,dispatch.go}`, `coordinator/services/build-gateway/cmd/build-gateway/main.go`, `daemon/crates/workload-ios/src/lib.rs`, `mobile/ios/src/lib/wallets/ping-pay.ts`, `coordinator/services/billing-svc/{cmd/settlement-worker/main.go,internal/store/earnings.go,internal/server/grid_handlers.go}`.
