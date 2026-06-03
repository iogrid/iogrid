# gateway-bff

Backend-for-Frontend for the iogrid Next.js management plane. Aggregates
calls across the coordinator microservices, terminates customer-facing
JWTs, enforces CORS + per-user rate limits, and exposes Server-Sent
Event streams for the real-time transparency feed.

The web app NEVER talks to a microservice directly — every call routes
through this service. That keeps customer-facing contracts stable while
backend services evolve.

## Surface area

All paths are rooted at `/api/v1`. JSON in, JSON out. Bearer tokens go
in `Authorization: Bearer <jwt>`.

### Identity / account

| Method | Path                                          | Auth | Notes                                             |
| ------ | --------------------------------------------- | ---- | ------------------------------------------------- |
| GET    | `/me`                                         | yes  | Returns user + linked identifiers.                |
| POST   | `/account/sign-in/google`                     | no   | Starts the Google OAuth flow.                     |
| POST   | `/account/sign-in/google/complete`            | no   | Finishes Google OAuth.                            |
| POST   | `/account/sign-in/magic`                      | no   | Requests an email magic-link.                     |
| POST   | `/account/sign-in/magic/complete`             | no   | Redeems a magic-link token.                       |
| POST   | `/account/sign-out`                           | yes  | Revokes the supplied refresh token.               |
| GET    | `/account/sessions`                           | yes  | Lists active sessions.                            |

### Provider (`/provide`)

| Method | Path                                          | Notes                                                                     |
| ------ | --------------------------------------------- | ------------------------------------------------------------------------- |
| GET    | `/provide/dashboard?provider_id=`             | Fan-out: earnings + state + recent audit page.                            |
| GET    | `/provide/schedule?provider_id=`              | Current scheduling config.                                                |
| POST   | `/provide/schedule`                           | Replaces the scheduling config (read-modify-write).                       |
| GET    | `/provide/audit/stream?provider_id=`          | **SSE** — live transparency feed from providers-svc.                      |
| GET    | `/provide/earnings?provider_id=&start=&end=`  | Earnings summary only.                                                    |

### Customer (`/customer`)

| Method | Path                                                   | Notes                                                            |
| ------ | ------------------------------------------------------ | ---------------------------------------------------------------- |
| POST   | `/customer/api-keys`                                   | Creates a key. Plaintext returned ONCE.                          |
| GET    | `/customer/api-keys?workspace_id=`                     | Lists keys (no plaintexts).                                      |
| DELETE | `/customer/api-keys/{id}?workspace_id=`                | Revokes a key.                                                   |
| GET    | `/customer/usage?workspace_id=&start=&end=`            | Metering aggregates from billing-svc.                            |
| POST   | `/customer/workloads`                                  | Submits a workload to workloads-svc.                             |
| GET    | `/customer/workloads/{id}/events`                      | **SSE** — per-workload status updates.                           |

### Admin (`/admin`) — `ADMIN` role required

| Method | Path                                | Notes                                                |
| ------ | ----------------------------------- | ---------------------------------------------------- |
| GET    | `/admin/abuse-queue`                | Active anti-abuse rule set + ruleset hash.           |
| POST   | `/admin/abuse/{id}/resolve`         | Records an allow/block decision for a flagged event. |

### Consumer VPN (`/vpn`)

| Method | Path                                  | Notes                                              |
| ------ | ------------------------------------- | -------------------------------------------------- |
| GET    | `/vpn/account?workspace_id=`          | Tier + bandwidth usage view.                       |
| POST   | `/vpn/upgrade`                        | Returns a Stripe Checkout URL for upgrade.         |

### Smoke

| Method | Path        | Notes                                              |
| ------ | ----------- | -------------------------------------------------- |
| GET    | `/v1/`      | Stable JSON envelope for liveness/discovery.       |
| GET    | `/healthz`  | Live check (always 200 once the process is up).    |
| GET    | `/readyz`   | Ready check (200 after `MarkReady()`).             |
| GET    | `/metrics`  | Prometheus metrics scrape.                         |

## SSE protocol

Streams follow the HTML5 SSE spec:

```
id: <opaque event id>
event: <kind, e.g. audit_event | workload_event | error>
data: <JSON payload>

```

- `Content-Type: text/event-stream`
- `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no`
- Server emits `:keep-alive` comments every 15 s by default to defeat
  intermediary idle timeouts.
- Clients reconnect via the `Last-Event-ID` header — the server
  forwards it to the producer so resume-aware downstream services pick
  up where they left off.
- Terminal errors are emitted as `event: error` before the connection
  closes (SSE has no status-code path once headers are sent).

## Authentication

- Bearer JWTs (RS256), validated against the JWKS set fetched from
  `identity-svc`. The JWKS set is cached and refreshed every
  `JWKS_REFRESH_INTERVAL` (default 15 min); the resolver also re-polls
  on cache-miss to pick up freshly rotated `kid`s without waiting a
  full TTL.
- Expected `iss` = `$JWT_ISSUER`; expected `aud` contains
  `$JWT_AUDIENCE`. Tokens missing either are rejected with 401.
- `/admin/*` additionally requires the `ADMIN` (alias of
  `USER_ROLE_ADMIN`) role to appear in the token's `roles` claim.

## Rate limiting

Per-key token-bucket, applied AFTER auth runs so authenticated callers
key on user-id rather than IP:

- Authenticated: `AUTHED_RATE_PER_SEC` (60/s default), burst `AUTHED_BURST` (120 default), keyed on user UUID.
- Anonymous: `ANONYMOUS_RATE_PER_SEC` (10/s default), burst `ANONYMOUS_BURST` (20 default), keyed on client IP (with `X-Forwarded-For` + `X-Real-IP` honored).
- Exceeded → HTTP 429 with `Retry-After` header and a JSON envelope
  `{code:"rate_limited", message, retry_after_s}`.

Buckets idle for 5 min are reaped every minute so memory remains
bounded under churn.

## CORS

- `Access-Control-Allow-Origin`: echoed from the request when the
  request's `Origin` is in `CORS_ALLOWED_ORIGINS` (compiled-in default
  still lists `https://app.iogrid.org,https://iogrid.org`; `app.iogrid.org`
  is being dropped in favour of the `iogrid.org` apex). No wildcards.
- `Access-Control-Allow-Credentials`: `true`.
- Allowed methods: GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS.
- Allowed headers: Authorization, Content-Type, X-Requested-With,
  Last-Event-ID.
- Preflight `OPTIONS` is short-circuited with `204 No Content` and a
  10-minute preflight cache.

## Environment variables

| Variable                  | Default                                                       | Purpose                                                |
| ------------------------- | ------------------------------------------------------------- | ------------------------------------------------------ |
| `LISTEN_ADDR`             | `:8080`                                                       | TCP listen for the HTTP server.                        |
| `IDENTITY_SVC_URL`        | `http://identity-svc:8080`                                    | Connect URL for identity-svc.                          |
| `PROVIDERS_SVC_URL`       | `http://providers-svc:8080`                                   | Connect URL for providers-svc.                         |
| `WORKLOADS_SVC_URL`       | `http://workloads-svc:8080`                                   | Connect URL for workloads-svc.                         |
| `ANTIABUSE_SVC_URL`       | `http://antiabuse-svc:8080`                                   | Connect URL for antiabuse-svc.                         |
| `BILLING_SVC_URL`         | `http://billing-svc:8080`                                     | Connect URL for billing-svc.                           |
| `VPN_GATEWAY_URL`         | `http://vpn-gateway:8080`                                     | Control-plane URL for vpn-gateway (consumer VPN).      |
| `DOWNSTREAM_TIMEOUT`      | `10s`                                                         | Per-call timeout for every Connect client.             |
| `DOWNSTREAM_RETRIES`      | `2`                                                           | Retries on transient (Unavailable / DeadlineExceeded). |
| `JWKS_URL`                | `http://identity-svc:8080/v1/.well-known/jwks.json`           | Identity-svc JWKS endpoint.                            |
| `JWT_ISSUER`              | `https://api.iogrid.org/identity`                             | Expected `iss` claim.                                  |
| `JWT_AUDIENCE`            | `gateway-bff`                                                 | Expected entry in `aud`.                               |
| `JWKS_REFRESH_INTERVAL`   | `15m`                                                         | How often to re-poll the JWKS.                         |
| `CORS_ALLOWED_ORIGINS`    | `https://app.iogrid.org,https://iogrid.org`                   | Comma-separated origin allowlist.                      |
| `AUTHED_RATE_PER_SEC`     | `60`                                                          | Per-user rate budget.                                  |
| `AUTHED_BURST`            | `120`                                                         | Per-user burst.                                        |
| `ANONYMOUS_RATE_PER_SEC`  | `10`                                                          | Per-IP rate budget for unauth requests.                |
| `ANONYMOUS_BURST`         | `20`                                                          | Per-IP burst.                                          |
| `SSE_KEEPALIVE_INTERVAL`  | `15s`                                                         | Keep-alive comment interval on SSE streams.            |

The shared bootstrap layer additionally consumes the standard
`OTEL_EXPORTER_OTLP_*` variables; see `coordinator/shared/otel`.

## Layout

```
cmd/gateway-bff/main.go     binary entry point
internal/config             env-driven Config
internal/auth               JWT verifier + JWKS resolver + chi middleware
internal/ratelimit          token-bucket per-key limiter + middleware
internal/cors               CORS middleware
internal/clients            typed Connect clients per downstream service
internal/sse                SSE writer + Producer abstraction
internal/handlers           per-domain HTTP handlers (account/provide/customer/admin/vpn)
internal/server             chi router wiring; Mount() composes the tree
```

## Running tests

```
go test ./...                          # unit tests
go test -tags integration ./...        # adds the chi-router end-to-end suite
```

The integration build tag spins the full server with stub downstream
clients and a synthetic JWT signer, exercising every route through the
real middleware stack. It exists alongside the per-package unit tests
that mock at the interface boundary.
