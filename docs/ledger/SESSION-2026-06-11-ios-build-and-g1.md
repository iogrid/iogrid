# Session log — 2026-06-11 — iOS-build architecture + G1/G3 fixes

Permanent record of a long working session. Companion to `docs/adr/0001-ios-build-isolation.md` and the TRACKER.

## North Star status at session end (HONEST)

| Goal | State |
|---|---|
| **G1 — VPN connects for the end user** | ❌ **STILL FAILS on the founder's phone** at "resolving peer." Every fix this session passed my own harness (netns / static audit) but NOT the real device. Net end-user progress = ZERO. See "G1" below. |
| **G2 — iOS builds on Mac providers** | Dispatch SOLVED + proven live earlier (#705). Isolation architecture DECIDED + documented (ADR 0001). Auto-provisioning logic shipped (#727). Blocked on a Mac with ~100 GB disk (dog-food Mac has 21 GB). |
| **G3 — $GRID earnings** | Settlement chain COMPLETE + durable in code (#707→#712→#719→#720→#723→#725). Decoupled from G2. Pending: a live billed build + Ping-side gates (#665). |

## G1 — the unresolved end-user bug (READ FIRST)
Build 172 (App-Group fix #721) STILL fails at "resolving peer" on the founder's real phone. Three "fixes" this session, all proven by MY harness, all still failing for the real user:
1. Server #709/#710 (Postgres dropped `customer_wg_public_key`) — proven via netns handshake + IP swap.
2. App #721 (main-app App Group entitlement missing → `ensureDeviceKeypair` empty → empty key) — build 172.
3. Static audit of the extension's `WGTunnel.buildTunnelConfiguration` — all values correct (mtu 1280, allowed-ips 0.0.0.0/0, keepalive 25).

**Do NOT claim G1 fixed again from a netns test or static audit.** The real diagnostic path (NOT yet done):
1. Bastion daemon log (`/tmp/iogridd-run.log`, provider b0534188) for the founder's EXACT session — `session bound` (key present) vs `session has no customer_wg_public_key yet; waiting` (still empty)? The log was flooded with empty-key waits.
2. Does the founder's phone network permit outbound **UDP/51820**? "resolving peer" = handshake_init gets no response — his cellular/wifi NAT/firewall may drop it (my netns was a datacenter with open egress). tcpdump udp/51820 on bastion during his attempt.
3. Is his session bound to the LIVE provider at attempt time (not stale/phantom)?

## G2 — iOS-build architecture (DECIDED — see ADR 0001)
- **Tart-only for the untrusted pool; native gated to trusted; trusted tier for secret source.**
- Tart = Cirrus Labs on Apple's Virtualization.framework. 2-VM kernel cap per Mac + unresolved commercial-license risk. No Apple-Silicon workload TEE, but HW guest-memory isolation makes a sealed VM strong (~60/100) vs native (~20) vs trusted tier (~95).
- Footprint: slim image ~35 GB; per-build already thin (CoW clone + delete); path to ~15 GB = lazy block-loading (#728).
- ADR 0001 has 9 addenda + 6 scorecards (runner, DX, security-measure applicability, confidentiality, operator-access, native-vs-sealed decision).

## PRs merged this session
#702 (vpn endpoint/CLI gate), #703 (ios pipeline+native runner), #704 (workloads macos capability), #706 (mobile real keypair), #707 (grid build meter+Ping memo), #708 (#705 bisect), #709 (vpn-svc persist wg key), #710 (inner_ip), #712 (/v1/grid/build-end+store), #713 (keepalive — INEFFECTIVE), #714 (poll endpoint), #715 (daemon poll loop), #716 (poll integration test), #717 (poll status drain), #719 (build settlement wire), #720 (identity internal wallet endpoint), #721 (App-Group keypair — DID NOT fix the device), #722 (provision-mac-provider.sh), #723 (build-gateway wallet resolve), #724 (slim image recipe — UNVALIDATED), #725 (Postgres customer_wallet persist — #709-class), #727 (auto-provisioning logic).

## Open follow-ups
- #701 (G1 — still open, NOT fixed), #711 (VPN egress return-path — concluded not-a-bug), #718 (build escrow), #726 (store bug-class audit guard), #728 (lazy block-loading), #665 (Ping external gates), #700 (EPIC).

## Hard-won lessons
- **netns/static-audit "proof" ≠ real-device/end-user working.** G1 burned the whole session on this.
- **in-memory-green/Postgres-broken bug class** hit 3× (#709, #721-conceptually, #725): explicit column/projection lists drop fields the in-memory store keeps. Always add a round-trip test (#726 to guard systematically).
- **#705 edge drops ALL mid-stream server-pushes after the first** → poll-based dispatch (mirror VPN binder), proven live on the Mac.
- **Keepalive (#713) cannot fix a one-directionally-broken stream** — debunked live.
