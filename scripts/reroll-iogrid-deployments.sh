#!/usr/bin/env bash
# reroll-iogrid-deployments.sh — re-roll iogrid Deployments to the image
# digests currently pinned in gitops (the repo's `infra(<svc>): deploy …`
# markers), bypassing a stuck shared Flux `apps` Kustomization.
#
# WHY THIS EXISTS (#636): iogrid has no dedicated Flux Kustomization — every
# iogrid Deployment is reconciled by the shared `flux-system/apps`
# Kustomization. When `apps` goes `Ready=False` because an UNRELATED tenant
# is unhealthy (observed: `powerdns` OCIRepository ghcr pull DENIED), Flux
# stops applying iogrid manifest bumps, so prod silently freezes on stale
# images while git keeps advancing. This script applies the gitops-declared
# images directly to the cluster — namespace-scoped, so it cannot affect
# other tenants. It is a BRIDGE, not a fix: the durable fix is to repair the
# blocking tenant OR give iogrid its own Flux Kustomization (see #636).
#
# Safe + idempotent: it only `set image`s a deployment whose live digest
# differs from the gitops-declared one. Re-running when everything is current
# is a no-op.
#
# Usage:
#   scripts/reroll-iogrid-deployments.sh            # apply
#   DRY_RUN=1 scripts/reroll-iogrid-deployments.sh  # show what would change
set -euo pipefail

NS="${NS:-iogrid}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

# Services that ship via the `infra(<svc>): deploy …@sha256:… after CI …`
# auto-deploy markers on the default branch. This list MUST stay in sync with
# the set of services whose CI emits those markers — a marker-emitting service
# left off this list silently stays stale (the exact #636 failure). Audited
# 2026-06-03 against `git log | grep 'infra(<svc>): deploy'` ∩ live Deployments.
SERVICES=(
  web gateway-bff billing-svc identity-svc providers-svc vpn-svc
  workloads-svc vpn-gateway telemetry-svc proxy-gateway build-gateway antiabuse-svc
  admin releases
)

changed=0
for svc in "${SERVICES[@]}"; do
  # Latest gitops-declared digest = the most recent deploy marker for this svc.
  # `|| true`: under `set -euo pipefail` an empty grep (a service with no harbor
  # deploy marker yet, e.g. admin before its first harbor build) returns non-zero
  # and would abort the whole script — skipping every later service — instead of
  # falling through to the graceful "no deploy marker" skip below.
  gitops_img="$(git log origin/main --oneline -200 2>/dev/null \
    | grep -iE "infra.${svc}.* deploy " \
    | head -1 \
    | grep -oE "harbor.openova.io/iogrid/${svc}@sha256:[a-f0-9]+" || true)"
  if [[ -z "$gitops_img" ]]; then
    echo "skip  ${svc}: no deploy marker found"
    continue
  fi

  live_img="$(kubectl -n "$NS" get deploy "$svc" \
    -o jsonpath='{.spec.template.spec.containers[0].image}' 2>/dev/null || true)"
  if [[ -z "$live_img" ]]; then
    echo "skip  ${svc}: deployment not found in ns/${NS}"
    continue
  fi

  if [[ "$live_img" == "$gitops_img" ]]; then
    echo "ok    ${svc}: already at gitops digest"
    continue
  fi

  cn="$(kubectl -n "$NS" get deploy "$svc" \
    -o jsonpath='{.spec.template.spec.containers[0].name}')"
  echo "ROLL  ${svc}: ${live_img##*@} -> ${gitops_img##*@}"
  changed=$((changed + 1))
  if [[ "${DRY_RUN:-0}" == "1" ]]; then
    continue
  fi
  kubectl -n "$NS" set image "deploy/${svc}" "${cn}=${gitops_img}"
done

if [[ "${DRY_RUN:-0}" == "1" ]]; then
  echo "DRY_RUN: ${changed} deployment(s) would be rolled."
  exit 0
fi

if [[ "$changed" -eq 0 ]]; then
  echo "All iogrid deployments already at gitops digests — nothing to do."
  exit 0
fi

echo "Rolled ${changed} deployment(s). Waiting for rollouts…"
for svc in "${SERVICES[@]}"; do
  kubectl -n "$NS" rollout status "deploy/${svc}" --timeout=180s 2>/dev/null || \
    echo "warn  ${svc}: rollout did not complete in 180s — check probes/logs"
done
echo "Done. Verify: kubectl -n ${NS} get deploy"
