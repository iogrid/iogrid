# Flux Kustomizations — reference copies

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
