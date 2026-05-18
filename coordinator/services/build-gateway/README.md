# build-gateway

Customer-facing iOS-build CI gateway. Receives build jobs, dispatches to Mac providers via `workloads-svc`, manages per-workspace S3 artifact storage, and surfaces live logs + signed-URL downloads back to the customer.

This service is one of the two customer-facing entry points in the iogrid coordinator (the other is `proxy-gateway`). It runs as a standalone Go binary in front of `build.iogrid.org`.

## Bounded context

- **Customer API** for iOS build submission, status, logs, cancel, and artifact retrieval.
- **Internal upload API** that Mac providers (via `workloads-svc` dispatch) use to push completed `.ipa` / `.app` / `.zip` artifacts into the per-workspace S3 bucket.
- **Webhook fan-out** for premium-tier customers — HMAC-SHA256-signed events on every status transition.
- **Build-time metering** — emits one event per terminal build to `iogrid.metering.build.v1` for `billing-svc` to aggregate.

It does NOT:
- run xcodebuild itself (that happens inside a Tart VM on a Mac provider, driven by the daemon's `workload-ios` crate);
- choose the provider (that's `workloads-svc`);
- store customer credentials (Apple Developer keys live in `secrets-svc`, fetched at dispatch time by the daemon).

## API

All customer-facing endpoints sit under `/v1/builds`. Authentication is a Bearer API key (or `X-Iogrid-Api-Key` header) validated against `billing-svc`. Every successful request is tagged with the `workspace_id` resolved from the key.

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/builds` | Submit a new iOS build job. Returns `build_id`, `status_url`, `logs_url`. |
| `GET` | `/v1/builds` | List recent builds for the calling workspace. Supports `?status=` and `?limit=`. |
| `GET` | `/v1/builds/{id}` | Fetch the current state + artifact metadata. |
| `DELETE` | `/v1/builds/{id}` | Request cancellation. Returns 202; the canonical `cancelled` status arrives once the provider acks. |
| `GET` | `/v1/builds/{id}/logs` | Server-Sent Events stream of `stdout`/`stderr`. Supports `Last-Event-ID` for resume. |
| `GET` | `/v1/builds/{id}/artifacts/{name}` | Returns a 15-minute pre-signed S3 URL for the named artifact. |
| `GET` | `/v1/xcode-versions` | Discovery: approved Xcode versions + default. |

Internal-only (called by `workloads-svc` / provider daemon, NOT by customers):

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/v1/builds/{id}/artifacts?name=...` | Upload a build artifact. Requires `X-Iogrid-Dispatch-Token`. |

### Submit body

```json
{
  "git_url":        "https://github.com/example/ios-app.git",
  "git_ref":        "main",
  "xcode_version":  "16.2",
  "build_command":  "xcodebuild -scheme App -destination 'generic/platform=iOS' archive",
  "signing_team_id": "ABCDEF1234",
  "env_vars": {
    "FASTLANE_USER":  "ci@example.com"
  },
  "webhook_url":    "https://customer.example.com/iogrid-webhook",
  "webhook_secret": "rotate-this-32-byte-secret"
}
```

Notes:
- `git_url` must be `https://` or `ssh://...` / `git@host:org/repo.git`. Authentication for private repos is configured per-workspace; the gateway never sees the credential.
- `xcode_version` is restricted to the approved list returned by `GET /v1/xcode-versions`. Pass `latest` to follow the Cirrus Labs upstream tag.
- `signing_team_id` is optional; unsigned CI builds (test runs only) are allowed.
- `env_vars` keys may NOT start with `IOGRID_` — that prefix is reserved for gateway-injected values.
- `webhook_url` requires the `pro` or `enterprise` plan; the secret must be at least 16 characters.

### Submit response (202 Accepted)

```json
{
  "build_id":  "f7a3c1b9e2d44801a0e6b8d9c2a5f413",
  "status":    "dispatched",
  "status_url": "/v1/builds/f7a3c1b9.../",
  "logs_url":  "/v1/builds/f7a3c1b9.../logs",
  "build":     { ... full Build record ... }
}
```

### Webhook payload

POSTed to `webhook_url` with `Content-Type: application/json`. Headers:

```
X-Iogrid-Signature-256: sha256=<hex>
X-Iogrid-Event-Id:       <uuid>
User-Agent:              iogrid-build-gateway/1.0
```

Body:

```json
{
  "event_id":     "...",
  "build_id":     "...",
  "workspace_id": "...",
  "status":       "running",
  "note":         "vm booted",
  "occurred_at":  "2026-05-19T12:34:56Z",
  "attempt_id":   "..."
}
```

Verification:

```python
import hmac, hashlib
expected = "sha256=" + hmac.new(secret, body, hashlib.sha256).hexdigest()
ok = hmac.compare_digest(expected, request.headers["X-Iogrid-Signature-256"])
```

## Status machine

```
queued ──► dispatched ──► running ──► succeeded | failed | timed_out
   │            │             │
   └── rejected ┘             └─► cancelled
```

Terminal states (`succeeded`, `failed`, `timed_out`, `cancelled`, `rejected`) are sticky — subsequent updates are rejected with `409 invalid_transition`. The state machine is intentionally permissive so the gateway never has to model provider-side race conditions; the only invariant is monotonic.

## Build-time metering

Each finished build emits exactly one event to `iogrid.metering.build.v1`:

```json
{
  "build_id":         "...",
  "workspace_id":     "...",
  "attempt_id":       "...",
  "terminal_status":  "succeeded",
  "started_at":       "...",
  "finished_at":      "...",
  "billable_minutes": 7
}
```

`billable_minutes` is wall-clock from `started_at` to `finished_at`, rounded up to the nearest whole minute. Builds that never started (i.e. `rejected` before a provider picked them up) bill zero.

## Storage layout

Each workspace gets its own bucket `iogrid-build-<workspace_compact>` on Hetzner Object Storage. Inside:

```
source/<build_id>/source.tar.gz       ← customer-uploaded source
artifacts/<build_id>/App.ipa          ← provider-uploaded artifacts
artifacts/<build_id>/dSYM.zip
artifacts/<build_id>/logs.txt
```

Buckets are created lazily on first submission. Server-side encryption is on by default (SSE-S3; SSE-C / SSE-KMS optional per plan). The lifecycle policy expires every artifact 30 days after upload (`enterprise` plans may raise to 365).

## Configuration

| Variable | Default | Purpose |
|----------|---------|---------|
| `LISTEN_ADDR` | `:8080` | HTTP bind address. |
| `BUILD_GATEWAY_DISPATCH_TOKEN` | (empty) | Shared secret for the internal artifact-upload endpoint. **Empty disables the check — only safe in tests / local dev.** |
| `BUILD_GATEWAY_STATIC_API_KEY` | (empty) | One-off API key registered in the in-memory validator. Useful for smoke testing before `billing-svc` is wired in. |
| `BUILD_GATEWAY_STATIC_WORKSPACE` | `default-workspace` | Workspace id bound to `BUILD_GATEWAY_STATIC_API_KEY`. |
| `BUILD_GATEWAY_STATIC_PLAN` | `free` | Plan tier (`free` / `pro` / `enterprise`) bound to the static key. |
| `BUILD_GATEWAY_S3_ENDPOINT` | (empty) | S3 endpoint host substituted into pre-signed URLs. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | — | Standard OTel envvar for trace export. |

## Internal architecture

```
+-------------------+      +----------------+      +------------------+
|  HTTP handlers    | ---> | builds.Service | ---> | workloadclient   |
|  internal/server  |      |                |      | (workloads-svc)  |
+-------------------+      +----------------+      +------------------+
        |                          |                       |
        |                          v                       v
        |                  +---------------+      +-------------------+
        |                  |  store.Store  |      |  webhook.Dispatcher|
        |                  |   (Postgres)  |      |  (HMAC-signed)    |
        |                  +---------------+      +-------------------+
        |                          |                       |
        v                          v                       v
+-------------------+      +----------------+      +-------------------+
|  auth.Validator   |      |  s3artifact    |      |  metering.Emitter |
| (billing-svc API) |      |  (Hetzner S3)  |      |   (NATS subject)  |
+-------------------+      +----------------+      +-------------------+
```

Every collaborator is an interface so handlers can be unit-tested against in-memory fakes (`store.InMemory`, `workloadclient.InMemory`, `s3artifact.InMemory`, `webhook.Recorder`, `metering.InMemory`). The cmd entrypoint wires production implementations once the corresponding peer services are reachable.

### Why webhooks are best-effort

The async webhook dispatcher uses a bounded in-memory queue + bounded retry (5 attempts, exponential backoff). When the queue is full or all retries fail we drop the event and increment a Prometheus counter. The customer-facing contract is "poll `GET /v1/builds/{id}` for ground truth; webhooks are a notification optimisation". This matches Stripe's and GitHub's stance.

## Local development

```bash
cd coordinator/services/build-gateway
go test ./...                         # unit + handler tests
go run ./cmd/build-gateway            # boots on :8080
```

Smoke test with a static key:

```bash
export BUILD_GATEWAY_STATIC_API_KEY=dev-key
export BUILD_GATEWAY_STATIC_PLAN=pro
go run ./cmd/build-gateway &
curl -sS http://localhost:8080/v1/xcode-versions | jq
curl -sS -X POST http://localhost:8080/v1/builds \
  -H 'Authorization: Bearer dev-key' \
  -H 'Content-Type: application/json' \
  --data '{"git_url":"https://github.com/example/ios-app.git","git_ref":"main","build_command":"xcodebuild"}'
```

## Tests

Two test layers:

- **Unit (`internal/builds`)** — exercises the Service against in-memory fakes; covers submit validation, lifecycle transitions, terminal-stickiness, artifact upload, webhook emission, metering, and tenancy.
- **Handler (`internal/server`)** — boots a real `httptest.Server` with the full chi mux and walks every endpoint with realistic HTTP bodies. Covers auth, dispatch-token gating, SSE log replay, cross-tenant 404, and webhook secret redaction.

Run everything:

```bash
go test ./... -race -count=1
```
