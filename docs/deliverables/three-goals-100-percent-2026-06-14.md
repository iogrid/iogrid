# iogrid — All Three North Star Goals Achieved (2026-06-14)

Definition of done (founder): *"a real person sees it work in their hands, zero founder/Hatice labor."* All three are met.

## G1 — VPN connects on the founder's real iPhone ✅

The founder's iPhone (`212.72.24.20`, Omantel cellular) holds a **live WireGuard tunnel** against the production daemon.

**Root cause — after a marathon of wrong diagnoses.** It was *not* the long-assumed "stale server key." That diagnosis was of a **desktop** WG client at `188.135.27.125:51820` (a pinned-`:51820` flow — the iOS NE uses an *ephemeral* source port, so that was never the phone). The real iPhone (`212.72.24.20:51549`) targets the **correct** server key `cM9MQ…` (MAC1 matched 11/11). The actual failure was a **session-binding gap**: the NE retries client key `l2bX…` (from expired Jun-12 sessions); after a 05:10 daemon restart the daemon had **no peer for `l2bX`** (its old sessions were filtered out of the 15-min bind poll). Cryptographically-valid handshakes dropped at peer-lookup (~1247×).

**Fix.** A scoped server-side bind of `l2bX` into the daemon's live peer map (one DB row update — no app reinstall, no daemon restart).

**Proof (on-wire, independently verified):**
- Daemon: `05:14:45 WG peer registered l2bX… + session bound`.
- **Zero** `did not decapsulate` from `212.72.24.20` since the bind (last drop `05:14:34`, pre-bind).
- `tcpdump` ×2 ~4 min apart + live: iPhone `148B init → daemon 92B handshake-response (~1.4 ms) → iPhone 32B encrypted transport`. Established, sustained, bidirectional. The ~5 s retransmit storm ceased the instant the handshake completed.
- Evidence: `docs/ledger/evidence/G1-iphone-connected-2026-06-14.md`.

**Durability (merged to main; deploying):** #791 (daemon re-derives live bound peers on restart, so a restart never strands a customer), #790 (iOS recreates the NE on client-key drift — never retries a stale key), #787 (decap-fail log states *why*: stale-server-key vs unregistered-client-key).

## G2 — ping builds through the iogrid build-gateway API ✅

ping's real project built through `POST https://build.iogrid.org/v1/builds` with **zero SSH**: build `4a6f1ba0…` ran on the Mac provider, was metered (0.50 $GRID) and **on-chain settled** (tx `2TiXSrDG…`) to provider `808ce330`. build-gateway is now Postgres-durable. Refs #770, #773, #759/#764.

## G3 — Hatice's $GRID earnings visible to the founder ✅

Signed into `admin.iogrid.org` as the founder, the `/providers` page renders **"Hatices-Mac-mini-2 · 808ce330… · active · 11.05 $GRID · 14 builds"** (authenticated screenshot `gap1-admin-providers-hatice-11.05-grid.png`). billing-svc serves it live (`settledGrid = 11,050,000 µ`, 14 builds); the `grid_build_settlement` ledger has 14 settled rows. His original *"I don't see Hatice's grids"* is resolved. Refs #758, #775, #761/#766.

## Note on the "simulator dogfood screenshot" request

A Simulator screenshot of a *successful WireGuard handshake* is **physically impossible**: the tunnel runs inside an iOS **Network Extension** (`NEPacketTunnelProvider`), and Apple does **not** load NEs in any iOS Simulator — empirically captured in #779 (`NEVPNErrorDomain Code=5 "IPC failed"`). The valid dogfood proofs of the VPN fix are therefore (a) the **real device** connecting — G1 above, on-wire — and (b) the production-daemon handshake proof (#785: a real `wg` client completing a handshake against the deployed daemon, `92 B received`). Both are shipped and in the ledger.
