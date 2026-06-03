# Flux Kustomizations — reference copies

> ## 🔴 DO NOT UNSUSPEND / DO NOT RECONCILE onto prod yet (#637)
>
> The `iogrid-prod` and `iogrid-staging` Kustomizations here are **`suspend:
> true`** on purpose. Reconciling `overlays/prod` onto the live contabo-mkt
> cluster **crashloops the stack** — it caused a multi-service prod incident on
> 2026-06-03 (`iogrid.org` 502 + identity/providers/vpn/proxy-gateway
> CrashLoopBackOff). `kubectl diff` / `--dry-run=server` are **NOT** sufficient
> validation (they pass while the apply still breaks). **3 gates must clear
> before unsuspending:**
> 1. **Secrets provisioned** — `identity-svc-secrets` (Google OAuth +
>    `IOGRID_SERVICE_TOKEN`) + `proxy-gateway-secrets` (`DEV_API_KEYS`); else
>    those svcs lose env on apply → crashloop. Values already exist in-cluster;
>    consolidate via SealedSecrets (#640).
> 2. **Routing wired** — `infra/k8s/traefik/` (working Traefik IngressRoutes +
>    Middlewares + Certs, captured 2026-06-03) referenced by `base`; else the
>    build ships only inert Gateway-API HTTPRoutes (cilium Gateway
>    unprogrammed).
> 3. **Off-prod RUNTIME validation** of `overlays/prod` on a replica cluster.
>
> Until then, the ONLY safe deploy is `scripts/reroll-iogrid-deployments.sh`
> (image-only). `prune: false` until first adoption is validated.

This directory holds **reference copies** of the Flux Kustomization CRs that the
mothership cluster applies. The **authoritative** copies live in the separate
ops repo:

> https://github.com/iogrid/iogrid-ops

The flow is:

```
iogrid/iogrid (this repo) ── infra/k8s/{base,overlays/*}    ◄── source of truth for manifests
              │                                                  Flux pulls from main branch
              ▼
iogrid/iogrid-ops          ── clusters/<cluster>/iogrid/      ◄── per-cluster Flux Kustomization
                                                                   binds the cluster to a path here
```

Each Flux Kustomization references a `GitRepository` pointing to `iogrid/iogrid`
on a specific branch, and selects an overlay path:

| Cluster                 | Branch | Overlay path                              |
|-------------------------|--------|--------------------------------------------|
| `contabo-mothership`    | `main` | `infra/k8s/overlays/prod`                  |
| `staging`               | `main` | `infra/k8s/overlays/staging`               |

The `iogrid-ops` repo additionally holds:

- `GitRepository` CR for `iogrid/iogrid`
- ImagePolicy / ImageUpdateAutomation CRs for SHA-pinned per-service rollouts
- Flux notification provider (Slack / GH webhook)
- Bootstrap kustomization (`clusters/<name>/flux-system/`)
