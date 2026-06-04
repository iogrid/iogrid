# CoreDNS-SPOF hardening (config-as-code) — #692

Persisted artifacts + procedure for the CoreDNS high-availability hardening
that the #691 incident proved necessary. **Nothing here is auto-applied** —
this directory is deliberately *not* referenced by any kustomization so it
can never be swept into the banned `kubectl apply -k overlays/prod` path
(which crashlooped prod once; see CLAUDE.md). Apply manually, in order,
**after #682 raises the node's max-pods** (every step needs the headroom
the cap currently denies).

Reference runbook: [`docs/runbooks/2026-06-04-coredns-starvation-cascade.md`](../../../docs/runbooks/2026-06-04-coredns-starvation-cascade.md).

## Why (the #691 chain, root-caused)

One CoreDNS pod was disrupted; its replacement was stuck `Pending` at the
110-pod cap → in-cluster DNS degraded → the CNPG operator lost its leader
lease (couldn't reach the API-server service IP) → couldn't promote pg →
`iogrid-pg-rw` endpoint empty → authenticated API `503`; harbor + traefik
cascaded too. **Root constraint: #682** (single node at the pod cap, no
headroom for recovery pods). CoreDNS redundancy removes the single point of
failure so one pod-death is survivable.

## Apply order (operator, post-#682)

1. **Raise CoreDNS to 2 replicas** — k3s manages CoreDNS via its bundled
   AddOn, so this is a **node-access** step (operator-gated; no solo
   control-plane changes). Create a `HelmChartConfig` on the node so k3s'
   helm-controller reconciles it (survives k3s restarts, unlike a manual
   `kubectl scale`):

   ```yaml
   # /var/lib/rancher/k3s/server/manifests/coredns-ha.yaml  (on the node)
   apiVersion: helm.cattle.io/v1
   kind: HelmChartConfig
   metadata:
     name: coredns
     namespace: kube-system
   valuesContent: |-
     replicas: 2
   ```

   (Confirm the value key against the running chart first:
   `kubectl -n kube-system get helmchart coredns -o yaml` — older k3s use
   `replicas`, some bundles `replicaCount`. Adjust before writing the file.)

2. **Apply the PodDisruptionBudget** — `kubectl apply -f coredns-pdb.yaml`.
   Keeps ≥1 CoreDNS pod through any voluntary drain once there are 2.

3. **Verify:** `kubectl -n kube-system get pods -l k8s-app=kube-dns` shows
   2/2 Ready on (ideally) different nodes once node-2 lands; `kubectl -n
   kube-system get pdb coredns-availability` shows `ALLOWED DISRUPTIONS 1`.

## Status

- `coredns-pdb.yaml` — ready; inert on the current single replica, activates at 2.
- Replica raise — **#682-gated** (needs a pod slot + node access).
- CNPG-operator priority + `Recreate` (the other #692 item) — already persisted
  in openova-private (`cnpg` HelmRelease, PR #783, merged).
