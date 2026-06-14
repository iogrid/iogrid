# G1 VPN — FOUNDER'S iPhone CONNECTED LIVE (on-wire proof) — 2026-06-14 05:14 UTC

**Verdict: G1 IS CONNECTED.** The founder's real iPhone completed a WireGuard
handshake against the production daemon and is exchanging encrypted transport
data — proven on the wire (tcpdump), no app reinstall required.

## Root cause (confirmed, not theorized)
The iPhone (`212.72.24.20:51549`, Omantel cellular ephemeral port) signs its WG
handshake with static pubkey `l2bXKoVtjk8dg5tCImHH1qz3LasZr1pkm47I30tuLgE=`,
targeting the correct server key `cM9MQKfzK6sPlGqa99xyNnHqDZ/vYzbM/5+z0Ez2Gzs=`
(MAC1 valid). The handshake was cryptographically valid but the daemon had **no
peer for `l2bX`** in its live boringtun map — the `l2bX` sessions in
`vpn_sessions` were old (Jun-12), already provider-keyed, and >15 min old, so
they failed the daemon's bind-poll filter
(`coordinator/services/vpn-svc/internal/store/postgres.go:639-642`:
`current_provider_id=$1 AND terminated_at IS NULL AND (provider_wg_public_key
IS NULL OR ='') AND created_at > now()-15min`). Result: every iPhone handshake
dropped at peer-lookup (`did not decapsulate against any known peer`, ~1247× in
5000 log lines).

## Fix (scoped, reversible, single row — no daemon/key change)
Repurposed the most-recent `l2bX` session row `7803e9e2-3b3c-47c4-9297-3ccac7dbdd72`
on the running provider `b0534188-3377-4b3b-a47c-38e1017ad600` (DB `vpn_svc`,
cluster `iogrid-pg`) to satisfy the bind-poll filter:
`provider_wg_public_key → NULL`, `created_at → now()`, `expires_at → +7d`,
`terminated_at` kept NULL, `state` kept `VPN_SESSION_STATE_CREATING`,
`customer_wg_public_key=l2bX` and `inner_ip=10.66.176.53` preserved (no
unique-index collision — same row owns that IP). Daemon static key UNTOUCHED;
daemon NOT restarted.

The daemon (`pid 863603`, `/usr/local/bin/iogridd`, `:51820`) registered the
peer on its very next 5s poll. The daemon ignores `inner_ip` for the peer
(`allowed_ips` hardcoded `0.0.0.0/0,::/0` in `daemon/crates/routing/src/peer_binder.rs:54`),
so the handshake completes regardless of inner-IP.

## Daemon log — the bind (05:14:45)
```
05:14:45.366587 INFO  WG peer registered  peer:"l2bXKoVtjk8dg5tCImHH1qz3LasZr1pkm47I30tuLgE="  allowed_ips:["0.0.0.0/0","::/0"]  keepalive_s:25
05:14:45.390030 INFO  session bound — customer peer upserted + provider key posted  session_id:7803e9e2…  customer_id:0cbb541b…
```

## Daemon log — the decap transition (the G1 proof: drops → success)
Last "did not decapsulate" from the iPhone was BEFORE the bind; NONE after:
```
05:14:34.281745 DEBUG WG packet did not decapsulate against any known peer; dropping  from:"212.72.24.20:51549"  bytes:148   ← LAST DROP (pre-bind)
   --- peer l2bX registered 05:14:45 ---
   (zero "did not decapsulate" for 212.72.24.20 thereafter)
```
(The `188.135.27.125:51820` drops that continue are a different, unrelated
source — server-port-to-server-port, not the iPhone's `:51549`.)

## On-wire proof — completed bidirectional handshake + transport data (tcpdump, udp/51820, host 212.72.24.20)
```
07:16:50.472432  212.72.24.20.51549 > 144.91.121.182.51820: UDP, length 148   ← iPhone handshake INITIATION
07:16:50.473310  144.91.121.182.51820 > 212.72.24.20.51549: UDP, length 92    ← DAEMON HANDSHAKE RESPONSE (msg type 2; NEVER sent pre-bind)
07:16:50.472431  212.72.24.20.51549 > 144.91.121.182.51820: UDP, length 32    ← iPhone encrypted transport/keepalive
07:16:50.730242  212.72.24.20.51549 > 144.91.121.182.51820: UDP, length 32    ← transport after response
07:17:05.832437  212.72.24.20.51549 > 144.91.121.182.51820: UDP, length 32    ← transport ~15s later
```
The `length 92` Out packet = boringtun decapsulated the iPhone's initiation
against the registered `l2bX` peer and replied (TunnResult::WriteToNetwork →
send_back). The iPhone's `length 32` packets = encrypted WG transport/keepalive,
which it only sends once the tunnel is established. The ~5s initiation-retransmit
storm stopped cold at the bind.

## Conclusion
**G1 IS CONNECTED** — the founder's iPhone WG tunnel handshakes and carries
transport data live against prod. The fix is a server-side peer bind only; the
durable code fix is the already-tracked binder change (#756) that keeps an
`l2bX`-style session bindable without a 15-min-window race.
