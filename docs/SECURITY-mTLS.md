# Service-to-Service mTLS — SPIFFE-style Identities via Cilium

> Status: shipped under issue [#35](https://github.com/iogrid/iogrid/issues/35). Cilium 1.14+ mutual auth + SPIRE-backed workload identities. The plain Kubernetes NetworkPolicy ships in parallel as L3/L4 defense-in-depth.

This document covers the identity model, where the policies live in the
tree, how mutual auth is enforced on the wire, and how to debug the
common failure modes with `cilium-cli` / `kubectl`.

## Identity model

Every iogrid microservice runs under a Kubernetes ServiceAccount whose
name matches the service:

| Service          | ServiceAccount    | SPIFFE ID                                              |
|------------------|-------------------|--------------------------------------------------------|
| identity-svc     | `identity-svc`    | `spiffe://iogrid/ns/iogrid/sa/identity-svc`            |
| providers-svc    | `providers-svc`   | `spiffe://iogrid/ns/iogrid/sa/providers-svc`           |
| workloads-svc    | `workloads-svc`   | `spiffe://iogrid/ns/iogrid/sa/workloads-svc`           |
| antiabuse-svc    | `antiabuse-svc`   | `spiffe://iogrid/ns/iogrid/sa/antiabuse-svc`           |
| billing-svc      | `billing-svc`     | `spiffe://iogrid/ns/iogrid/sa/billing-svc`             |
| telemetry-svc    | `telemetry-svc`   | `spiffe://iogrid/ns/iogrid/sa/telemetry-svc`           |
| gateway-bff      | `gateway-bff`     | `spiffe://iogrid/ns/iogrid/sa/gateway-bff`             |
| proxy-gateway    | `proxy-gateway`   | `spiffe://iogrid/ns/iogrid/sa/proxy-gateway`           |
| vpn-gateway      | `vpn-gateway`     | `spiffe://iogrid/ns/iogrid/sa/vpn-gateway`             |
| build-gateway    | `build-gateway`   | `spiffe://iogrid/ns/iogrid/sa/build-gateway`           |
| web              | `web`             | `spiffe://iogrid/ns/iogrid/sa/web`                     |

The trust domain is `iogrid`. SPIFFE IDs are minted by the Cilium
SPIRE server (`cilium-spire-server`, bootstrapped by the iogrid-ops
Helm chart) with the format:

```
spiffe://<trust-domain>/ns/<namespace>/sa/<serviceAccount>
```

This is the **default Cilium 1.14 layout** — we do not bake any custom
SPIRE registration entries. Each Cilium agent's per-node SPIRE agent
auto-discovers pods, asks the kube-apiserver for the pod's SA, and
mints a workload SVID with the corresponding SPIFFE ID. Cilium's
`mutual-auth-spiffe-enabled: true` agent flag wires the data path so
the SPIFFE handshake happens transparently before the L7 path opens
on selected ports.

## How the policy enforces it

Each microservice ships two policy resources side-by-side:

1. `infra/k8s/base/<svc>/networkpolicy.yaml` — plain
   `networking.k8s.io/v1 NetworkPolicy`. L3/L4 only (port + IP/pod
   selectors). Defense-in-depth: still enforced even if Cilium is
   downgraded or the mutual-auth feature is toggled off.
2. `infra/k8s/base/<svc>/ciliumnetworkpolicy.yaml` — `cilium.io/v2
   CiliumNetworkPolicy`. Identity-aware (pod labels →
   serviceAccount → SPIFFE ID) with `authentication.mode: required`
   on every intra-mesh ingress rule.

Example ingress rule from `identity-svc/ciliumnetworkpolicy.yaml`:

```yaml
ingress:
  - fromEndpoints:
      - matchLabels:
          app.kubernetes.io/name: gateway-bff
          io.kubernetes.pod.namespace: iogrid
    authentication:
      mode: required
    toPorts:
      - ports:
          - port: "8080"
            protocol: TCP
```

`fromEndpoints` matches the source pod by its labels (which in turn
identify the source ServiceAccount, by Cilium's identity derivation).
`authentication.mode: required` instructs the Cilium datapath to
complete a SPIRE-backed mTLS handshake before allowing the L7 (gRPC
:8080) path to open. If the source pod has no SPIFFE identity (e.g.
not running under a SA, or running in a different trust domain), the
connection is **dropped pre-handshake**, not refused at L7.

### Trust-domain boundaries — when SPIFFE is NOT required

Three ingress hops intentionally stay un-authenticated, because the
source isn't a SPIFFE workload:

1. **Public Gateway → BFF/proxy-gateway/build-gateway/vpn-gateway** —
   the source is an end-user; the Gateway terminates customer TLS
   here, no SPIFFE workload exists upstream.
2. **monitoring namespace → :9090 /metrics scrape** — the Prometheus
   pod lives in the mothership monitoring trust domain, not the
   iogrid SPIFFE trust domain. We don't gate observability on
   mutual auth (would couple two trust domains we want decoupled).
3. **OTLP receivers (telemetry-svc :4317/:4318)** — any iogrid pod
   can emit telemetry; we don't want a newborn pod that hasn't yet
   negotiated a SPIRE SVID to drop spans during boot.

For these hops, only the plain `NetworkPolicy` enforces (L3/L4
allow-list).

## Cluster bootstrap — enabling mutual auth

The cluster-wide feature flag lives in
`infra/k8s/base/cilium-mutual-auth-feature.yaml` — a ConfigMap in
`kube-system` that the Cilium Helm chart consumes via `extraConfigMap`.
The mothership iogrid-ops repo references it from its
`apps/cilium/values.yaml`. After the chart upgrade rolls all Cilium
agents, the per-node SPIRE agent picks up the new flag and starts
issuing SVIDs for every iogrid pod automatically.

The ConfigMap is intentionally **not** wired into `infra/k8s/base/
kustomization.yaml` — the base kustomization rewrites every
resource's namespace to `iogrid`, which would mangle the
`kube-system` placement. The file ships as a reference manifest
applied by Flux from `iogrid-ops`.

To verify the flag took effect:

```bash
kubectl -n kube-system get cm cilium-config -o yaml \
  | grep -E '(mutual-auth-spiffe-enabled|spire-server-address|mesh-auth-spiffe-trust-domain)'
```

Expected:

```yaml
mutual-auth-spiffe-enabled: "true"
spire-server-address: "spire-server.cilium-spire.svc:8081"
mesh-auth-spiffe-trust-domain: "iogrid"
```

## Debugging with cilium-cli

### Verify mutual auth is enabled cluster-wide

```bash
cilium config view | grep mutual-auth
# mutual-auth-spiffe-enabled    true
```

### Verify a pod has an SVID

```bash
POD=$(kubectl -n iogrid get pod -l app.kubernetes.io/name=identity-svc -o name | head -1)
cilium identity get $(kubectl get $POD -n iogrid -o jsonpath='{.metadata.labels.io\.cilium\.k8s\.policy\.serviceaccount}')
```

Or directly query the SPIRE agent socket from inside the pod:

```bash
kubectl exec -n iogrid $POD -- /bin/sh -c \
  '/usr/local/bin/spire-agent api fetch x509 -socketPath /run/spire/sockets/agent.sock'
```

You should see one SVID with URI SAN
`spiffe://iogrid/ns/iogrid/sa/identity-svc`.

### Watch a denied handshake

The Cilium agent emits Hubble flow events for SPIFFE auth failures.
The key field is `auth_type: spire` with `verdict: DROPPED`:

```bash
hubble observe --type drop --label app.kubernetes.io/name=identity-svc \
  --since 5m --output jsonpb \
  | jq 'select(.flow.auth_type == "spire" and .flow.verdict == "DROPPED")'
```

### Force a handshake from a peer

```bash
SOURCE_POD=$(kubectl -n iogrid get pod -l app.kubernetes.io/name=gateway-bff -o name | head -1)
kubectl exec -n iogrid $SOURCE_POD -- \
  grpcurl -plaintext identity-svc.iogrid.svc.cluster.local:8080 \
  iogrid.identity.v1.IdentityService/Health
```

If mutual auth is healthy this returns `{"status": "OK"}`. If the
SPIRE registration for one side hasn't propagated yet, the call
hangs for `mesh-auth-mutual-connect-timeout` (5s by default — see
the ConfigMap) and then returns `context deadline exceeded`.

### Common failure: SA mismatch

If a pod is rolled but the operator forgot to bind the deployment to
its named ServiceAccount, Cilium derives the SPIFFE ID from the
fall-back `default` SA, which has no CiliumNetworkPolicy match.
Symptom: ALL outbound calls from that pod start failing with
`context deadline exceeded` (timeout from the SPIFFE handshake).
Fix: ensure `deployment.spec.template.spec.serviceAccountName` is
set to the per-service SA (`identity-svc`, `gateway-bff`, ...).

## Rollout posture

* `networkPolicy.mutualAuth.enabled` in the coordinator chart
  (`coordinator/charts/iogrid/values.yaml`) defaults to `false` so
  the chart renders cleanly on clusters without Cilium (kindnet dev
  overlay).
* The kustomize-based GitOps layer (`infra/k8s/base/`) ships the
  CiliumNetworkPolicy resources unconditionally. On dev (kindnet)
  the CRDs aren't installed and the manifest application is
  skipped by Flux's `CustomizationKind: Kustomize` controller after
  one warn-log. On prod (Cilium), every CNP applies and enforces.

## Future work

* Wire the daemon ↔ providers-svc long-lived gRPC stream through the
  same SPIFFE path. Today the daemon authenticates with a one-time
  pairing PIN and a long-lived bearer token. Migrating to SPIFFE
  requires the daemon to run as a SPIRE-attested workload, which
  needs per-provider SPIRE node attestation — designed but not yet
  shipped (issue follow-up TBD).
* Apply `CiliumClusterwideNetworkPolicy` for cross-namespace gates
  (e.g. iogrid → gateway-system → iogrid) when we adopt Cilium
  ClusterMesh in the multi-region phase.
* Cross-reference the Hubble L7-flow dashboards in
  `infra/k8s/base/telemetry-svc/assets/dashboards/` once we publish
  the `iogrid-mtls` Grafana panel.
