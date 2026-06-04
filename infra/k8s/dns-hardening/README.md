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

1. **Raise CoreDNS to 2 replicas + loosen the liveness probe** — **VERIFIED
   2026-06-04 against the live cluster (k3s v1.34.4):** CoreDNS here is a
   **k3s AddOn**, NOT a HelmChart. `kubectl get helmchart -A` shows only
   `traefik`/`traefik-crd`; the `coredns` Deployment's owner is
   `objectset.rio.cattle.io/owner-gvk: k3s.cattle.io/v1, Kind=Addon,
   name=coredns`. So a `HelmChartConfig` would **no-op** (there is no
   coredns chart to override — that earlier instruction was wrong).

   The change goes in the **AddOn source manifest on the node** (node-access,
   operator-gated — no solo control-plane changes); the k3s AddOn controller
   reconciles it within seconds and survives restarts (a bare `kubectl
   scale`/`patch` gets reverted by the controller):

   ```bash
   # On the node, edit the k3s-managed CoreDNS AddOn manifest:
   #   /var/lib/rancher/k3s/server/manifests/coredns.yaml
   # In the CoreDNS Deployment spec, set:
   #   spec.replicas: 2
   #   spec.template.spec.containers[name=coredns].livenessProbe.timeoutSeconds: 5   # was 1
   #   spec.template.spec.containers[name=coredns].livenessProbe.failureThreshold: 5  # was 3
   # Save → the AddOn controller re-applies it automatically.
   ```

   (Confirmed live: `kubectl get deploy coredns -n kube-system` → `replicas=1`;
   probe today is `timeout=1s period=10s failure=3` — the cause of the
   restart loop under #682 pressure.)

2. **Apply the PodDisruptionBudget** — `kubectl apply -f coredns-pdb.yaml`.
   Keeps ≥1 CoreDNS pod through any voluntary drain once there are 2.

3. **Verify:** `kubectl -n kube-system get pods -l k8s-app=kube-dns` shows
   2/2 Ready on (ideally) different nodes once node-2 lands; `kubectl -n
   kube-system get pdb coredns-availability` shows `ALLOWED DISRUPTIONS 1`.

## Why CoreDNS keeps restarting (diagnosed 2026-06-04) — a third #682 symptom

The lone CoreDNS pod has 17 restarts (latest 13:26Z, ongoing). Cause is
**not** OOM — it's the **liveness probe timing out under node pressure**:
`kubectl describe` shows `Killing … failed liveness probe` (×7 over 3h26m)
and `Unhealthy … /health … context deadline exceeded` (×110). The probe is
`GET /health timeout=1s period=10s #failure=3`; on the saturated node
(#682) CoreDNS is CPU-starved badly enough that `/health` can't answer
within **1 second**, so after 3 misses kubelet kills + restarts it — which
is itself a DNS blip, and at the cap a failed reschedule re-arms #691.

**Hardening (operator — folded into step 1's AddOn-manifest edit):** loosen
the probe so transient starvation doesn't kill a healthy CoreDNS —
`livenessProbe.timeoutSeconds 1→5` + `failureThreshold 3→5` in the CoreDNS
Deployment inside `/var/lib/rancher/k3s/server/manifests/coredns.yaml` (NOT
a one-off `kubectl patch` — the k3s AddOn controller reverts anything not in
that manifest).
This is a band-aid over the real fix (the #682 cap raise removes the CPU
starvation). Both: the cap raise stops the starvation, the probe loosening
+ 2nd replica + PDB make any residual blip survivable.

## Status

- `coredns-pdb.yaml` — ready; inert on the current single replica, activates at 2.
- Replica raise — **#682-gated** (needs a pod slot + node access).
- CNPG-operator priority + `Recreate` (the other #692 item) — already persisted
  in openova-private (`cnpg` HelmRelease, PR #783, merged).
