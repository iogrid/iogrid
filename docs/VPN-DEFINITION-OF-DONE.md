# VPN Definition of Done — Non-Stop Implementation Target

**Project**: iogrid P2P VPN with Seamless Regional Roaming  
**Target Completion**: 2026-06-15  
**First Client**: bastion machine (current working host)  
**Success**: Can ping internet through provider tunnel, auto-failover on provider death, auto-reconnect on IP change — all without self-disconnect.

---

## One-Paragraph Functional Definition

**iogrid VPN enables direct, encrypted P2P tunnels between customer (bastion) and residential provider through automated NAT traversal (ICE), seamless roaming (IP changes auto-reconnect <1s), and regional failover (provider offline → auto-switch to alternate in <2s) without requiring central proxy relay. Bastion remains connected to Coordinator throughout (strict routing isolation prevents self-disconnect). Customer internet traffic flows through provider endpoint, verified by external IP check. System handles IPv4-only, IPv6-only, double NAT, and symmetric NAT scenarios. Graceful stop/start with no host impact.**

---

## One-Paragraph Non-Functional Definition

**Implementation must achieve <100ms latency (direct path, no relay overhead), <5% tunnel overhead (WireGuard ~1ms, ICE <100µs), >85% code coverage on tunnel/roaming/failover critical paths, and >99% uptime (survives double NAT, symmetric NAT, IPv4-only, IPv6-only networks). Coordinator IP pinned (no DNS), all Coordinator traffic isolated from VPN (separate routing table). Fails safely: if VPN dies, bastion connectivity to Coordinator unaffected (verified every 30s). Security: WireGuard proven crypto (no CA compromise), no elevation of Coordinator trust. Scalable: 3+ concurrent VPN instances on bastion simultaneously.**

---

## Detailed Functional DoD (Checklist)

### Tunnel Establishment
- [ ] **Customer connects to Coordinator, requests VPN session**
  - Input: `region="us-east"`, API key
  - Output: session_id, provider_id, provider WireGuard public key, ICE candidates
  - Passes: auth + session creation takes <5s

- [ ] **ICE Candidate Discovery**
  - Provider reports: host + srflx + prflx candidates
  - Customer receives all candidates
  - Customer probes candidates in parallel (timeout 2s per candidate)
  - At least 1 candidate responds (or fallback to DERP)
  - Passes: ICE completes in <5s, finds working candidate

- [ ] **WireGuard Tunnel Established**
  - Customer creates WireGuard interface (wg-iogrid0)
  - Customer adds provider as peer with best-candidate endpoint
  - Both sides exchange packets
  - Tunnel verified: customer pings provider WireGuard IP (10.64.0.1), responds

- [ ] **Internet Traffic Routes Through Provider**
  - Customer external IP check: `curl http://ipecho.net` through tunnel
  - Returns provider's IP (not bastion's home router IP)
  - Passes: external IP matches provider's registered IP

### Roaming (IP Change Detection & Reconnection)
- [ ] **Bastion IP Changes (simulated: WiFi → cellular, home → office)**
  - Detected: within 1s (customer detects packet loss or provider sends probe)
  - Action: customer re-runs ICE candidate check from new IP
  - Outcome: WireGuard endpoint updated to new candidate
  - Passes: tunnel resumes without user action, no traffic loss >1 second

- [ ] **Automatic ICE Refresh**
  - Provider sends probe packet every 10s
  - Customer ACKs immediately
  - If 3 probes fail: customer assumes provider dead, asks Coordinator for failover
  - Passes: roaming transparent to applications

### Regional Failover
- [ ] **Primary Provider Fails (simulate: kill provider daemon)**
  - Detected: customer doesn't receive 3 consecutive probes
  - Action: customer notifies Coordinator, requests next provider in region
  - Coordinator returns: provider_id_2, candidates_2, wg_pubkey_2
  - Customer: ICE check → WireGuard setup → resume traffic
  - Passes: failover takes <2s, traffic resumes, no self-disconnect

- [ ] **Alternate Provider Succeeds**
  - New tunnel working (ping, external IP check)
  - Sticky session updated to new provider
  - Metrics logged: failover_count++, time_to_failover_ms

### Bastion Safety (Critical)
- [ ] **Coordinator Always Reachable**
  - Coordinator IP pinned in config (not DNS lookup)
  - Coordinator traffic routed via host's default gateway (not through wg-iogrid0)
  - Verification: ping Coordinator every 30s (background task)
  - Passes: Coordinator stays online even if VPN dies

- [ ] **No Routing Loop**
  - VPN interface created with separate routing table
  - Coordinator traffic explicitly excluded from VPN table
  - Test: kill provider, verify Coordinator still reachable, VPN can failover
  - Passes: no self-disconnect scenario

- [ ] **Kill Switch Operational**
  - If wg-iogrid0 goes down: policy drop on VPN table (no traffic leaks)
  - Customer traffic cannot escape through home gateway
  - Test: simulate WireGuard crash, verify no traffic escapes
  - Passes: no unencrypted traffic leaks on disconnect

- [ ] **Graceful Cleanup**
  - `pkill -TERM vpnd` or script exit (even error): cleanup trap runs
  - Actions: remove WireGuard interface, restore original routes, flush sessions
  - Result: bastion host fully operational after VPN stop
  - Passes: no orphaned interfaces, no broken routes

### Metrics & Observability
- [ ] **Per-Session Metrics Tracked**
  - session_id, customer_id, region, provider_id
  - ice_time_ms, wg_establish_time_ms, first_packet_latency_ms
  - failover_count, roaming_events, roaming_reconnect_time_ms
  - bytes_in, bytes_out, duration_sec, exit_reason

- [ ] **Alerting on Failures**
  - Coordinator unreachable >30s: alert admin
  - Failover >2x in 5min: alert (provider flaky)
  - Roaming >5x in 5min: alert (customer roaming too much)

---

## Non-Functional DoD (Performance & Security)

### Performance
- [ ] **Latency**
  - Direct path latency: <100ms (no proxy relay overhead)
  - Proof: measure RTT from bastion through provider to public IP
  - Baseline: home router latency, VPN latency - baseline < 5ms

- [ ] **Bandwidth**
  - Tunnel overhead: <5%
  - Proof: iperf3 baseline vs through VPN tunnel
  - 1Gbps baseline → >950Mbps through tunnel

- [ ] **ICE Discovery Time**
  - First working candidate found: <5s
  - Proof: time from ICE start to first packet over WG

- [ ] **Failover Time**
  - Coordinator request → new provider WireGuard online: <2s
  - Proof: measure per-phase: dispatch (0.3s) + ICE (0.8s) + WG setup (0.5s) = 1.6s

- [ ] **Roaming Reconnection**
  - IP change detected: <1s
  - New tunnel online: <1s total
  - Proof: traffic graph shows <1s gap

### Reliability
- [ ] **NAT Scenario Coverage**
  - [ ] Open network (no NAT) ✓
  - [ ] Single NAT (home router) ✓
  - [ ] Double NAT (corporate + home) ✓
  - [ ] Symmetric NAT (challenging) ✓
  - [ ] IPv4-only ✓
  - [ ] IPv6-only ✓
  - [ ] Dual-stack ✓

- [ ] **Uptime**
  - 3+ hour continuous test: >99% packets delivered
  - With simulated roaming (IP changes every 5min): >99%
  - With simulated failover (provider dies every 10min): >99%

### Code Quality
- [ ] **Coverage**
  - Tunnel setup: >85%
  - Roaming logic: >85%
  - Failover logic: >85%
  - Safety checks: 100%

- [ ] **Testing**
  - Unit tests: all protocol handlers
  - Integration tests: tunnel creation → traffic flow
  - E2E tests: roaming + failover scenarios
  - Chaos tests: provider dies, customer IP changes, network loss

### Security
- [ ] **Crypto**
  - WireGuard: proven (audited, used by Mullvad)
  - No CA compromise (pinned public key per provider)
  - No downgrade attacks (fixed protocol version)

- [ ] **Privacy**
  - Coordinator only sees: customer_id, provider_id, region, bandwidth
  - Coordinator does NOT see: traffic content, destinations
  - Fail-closed: if Coordinator dies, customers stay connected (no fallback)

- [ ] **No Leaks**
  - IPv4 + IPv6: both routed through VPN (no split-tunnel)
  - DNS: must be specified (either through VPN or custom)
  - Kill switch: no traffic without tunnel

---

## Implementation Phases & Checkpoints

### Phase 1: Core (Weeks 1-2) — HARD STOP UNTIL 100% DONE
- Coordinator + Provider + Customer SDK: basic P2P tunnel
- **Checkpoint**: Customer ping provider through WireGuard tunnel
- **Gate**: External IP check returns provider's IP (not bastion home IP)

### Phase 2: Roaming (Weeks 2-3) — HARD STOP UNTIL 100% DONE
- Roaming detection + automatic reconnection
- **Checkpoint**: Simulate bastion IP change, tunnel auto-recovers <1s
- **Gate**: No packet loss >1s during IP change

### Phase 3: Failover (Week 3) — HARD STOP UNTIL 100% DONE
- Regional failover logic
- **Checkpoint**: Kill provider, customer auto-switches to alternate <2s
- **Gate**: Traffic resumes, no manual intervention needed

### Phase 4: Hardening (Week 4) — HARD STOP UNTIL 100% DONE
- Safety, DERP fallback, comprehensive NAT testing
- **Checkpoint**: All 7 NAT scenarios working
- **Gate**: 3+ hour stress test with simulated failures >99% uptime

### Phase 5: Production (Week 5) — NEVER STOP
- Internal dogfooding (all engineers use iogrid VPN as primary)
- Monitor: latency, uptime, failover rate, roaming frequency
- **Gate**: Ready for beta customer release

---

## Stop Criteria (NEVER TRIGGER THESE)

❌ **DO NOT STOP** if:
- Test fails → fix it
- Coverage <85% → add tests
- NAT scenario doesn't work → redesign
- Bastion self-disconnects → debug safety layer
- Latency >100ms → find cause and optimize

✅ **ONLY STOP** when:
- All phases 100% complete
- All checkpoints passed
- All NAT scenarios working
- All non-functional metrics achieved
- 3+ hour stress test >99% uptime
- Code coverage >85% on critical paths
- Security review complete

---

## Daily Standup Checklist

**Each day, start standup with:**

1. **Yesterday's work**: Which DoD items completed?
2. **Today's target**: Which phase + which DoD items?
3. **Blockers**: Any hard stops? (answer: there shouldn't be any, we design around them)
4. **Metrics**: Current latency, failover time, uptime?

**End of sprint (Friday):**
- [ ] Phase <N> 100% DoD checklist signed off
- [ ] All tests passing
- [ ] Stress test results logged
- [ ] Ready for Phase <N+1> Monday

---

## Success Declaration

**VPN Launch is DONE when:**

```
Phase 1 ✓ COMPLETE
  - [x] Customer establishes P2P WireGuard tunnel
  - [x] External IP check returns provider IP
  - [x] Latency <100ms verified
  - [x] Tests >85% coverage
  
Phase 2 ✓ COMPLETE
  - [x] Roaming detection <1s
  - [x] Auto-reconnect <1s
  - [x] No packet loss >1s during roaming
  
Phase 3 ✓ COMPLETE
  - [x] Failover <2s
  - [x] Regional alternate selected correctly
  - [x] Sticky session updated
  
Phase 4 ✓ COMPLETE
  - [x] 7/7 NAT scenarios working
  - [x] 3+ hour stress test >99% uptime
  - [x] Bastion never self-disconnects (Coordinator always reachable)
  - [x] Kill switch verified
  - [x] Routing isolation verified
  
Hardening ✓ COMPLETE
  - [x] Security review passed
  - [x] Coverage >85%
  - [x] No regressions in existing systems
  - [x] Metrics dashboard live
  
GO/NO-GO: Launch to internal beta (all engineers use for 1 week)
  - [x] Zero self-disconnect incidents
  - [x] Zero unencrypted traffic leaks
  - [x] Zero Coordinator disconnects
  - [x] Uptime >99.9% across team
  
✅ VPN PRODUCTION READY
```

---

## References

- Architecture: docs/VPN-ARCHITECTURE.md
- RFC 5245 (ICE): https://tools.ietf.org/html/rfc5245
- RFC 8445 (ICE 2.0): https://tools.ietf.org/html/rfc8445
- WireGuard: https://www.wireguard.com/

---

**Last Updated**: 2026-06-01  
**Status**: Ready for Phase 1 implementation  
**Next Action**: Create GitHub issues (VPN-1 through VPN-15) and begin backlog sprint
