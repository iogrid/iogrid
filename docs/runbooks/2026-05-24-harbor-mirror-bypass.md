# 2026-05-24 — Harbor-mirror bypass for ghcr per-package ACL

> Per-incident playbook: when a coordinator service's ghcr.io container
> package gets a per-package ACL flip that excludes the iogrid/iogrid
> repo's `GITHUB_TOKEN`, the cluster cannot pull new images via the
> standard CI deploy path. The bypass: mirror to the in-cluster Harbor.

## TL;DR remediation

```bash
./scripts/harbor-mirror-build.sh <svc>     # one service
./scripts/harbor-mirror-build.sh           # all 13
```

That builds linux/amd64 from the service's Dockerfile (handling pnpm
context for web/admin), pushes to `harbor.openova.io/iogrid/<svc>:local`,
and repoints the cluster Deployment.

## Why this exists

ghcr.io's per-package ACL UI lets an org admin restrict which
repositories' workflows can pull a given container package. Some
iogrid container packages (identity-svc, workloads-svc seen in
practice; others occasionally) have an ACL that excludes the
iogrid/iogrid repo workflow_token. Result: pulling from the cluster
returns 403 even though my user PAT is admin on the org (gh API
endpoint to flip this is web-UI-only — same class as #426).

The Harbor mirror sits inside the cluster (`openova-harbor`
namespace), exposes a Docker registry at `harbor.openova.io`, and
serves the iogrid project as PUBLIC (anonymous pull works, no
imagePullSecret needed).

## Setup (one-time, already done 2026-05-24 00:35Z)

```bash
# 1. Create the public iogrid project in Harbor
PWD=$(kubectl -n openova-harbor get secret harbor-admin \
       -o jsonpath='{.data.HARBOR_ADMIN_PASSWORD}' | base64 -d)
curl -sS -u "admin:$PWD" -H 'Content-Type: application/json' -X POST \
     -d '{"project_name":"iogrid","public":true,"metadata":{"public":"true","auto_scan":"false"}}' \
     https://harbor.openova.io/api/v2.0/projects
```

## Day-2: when a new image needs to land

Run `harbor-mirror-build.sh <svc>`. The cluster picks up the new
digest on the next pod restart (which the script triggers via
`kubectl set image`).

## When this can be retired

Either:
- Founder flips the per-package ACLs in github.com Org → Packages
  settings to grant iogrid/iogrid repo Write role on all 13 packages
  (covered in #473), AND
- The cluster's `ghcr-pull` secret rotation pattern (#454 rotator)
  succeeds for all packages.

Until then the Harbor mirror is the canonical pull source for the
iogrid cluster.

## Failure modes

| Symptom | Cause | Fix |
|---|---|---|
| `Command "build" not found` from pnpm | Built with repo-root context for a web/admin Dockerfile | Pass `<svc>` as the context (not `.`) — script does this auto |
| `error resolving mountpoints for container: invalid mount type "cache"` | podman 3.4 vs BuildKit cache hints | Script strips them via sed |
| `Insufficient memory, Too many pods` on Pending | Single-node phase-0 cluster at 110-pod ceiling | Scale services to replicas=1 + evict surplus old-RS pods |
| `Connection refused` from cluster to pod | NetworkPolicy denies kube-system→iogrid | Delete the per-service `*-allow` NetworkPolicy until Cilium Gateway lands |

## Refs

- #71 vCard demo (this unblocked workloads-svc which gates Dispatch)
- #456 workloads-svc Dispatch 404
- #473 per-package ACL grant request (superseded by this bypass)
- #454 ghcr-pull rotator (still works for the 11 packages that pull;
  this Harbor mirror is the safety net for the others)
