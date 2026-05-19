#!/usr/bin/env bash
# billing-subscription.sh — billing-svc round-trip smoke.
#
# Real prod flow:
#   1. Customer hits POST /v1/subscriptions/{workspace}/checkout — billing
#      creates a Stripe Checkout Session, returns the URL.
#   2. Customer completes payment, Stripe webhook fires
#      /v1/stripe/webhook → subscription row inserted.
#   3. Subsequent /v1/subscriptions/{workspace} returns the active sub.
#
# Smoke flow (no real Stripe — sk_test_ only):
#   - GET /v1/ (index probe)
#   - POST /v1/subscriptions/<ws>/checkout — should at least respond
#     2xx OR a structured error (not a 5xx panic).
#   - Assert /healthz + /readyz are green.

FLOW_NAME=billing-subscription
. "$(dirname "$0")/_lib.sh"

flow_log "starting billing subscription smoke"

flow_log "port-forwarding billing-svc 18084:8080"
PF=$(port_forward billing-svc 18084:8080)
add_pf_pid "$PF"

# Health probes
for ep in /healthz /readyz /v1/; do
  http_code=$(curl -s -o /tmp/billing-$(basename "$ep" / | tr -d /).txt \
    -w '%{http_code}' "http://127.0.0.1:18084$ep")
  flow_log "$ep -> $http_code"
  [ "$http_code" = "200" ] || fail "$ep returned $http_code"
done

# Subscription read on a workspace that doesn't exist — expect 404.
WS_ID="00000000-0000-0000-0000-0000aaaaaaaa"
flow_log "GET /v1/subscriptions/$WS_ID"
http_code=$(curl -s -o /tmp/sub-get.json -w '%{http_code}' \
  "http://127.0.0.1:18084/v1/subscriptions/$WS_ID")
flow_log "code=$http_code body=$(cat /tmp/sub-get.json | head -c 240)"

case "$http_code" in
  404|200) flow_log "PASS: subscription endpoint returned $http_code" ;;
  5*)      fail "subscription endpoint 5xx: $http_code (panic / unconfigured)" ;;
  *)       flow_log "WARN: unexpected $http_code (acceptable for stub)" ;;
esac

# Checkout-create — billing-svc tries Stripe, will likely fail without a
# real key + price ID. Assert the failure is structured (4xx), not a 5xx
# panic.
flow_log "POST /v1/subscriptions/$WS_ID/checkout"
http_code=$(curl -s -o /tmp/sub-checkout.json -w '%{http_code}' \
  -X POST "http://127.0.0.1:18084/v1/subscriptions/$WS_ID/checkout" \
  -H 'Content-Type: application/json' \
  --data '{"tier":"starter","success_url":"http://localhost/ok","cancel_url":"http://localhost/cancel"}')
flow_log "code=$http_code body=$(cat /tmp/sub-checkout.json | head -c 240)"
case "$http_code" in
  200|201|202) flow_log "PASS: checkout responded $http_code" ;;
  400|401|403|404|422) flow_log "PASS: structured 4xx ($http_code) — expected without real Stripe price ID" ;;
  5*) fail "checkout returned 5xx ($http_code) — likely panic" ;;
  *)  flow_log "WARN: unexpected $http_code" ;;
esac

flow_log "done"
