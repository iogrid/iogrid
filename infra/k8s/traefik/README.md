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
