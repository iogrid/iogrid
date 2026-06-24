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

# resolve_gitops_img <svc> — read deploy-marker lines on stdin, echo the most
# recent harbor digest that belongs to EXACTLY <svc> (empty if none).
#
# #822: the old two-stage match (`infra.${svc}.* deploy ` then extract) had a
# PREFIX COLLISION — `${svc}` was an UNANCHORED substring, so antiabuse-svc's
# grep also matched the newer `infra(antiabuse-svc-transparency-report): deploy …`
# marker; `head -1` picked that line, then the harbor-path extraction
# (`…/antiabuse-svc@sha256:`) failed against the `…-transparency-report@sha256:`
# path → empty → "skip: no deploy marker found" → antiabuse-svc frozen stale.
# FIX: select only lines carrying the EXACTLY-anchored conventional-commit scope
# `infra(${svc}): ` (literal parens via grep -F forbid a longer sibling scope),
# then extract this svc's exact harbor path `.../iogrid/${svc}@sha256:` (the
# `@sha256:` boundary forbids a longer sibling image name), THEN `head -1`. So
# head -1 can only land on a digest this svc actually owns — no prefix-named
# sibling can steal the slot. Factored out so scripts/test-reroll-resolver.sh
# can exercise it against a crafted log without touching the real git history.
resolve_gitops_img() {
  local svc="$1"
  grep -F "infra(${svc}): " \
    | grep -oE "harbor.openova.io/iogrid/${svc}@sha256:[a-f0-9]+" \
    | head -1 || true
}

# Allow `source`-ing for tests without running the reroll body.
if [[ "${REROLL_LIB_ONLY:-0}" == "1" ]]; then
  return 0 2>/dev/null || exit 0
fi

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
  # Latest gitops-declared digest for EXACTLY this svc (see resolve_gitops_img,
  # #822 — anchored scope + harbor path, no prefix-collision). The `|| true`
  # inside the function keeps an empty match (a service with no harbor deploy
  # marker yet, e.g. admin before its first harbor build) from aborting the
  # whole script under `set -euo pipefail`; it falls through to the graceful
  # "no deploy marker" skip below.
  gitops_img="$(git log origin/main --oneline -200 2>/dev/null \
    | resolve_gitops_img "$svc")"
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
