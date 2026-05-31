# iogrid P2P VPN Architecture
**Status**: Design Phase  
**Priority**: P0  
**Target Launch**: 2026-06-15  

---

## Executive Summary

**iogrid VPN** is a **seamless, roaming-aware P2P VPN** that establishes direct encrypted tunnels between customers and residential providers. Unlike centralized proxies, it:

- ✅ **Direct P2P**: Customer ↔ Provider (no relay bottleneck)
- ✅ **Seamless Roaming**: IP change? Auto-reconnect in <1s
- ✅ **Regional Failover**: Provider dies → auto-switch to alternate in same region (<2s)
- ✅ **Bastion-Safe**: First client is bastion itself (strict isolation to prevent self-disconnect)
- ✅ **Bleeding-Edge**: ICE-based NAT traversal + WireGuard tunnel

**Differentiation from competitors:**
- Tailscale: Requires DERP relay, centralized trust
- Mullvad: Fixed datacenters, not residential P2P
- Custom protocols: Reinventing crypto (bad)
- **iogrid**: WireGuard proven crypto + ICE auto-discovery + residential provider mesh

---

## Technology Stack

### Core Tunnel: WireGuard

**Why WireGuard:**
- 600 lines of code (auditable)
- Modern elliptic curve crypto (ChaCha20-Poly1305)
- Sub-1ms latency overhead
- Native: Linux kernel, Windows NDIS, macOS/iOS/Android userspace
- Proven battle-tested (Mullvad, Tailscale, Wireguard.com)

**Constraints:**
- No protocol versioning (fixed packet format)
- No connection state (stateless on both ends)
- Forces session management to application layer (we do this)

### NAT Traversal: ICE (RFC 8445)

**Why ICE (not STUN alone):**
- **STUN** finds your external IP (basic)
- **TURN** relays traffic (expensive)
- **ICE** tries STUN candidates, falls back to TURN (used by WebRTC, proven)
- **Built-in roaming**: ICE detects IP change, replaces candidate without reconnect

**ICE Candidates we gather:**
1. **Host candidates** (local LAN IPs: 192.168.x.x, 10.x.x.x)
2. **Server Reflexive** (STUN: your external IP + port)
3. **Peer Reflexive** (discovered from responses)
4. **Relay** (TURN fallback, minimal use)

### Session Layer: Custom Roaming Manager

**Detects IP change:**
- Periodic "are you still there?" probes from provider
- Customer detects packet loss → triggers ICE re-check
- Provider sends candidate refresh every 30s

**Automatic failover within region:**
- Coordinator maintains `provider_list[region]`
- If primary provider unreachable → try #2, #3... in region
- Sticky session on new provider (30min sticky)
- Audit log: which provider, when, why failed

---

## System Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        COORDINATOR                          │
│  • Provider registration + ICE candidate tracking           │
│  • Session ledger (customer → provider binding)             │
│  • Regional grouping (us-east: [prov1, prov2, prov3])      │
│  • STUN server (or coturn integration)                      │
└─────────────────────────────────────────────────────────────┘
     ▲                                          ▲
     │ (signaling only: protobuf messages)     │
     │                                          │
  ┌──┴──────────────────────────┐   ┌─────────┴──────────────┐
  │   CUSTOMER (bastion)         │   │   PROVIDER (daemon)    │
  │                              │   │                        │
  │ 1. Request session           │   │ 1. Register + ICE      │
  │    (region: us-east)         │   │    candidates          │
  │                              │   │                        │
  │ 2. Receive provider IP +     │   │ 2. Listen on           │
  │    WireGuard pubkey +        │───│    WireGuard iface     │
  │    ICE candidates            │   │                        │
  │                              │   │ 3. Report any IP       │
  │ 3. ICE connectivity check    │   │    change to Coord     │
  │    (try candidates A, B, C)  │   │                        │
  │                              │   │ 4. Health checks       │
  │ 4. WireGuard establish       │───│    from customer       │
  │    (direct peer)             │   │                        │
  │                              │   │ 5. If customer silent  │
  │ 5. Verify working tunnel     │   │    → notify Coord      │
  │    (ping provider, confirm   │   │                        │
  │     routing works)           │   │                        │
  │                              │   │                        │
  │ 6. On IP change:             │   │                        │
  │    → re-run ICE              │───│    (auto-detect        │
  │    → reconnect WG if needed  │   │     new candidate)     │
  │    → resume traffic          │   │                        │
  │                              │   │                        │
  │ 7. On primary provider fail: │   │                        │
  │    → ask Coord for next      │───│ (N/A if failed)        │
  │    → repeat ICE + WG setup   │   │                        │
  └──────────────────────────────┘   └────────────────────────┘
         │                                     │
         └──────────────────────────────────────┘
              (direct P2P WireGuard tunnel)
              all traffic through provider
              provider routes to internet
```

---

## Protocol Specification

### 1. Session Request (Customer → Coordinator)

```protobuf
message RequestVpnSession {
  string customer_id = 1;
  string region = 2;  // "us-east", "eu-west", etc.
  string workload_type = 3;  // "vpn"
  repeated string exclude_provider_ids = 4;  // failed providers to skip
}

message VpnSessionAssignment {
  string session_id = 1;
  string provider_id = 2;
  string provider_wg_public_key = 3;  // base64
  repeated IceCandidate candidates = 4;
  string derp_relay_fallback = 5;  // IP:port of DERP (if no ICE works)
}

message IceCandidate {
  string foundation = 1;
  uint32 component = 2;  // always 1 for RTP (we use 1)
  string transport = 3;  // "udp"
  uint32 priority = 4;   // ICE priority formula
  string connection_address = 5;  // IP
  uint32 connection_port = 6;
  string candidate_type = 7;  // "host", "srflx", "prflx", "relay"
  string related_address = 8;  // optional (for reflexive)
  uint32 related_port = 9;
}
```

### 2. ICE Connectivity Check (Customer → Provider UDP candidates)

```
Customer sends STUN binding request to each candidate IP:port
Provider responds from same IP:port
→ That's a working candidate
Customer picks best candidate (lowest latency/priority)
```

### 3. WireGuard Tunnel Establishment (Customer ↔ Provider)

```
1. Customer generates ephemeral WG privkey
2. Customer sends to Coordinator:
   {session_id, customer_wg_public_key, chosen_ice_candidate}
3. Coordinator forwards to Provider
4. Provider adds customer pubkey to WG interface
5. Customer adds provider pubkey to WG config
6. Both sides: wg set peer <pubkey> endpoint <ip:port>
7. Traffic flows through tunnel
```

### 4. Roaming Detection & Reconnection

```
Provider sends probe packet to customer every 10s
Customer ACKs probe

If provider doesn't receive 3 probes (30s):
  → marks session as stale
  → waits for customer to reconnect

If customer detects packet loss:
  → re-runs ICE connectivity check
  → if new IP found, updates WG endpoint
  → re-sends to Coordinator
  → continues traffic

If customer detects 3 failed probes:
  → assumes provider dead or network partition
  → asks Coordinator for next provider in region
  → repeats session + ICE + WG setup
  → total time: <2s
```

### 5. Regional Failover

```
Coordinator maintains:
  regions["us-east"] = [
    {provider_id: "prov-123", available: true, last_seen: 2s},
    {provider_id: "prov-456", available: true, last_seen: 15s},
    {provider_id: "prov-789", available: false, last_seen: 65s},
  ]

On failover request from customer:
  1. Sort by: available first, then by last_seen (fresher first)
  2. Exclude any in customer's "exclude" list
  3. Return top 3 candidates
  4. Customer tries in order, stops at first success

Timeout: per-provider connection attempt = 3s
Region search timeout = 10s (if no provider connects, ask for DERP fallback)
```

---

## Safety Architecture (Bastion as Client)

### Threat: Routing Loop

**Scenario**: VPN routes all traffic through provider, including coordinator traffic.
**Result**: If provider dies, coordinator unreachable → VPN can't reconnect → bastion isolated.

**Mitigation (strict separation):**
```
Bastion routing tables:

Route Table A (default):
  0.0.0.0/0 → gateway 192.168.1.1  (home router)
  
Route Table B (VPN-only):
  10.1.0.0/16 → wg0 (VPN interface)
  
Coordinator traffic ALWAYS via Table A:
  - DNS lookups for coordinator.iogrid.org
  - TCP/IP to coordinator IP
  - Never enters Table B
  
VPN traffic (customer apps) via Table B:
  - Everything else
  
Kill switch: if wg0 dies, no traffic allowed on Table B
```

**Implementation:**
```bash
# Never route these through VPN
ip rule add from <coord_ip> table 100 priority 100
ip route add <coord_ip> via <default_gw> table 100

# OR: use network namespace isolation
# VPN interface in separate netns, marked traffic only
```

### Threat: DNS Hijack

**Scenario**: VPN provider intercepts DNS → returns wrong IP for coordinator.

**Mitigation:**
- Coordinator IP pinned in config (not DNS)
- All Coordinator gRPC calls use pinned IP
- Customer DNS queries go through VPN (as desired for privacy)
- Coordinator DNS stays separate

### Threat: Coordinator Unreachable → Can't Failover

**Mitigation:**
- Cache last-known provider list locally
- On Coordinator unreachable: use cached list for failover
- Retry Coordinator connection with exponential backoff
- Beacon mode: if no provider works, enable DERP relay and retry Coordinator

---

## Regional Architecture

```
Region: us-east (3+ providers per region)
  Provider A (IP: 1.2.3.4, latency: 45ms, load: 45%)
  Provider B (IP: 1.2.3.5, latency: 52ms, load: 60%)
  Provider C (IP: 1.2.3.6, latency: 120ms, load: 10%)
  
Region: eu-west (2+ providers)
  Provider D
  Provider E
  
Region: ap-south (1 provider, low redundancy)
  Provider F

Customer in us-east requests:
  → Assigned Provider A (lowest latency)
  → If A fails: failover to B (within region, similar latency)
  → If B fails: failover to C (higher latency but functional)
  → If all fail in region: ask Coordinator for fallback
     → Offer DERP relay (latency 200-400ms, but online)
     → OR ask for different region (higher latency, more hops)
```

---

## Bastion-Specific Safety Measures

### Before VPN Starts
- [ ] Verify Coordinator reachable (health check)
- [ ] Pre-configure kill switch
- [ ] Test: ping coordinator through VPN off (baseline)

### While VPN Runs
- [ ] Every 30s: verify Coordinator still reachable
- [ ] If Coordinator unreachable: trigger failover to cached provider list
- [ ] If failover exhausted: enable DERP, retry Coordinator
- [ ] If still unreachable: pull down VPN, alert admin, preserve bastion connectivity

### Graceful Shutdown
- [ ] `pkill -TERM vpnd` → flush sessions → close tunnels → clean routes → exit
- [ ] On script exit (even error): trap cleanup, restore original routes

---

## Success Criteria (DoD)

### Functional
- ✅ Bastion can establish direct P2P WireGuard tunnel to provider
- ✅ Internet traffic routes through provider endpoint
- ✅ Provider endpoint discovered via ICE (tries 3+ candidates)
- ✅ Regional failover works: primary fails → secondary up in <2s
- ✅ Roaming works: IP change detected → tunnel updates in <1s (no traffic loss)
- ✅ Bastion never self-disconnects (Coordinator always reachable)
- ✅ Can cleanly stop/start VPN without host impact

### Non-Functional
- ⚡ Latency: <100ms to provider (direct path, no relay)
- ⚡ Failover time: <2s (re-request, ICE, WG setup)
- ⚡ Roaming detection: <1s
- ⚡ Tunnel overhead: <5% (WireGuard ~1ms, ICE <100µs overhead)
- 🛡️ Code coverage: >85% for tunnel, roaming, failover logic
- 🛡️ Security: no CA compromise, no Coordinator trust elevation
- 🛡️ Resilience: survives double NAT, symmetric NAT, IPv4-only, IPv6-only
- 📈 Scalability: 3+ simultaneous VPN instances on bastion

---

## Phased Rollout

### Phase 1: Core (Week 1-2)
- Coordinator: Session + ICE candidate tracking
- Provider: WireGuard interface + candidate reporting
- Customer: ICE discovery + WireGuard tunnel
- Bastion: Manual testing

### Phase 2: Roaming (Week 2-3)
- Roaming detection (IP change, probe ACK)
- Automatic ICE re-check
- WireGuard endpoint update
- Test: network switch, WiFi → cellular

### Phase 3: Regional Failover (Week 3)
- Failover logic
- Regional provider grouping
- Sticky session on new provider
- Test: kill provider, auto-switch

### Phase 4: Hardening (Week 4)
- Safety: Coordinator always reachable
- Kill switch + route isolation
- DERP fallback
- Comprehensive testing (all NAT scenarios)

---

## Metrics to Track

```
Per-session:
  - session_id, customer_id, region, primary_provider, assigned_at
  - ice_candidate_count (how many worked?)
  - ice_time_ms (time to find first candidate)
  - wg_establish_time_ms
  - first_packet_latency_ms
  - failover_count (how many times switched provider?)
  - roaming_events (IP changes)
  - roaming_reconnect_time_ms
  - bytes_in, bytes_out, duration_sec

Aggregates:
  - success_rate by region
  - avg_latency by region
  - failover_rate by region
  - NAT type distribution (symmetric? cgnat? open?)
  - platform/ISP insights (what works, what doesn't?)
```

---

## References

- RFC 5245 (ICE): https://tools.ietf.org/html/rfc5245
- RFC 8445 (ICE 2.0): https://tools.ietf.org/html/rfc8445
- WireGuard Protocol: https://www.wireguard.com/protocol/
- WireGuard Whitepaper: https://www.wireguard.com/papers/wireguard.pdf
- WebRTC ICE (reference implementation): https://github.com/pion/ice

---

**Next Step**: Create detailed backlog items based on this architecture.
