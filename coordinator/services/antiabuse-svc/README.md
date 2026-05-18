# antiabuse-svc

iogrid coordinator microservice that runs the **mandatory pre-flight
filters** required by `docs/LEGAL.md` before any external provider can
join the grid.

The same wire RPCs (defined in `proto/iogrid/antiabuse/v1/filters.proto`)
are implemented here server-side and mirrored locally in the Rust
daemon so providers can audit them via the local UI bridge.

---

## Filters

| Layer | Slug | Coverage | Decision |
|------|------|----------|----------|
| Outbound ports | `ports.default` | SMTP (25/465/587/2525), IRC (6667/6697), Tor (9001/9030), telnet (23) | BLOCK |
| Government domains | `domains.government` | `.gov`, `.mil` | BLOCK unconditionally |
| Banking domains | `domains.banking` | Static list (chase, BofA, Wells, …) — Phase 2 moves to DB | BLOCK until KYC opt-in |
| Adult domains | `domains.adult` | Static list (pornhub, onlyfans, …) | BLOCK unless provider opted-in (`category=ADULT_CONTENT`) |
| Per-customer RPS | `ratelimit.customer` | 100 RPS default / 1000 RPS premium | BLOCK + `retry_after` |
| Per-provider per-dest RPS | `ratelimit.provider_destination` | 10 RPS to LinkedIn / Facebook / Twitter / Google / Instagram | BLOCK + `retry_after` |
| PhishTank | `phishtank` | URL feed, refreshed every 24h | BLOCK on match |
| OpenPhish | `openphish` | URL feed, refreshed every 6h | BLOCK on match |
| Google Safe Browsing | `google_safe_browsing` | per-request lookup, 5-min cache | BLOCK on match |
| NCMEC PhotoDNA | `ncmec_photodna` | hash lookup against NCMEC (stub until partnership) | BLOCK on CSAM hit |
| Container registry allowlist | `registry.docker_allowlist` | ghcr.io, docker.io/library, Bitnami/Grafana/Redis namespaces, quay.io, registry.k8s.io | BLOCK on miss |

The orchestrator (`internal/filters/orchestrator.go`) fans every
reputation backend in parallel and aggregates the strictest decision —
a single feed outage cannot collapse the whole pipeline.

Every Check\* call emits an audit event to NATS JetStream (`AUDIT`
stream, 90-day retention) per `docs/LEGAL.md` — or to slog at INFO
when `NATS_URL` is unset.

---

## Environment variables

| Var | Default | Effect |
|-----|---------|--------|
| `LISTEN_ADDR` | `:8080` | HTTP bind address |
| `PHISHTANK_API_KEY` | _empty_ | Registered-app key for unthrottled PhishTank feed; empty falls back to public unauthenticated feed (warning logged) |
| `PHISHTANK_REFRESH` | `24h` | Cache refresh cadence |
| `OPENPHISH_REFRESH` | `6h` | OpenPhish cache refresh cadence |
| `GSB_API_KEY` | _empty_ | Google Safe Browsing v4 lookup API key; empty disables the backend |
| `PHOTODNA_API_KEY` | _empty_ | NCMEC PhotoDNA key; empty puts the backend in stub mode (warning logged on first call) |
| `REDIS_URL` | _empty_ | Redis connection URL for rate limiting; empty uses in-memory fallback (NOT safe for multi-replica deploys) |
| `NATS_URL` | _empty_ | NATS JetStream URL for audit emission; empty falls back to slog |
| `HIGH_VALUE_TARGETS` | `linkedin.com,facebook.com,twitter.com,google.com,instagram.com` | Comma-separated destinations under the 10 RPS-per-provider cap |
| `DEFAULT_CUSTOMER_RPS` | `100` | Per-customer aggregate cap |
| `PREMIUM_CUSTOMER_RPS` | `1000` | Premium-tier per-customer cap |
| `HIGH_VALUE_PROVIDER_RPS` | `10` | Per-provider per-destination cap for high-value targets |

---

## Wire surface

Connect-RPC (HTTP/1.1 + HTTP/2 + gRPC + gRPC-Web) mounted at
`/iogrid.antiabuse.v1.AbuseFilterService/`.

| RPC | Purpose |
|-----|---------|
| `CheckUrl(CheckUrlRequest)` | Full URL pre-flight (before HTTP relay) |
| `CheckDomain(CheckDomainRequest)` | Header-time check (SOCKS5 CONNECT) |
| `CheckContainerImage(CheckContainerImageRequest)` | Registry allowlist check (Docker workload submission) |
| `ReportEvent(ReportEventRequest)` | Audit-event ingestion from other services (proxy-gateway, daemon) |
| `ListFilters(ListFiltersRequest)` | Rule snapshot + ruleset hash (daemon mirror) |

The legacy JSON probe at `/v1/` returns
`{"service":"antiabuse-svc","status":"ready","rpc":"iogrid.antiabuse.v1.AbuseFilterService"}`
for service-discovery sweeps.

---

## Operations

### Refreshing the PhishTank cache manually

The backend refreshes the cache every `PHISHTANK_REFRESH`. To force a
refresh out-of-band, restart the Pod or send the binary a SIGHUP (TODO:
SIGHUP handler — for now Pod restart is the documented path).

### Disabling a backend at runtime

There is no live toggle. Set the relevant `*_API_KEY` env var to empty
and restart. The orchestrator handles a disabled backend transparently:
the backend reports `Enabled() == false` and short-circuits every
CheckURL / CheckDomain to ALLOW with `reason=no_match`.

### Adding a banking / adult / high-value domain

Phase 0/1: edit the constant lists in `internal/domains/domains.go` and
`HIGH_VALUE_TARGETS` env var, redeploy. Phase 1+ migrates this to a
DB-backed loader (issue #72).

### NCMEC PhotoDNA onboarding

Until NCMEC issues the API key the backend logs a one-shot WARNING on
first traffic and short-circuits to ALLOW. **This is the highest-priority
gap to close before Phase 1 onboarding** — see `docs/LEGAL.md` §
"Mandatory anti-abuse before any external provider joins".

---

## Tests

Unit tests cover every layer — `go test ./...`. They make no external
network calls; PhishTank / OpenPhish / GSB are mocked via
`httptest.NewServer`.

Integration tests live under `internal/handler/integration_test.go`
behind a `//go:build integration` tag. They need a reachable Redis
(default `redis://localhost:6379/0`):

```bash
docker run --rm -d -p 6379:6379 redis:7
go test -tags=integration ./internal/handler/...
```

The integration test still mocks the feed endpoints so the only
external dependency is Redis.

---

## Liability shield rationale

Per `docs/LEGAL.md`:

> The reason commercial intermediaries take the legal hit is: deeper
> pockets, stronger anti-abuse defenses, central audit logs that
> pinpoint customers. We have to maintain those defenses or we lose
> the liability shield.

If a filter stops being functional, the whole shield collapses. Every
backend therefore exposes `Enabled()` and the `ListFilters` RPC so
the daemon (and ops) can verify in real time which layers are armed.
