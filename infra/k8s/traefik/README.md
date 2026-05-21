# Traefik Phase 0 shim

The iogrid.org public surface is fronted by the OpenOva mothership's
Traefik instance (in `kube-system`) — see `docs/PHASE0-UNBLOCK.md`. The
canonical routing intent lives in `infra/k8s/gateways/` (Cilium Gateway
+ HTTPRoute), but until the mothership migrates to Cilium Gateway we
translate that intent into Traefik `IngressRoute` objects here.

This directory is **applied directly to the iogrid namespace** of the
mothership cluster (not via Flux) so the manifests can be cherry-picked
quickly without a reconcile loop. A follow-up PR wires them into the
Flux Kustomization once the dispatch chain is stable.

## What's in here

| File | Purpose |
|---|---|
| `serverstransport-long-lived.yaml` | Disables Traefik→backend idle-conn / read / write timeouts so Connect-RPC bidi streams (`WorkloadDispatchService.Dispatch`) can stay open for the lifetime of a paired daemon (minutes-to-hours). Also enables h2 PING keepalive on the Traefik→backend leg. #271 |
| `ingressroute-workloads.yaml` | `api.iogrid.org/iogrid.workloads.v1*` → `workloads-svc:8080` (h2c). References the long-lived `ServersTransport` and sets `responseForwarding.flushInterval: 100ms` so server-sent frames (CoordinatorHello, Assignment, TunnelData) reach the daemon within ~100ms instead of being buffered to Traefik's default 1s mark. |
| `ingressroutetcp-proxy-passthrough.yaml` | `proxy.iogrid.org:443` → `proxy-gateway:443` (TLS passthrough, NOT termination). proxy-gateway speaks SOCKS5-over-TLS; Traefik must NOT terminate TLS at the edge or the SOCKS5 framing gets parsed as HTTP and the customer connection hangs. The Cilium Gateway form of the same intent already lives in `infra/k8s/gateways/tlsroute-proxy.yaml`; this IngressRouteTCP is the live Traefik materialisation until the mothership cuts over. #350 |

## Why these exist (issue #271)

After #260 (Service `appProtocol: kubernetes.io/h2c`) and #261 (server
`h2c.NewHandler`) shipped, the daemon's TLS handshake to the coordinator
succeeded but the bidi `Dispatch` stream still died within ~5-10 seconds
before the `DaemonHello` frame reached `workloads-svc`. The investigation
in #271 traced this to Traefik's default `ServersTransport` settings:

* `forwardingTimeouts.idleConnTimeout: 90s` — pool-level idle close
* `forwardingTimeouts.readIdleTimeout: 0s` — but Go's HTTP/2 transport
  may still h2-PING and reset on missed pongs
* No explicit `responseForwarding.flushInterval` — defaults to 1s for
  non-SSE responses, which holds the `CoordinatorHello` frame in the
  proxy's write buffer past the daemon's 10s ack timeout

The `ServersTransport` here disables those timeouts and the `Middleware`
forces an immediate flush on every server-sent frame. With both applied
the daemon stays connected for the lifetime of `iogridd` and
`DaemonHello` is registered in the dispatcher on the first attempt.

## Apply

```
kubectl -n iogrid apply -f infra/k8s/traefik/
```

The objects are idempotent; re-applying is safe.

## Mothership Traefik static config — `forwardedHeaders.insecure: true` (#381)

The dynamic objects in this directory route traffic; they do not
configure the underlying Traefik entrypoint. Two pieces of static
config on the **mothership** Traefik Helm release are required for
iogrid to function correctly end-to-end:

| Static knob | Why we need it | Symptom when missing |
|---|---|---|
| `ports.websecure.http2.enabled: true` | h2c bidi streams (`WorkloadDispatchService.Dispatch`, `SchedulingService.StreamHeartbeats`) | Daemon sees `HTTP 505 / grpc-status header missing` on stream open. Surfaced in #271. |
| `ports.websecure.forwardedHeaders.insecure: true` | `X-Forwarded-For` actually reaches providers-svc so the GeoIP2 path (PR #378) can resolve the provider's country + region | `providers.country_code / region_name / public_ip` columns stay NULL despite a healthy heartbeat stream. Diagnosed in #381 (PR #378 shipped the lookup code but the geo cells never populated for hatice's daemon on 2026-05-21). |

The `forwardedHeaders.insecure: true` knob accepts XFF/Forwarded
headers from any upstream hop. This is safe on the iogrid Phase-0
topology because:

1. The only thing in front of mothership Traefik is a **Hetzner L4
   load balancer** (TCP-mode, no proxy-protocol). Clients cannot
   directly inject XFF — their TLS connection terminates at Traefik,
   and Traefik unconditionally overwrites XFF with the connection's
   `RemoteAddr` (the LB's outbound IP, which is itself rewritten by
   the LB to the real client's IP).
2. The dameon's mTLS chain authenticates the daemon to the
   coordinator at the application layer; even if a malicious client
   somehow injected an XFF, it could only forge the **geo** cells (not
   identity) and the daemon's mTLS cert is the authoritative provider
   identity.

If/when the platform puts a CDN (Cloudflare, Fastly) in front of the
LB, swap `insecure: true` for `trustedIPs: [<LB CIDR>, <CDN CIDR>]` so
only those hops can populate XFF — clients still cannot forge it
because they terminate TLS at the CDN edge first.

Apply via:

```bash
helm upgrade traefik traefik/traefik -n kube-system --reuse-values \
  --set ports.websecure.forwardedHeaders.insecure=true
```

Verify post-roll with a sample `kubectl -n iogrid logs deploy/providers-svc` —
every `heartbeat: stream opened` line should now carry a non-empty
`client_ip=<public IP>` instead of `client_ip=`. The corresponding
provider row's geo columns populate within ≤ 24h (immediately on the
first heartbeat of a fresh stream, per the LastGeoLookupAt-zero
short-circuit in `scheduling.go`).
