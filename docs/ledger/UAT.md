# iogrid — UAT (canonical test-case index)

Living test-case ledger. Rule: a TC passes only with **hard evidence** (a committed
screenshot, a backend receipt, or a timestamped wire/log capture). No evidence = not ✅.
Symbols: ✅ proven · ⚠️ fix shipped, validation pending · ⏳ unconfirmed · ❌ broken.

Deep-walk records: [`UAT-iogrid-2026-06-15.md`](./UAT-iogrid-2026-06-15.md),
[`UAT-iogrid-2026-06-14.md`](./UAT-iogrid-2026-06-14.md).
VPN wire evidence: [`evidence/G1-vpn-live-wire-2026-06-14.md`](./evidence/G1-vpn-live-wire-2026-06-14.md).

| TC | Surface / capability | Status | Evidence |
|---|---|---|---|
| WEB-1…7 | Web surfaces (provider, customer, account, vpn, welcome, admin) | ✅ **7/7** | `UAT-iogrid-2026-06-15.md` (`709532cb`) — live walk + backend receipts |
| G3 | Admin $GRID earnings render (Hatice 12.325 $GRID / 17 builds) | ✅ | `UAT-iogrid-2026-06-15.md` admin/providers screenshot |
| G2 | iOS build through the public API on a real Mac, exit 0, on-chain $GRID settle | ✅ | #700/#770 closed; builds `6f131695`, `4a6f1ba0` |
| **VPN-DP** | **VPN data plane** — founder's iPhone establishes a real WireGuard tunnel | ✅ **PROVEN on-wire** | `evidence/G1-vpn-live-wire-2026-06-14.md` (`099f9c78`): **3 handshakes 19:12:58 / 19:15:06 / 19:17:15** (148→92, accepted), ~6 min sustained bidirectional transport, **0 decap-drops** |
| **#701** | NE gates "Connected" on a *real* handshake (no fake/black-hole) | ⚠️ **code fixed + gate logic PROVEN; device-validation pending** | **Native gate shipped — PR #812** (CI validating the Swift build): `startTunnel` now waits for a confirmed handshake before signaling connected, and **fails** rather than reporting a fake tunnel on timeout (the native half #786 never touched). **Gate logic PROVEN** — `handshakeConfirmed` truth table **4/4**: interface-up-without-handshake (`last_handshake_time_sec=0`) → connected=**FALSE** (the fake branch is dead). #786 = the TS/UI layer. Full NE flow validates **on-device** (next build) — an NE **cannot run in any simulator**; there is **no "iogrid simulator"** (verified: no such script/tool exists). |
| **#789** | iOS NE self-heals on stale WG client-key drift | ⚠️ **NOT ✅** | fix #790 merged → Build 190; device-validation **PENDING install** |
| VPN-E2E | End-user actually browses through the tunnel (egress IP = server) | ⏳ **UNCONFIRMED** | awaiting founder's browse check (tunnel is up per VPN-DP; routing/DNS not yet verified) |
| VPN-DUR | Tunnel survives a daemon restart without manual rebind | ⏳ **UNCONFIRMED** | morning bind was manual + did not survive a restart; current tunnel re-established after a manual toggle |

**Honest summary (2026-06-14):** Web + G2 + G3 = ✅ with committed evidence. VPN
data-plane = ✅ proven live on the founder's device (first time today). The VPN is
**not** declared "done": end-to-end browsing, restart-durability, and the client-side
#701/#789 fixes remain ⚠️/⏳ until validated on Build 190 and confirmed by the founder.
