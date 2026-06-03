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

These are the **live Traefik IngressRoutes/Middlewares/Certs captured from the
working prod runtime on 2026-06-03** (the routing intent `base` references when
the Flux unsuspend gates clear — see `infra/k8s/flux/README.md`).

| File | Purpose |
|---|---|
| `serverstransport-long-lived.yaml` | Disables Traefik→backend idle-conn / read / write timeouts so Connect-RPC bidi streams (`WorkloadDispatchService.Dispatch`) can stay open for the lifetime of a paired daemon (minutes-to-hours). Also enables h2 PING keepalive on the Traefik→backend leg. #271 |
| `ingressroutes.yaml` | All HTTP IngressRoutes. Highlights: `api.iogrid.org` → `gateway-bff` (with per-prefix routes to identity-svc / providers-svc / vpn-svc / workloads-svc, the last referencing the long-lived `ServersTransport` for the daemon dispatch stream); `iogrid.org` / `www.iogrid.org` apex → `web`; `admin.iogrid.org` → the **separate `admin` Service**; `releases.iogrid.org` → installer redirects; and `app.iogrid.org` → a `redirect-app-to-apex` middleware that **301s the dropped `app` subdomain to the `iogrid.org` apex**. |
| `middlewares.yaml` | CORS + the `redirect-app-to-apex` redirect + the `releases-*` installer redirects referenced by `ingressroutes.yaml`. |
| `certificates.yaml` | cert-manager `Certificate` objects for the iogrid hostnames. |
| `ingressroutetcp-proxy-passthrough.yaml` | `proxy.iogrid.org:443` → `proxy-gateway:443` (TLS passthrough, NOT termination). proxy-gateway speaks SOCKS5-over-TLS; Traefik must NOT terminate TLS at the edge or the SOCKS5 framing gets parsed as HTTP and the customer connection hangs. The Cilium Gateway form of the same intent already lives in `infra/k8s/gateways/tlsroute-proxy.yaml`; this IngressRouteTCP is the live Traefik materialisation until the mothership cuts over. #350 |

> **Domain model (canonical):** `iogrid.org` is the apex; `app.iogrid.org` has
> been **dropped** and 301-redirects to the apex; `admin.iogrid.org` is a
> **separate** Next.js app (its own `admin` Deployment + Service), NOT a Host
> alias of `web`.

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

## Mothership Traefik static config — XFF + source-IP preservation (#381)

The dynamic objects in this directory route traffic; they do not
configure the underlying Traefik entrypoint. Three pieces of static
config on the **mothership** Traefik Helm release are required for
iogrid to function correctly end-to-end:

| Static knob | Why we need it | Symptom when missing |
|---|---|---|
| `ports.websecure.http2.enabled: true` | h2c bidi streams (`WorkloadDispatchService.Dispatch`, `SchedulingService.StreamHeartbeats`) | Daemon sees `HTTP 505 / grpc-status header missing` on stream open. Surfaced in #271. |
| `ports.websecure.forwardedHeaders.insecure: true` | `X-Forwarded-For` actually reaches providers-svc so the GeoIP2 path (PR #378) can resolve the provider's country + region | `providers.country_code / region_name / public_ip` columns stay NULL despite a healthy heartbeat stream. Diagnosed in #381 (PR #378 shipped the lookup code but the geo cells never populated for hatice's daemon on 2026-05-21). |
| `service.spec.externalTrafficPolicy: Local` + `deployment.replicas: 2` | Hetzner LB forwards the **real client public IP** to Traefik instead of SNAT-rewriting it to the cluster gateway IP (`10.42.0.1`) | `providers.country_code` populates as a near-by-cluster country (or NULL when the GeoIP db has no entry for `10.42.0.1`) instead of the actual provider geolocation. Resolved in #381 (this PR) — once ETP=Local landed, the LB→Traefik hop preserved source IP, `XFF` was filled with the real public IP, and GeoIP resolved correctly. |

### Why ETP=Local is safe on a single-node Phase-0 mothership

Kubernetes `externalTrafficPolicy: Local` instructs the LoadBalancer
to ONLY route to nodes that have at least one healthy Pod for the
Service. On a single-node cluster all pods land on that one node so
the LB's health-check probes succeed for it; on a multi-node cluster
the LB's healthCheckNodePort gates per-node traffic correctly.

The `replicas: 2` bump exists so that during a Traefik pod rollout
(image bump, config change) one Pod stays Ready while the other
restarts — without it, the LB's per-node healthcheck flaps and traffic
briefly drops for every Traefik upgrade.

### Why XFF / `forwardedHeaders.insecure: true` is safe

The knob accepts XFF/Forwarded headers from any upstream hop. This is
safe on the iogrid Phase-0 topology because:

1. The only thing in front of mothership Traefik is a **Hetzner L4
   load balancer** (TCP-mode, no proxy-protocol). With ETP=Local the
   LB-forwarded source IP IS the real client IP. Clients cannot
   directly inject XFF — their TLS connection terminates at Traefik,
   and Traefik unconditionally overwrites XFF with the connection's
   `RemoteAddr` (now the real client IP, not the LB's).
2. The daemon's mTLS chain authenticates the daemon to the
   coordinator at the application layer; even if a malicious client
   somehow injected an XFF, it could only forge the **geo** cells (not
   identity) and the daemon's mTLS cert is the authoritative provider
   identity.

If/when the platform puts a CDN (Cloudflare, Fastly) in front of the
LB, swap `insecure: true` for `trustedIPs: [<LB CIDR>, <CDN CIDR>]` so
only those hops can populate XFF — clients still cannot forge it
because they terminate TLS at the CDN edge first.

### Apply

Because the mothership Traefik is managed by **k3s's HelmChart
controller** (the `HelmChart/traefik` CR in `kube-system` is the
durable source-of-truth), direct `helm upgrade` calls are not stable
across k3s restarts — the helm-controller will re-roll the chart with
whatever `spec.valuesContent` is in the CR. Always patch the
HelmChart CR `spec.valuesContent` to make changes durable; the
in-cluster Helm release is just a downstream materialisation.

The canonical fragment of `HelmChart/traefik.spec.valuesContent` that
satisfies all three knobs above:

```yaml
deployment:
  replicas: 2
ports:
  websecure:
    forwardedHeaders:
      insecure: true
service:
  spec:
    externalTrafficPolicy: Local
```

Verify post-roll with a sample `kubectl -n iogrid logs deploy/providers-svc` —
every `heartbeat: stream opened` line should now carry a non-empty
`client_ip=<real public IP>` (NOT `10.42.0.1` and NOT empty). The
corresponding provider row's geo columns populate within ≤ 24h
(immediately on the first heartbeat of a fresh stream, per the
`LastGeoLookupAt`-zero short-circuit in `scheduling.go`).

Reference live state (mothership chart revision 8, applied 2026-05-21
08:24:32Z): `provider-a7a93576` resolved `public_ip=188.66.253.46`,
`country_code=OM`, `region_name=Muscat` on its first reconnect
after the providers-svc rollout that followed the ETP=Local flip.
