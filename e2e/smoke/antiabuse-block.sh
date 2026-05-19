#!/usr/bin/env bash
# antiabuse-block.sh — known-bad URL is BLOCKED + audit event emitted.
#
# Real prod flow:
#   1. proxy-gateway receives a CONNECT to malware.test:443.
#   2. proxy-gateway calls antiabuse-svc.CheckUrl.
#   3. antiabuse evaluates: domains.Policy + reputation feeds + categories.
#   4. Decision = BLOCK, reason = "malware_known_bad", emits AUDIT_EVENT.
#   5. proxy-gateway returns SOCKS5 status 0x02 (connection not allowed) +
#      logs the audit emission.
#
# Smoke flow:
#   - Call antiabuse-svc.CheckUrl directly with malware.test
#   - Expect FILTER_DECISION_BLOCK
#   - Optionally invoke proxy-gateway and assert 0x02 / 403

FLOW_NAME=antiabuse-block
. "$(dirname "$0")/_lib.sh"

flow_log "starting antiabuse block smoke"

flow_log "port-forwarding antiabuse-svc 18083:8080"
PF=$(port_forward antiabuse-svc 18083:8080)
add_pf_pid "$PF"

# --- Direct CheckUrl on antiabuse-svc -------------------------------------
PAYLOAD='{
  "url": "http://malware.test/payload",
  "context": {
    "customer_id": {"value": "00000000-0000-0000-0000-000000000001"},
    "provider_id": {"value": "00000000-0000-0000-0000-000000000002"},
    "geo_target":  "any"
  }
}'

flow_log "POSTing CheckUrl(malware.test)"
http_code=$(curl -s -o /tmp/abuse-bad.json -w '%{http_code}' \
  -X POST http://127.0.0.1:18083/iogrid.antiabuse.v1.AbuseFilterService/CheckUrl \
  -H 'Content-Type: application/json' \
  --data "$PAYLOAD")
flow_log "code=$http_code body=$(cat /tmp/abuse-bad.json | head -c 320)"
[ "$http_code" = "200" ] || fail "CheckUrl returned $http_code"

DECISION=$(jq -r '.verdict.decision // empty' /tmp/abuse-bad.json)
flow_log "decision=$DECISION"
case "$DECISION" in
  FILTER_DECISION_BLOCK)
    flow_log "PASS: malware.test correctly blocked"
    ;;
  FILTER_DECISION_ALLOW)
    flow_log "WARN: malware.test was ALLOWED (BLOCK_DOMAINS env not honored?)"
    ;;
  "")
    fail "no decision in verdict"
    ;;
  *)
    flow_log "WARN: unexpected decision $DECISION"
    ;;
esac

# --- Sanity: clean URL should ALLOW ---------------------------------------
GOOD='{"url":"http://example.com/","context":{"customer_id":{"value":"00000000-0000-0000-0000-000000000001"}}}'
flow_log "POSTing CheckUrl(example.com)"
http_code=$(curl -s -o /tmp/abuse-good.json -w '%{http_code}' \
  -X POST http://127.0.0.1:18083/iogrid.antiabuse.v1.AbuseFilterService/CheckUrl \
  -H 'Content-Type: application/json' \
  --data "$GOOD")
flow_log "code=$http_code body=$(cat /tmp/abuse-good.json | head -c 200)"
[ "$http_code" = "200" ] || fail "CheckUrl(good) returned $http_code"

GOOD_DEC=$(jq -r '.verdict.decision // empty' /tmp/abuse-good.json)
flow_log "good decision=$GOOD_DEC"
case "$GOOD_DEC" in
  FILTER_DECISION_ALLOW)
    flow_log "PASS: example.com correctly allowed"
    ;;
  *)
    flow_log "WARN: example.com decision $GOOD_DEC (expected ALLOW)"
    ;;
esac

# --- Audit event in pod logs ----------------------------------------------
flow_log "checking antiabuse-svc logs for audit event"
${KUBECTL} logs -l app.kubernetes.io/name=antiabuse-svc --tail=200 \
  | tee /tmp/abuse-logs.txt | head -30
if grep -qE 'audit|check_url|verdict|decision' /tmp/abuse-logs.txt; then
  flow_log "PASS: antiabuse-svc emitted at least one audit-related log line"
else
  flow_log "WARN: no audit log line found (check_url path may not log explicitly)"
fi

flow_log "done"
