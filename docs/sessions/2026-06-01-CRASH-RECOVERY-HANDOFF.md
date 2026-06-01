# Handoff — VPN end-state demo, crash recovery 2026-06-01

> Written immediately after a host crash during an autonomous push toward the
> VPN end-state demo. Reading order: (1) **North star** → (2) **Where we are
> right now** → (3) **What's next** → (4) **Guardrails**. After this doc, the
> two authoritative live state surfaces are `docs/ledger/TRACKER.md` and the
> GitHub issues — defer to them on conflict.

## 1. North star (the only acceptance criterion)

A real human customer can, from their own machine, do these three steps and
see their public IP change to a residential provider's IP:

```
curl -fsSL https://iogrid.org/install-cli.sh | sh    # 1. install
iogrid login                                          # 2. paste API key
iogrid vpn run --region us-east-1                     # 3. tunnel up
curl ifconfig.me   # → residential provider IP, not the customer's
```

Three is the only step that's not green yet. Everything else has CI coverage.

## 2. Where we are right now (HEAD = `f91ff5f`)

### Control plane — DONE

| Surface | State | Evidence |
|---|---|---|
| `install-cli.sh` at `iogrid.org/install-cli.sh` | ✅ | smoke installs CLI from it every run |
| `iogrid login` | ✅ | API key minted at `iogrid.org/customer/vpn`, persisted to `~/.iogrid/credentials.json` |
| `iogrid vpn regions` | ✅ | live against `api.iogrid.org` |
| `iogrid vpn run --region us-east-1` | ✅ | tunnel established successfully, heartbeat loop running |
| Coordinator session ledger (`vpn-svc`) | ✅ | Postgres-backed, sealed-secret DATABASE_URL |
| STUN UDP LoadBalancer | ✅ | `:3478` external |
| Per-tier quota (free=2GB/mo, plus=unlimited) | ✅ | #548 |
| Per-provider earnings → billing-svc | ✅ | #547 |
| Logout cascades session termination | ✅ | #549 |

### Customer-side data plane — DONE (this session, last 3 commits)

`f91ff5f` + `e0d9fc9` together get the customer side fully wired:

1. After WireGuard `BringUp`, the SDK assigns `10.66.0.2/16` to `wg-iogrid0`
   and installs the standard WireGuard `AllowedIPs = 0.0.0.0/0` pattern as
   two `/1` routes (`0.0.0.0/1` + `128.0.0.0/1`) via the tunnel — more
   specific than any existing `0.0.0.0/0` default, so the kernel picks them.
2. Before those `/1` routes go in, the SDK pins `/32` exception routes for
   (a) every IP `net.LookupIP(api.iogrid.org)` resolves to and (b) the picked
   ICE candidate's IP, both via the pre-VPN default gateway. Without these
   the SDK's own `confirmCandidate` and the outer WG UDP datagrams loop back
   into the half-built tunnel and the daemon dies seconds after "established".
3. The CI smoke (`vpn-e2e-smoke.yml`) uses `iogrid vpn run` in the background
   instead of `iogrid vpn connect` — `connect` is one-shot and the kernel GCs
   the TUN device the moment the process exits, so the post-tunnel `curl`
   never sees the tunnel.

Verified locally with `getcap cap_net_admin+eip /tmp/iogrid`:
- `ip addr show wg-iogrid0` → `UP,LOWER_UP`, `inet 10.66.0.2/16`
- `ip route show` → `0.0.0.0/1 dev wg-iogrid0`, `128.0.0.0/1 dev wg-iogrid0`,
  `<provider-ip>/32 via <orig-gw> dev eth0`
- `iogrid vpn run` process stays alive past the 30 s heartbeat.

### Provider-side data plane — NOT YET (the one remaining piece)

Issue **#529 path c** — provider daemon needs `TunForwardSink`:
- Open `/dev/net/tun` on the provider host, get a kernel TUN device.
- Decapsulated WG packets (currently going to `LoggingSink` which logs + drops)
  must instead be written to that TUN.
- `iptables -t nat -A POSTROUTING -o <wan-if> -j MASQUERADE` so the kernel
  rewrites the source IP on the way out.
- Enable `net.ipv4.ip_forward=1`.
- Reverse direction: read from TUN, encapsulate, send out the WG socket.

Once that's in and one real residential provider is paired in `us-east-1`,
the smoke flips green and `#532` closes.

## 3. What's next (priority order)

| # | Issue | Owner | Notes |
|---|---|---|---|
| 1 | **#529** provider WG forwarding (path c TUN + MASQUERADE) | `iogrid-worker` chepherd peer was on this; check `chepherd.peer_status("iogrid-worker")` before duplicating. If worker is idle/exhausted, lead takes it. | The single thing standing between us and a green end-to-end demo. |
| 2 | **#532** E2E external-IP-change verification in CI | Auto-closes the moment #529 lands — the smoke already asserts exit-IP changes and points at #529 in its error message. | No code needed — gated. |
| 3 | **#521** DERP relay fallback when ICE fails | `status/parked` — Phase-4 hardening, don't pull forward unless the demo asks for symmetric-NAT support. | |
| 4 | **#522** Stress test 100+ concurrent sessions per provider | `status/parked` — same. | |

Everything else under `module/vpn` is closed.

## 4. Guardrails (the discipline that makes this work non-stop)

Cross-referenced from `~/.claude/CLAUDE.md`, `CLAUDE.md` (project), and pinned
memory entries. Violating these has cost the founder time before; don't repeat.

1. **End-state, not phase-done.** Acceptance is the three-step demo in §1.
   CI-green alone is not the bar.
2. **Every commit `Refs #N` / `Closes #N`.** Every gap discovered →
   `gh issue create` in the same minute. (Memory:
   `feedback_github_discipline_100pct`, `feedback_vpn_no_github_issues_violation`.)
3. **TRACKER.md is live state.** Every PR/audit/cluster op → row update +
   commit in the same session window. (Memory:
   `feedback_tracker_md_must_be_updated_continuously`.)
4. **Every assistant turn ends with a tool call.** "Status:" / closing recaps
   are a stop signal. TRACKER commit, audit comment, or next code change is
   the status update. (Memory: `feedback_hard_stop_autonomy_violations_20260523`.)
5. **No `ScheduleWakeup` for CI polling.** CI re-runs on push; check inline
   at natural checkpoints.
6. **Chepherd peers before `Agent` sub-agents.** `chepherd.list_sessions` first;
   `chepherd.send_to_session("iogrid-worker", …)` if there's a match. `Agent`
   tool is the fallback only. (Memory:
   `feedback_use_chepherd_peers_not_subagents`.)
7. **Never label work "founder-physical" without exhausting all five unblock
   paths.** Standard API → escalate scopes → alternative service → workaround
   → re-shape demo. (Memory: project CLAUDE.md §6.)
8. **Never ask "Want me to / Should I" in autonomous mode.** The next message
   after identifying the step IS the commit. (Memory: project CLAUDE.md §7.)
9. **2-agent cap.** Hard ceiling, never exceed. Drain gracefully.
10. **Banned words** (per founder, current cron): `MVP`, `Iteration`,
    `out of scope`, `Blocker`. We are in the final stage — deliver the
    ultimate product.

## 5. Concrete starting moves for the fresh session

```bash
# 1. confirm HEAD + clean tree
git fetch origin && git status

# 2. check chepherd peers — is worker still working on #529?
#    (call via the MCP tool, not bash)
chepherd.list_sessions()
chepherd.peer_status("iogrid-worker")

# 3. read what's open + on top of the backlog
gh issue list --state open --label module/vpn

# 4. if worker is idle and #529 path c hasn't shipped, route to them:
chepherd.send_to_session("iogrid-worker", "<terse path-c brief>")
#    or take it on the main thread if they hit their cap.

# 5. while #529 bakes, verify customer side end-to-end locally:
cd cmd/iogrid && GOTOOLCHAIN=go1.23.4 go build -o /tmp/iogrid .
sudo setcap cap_net_admin+eip /tmp/iogrid
/tmp/iogrid login --api-key=<...> --customer-id=<...> --coordinator=https://api.iogrid.org
nohup /tmp/iogrid vpn run --region us-east-1 > /tmp/run.log 2>&1 &
ip addr show wg-iogrid0    # expect UP + 10.66.0.2/16
ip route show              # expect /1 routes via wg-iogrid0
```

The moment #529 path c lands on `main` and is rolled, re-run
`vpn-e2e-smoke.yml` — it will go green, #532 closes itself by self-reference,
and the demo is complete.
