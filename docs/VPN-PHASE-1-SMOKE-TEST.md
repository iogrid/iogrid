# VPN Phase 1 On-Cluster Smoke Test

> Refs: [EPIC #504](https://github.com/iogrid/iogrid/issues/504), [VPN-18 #523](https://github.com/iogrid/iogrid/issues/523)

This runbook validates the Phase 1 P2P VPN end-to-end on the live mothership cluster. Run after `vpn-svc` is `Running` (check `kubectl -n iogrid get pod -l app.kubernetes.io/name=vpn-svc`).

## Prerequisites

- Kubeconfig pointed at the iogrid mothership
- `curl`, `jq`, `kubectl`
- A paired provider daemon (any `iogridd` running on a real machine + paired with this Coordinator)
- Public DNS or LB endpoint for `vpn-svc-stun` (UDP :3478)

## 1. Health checks (10 seconds)

```bash
kubectl -n iogrid port-forward svc/vpn-svc 8080:8080 &
PF_PID=$!
sleep 2

# HTTP health
curl -fsS http://localhost:8080/healthz | jq .
# expect: {"status":"ok"}

# Service routes mounted
curl -fsS -X GET http://localhost:8080/v1/vpn/regions/us-east-1/providers | jq .
# expect: {"region":"us-east-1","providers":null,"count":0}  (no providers yet)

kill $PF_PID
```

## 2. STUN binding (UDP, public)

The STUN server is exposed via the `vpn-svc-stun` LoadBalancer Service on UDP :3478. From any machine with public internet:

```bash
# Discover the LoadBalancer external IP
STUN_IP=$(kubectl -n iogrid get svc vpn-svc-stun -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "STUN at ${STUN_IP}:3478"

# Use stunclient (apt install stuntman-client) or any STUN tool
stunclient ${STUN_IP} 3478
# expect: "Mapped address" matching your external IP
```

If `stunclient` is not available, the `tools/proxy/get-ip` Go binary can be patched to do a STUN BINDING REQUEST roundtrip against `${STUN_IP}:3478` — the format is in `coordinator/services/vpn-svc/internal/ice/stun_test.go`.

## 3. Provider lifecycle

This simulates a paired daemon's first contact. In practice, the daemon does this automatically once it's running.

```bash
# Generate a deterministic provider ID for the test
PROVIDER_ID=$(uuidgen)

# 3a. Register
curl -fsS -X POST http://localhost:8080/v1/vpn/providers/${PROVIDER_ID}/register \
  -H 'Content-Type: application/json' \
  -d '{"region":"us-east-1"}' | jq .
# expect: {"status":"registered","provider_id":"<uuid>","region":"us-east-1"}

# 3b. List providers in region (should now show 1)
curl -fsS http://localhost:8080/v1/vpn/regions/us-east-1/providers | jq .
# expect: count=1, providers[0].status="healthy"

# 3c. Heartbeat
curl -fsS -X POST http://localhost:8080/v1/vpn/providers/${PROVIDER_ID}/health \
  -H 'Content-Type: application/json' \
  -d "{\"status\":\"healthy\",\"at_unix_ms\":$(date +%s%3N)}" | jq .
# expect: {"status":"healthy"}

# 3d. Register ICE candidates (skeleton — production format from proto)
curl -fsS -X POST http://localhost:8080/v1/vpn/providers/${PROVIDER_ID}/candidates \
  -H 'Content-Type: application/json' \
  -d '{"candidates":[{"foundation":"1","connection_address":"203.0.113.5","connection_port":51820,"candidate_type":"srflx","priority":100}]}' | jq .
# expect: {"candidate_count":1}

# 3e. Retrieve candidates
curl -fsS http://localhost:8080/v1/vpn/providers/${PROVIDER_ID}/candidates | jq .
# expect: count=1
```

## 4. Session lifecycle

```bash
CUSTOMER_ID=$(uuidgen)

# 4a. Create session
SESSION_ID=$(curl -fsS -X POST http://localhost:8080/v1/vpn/sessions \
  -H 'Content-Type: application/json' \
  -d "{\"customer_id\":\"${CUSTOMER_ID}\",\"region\":\"us-east-1\",\"api_key_hash\":\"test\"}" \
  | jq -r .session_id)
echo "Session: ${SESSION_ID}"

# 4b. Get session
curl -fsS http://localhost:8080/v1/vpn/sessions/${SESSION_ID} | jq .

# 4c. Heartbeat
curl -fsS -X POST http://localhost:8080/v1/vpn/sessions/${SESSION_ID}/refresh \
  -H 'Content-Type: application/json' \
  -d '{"bytes_in":1024,"bytes_out":2048,"roaming_events":0,"failover_count":0}' | jq .

# 4d. Trigger failover (should 503 — only 1 provider in region)
curl -sS -X POST http://localhost:8080/v1/vpn/sessions/${SESSION_ID}/failover \
  -H 'Content-Type: application/json' \
  -d '{"failure_reason":"endpoint_unreachable"}' -w 'HTTP %{http_code}\n'
# expect: HTTP 503 (no alternate provider)

# 4e. Terminate
curl -fsS -X POST http://localhost:8080/v1/vpn/sessions/${SESSION_ID}/terminate \
  -H 'Content-Type: application/json' \
  -d '{"reason":"smoke_test_complete"}' | jq .
```

## 5. Multi-provider failover

```bash
# Register a second provider
PROVIDER_2=$(uuidgen)
curl -fsS -X POST http://localhost:8080/v1/vpn/providers/${PROVIDER_2}/register \
  -H 'Content-Type: application/json' \
  -d '{"region":"us-east-1"}'

# Create new session
SESSION_2=$(curl -fsS -X POST http://localhost:8080/v1/vpn/sessions \
  -H 'Content-Type: application/json' \
  -d "{\"customer_id\":\"${CUSTOMER_ID}\",\"region\":\"us-east-1\",\"api_key_hash\":\"test\"}" \
  | jq -r .session_id)

# Manually set CurrentProvider via store would normally happen on
# session establishment — for now, the failover handler will pick any
# of the 2 providers in us-east-1 if we trigger it (it just won't
# correctly exclude the "current" one since there's no current).

# Trigger failover — should now succeed
curl -fsS -X POST http://localhost:8080/v1/vpn/sessions/${SESSION_2}/failover \
  -H 'Content-Type: application/json' \
  -d '{"failure_reason":"test","failed_provider_id":"'${PROVIDER_ID}'"}' | jq .
# expect: status="failover_complete", new_provider_id != failed_provider_id
```

## 6. Metrics

```bash
curl -fsS http://localhost:8080/metrics | grep iogrid_vpn_svc | head -20
# expect counters:
#   iogrid_vpn_svc_sessions_created_total
#   iogrid_vpn_svc_session_refreshes_total
#   iogrid_vpn_svc_failovers_triggered_total{region="us-east-1",outcome="success"}
#   iogrid_vpn_svc_failovers_triggered_total{region="us-east-1",outcome="no_alternate"}
```

## 7. Cleanup

```bash
# Optional: delete test providers via direct DB or restart vpn-svc with empty store
kubectl -n iogrid rollout restart deployment/vpn-svc
```

## Pass criteria

- [ ] Step 1: `/healthz` returns 200; routes mount
- [ ] Step 2: STUN returns mapped address matching client external IP
- [ ] Step 3: provider register → list → heartbeat → candidates round-trip works
- [ ] Step 4: session create → get → refresh → terminate; failover 503 with 1 provider
- [ ] Step 5: failover succeeds with 2 providers, new ≠ failed
- [ ] Step 6: Prometheus counters non-zero, correctly labeled

After all 6 pass, comment evidence on issue #523 and close EPIC #504.

## Known limitations (deferred to Phase 2-4)

- **No customer SDK integration on the bastion yet** — this smoke validates only the Coordinator side. The full bastion → provider WireGuard tunnel requires a paired daemon running with VPN-6 (ICE discovery) + VPN-7 (health) actually firing against this `vpn-svc`. That's the next milestone after this smoke passes.
- **External IP check** (the headline DoD: "external IP returns provider's IP") needs a real WireGuard tunnel + customer SDK Connect() call against this `vpn-svc`. Track separately once Phase 2 customer integration ships.
- **DERP fallback** for symmetric NAT — issue #521, Phase 4.
- **Stress testing** at 100+ concurrent sessions — issue #522, Phase 4.
