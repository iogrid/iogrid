# Phase 0 unblock — founder runbook for issue #201

This is the step-by-step runbook for unblocking the three-layer Phase 0
failure documented in
[issue #201](https://github.com/iogrid/iogrid/issues/201). It assumes
you're sitting at a workstation with `kubectl` pointed at the
mothership cluster, and that you have shell access to the founder's Mac
(directly or via the reverse-SSH tunnel).

> **TL;DR sequence**:
>
> 1. Decide / generate the 7 secrets (table below).
> 2. On the mothership, `kubectl apply -k gitops/flux/` then create the
>    7 secrets.
> 3. Verify Flux reconcile with `flux get all -n flux-system`.
> 4. Wait for Traefik IngressRoute to bind app/api/proxy.iogrid.org —
>    OR ship the Traefik shim (Layer 3, still missing as of this PR).
> 5. On the founder's Mac, `curl -fsSL .../install-iogridd.sh | bash`.
> 6. Re-run the vCard E2E smoke from `examples/phase0-vcard-customer/`.
> 7. Acceptance: criterion from the bottom of issue #201 satisfied.

---

## Layer 2 unblock — coordinator services to mothership

### Step 1. Decide / generate the 7 secrets

| # | Secret name              | What it gates                       | How to obtain                                                        |
|---|--------------------------|-------------------------------------|----------------------------------------------------------------------|
| 1 | `iogrid-google-oauth`    | identity-svc social sign-in         | https://console.cloud.google.com/apis/credentials — create OAuth 2.0 Client ID, type Web, redirect `https://app.iogrid.org/auth/google/callback`. |
| 2 | `iogrid-smtp`            | transactional email                 | Stalwart admin at `mail.openova.io` — create a service principal `iogrid-noreply@iogrid.org`. **Do NOT touch `emrah.baysal@`** (see global rules). |
| 3 | `iogrid-database`        | every -svc reads `DATABASE_URL`     | Re-wrap CNPG's auto-generated `iogrid-pg-app` Secret (one-liner in `gitops/flux/iogrid-secrets-skeleton.yaml`). |
| 4 | `iogrid-nats`            | inter-service event bus             | In-cluster, no auth required for Phase 0 (`nats://nats.iogrid.svc.cluster.local:4222`). |
| 5 | `iogrid-redis`           | scheduler hot-path cache            | bitnami Redis chart auto-generates the password; pull from `redis-master-secret`. |
| 6 | `iogrid-solana-payout`   | billing-svc pay-outs                | `solana-keygen new --no-bip39-passphrase -o /tmp/iogrid-payout.json`; fund the pubkey with ~5-10 SOL. |
| 7 | `iogrid-apollo`          | vCard enrichment fallback           | https://app.apollo.io/#/settings/integrations/api — production key for the Dynolabs / OpenOva account. |

The exact key set per Secret is in
[`gitops/flux/iogrid-secrets-skeleton.yaml`](../gitops/flux/iogrid-secrets-skeleton.yaml).
Each Secret has an inline `kubectl create secret generic ...` recipe at
the bottom of that file.

### Step 2. Apply the Flux bootstrap

```bash
# From the bastion (or any workstation with mothership kubeconfig):
cd /path/to/iogrid-checkout    # any branch — gitops/flux/ is static

# 2a. Pre-create the substitution vars
kubectl -n flux-system create configmap iogrid-flux-vars \
  --from-literal=PUBLIC_API_BASE=https://api.iogrid.org \
  --from-literal=PUBLIC_PROXY_BASE=https://proxy.iogrid.org \
  --from-literal=PUBLIC_APP_BASE=https://app.iogrid.org \
  --from-literal=MOTHERSHIP_REGION=hz-fsn1

# 2b. Apply the namespace + GitRepository + Kustomization
kubectl apply -k gitops/flux/

# 2c. Create the 7 secrets (after editing the skeleton with real values,
# or by running the inline `kubectl create secret generic` recipes)
kubectl apply -f /path/to/your/filled-in-secrets.yaml
# ... or run the per-secret recipes from the skeleton file's footer.
```

**Alternative**: copy `gitops/flux/iogrid-source.yaml` and
`gitops/flux/iogrid-kustomization.yaml` into the
`openova-io/openova-private` Flux structure (e.g. under
`clusters/mothership/iogrid/`) and let your existing
bootstrap-kustomization pick them up.

### Step 3. Verify reconcile

```bash
flux get all -n flux-system
# Expect:
#   GitRepository/iogrid       Ready=True  revision=main@sha1:<sha>
#   Kustomization/iogrid       Ready=True  applied revision...

flux get kustomization iogrid -n flux-system
# Inspect last applied revision + healthCheck status

kubectl -n iogrid get deploy
# Expect 7 Deployments, all `1/1 Ready` after ~5 minutes:
#   identity-svc providers-svc workloads-svc antiabuse-svc
#   billing-svc telemetry-svc gateway-bff
```

If anything is `0/1`:

```bash
kubectl -n iogrid describe deploy <name>          # Events
kubectl -n iogrid logs deploy/<name> --tail=200   # Application errors
```

99% of the time the culprit is a missing/misspelled Secret key.

---

## Layer 3 unblock — make api/proxy/app.iogrid.org actually route

> **As of this PR (#201 prep) this layer is STILL MISSING.** The
> [`infra/k8s/gateways/`](../infra/k8s/base/gateway) manifests are
> committed but use Cilium Gateway API resources, and the mothership
> currently runs Traefik. Until either (a) the shim ships or (b) the
> migration completes, hitting `https://api.iogrid.org/healthz` returns
> a Traefik default 404 (per issue #201 Layer 3 evidence).

### Step 4a. Option A — Traefik IngressRoute shim (recommended for Phase 0)

Translate each `Gateway`/`HTTPRoute` in `infra/k8s/base/gateway/` into a
Traefik `IngressRoute` (HTTP) or `IngressRouteTCP` (proxy passthrough),
referencing the existing in-namespace `Service`s. Required routes:

| Host                 | Path  | Backend Service           | Traefik kind     |
|----------------------|-------|---------------------------|------------------|
| `api.iogrid.org`     | `/`   | `gateway-bff:8080`        | `IngressRoute`   |
| `app.iogrid.org`     | `/`   | `web:3000`                | `IngressRoute`   |
| `proxy.iogrid.org`   | `/`   | `proxy-gateway:1080`      | `IngressRouteTCP` (SNI passthrough — SOCKS5 is plain TCP, no TLS) |

TLS lives on the Traefik side via cert-manager + `tls.certResolver`.

### Step 4b. Option B — finish Traefik → Cilium Gateway migration

Tracked separately in the OpenOva backlog; high blast radius (touches
every Service exposed off the mothership). Not viable on the Phase 0
critical path.

### Step 4 verification

```bash
curl -fsS https://api.iogrid.org/healthz
# expect: HTTP 200 with JSON {"ok":true,...}

openssl s_client -servername api.iogrid.org -connect api.iogrid.org:443 </dev/null 2>&1 \
  | grep -E 'subject=|issuer='
# expect: a real Let's Encrypt cert, NOT "CN = TRAEFIK DEFAULT CERT"
```

### Step 4c. identity-svc service-token + workspace bootstrap (#232)

The customer dashboard's first-login workspace auto-create
(`/api/customer/workspaces/init`, see `web/src/app/api/customer/`)
proxies into identity-svc using a shared service-token + the
`X-Iogrid-User-Id` header. To enable it on the cluster:

1. Mint a service token (random 32B) and store it in a sealed
   Secret mounted into BOTH the `web` Deployment and the
   `identity-svc` Deployment as `IOGRID_SERVICE_TOKEN`:

   ```bash
   tok=$(openssl rand -hex 32)
   kubectl -n iogrid create secret generic iogrid-bff-service-token \
     --from-literal=IOGRID_SERVICE_TOKEN="$tok" \
     --dry-run=client -o yaml | kubeseal -o yaml > sealed.yaml
   ```

2. Add an envFrom secretRef on both Deployments. The `web` Deployment
   ALSO needs `IOGRID_IDENTITY_SVC_URL=http://identity-svc:8080` so the
   BFF knows where to dial.

3. Apply a Traefik IngressRoute exposing the identity-svc JSON
   surface at `api.iogrid.org/v1/workspaces` (the BFF dials in-cluster
   so this is only needed if you want to expose Connect-RPC to
   browsers later — Phase 0 doesn't):

   ```yaml
   apiVersion: traefik.io/v1alpha1
   kind: IngressRoute
   metadata:
     name: api-iogrid-org-identity
     namespace: iogrid
   spec:
     entryPoints: [websecure]
     routes:
       # JSON surface (eventually for direct browser calls).
       - match: Host(`api.iogrid.org`) && PathPrefix(`/v1/workspaces`)
         kind: Rule
         services:
           - name: identity-svc
             port: 8080
       # Connect-RPC surface — h2c so HTTP/2 cleartext is bridged.
       - match: Host(`api.iogrid.org`) && PathPrefix(`/iogrid.identity.v1.`)
         kind: Rule
         services:
           - name: identity-svc
             port: 8080
             scheme: h2c
     tls:
       certResolver: letsencrypt
   ```

4. Until both Secrets are populated AND `IOGRID_IDENTITY_SVC_URL` is
   set on `web`, the init route returns HTTP 503 and the dashboard
   falls back to the collapsed paste-UUID escape hatch — users are
   degraded but not blocked.

### Step 4d. Web → gateway-bff auth bridge (#237)

In-browser fetches from `app.iogrid.org` to `api.iogrid.org/api/v1/*`
were returning HTTP 401 because the web uses NextAuth (cookies) and
gateway-bff requires an identity-svc Bearer JWT — no bridge existed
between the two. The fix migrated every `browserApi().get/post(/api/v1/*)`
call to a same-origin Next.js Route Handler under `web/src/app/api/v1/*`
that reads the session server-side and forwards to gateway-bff with
the shared service-token + `X-Iogrid-User-Id` shim.

**Env required on the `web` Deployment**:

| Env var                       | Value (Phase 0)                                           | Purpose                                                                 |
|-------------------------------|-----------------------------------------------------------|-------------------------------------------------------------------------|
| `IOGRID_GATEWAY_BFF_URL`      | `http://gateway-bff.iogrid.svc.cluster.local:8080`        | The in-cluster URL of gateway-bff. Each Route Handler dials it.         |
| `IOGRID_SERVICE_TOKEN`        | random 32-byte hex (same sealed Secret as identity-svc)   | Shared bearer the Route Handlers present to gateway-bff.                |
| `IOGRID_PROVIDERS_RPC_URL`    | (optional) override for providers-svc Connect-RPC host    | Defaults to `IOGRID_GATEWAY_BFF_URL`. Set this only if providers-svc is fronted on a separate host. |

**Env required on the `gateway-bff` Deployment**:

| Env var                       | Value (Phase 0)                                           | Purpose                                                                 |
|-------------------------------|-----------------------------------------------------------|-------------------------------------------------------------------------|
| `IOGRID_SERVICE_TOKEN`        | same value as on `web`                                    | Activates the BFF-side shim in `internal/auth/auth.go`. The middleware materialises a Claims object from the request headers when this env is set AND the supplied bearer equals it. |

**Headers the Route Handler forwards**:

| Header                 | Source (in the web Route Handler)                            |
|------------------------|--------------------------------------------------------------|
| `Authorization`        | `Bearer ${IOGRID_SERVICE_TOKEN}`                             |
| `X-Iogrid-User-Id`     | `session.user.id` (UUID from NextAuth)                       |
| `X-Iogrid-User-Email`  | `session.user.email`                                         |
| `X-Iogrid-User-Roles`  | `session.user.roles` (comma-separated), + `ADMIN` for /admin |

When either env is unset on `web` the Route Handler returns 503 and
the UI surfaces a "BFF not configured" error — no silent 401. When
unset on `gateway-bff` the shim is dormant and the BFF only accepts
real JWT bearers (the legacy behaviour).

**Affected client-side paths now routed same-origin**:

- `/api/v1/provide/dashboard` (GET)
- `/api/v1/provide/schedule` (GET / POST)
- `/api/v1/provide/earnings` (GET, with `?start=&end=` passthrough)
- `/api/v1/provide/audit/stream` (GET — SSE pass-through)
- `/api/v1/admin/abuse-queue` (GET, ADMIN-asserted)
- `/api/v1/admin/abuse/{id}/resolve` (POST, ADMIN-asserted)
- `/api/v1/admin/providers/list` (POST — wraps the providers-svc
  Connect-RPC `ListProviders` so the admin UI no longer needs a
  direct cross-origin Connect call)

The `browserApi().baseUrl` default is now empty (same-origin) so
existing callers that template `${browserApi().baseUrl}/api/v1/...`
continue to work — they just resolve to relative paths.

---

## Layer 1 unblock — install iogridd on the founder's Mac

### Step 5. Run the installer

On the founder's Mac (Terminal):

```bash
# Quick install (interactive — prompts for pairing token):
curl -fsSL https://raw.githubusercontent.com/iogrid/iogrid/main/installer/macos/install-iogridd.sh | bash

# Or with a pre-grabbed pair token (no prompts):
curl -fsSL https://raw.githubusercontent.com/iogrid/iogrid/main/installer/macos/install-iogridd.sh \
  | bash -s -- --pair-token=<TOKEN_FROM_COORDINATOR_UI>
```

> **Prerequisite**: the iogrid GitHub Releases page must have a
> `iogridd-darwin-arm64` (and ideally `iogridd-darwin-amd64`) asset
> attached to the latest release. If no release exists yet, ship one
> from CI before running this installer.

Grab the pairing token from
`https://app.iogrid.org/dashboard/devices/pair` (renders after Layer 2
+ 3 are green).

### Step 6. Verify the daemon is alive

On the Mac:

```bash
launchctl list | grep io.iogrid.daemon       # expect a row with PID (not -)
pgrep -af iogridd                             # expect the running process
iogridd status --config ~/.iogrid/config.toml # expect "connected" / "ready"
tail -f ~/.iogrid/logs/iogridd.out.log        # live tail of the daemon
```

From the bastion (cross-check via the coordinator):

```bash
curl -fsS https://api.iogrid.org/api/v1/providers \
  -H "Authorization: Bearer <admin-token>" \
  | jq '.[] | select(.country=="<your-country>") | {handle, online_since, categories}'
```

---

## Step 7. Re-run the vCard E2E smoke

From the bastion or any iogrid CI runner:

```bash
cd examples/phase0-vcard-customer
go run . -vanity emrahbaysal
# Expect a JSON object:
#   {
#     "name": "Emrah Baysal",
#     "title": "...",
#     "company": "...",
#     "proxy_used": true,
#     "latency_ms": 720
#   }
```

The acceptance criterion from the bottom of #201:

> `go run examples/phase0-vcard-customer -vanity <handle>` from the
> bastion (or any iogrid CI runner) returns a JSON object with
> non-empty `name` and `proxy_used: true` and `latency_ms < 1500`,
> with the response originating from the founder's Mac residential IP
> (confirmed in `X-Iogrid-Provider-Country` header).

When all three of these are true, **#201 closes**.

---

## Reference — issue #201 reproducer

To re-verify the original break before/after each fix, the reproducer
from the issue body:

```python
# From the bastion:
import socket
s = socket.create_connection(("proxy.iogrid.org", 443), timeout=8)
s.sendall(b"\x05\x01\x02")   # SOCKS5, 1 method, METHOD=USERPASS
print(s.recv(2))              # expected b"\x05\x02"; today: timeout
```

Once Layers 2 + 3 are green, that snippet should return `b"\x05\x02"`
within ~200 ms. Once Layer 1 is also green, the full vCard smoke above
exercises the same path with a real LinkedIn fetch.

---

## What's in this repo to support the runbook

| Path                                                                          | Purpose                                                              |
|-------------------------------------------------------------------------------|----------------------------------------------------------------------|
| [`gitops/flux/`](../gitops/flux/)                                             | Directly-applicable Flux bootstrap manifests for Layer 2.            |
| [`gitops/README.md`](../gitops/README.md)                                     | Founder quick-start for the gitops directory.                        |
| [`gitops/flux/iogrid-secrets-skeleton.yaml`](../gitops/flux/iogrid-secrets-skeleton.yaml) | Exact key set for the 7 secrets + `kubectl create secret` recipes. |
| [`installer/macos/install-iogridd.sh`](../installer/macos/install-iogridd.sh) | curl-pipe-sh installer for the Mac daemon — handles Layer 1.         |
| [`installer/macos/io.iogrid.daemon.plist`](../installer/macos/io.iogrid.daemon.plist) | LaunchAgent template the installer drops into `~/Library/LaunchAgents/`. |
| [`docs/PHASE0-SETUP.md`](./PHASE0-SETUP.md)                                   | Operator-only reverse-SSH tunnel setup (separate concern from #201). |
| [`docs/PHASE0_FIRST_CUSTOMER.md`](./PHASE0_FIRST_CUSTOMER.md)                 | Customer-side walkthrough of the vCard smoke (acceptance test).      |
