# Security

> **WHAT:** Canonical security posture for iogrid — threat model, trust boundaries, service mTLS, secrets, identity, supply chain.
> **AUTHORITY:** Canon. Supersedes (now-removed) `SECURITY-mTLS.md` (folded as §3 below).
> **POINTER:** Architectural threat models (per-workload customer/provider/IP-blame) live in [`ARCHITECTURE.md`](./ARCHITECTURE.md) §9; this doc covers cross-cutting security infrastructure. Legal risk + AUP + counsel framework live in [`BUSINESS-STRATEGY.md`](./BUSINESS-STRATEGY.md) §6. User-global security principles at [`~/.claude/CLAUDE.md`](../CLAUDE.md) §7 ("Security" pattern catalog).

---

## 1. Threat model overview

iogrid's threat surfaces decompose into four classes. The per-workload analysis (malicious customer, malicious provider, provider-IP-blame, LinkedIn-or-banking flagging) lives in [`ARCHITECTURE.md`](./ARCHITECTURE.md) §9 — read that first. This section adds the cross-cutting platform threats that touch every workload.

### 1.1 Cross-cutting threats

| Threat | Surface | Primary mitigation |
|---|---|---|
| **Compromised microservice** (RCE, supply-chain) | Any coordinator service | Per-service SPIFFE-style mTLS (§3) + least-privilege Cilium NetworkPolicy + Kyverno admission |
| **Stolen secret in image** | Container images | No secrets in env / images / git; external-secrets operator (§4) |
| **Stolen secret from running pod** | Pod filesystem | Short-lived SVIDs (§3.4), no long-lived API keys in pods |
| **Tampered image deployed** | Registry / CI | Cosign signature verification at admission (§6); SBOM + grype scan |
| **Insider exfiltration** of provider audit logs | DB / log aggregator | Per-field redaction at log boundary (§5.3); audit-grant flow for raw access |
| **Customer breach pivots to coordinator** | proxy-gateway / build-gateway | Customer-side TLS terminates at gateway, gateway holds no upstream credentials |
| **Daemon impersonation** | Provider-coordinator transport | Per-daemon mTLS using one-time pairing + long-lived bearer (today) → SPIFFE-attested (target, §3.6) |
| **Lateral movement provider → coordinator** | Daemon transport | Coordinator never trusts daemon-supplied identity claims; provider_id comes from the pairing record |

### 1.2 Zero-trust posture

The platform follows the standard zero-trust five tenets:

1. **No implicit trust by network location** — every intra-mesh hop must present an SVID and pass policy (§3).
2. **Least privilege per workload** — each microservice has its own SA + Postgres role + NATS account; cross-service reads only via gRPC.
3. **Continuous verification** — SVIDs rotate sub-hourly via Cilium SPIRE.
4. **Encrypt everything in transit** — TLS at every boundary; bandwidth tunnel uses WireGuard.
5. **Assume breach** — Hubble flow logs capture every denied handshake (§3.5) for forensics.

---

## 2. Trust boundaries

```
┌──────────────────────────────────────────────────────────────────────────┐
│   PUBLIC INTERNET                                                        │
│                                                                          │
│   end-user browser   B2B customer SDK    provider daemon  prometheus     │
│         │                  │                   │           (mothership)  │
└─────────┼──────────────────┼───────────────────┼───────────────┼─────────┘
          │                  │                   │               │
          ▼                  ▼                   ▼               │
   ┌──────────────────────────────────────────────────────┐      │
   │   CILIUM GATEWAY (cluster edge, TLS terminate)       │      │
   │   §3 boundary: NO SVID upstream, NO SPIFFE downstream│      │
   └──────┬───────────────┬──────────────────┬────────────┘      │
          │               │                  │                   │
          ▼               ▼                  ▼                   │
   ┌──────────┐    ┌──────────────┐    ┌────────────────┐        │
   │ web      │    │ gateway-bff  │    │ proxy/build/   │        │
   │ (next.js)│    │ (BFF)        │    │ vpn-gateway    │        │
   └────┬─────┘    └──────┬───────┘    └────────┬───────┘        │
        │                 │                     │                │
        ▼                 ▼                     ▼                │
   ┌────────────────────────────────────────────────────┐        │
   │  CORE MICROSERVICES (intra-mesh, SPIFFE required)  │        │
   │  identity / providers / workloads / antiabuse /    │        │
   │  billing / telemetry / vpn-svc                     │        │
   └────────────────┬───────────────────────────────────┘        │
                    │                                            │
                    ▼                                            │
              ┌─────────────────────┐                            │
              │  Postgres (CNPG)    │  ← per-service role        │
              │  Redis              │  ← per-service auth        │
              │  NATS JetStream     │  ← per-service account     │
              │  S3 / OpenBao       │                            │
              └─────────────────────┘                            │
                                                                 │
              ┌─────────────────────┐                            │
              │  /metrics endpoints │◄───────────────────────────┘
              │  (no SVID required) │   monitoring scrape only
              └─────────────────────┘
```

Each `→` arrow either crosses a trust boundary (requires explicit policy) or stays within one. The boundary at the Gateway is the strongest: nothing from outside the mesh has an SVID, and nothing inside the mesh trusts a header claim from outside without re-verification at the BFF.

---

## 3. Service-to-service mTLS — SPIFFE-style identities via Cilium

> Source: previously `docs/SECURITY-mTLS.md` (merged here on 2026-05-20).

> Status: shipped under issue [#35](https://github.com/iogrid/iogrid/issues/35). Cilium 1.14+ mutual auth + SPIRE-backed workload identities. The plain Kubernetes NetworkPolicy ships in parallel as L3/L4 defence-in-depth.

This section covers the identity model, where the policies live in the tree, how mutual auth is enforced on the wire, and how to debug the common failure modes with `cilium-cli` / `kubectl`.

### 3.1 Identity model

Every iogrid microservice runs under a Kubernetes ServiceAccount whose name matches the service:

| Service          | ServiceAccount    | SPIFFE ID                                              |
|------------------|-------------------|--------------------------------------------------------|
| identity-svc     | `identity-svc`    | `spiffe://iogrid/ns/iogrid/sa/identity-svc`            |
| providers-svc    | `providers-svc`   | `spiffe://iogrid/ns/iogrid/sa/providers-svc`           |
| workloads-svc    | `workloads-svc`   | `spiffe://iogrid/ns/iogrid/sa/workloads-svc`           |
| antiabuse-svc    | `antiabuse-svc`   | `spiffe://iogrid/ns/iogrid/sa/antiabuse-svc`           |
| billing-svc      | `billing-svc`     | `spiffe://iogrid/ns/iogrid/sa/billing-svc`             |
| telemetry-svc    | `telemetry-svc`   | `spiffe://iogrid/ns/iogrid/sa/telemetry-svc`           |
| vpn-svc          | `vpn-svc`         | `spiffe://iogrid/ns/iogrid/sa/vpn-svc`                 |
| gateway-bff      | `gateway-bff`     | `spiffe://iogrid/ns/iogrid/sa/gateway-bff`             |
| proxy-gateway    | `proxy-gateway`   | `spiffe://iogrid/ns/iogrid/sa/proxy-gateway`           |
| vpn-gateway      | `vpn-gateway`     | `spiffe://iogrid/ns/iogrid/sa/vpn-gateway`             |
| build-gateway    | `build-gateway`   | `spiffe://iogrid/ns/iogrid/sa/build-gateway`           |
| web              | `web`             | `spiffe://iogrid/ns/iogrid/sa/web`                     |

The trust domain is `iogrid`. SPIFFE IDs are minted by the Cilium SPIRE server (`cilium-spire-server`, bootstrapped by the iogrid-ops Helm chart) with the format:

```
spiffe://<trust-domain>/ns/<namespace>/sa/<serviceAccount>
```

This is the **default Cilium 1.14 layout** — we do not bake any custom SPIRE registration entries. Each Cilium agent's per-node SPIRE agent auto-discovers pods, asks the kube-apiserver for the pod's SA, and mints a workload SVID with the corresponding SPIFFE ID. Cilium's `mutual-auth-spiffe-enabled: true` agent flag wires the data path so the SPIFFE handshake happens transparently before the L7 path opens on selected ports.

### 3.2 How the policy enforces it

Each microservice ships two policy resources side-by-side:

1. `infra/k8s/base/<svc>/networkpolicy.yaml` — plain `networking.k8s.io/v1 NetworkPolicy`. L3/L4 only (port + IP/pod selectors). Defence-in-depth: still enforced even if Cilium is downgraded or the mutual-auth feature is toggled off.
2. `infra/k8s/base/<svc>/ciliumnetworkpolicy.yaml` — `cilium.io/v2 CiliumNetworkPolicy`. Identity-aware (pod labels → serviceAccount → SPIFFE ID) with `authentication.mode: required` on every intra-mesh ingress rule.

Example ingress rule from `identity-svc/ciliumnetworkpolicy.yaml`:

```yaml
ingress:
  - fromEndpoints:
      - matchLabels:
          app.kubernetes.io/name: gateway-bff
          io.kubernetes.pod.namespace: iogrid
    authentication:
      mode: required
    toPorts:
      - ports:
          - port: "8080"
            protocol: TCP
```

`fromEndpoints` matches the source pod by its labels (which in turn identify the source ServiceAccount, by Cilium's identity derivation). `authentication.mode: required` instructs the Cilium datapath to complete a SPIRE-backed mTLS handshake before allowing the L7 (gRPC :8080) path to open. If the source pod has no SPIFFE identity (e.g. not running under a SA, or running in a different trust domain), the connection is **dropped pre-handshake**, not refused at L7.

### 3.3 Trust-domain boundaries — when SPIFFE is NOT required

Three ingress hops intentionally stay un-authenticated, because the source isn't a SPIFFE workload:

1. **Public Gateway → BFF / proxy-gateway / build-gateway / vpn-gateway** — the source is an end-user; the Gateway terminates customer TLS here, no SPIFFE workload exists upstream.
2. **monitoring namespace → :9090 /metrics scrape** — the Prometheus pod lives in the mothership monitoring trust domain, not the iogrid SPIFFE trust domain. We don't gate observability on mutual auth (would couple two trust domains we want decoupled).
3. **OTLP receivers (telemetry-svc :4317/:4318)** — any iogrid pod can emit telemetry; we don't want a newborn pod that hasn't yet negotiated a SPIRE SVID to drop spans during boot.

For these hops, only the plain `NetworkPolicy` enforces (L3/L4 allow-list).

### 3.4 Cluster bootstrap — enabling mutual auth

The cluster-wide feature flag lives in `infra/k8s/base/cilium-mutual-auth-feature.yaml` — a ConfigMap in `kube-system` that the Cilium Helm chart consumes via `extraConfigMap`. The mothership iogrid-ops repo references it from its `apps/cilium/values.yaml`. After the chart upgrade rolls all Cilium agents, the per-node SPIRE agent picks up the new flag and starts issuing SVIDs for every iogrid pod automatically.

The ConfigMap is intentionally **not** wired into `infra/k8s/base/kustomization.yaml` — the base kustomization rewrites every resource's namespace to `iogrid`, which would mangle the `kube-system` placement. The file ships as a reference manifest applied by Flux from `iogrid-ops`.

To verify the flag took effect:

```bash
kubectl -n kube-system get cm cilium-config -o yaml \
  | grep -E '(mutual-auth-spiffe-enabled|spire-server-address|mesh-auth-spiffe-trust-domain)'
```

Expected:

```yaml
mutual-auth-spiffe-enabled: "true"
spire-server-address: "spire-server.cilium-spire.svc:8081"
mesh-auth-spiffe-trust-domain: "iogrid"
```

### 3.5 Debugging with cilium-cli

#### Verify mutual auth is enabled cluster-wide

```bash
cilium config view | grep mutual-auth
# mutual-auth-spiffe-enabled    true
```

#### Verify a pod has an SVID

```bash
POD=$(kubectl -n iogrid get pod -l app.kubernetes.io/name=identity-svc -o name | head -1)
cilium identity get $(kubectl get $POD -n iogrid -o jsonpath='{.metadata.labels.io\.cilium\.k8s\.policy\.serviceaccount}')
```

Or directly query the SPIRE agent socket from inside the pod:

```bash
kubectl exec -n iogrid $POD -- /bin/sh -c \
  '/usr/local/bin/spire-agent api fetch x509 -socketPath /run/spire/sockets/agent.sock'
```

You should see one SVID with URI SAN `spiffe://iogrid/ns/iogrid/sa/identity-svc`.

#### Watch a denied handshake

The Cilium agent emits Hubble flow events for SPIFFE auth failures. The key field is `auth_type: spire` with `verdict: DROPPED`:

```bash
hubble observe --type drop --label app.kubernetes.io/name=identity-svc \
  --since 5m --output jsonpb \
  | jq 'select(.flow.auth_type == "spire" and .flow.verdict == "DROPPED")'
```

#### Force a handshake from a peer

```bash
SOURCE_POD=$(kubectl -n iogrid get pod -l app.kubernetes.io/name=gateway-bff -o name | head -1)
kubectl exec -n iogrid $SOURCE_POD -- \
  grpcurl -plaintext identity-svc.iogrid.svc.cluster.local:8080 \
  iogrid.identity.v1.IdentityService/Health
```

If mutual auth is healthy this returns `{"status": "OK"}`. If the SPIRE registration for one side hasn't propagated yet, the call hangs for `mesh-auth-mutual-connect-timeout` (5s by default — see the ConfigMap) and then returns `context deadline exceeded`.

#### Common failure: SA mismatch

If a pod is rolled but the operator forgot to bind the deployment to its named ServiceAccount, Cilium derives the SPIFFE ID from the fall-back `default` SA, which has no CiliumNetworkPolicy match. Symptom: ALL outbound calls from that pod start failing with `context deadline exceeded` (timeout from the SPIFFE handshake). Fix: ensure `deployment.spec.template.spec.serviceAccountName` is set to the per-service SA (`identity-svc`, `gateway-bff`, ...).

### 3.6 Rollout posture

- `networkPolicy.mutualAuth.enabled` in the coordinator chart (`coordinator/charts/iogrid/values.yaml`) defaults to `false` so the chart renders cleanly on clusters without Cilium (kindnet dev overlay).
- The kustomize-based GitOps layer (`infra/k8s/base/`) ships the CiliumNetworkPolicy resources unconditionally. On dev (kindnet) the CRDs aren't installed and the manifest application is skipped by Flux's `CustomizationKind: Kustomize` controller after one warn-log. On prod (Cilium), every CNP applies and enforces.

### 3.7 Future work

- Wire the daemon ↔ providers-svc long-lived gRPC stream through the same SPIFFE path. Today the daemon authenticates with a one-time pairing PIN and a long-lived bearer token. Migrating to SPIFFE requires the daemon to run as a SPIRE-attested workload, which needs per-provider SPIRE node attestation — designed but not yet shipped (issue follow-up TBD).
- Apply `CiliumClusterwideNetworkPolicy` for cross-namespace gates (e.g. iogrid → gateway-system → iogrid) when we adopt Cilium ClusterMesh in the multi-region phase.
- Cross-reference the Hubble L7-flow dashboards in `infra/k8s/base/telemetry-svc/assets/dashboards/` once we publish the `iogrid-mtls` Grafana panel.

---

## 4. Secrets policy

### 4.1 No secrets in code, images, or env

- **No secret in any `*.yaml` checked into git.** Anything matching a secret pattern triggers a Kyverno admission block at apply time.
- **No secret in any container image.** SBOM scan in CI greps the final image layer for known credential formats (AWS keys, Stripe sk_*, JWT-shaped tokens) and fails the build.
- **No secret in pod env vars.** Env vars are visible to `kubectl describe pod` for any user with `pods/exec`. Secrets land via projected volume mounts only.

### 4.2 External-secrets operator (ESO)

Secrets in the cluster are projected by `external-secrets.io/v1beta1 ExternalSecret` from a backing store (OpenBao Phase 1+; sealed-secrets for bootstrap only). The store is shared with OpenOva mothership; iogrid has its own `SecretStore` scoped to a per-namespace token.

Reference: `infra/k8s/base/<svc>/externalsecret.yaml`. The naming convention is `iogrid-<svc>-<purpose>` (e.g. `iogrid-billing-stripe`, `iogrid-providers-pairing-pin`).

### 4.3 SPIFFE-derived service identity replaces long-lived API keys

Where possible, intra-cluster auth uses the workload's SVID (§3.1) instead of a shared API key. The exception list:

- Stripe — external SaaS, requires fixed API key in `iogrid-billing-stripe`.
- Solana RPC (Helius) — external, requires fixed token in `iogrid-billing-helius`.
- SMTP (Stalwart) — token in `iogrid-identity-smtp`.
- Dynadot API — token in `iogrid-infra-dynadot`, only accessible by the registrar-update job pod.

All four rotate via the OpenBao "auto-rotate" hook, which writes a new token then triggers `kubectl rollout restart deploy/<svc>` to pick it up.

### 4.4 Secrets the founder is allowed to read directly

Operationally, only two human accounts can decrypt secrets from OpenBao for break-glass:

- The founder's `emrah.baysal@openova.io` MFA-bound account.
- A break-glass account whose credentials live in a sealed envelope (physical safe), never used in normal ops.

All other operators (including hatice.yildiz@openova.io and any future ops staff) receive secrets via projected ESO mounts on a per-pod basis — they can never `vault kv get` the raw secret.

### 4.5 Bootstrap (sealed-secrets) — Phase 0 only

For Phase 0, before OpenBao + ESO are wired on the mothership, a tiny set of sealed-secrets ships the same way:

- `iogrid-providers-db` (CNPG-managed Postgres role for providers-svc).
- `iogrid-identity-smtp` (Stalwart SMTP credentials).

These migrate to ExternalSecrets as soon as the OpenBao tenant onboarding lands.

---

## 5. Identity model

The full user-facing identity model (User → Identifier, magic-link, Google OAuth, auto-merge, JWT issuance, workspace roles, Solana wallet binding) is canonised in [`ARCHITECTURE.md`](./ARCHITECTURE.md) §5. This section adds the security-specific posture.

### 5.1 Authn-vs-authz split

- **identity-svc** owns authentication (issuing JWTs).
- **Every other service** owns its own authorisation (RBAC, ownership checks, rate limits). identity-svc never returns "can hatice edit provider 808ce330?" — providers-svc decides that.
- gateway-bff is the only service that translates user identity into the SVID-bridge headers (`X-Iogrid-User-Id`, `X-Iogrid-Session-Id`, `X-Iogrid-User-Roles`, `X-Iogrid-User-Email`) forwarded to upstream services. Those headers are trusted ONLY because they cross an SVID-authenticated hop — the upstream service trusts gateway-bff's SVID, not the headers themselves.

### 5.2 Session management

- Short-lived access JWT (15 min, RS256). Public key published at `https://iogrid.org/.well-known/jwks.json` (the apex serves the app; `app.iogrid.org` was dropped — EPIC #422).
- Refresh token (30 d, opaque, server-side rotation on use).
- `ListSessions` + `RevokeSession` RPCs (shipped PR #336): user sees every active session with IP/UA, can revoke any except their current one.
- Step-up auth (re-auth within 5 min) required for payout changes, identity merging, wallet binding, account deletion.

### 5.3 Audit log redaction

- All structured logs are JSON via slog (Go) / tracing (Rust).
- Fields tagged `pii: true` (provider IP, customer URL, magic-link email) hash at the log boundary with HMAC-SHA256 under a per-tenant key.
- Raw values are accessible only via an explicit `audit-grant` request opened by an operator, granted by the founder for a bounded time window, and audited in a separate grant log.

---

## 6. Supply chain — image signing + SBOM + provenance

### 6.1 Cosign signatures on every image

Every CI-built image is signed at push time with a project-scoped Sigstore Fulcio identity. The mothership cluster runs a Kyverno admission policy that REQUIRES `cosign verify` to pass against the iogrid Fulcio identity before any image with `iogrid/` prefix can be applied.

Unsigned images (or images signed with a different identity) are rejected at admission, not at runtime.

### 6.2 SBOM via syft + grype scan

CI runs `syft <image> -o spdx-json > sbom.json` then `grype sbom.json --fail-on critical`. Critical CVEs in the build chain block the merge. The SBOM is attached to the image as a Sigstore attestation.

Operators can fetch any deployed image's SBOM via:

```bash
cosign download attestation ghcr.io/iogrid/providers-svc:<sha> --predicate-type spdx | jq -r .payload | base64 -d | jq
```

### 6.3 SLSA provenance

GitHub Actions emits SLSA-3 provenance (build platform attests "this image was built by this workflow from this commit SHA"). Provenance lands as a second attestation alongside the SBOM, also Cosign-verifiable.

This is the chain that gives "image deployed on prod IS the image I reviewed in PR" the cryptographic backing it needs — the runtime check is one `cosign verify-attestation --type slsaprovenance` away.

### 6.4 Future supply-chain hardening

- Bind cosign verification to a specific commit-SHA range per Deployment (deploy-time policy: only this branch's commits are allowed for this service).
- Add Trivy + Snyk for second-source CVE scanning (defence-in-depth against single-scanner blind spots).
- Migrate from ghcr.io to a self-hosted Harbor registry with built-in signature + SBOM enforcement.
- Wire the `infra/k8s/base/<svc>/ciliumnetworkpolicy.yaml` admission policy to also check the signing identity (so an unsigned but L3/L4-allowed pod can't be applied even when admission's main path is bypassed).
