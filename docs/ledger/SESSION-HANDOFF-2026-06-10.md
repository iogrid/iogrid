# Session Handoff — 2026-06-10

> **Purpose:** close the gap between ground reality and the human view. Everything below is **independently verifiable** with the commands in §5 — not narrative. Written so a *regular-effort* Opus 4.8 agent (no ultracode / no workflows) can pick up the remaining work.

## 1. Honest TL;DR

- **Not theater.** 2 branches pushed, a **real VPN connection proven**, a **permanent Mac backdoor**, an **iOS pipeline coded** (22 files, +2027 lines). All checkable in §5.
- **Not complete.** The headline — *a live iOS build through iogrid* — is **blocked on the Mac disk** (~4 GiB free; the Tart runner needs ~60 GiB). That decision is yours.
- **Handoff-ready: YES for the remaining code work.** The VPN follow-ups and iOS Phases D/E are scoped (§4 + the committed plan `ios-build-pipeline-plan.md`). A regular agent can continue. The **disk** and **mainnet $GRID** decisions are yours alone.

## 2. Big-picture matrix

| # | Goal | Deliverable | Status | Remaining | Who does the rest |
|---|------|-------------|--------|-----------|-------------------|
| 1 | **Real VPN connection** | customer → provider → exit-IP swap + metered bytes | ✅ **PROVEN** manually (exit IP → `144.91.121.182`, 4.4 KiB rx); 🟡 not yet via CI/mobile-app | deploy the 2 committed fixes to vpn-svc; release the CLI; reproduce the deeper CLI-tunnel bug → green `vpn-e2e-smoke`. Residential value-prop needs the **DERP relay (#521)**. | regular agent (code), you (deploy approval) |
| 2 | **iOS builds via iogrid** | real app build on a Mac through iogrid, artifacts back, paid in devnet $GRID | 🟡 pipeline code **A–C done + compiles**; 🔴 **live build blocked on Mac disk**; D/E not done | **free the Mac disk (YOU)**; finish Phase D (payment) + E (dogfood); build+run the fixed daemon on the Mac; deploy build-gateway/workloads-svc; run a live build | you (disk), regular agent (code) |
| 3 | **$GRID / Ping (devnet)** | buy + earn $GRID via Ping, zero-exception devnet | ✅ devnet path built pre-session (mint, escrow, sig-verify, 24 tests); 🟡 build-payment (Phase D) not done | Phase D build-meter + memo; mainnet mint = **YOUR** go-live | regular agent (code), you (mainnet) |
| 4 | **Mac backdoor reliability** | permanent never-dropping admin tunnel | ✅ **DONE** | none | — |

Legend: ✅ done/proven · 🟡 partial · 🔴 blocked-on-decision.

## 3. What the 3 workflows actually produced (not theater)

1. **`iogrid-vpn-connect-delivery`** (17 agents) — diagnosed that the architecture has **no NAT-traversal** and the provider was running on the **wrong, NAT'd machine**. That diagnosis is what cracked the "VPN never works" mystery → I moved the provider to a reachable machine (`bastion.openova.io`) and **proved the connection**. *Tangible outcome: the proven tunnel + the root-cause.*
2. **`iogrid-ios-build-milestone-scope`** (7 agents) — produced the concrete buildable iOS plan (committed as `ios-build-pipeline-plan.md`). *Tangible outcome: the plan a regular agent now follows.*
3. **`iogrid-ios-build-implement`** (killed mid-Phase-C by a tmux kill) — implemented Phases A–C → commit `6dfb3e52` (22 files, compiles). *Tangible outcome: the pipeline code.*

## 4. Remaining work, structured for a regular agent

**VPN → green CI (issue #701):**
- a. Deploy `fix/vpn-cli-e2e` vpn-svc change (pickEndpoint) to the cluster (image + reroll). Low blast-radius (only changes the endpoint string for new mobile sessions).
- b. Release the CLI with the handshake-gate (so `install-cli.sh` serves it).
- c. Reproduce the CLI-flow failure live (a netns customer on the clients machine vs. the bastion provider) to find why traffic doesn't route even to a reachable provider; the daemon + data plane are proven, so the bug is in the CLI's bring-up/handshake timing.

**iOS pipeline (epic #700) — follow `ios-build-pipeline-plan.md` Phases C-finish/D/E:**
- Phase C-finish: advertise `ios_build` capability in `DaemonHello` (daemon transport + dispatch.proto).
- Phase D: `coordinator/services/billing-svc/internal/grid/build_meter.go` (mirror `session_meter.go`, key on build_id+attempt_id), settlement-worker subscribe `iogrid.metering.build.v1`, `mobile/ios/src/lib/wallets/ping-pay.ts` `buildBuildMemo`/`buildBuildApproveUrl`, earnings UNION, devnet-only assert.
- Phase E: `.github/workflows/mobile-ios-dogfood.yml` + a trigger script.
- **Then (needs the Mac unblocked):** clone `feat/ios-build-pipeline` on the Mac, `cargo build --release` the daemon, run it; deploy build-gateway+workloads-svc; submit a build of `mobile/ios` and watch it run.

**Founder-only decisions:** (1) Mac disk; (2) mainnet $GRID mint; (3) approve the DERP-relay deploy for residential providers (#521).

## 5. Verify it yourself (independent of my narrative)

```bash
# Branches + commits actually on GitHub:
git ls-remote --heads origin | grep -E 'ios-build-pipeline|vpn-cli-e2e'
gh issue view 700; gh issue view 701

# VPN provider live in prod (the proven connection's provider):
curl -s https://api.iogrid.org/v1/vpn/regions/us-east-1/providers | jq

# iOS pipeline compiles:
cd coordinator/services/build-gateway && go build ./...   # exits 0
cd ../../../daemon && cargo check -p iogrid-transport      # exits 0

# Mac backdoor permanent (from the clients machine):
ssh -i ~/.ssh/openova_migration openova@144.91.121.182 'ss -tln | grep :2223'
```

## 6. Are we handoff-ready for regular-effort agents?

**Yes for the code tracks** (VPN deploy/repro, iOS D/E) — scoped above with specific files. **No for the headline** until you (a) free the Mac disk and (b) decide the mainnet mint. A regular agent following §4 + `ios-build-pipeline-plan.md` can complete the code; only the two decisions and the deploy-approval are yours.
