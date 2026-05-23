#!/usr/bin/env bash
# harbor-mirror-build.sh — build + push iogrid services to the in-cluster
# Harbor mirror so the cluster can pull them without touching ghcr.io.
#
# Why this exists: the ghcr per-package ACL (#473) walls the iogrid/iogrid
# workflow_token from pulling identity-svc + workloads-svc + others. The
# workaround is to mirror everything to harbor.openova.io/iogrid (public
# project, anonymous pull) and repoint each Deployment.
#
# This script:
#   1. Logs into harbor via the in-cluster harbor-admin Secret
#   2. Strips podman-3.4-incompatible --mount=type=cache hints from each
#      service Dockerfile (BuildKit-only syntax)
#   3. Builds linux/amd64 with the correct build context (web/admin need
#      their own dir, not repo root, because they're standalone pnpm apps;
#      coordinator services need repo root because they share coordinator/
#      with shared/internal/ packages)
#   4. Pushes to harbor.openova.io/iogrid/<svc>:local
#   5. Optionally: kubectl set image deploy/<svc> to repoint the cluster
#
# Usage:
#   ./scripts/harbor-mirror-build.sh                  # all services
#   ./scripts/harbor-mirror-build.sh identity-svc     # one
#   ./scripts/harbor-mirror-build.sh --no-repoint     # build+push only

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

ALL_COORD=(
  identity-svc providers-svc workloads-svc antiabuse-svc billing-svc
  telemetry-svc gateway-bff proxy-gateway build-gateway vpn-gateway
)
TOP_LEVEL=(releases)            # repo-root context, services/releases/
FRONTEND=(web admin)             # dir-context Dockerfiles

NO_REPOINT=false
TARGETS=()
for arg in "$@"; do
  case "$arg" in
    --no-repoint) NO_REPOINT=true ;;
    -h|--help)
      grep -E '^#' "$0" | head -25 | sed 's/^# //'
      exit 0 ;;
    *) TARGETS+=("$arg") ;;
  esac
done

if [ ${#TARGETS[@]} -eq 0 ]; then
  TARGETS=("${ALL_COORD[@]}" "${TOP_LEVEL[@]}" "${FRONTEND[@]}")
fi

echo "→ Logging into harbor via in-cluster admin secret"
HARBOR_PWD=$(kubectl -n openova-harbor get secret harbor-admin \
              -o jsonpath='{.data.HARBOR_ADMIN_PASSWORD}' | base64 -d)
echo "$HARBOR_PWD" | podman login harbor.openova.io -u admin --password-stdin

build_and_push() {
  local svc=$1
  local dockerfile context
  if [[ " ${FRONTEND[*]} " =~ " $svc " ]]; then
    dockerfile="$svc/Dockerfile"
    context="$svc"
  elif [[ " ${ALL_COORD[*]} " =~ " $svc " ]]; then
    dockerfile="coordinator/services/$svc/Dockerfile"
    context="."
  elif [[ " ${TOP_LEVEL[*]} " =~ " $svc " ]]; then
    dockerfile="$svc/Dockerfile"
    context="."
  else
    echo "× unknown service: $svc" >&2
    return 1
  fi

  echo
  echo "── $svc ─────────────────────────────────────────"
  echo "  dockerfile: $dockerfile"
  echo "  context:    $context"

  # Strip BuildKit-only --mount=type=cache hints (podman 3.4 chokes).
  local tmp=/tmp/${svc}-Dockerfile.nocache
  sed -e 's|--mount=type=cache,target=/root/.cache/go-build||g' \
      -e 's|--mount=type=cache,target=/go/pkg/mod||g' \
      -e 's|--mount=type=cache,target=/root/.local/share/pnpm/store||g' \
      -e 's|--mount=type=cache,target=/root/.npm||g' \
      "$dockerfile" > "$tmp"

  podman build --platform linux/amd64 \
    -t "harbor.openova.io/iogrid/$svc:local" \
    -f "$tmp" "$context"

  podman push "harbor.openova.io/iogrid/$svc:local"

  if [ "$NO_REPOINT" = false ]; then
    if kubectl -n iogrid get deploy "$svc" >/dev/null 2>&1; then
      kubectl -n iogrid set image \
        "deploy/$svc" \
        "$svc=harbor.openova.io/iogrid/$svc:local"
      echo "✓ $svc — repointed Deployment"
    else
      echo "  (no Deployment named $svc in iogrid namespace; skipping repoint)"
    fi
  fi
}

failures=()
for svc in "${TARGETS[@]}"; do
  if ! build_and_push "$svc"; then
    failures+=("$svc")
  fi
done

if [ ${#failures[@]} -gt 0 ]; then
  echo
  echo "FAILURES: ${failures[*]}" >&2
  exit 1
fi

echo
echo "✓ All ${#TARGETS[@]} services built + pushed (+ repointed unless --no-repoint)."
