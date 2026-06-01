# Operator runbook — pair a real provider daemon for VPN

> Source: live verification 2026-06-01 by iogrid-lead.
> Companion to `docs/runbooks/vpn/customer-onboarding.md`.

End-state: a residential PC or Mac runs the `iogridd` daemon, registers
with vpn-svc, publishes ICE candidates, and accepts customer WireGuard
peer bindings — so the customer's `curl ifconfig.me` returns the
provider's exit IP.

For the demo bastion (one operator, one machine that's both provider
AND customer-tester), see "Bastion smoke pair" below. For a residential
provider running at someone's home, see "Residential install".

---

## Bastion smoke pair (Linux host on a public IP)

Use this when you want a paired provider running on the same Linux box
you're testing from. The bastion provider isn't "residential" — it's
a stand-in so the data plane has a real target.

```bash
# 1. Build the daemon (one-shot; release binary cached).
cd /home/openova/repos/iogrid
cargo build --release --bin iogridd

# 2. Run with --public-ip pinned to the bastion's externally-reachable
#    IPv4. Otherwise the daemon only publishes LAN host candidates
#    (10.x, 172.17.x) which customers outside the LAN can't route to.
#    --provider-id can be persistent: empty + first run generates a
#    UUID at <state-dir>/provider_id and reuses it on subsequent boots.
PUBLIC_IP=$(curl -s4 ifconfig.me)
sudo setcap cap_net_admin+eip ./daemon/target/release/iogridd

nohup ./daemon/target/release/iogridd \
  --state-dir=/tmp/iogridd-vpn-state \
  --vpn-svc=https://api.iogrid.org \
  --vpn-listen-addr=0.0.0.0:51820 \
  --stun-server=45.151.123.50:3478 \
  --region=us-east-1 \
  --public-ip="$PUBLIC_IP" \
  > /tmp/iogridd-vpn.log 2>&1 &
disown

# 3. Verify registration + candidate publication
sleep 5
PROVIDER_ID=$(grep -oE 'provider_id":"[a-f0-9-]+' /tmp/iogridd-vpn.log | head -1 | cut -d'"' -f3)
curl -s "https://api.iogrid.org/v1/vpn/regions" | jq '.regions'
curl -s "https://api.iogrid.org/v1/vpn/providers/$PROVIDER_ID/candidates" | jq '.candidates'
```

Expected: at least one `candidate_type=host` entry with the public IPv4
+ a higher priority than the LAN candidates (the #557 boost).

If srflx discovery succeeds (post-#551 daemon), you'll also see a
`candidate_type=srflx` entry that maps the bastion's local port to the
external STUN-mapped address. With both, residential providers behind
NAT don't need the `--public-ip` override.

---

## Residential install (home PC or Mac, behind NAT)

End-state install path the customer-facing README points at. Different
binary (`installer/install.sh`, NOT the customer `install-cli.sh`):

```bash
curl -fsSL https://iogrid.org/installer/install.sh | sh
```

This installs the platform-specific package:
- Linux: .deb / .rpm / .apk → systemd unit `iogridd.service`
- macOS: signed .pkg → `LaunchAgent io.iogrid.daemon`
- Windows: signed .msi → Windows service `IogridDaemon`

After install, the daemon launches with defaults from `config.toml`. To
join the VPN data plane the operator pastes a pairing token from
`https://iogrid.org/provider` — that token mints a `provider_id` + mTLS
identity bundle bound to this device.

**NAT traversal (when --public-ip isn't available):**
1. Daemon STUNs `stun.iogrid.org:3478` on the same UDP socket
   `vpn_listen_addr` binds to.
2. The mapped address from the STUN BINDING SUCCESS is published as a
   `srflx` ICE candidate (priority 4 per the SDK candidateScore).
3. Customer SDK picks the highest-scoring candidate; for residential
   behind 1:1 NAT this is the srflx address.
4. WireGuard handshake initiates from customer → mapped address; NAT
   keeps the binding alive because the daemon also sent a STUN BINDING
   REQUEST through it within the last 30s.

When STUN fails (asymmetric NAT, no public IPv4, ISP CGNAT without
hairpin), the daemon should fall back to a DERP relay — that's
#521 (parked).

---

## Health + bind diagnostics

The daemon logs to stderr (Linux: `journalctl -u iogridd`). Look for:

| Line | What it means |
|---|---|
| `provider registered with vpn-svc` | Health POST succeeded — provider is visible in `/v1/vpn/regions` |
| `publishing manual public-IP host candidate (#557)` | --public-ip override is active |
| `srflx discovery failed; publishing host-only` | STUN BINDING failed (#551 was THE common cause; defensive parser landed in #555) |
| `session bound — customer peer upserted + provider key posted` | A customer's WG key was just registered; data plane is now ready for that session |
| `bind session failed; will retry next tick` | Customer's posted key is malformed or the upsert is failing inside boringtun |

---

## Stopping / restarting

```bash
# Bastion / hand-run
pkill -f 'iogridd --vpn-svc'

# Linux systemd
sudo systemctl restart iogridd

# macOS LaunchAgent
launchctl kickstart -k gui/$(id -u)/io.iogrid.daemon

# Windows
sc.exe stop IogridDaemon && sc.exe start IogridDaemon
```

The provider auto-marks itself offline in vpn-svc 90s after the last
health POST stops arriving — customers connecting in that window get
the next healthy provider in the region (or 503 if none).

---

## Reference

- `daemon/crates/core/src/main.rs` — CLI flags + supervisor entrypoint
- `daemon/crates/core/src/vpn_wiring.rs` — vpn-svc + health + ice + binder wire-up
- `daemon/crates/routing/src/ice.rs` — host + srflx candidate discovery, STUN client
- `daemon/crates/routing/src/peer_binder.rs` — assigned-session poll loop
- `daemon/crates/routing/src/boringtun_impl.rs` — WG userspace stack
