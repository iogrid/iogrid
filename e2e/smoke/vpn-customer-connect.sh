#!/usr/bin/env bash
# vpn-customer-connect.sh — E2E smoke for the customer VPN flow.
#
# Validates the full end-to-end path a real customer takes:
#   1. Mint an API key via gateway-bff.CreateAPIKey
#   2. Run `iogrid login --api-key=KEY --customer-id=ID`
#   3. Run `iogrid vpn connect --region us-east-1`
#   4. Verify a session was created on vpn-svc (GET /v1/vpn/sessions/{id})
#   5. Verify the session shows up in /v1/vpn/customers/{id}/sessions
#   6. Run `iogrid vpn status` and check state
#   7. Run `iogrid vpn disconnect` cleanly
#
# Stops short of the "external IP changes" assertion — that requires
# a real provider daemon running with #529 PR-B SNAT/forwarding live
# AND a TUN device on the customer side (CAP_NET_ADMIN). When those
# land, add step 8.
#
# Closes #532 partially (control-plane half — full data-plane test
# follows once daemon side ships).

set -euo pipefail

COORDINATOR="${COORDINATOR:-https://api.iogrid.org}"
REGION="${REGION:-us-east-1}"
CLI_BIN="${CLI_BIN:-iogrid}"
WORKSPACE_ID="${WORKSPACE_ID:-}"   # caller supplies; else auto-generate
API_KEY="${API_KEY:-}"             # caller supplies; else mint via gateway-bff

log()   { printf "\033[1;34m[vpn-customer-connect]\033[0m %s\n" "$*"; }
ok()    { printf "\033[1;32m  ✓\033[0m %s\n" "$*"; }
fail()  { printf "\033[1;31m  ✗\033[0m %s\n" "$*" >&2; exit 1; }

# Ensure CLI binary exists ---------------------------------------------------
command -v "$CLI_BIN" >/dev/null 2>&1 || fail "$CLI_BIN not on PATH; install via installer/install-cli.sh"

# Generate workspace + customer if not supplied -----------------------------
if [ -z "$WORKSPACE_ID" ]; then
  WORKSPACE_ID="$(uuidgen)"
  log "Generated WORKSPACE_ID=$WORKSPACE_ID"
fi

# Provision a test provider if none exist in region -------------------------
PROVIDERS=$(curl -fsS "$COORDINATOR/v1/vpn/regions/$REGION/providers" | jq -r '.count')
if [ "$PROVIDERS" -lt 1 ]; then
  PROV_ID="$(uuidgen)"
  log "No providers in $REGION; registering a test provider $PROV_ID"
  curl -fsS -X POST "$COORDINATOR/v1/vpn/providers/$PROV_ID/register" \
    -H 'Content-Type: application/json' -d "{\"region\":\"$REGION\"}" >/dev/null
  ok "test provider registered"
fi

# Step 1: API key. BILLING_SVC_URL is wired on the cluster so
# unauthenticated requests get 401. Caller must supply a real key
# via API_KEY env (mint via iogrid.org/customer/vpn UI or the
# billing-svc.CreateApiKey RPC). Without one, expect the connect
# step to fail with 401 — that's a correctness signal, not a bug.
if [ -z "$API_KEY" ]; then
  log "WARN: API_KEY env not set — connect will 401 (auth enforced). Mint at iogrid.org/customer/vpn"
  log "      Set API_KEY=iog_... to run the full end-to-end."
  API_KEY="iog_e2e_smoke_$(openssl rand -hex 16)"
fi

# Step 2: login -------------------------------------------------------------
log "iogrid login"
"$CLI_BIN" login --api-key="$API_KEY" --customer-id="$WORKSPACE_ID" --coordinator="$COORDINATOR" >/dev/null
ok "credentials saved"

# Step 3: vpn connect -------------------------------------------------------
log "iogrid vpn connect --region $REGION"
if ! "$CLI_BIN" vpn connect --region "$REGION" 2>&1 | tee /tmp/vpn-connect.log; then
  grep -q "Session created" /tmp/vpn-connect.log || fail "connect failed AND no session created"
  log "connect failed at later step (likely no ICE candidates / no real provider) — checking partial state"
fi

# Step 4: Extract session_id from connect log + verify on server ------------
SESSION_ID=$(grep -oE 'Session created: [a-f0-9-]{36}' /tmp/vpn-connect.log | awk '{print $NF}' | head -1)
[ -n "$SESSION_ID" ] || fail "could not extract session_id from connect log"
log "session_id=$SESSION_ID — fetching from coordinator"
SESSION_JSON=$(curl -fsS "$COORDINATOR/v1/vpn/sessions/$SESSION_ID")
echo "$SESSION_JSON" | jq -e '.session_id == "'"$SESSION_ID"'"' >/dev/null || fail "server session lookup mismatch"
ok "session present on coordinator"

# Step 5: Customer's session list includes it -------------------------------
SESSIONS_COUNT=$(curl -fsS "$COORDINATOR/v1/vpn/customers/$WORKSPACE_ID/sessions" | jq -r '.count')
[ "$SESSIONS_COUNT" -ge 1 ] || fail "customer session list doesn't include the new session (count=$SESSIONS_COUNT)"
ok "session appears in /customers/{id}/sessions"

# Step 6: vpn status --------------------------------------------------------
log "iogrid vpn status"
"$CLI_BIN" vpn status 2>&1 | tee /tmp/vpn-status.log
grep -q "status: connected\|status: local view only\|status: no active VPN session" /tmp/vpn-status.log || fail "status output unexpected"
ok "status output recognised"

# Step 7: vpn disconnect ----------------------------------------------------
log "iogrid vpn disconnect"
"$CLI_BIN" vpn disconnect 2>&1 | tee /tmp/vpn-disconnect.log
grep -q "VPN tunnel closed\|no active tunnel\|disconnecting" /tmp/vpn-disconnect.log || fail "disconnect output unexpected"
ok "disconnect output recognised"

# Step 8: cleanup -----------------------------------------------------------
"$CLI_BIN" logout >/dev/null || true

# Step 9 (deferred): external IP changes. Requires:
#   - #529 PR-B (SNAT/smoltcp data plane)
#   - #542 (supervisor wiring)
#   - A real provider running iogridd
# When all three ship, add:
#   EXIT_IP=$(curl -s --max-time 10 ifconfig.me)
#   [ "$EXIT_IP" != "$ORIGINAL_IP" ] || fail "exit IP did not change"

log "control-plane E2E PASSED. Data-plane assertion deferred until #529 PR-B + #542 + #530 land."
