# Shared smoke-flow helpers. Source this from each flow with:
#   . "$(dirname "$0")/_lib.sh"
#
# Convention: every flow MUST exit 0 on assertion success, non-zero on
# failure. The runner captures stdout/stderr into <name>.log; print
# tracing freely.

set -euo pipefail

NS=${NAMESPACE:-iogrid}
KUBECTL="kubectl -n $NS"

# Pretty logger.
flow_log() {
  printf '%s [%s] %s\n' "$(date -u +%H:%M:%SZ)" "${FLOW_NAME:-flow}" "$*"
}

# fail "<msg>" — prints to stderr and exits 1.
fail() {
  printf '%s [%s] FAIL: %s\n' "$(date -u +%H:%M:%SZ)" "${FLOW_NAME:-flow}" "$*" >&2
  exit 1
}

# assert_eq <actual> <expected> <context>
assert_eq() {
  if [ "$1" != "$2" ]; then
    fail "expected [$2] got [$1] — $3"
  fi
}

# wait_for_pod_ready <app-label> [timeout-sec=120]
wait_for_pod_ready() {
  local label=$1
  local timeout=${2:-120}
  $KUBECTL wait --for=condition=Ready pod \
    -l "app.kubernetes.io/name=$label" --timeout="${timeout}s"
}

# wait_for_url <url> [timeout-sec=60] — keeps probing until 2xx.
wait_for_url() {
  local url=$1
  local timeout=${2:-60}
  local deadline=$(( $(date +%s) + timeout ))
  while [ "$(date +%s)" -lt "$deadline" ]; do
    if curl -fsS -o /dev/null -w '%{http_code}' "$url" 2>/dev/null | grep -qE '^(2|3)..'; then
      return 0
    fi
    sleep 1
  done
  fail "timeout waiting for $url"
}

# port_forward <service> <local-port>:<remote-port> — backgrounds kubectl
# port-forward and prints its pid. Caller is responsible for kill.
port_forward() {
  local svc=$1
  local ports=$2
  $KUBECTL port-forward "svc/$svc" "$ports" >/tmp/pf-$svc.log 2>&1 &
  local pid=$!
  sleep 2
  if ! kill -0 "$pid" 2>/dev/null; then
    cat /tmp/pf-$svc.log >&2
    fail "port-forward $svc $ports failed to start"
  fi
  echo "$pid"
}

# Cleanup all port-forwards on exit. Each flow registers its pids via
# add_pf_pid <pid>.
PF_PIDS=()
add_pf_pid() { PF_PIDS+=("$1"); }
cleanup_pf() {
  for p in "${PF_PIDS[@]}"; do kill "$p" 2>/dev/null || true; done
}
trap cleanup_pf EXIT
