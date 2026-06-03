#!/usr/bin/env bash
# iogrid-image-reroll-cron.sh — bastion cron wrapper that closes the #636/#637
# stale-image gap.
#
# THE GAP (proven 2026-06-03): CI builds each service, pushes to
# harbor.openova.io, and commits an `infra(<svc>): deploy …@sha256:… after CI …`
# marker to main — but CI has NO cluster credentials (there is deliberately no
# kubeconfig secret in GitHub Actions). iogrid also has no dedicated Flux
# Kustomization, and the shared `flux-system/apps` Kustomization stalls whenever
# an UNRELATED tenant is unhealthy. Net: markers advance in git while prod keeps
# serving stale images (observed: all 6 services stale, frozen ~14d in #636).
#
# WHY A BASTION CRON (not CI kubeconfig, not Flux-wire):
#   * Putting a prod kubeconfig in GitHub Actions is a real attack surface.
#   * Full-manifest Flux reconcile of overlays/prod is BANNED — it crashloops
#     identity/providers/vpn/proxy-gateway + mutates the DB (incident
#     2026-06-03; a live `kubectl diff` still shows convergence drift, #637).
#   * The bastion already holds the trusted kubectl context and is the only
#     sanctioned place iogrid prod is deployed from.
#
# WHAT IT DOES: runs scripts/reroll-iogrid-deployments.sh — image-only,
# namespace-scoped `kubectl set image`, applied ONLY when the live digest
# differs from the latest gitops-declared digest. It cannot change config,
# cannot touch the CNPG Cluster / DB, and cannot affect other namespaces. The
# digests it applies are always post-CI-green (the marker literally records
# "after CI <sha>"). Idempotent: a no-op when everything is already current.
#
# Install (operator, one-time):
#   ( crontab -l 2>/dev/null; \
#     echo '5,20,35,50 * * * * /home/openova/repos/iogrid/scripts/iogrid-image-reroll-cron.sh >>/tmp/iogrid-image-reroll.log 2>&1' \
#   ) | crontab -
#
# This is the minimal recurrence-prevention for #636. The durable replacement
# is a dedicated, drift-closed Flux Kustomization (#637) once overlays/prod is
# reconciled to live + runtime-health-validated.
set -uo pipefail

CLONE="${IOGRID_REROLL_CLONE:-/home/openova/.local/iogrid-reroll}"
REMOTE="${IOGRID_REROLL_REMOTE:-https://github.com/iogrid/iogrid.git}"
EDGE_URL="${EDGE_URL:-https://iogrid.org}"

ts() { date -u +%Y-%m-%dT%H:%M:%SZ; }
log() { printf '%s [reroll-cron] %s\n' "$(ts)" "$*"; }

# A dedicated checkout keeps the cron clear of the developer working tree
# (concurrent Claude/operator sessions race the primary checkout's branch).
if [[ ! -d "$CLONE/.git" ]]; then
  log "bootstrapping dedicated clone at $CLONE"
  git clone --quiet "$REMOTE" "$CLONE" || { log "FATAL: clone failed"; exit 1; }
fi

cd "$CLONE" || { log "FATAL: cannot cd $CLONE"; exit 1; }
git fetch --quiet origin main || { log "WARN: git fetch failed — using cached markers"; }
git reset --hard --quiet origin/main || { log "WARN: reset failed"; }

# Pre-flight: only deploy onto a currently-healthy edge. If the edge is already
# down, a roll won't help and could muddy the incident — bail and let the
# operator look.
code="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$EDGE_URL" 2>/dev/null)"
if [[ "$code" != "200" ]]; then
  log "SKIP: edge $EDGE_URL = $code (not 200) — refusing to roll onto an unhealthy edge"
  exit 0
fi

log "edge $EDGE_URL=200 @ $(git rev-parse --short HEAD); running sanctioned image-only reroll"
NS="${NS:-iogrid}" bash scripts/reroll-iogrid-deployments.sh
rc=$?

post="$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$EDGE_URL" 2>/dev/null)"
log "reroll exit=$rc; edge after=$post"
exit "$rc"
