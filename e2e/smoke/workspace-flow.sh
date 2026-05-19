#!/usr/bin/env bash
# workspace-flow.sh — verify identity-svc's Workspace bounded context.
#
# This is the canonical happy path for issue #146:
#   1. Sign in via magic-link (re-uses identity-flow.sh's logic).
#   2. Assert the auto-created "personal workspace" via GET /v1/workspaces.
#   3. Create a second workspace; verify it shows up in List.
#   4. Add a pending invite for an unknown email; verify pending=true.
#   5. Sign in as that invited email; verify the membership is auto-consumed.
#   6. Get the workspace as the invitee → caller_role is the invited role.
#
# Run against a live kind cluster with identity-svc + MailHog reachable.

FLOW_NAME=workspace-flow
. "$(dirname "$0")/_lib.sh"

flow_log "starting workspace flow smoke"

flow_log "port-forwarding identity-svc 18082:8080"
PF=$(port_forward identity-svc 18082:8080)
add_pf_pid "$PF"

# Helper: redeem a magic-link for the supplied email; return the access
# token + user id via globals ACCESS / USER_ID.
sign_in() {
  local email="$1"
  flow_log "requesting magic link for $email"
  http_code=$(curl -s -o /tmp/ml-req.json -w '%{http_code}' \
    -X POST http://127.0.0.1:18082/v1/auth/magic-link/request \
    -H 'Content-Type: application/json' \
    --data "{\"email\":\"$email\"}")
  [ "$http_code" = "200" ] || fail "magic-link/request returned $http_code"

  flow_log "polling MailHog for delivered mail to $email"
  local mail_json=""
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    mail_json=$(curl -fsS "http://127.0.0.1:30180/api/v2/messages?limit=20" 2>/dev/null \
      | jq -c ".items[] | select(.Content.Headers.To[0] | contains(\"$email\"))" 2>/dev/null | head -1 || true)
    [ -n "$mail_json" ] && break
    sleep 1
  done
  [ -n "$mail_json" ] || fail "no magic-link email delivered to MailHog for $email"

  local token
  token=$(echo "$mail_json" | jq -r '.Content.Body // ""' \
    | grep -oE 'token=[a-zA-Z0-9_.\-]+' | head -1 | cut -d= -f2)
  [ -n "$token" ] || fail "could not extract token for $email"

  http_code=$(curl -s -o /tmp/ml-complete.json -w '%{http_code}' \
    -X POST http://127.0.0.1:18082/v1/auth/magic-link/complete \
    -H 'Content-Type: application/json' \
    --data "{\"token\":\"$token\"}")
  [ "$http_code" = "200" ] || fail "magic-link/complete returned $http_code"

  ACCESS=$(jq -r '.access_token // empty' /tmp/ml-complete.json)
  USER_ID=$(jq -r '.user.id // empty' /tmp/ml-complete.json)
  [ -n "$ACCESS" ] || fail "no access_token in bundle for $email"
  [ -n "$USER_ID" ] || fail "no user.id in bundle for $email"
}

OWNER_EMAIL="ws-owner-$(date +%s)@example.com"
INVITEE_EMAIL="ws-invitee-$(date +%s)@example.com"

# --- 1. Sign in as workspace owner ---------------------------------------
flow_log "step 1: sign-in as $OWNER_EMAIL"
sign_in "$OWNER_EMAIL"
OWNER_TOKEN=$ACCESS
OWNER_USER_ID=$USER_ID
flow_log "owner id=$OWNER_USER_ID"

# --- 2. Assert personal workspace ---------------------------------------
flow_log "step 2: list workspaces — expect 1 (auto-created personal)"
http_code=$(curl -s -o /tmp/ws-list.json -w '%{http_code}' \
  -H "Authorization: Bearer $OWNER_TOKEN" \
  http://127.0.0.1:18082/v1/workspaces/)
[ "$http_code" = "200" ] || fail "workspaces/ returned $http_code"
WS_COUNT=$(jq '.workspaces | length' /tmp/ws-list.json)
[ "$WS_COUNT" = "1" ] || fail "expected 1 personal workspace, got $WS_COUNT"
PERSONAL_ID=$(jq -r '.workspaces[0].id' /tmp/ws-list.json)
PERSONAL_ROLE=$(jq -r '.workspaces[0].caller_role' /tmp/ws-list.json)
[ "$PERSONAL_ROLE" = "OWNER" ] || fail "expected OWNER role, got $PERSONAL_ROLE"
flow_log "PASS: personal workspace $PERSONAL_ID (role=$PERSONAL_ROLE)"

# --- 3. Create a second workspace ---------------------------------------
flow_log "step 3: create second workspace"
http_code=$(curl -s -o /tmp/ws-create.json -w '%{http_code}' \
  -X POST -H "Authorization: Bearer $OWNER_TOKEN" -H 'Content-Type: application/json' \
  --data '{"name":"Acme Lab","plan":"STARTER"}' \
  http://127.0.0.1:18082/v1/workspaces/)
[ "$http_code" = "201" ] || fail "create workspace returned $http_code"
LAB_ID=$(jq -r '.id' /tmp/ws-create.json)
flow_log "PASS: created workspace $LAB_ID"

# Verify list now returns 2.
curl -s -H "Authorization: Bearer $OWNER_TOKEN" \
  http://127.0.0.1:18082/v1/workspaces/ > /tmp/ws-list2.json
WS_COUNT=$(jq '.workspaces | length' /tmp/ws-list2.json)
[ "$WS_COUNT" = "2" ] || fail "expected 2 workspaces after create, got $WS_COUNT"

# --- 4. Invite an unknown email → pending=true --------------------------
flow_log "step 4: invite $INVITEE_EMAIL (unknown user) to $LAB_ID"
http_code=$(curl -s -o /tmp/ws-invite.json -w '%{http_code}' \
  -X POST -H "Authorization: Bearer $OWNER_TOKEN" -H 'Content-Type: application/json' \
  --data "{\"user_email\":\"$INVITEE_EMAIL\",\"role\":\"ADMIN\"}" \
  "http://127.0.0.1:18082/v1/workspaces/$LAB_ID/members")
[ "$http_code" = "201" ] || fail "invite returned $http_code"
PENDING=$(jq -r '.pending' /tmp/ws-invite.json)
[ "$PENDING" = "true" ] || fail "expected pending=true for unknown email, got $PENDING"
flow_log "PASS: invite recorded as pending"

# --- 5. Invitee signs in → workspace_members auto-populated -------------
flow_log "step 5: invitee signs in"
sign_in "$INVITEE_EMAIL"
INVITEE_TOKEN=$ACCESS
INVITEE_USER_ID=$USER_ID

# Invitee should now see TWO workspaces: their personal + the invited one.
curl -s -H "Authorization: Bearer $INVITEE_TOKEN" \
  http://127.0.0.1:18082/v1/workspaces/ > /tmp/ws-list-inv.json
WS_COUNT=$(jq '.workspaces | length' /tmp/ws-list-inv.json)
[ "$WS_COUNT" = "2" ] || fail "invitee expected 2 workspaces (personal + invited), got $WS_COUNT"
flow_log "PASS: invite auto-consumed; invitee sees $WS_COUNT workspaces"

# --- 6. Invitee can Get the lab workspace; role = ADMIN -----------------
flow_log "step 6: invitee fetches $LAB_ID"
http_code=$(curl -s -o /tmp/ws-get.json -w '%{http_code}' \
  -H "Authorization: Bearer $INVITEE_TOKEN" \
  "http://127.0.0.1:18082/v1/workspaces/$LAB_ID")
[ "$http_code" = "200" ] || fail "invitee get $LAB_ID returned $http_code"
INVITEE_ROLE=$(jq -r '.caller_role' /tmp/ws-get.json)
[ "$INVITEE_ROLE" = "ADMIN" ] || fail "expected ADMIN, got $INVITEE_ROLE"
flow_log "PASS: invitee role=$INVITEE_ROLE in lab workspace"

# --- 7. ListMembers shows both users ------------------------------------
flow_log "step 7: list members"
curl -s -H "Authorization: Bearer $OWNER_TOKEN" \
  "http://127.0.0.1:18082/v1/workspaces/$LAB_ID/members" > /tmp/ws-members.json
MEMBER_COUNT=$(jq '.members | length' /tmp/ws-members.json)
[ "$MEMBER_COUNT" = "2" ] || fail "expected 2 members, got $MEMBER_COUNT"
flow_log "PASS: members count=$MEMBER_COUNT"

# --- 8. Non-owner cannot delete ----------------------------------------
flow_log "step 8: invitee tries to delete (should be denied)"
http_code=$(curl -s -o /dev/null -w '%{http_code}' \
  -X DELETE -H "Authorization: Bearer $INVITEE_TOKEN" \
  "http://127.0.0.1:18082/v1/workspaces/$LAB_ID")
[ "$http_code" = "403" ] || fail "expected 403 for non-owner delete, got $http_code"
flow_log "PASS: non-owner delete returned 403"

flow_log "done"
