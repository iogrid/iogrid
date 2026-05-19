# vpn-gateway

Consumer-VPN aggregator. The free / Plus / Pro side of the iogrid mesh.

This microservice terminates customer WireGuard tunnels at the edge of
the iogrid cluster, then routes the customer's outbound traffic through
the same provider pool that `proxy-gateway` consumes.

Customers (iOS / Android / macOS / Windows / Linux) install the WireGuard
client of their choice, import the configuration shipped to them by
`gateway-bff`, and connect to `vpn.iogrid.org:51820`. The vpn-gateway
authenticates the peer against the customer registry, enforces the
tier-specific limits, and forwards the packets out via a provider exit
matching the customer's chosen country.

## Tier matrix

| Tier | Monthly cap | Locations | Ad-block | Kill switch (advisory) | Price |
|------|-------------|-----------|----------|------------------------|-------|
| Free | 2 GB        | 1 (server-picked) | no  | no  | $0       |
| Plus | unlimited   | 30        | no       | no                      | $2.99/mo |
| Pro  | unlimited   | 30        | yes      | yes                     | $4.99/mo |

The canonical definition lives in `internal/tier/tier.go::LimitsFor`.

## Platforms supported

Configuration artefacts are rendered per-platform by `internal/wgconfig`.
The `gateway-bff` endpoint `/api/v1/vpn/config-for-platform` resolves the
customer + platform combination and returns the right artefact:

| Platform | Artefact            | MIME type                              |
|----------|---------------------|----------------------------------------|
| iOS      | `.mobileconfig`     | `application/x-apple-aspen-config`     |
| Android  | `.conf` + QR        | `text/plain; charset=utf-8`            |
| macOS    | `.conf`             | `text/plain; charset=utf-8`            |
| Windows  | `.conf`             | `text/plain; charset=utf-8`            |
| Linux    | `.conf`             | `text/plain; charset=utf-8`            |

The QR payload is always the underlying `.conf` text — clients render the
PNG in-browser from the payload field on the API response.

## Architecture

```
                                customer device
                                ┌──────────────┐
                                │ WG client    │
                                └──────┬───────┘
                                       │ UDP :51820  (vpn.iogrid.org)
                                       ▼
                          ┌───────────────────────┐
                          │  vpn-gateway (this)   │
                          │  ┌─────────────────┐  │
                          │  │ WireGuard svr   │  │
                          │  │ + admit + meter │  │
                          │  └────────┬────────┘  │
                          │           │           │
                          │  ┌────────▼────────┐  │
                          │  │ session sticky  │  │  redis (per-customer 15min)
                          │  └────────┬────────┘  │
                          │           │           │
                          │  ┌────────▼────────┐  │
                          │  │ Pro tier DNS    │  │  StevenBlack list (weekly refresh)
                          │  │ ad/tracker block│  │
                          │  └─────────────────┘  │
                          └────────┬──────────────┘
                                   │ SubmitWorkload(type=bandwidth,
                                   │                geo_preference=<country>)
                                   ▼
                          ┌───────────────────────┐
                          │ workloads-svc         │
                          │   dispatcher          │
                          └────────┬──────────────┘
                                   │
                                   ▼
                            provider exit (home PC / Mac)
                                   │
                                   ▼
                              the internet
```

## Components

| Package | Role |
|---------|------|
| `internal/tier`      | Tier definitions, cap check, country selectability |
| `internal/blocklist` | StevenBlack-style host blocking, trie-backed, weekly refresh |
| `internal/customer`  | In-memory customer registry, pubkey -> customer lookup |
| `internal/session`   | Sticky per-customer provider binding with 15-minute TTL |
| `internal/metering`  | Per-customer byte counter, BILLING NATS event emitter |
| `internal/wireguard` | WireGuard server abstraction (+ in-memory mock) |
| `internal/wgconfig`  | Per-platform config renderer (.conf, .mobileconfig, QR) |
| `internal/server`    | HTTP control plane (admit, DNS resolve, stats, render) |

## HTTP control surface

All routes return JSON. `LISTEN_ADDR` defaults to `:8080`.

| Method | Path | Purpose |
|--------|------|---------|
| GET    | `/healthz`                              | Liveness |
| GET    | `/readyz`                               | Readiness |
| GET    | `/metrics`                              | Prometheus |
| GET    | `/v1/`                                  | Service identity envelope |
| GET    | `/v1/dns/resolve?host=&customer_id=`    | DNS policy probe for the in-tunnel resolver |
| POST   | `/v1/admit`                             | First-handshake admit/reject decision |
| GET    | `/v1/peers/{pubkey}/stats`              | Per-peer byte counters |
| POST   | `/v1/config/render`                     | Per-platform wgconfig render |

## Environment variables

| Variable | Default | Purpose |
|----------|---------|---------|
| `LISTEN_ADDR` | `:8080` | HTTP control surface |
| `WG_LISTEN_ENABLE` | (off) | Set `true` in production to bring up the WG listener |
| `WG_LISTEN_PORT` | `51820` | UDP port for the WG server |
| `SERVER_PUBLIC_KEY_B64` | (empty) | The vpn-gateway's WG server pubkey, base64 |
| `SERVER_ENDPOINT` | `vpn.iogrid.org:51820` | The host:port published to client configs |
| `DNS_ADDRESS` | `10.99.0.1` | The in-tunnel DNS server (CoreDNS sidecar) |
| `BLOCKLIST_FILE` | (none) | Local path; loaded at boot |
| `BLOCKLIST_URL` | (none) | StevenBlack-style URL; loaded at boot and refreshed weekly |
| `SUPPORTED_COUNTRIES` | (30-country default) | Comma-separated ISO alpha-2 codes |

## Customer flow (end-to-end)

1. **Sign-up.** Customer goes to `app.iogrid.org/vpn`, creates an
   account (Google OAuth or magic-link). `identity-svc` creates the user.
2. **Tier choice.** Customer picks Free / Plus / Pro. Plus / Pro pays via
   Stripe Checkout. `billing-svc` records the subscription.
3. **Config download.** Customer picks a platform; the BFF calls
   `vpn-gateway /v1/config/render` with the customer ID + platform.
   The vpn-gateway:
   - Issues the customer a `10.99.X.Y/32` tunnel IP (already allocated
     at signup time by the customer registry).
   - Renders the appropriate artefact (`.conf`, `.mobileconfig`, QR).
   - Embeds the server pubkey + endpoint + DNS.
4. **Connect.** Customer imports the config into their WG client and
   activates the tunnel. First UDP packet hits vpn-gateway.
5. **Admit.** vpn-gateway looks up the customer by pubkey, checks tier
   cap, checks country selectability, binds a sticky provider in
   `internal/session`, returns admit=true to the WG frontend.
6. **Route.** Packets flow: customer -> vpn-gateway -> workloads-svc
   bandwidth dispatch -> provider exit -> the internet. Per-byte counters
   are accumulated in `internal/metering` and flushed to NATS BILLING
   subjects on a fixed cadence.
7. **Cap.** When a Free user's month-to-date crosses 2 GB, `tier.OverCap`
   returns true on the next handshake; admit=false reason=`MONTHLY_CAP_EXCEEDED`,
   and the client surfaces an upgrade prompt.
8. **Ad-block (Pro).** The in-tunnel CoreDNS sidecar resolves every query
   by first calling `/v1/dns/resolve`. Pro-tier customers see NXDOMAIN for
   any host in the StevenBlack blocklist.

## Troubleshooting

| Symptom | Likely cause |
|---------|--------------|
| Customer's WG client connects but no traffic flows | check `/v1/admit` log — could be `MONTHLY_CAP_EXCEEDED`, `UNKNOWN_PEER`, or `UNSUPPORTED_COUNTRY` |
| Customer says "every site now broken on Pro tier" | StevenBlack list has been corrupted by an upstream commit; pin to a known-good SHA via `BLOCKLIST_URL` |
| iOS profile install fails | check the rendered `.mobileconfig` is well-formed XML; the `WgQuickConfig` field must be XML-escaped |
| Sticky session keeps rotating providers | `internal/session` TTL is per-pod; if customers traverse pods, enable Redis-backed bindings |
| `/metrics` shows zero `vpn_active_peers` | the WG listener never came up — check `WG_LISTEN_ENABLE=true` and the LoadBalancer service has an external IP |

## Local development

```bash
cd coordinator/services/vpn-gateway
go test ./... -count=1
go run ./cmd/vpn-gateway
# control plane on :8080
# (WG listener disabled by default in dev; set WG_LISTEN_ENABLE=true to enable)
curl localhost:8080/v1/
curl localhost:8080/readyz
```

## Related services

- `identity-svc`  — issues the customer's identity, signed JWT.
- `billing-svc`   — owns the Subscription record; `GetCustomerTier`.
- `workloads-svc` — dispatches bandwidth workloads to provider daemons.
- `antiabuse-svc` — pre-flight filter (CSAM / phishing / port limits).
- `gateway-bff`   — surfaces the config download routes to the web app.

## Issue

Implements EPIC [#75](https://github.com/iogrid/iogrid/issues/75).
