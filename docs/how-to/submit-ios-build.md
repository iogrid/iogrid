# Submit an iOS build to iogrid (as a customer)

> Turnkey iOS-build CI on the iogrid mesh. You submit a build with **one API
> call + an API key**; a real macOS provider runs `xcodebuild`, you poll status
> and download the artifact, and the provider is paid in devnet **$GRID**.
> **Zero SSH. No access to the build machine.** Refs #700 / #757.

This is the product behind iogrid's flagship differentiator: iOS-build CI at
~50% of GitHub Actions pricing, running on home-Mac providers. `ping` is the
first customer.

## TL;DR

```bash
export IOGRID_API_KEY=<your-iogrid-customer-api-key>

./scripts/submit-ios-build.sh \
  --repo https://github.com/ping/ping.git \
  --ref  main \
  --cmd  'xcodebuild -workspace Ping.xcworkspace -scheme Ping \
            -destination "platform=iOS Simulator,name=iPhone 16 Pro" \
            -derivedDataPath /tmp/ping-build build' \
  --artifact Ping.app.zip
```

The script submits the build, tails the live logs, polls until terminal, and
downloads the artifact via a pre-signed URL.

## Base URL & auth

- **Base URL:** `https://build.iogrid.org`
- **Auth header:** `Authorization: Bearer <api_key>` (or `X-Iogrid-Api-Key: <api_key>`)

Get an API key from the iogrid customer console (billing-svc issues
workspace-scoped keys). For the devnet dogfood, the operator provisions a key on
the build-gateway. **Never commit your key.**

## The API

| Method | Path | Purpose |
|--------|------|---------|
| `GET`  | `/v1/xcode-versions` | Approved Xcode versions + default. |
| `POST` | `/v1/builds` | Submit a build. Returns `build_id`, `status_url`, `logs_url`. |
| `GET`  | `/v1/builds` | List your workspace's recent builds (`?status=`, `?limit=`). |
| `GET`  | `/v1/builds/{id}` | Current status + artifact metadata. |
| `GET`  | `/v1/builds/{id}/logs` | Live SSE stream of stdout/stderr (`Last-Event-ID` resumes). |
| `DELETE` | `/v1/builds/{id}` | Cancel a running build. |
| `GET`  | `/v1/builds/{id}/artifacts/{name}` | 15-min pre-signed download URL. |

### 1. Submit

```bash
curl -sS -X POST https://build.iogrid.org/v1/builds \
  -H "Authorization: Bearer $IOGRID_API_KEY" \
  -H 'Content-Type: application/json' \
  --data '{
    "git_url": "https://github.com/ping/ping.git",
    "git_ref": "main",
    "build_command": "xcodebuild -workspace Ping.xcworkspace -scheme Ping -destination \"platform=iOS Simulator,name=iPhone 16 Pro\" -derivedDataPath /tmp/ping-build build"
  }'
```

Response (`202 Accepted`):

```json
{
  "build_id":  "f7a3c1b9e2d44801a0e6b8d9c2a5f413",
  "status":    "dispatched",
  "status_url":"/v1/builds/f7a3c1b9.../",
  "logs_url":  "/v1/builds/f7a3c1b9.../logs",
  "build":     { "...": "full record" }
}
```

**Submit body fields**

| Field | Required | Notes |
|-------|----------|-------|
| `git_url` | yes | `https://...` or `ssh://git@host/...` / `git@host:org/repo.git`. The provider `git clone`s it. Private-repo creds are configured per-workspace; the gateway never sees them. |
| `git_ref` | yes | branch / tag / commit SHA. |
| `build_command` | yes | the single shell/`xcodebuild` command run after clone+checkout (≤ 8192 chars). You own any signing-disable flags. |
| `xcode_version` | no | must be in `GET /v1/xcode-versions`; omit for the server default. Pass `latest` to follow the upstream tag. |
| `signing_team_id` | no | Apple Developer team for signing; omit for unsigned CI/simulator builds. |
| `env_vars` | no | exported into the build env. Keys may NOT start with `IOGRID_` (reserved). |
| `webhook_url` + `webhook_secret` | no | **pro/enterprise plans only.** HMAC-SHA256-signed status callbacks; secret ≥ 16 chars. |

### 2. Watch logs (optional)

```bash
curl -sS -N https://build.iogrid.org/v1/builds/$BUILD_ID/logs \
  -H "Authorization: Bearer $IOGRID_API_KEY"
```

Server-Sent Events; each line is `data: {"seq":N,"stream":"stdout","text":"..."}`.
Reconnect with `Last-Event-ID: <seq>` to resume. The provider streams the
build's **real stdout/stderr live** as it runs — the clone, the checkout, and
every line of `xcodebuild` output — so you can watch and debug a failure
without SSH. (#763)

### 3. Poll status

```bash
curl -sS https://build.iogrid.org/v1/builds/$BUILD_ID \
  -H "Authorization: Bearer $IOGRID_API_KEY" | jq .status
```

Status machine:

```
queued -> dispatched -> running -> succeeded | failed | timed_out
   |          |            |
   +-rejected-+            +-> cancelled
```

Terminal states (`succeeded`, `failed`, `timed_out`, `cancelled`, `rejected`)
are sticky. Poll `GET /v1/builds/{id}` for ground truth; webhooks are a
notification optimisation, not the source of truth.

### 4. Download the artifact

The build command should write its `.app` / `.ipa` / `.xcarchive` (zip it for a
single object), then:

```bash
PRESIGN=$(curl -sS https://build.iogrid.org/v1/builds/$BUILD_ID/artifacts/Ping.app.zip \
  -H "Authorization: Bearer $IOGRID_API_KEY" | jq -r .url)
curl -sS -o Ping.app.zip "$PRESIGN"
```

## How it runs (under the hood)

1. **build-gateway** (`build.iogrid.org`) validates your API key, persists the
   build, and dispatches an `IosBuildRequest` workload to **workloads-svc** over
   Connect-Go, stamping a `build_id` label for status correlation.
2. **workloads-svc** schedules the job to an eligible macOS provider and records
   the assignment.
3. The provider **daemon** (on a real Mac) **polls**
   `GET /v1/providers/{id}/assigned-workloads` (the bidi-stream server-push is
   dropped by the edge, so dispatch is poll-based, mirroring the VPN binder —
   #705/#714/#715), `git clone`s your repo, runs your `build_command` via the
   **native host-direct runner** (no VM/GUI session needed on a trusted dev Mac),
   and POSTs status back.
4. workloads-svc forwards the terminal status to the build-gateway, which
   **meters** the build to a provider-attributed `usage_event` (#744) and
   **settles** the provider's earnings in devnet **$GRID** via billing-svc
   `/v1/grid/build-end` → the settlement-worker transfers $GRID on-chain
   (devnet; #718/#748).

You never touch the Mac. The whole interaction is the four HTTP calls above.

## Notes & limits

- **Devnet only.** $GRID settlement runs against Solana **devnet**. No mainnet.
- **Artifact durability (current dogfood limitation):** the build-gateway's
  artifact store is in-process today — artifacts are retrievable for the build's
  lifetime + a short window, but a gateway pod restart drops them. Download
  promptly on success. (Durable S3/MinIO artifact backing is a follow-up; the
  pre-signed-URL API contract is already final, so nothing on the customer side
  changes when it lands.)
- **Provider supply:** a build needs an online macOS provider advertising the
  `IOS_BUILD` workload. For the dogfood that's Hatice's Mac (daemon running with
  `tart` off PATH → native runner). If no provider is connected, the build stays
  `dispatched` until one appears.
- **Xcode version match:** the build runs against the provider's installed
  Xcode. Pin `xcode_version` from `GET /v1/xcode-versions`; routing by host
  Xcode version is tracked in #737.

## Build environment

Your `build_command` runs after `git clone` + `git checkout` inside a real
`/bin/bash -lc` shell on the provider Mac. The native runner hardens the
environment before your command so the toolchain is deterministic (#763):

| Variable | Value | Why |
|---|---|---|
| `DEVELOPER_DIR` | `/Applications/Xcode-26.5.0.app/Contents/Developer` (when present) | `xcodebuild` resolves to a known **Xcode 26** regardless of the host's global `xcode-select`. |
| `PATH` | `/opt/homebrew/opt/node@22/bin` prepended (when present) | RN/Expo tooling sees **node 22**, not a stale system node. |

Both are existence-guarded: on a Mac without the pinned paths the build still
runs against whatever `xcode-select` / `PATH` already resolve. A quick way to
confirm the live toolchain in your own build:

```bash
--cmd 'sw_vers && xcodebuild -version && node --version'
# streams (via GET /v1/builds/{id}/logs):
#   ProductVersion:	26.5.1
#   Xcode 26.5
#   v22.22.3
```

The reported `exit_code` is the **real** exit status of your command — a
non-zero `xcodebuild` fails the build (no phantom success).

## See also

- `scripts/submit-ios-build.sh` — the runnable wrapper used in the TL;DR.
- `coordinator/services/build-gateway/README.md` — full gateway API + internals.
- EPIC #700 — iOS builds through iogrid (Mac provider runner, devnet $GRID pay).
