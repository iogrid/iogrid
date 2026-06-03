# infra/k8s — Kubernetes manifests

> ## 🔴 iogrid is NOT Flux-wired, and `overlays/prod` is NOT a safe live apply
>
> The reference Flux Kustomizations (`infra/k8s/flux/`) are **`suspend: true`**.
> Reconciling / `kubectl apply -k overlays/prod` onto the live cluster
> **crashloops the stack** — it caused a multi-service prod incident on
> 2026-06-03 (identity / providers / vpn / proxy-gateway CrashLoopBackOff +
> `iogrid.org` 502) because the manifests have drifted from the working prod
> runtime and depend on secrets/routing not yet consolidated (see #636/#637
> and `infra/k8s/flux/README.md`). `kubectl diff` / `--dry-run=server` are
> **NOT** sufficient validation — they pass while the apply still breaks.
>
> **The ONLY safe deploy today is `scripts/reroll-iogrid-deployments.sh`**,
> which re-rolls the running Deployments to the image digests pinned in gitops
> (image-only, namespace-scoped). The Kustomize overlays below remain the
> source of truth for workload *shape* and are exercised off-prod by CI
> (`k8s-validate.yml`), but are not applied to prod wholesale until the
> unsuspend gates in `flux/README.md` clear.

Kustomize layout that targets three environments from one shared base.

```
infra/k8s/
├── base/                 # Operator-coupled prod manifests (CNPG, Cilium,
│                         # cert-manager, sealed-secrets, prometheus-operator).
│                         # NOT directly applyable on a vanilla cluster — use
│                         # an overlay below.
├── overlays/
│   ├── dev/              # kind / local-dev — ZERO operator prereqs
│   ├── staging/          # *.staging.iogrid.org — LE staging, lower replicas
│   └── prod/             # *.iogrid.org — full HPA, PDB, LE prod
├── flux/                 # Flux GitRepository + Kustomization wiring
├── cert-manager/         # Out-of-band ClusterIssuer Helm-managed install
├── certificates/         # Out-of-band Certificate templates
├── gateways/             # Gateway + Route templates referenced by base
└── namespaces/           # Namespace templates referenced by base
```

## Apply paths

| Target | Command | Prereqs |
|---|---|---|
| **kind / local dev** | `kubectl apply -k infra/k8s/overlays/dev` | kind (kindnet, no operators) |
| **staging** | `kubectl apply -k infra/k8s/overlays/staging` | full operator stack (see below) |
| **prod (live)** | `scripts/reroll-iogrid-deployments.sh` (image-only) | running prod cluster |
| **prod (full kustomize apply)** | 🔴 **not safe — see banner above (crashloops prod)** | gated on #637 unsuspend criteria |

### Why `kubectl apply -k infra/k8s/base` is intentionally not supported

The `base/` kustomization is the source of truth for prod workload
shapes — Deployments, NetworkPolicies, Services, HPAs — but it also
declares resources that require operators not present on a vanilla
cluster:

| Resource | Operator |
|---|---|
| `Cluster` (`postgresql.cnpg.io/v1`) | [CloudNativePG](https://cloudnative-pg.io) |
| `Certificate`, `ClusterIssuer` | [cert-manager](https://cert-manager.io) |
| `Gateway`, `HTTPRoute`, `TLSRoute`, `ReferenceGrant` | [Gateway-API](https://gateway-api.sigs.k8s.io) + a controller (Cilium or Envoy Gateway) |
| `ServiceMonitor`, `PrometheusRule` | [prometheus-operator](https://prometheus-operator.dev) |
| Namespace label `io.cilium.k8s.policy.cluster` | [Cilium](https://cilium.io) ClusterMesh |
| `SealedSecret` references | [sealed-secrets](https://sealed-secrets.netlify.app) |

`kubectl apply -k infra/k8s/base` on a kind cluster fails with:

```
error: resource mapping not found for name: "iogrid-pg" namespace: "iogrid":
  no matches for kind "Cluster" in version "postgresql.cnpg.io/v1"
```

Use `overlays/dev/` to side-step every operator dependency.

## Dev overlay — what it does

`infra/k8s/overlays/dev/kustomization.yaml`:

1. Inherits the full base via `resources: [../../base]`
2. Removes the operator-coupled base resources via strategic-merge
   `$patch: delete` (CNPG Cluster, cert-manager Certificate/ClusterIssuer,
   Gateway-API Gateway/HTTPRoute/TLSRoute/ReferenceGrant, prometheus-operator
   ServiceMonitor/PrometheusRule)
3. Strips the Cilium namespace label from `iogrid` and switches PSS to
   `baseline` so workloads boot on kindnet without the Cilium agent
4. Adds plain-Postgres + per-service DB ConfigMap stand-in for the
   removed CNPG Cluster (`overlays/dev/data/postgres.yaml`)
5. Adds static self-signed `*.iogrid.local` wildcard TLS Secrets in the
   `iogrid` namespace for each Certificate the base referenced
   (`overlays/dev/certs/wildcard-tls.yaml`) — covers app/api/build/proxy
6. Collapses every Deployment to `replicas: 1` and forces every HPA to
   `min=max=1` (kind clusters have no spare capacity)

The static keypair + Postgres passwords in the dev overlay are checked
in to git and **must never be used outside a kind / local-dev cluster**.
For prod / staging, sealed-secrets + cert-manager generate fresh
credentials on every cluster bootstrap.

### Bring-up

```bash
kind create cluster --name iogrid-dev
kubectl apply -k infra/k8s/overlays/dev
kubectl -n iogrid get pods -w
```

First boot takes 60-90s on a 4-core dev box (Postgres init, image pulls).

## Staging / prod prereqs

If you want to apply `overlays/staging` or `overlays/prod` to a fresh
cluster, install the operators first (the iogrid-ops Flux repo wires
these in via HelmRelease; the manual path is also documented in
`docs/RUNBOOKS.md`):

```bash
# Gateway-API CRDs (experimental channel for TLSRoute)
kubectl apply -f https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.2.0/experimental-install.yaml

# CloudNativePG
kubectl apply --server-side \
  -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.24/releases/cnpg-1.24.1.yaml

# Cilium (with Gateway-API mode + ClusterMesh enabled)
helm install cilium cilium/cilium --version 1.16 \
  --namespace kube-system \
  --set gatewayAPI.enabled=true \
  --set clustermesh.useAPIServer=true

# cert-manager (with CRDs)
helm install cert-manager jetstack/cert-manager --namespace cert-manager \
  --create-namespace --version v1.16.1 --set crds.enabled=true

# Sealed-secrets controller
kubectl apply -f https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.27.0/controller.yaml

# Prometheus-operator (or the kube-prometheus-stack chart)
helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
  --namespace monitoring --create-namespace
```

Once the operators are healthy, `kubectl apply -k infra/k8s/overlays/staging`
(or `prod`) applies the full base + the environment-specific patches.

## Data / backups (iogrid-pg)

The prod Postgres is a CloudNativePG `Cluster` (`base/data/postgres.yaml`).
It does **continuous WAL archiving + PITR** to an **in-cluster MinIO**
(`base/data/minio-backup.yaml`), via the `barmanObjectStore` stanza pointed
at `s3://iogrid-backups/cnpg/iogrid-pg` on `minio.iogrid.svc.cluster.local:9000`
(creds in the `iogrid-pg-backup-s3` SealedSecret). A logical `pg_dump` CronJob
(`base/data/pg-dump-backup.yaml`) runs as a belt-and-braces stop-gap (#642).

## Network policies

Default-deny NetworkPolicies were **removed** — they are not applied in the
base. The one NetworkPolicy that remains is a **scoped allow** for `vpn-svc`
(`base/vpn-svc/networkpolicy.yaml`, `vpn-svc-allow`), permitting ingress from
gateway-bff / providers-svc / proxy-gateway / web and egress to Postgres /
NATS / DNS / Traefik / monitoring / billing-svc.

## CI validation

`.github/workflows/k8s-validate.yml` runs on every PR that touches
`infra/**`:

- `kustomize build` for `base`, `overlays/staging`, `overlays/prod`,
  and `overlays/dev`
- `kubeval --strict --ignore-missing-schemas` against each rendered
  manifest
- `kubectl apply --dry-run=client` then `--dry-run=server` against a
  kind cluster pre-loaded with every relevant CRD set
