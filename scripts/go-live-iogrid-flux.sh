#!/usr/bin/env bash
# go-live-iogrid-flux.sh — rollback-guarded go-live for the iogrid Flux Kustomization (#637).
#
# Context: iogrid is NOT yet Flux-managed. Its reference Kustomizations
# (infra/k8s/flux/kustomization-{prod,staging}.yaml) are suspend:true on
# purpose — a naive reconcile of overlays/prod onto the live cluster caused a
# multi-service incident on 2026-06-03. Since then EVERY blocker has been fixed
# and validated: netpols removed, per-svc config + sealed secrets provisioned,
# the working Traefik routing wired into base, replicas pinned to live (so a
# reconcile is a no-op on replicas), and the off-prod runtime gate
# (k8s-validate "#637 gate") is GREEN. A live `kubectl diff` confirmed the
# reconcile delta is a clean rolling deploy (restartedAt + 2 image bumps + 1
# additive secretRef + the new in-cluster MinIO), with NO immutable-selector
# changes and NO env-loss/crashloop pattern.
#
# This script makes the FINAL go-live step a single, guarded command instead of
# an ad-hoc operation. It DRY-RUNs by default; pass GO=1 to actually execute.
# On ANY health regression within the watch window it AUTO-ROLLS-BACK (suspends
# the Kustomization) so a bad reconcile cannot linger.
#
# Usage:
#   ./scripts/go-live-iogrid-flux.sh            # DRY-RUN: pre-flight checks only
#   GO=1 ./scripts/go-live-iogrid-flux.sh        # EXECUTE with rollback guard
#
# Requires: kubectl pointed at the prod cluster; flux CLI (optional, falls back
# to kubectl patch); the iogrid-prod Flux Kustomization already INSTALLED in
# flux-system (suspended). If it is not installed yet, this script reports that
# and stops — installing it is a one-time `kubectl apply` of
# infra/k8s/flux/kustomization-prod.yaml (with suspend:false) into the cluster's
# flux-system, which the operator does once; thereafter go-live is this script.
set -uo pipefail

NS=iogrid
KUST=iogrid-prod
EDGE_URL=${EDGE_URL:-https://iogrid.org}
WATCH_SECS=${WATCH_SECS:-300}   # how long to watch health after unsuspend
POLL_SECS=${POLL_SECS:-15}
GO=${GO:-0}

say() { printf '\033[1m[go-live]\033[0m %s\n' "$*"; }
fail() { printf '\033[31m[go-live] FAIL:\033[0m %s\n' "$*" >&2; }

edge_ok() { [ "$(curl -s -o /dev/null -w '%{http_code}' --max-time 10 "$EDGE_URL" 2>/dev/null)" = "200" ]; }
nonrunning_pods() { kubectl -n "$NS" get pods --no-headers 2>/dev/null | awk '$3!="Running"&&$3!="Completed"&&$3!="Succeeded"' | wc -l | tr -d ' '; }

# ── 1. Pre-flight: nothing proceeds unless the cluster is healthy NOW ──────────
say "pre-flight: cluster + gate checks"
edge_ok || { fail "$EDGE_URL is not 200 right now — refusing to go live on an unhealthy edge"; exit 1; }
say "  ✓ edge $EDGE_URL = 200"
base_nonrunning=$(nonrunning_pods)
say "  ✓ non-running pods (baseline): $base_nonrunning"
[ "$base_nonrunning" -gt 0 ] && { fail "$base_nonrunning pod(s) not Running before go-live — stabilise first"; exit 1; }

# Confirm the Kustomization exists in flux-system (suspended) to unsuspend.
if ! kubectl -n flux-system get kustomization "$KUST" >/dev/null 2>&1; then
  fail "Kustomization flux-system/$KUST is NOT installed. One-time operator step:"
  fail "  kubectl apply -f infra/k8s/flux/kustomization-prod.yaml  (set suspend:false there, or let this script flip it)"
  fail "Then re-run this script for the guarded reconcile."
  exit 2
fi
suspended=$(kubectl -n flux-system get kustomization "$KUST" -o jsonpath='{.spec.suspend}' 2>/dev/null)
say "  ✓ flux-system/$KUST present (suspend=$suspended)"

if [ "$GO" != "1" ]; then
  say "DRY-RUN complete. Pre-flight is GREEN. Re-run with GO=1 to unsuspend + reconcile under the rollback guard."
  exit 0
fi

# ── 2. Execute: unsuspend (flux resume, or kubectl patch fallback) ────────────
say "GO=1 — unsuspending $KUST"
if command -v flux >/dev/null 2>&1; then
  flux resume kustomization "$KUST" -n flux-system --timeout=10m || true
else
  kubectl -n flux-system patch kustomization "$KUST" --type=merge -p '{"spec":{"suspend":false}}'
  kubectl -n flux-system annotate kustomization "$KUST" reconcile.fluxcd.io/requestedAt="$(kubectl -n flux-system get kustomization "$KUST" -o jsonpath='{.metadata.resourceVersion}')" --overwrite
fi

# ── 3. Watch health; AUTO-ROLLBACK (re-suspend) on any regression ─────────────
rollback() {
  fail "health regression detected — ROLLING BACK ($1)"
  kubectl -n flux-system patch kustomization "$KUST" --type=merge -p '{"spec":{"suspend":true}}' 2>/dev/null
  fail "re-suspended $KUST. If a Deployment was already rolled bad, run: kubectl -n $NS rollout undo deploy/<svc>"
  fail "edge=$EDGE_URL  non-running pods=$(nonrunning_pods)"
  exit 3
}
say "watching health for ${WATCH_SECS}s (edge 200 + 0 new non-running pods)…"
elapsed=0
while [ "$elapsed" -lt "$WATCH_SECS" ]; do
  sleep "$POLL_SECS"; elapsed=$((elapsed+POLL_SECS))
  edge_ok || rollback "edge $EDGE_URL != 200 at ${elapsed}s"
  nr=$(nonrunning_pods)
  [ "$nr" -gt "$base_nonrunning" ] && rollback "$nr non-running pods at ${elapsed}s (baseline $base_nonrunning)"
  say "  [${elapsed}s] edge 200 ✓  non-running pods $nr ✓"
done

say "✅ GO-LIVE SUCCESS — $KUST reconciled, edge healthy, no new non-running pods over ${WATCH_SECS}s."
say "iogrid is now Flux-managed. Verify: kubectl -n flux-system get kustomization $KUST"
