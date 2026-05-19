#!/usr/bin/env bash
# seed.sh — deploy a stripped-down iogrid service stack onto kind.
#
# This is NOT a copy of infra/k8s/base — that overlay assumes CNPG /
# Cilium / cert-manager CRDs which we skipped in bootstrap.sh. Instead
# we apply hand-rolled Deployments + Services that point at the same
# scaffold images (ghcr.io/iogrid/<svc>:scaffold) but with envs wired
# to the kind-local Postgres + NATS + MailHog.
#
# When BUILD_LOCAL=1 we additionally build local images from the
# coordinator/ tree and `kind load docker-image` them in — much slower
# but lets devs iterate without push-to-ghcr.

set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
NS=${NAMESPACE:-iogrid}
BUILD_LOCAL=${BUILD_LOCAL:-0}

log() { printf '%s [seed] %s\n' "$(date -u +%FT%TZ)" "$*"; }

if [ "$BUILD_LOCAL" = "1" ]; then
  log "BUILD_LOCAL=1 — building and loading local images"
  for svc in identity-svc providers-svc workloads-svc antiabuse-svc \
             billing-svc proxy-gateway; do
    log "  build $svc"
    (
      cd "$HERE/.."
      docker build -f "coordinator/services/$svc/Dockerfile" \
        -t "ghcr.io/iogrid/$svc:e2e" \
        --build-arg TARGETOS=linux --build-arg TARGETARCH=amd64 \
        coordinator >/dev/null
    )
    kind load docker-image "ghcr.io/iogrid/$svc:e2e" --name iogrid-e2e
  done
  IMAGE_TAG=e2e
else
  IMAGE_TAG=${IMAGE_TAG:-scaffold}
fi

log "deploying services (image tag: $IMAGE_TAG)"
# Render the stack via envsubst so a single template handles tag swaps.
# NB: envsubst is part of gettext-base — pre-installed on ubuntu-latest.
for f in "$HERE"/manifests/services/*.yaml; do
  IMAGE_TAG="$IMAGE_TAG" envsubst < "$f" | kubectl -n "$NS" apply -f -
done

log "waiting for service rollouts (90s each, parallel)"
PIDS=()
for svc in identity-svc providers-svc workloads-svc antiabuse-svc \
           billing-svc proxy-gateway; do
  (
    kubectl -n "$NS" rollout status deployment/$svc --timeout=90s \
      && echo "OK   $svc" \
      || echo "WARN $svc (continuing)"
  ) &
  PIDS+=($!)
done
for p in "${PIDS[@]}"; do wait "$p" || true; done

log "current pod state:"
kubectl -n "$NS" get pods -o wide

log "seed complete"
