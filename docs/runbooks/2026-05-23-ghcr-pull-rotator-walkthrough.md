# 2026-05-23 — ghcr-pull rotator + image-warmer pattern

> Per-incident playbook for the #454 fix path: revoked hatiyildiz PAT
> → workflow GITHUB_TOKEN rotator with in-cluster image-warmer Pod
> pattern. Captures the mechanism so future PAT rotations follow the
> same path.

## TL;DR remediation

1. Trigger `.github/workflows/ghcr-pull-rotator.yml` manually
   (workflow_dispatch) or wait for the 45-min cron tick.
2. The workflow does 4 things in order:
   a. Wires kubeconfig from GHA Secret IOGRID_KUBECONFIG_B64 (scoped
      ServiceAccount `ghcr-pull-rotator` with patch-only RBAC on
      Secret/ghcr-pull + Deployments)
   b. Mints a Bearer from the workflow `GITHUB_TOKEN` via
      `https://ghcr.io/token?scope=repository:iogrid/<svc>:pull&service=ghcr.io`
   c. Patches the in-cluster ghcr-pull dockerconfigjson with the
      fresh Bearer
   d. Pre-caches every desired image on the node via a one-shot Pod
      whose init-containers each `command: ["sh","-c","true"]` (image
      pull is the side-effect; layer cache lands on the node so
      future pods using those images skip the network pull)
3. Optional: scale down → up affected Deployments to force them to
   pick up the cached layer.

## Why this exists

GitHub's user-PAT auth path to ghcr.io is gated by per-org SAML SSO.
The cluster's previous credential (a classic PAT issued by
`hatiyildiz` user) was revoked, and re-issuing a user PAT requires a
browser flow (web-UI only). The workflow `GITHUB_TOKEN`, by contrast,
is auto-minted on every Actions run with `packages:read` scope on the
iogrid org (because the workflow runs in the iogrid/iogrid repo
context). It expires when the job ends (~1h max), so the rotator
must pre-cache images while the token is still valid.

## Failure modes + recovery

| Symptom | Cause | Recovery |
|---|---|---|
| Workflow `pending` for ≥10 min | GitHub Actions queue backed up | Wait; do not re-fire (queues a duplicate run) |
| Patch step fails with 401 | IOGRID_KUBECONFIG_B64 GHA Secret expired or SA token TTL elapsed | Re-mint the SA kubeconfig (`kubectl -n iogrid get secret ghcr-pull-rotator-token -o jsonpath='{.data.token}' \| base64 -d`) + re-upload via `gh secret set IOGRID_KUBECONFIG_B64 ...` |
| Warmer pod stuck on init container N | Image at index N has wrong digest (e.g., `:scaffold` instead of `@sha256:...`) | Fix the deployment.yaml that pins that bad ref; re-trigger rotator |
| Pull works in workflow but fails in cluster | Token expired between rotator run and kubelet pull attempt | Symptoms of late-bound pulls; ensure warmer pod ran successfully BEFORE deployments roll |

## Why a separate ServiceAccount + scoped kubeconfig?

Minimum blast radius. The SA `ghcr-pull-rotator` in iogrid namespace
has Role bound to: `secrets["ghcr-pull"]` patch, deployments patch,
deployments/scale patch, pods (full for warmer creation). It cannot
read other Secrets, cannot delete Deployments, cannot reach other
namespaces. The kubeconfig stored in GHA Secret embeds this SA's
token only — if GHA Secrets were compromised, the blast radius is
that single capability set, not cluster-admin.

## Refs

- [#454](https://github.com/iogrid/iogrid/issues/454) — root issue
- [#467](https://github.com/iogrid/iogrid/issues/467) — cert-manager Gateway-API solver fix (caused secondary 502s while diagnosing #454)
- `.github/workflows/ghcr-pull-rotator.yml` — implementation
- `scripts/identity-svc-jwt-keypair-gen.sh` — companion script for #452
- `docs/runbooks/jwt-keypair-rotation.md` — companion runbook
