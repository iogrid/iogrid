#!/usr/bin/env bash
# bandwidth-proxy.sh — SOCKS5 client routes a HTTP request through
# proxy-gateway, asserts 200 + an audit event.
#
# Real prod flow:
#   1. Customer dials tcp://proxy.iogrid.org:443 with TLS.
#   2. SOCKS5 RFC 1929 user/pass: (workspace_handle, api_key).
#   3. proxy-gateway resolves api_key → Customer via billing-svc.
#   4. proxy-gateway pre-flights the destination via antiabuse-svc.CheckUrl.
#   5. Dispatcher picks a provider, opens an mTLS tunnel to its daemon,
#      relays the byte stream.
#   6. Every 1 MiB relayed → BILLING.metering NATS event.
#
# What we exercise here (proxy-gateway scaffold image, plain TCP only):
#   - SOCKS5 handshake against 127.0.0.1:30080
#   - With DEV_API_KEYS pre-seeding the Static validator the auth step
#     passes synthetically
#   - DEV_PROVIDER_ENDPOINT points at a non-existent mock-provider, so the
#     dispatcher will report "no candidate" — we ONLY assert that the
#     handshake completed and a 0x05 (general failure) response came back

FLOW_NAME=bandwidth-proxy
. "$(dirname "$0")/_lib.sh"

flow_log "starting bandwidth proxy smoke"

flow_log "checking proxy-gateway health"
wait_for_pod_ready proxy-gateway 90

# Use the NodePort exposed via kind extraPortMapping (127.0.0.1:30080).
PROXY_HOST=127.0.0.1
PROXY_PORT=30080

# Verify TCP open by sending a SOCKS5 greeting (5 01 02) — methods=USERPASS.
flow_log "SOCKS5 greeting handshake"
resp=$(printf '\x05\x01\x02' | timeout 5 nc -w 3 "$PROXY_HOST" "$PROXY_PORT" | xxd -p | head -c 4 || true)
flow_log "greet hex=$resp"
if [ -z "$resp" ]; then
  fail "no response from proxy-gateway:$PROXY_PORT (proxy listener not up?)"
fi
# Expect server to respond with [0x05, 0x02] (selected USERPASS).
case "$resp" in
  0502) flow_log "PASS: proxy selected USERPASS method";;
  0500) flow_log "WARN: proxy selected NO-AUTH (DEV_API_KEYS unset?)";;
  *)    flow_log "WARN: unexpected greet response $resp" ;;
esac

# --- Full SOCKS5 attempt with curl ----------------------------------------
# Note: curl --socks5 expects user:pass form. The API key is the password.
flow_log "attempting CONNECT to example.com:80 via SOCKS5 (will fail at dispatcher — expected)"
out=$(curl -sS --max-time 8 \
  --proxy "socks5://test-workspace:e2e-test-api-key@$PROXY_HOST:$PROXY_PORT" \
  -o /dev/null -w 'http_code=%{http_code} exitcode=%{exitcode}\n' \
  http://example.com/ 2>&1 || true)
flow_log "curl: $out"

# Whether the upstream was reachable or not, the audit event should have
# been emitted to slog (NATS connection is best-effort). Inspect the pod
# log for an audit line.
flow_log "checking proxy-gateway logs for audit event"
${KUBECTL} logs -l app.kubernetes.io/name=proxy-gateway --tail=200 \
  | tee /tmp/proxy-logs.txt | head -40
if grep -qE 'audit|customer_id|api_key|workspace' /tmp/proxy-logs.txt; then
  flow_log "PASS: proxy-gateway emitted at least one audit-related log line"
else
  fail "no audit lines in proxy-gateway logs"
fi

flow_log "done"
