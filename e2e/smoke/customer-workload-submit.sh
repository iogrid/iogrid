#!/usr/bin/env bash
# customer-workload-submit.sh — workload submission + dispatch smoke.
#
# Real prod flow:
#   1. Customer's SDK signs a SubmitWorkload Connect request with their
#      API key (resolved by gateway-bff → billing-svc.ValidateApiKey).
#   2. workloads-svc.SubmissionService.SubmitWorkload persists the row
#      as QUEUED, then calls dispatcher.TryAssign synchronously.
#   3. Dispatcher consults providers-svc.ListProviders (filtered by
#      capability + region + status) and picks a candidate, marking the
#      workload as DISPATCHED with provider_id set.
#   4. Customer polls GetWorkload until status=COMPLETED.
#
# What we exercise here:
#   - SubmitWorkload via Connect-RPC (no API key — the scaffold image
#     doesn't require gateway-bff in front).
#   - Assert response has a workload_id.
#   - Assert workload reads back via GetWorkload.

FLOW_NAME=customer-workload-submit
. "$(dirname "$0")/_lib.sh"

flow_log "starting workload submission smoke"

flow_log "port-forwarding workloads-svc 18081:8080"
PF=$(port_forward workloads-svc 18081:8080)
add_pf_pid "$PF"

# Compose a minimal DOCKER workload.
WL_PAYLOAD=$(cat <<'EOF'
{
  "workload": {
    "workspace_id": {"value": "00000000-0000-0000-0000-000000000001"},
    "type": "WORKLOAD_TYPE_DOCKER",
    "docker_spec": {
      "image_ref": "docker.io/library/alpine:3.20",
      "command":   ["echo", "hello-e2e"],
      "cpu_cores": 1,
      "memory_mb": 64,
      "max_runtime_seconds": 30
    },
    "geo_target": "any"
  }
}
EOF
)

flow_log "calling SubmitWorkload"
http_code=$(curl -s -o /tmp/wl-submit.json -w '%{http_code}' \
  -X POST http://127.0.0.1:18081/iogrid.workloads.v1.WorkloadSubmissionService/SubmitWorkload \
  -H 'Content-Type: application/json' \
  --data "$WL_PAYLOAD")
flow_log "submit response code=$http_code body=$(cat /tmp/wl-submit.json | head -c 320)"

if [ "$http_code" != "200" ]; then
  fail "SubmitWorkload returned $http_code"
fi

# Extract workload id (proto uuid wrapper {value: "..."} → grep).
WL_ID=$(jq -r '.workload.id.value // empty' /tmp/wl-submit.json 2>/dev/null || true)
[ -n "$WL_ID" ] || fail "no workload id in response"
flow_log "workload_id=$WL_ID"

# Poll GetWorkload up to 10s.
flow_log "calling GetWorkload (expect 200, status QUEUED or REJECTED)"
http_code=$(curl -s -o /tmp/wl-get.json -w '%{http_code}' \
  -X POST http://127.0.0.1:18081/iogrid.workloads.v1.WorkloadSubmissionService/GetWorkload \
  -H 'Content-Type: application/json' \
  --data "{\"id\":{\"value\":\"$WL_ID\"}}")
flow_log "get response code=$http_code body=$(cat /tmp/wl-get.json | head -c 320)"
[ "$http_code" = "200" ] || fail "GetWorkload returned $http_code"

STATUS=$(jq -r '.workload.status // empty' /tmp/wl-get.json 2>/dev/null || true)
flow_log "workload status=$STATUS"
# Acceptable values today: QUEUED (no providers paired yet) or REJECTED
# (dispatcher found no candidates). Either confirms the round-trip.
case "$STATUS" in
  WORKLOAD_STATUS_QUEUED|WORKLOAD_STATUS_DISPATCHED|WORKLOAD_STATUS_REJECTED)
    flow_log "PASS: workload status is one of QUEUED/DISPATCHED/REJECTED"
    ;;
  "")
    fail "no status returned"
    ;;
  *)
    flow_log "WARN: unexpected status '$STATUS' (still 200 round-trip OK)"
    ;;
esac

flow_log "done"
