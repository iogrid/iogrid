# proxy-gateway

Customer-facing SOCKS5 / HTTP CONNECT proxy at `proxy.iogrid.org:443`.

This service is the **only** public ingress for bandwidth-workload customers.
It terminates TLS, authenticates the customer's API key, runs the pre-flight
anti-abuse pipeline mandated by [`docs/LEGAL.md`](../../../docs/LEGAL.md),
asks `workloads-svc` to assign a provider, and relays bytes between the
customer's connection and the chosen provider's WireGuard tunnel endpoint.

It never inspects content — neither HTTP headers nor TLS payload — consistent
with the common-carrier defence described in `docs/LEGAL.md`.

---

## Protocol surface

A single TCP listener accepts both protocols on the same port; the first byte
of the byte stream disambiguates:

| First byte | Protocol |
|------------|----------|
| `0x05`     | SOCKS5 greeting (RFC 1928 + RFC 1929 user/pass sub-negotiation) |
| `C` / `c`  | HTTP `CONNECT host:port HTTP/1.1` |

### SOCKS5

1. Client sends greeting offering `AuthUserPass (0x02)`.
2. Server replies selecting `AuthUserPass`.
3. Client sends RFC 1929 user/pass — **username is the workspace handle
   (optional, audit-only); password is the API key**.
4. Server replies `0x00` on success or `0x01` and closes on failure.
5. Client sends `CONNECT atyp host port` — IPv4, IPv6, or DOMAIN address types.
6. Server replies `0x00 succeeded` once the provider connection is ready;
   relay begins immediately.

Other failure replies:

| Code | Meaning |
|------|---------|
| `0x01` | general failure (dispatch returned no provider, dial timeout) |
| `0x02` | connection not allowed by ruleset (port blocked / anti-abuse block / rate-limited) |
| `0x07` | command not supported (BIND / UDP ASSOCIATE) |
| `0x08` | address type not supported |

### HTTP CONNECT

1. Client sends `CONNECT host:port HTTP/1.1\r\n` + headers, including
   `Proxy-Authorization: Basic base64(workspace:api_key)`.
2. Server replies `200 Connection Established` on success.
3. Other replies:

| Status | Meaning |
|--------|---------|
| `400 Bad Request` | malformed CONNECT line |
| `403 Forbidden` | port blocked / anti-abuse BLOCK |
| `407 Proxy Authentication Required` | missing or invalid `Proxy-Authorization` |
| `429 Too Many Requests` | anti-abuse RATE_LIMIT |
| `502 Bad Gateway` | no eligible provider / all failover attempts exhausted |

---

## Pipeline per accepted connection

```
+--------------------+    +-----------+    +-----------------+    +-------------+
| TLS terminate      |--> | protocol  |--> | API key auth    |--> | port allow  |
| (LISTEN_ADDR)      |    | detect    |    | (billing-svc)   |    | / block     |
+--------------------+    +-----------+    +-----------------+    +-------------+
                                                                       |
+------------------+    +-----------------+    +-------------------+   |
| relay (bidir +   |<-- | dial provider   |<-- | dispatch          |<--+
|  metering 1MiB)  |    | (failover x3)   |    | (workloads-svc +  |
+------------------+    +-----------------+    |  sticky-session)  |
        |                                      +-------------------+
        v
+----------------+   +-------------------+
| BILLING stream |   | AUDIT stream      |
| (NATS JetStr.) |   | (NATS JetStream)  |
+----------------+   +-------------------+
```

### Customer auth

API key validation happens against `billing-svc`'s `ValidateApiKey` RPC. Until
that wire format lands the proxy ships with an in-memory `auth.Static`
implementation seeded from `DEV_API_KEYS` (format `key1=workspace1;key2=workspace2`).

### Sticky sessions

Per `docs/ARCHITECTURE.md`, the same `(customer_id, destination)` pair routes
to the **same provider** for up to 30 minutes (configurable via `SESSION_TTL`).
The ledger lives in Redis (`REDIS_URL`) when set, falling back to an in-memory
map. Sticky bindings are invalidated automatically when the bound provider's
endpoint refuses connection, transparently failing over to the next provider.

### Anti-abuse pre-flight

Every accepted connection — SOCKS5 or HTTP CONNECT — calls
`antiabuse-svc.CheckUrl` BEFORE any bytes are relayed. The check covers:

- CSAM hash (NCMEC PhotoDNA)
- Phishing feeds (PhishTank, OpenPhish, Google Safe Browsing)
- Per-customer + per-provider rate limits (Redis token bucket inside antiabuse-svc)
- Domain class (banking → KYC required, `.gov`/`.mil` → block, adult → opt-in required)

The check **fails closed** — RPC errors return `DecisionBlock` so a misconfigured
upstream never opens the door.

### Failover

If the provider connection refuses (TCP reset, dial timeout), the proxy:

1. Invalidates the sticky-session binding so it doesn't keep picking the dead provider.
2. Emits a `failover` audit event with the failing `provider_id`.
3. Adds the provider to the excluded set and re-dispatches.

`MAX_FAILOVER_ATTEMPTS` caps the loop (default 3). From the customer's perspective
this looks like a single one-time connection reset followed by working relay.

### Metering

Every `METER_BYTES_EVERY` bytes (default 1 MiB) the relay emits a
`BillingEvent` to the JetStream `BILLING` stream on subject
`iogrid.billing.bandwidth.bandwidth`. A terminal emission is forced when the
relay ends.

```json
{
  "timestamp": "2026-05-19T12:34:56.789Z",
  "customer_id": "cust-...",
  "workspace_id": "ws-...",
  "provider_id": "prov-...",
  "workload_type": "bandwidth",
  "bytes_in": 1048576,
  "bytes_out": 524288,
  "session_id": "...",
  "workload_id": "..."
}
```

### Audit emission

Every accept / reject / relay-start / relay-end / failover emits an
`AuditEvent` to the JetStream `AUDIT` stream (`iogrid.audit.proxy.<event_kind>`).
Retention: 90 days per `docs/LEGAL.md`.

---

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `LISTEN_ADDR` | `:443` | Customer-facing SOCKS5/HTTP-CONNECT bind |
| `HEALTH_ADDR` | `:8080` | HTTP control plane (/healthz /readyz /metrics) |
| `TLS_CERT_PATH` | (empty) | Path to TLS cert PEM. When set, listener terminates TLS. |
| `TLS_KEY_PATH` | (empty) | Path to TLS private key PEM. Required if `TLS_CERT_PATH` is set. |
| `WORKLOADS_SVC_URL` | (empty) | Base URL of `workloads-svc` Connect-RPC. Empty enables in-memory static pool (DEV ONLY). |
| `ANTIABUSE_SVC_URL` | (empty) | Base URL of `antiabuse-svc`. Empty bypasses anti-abuse (DEV ONLY — warns at startup). |
| `BILLING_SVC_URL` | (empty) | Base URL of `billing-svc`. Empty falls back to `DEV_API_KEYS`. |
| `REDIS_URL` | (empty) | Redis sticky-session ledger. Empty falls back to in-memory map. |
| `NATS_URL` | (empty) | NATS JetStream for AUDIT + BILLING streams. Empty falls back to slog. |
| `SESSION_TTL` | `30m` | Sticky-session ledger TTL. |
| `METER_BYTES_EVERY` | `1048576` (1 MiB) | Byte interval between BillingEvent emissions. |
| `MAX_FAILOVER_ATTEMPTS` | `3` | Cap on dispatch + dial retries per accepted connection. |
| `IDLE_TIMEOUT` | `5m` | Tear down the relay if no bytes flow in either direction. |
| `DIAL_TIMEOUT` | `10s` | Per-provider dial deadline. |
| `ALLOW_PORTS` | (empty) | When set, comma-separated whitelist of destination ports. |
| `BLOCK_PORTS` | `25,465,587,2525,6667,6697,9001,9030` | docs/LEGAL.md outbound port blocklist (SMTP, IRC, Tor exit). |
| `DEV_API_KEYS` | (empty) | Dev-only seed `key=workspace;key=workspace`. |
| `DEV_PROVIDER_ENDPOINT` | (empty) | Dev-only static provider endpoint when `WORKLOADS_SVC_URL` is unset. |

---

## Local development

The binary boots with no env vars set (TLS disabled, in-memory stubs throughout):

```bash
cd coordinator/services/proxy-gateway
DEV_API_KEYS="sk_dev=ws-dev" \
DEV_PROVIDER_ENDPOINT="127.0.0.1:9000" \
go run ./cmd/proxy-gateway
```

Then run a SOCKS5-aware client:

```bash
curl --socks5 ws-dev:sk_dev@127.0.0.1:443 https://example.com
```

Or HTTP CONNECT:

```bash
curl --proxy http://ws-dev:sk_dev@127.0.0.1:443 https://example.com
```

---

## Testing

```bash
go test ./...           # unit + integration tests
go test ./... -race     # race detector
```

Integration tests (`internal/proxy/integration_test.go`) spin up:

- a test `tcp echo server` (the "provider")
- an in-memory `dispatch.StaticPool` (the "workloads-svc")
- an `abuse.StaticFilter` (the "antiabuse-svc")
- an in-memory `sessions.Memory` (the "Redis")
- the real `proxy.Server`

and exercise the full SOCKS5 + HTTP-CONNECT handshake → auth → preflight →
dispatch → relay flow end-to-end, plus blocked-destination, blocked-port,
401/403/407/429 error paths, failover, and sticky-session pinning.

---

## Throughput design constraints

- **Concurrency model.** One goroutine per accepted TCP connection (plus 2
  per relay session for the bidirectional copy and 1 per session for the idle
  watchdog when `IDLE_TIMEOUT > 0`). The Go runtime handles ~100K concurrent
  connections per pod before scheduler overhead dominates; we expect to scale
  out horizontally via k8s Deployment replicas long before any single Pod hits
  that ceiling.
- **Copy buffer.** 32 KiB per direction is the default — large enough to keep
  syscall overhead amortised, small enough that 10K concurrent sessions fit
  comfortably in 640 MiB of buffer memory.
- **Metering granularity.** Default 1 MiB threshold gives ≤1 publish per MiB
  per direction → ~1 NATS publish/sec/customer at 8 Mbps sustained. NATS
  JetStream handles 100K msg/sec on a single 50 MiB pod, so we have ~5 orders
  of magnitude of headroom before metering becomes the bottleneck.
- **Anti-abuse RPC.** One `CheckUrl` per accepted connection (NOT per byte).
  At antiabuse-svc's published budget of 1000 RPS per pod, a single
  proxy-gateway Pod can sustain ~1000 new-connection-per-second peak with one
  antiabuse-svc replica.
- **Sticky-session lookup.** Two Redis round-trips per accepted connection
  (Get + Put). Redis Cluster handles ~100K ops/sec/shard; one shard covers
  ~50K new-connections-per-second from the proxy-gateway fleet.
- **TLS termination.** The Go standard library `crypto/tls` handshake costs
  ~2 ms CPU on cpx52. At 100 % new-handshake load a single core sustains
  ~500 handshakes/sec; in practice TLS session resumption + HTTP/2 keepalives
  drop the actual rate by 10–100×.
- **Hard ceilings.**
  - Maximum 64 headers + 16 KiB header block per HTTP CONNECT request.
  - Maximum 30s handshake deadline (slow-loris cap).
  - Maximum 3 failover attempts per accepted connection (configurable).

---

## Files of interest

| Path | Role |
|------|------|
| `cmd/proxy-gateway/main.go` | Composition root — wires every dependency, runs the two listeners. |
| `internal/config/config.go` | Env-var contract + defaults. |
| `internal/proxy/proxy.go` | Top-level accept loop + per-connection pipeline. |
| `internal/socks5/socks5.go` | RFC 1928 + RFC 1929 protocol parser. |
| `internal/httpconnect/httpconnect.go` | HTTP CONNECT parser. |
| `internal/auth/auth.go` | API key validator interface + Static impl. |
| `internal/abuse/abuse.go` | antiabuse-svc Connect-RPC client wrapper. |
| `internal/dispatch/dispatch.go` | workloads-svc Connect-RPC client + StaticPool failover dispatcher. |
| `internal/sessions/sessions.go` | Redis + in-memory sticky-session ledger. |
| `internal/relay/relay.go` | Bidirectional byte pump with periodic meter callback. |
| `internal/audit/audit.go` | JetStream AUDIT + BILLING emitter (slog fallback). |

---

## Troubleshooting

- **Customer gets repeated `0x02 ConnNotAllowed` SOCKS5 replies.**
  - Either the destination port is in `BLOCK_PORTS` (docs/LEGAL.md mandate),
    or `antiabuse-svc` is returning a BLOCK / RATE_LIMIT verdict. Check the
    `AUDIT` stream subject `iogrid.audit.proxy.rejected` for the reason slug.
- **Customer gets `407 Proxy Authentication Required` even with a key.**
  - Verify the `Proxy-Authorization: Basic base64(user:key)` header is well-formed.
    The `Bearer` scheme is NOT supported on the SOCKS5/HTTP-CONNECT seam.
- **All connections fail with `502 Bad Gateway`.**
  - Check `WORKLOADS_SVC_URL` connectivity. With no `WORKLOADS_SVC_URL` set
    the proxy uses an empty `StaticPool` and every dispatch returns
    `ErrNoEligibleProvider`. Set `DEV_PROVIDER_ENDPOINT` for local smoke tests.
- **Bytes flow but no BILLING events show up in JetStream.**
  - Check `NATS_URL` and that the `BILLING` stream was created at boot
    (the binary logs `proxy audit/billing emitter using NATS JetStream` on
    success). Without `NATS_URL` the events fall back to slog-only at DEBUG.
- **Sticky session keeps flipping providers.**
  - `REDIS_URL` is probably unset and the in-memory ledger doesn't survive
    pod restarts. In multi-replica deployments this also means each replica
    has its own non-shared map. Set `REDIS_URL` for cross-pod stickiness.
