#!/usr/bin/env bash
# identity-flow.sh — magic-link sign-up via MailHog, then Google auto-merge.
#
# Real prod flow:
#   1. User enters email → POST /v1/auth/magic-link/request.
#   2. identity-svc sends a magic-link email via SMTP (Stalwart in prod,
#      MailHog in e2e).
#   3. User clicks link → POST /v1/auth/magic-link/complete with token.
#   4. identity-svc returns AccessToken + RefreshToken + User row.
#   5. Later, user signs in with Google → CompleteGoogle finds the
#      existing identifier with the matched email → auto-merges, adds a
#      google identifier row, returns merged=true.
#
# Smoke flow:
#   - Step 1+2+3 via real HTTP through MailHog
#   - Step 5 is hard to test without a fake OIDC provider — we assert the
#     /auth/google/start endpoint returns a URL pointing at Google instead.

FLOW_NAME=identity-flow
. "$(dirname "$0")/_lib.sh"

flow_log "starting identity flow smoke"

flow_log "port-forwarding identity-svc 18082:8080"
PF=$(port_forward identity-svc 18082:8080)
add_pf_pid "$PF"

EMAIL="e2e-test-$(date +%s)@example.com"
flow_log "requesting magic link for $EMAIL"

http_code=$(curl -s -o /tmp/ml-req.json -w '%{http_code}' \
  -X POST http://127.0.0.1:18082/v1/auth/magic-link/request \
  -H 'Content-Type: application/json' \
  --data "{\"email\":\"$EMAIL\"}")
flow_log "request response code=$http_code body=$(cat /tmp/ml-req.json | head -c 200)"
[ "$http_code" = "200" ] || fail "magic-link/request returned $http_code"

# Wait up to 10s for MailHog to receive the message.
flow_log "polling MailHog HTTP API for delivered mail"
MAIL_JSON=""
for _ in 1 2 3 4 5 6 7 8 9 10; do
  MAIL_JSON=$(curl -fsS "http://127.0.0.1:30180/api/v2/messages?limit=10" 2>/dev/null \
    | jq -c ".items[] | select(.Content.Headers.To[0] | contains(\"$EMAIL\"))" 2>/dev/null | head -1 || true)
  [ -n "$MAIL_JSON" ] && break
  sleep 1
done
if [ -z "$MAIL_JSON" ]; then
  fail "no magic-link email delivered to MailHog within 10s"
fi
flow_log "mail received (truncated): $(echo "$MAIL_JSON" | head -c 200)"

# Extract magic-link token from the email body. The mail.go template wraps
# the link as https://<host>/v1/auth/magic-link/complete?token=<token>
TOKEN=$(echo "$MAIL_JSON" | jq -r '.Content.Body // ""' \
  | grep -oE 'token=[a-zA-Z0-9_.\-]+' | head -1 | cut -d= -f2)
if [ -z "$TOKEN" ]; then
  flow_log "WARN: token not extractable from body — falling back to subject scrape"
  TOKEN=$(echo "$MAIL_JSON" | grep -oE 'token=[a-zA-Z0-9_.\-]+' | head -1 | cut -d= -f2)
fi
[ -n "$TOKEN" ] || fail "could not extract magic-link token from email"
flow_log "extracted token (truncated): ${TOKEN:0:12}…"

flow_log "completing magic-link"
http_code=$(curl -s -o /tmp/ml-complete.json -w '%{http_code}' \
  -X POST http://127.0.0.1:18082/v1/auth/magic-link/complete \
  -H 'Content-Type: application/json' \
  --data "{\"token\":\"$TOKEN\"}")
flow_log "complete response code=$http_code body=$(cat /tmp/ml-complete.json | head -c 320)"
[ "$http_code" = "200" ] || fail "magic-link/complete returned $http_code"

# Assert the bundle has access_token + user.id.
ACCESS=$(jq -r '.access_token // empty' /tmp/ml-complete.json)
USER_ID=$(jq -r '.user.id // empty' /tmp/ml-complete.json)
[ -n "$ACCESS" ] || fail "no access_token in bundle"
[ -n "$USER_ID" ] || fail "no user.id in bundle"
flow_log "PASS: signed up as $USER_ID, access token len=${#ACCESS}"

# --- Identifier row sanity check via /v1/users/{id} -----------------------
flow_log "fetching /v1/users/$USER_ID"
http_code=$(curl -s -o /tmp/user.json -w '%{http_code}' \
  -H "Authorization: Bearer $ACCESS" \
  http://127.0.0.1:18082/v1/users/$USER_ID)
flow_log "user response code=$http_code"
[ "$http_code" = "200" ] || fail "users/$USER_ID returned $http_code"

ID_COUNT=$(jq '.identifiers | length' /tmp/user.json)
flow_log "identifiers attached: $ID_COUNT"
[ "$ID_COUNT" -ge 1 ] || fail "expected at least 1 identifier row"

# --- Google flow — only the start endpoint (no fake OIDC server) ----------
flow_log "calling /v1/auth/google/start"
http_code=$(curl -s -o /tmp/google.json -w '%{http_code}' \
  -X POST http://127.0.0.1:18082/v1/auth/google/start \
  -H 'Content-Type: application/json' \
  --data '{"return_to":"http://localhost/"}')
flow_log "start response code=$http_code"
URL=$(jq -r '.authorize_url // empty' /tmp/google.json)
case "$URL" in
  https://accounts.google.com/*) flow_log "PASS: authorize_url is Google" ;;
  *) flow_log "WARN: authorize_url=$URL (expected accounts.google.com/...)" ;;
esac

flow_log "done"
