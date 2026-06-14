# VPN peer resolution — infra-level end-to-end verification (PROD daemon, real WG client)

**Date:** 2026-06-14
**Host:** prod VPN provider `144.91.121.182` (daemon `iogridd` pid 685469, provider `b0534188-3377-4b3b-a47c-38e1017ad600`, server WG pubkey `cM9MQKfzK6sPlGqa99xyNnHqDZ/vYzbM/5+z0Ez2Gzs=`)
**Method:** a real WireGuard client (Linux network namespace, throwaway keypair) registered as a customer session via the **production** vpn-svc mobile flow, handshaking the **live** prod daemon. No live-daemon restart, no key change.
**Issue:** [#781](https://github.com/iogrid/iogrid/issues/781) (root cause), refs #701, G1.

---

## TL;DR — does peer resolution succeed server-side? **NO — and we found exactly why.**

A correctly-registered WG client with the **right** server key (`cM9MQ…`) **still does not complete the WG handshake** against the prod daemon. The daemon answers every handshake initiation with a 64-byte **cookie reply** (WG message type 3) and the tunnel stays at **`0 B received`**.

This is **not** a client-side stale-key problem (the premise that only the founder's old build is at fault is **disproven** here: a freshly-generated, correctly-keyed client fails identically). It is a **real, isolated, fixable server-side daemon bug**:

> **The daemon never calls boringtun's `RateLimiter::reset_count()`.** boringtun documents this must run ~once/second. Without it, each peer's handshake-packet counter increments forever; after just **10** packets (`PEER_HANDSHAKE_RATE_LIMIT = 10`) `is_under_load()` latches `true` **permanently**, and `verify_packet` answers every handshake initiation with a cookie reply that never resolves into a completed handshake — for **every** client.

The founder's phone alone floods one 148-byte handshake init ~every 1.4 s, so the prod limiter crossed the threshold hours ago and stays latched. **This is the server-side root cause of the long-standing "did not decapsulate" / "tunnel established, no real handshake" (#701) / on-device G1 VPN failures.**

---

## What the verification PROVED works (the parts up to the cookie loop)

The control-plane and peer-registration path are **correct end-to-end** — the failure is isolated to the WG rate-limiter, the very last step:

1. **Session create (prod mobile flow).** `POST https://api.iogrid.org/v1/vpn/sessions/mobile` with a fresh client pubkey + a 16-digit register-on-first-use account number returned **201** with:
   - `peer_public_key` = `cM9MQKfzK6sPlGqa99xyNnHqDZ/vYzbM/5+z0Ez2Gzs=` — **matches the prod daemon's server key exactly** ✓
   - `peer_endpoint` = `144.91.121.182:51820` ✓
   - `inner_ip` = `10.66.176.94` ✓
   - `session_id` = `4a4765fd-e42b-48bd-b47b-c51787fcefdf`
2. **Daemon bound the customer peer.** ~5 s later the daemon's `peer_binder` logged:
   ```
   session bound — customer peer upserted + provider key posted
   session_id=4a4765fd-… customer_id=9470c8e8-… region=us-east-1
   ```
   So the client pubkey is a live peer in the boringtun map (`upsert_peer` ran), and the provider key was posted back.
3. **MAC1 is valid → peer IS resolved.** The daemon **replies** to the client's handshake init (it doesn't drop it as "did not decapsulate"). A reply at all means the init's **MAC1 against `cM9MQ…` verified** — i.e. the client's configured peer key is correct and the responder recognises the packet. Peer resolution in the cryptographic-identity sense **succeeds**; it is the rate-limiter that then refuses to finish.

(A second, independently-registered fresh peer — session `45b7bf28-…`, inner_ip `10.66.176.95` — was also bound and exhibits the identical cookie loop, confirming the failure is not specific to one client.)

---

## The failure, captured on the wire (prod)

`tcpdump` on the client↔daemon path, real `wg` kernel client, correct key:

```
04:41:24.014010 IP 10.231.0.2.51261 > 10.231.0.1.51820: UDP, length 148   # handshake INIT (type 0x01)
04:41:24.014380 IP 10.231.0.1.51820 > 10.231.0.2.51261: UDP, length 64    # daemon reply, payload:
        0x0010:  0ae7 0002 ca6c c83d 0048 162a 0300 0000   # 0300 0000 = WG msg type 0x03 = COOKIE REPLY
04:41:29.506033 IP 10.231.0.2.51261 > 10.231.0.1.51820: UDP, length 148   # retry INIT (with cookie)
04:41:29.506743 IP 10.231.0.1.51820 > 10.231.0.2.51261: UDP, length 64    # ... cookie reply AGAIN
```

`wg show` on the client after seconds of attempts (full capture in `evidence/prod-wire-capture.txt`):

```
peer: cM9MQKfzK6sPlGqa99xyNnHqDZ/vYzbM/5+z0Ez2Gzs=
  endpoint: 10.231.0.1:51820
  allowed ips: 0.0.0.0/0
  transfer: 0 B received, 13.73 KiB sent      # <-- 0 B received, NO "latest handshake" line
```

A successful handshake would show a **92-byte handshake response (type 0x02)** and **`X B received`** plus a `latest handshake:` timestamp. We get neither — only the 64-byte cookie reply, forever.

> Note: the daemon was reached via a veth host-side address (`10.231.0.1:51820`; the daemon binds `0.0.0.0:51820`) to avoid a NAT hairpin to the host's own public IP. The exchange is byte-identical to a public-IP client; the daemon's reply path is exercised exactly as for any external peer.

---

## Root cause, isolated by A/B (same boringtun rev, only `reset_count` toggled)

boringtun rev `253f7afb2b` (`cloudflare/boringtun`), server `Tunn` built **exactly as the daemon builds it** (`Tunn::new(static_private, client_pub, None, None, idx, None)` — 6th arg `rate_limiter = None`, so boringtun creates its **own default** `RateLimiter::new(&static_public, PEER_HANDSHAKE_RATE_LIMIT=10)`). The daemon's pump (`daemon/crates/routing/src/boringtun_impl.rs::run_pump`) trial-decapsulates each datagram against every peer and forwards the first `WriteToNetwork` — **and never calls `reset_count()` on any limiter** (grep: zero references in `daemon/crates/routing/`).

The decisive A/B — the **only** difference between the two runs is whether `reset_count()` is pumped ~once/second (full output in `evidence/boringtun-rate-limiter-repro-output.txt`, source `evidence/boringtun-rate-limiter-repro.rs`):

```
[A/B daemon as-is (no reset_count)] pump_reset=false => COMPLETED=false cookie_replies=1 handshake_responses=0
[A/B with reset_count pumped (fix)] pump_reset=true  => COMPLETED=true  cookie_replies=0 handshake_responses=1
```

And the threshold behaviour:

```
[FOUR peers, under-load (prod-like)]      preload_underload=true  => COMPLETED=false  cookie_replies=1  handshake_responses=0
[FOUR peers, NOT under-load (fresh daemon)] preload_underload=false => COMPLETED=true  cookie_replies=0  handshake_responses=1
```

A **fresh** daemon (count < 10) completes the handshake; an **under-load** daemon (count ≥ 10, never reset) does not. This reproduces the prod wire capture exactly.

### Why boringtun does this

`boringtun/src/noise/rate_limiter.rs`:
- `RESET_PERIOD = 1` second; `reset_count()` zeroes `count` but **only if you call it**: *"ideally should be called with a period of 1 second"*.
- `is_under_load()` = `self.count.fetch_add(1) >= self.limit` — increments on **every** packet, never self-resets.
- `verify_packet(Some(addr), …)` when under load: if the init's MAC2 ≠ the current per-limiter cookie → returns a cookie reply (the 64-byte packet).

`boringtun/src/noise/mod.rs`: `Tunn::new(..., rate_limiter=None)` → `RateLimiter::new(&static_public, PEER_HANDSHAKE_RATE_LIMIT)` with `PEER_HANDSHAKE_RATE_LIMIT = 10`.

---

## The fix (for #781)

Pump `reset_count()` ~once/second. boringtun's intended pattern: build **one shared** `RateLimiter`, pass it to every `Tunn::new(..., Some(limiter))`, and run a 1 s `tokio::time::interval` task calling `limiter.reset_count()` (same cadence as `Tunn::update_timers`). Today each peer silently constructs its own internal default limiter and none is ever reset, so the responder DoS-mitigation latches on and never lets go.

This is **independent of** the client-side build-185 stale-key work: even after the client bakes the correct key, the handshake cannot complete until the daemon pumps `reset_count()`.

---

## Verdict

| Question | Answer |
|---|---|
| Did the prod mobile flow create a session + bind the peer? | **Yes** — 201, server key `cM9MQ…`, `session bound` logged. |
| Is the client's peer cryptographically resolved (MAC1 valid)? | **Yes** — the daemon replies to the init (not "did not decapsulate"). |
| Did the WG handshake COMPLETE (bidirectional bytes > 0, latest-handshake set)? | **No** — `0 B received`; daemon emits only 64-byte cookie replies. |
| Root cause? | **Daemon never calls `RateLimiter::reset_count()`** → responder permanently "under load" after 10 packets → cookie reply on every init. Isolated by A/B. → [#781](https://github.com/iogrid/iogrid/issues/781). |
| Is it the client's stale key (build-185 premise)? | **No** — a freshly, correctly-keyed client fails identically. |

**Peer resolution succeeds server-side at the identity/registration layer, but the WG handshake does NOT complete** due to the un-pumped boringtun rate limiter. Fix tracked in #781; until it lands, no client (phone or otherwise) can establish the tunnel against this daemon.

---

### Evidence files
- `evidence/prod-wire-capture.txt` — prod `tcpdump` (148B init → 64B cookie reply) + `wg show` (`0 B received`).
- `evidence/daemon-log-session-bound-and-decap.txt` — `session bound …` for both test sessions + the phone's decap-drop flood.
- `evidence/boringtun-rate-limiter-repro.rs` — faithful A/B reproduction (same boringtun rev, daemon-identical `Tunn::new`).
- `evidence/boringtun-rate-limiter-repro-output.txt` — A/B output proving `reset_count` is the sole variable.
