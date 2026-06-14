# #781 PROD handshake proof — the deployed daemon now completes real WG handshakes

**Date:** 2026-06-14
**Scope:** Re-run the EXACT test that originally found the bug (agent `a56ee58b`), now against the **deployed, fixed** prod daemon.
**Issue:** [#781](https://github.com/iogrid/iogrid/issues/781) (RESOLVED via PR #783, merged `fed1d2b7`), refs #701, G1.

---

## What this proves (and what it does NOT)

> **This proves the SERVER-SIDE #781 rate-limiter fix works end-to-end on the live production daemon.** A real WireGuard **kernel** client, freshly and correctly keyed, registered through the production mobile flow, completes a full WG handshake against the deployed daemon on `144.91.121.182:51820` — bytes flow both ways.

It does **NOT** prove the founder's phone connects. His iPhone separately needs **build 185** (a stale *server* key is baked into his iOS Network Extension — proven by three prior agents, tracked in #762/#701). That is a distinct, client-side track upstream of the rate limiter.

**Claim:** the deployed daemon now completes real WG handshakes — the #781 fix is live and working. **Not** "G1 done".

---

## The before/after — same test, opposite result

The pre-fix run (`a56ee58b`, captured in [`evidence/prod-wire-capture.txt`](evidence/prod-wire-capture.txt)) registered a correctly-keyed fresh client and got a permanent **cookie loop**: every 148-byte handshake init was answered with a 64-byte **type-0x03 cookie reply**, `wg show` stuck at **`0 B received`**, no `latest handshake`. That was the bug (the daemon never pumped boringtun `RateLimiter::reset_count()`, so `is_under_load()` latched `true` after 10 packets and cookie-replied forever).

This run is the **exact inverse**:

| Observation | Pre-fix (`a56ee58b`) | **Post-fix (this run)** |
|---|---|---|
| 148B init → daemon reply | **64B type-0x03 COOKIE REPLY** | **92B type-0x02 HANDSHAKE RESPONSE** |
| `wg show` transfer | **`0 B received`** | **`92 B received`** |
| `latest handshake` line | **ABSENT** | **PRESENT (`22 seconds ago`)** |
| Tunnel state | never establishes | **established (transport keepalives flow)** |

---

## Test setup

- **Live prod daemon (UNCHANGED throughout):** pid `863603`, `/usr/local/bin/iogridd --state-dir /var/lib/iogridd --vpn-svc https://api.iogrid.org --region us-east-1 --public-ip 144.91.121.182`, binary sha256 `2eb1d39b066b9de0918bced3ba28ff5f2cf820c6c8c0ccc37f41b9c083e7b5e1` (== the #781 deploy comment's fixed binary), UDP `0.0.0.0:51820`, server pubkey `cM9MQKfzK6sPlGqa99xyNnHqDZ/vYzbM/5+z0Ez2Gzs=`. Provider `b0534188-3377-4b3b-a47c-38e1017ad600`. **No restart, no key change** (the founder was actively connecting on 188.135.27.125).
- **Client:** a real **kernel** WireGuard interface (`wg`, `wireguard` kmod loaded) in a throwaway Linux network namespace with a freshly generated X25519 keypair — the same gold-standard client class that found the bug.
- **Reachability:** the netns reaches the daemon via a host-side veth IP (`10.241.0.1:51820`); the daemon binds `0.0.0.0:51820` so its reply path is exercised byte-identically to any external peer (same technique as the pre-fix capture, avoids a NAT hairpin to the host's own public IP).

---

## Step 1 — session created via the PRODUCTION mobile flow

`POST https://api.iogrid.org/v1/vpn/sessions/mobile` with the fresh client pubkey + a 16-digit register-on-first-use consumer account number as the `api_key` (Mullvad model #569 — the account number IS the credential; the `ConsumerRegisteringValidator` self-registers it then re-validates) → **`201`**:

```json
{
  "session_id": "61539b4e-1345-4ee0-8932-608dc075430b",
  "client_public_key": "kAdp0fPmGuyCh3xRdzJgJyg7sMAcquELhzdyBI/EoWQ=",
  "peer_public_key": "cM9MQKfzK6sPlGqa99xyNnHqDZ/vYzbM/5+z0Ez2Gzs=",   ← MATCHES prod server key ✓
  "peer_endpoint": "144.91.121.182:51820",                              ← prod daemon ✓
  "customer_inner_cidr": "10.66.176.100/32"
}
```

## Step 2 — daemon bound the customer peer

~5 s later the daemon's `peer_binder` logged the bind for **this exact session** (full line in [`evidence/wg781-prod-postfix-capture.txt`](evidence/wg781-prod-postfix-capture.txt)):

```json
{"timestamp":"2026-06-14T04:33:35.377990Z","level":"INFO",
 "fields":{"message":"session bound — customer peer upserted + provider key posted",
 "session_id":"61539b4e-1345-4ee0-8932-608dc075430b",
 "customer_id":"9d0582db-f48d-52bf-b49a-11ced0a92361","region":"us-east-1"},
 "target":"iogrid_routing::peer_binder"}
```

So the client pubkey is a live peer in the daemon's boringtun map (`upsert_peer` ran).

## Step 3 — the handshake COMPLETES (this is the #781 fix working)

`wg show` on the client after triggering a handshake:

```
interface: wgt0
  public key: kAdp0fPmGuyCh3xRdzJgJyg7sMAcquELhzdyBI/EoWQ=
  private key: (hidden)
  listening port: 55343

peer: cM9MQKfzK6sPlGqa99xyNnHqDZ/vYzbM/5+z0Ez2Gzs=
  endpoint: 10.241.0.1:51820
  allowed ips: 0.0.0.0/0
  latest handshake: 22 seconds ago          ← PRESENT (pre-fix: ABSENT)
  transfer: 92 B received, 308 B sent        ← 92 B RECEIVED (pre-fix: 0 B received)
  persistent keepalive: every 5 seconds
```

## Step 4 — on the wire: a real 0x02 RESPONSE, not a 0x03 cookie

`tcpdump` on the host-side veth, capturing a fresh handshake, decoded by WG message type (first byte of UDP payload):

```
 # direction        udp_payload_len  wg_msg_type
 0 client->daemon               148  0x01 HANDSHAKE_INIT       <== handshake INIT
 1 daemon->CLIENT                92  0x02 HANDSHAKE_RESPONSE   <== handshake RESPONSE (type 0x02), NOT a 0x03 cookie
 2 client->daemon                32  0x04 TRANSPORT_DATA
 3 client->daemon                32  0x04 TRANSPORT_DATA
 4 client->daemon                32  0x04 TRANSPORT_DATA

 daemon HANDSHAKE_RESPONSE (0x02) count = 1
 daemon COOKIE_REPLY      (0x03) count = 0
 RESULT: PASS — daemon sends real handshake responses (fix live)
```

Raw wire (full hexdump in [`evidence/wg781-prod-postfix-capture.txt`](evidence/wg781-prod-postfix-capture.txt)):

```
06:35:20.073906 IP 10.241.0.2.55343 > 10.241.0.1.51820: UDP, length 148   # INIT
	0x0010:  0af1 0001 d82f ca6c 009c 1692 0100 0000   #   ...... 0100 0000 = WG type 0x01 (init)
06:35:20.074635 IP 10.241.0.1.51820 > 10.241.0.2.55343: UDP, length 92    # daemon reply
	0x0010:  0af1 0002 ca6c d82f 0064 165a 0200 0000   #   ...... 0200 0000 = WG type 0x02 (RESPONSE)
```

The pre-fix daemon answered the identical 148B init with a **64-byte** packet whose type field was **`0300 0000` (0x03 cookie reply)**. The fixed daemon answers with a **92-byte type-0x02 handshake response** — the Noise handshake's second message — and transport data (type 0x04) then flows. Zero cookie replies.

---

## Verdict

| Question | Answer |
|---|---|
| Did the prod mobile flow create a session + bind the peer? | **Yes** — 201, server key `cM9MQ…`, `session bound` logged for `61539b4e`. |
| Did the daemon answer the init with a real handshake RESPONSE (0x02), not a cookie (0x03)? | **Yes** — 1× type-0x02, 0× type-0x03 on the wire. |
| Did the WG handshake COMPLETE (bytes received > 0, `latest handshake` set)? | **Yes** — `92 B received`, `latest handshake: 22 s ago`. |
| **Does the deployed prod daemon complete a real WG handshake?** | **YES.** |
| Does this prove the founder's phone connects? | **No** — his iPhone needs build 185 (stale *server* key baked in the NE, #762/#701). Separate client-side track. |

**The #781 server-side rate-limiter fix is live on the production daemon and working: a real WireGuard client completes a full handshake against `144.91.121.182:51820`, bytes flow both ways.**

---

### Reproduction & cleanup

The test ran a throwaway netns + veth + a single MASQUERADE rule on the prod host, all **removed afterward** (`ip netns del wg781p`, `ip link del wgp_h`, `iptables -t nat -D …`). The daemon was never restarted and its key never changed (re-verified post-test: pid `863603`, `:51820` bound, key `cM9MQ…`). The test session `61539b4e` was left to expire on its own 24 h TTL — no DB mutation.

### Evidence files
- [`evidence/wg781-prod-postfix-capture.txt`](evidence/wg781-prod-postfix-capture.txt) — post-fix prod `tcpdump` (148B init → **92B type-0x02 response**), the decoded msg-type table, `wg show` (`92 B received`, `latest handshake`), and the daemon `session bound` log for session `61539b4e`.
- [`evidence/prod-wire-capture.txt`](evidence/prod-wire-capture.txt) — the **pre-fix** capture for direct comparison (148B init → 64B type-0x03 cookie, `0 B received`).
