# VPN Definition of Done — COMPREHENSIVE & SPECIFIC

**Status**: Design Phase  
**Target**: 2026-06-08 Phase 1 Complete  
**Comprehensive Spec**: This DoD is implementation-ready. No questions should remain after reading.

---

## COMPREHENSIVE FUNCTIONAL DEFINITION OF DONE

### Authentication & Credential Management

#### Customer Authentication Flow
```
1. Customer (bastion) obtains API key from Coordinator
   - Endpoint: POST /api/v1/vpn/request-credentials
   - Input: customer_id, workspace_id
   - Output: {api_key, api_key_hash, ttl_seconds, created_at}
   - API key format: "iog_<32-byte-hex>" (48 chars total)
   - Key stored in: ~/.iogrid/vpn/credentials (mode 0600)
   - Validity: 90 days from creation (non-rotatable, issue new on expiry)

2. Customer authenticates to Coordinator on every session request
   - Method: mTLS + API key in Authorization header
   - Header: "Authorization: Bearer <api_key>"
   - Coordinator validates: api_key exists, not expired, hash matches database
   - On failure: return 401 Unauthorized, session denied
   - On success: session ledger created with customer_id + api_key_hash

3. During session lifetime (30min sticky, up to 24hr max)
   - Customer periodically refreshes session (every 15min)
   - Refresh: POST /api/v1/vpn/sessions/{session_id}/refresh
   - Coordinator verifies: api_key still valid, customer not revoked
   - On failure: session terminated immediately, customer must re-authenticate
   - Proof: session logs include auth_timestamp, refresh_timestamps

#### Credential Rotation Policy
```
Rotation happens automatically (customer doesn't manage):

Pre-expiry notification (7 days before 90-day expiry):
  - Coordinator sends: NotifyKeyExpiring {session_id, days_remaining}
  - Customer logs warning: "API key expires in 7 days"
  - Customer behavior: request new credentials via web UI

New key issuance:
  - Endpoint: POST /api/v1/vpn/rotate-credentials
  - Old key remains valid for 24hr after new key issued
  - New key immediately active for new sessions
  - Existing sessions continue with old key (no interruption)
  - After 24hr: old key invalidated, existing sessions terminated
  - Force-rotation possible (security incident): POST /admin/vpn/revoke-credentials

On-the-wire rotation during session:
  - Coordinator includes {new_api_key, rotate_at_timestamp} in refresh response
  - Customer: store new key, use old key until rotate_at_timestamp
  - At rotate_at_timestamp: switch to new key immediately
  - Test: rotate key mid-transfer, verify no packet loss

Proof: Metrics tracked per session
  - auth_method: "bearer", "mTLS"
  - key_age_days
  - refresh_count
  - key_rotation_count
  - session_continuation_after_rotation: yes/no
```

#### Provider Authentication
```
Provider proves ownership of WireGuard public key:

1. Provider registration:
   - Daemon generates: private_key (persisted) → public_key
   - Daemon sends to Coordinator: {provider_id, public_key, csr_pubkey}
   - Coordinator stores: providers.wg_public_key = public_key
   - Coordinator verifies: SPKI fingerprint matches previous registration
   - On mismatch: create new provider (unless SPKI dedupe PR #503 override)

2. During customer-provider handshake:
   - Coordinator sends to customer: {provider_id, provider_wg_public_key}
   - Customer adds provider as WireGuard peer with pubkey
   - Customer pings provider on WireGuard iface (10.64.0.1)
   - Provider responds if: peer pubkey matches its own private key
   - Proof: ping succeeds = provider has matching private key
   - No additional authentication needed (WireGuard key = proof of ownership)

3. Forward secrecy:
   - Provider never sends private key (only public key to Coordinator)
   - Coordinator never possesses provider private key
   - Customer never possesses provider private key
   - Proof: zero private keys in logs, zero private keys in protocol messages
```

### Session Management

#### Session Lifecycle
```
1. CREATE Session (Duration: 24hr max, Sticky: 30min per provider)
   - Endpoint: POST /api/v1/vpn/sessions
   - Request: {customer_id, api_key, region, workload_type: "vpn"}
   - Response: {
       session_id: UUID,
       provider_id: UUID,
       provider_wg_public_key: base64,
       candidates: [{foundation, type, ip, port}, ...],
       derp_relay_fallback: "IP:port",
       ttl_seconds: 86400,
       created_at: RFC3339,
       expires_at: RFC3339
     }
   - Stored in: Coordinator.vpn_sessions table
   - Fields tracked: session_id, customer_id, provider_id, api_key_hash, state, created_at, expires_at

2. REFRESH Session (triggered every 15min by customer, or on roaming/failover)
   - Endpoint: POST /api/v1/vpn/sessions/{session_id}/refresh
   - Request: {session_id, api_key, current_ice_candidate}
   - Response: {
       session_id: session_id,
       status: "ok|provider_dead|api_key_expired|session_expired",
       provider_id: provider_id (same or different if failover),
       candidates: [...] (updated list if provider changed),
       new_api_key?: (if rotation needed),
       rotate_at?: RFC3339
     }
   - Stored: Update vpn_sessions.last_refreshed_at, refresh_count++
   - On provider dead: candidates = [] (signal customer to failover)
   - On api_key_expired: status = "api_key_expired", force re-auth

3. HEARTBEAT from Provider (every 10sec, timeout 30sec)
   - Daemon sends: {session_id, provider_id, status: "online", metrics: {cpu, mem, load}}
   - Coordinator: Update vpn_sessions.provider_last_seen_at, health_status
   - Customer pings provider: ICMP echo on wg iface
   - Provider responds if session still valid
   - Proof: ping/pong exchanges logged with timestamps

4. TERMINATE Session (customer-initiated or timeout)
   - Endpoint: DELETE /api/v1/vpn/sessions/{session_id}
   - Actions: 
     - Remove provider peer from WireGuard interface
     - Coordinator: mark session terminated, store end_time
     - Metrics: bytes_in, bytes_out, duration_sec, exit_reason
   - Graceful: customer closes resources first (5s timeout before force-kill)
   - Logs: SessionTerminated event with all metrics

Session state machine:
  CREATING (0-5s) → ESTABLISHING (5-20s) → ACTIVE (>20s)
            ↓                    ↓              ↓
         [error]             [error]       [normal operation]
            ↓                    ↓              ↓
          FAILED             FAILED        ROAMING/REFRESHING
                                             ↓
                                          ACTIVE
                                             ↓
                                         TERMINATED

Proof: State transitions logged with timestamps, no invalid transitions allowed
```

### STUN/TURN Usage & Fallback

#### STUN Candidate Gathering (Provider Side)

```
1. Provider startup: Run ICE candidate gatherer every 30s

   Task: stun_candidate_gatherer {
     // Get local IPs
     local_ips = get_local_ips() // [192.168.1.100, 10.64.0.1, ...]
     
     // Get STUN server from Coordinator
     stun_server = env("STUN_SERVER_URL") // "stun.iogrid.org:3478"
     
     for each local_ip:
       // Send STUN BINDING REQUEST
       request = stun_binding_request()
       response = send(stun_server, request, timeout=2s)
       
       if response.success:
         candidate = {
           foundation: hash(local_ip + stun_server),
           type: "srflx" (server reflexive),
           connection_address: response.external_ip,
           connection_port: response.external_port,
           priority: calculate_priority(type=srflx),
           related_address: local_ip,
           related_port: listen_port,
           rtt_ms: response_time
         }
         candidates.append(candidate)
         
       if response.timeout:
         candidate = {
           type: "host",
           connection_address: local_ip,
           connection_port: listen_port,
           ...
         }
         candidates.append(candidate) // fallback to host if STUN fails
   }

2. Report to Coordinator (every heartbeat, max 3 candidates)
   - Endpoint: POST /api/v1/providers/heartbeat
   - Payload includes: candidates[], timestamp
   - Sort by: priority (srflx > host), then by RTT
   - Include: top 3 candidates (best priority + lowest latency)
   - Proof: candidate update logged with old vs new values

3. Coordinator validation
   - STUN candidates must have: foundation, type, address, port
   - Provider can only update own candidates
   - Coordinator deduplicates: if same (ip,port) reported twice, keep first
   - Proof: dedupe logged with count of duplicates removed
```

#### STUN Connectivity Check (Customer Side)

```
1. Customer receives candidates from Coordinator
   Example response:
   {
     candidates: [
       {foundation: "abc123", type: "srflx", ip: "1.2.3.4", port: 51820, rtt_ms: 45},
       {foundation: "def456", type: "host", ip: "192.168.1.100", port: 51820, rtt_ms: 200},
       {foundation: "ghi789", type: "relay", ip: "5.6.7.8", port: 51820, rtt_ms: 150}
     ]
   }

2. Perform connectivity check (RFC 5245)
   for each candidate in sorted_order (by priority):
     timeout = 2s
     start = now()
     
     send STUN BINDING REQUEST to candidate.ip:candidate.port
     wait for response or timeout
     
     if response received:
       rtt = now() - start
       working_candidates.append({candidate, rtt_measured: rtt})
       return candidate // use first working candidate
       
     if timeout:
       continue to next candidate
   
   if no working_candidate:
     fallback to DERP relay (see below)

3. Pick best candidate
   - Pick: working_candidates.min_by(rtt_measured)
   - Verify: type != "relay" (if srflx or host available, skip relay)
   - Fallback: relay type only if all srflx/host fail

4. Probe result metrics
   - Proof: metrics include:
     - total_candidates_tested: N
     - working_candidates: N
     - first_working_candidate_type: "srflx"|"host"|"relay"
     - time_to_first_working_ms: integer
     - preferred_candidate: {foundation, ip, port, rtt}
     - fallback_used: yes|no
```

#### TURN Relay Fallback

```
If all STUN candidates fail (no response within 5s total):

1. Request DERP relay from Coordinator
   - Endpoint: POST /api/v1/vpn/sessions/{session_id}/derp-fallback
   - Coordinator allocates: TURN relay IP:port, allocation token
   - Response: {relay_ip, relay_port, allocation_token, ttl_seconds}

2. Provider configures TURN
   - Provider sends: TURN ALLOCATE request with allocation_token
   - TURN server responds: relayed_transport_address (IP:port)
   - Provider adds: connection_address = relayed_transport_address to WireGuard config

3. Customer uses TURN relay
   - Send TURN CONNECT request with provider's allocation token
   - TURN relays packets: customer ↔ TURN ↔ provider
   - Latency hit: +20-50ms (additional hop)
   - Proof: relay_candidate marked as "relay" type, latency measured vs direct

4. Exit TURN relay (when direct path available)
   - After re-running ICE: if new srflx/host candidate works, switch away from relay
   - Endpoint: POST /api/v1/vpn/sessions/{session_id}/drop-relay
   - Provider stops allocation
   - Customer switches WireGuard endpoint to direct candidate
   - Proof: transition logged with old endpoint → new endpoint

Fallback strategy:
  ICE candidates (STUN) [0-5s] 
    → if all fail: request DERP relay [+1s] 
    → if relay succeeds: continue on relay [+20-50ms latency]
    → periodically re-check ICE [every 30s]
    → if ICE recovers: migrate back to direct path [migration logged]

Proof: Metrics show
  - derp_fallback_used: yes|no
  - derp_migration_time_ms
  - derp_latency_overhead_ms
  - derp_duration_sec
```

### NAT Traversal & Roaming

#### NAT Type Detection

```
1. Provider detects own NAT type
   - From STUN response: compare local_ip vs external_ip
   - If same: "OPEN" (no NAT)
   - If different but same port: "CONE_NAT" (predictable, easy)
   - If different port: "SYMMETRIC_NAT" (hard, requires DERP)
   
   Proof: NAT type reported in heartbeat {nat_type: "OPEN"|"CONE_NAT"|"SYMMETRIC_NAT"}

2. Customer detects NAT type
   - From STUN response received: compare local_ip vs external_ip
   - Same logic as provider
   
   Proof: NAT type reported in session metrics {customer_nat_type}
```

#### Roaming Detection & Reconnection

```
1. Provider sends probes to customer (every 10s)
   - Send: UDP keepalive packet on WireGuard tunnel
   - Packet: {sequence: N, timestamp: RFC3339}
   
2. Customer ACKs probes (or detects missing probes)
   - Customer receives probe: increment received_count
   - Customer doesn't receive 3 consecutive probes (30s): declare provider dead
   
3. Customer detects own IP change (roaming scenario)
   - Scenario A: WiFi → cellular (IP changes)
   - Scenario B: Network switch (office → home)
   - Scenario C: VPN reconnect after sleep
   
   Detection mechanisms:
   - Mechanism 1: Customer sends STUN probe every 30s, gets different external IP
   - Mechanism 2: Provider detects packets from new IP, notifies Coordinator
   - Mechanism 3: Heartbeat timeout (provider doesn't receive ACKs for 10s)
   
   Proof: Roaming event logged {old_ip, new_ip, detection_mechanism, timestamp}

4. Automatic reconnection on roaming
   - Customer: detect IP change (via STUN or timeout)
   - Customer: re-run ICE candidate gathering (timeout: 5s)
   - Customer: pick best new candidate
   - Customer: update WireGuard endpoint: `wg set peer <pubkey> endpoint <new_ip:port>`
   - Provider: accept packets from new IP (WireGuard validation passes)
   - Session continues uninterrupted
   
   Proof: Metrics show
   - roaming_detected_at: RFC3339
   - ip_changed_from: old_ip
   - ip_changed_to: new_ip
   - ice_recheck_time_ms: (should be <1s)
   - wg_endpoint_update_time_ms: (should be <100ms)
   - packet_loss_during_roam_ms: (should be <1000ms, target <500ms)
     Measured as: gap in ping sequence numbers or TCP sequence number jump
```

### Provider Discovery & Coordinator Integration

#### Provider Registration

```
1. Provider registers with Coordinator
   - Endpoint: POST /api/v1/providers/register
   - Request: {
       display_name: "Hatices-Mac-mini-2",
       public_key: base64(wg_pubkey),
       platform: "darwin",
       architecture: "arm64",
       region: "us-east"
     }
   - Response: {
       provider_id: UUID,
       status: "registered",
       created_at: RFC3339
     }

2. Provider heartbeat (includes all dynamically-discovered info)
   - Endpoint: POST /api/v1/providers/heartbeat (streaming gRPC bidi)
   - Every 5s send: {
       provider_id: UUID,
       public_key: base64(wg_pubkey), // verify SPKI matches registration
       status: "online",
       candidates: [
         {foundation, type, ip, port, rtt_ms, priority}
       ],
       nat_type: "OPEN"|"CONE_NAT"|"SYMMETRIC_NAT",
       last_seen_at: RFC3339,
       cpu_load: 0.45,
       memory_mb: 1024,
       active_sessions: 2,
       bytes_in_total: 10485760,
       bytes_out_total: 5242880
     }

3. Coordinator validation on heartbeat
   - Verify: public_key SPKI fingerprint matches providers.wg_public_key
   - If mismatch: reject heartbeat, provider must re-register
   - If OK: update providers.last_seen_at = now()
   - Deduplicate: candidates with same (ip,port) are merged (keep best RTT)

Proof: Heartbeats logged in audit trail with
  - provider_id
  - heartbeat_timestamp
  - candidate_count
  - nat_type
  - active_sessions_count
```

#### Provider Assignment (Sticky Sessions)

```
1. Customer requests session (region specified)
   - Request: {customer_id, region: "us-east"}

2. Coordinator assigns provider
   - Query: SELECT provider_id FROM providers 
            WHERE region = "us-east" AND status = "online"
            AND last_seen_at > now() - 60s
            ORDER BY load_estimate ASC, priority DESC
   - Sticky session check: 
     SELECT provider_id FROM sticky_sessions 
     WHERE (customer_id, destination_pattern) = (...) 
     AND expires_at > now()
   - If sticky hit: return same provider_id (refresh ttl to now() + 30min)
   - If no sticky: assign new provider (round-robin or load-balanced)
   - Store: sticky_sessions {customer_id, provider_id, destination_pattern, expires_at}

3. On provider failure: sticky session invalidated
   - Coordinator: DELETE sticky_sessions WHERE provider_id = failed_provider
   - Next refresh request: get new provider assignment
   
Proof: Sticky session table shows
  - customer_id
  - assigned_provider_id
  - assignment_timestamp
  - sticky_expires_at
  - was_invalidated: yes|no
  - invalidation_reason: "provider_dead"|"normal_expiry"|"manual"
```

### Encryption & Security

#### Per-Layer Encryption

```
Layer 1: TLS (Coordinator ↔ Customer/Provider)
  - Protocol: TLS 1.3 only (no 1.2 fallback)
  - Ciphersuites: TLS_AES_256_GCM_SHA384 only (no weaker options)
  - Certificates: mTLS (client + server certs required)
  - Pinning: Coordinator cert pinned in customer config (no CA chain verification)
  - Proof: TLS handshake logged with cipher negotiated, cert verification result

Layer 2: WireGuard (Customer ↔ Provider)
  - Protocol: WireGuard (RFC not yet standardized, but cryptographically sound)
  - Cipher: ChaCha20-Poly1305 for payload, Curve25519 for keys
  - Nonce: Per-packet, derived from sender's current counter
  - Forward secrecy: Ephemeral keys per session (keys not reused across sessions)
  - Proof: WireGuard interface shows counter increments, pcap shows no plaintext

Layer 3: STUN (Candidate Discovery — unencrypted by design)
  - STUN probes are unencrypted (necessary to discover external IP through NAT)
  - But probes are timing-only, no sensitive data
  - Proof: STUN packets contain only {message_type, transaction_id, xor_mapped_address}

Encryption guarantee:
  - Customer ↔ Coordinator: TLS encrypted
  - Customer ↔ Provider: WireGuard encrypted (all traffic, all layers)
  - Provider ↔ Internet: depends on customer's application (HTTPS, SSH, etc.)
  - No unencrypted customer data traverses iogrid infrastructure except Coordinator logs

Proof: Packet inspection
  - Coordinator sees: session_id, customer_id, bandwidth_stats (not content)
  - Provider sees: only packets from assigned customer (not other customers)
  - Customer sees: decrypted traffic (as expected)
  - Proof: tcpdump on Coordinator shows no decrypted user content
```

#### No Downgrade Attacks

```
1. TLS version pinned to 1.3 only
   - Config: tls_config.MinVersion = tls.VersionTLS13
   - Config: tls_config.MaxVersion = tls.VersionTLS13
   - Proof: TLS negotiation fails if client offers TLS 1.2

2. WireGuard protocol version pinned to 1.0
   - No version negotiation in WireGuard handshake
   - If peer protocol differs: handshake fails (no downgrade)
   - Proof: version mismatch causes connection timeout (silent failure)

3. Ciphersuites pinned
   - Only ChaCha20-Poly1305, no AES
   - Proof: negotiation fails if peer offers weaker cipher
```

---

## COMPREHENSIVE NON-FUNCTIONAL DEFINITION OF DONE

### Performance Criteria (Per Component)

#### Latency

```
Customer ↔ Coordinator (gRPC):
  - Baseline: <50ms RTT (network latency only, no processing)
  - Request latency: <100ms p99 (including Coordinator processing)
  - Session create: <500ms p99 (gRPC + DB write + STUN validation)
  - Session refresh: <200ms p99

Customer ↔ Provider (WireGuard):
  - Direct path latency: <100ms p99 (no proxy relay)
  - Goal: within 5ms of direct internet path
  - Measured: ping through WireGuard tunnel vs baseline
  - Proof: iperf3 test with latency histogram

ICE Candidate Discovery:
  - STUN probe timeout: 2s per candidate
  - Total discovery time: <5s (3 candidates in parallel)
  - Fallback to DERP: +1s (request to allocation)
  - Proof: metrics include time_to_first_working_candidate_ms

Provider Failover:
  - Coordinator dispatch time: <300ms (assign next provider)
  - ICE recheck time: <1s (test new candidates)
  - WireGuard setup time: <200ms (add peer, set endpoint)
  - Total failover time: <2s p99
  - Proof: end-to-end test kills provider, measures time to next provider online
```

#### Throughput

```
Bandwidth efficiency:
  - Tunnel overhead: <5% (WireGuard header ~80 bytes per packet)
  - Baseline: 1Gbps internet connection
  - Through VPN: >950Mbps (measured with iperf3)
  - Proof: iperf3 upstream vs downstream, calculate (downstream / upstream)

Multiple concurrent VPN instances (Bastion scenario):
  - 3 concurrent sessions: each maintaining >95% of single-session throughput
  - CPU overhead: <20% (WireGuard is lightweight)
  - Memory per session: <50MB (including buffers)
  - Proof: top/vmstat while running 3 VPN instances
```

#### Roaming Latency

```
IP change detection:
  - Detection time: <1s (from IP change to WireGuard endpoint update)
  - Mechanism: STUN re-probe every 30s, or probe timeout
  - Proof: tcpdump shows new IP candidate + endpoint update timestamp

Session continuity during roaming:
  - Packet loss: <1s worth of packets (TCP retransmit will recover)
  - No application restart required
  - Proof: ping through tunnel shows gap <1s during roaming

Failover latency:
  - Failover completion: <2s (from provider death to alternate provider online)
  - Measured: customer continuous ping, time to next ping success
```

### Reliability Criteria

#### NAT Scenario Coverage (7 Types)

```
✅ Test Case 1: OPEN (no NAT)
   Setup: Public IP, no firewalls
   Expected: Host candidate works immediately
   Proof: ICE completes in <1s, first_candidate_type = "host"

✅ Test Case 2: CONE NAT (single NAT, predictable)
   Setup: Home router with UPnP/NAT-PMP
   Expected: STUN finds external IP:port, srflx candidate works
   Proof: candidate.type = "srflx", latency <100ms

✅ Test Case 3: SYMMETRIC NAT (double NAT or restrictive)
   Setup: Corporate NAT + home NAT
   Expected: STUN finds different port per target, fallback to DERP
   Proof: first_working_candidate_type = "relay", derp_fallback_used = yes

✅ Test Case 4: IPv4-only (no IPv6)
   Setup: Customer and provider both IPv4-only
   Expected: All candidates IPv4, tunnel works
   Proof: candidates all have ip_version = "4", tunnel.src = ipv4

✅ Test Case 5: IPv6-only (no IPv4)
   Setup: Customer and provider both IPv6-only
   Expected: All candidates IPv6, tunnel works
   Proof: candidates all have ip_version = "6", tunnel.src = ipv6

✅ Test Case 6: Dual-stack (both IPv4 and IPv6 available)
   Setup: Most residential networks
   Expected: Candidate list has both v4 and v6, prefer v4 (higher priority)
   Proof: first_working_candidate.ip_version = "4" (if available)

✅ Test Case 7: Provider behind residential NAT (typical case)
   Setup: Provider is home PC on residential ISP (Comcast, AT&T, etc.)
   Expected: STUN finds public IP, customer connects directly
   Proof: provider_nat_type = "CONE_NAT", srflx candidate works, <100ms latency
```

#### Uptime & Fault Tolerance

```
Continuous operation test:
  - Duration: 3+ hours
  - Baseline: customer pings provider continuously (1 ping/sec)
  - Faults injected:
    - At 30min: Kill provider daemon (simulate provider crash)
    - At 60min: Change customer IP (simulate roaming)
    - At 90min: Kill provider again
  - Success criteria: Uptime >99% (allow <54 seconds of packet loss across 3 hours)
  
  Measurement:
    - Total packets: 3h × 3600s × 1 ping/sec = 10,800 packets
    - Loss allowed: <108 packets (1%)
    - Proof: pcap file shows ping timestamps, loss during failover <2s
```

#### Session Robustness

```
Session persistence on failover:
  - Sticky session transfers to new provider
  - Customer's session_id remains valid
  - No re-authentication required on failover
  - Proof: same session_id used before and after failover

Concurrent sessions:
  - Same customer can have 3+ concurrent sessions
  - Sessions remain independent (one failure doesn't affect others)
  - Proof: 3 tcpdump windows show independent traffic flows
```

### Code Quality Criteria

#### Test Coverage

```
Unit tests (>85% coverage):
  - Tunnel setup: connection_handler.go (ICE, WireGuard, validation)
  - Roaming logic: roaming_manager.go (IP change detection, reconnect)
  - Failover logic: failover_controller.go (provider selection, sticky session)
  - Safety checks: bastion_safety.go (routing isolation, kill switch)

  Coverage tool: go test -cover ./...
  Target: >85% for critical paths, >70% for edge cases
  Proof: go test output shows coverage percentage

Integration tests (E2E):
  - Scenario 1: Customer connects → provider → external IP check passes
  - Scenario 2: IP change → roaming → no packet loss >1s
  - Scenario 3: Provider dies → failover → no manual intervention
  - Scenario 4: 3+ concurrent sessions → each independent

  Tool: custom test harness in tests/vpn_e2e_test.go
  Proof: test logs show each scenario pass/fail + timing

Chaos tests:
  - Inject: network latency, packet loss, jitter, timeouts
  - Verify: system degrades gracefully (no crashes, no security issues)
  - Proof: stress test output shows no panics, no goroutine leaks
```

#### Security Audit Checklist

```
Crypto verification:
  ✅ WireGuard uses Curve25519 + ChaCha20-Poly1305 (industry standard)
  ✅ No custom crypto (only use battle-tested libraries)
  ✅ Forward secrecy: per-session keys (not reused)
  ✅ No key reuse across different context (different providers get different keys)

TLS verification:
  ✅ TLS 1.3 only (no downgrade to 1.2)
  ✅ AEAD ciphers only (no stream ciphers)
  ✅ Certificate pinning for Coordinator (no CA chain trust)

Secret handling:
  ✅ API keys stored with 0600 permissions (read-only by owner)
  ✅ API keys never logged in plaintext (only hashes)
  ✅ WireGuard private keys never transmitted (only pubkey)
  ✅ Session credentials cleared on termination

Authorization verification:
  ✅ Customer can only access own sessions (not others' sessions)
  ✅ Provider can only serve assigned customer (not other customers)
  ✅ Coordinator is only authority (no peer-to-peer auth bypass)

Data leakage verification:
  ✅ No user traffic routed through Coordinator (direct P2P only)
  ✅ No unencrypted traffic on Internet path (WireGuard layer)
  ✅ No DNS leaks (all customer DNS queries through VPN)
  ✅ No split-tunnel by default (all traffic through VPN)

Proof: Security audit checklist signed off + no findings in codebase scan
```

### Operational Criteria

#### Metrics & Observability

```
Per-session metrics (logged to METRICS stream in NATS JetStream):
  {
    "session_id": "uuid",
    "customer_id": "uuid",
    "provider_id": "uuid",
    "region": "us-east",
    "created_at": "RFC3339",
    "started_at": "RFC3339", // when first packet sent
    "ended_at": "RFC3339",
    
    "authentication": {
      "api_key_hash": "sha256...",
      "auth_method": "bearer_token",
      "auth_duration_ms": 50
    },
    
    "ice": {
      "candidate_count": 3,
      "working_candidates": 2,
      "discovery_time_ms": 1200,
      "first_working_candidate": {
        "type": "srflx",
        "ip": "1.2.3.4",
        "port": 51820,
        "rtt_ms": 45
      },
      "fallback_used": false
    },
    
    "roaming": {
      "events_count": 2,
      "events": [
        {
          "timestamp": "RFC3339",
          "old_ip": "192.168.1.100",
          "new_ip": "192.168.1.101",
          "detection_mechanism": "stun_probe",
          "reconnect_time_ms": 850
        }
      ]
    },
    
    "failover": {
      "triggered": false,
      "triggers": [],
      "total_time_ms": 0
    },
    
    "bandwidth": {
      "bytes_in": 1048576,
      "bytes_out": 524288,
      "duration_sec": 3600,
      "throughput_mbps": 2.33
    },
    
    "tunnel": {
      "latency_p50_ms": 45,
      "latency_p99_ms": 95,
      "packet_loss_percent": 0.1,
      "uptime_percent": 99.9
    },
    
    "exit": {
      "reason": "customer_disconnect",
      "timestamp": "RFC3339"
    }
  }

Aggregated metrics (rolled up per hour, per region):
  - Session success rate (how many reach ACTIVE state)
  - Average latency (p50, p99) by region
  - Failover rate (percent of sessions triggering failover)
  - Roaming frequency (roams per session hour)
  - NAT type distribution
  - DERP fallback rate
  
Alerts triggered:
  - Failover rate >5% in region (indicates provider instability)
  - Session success rate <95% (indicates infrastructure issue)
  - Latency p99 >150ms (indicates performance degradation)

Proof: Metrics dashboard live in Grafana, queries available for debugging
```

#### Graceful Degradation

```
On Coordinator unreachable (>30s):
  - Action: Customer can continue current sessions (no refresh needed)
  - Action: Cannot create new sessions (blocked until Coordinator online)
  - Proof: Session continues, new session returns "coordinator_unreachable" error

On provider unreachable:
  - Action: Automatic failover to alternate provider in region (<2s)
  - If no alternate: gracefully disconnect, inform user
  - Proof: Failover triggered, session transferred to new provider

On STUN server unreachable:
  - Action: Fallback to host candidates only (if available)
  - Action: If no candidates: fallback to DERP relay
  - Action: Continue operation on relay (higher latency, but functional)
  - Proof: First working candidate metric shows "relay" type used

On WireGuard interface down:
  - Action: Kill switch activates (drop all traffic on VPN table)
  - Action: Customer app sees no network (won't leak unencrypted traffic)
  - Action: User must explicitly restart VPN to resume
  - Proof: iptables rules show policy DROP for VPN table
```

---

## ACCEPTANCE CRITERIA (Engineer Can Build From This)

### Bastion Gets Encrypted Internet via Provider (Phase 1)

**Done when:**
```
1. curl https://ipecho.net/ through VPN tunnel returns PROVIDER's IP, not home router IP
   Proof: Screenshot of curl output with IP verification
   
2. ping 8.8.8.8 through tunnel shows <100ms latency (direct path, no relay)
   Proof: tcpdump shows WireGuard encrypted packets, latency histogram
   
3. Kill provider daemon → verify Coordinator still reachable (separate routing table)
   Proof: pkill provider, bastion can still SSH to Coordinator
   
4. Restart VPN: old routes cleaned up, bastion fully operational
   Proof: ip route show after VPN stop shows original routes restored
   
5. Test coverage >80% on tunnel, ICE, WireGuard modules
   Proof: go test -cover output
```

### No self-disconnect, Bastion stays safe (All Phases)

**Done when:**
```
1. Coordinator IP pinned in config (not DNS lookup)
   Proof: grep config, no "coordinator.iogrid.org" hostname, only IP
   
2. Coordinator traffic routed via default gateway, not VPN
   Proof: ip rule show, ip route show table 100 (separate table)
   
3. Kill VPN interface: Coordinator still reachable
   Proof: ip link delete wg-iogrid0, ping coordinator succeeds
   
4. 3+ hour stress test with failovers: Coordinator never unreachable >1s
   Proof: test log shows Coordinator connectivity check every 30s, all pass
   
5. Kill switch verified: if wg-iogrid0 down, no traffic escapes
   Proof: tcpdump shows no packets on eth0 when wg-iogrid0 down
```

---

## STOP CRITERIA (NEVER COMPROMISE)

❌ **HARD STOPS** — Do not proceed to Phase 2 until 100% complete:

1. External IP check must return **provider's IP**, not home router IP
   - If fails: debug tunnel setup, ICE candidate selection, routing

2. Bastion must never lose Coordinator connectivity during VPN operation
   - If fails: fix routing isolation, implement health checks

3. Graceful stop/start must not impact bastion host connectivity
   - If fails: ensure cleanup trap runs on all exit paths

4. All 7 NAT scenarios must pass (or fallback to DERP)
   - If fails: debug STUN probe, DERP allocation

5. Latency <100ms on direct path verified with iperf3
   - If fails: profile tunnel overhead, check MTU

6. Test coverage >80% on critical paths
   - If fails: add unit + integration tests

---

**This DoD is implementation-complete. An engineer should be able to build the entire VPN system from these specifications without asking a single question.**

---

**Status**: Ready for Phase 1 Engineering  
**Next**: Begin VPN-1 (ICE protocol spec) immediately, commit test specifications, start implementation
