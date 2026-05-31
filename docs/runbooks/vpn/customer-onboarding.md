# Customer VPN onboarding — operator + end-user runbook

> Source: live verification 2026-06-01 by iogrid-lead. Pairs with
> `e2e/smoke/vpn-customer-connect.sh` (control plane) and
> `.github/workflows/vpn-e2e-smoke.yml` (data plane).

The README's quickstart promises:

```bash
curl -fsSL https://iogrid.org/install-cli.sh | sh
iogrid login --api-key=iog_YOUR_KEY --customer-id=YOUR_ID
iogrid vpn regions
iogrid vpn connect --region us-east-1
curl ifconfig.me      # exit IP changed
```

This runbook documents what each step actually does in production, what
fails when each step is broken, and how operators fix it.

---

## Step 1 — install-cli.sh

**URL:** `https://iogrid.org/install-cli.sh` (200 OK, ~2 KB POSIX sh)

**What it does:** detects OS+arch, fetches the matching binary from
`https://github.com/iogrid/iogrid/releases/latest/download/iogrid-<os>-<arch>`,
chmod +x, drops it in `/usr/local/bin/iogrid` (or `~/.local/bin/iogrid`
without sudo), then prints the next-steps banner.

**Common failures**

| Symptom | Likely cause | Fix |
|---|---|---|
| `HTTP/2 404` on iogrid.org/install-cli.sh | Web pod stale, doesn't have `web/public/install-cli.sh` baked in | Roll the web Deployment to latest digest (`kubectl -n iogrid set image deploy/web web=harbor.openova.io/iogrid/web@sha256:<latest>`); see #558 closure |
| GitHub release returns 404 | `latest` tag pointing at a release without that arch asset | Re-tag a new `v0.x.y-cli` release; cli-ci.yml auto-uploads all 5 binaries (linux/darwin × amd64/arm64 + windows) |
| Binary banner says "Phase-1" | Binary was built before the wording cleanup | Cut a new `v0.x.y-cli` release; #557 closure shipped `v0.1.1-cli` |

---

## Step 2 — iogrid login

**Endpoint touched:** none on login itself (writes
`~/.config/iogrid/credentials.json` locally). On the FIRST authenticated
RPC the API key is sent as bearer token to billing-svc.ValidateApiKey
via vpn-svc.

**Where to get the key:** https://iogrid.org/customer/vpn — sign in,
click "Mint VPN key", copy `iog_…` plus the displayed customer UUID.

**Common failures**

| Symptom | Likely cause | Fix |
|---|---|---|
| `ERROR: --api-key and --customer-id are required` | Both flags missing | Re-run with `--api-key` AND `--customer-id` |
| `vpn connect: api key rejected` | Key revoked, never minted, or vpn-svc auth mis-wired | (a) Mint a fresh key at iogrid.org/customer/vpn. (b) Check `kubectl -n iogrid logs deploy/vpn-svc | grep "api key validation"` — must say "enabled", not "disabled". If disabled, BILLING_SVC_URL env was stripped (see #561). |

---

## Step 3 — iogrid vpn regions

**Endpoint:** unauthenticated `GET https://api.iogrid.org/v1/vpn/regions`.

**Expected output:** one row per region with `healthy_providers` ≥ 1.

| Symptom | Cause | Fix |
|---|---|---|
| `count: 0` for `us-east-1` | No paired daemon registered + healthy in region | Pair a daemon on a public IP (see `docs/runbooks/vpn/operator-paired-daemon.md` — to be written) |
| HTTP error connecting | api.iogrid.org down or Traefik IngressRoute lost | Check `kubectl -n iogrid get pods -l app.kubernetes.io/name=vpn-svc` + `kubectl -n traefik get ingressroute` |

---

## Step 4 — iogrid vpn connect

This is the long step. On success the CLI prints:

```
Connecting to iogrid VPN (region=us-east-1, coordinator=https://api.iogrid.org)...
Requesting session from Coordinator...
Session created: <uuid>
Creating WireGuard interface...
Customer WG pubkey posted; waiting for provider binding...
Got N ICE candidates + provider pubkey from provider
Picked candidate: <ip>:<port> (type=host)
Configuring WireGuard peer...
Bringing up WireGuard interface...
Confirming working candidate to Coordinator...
✓ VPN tunnel established successfully!
```

**Required local capabilities:**

```bash
sudo setcap cap_net_admin+eip "$(command -v iogrid)"   # Linux
# macOS: no setcap; utun creation works as the invoking user
# Windows: needs WinTun installed (bundled by the MSI)
```

**Common failures**

| Symptom | Cause | Fix |
|---|---|---|
| `WireGuard interface creation needs CAP_NET_ADMIN` | Capabilities missing on the binary | Run the `setcap` above |
| `timeout waiting for provider binding (provider_wg_public_key="", candidates=N)` | Daemon's peer-binder loop isn't seeing the session or can't post back its WG pubkey | Check `journalctl -u iogridd -n 50` on the daemon host for `"upsert peer failed"` (#536). On the cluster side, check `vpn-svc` postgres SELECT includes `provider_wg_public_key` cols (#536 fix in ead6581) |
| `confirm candidate: unexpected status 500` | SDK posted wrong field names OR daemon registered private-only candidates | (1) Bump SDK to v0.1.1-cli+ (e0cbb2b fixed JSON tag mismatch). (2) Restart daemon with `--public-ip <reachable-ip>` (#557) |
| `✓ established` but no traffic flows | Data plane gap — provider daemon isn't NAT-forwarding inner packets | Track #529 path c (Linux TUN + iptables MASQUERADE in daemon) |

---

## Step 5 — curl ifconfig.me (exit-IP assertion)

**Expected:** the IP returned must DIFFER from your machine's baseline
exit IP. Identical IP means the tunnel handshake succeeded but the
provider isn't forwarding bytes — track #529.

**CI verification:** `.github/workflows/vpn-e2e-smoke.yml` runs this
every 3 hours against the live deployment + a long-lived test customer.
Workflow goes green automatically when #529 ships.

---

## Operator quick reference

| Action | Command |
|---|---|
| Roll vpn-svc to git-pinned image | `kubectl -n iogrid apply -f infra/k8s/base/vpn-svc/deployment.yaml` |
| Force re-pull (image digest unchanged) | `kubectl -n iogrid rollout restart deploy/vpn-svc` |
| Tail vpn-svc API-key + binder logs | `kubectl -n iogrid logs -f deploy/vpn-svc \| grep -E "api key\|bind"` |
| Inspect assigned sessions for a provider | `curl https://api.iogrid.org/v1/vpn/providers/<provider-uuid>/assigned-sessions` |
| Inspect provider ICE candidates | `curl https://api.iogrid.org/v1/vpn/providers/<provider-uuid>/candidates` |
| Restart paired-provider daemon | `sudo systemctl restart iogridd` (Linux) / `launchctl kickstart -k gui/$(id -u)/io.iogrid.daemon` (macOS) |

---

## Reference

- `cmd/iogrid/main.go` — CLI source
- `sdks/go/vpn/bastion_client.go` — SDK Connect flow
- `coordinator/services/vpn-svc/internal/server/handlers.go` — coordinator HTTP surface
- `daemon/crates/core/src/vpn_wiring.rs` — provider daemon supervisor wire-up
- `daemon/crates/routing/src/peer_binder.rs` — daemon's session bind loop
- `infra/k8s/base/vpn-svc/deployment.yaml` — k8s manifest (pinned to harbor.openova.io per #561)
