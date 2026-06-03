# gitops — Phase 0 Flux bootstrap (historical / NOT the live deploy path)

> ## 🔴 iogrid is NOT Flux-wired today
>
> This directory captures the **original Phase 0 Flux-bootstrap intent**. It is
> **not** how iogrid prod is deployed today. The Flux Kustomizations are
> **suspended** and a wholesale apply of `infra/k8s/base` / `overlays/prod`
> **crashloops the stack** (multi-service prod incident 2026-06-03 — see #636 /
> #637 and `infra/k8s/flux/README.md`). The ONLY safe deploy is
> `scripts/reroll-iogrid-deployments.sh` (image-only). Treat the bootstrap
> recipe below as historical context, not a runbook to execute against prod.

This directory contains the **Phase 0 unblock** Flux manifests for getting
the iogrid coordinator services running on the openova-io mothership
cluster. It is the iogrid-side half of issue
[#201](https://github.com/iogrid/iogrid/issues/201) Layer 2 (coordinator
services not deployed).

> This directory is intentionally separate from `infra/k8s/flux/`, which
> holds *reference copies* of the canonical Flux CRs that live in
> `iogrid/iogrid-ops`. `gitops/flux/` is the **directly-applicable**,
> founder-facing bootstrap set: one `kubectl apply -k` and Flux starts
> reconciling the iogrid namespace on the mothership.

---

## Layout

```
gitops/
├── README.md                                # this file
└── flux/
    ├── kustomization.yaml                   # `kubectl apply -k` entry point
    ├── iogrid-namespace.yaml                # iogrid ns + Cilium labels
    ├── iogrid-source.yaml                   # GitRepository → iogrid/iogrid
    ├── iogrid-kustomization.yaml            # Flux Kustomization → infra/k8s/base
    └── iogrid-secrets-skeleton.yaml         # template — DO NOT apply as-is
```

---

## Quick-start (founder, on the mothership)

Prerequisites:

- `kubectl` configured against the mothership cluster (default kubeconfig
  on the bastion: `~/.kube/config` → `https://45.151.123.50:6443`).
- Flux v2 already installed on the cluster (`flux check`).
- CNPG operator installed (the iogrid base ships a `Cluster` CR).
- Cilium with cluster-mesh + mutual-auth enabled (per
  [`infra/k8s/cilium-mutual-auth-feature.yaml`](../infra/k8s/base/cilium-mutual-auth-feature.yaml)).

### 1. Pre-create the `iogrid-flux-vars` ConfigMap

The Kustomization uses `postBuild.substituteFrom` against a ConfigMap of
non-secret cluster-side variables. Create it once:

```bash
kubectl -n flux-system create configmap iogrid-flux-vars \
  --from-literal=PUBLIC_API_BASE=https://api.iogrid.org \
  --from-literal=PUBLIC_PROXY_BASE=https://proxy.iogrid.org \
  --from-literal=PUBLIC_APP_BASE=https://iogrid.org \
  --from-literal=MOTHERSHIP_REGION=hz-fsn1
```

### 2. Apply the Flux bootstrap

```bash
# From a checkout of iogrid/iogrid (any branch — the manifests are static):
kubectl apply -k gitops/flux/
```

This creates:

- `Namespace/iogrid` (with Cilium + PSS labels)
- `GitRepository/iogrid` in `flux-system` (1-min pull interval)
- `Kustomization/iogrid` in `flux-system` (5-min reconcile, 10-min timeout)

### 3. Populate the secrets

The chart references 7 Secrets that the bootstrap deliberately does NOT
create. See [`flux/iogrid-secrets-skeleton.yaml`](flux/iogrid-secrets-skeleton.yaml)
for the exact key set and inline `kubectl create secret` recipes:

| # | Secret name              | Owner / how to obtain                                   |
|---|--------------------------|---------------------------------------------------------|
| 1 | `iogrid-google-oauth`    | Google Cloud Console > APIs & Services > Credentials   |
| 2 | `iogrid-smtp`            | Stalwart admin (mail.openova.io) — create service principal |
| 3 | `iogrid-database`        | wrap CNPG's auto-generated `iogrid-pg-app` Secret      |
| 4 | `iogrid-nats`            | in-cluster URL; no creds needed for Phase 0            |
| 5 | `iogrid-redis`           | bitnami Redis chart's auto-generated password          |
| 6 | `iogrid-solana-payout`   | `solana-keygen new -o` → fund the resulting pubkey     |
| 7 | `iogrid-apollo`          | https://app.apollo.io/#/settings/integrations/api      |

Verify:

```bash
kubectl -n iogrid get secrets | grep iogrid-
# expect 7 rows (plus iogrid-pg-app from CNPG = 8 total)
```

### 4. Watch reconcile

```bash
flux get all -n flux-system        # both objects → Ready=True
flux get kustomization iogrid -n flux-system -A
kubectl -n iogrid get deploy       # 7 deploys, eventually all Ready
```

Expected after ~5 minutes:

```
NAME            READY   UP-TO-DATE   AVAILABLE
identity-svc    1/1     1            1
providers-svc   1/1     1            1
workloads-svc   1/1     1            1
antiabuse-svc   1/1     1            1
billing-svc     1/1     1            1
telemetry-svc   1/1     1            1
gateway-bff     1/1     1            1
```

If any Deployment is stuck `0/1`, inspect with:

```bash
kubectl -n iogrid describe deploy <name>
kubectl -n iogrid logs deploy/<name> --tail=200
```

The most common Phase 0 cause is a missing/misspelled Secret — re-read
the skeleton and confirm every key exists.

---

## What this does NOT solve

This bootstrap addresses **Layer 2** of #201 only. The two other layers
of the Phase 0 blocker need separate work:

- **Layer 3** (Traefik 404 / Cilium Gateway gap) — even with all 7 pods
  Ready, external clients hitting `https://api.iogrid.org/healthz` will
  still see the Traefik default cert + 404 until either (a) the
  `infra/k8s/gateways/*.yaml` manifests get translated to Traefik
  `IngressRoute`/`IngressRouteTCP`, or (b) the mothership finishes the
  Traefik → Cilium Gateway migration. This is mothership-side work; see
  the Layer 3 section of [`docs/PHASE0-UNBLOCK.md`](../docs/PHASE0-UNBLOCK.md).

- **Layer 1** (Mac daemon not installed) — solved by
  [`installer/macos/install-iogridd.sh`](../installer/macos/install-iogridd.sh).
  Run that AFTER Layers 2 and 3 are green, so the daemon has a working
  coordinator to pair against.

See [`docs/PHASE0-UNBLOCK.md`](../docs/PHASE0-UNBLOCK.md) for the full
step-by-step runbook.

---

## Updating the bootstrap

The three Flux CRs above are version-controlled in THIS repo. Changes
to `infra/k8s/base/*` flow through naturally — Flux just pulls main and
reconciles. Changes to the bootstrap *itself* (interval, healthChecks,
ignore patterns) require:

1. Edit the relevant file in `gitops/flux/`.
2. PR + merge.
3. Founder re-runs `kubectl apply -k gitops/flux/` on the mothership.

Flux does NOT self-reconcile its own bootstrap CRs — by design. The
authoritative copy lives in `iogrid/iogrid-ops` for cluster-bootstrap
chains that want it managed by Flux itself; this directory is the
human-facing, direct-apply variant.
