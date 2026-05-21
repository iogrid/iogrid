# antiabuse-svc

iogrid coordinator microservice that runs the **mandatory pre-flight
filters** required by `docs/BUSINESS-STRATEGY.md` §6 (Legal risk landscape & mitigation) before any external provider can
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
stream, 90-day retention) per `docs/BUSINESS-STRATEGY.md` §6 — or to slog at INFO
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
| `AUDIT_POSTGRES_DSN` | _empty_ | pgx DSN for the optional relational audit mirror; when set, the 90-day retention pruner runs nightly DELETEs in batches of 10_000 |
| `AUDIT_RETENTION_DAYS` | `90` | Retention horizon for the JetStream stream + Postgres mirror. Clamped up to the 90-day legal floor — values below 90 are coerced to 90 |
| `PHOTODNA_BLOOM_REFRESH` | `168h` | Cadence at which the NCMEC published-hash bloom filter is rebuilt (7 days matches NCMEC's export cycle) |
| `HIGH_VALUE_TARGETS` | `linkedin.com,facebook.com,twitter.com,google.com,instagram.com` | Comma-separated destinations under the 10 RPS-per-provider cap |
| `BLOCK_DOMAINS` | _empty_ | Comma-separated operator deny-list (glob patterns supported). Matches return `FILTER_DECISION_BLOCK` with `reason=destination_blocked`. Intended for staging / e2e fixtures (`malware.test,known-bad.test,*.evil.example`); in prod the same list lives in the DB-backed loader (issue #72) |
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
gap to close before Phase 1 onboarding** — see `docs/BUSINESS-STRATEGY.md` §6
("Mandatory anti-abuse before any external provider joins").

#### Partnership application process

NCMEC restricts PhotoDNA access to vetted organisations under a signed
partnership agreement. Application is via the CyberTipline IPAM portal
(`https://report.cybertip.org/ipam`, organisation registration at
`https://www.cybertip.org/registration`) and typically takes 4–8
weeks. The required artefacts are:

1. **Organisation evidence** — Dynolabs's certificate of incorporation,
   doing-business-as evidence for iogrid (we operate iogrid as a
   product line under Dynolabs per `docs/BUSINESS-STRATEGY.md` §6), and contact for
   the named principal accountable for CSAM response.
2. **Use-case statement** — a 1–2 page description of *what* iogrid
   relays, *which* surfaces will run PhotoDNA lookups (the
   bandwidth-workload pre-flight path), and *how often* lookups will
   fire. Reviewers want enough specificity to understand the abuse
   model; rough numbers from `docs/BUSINESS-STRATEGY.md` §1 (Market) are sufficient.
3. **Anti-abuse posture** — confirmation that other filters
   (PhishTank / OpenPhish / Google Safe Browsing / domain deny-list /
   port deny-list / per-customer KYC) are in place. `docs/BUSINESS-STRATEGY.md` §6
   ("Mandatory anti-abuse") is the canonical evidence.
4. **Audit-log retention commitment** — written attestation that audit
   logs are retained for 90 days with the schema documented in
   `docs/BUSINESS-STRATEGY.md` §6. This package's `internal/audit` +
   `internal/audit/retention.go` implement that commitment.
5. **Incident-response plan** — named owner + escalation path when a
   PhotoDNA match occurs. NCMEC requires this so they know who to
   contact on the iogrid side when a customer dispute reaches NCMEC
   directly.
6. **Designated principal contact** — named individual at Dynolabs
   responsible for the agreement. Counsel co-signs.

Once issued, `PHOTODNA_API_KEY` is loaded into the
`antiabuse-svc-secrets` Kubernetes Secret and the backend transitions
from stub mode at the next rollout. The on-call channel receives a
slog INFO line confirming the switch (`ncmec_photodna bloom refreshed`
is the canonical "armed" signature).

#### Weekly hash-list refresh

In addition to per-image lookups the backend keeps an in-memory bloom
filter of the published NCMEC hash database. The refresh goroutine
pulls the export weekly (override via `PHOTODNA_BLOOM_REFRESH`); the
filter lets us short-circuit definitively-not-CSAM lookups before any
network round-trip. The bloom never false-negatives, so a "miss"
always escapes to the real API call.

### Audit log retention enforcement

The 90-day retention requirement in `docs/BUSINESS-STRATEGY.md` §6 is enforced at
three layers:

1. **JetStream `MaxAge`** — the `AUDIT` stream is configured with
   `MaxAge = 90d` at creation. The pruner re-applies this every 24h
   in case of config drift.
2. **Postgres mirror DELETE** — when `AUDIT_POSTGRES_DSN` is set the
   pruner runs a bounded-batch DELETE (`LIMIT 10_000` per pass) keyed
   on a `created_at` index, every 24h. The index is created
   idempotently on first boot via `EnsureIndex`.
3. **`Pruner.RunOnce` admin trigger** — exposed so a future admin
   endpoint can force a sweep out-of-band.

The minimum retention is 90 days; values below that are clamped up so
the legal-shield argument in `docs/BUSINESS-STRATEGY.md` §6 never silently breaks.

### Quarterly transparency report

The `cmd/transparency-report` binary aggregates the in-window audit
counters and publishes a JSON + Markdown report. The
`infra/k8s/base/antiabuse-svc/cronjob-transparency.yaml` CronJob fires
it at `03:00 UTC` on the 1st of January / April / July / October —
publishing a report for the just-completed quarter.

Each report covers:

- Total filter checks performed in the quarter
- Total blocks issued, aggregate block rate
- Per-category block counts (CSAM, phishing, fraud, rate-limit, etc.)
- Per-backend hit rates (PhotoDNA, PhishTank, OpenPhish, GSB)
- Law-enforcement inquiries received + responses sent + breakdown by
  jurisdiction + request type
- Audit-log retention compliance (configured vs required, last prune
  pass, oldest record)
- Methodology footer

Delivery channels:

- `s3://iogrid-transparency/{year}/Q{q}.{json,md}` (canonical archive)
- `gateway-bff` `POST /api/v1/transparency/publish` (cache for the
  public endpoint at `https://api.iogrid.org/status/transparency/{year}/{quarter}`)
- The marketing `/transparency` page consumes the gateway-bff endpoint
  to render the index + per-report links

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

Audit-retention integration tests live under
`internal/audit/retention_integration_test.go` (also `//go:build
integration`). They need a reachable Postgres (default
`postgres://postgres:postgres@localhost:5432/antiabuse_audit?sslmode=disable`)
and verify that 100-day-old rows are pruned while fresh rows survive:

```bash
docker run --rm -d -p 5432:5432 \
    -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=antiabuse_audit \
    postgres:16
go test -tags=integration ./internal/audit/...
```

---

## Liability shield rationale

Per `docs/BUSINESS-STRATEGY.md` §6:

> The reason commercial intermediaries take the legal hit is: deeper
> pockets, stronger anti-abuse defenses, central audit logs that
> pinpoint customers. We have to maintain those defenses or we lose
> the liability shield.

If a filter stops being functional, the whole shield collapses. Every
backend therefore exposes `Enabled()` and the `ListFilters` RPC so
the daemon (and ops) can verify in real time which layers are armed.
