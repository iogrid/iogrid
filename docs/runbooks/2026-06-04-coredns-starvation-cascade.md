# Incident: CoreDNS starvation cascade → multi-service prod outage (2026-06-04 ~09:30Z)

**Severity:** P1 — web + authenticated API down ~30+ min.
**Tracking:** #691. Root constraint: #682 (110-pod cap on the single node).

## Trigger chain (each link is a #682 consequence)
1. **CoreDNS replacement stuck `Pending` at the 110-pod cap** — one CoreDNS pod
   was `Terminating` (79 restarts), its replacement couldn't schedule → in-cluster
   DNS degraded.
2. DNS loss crashed every DNS-dependent control component:
   - **CNPG operator** lost its leader lease (couldn't reach the API-server service
     path) → crashlooped → couldn't promote the pg primary → `iogrid-pg-rw`
     endpoint went **empty** → authenticated API `503`.
   - **harbor-core + harbor-jobservice** CrashLoopBackOff → registry `503` → web
     pods `ImagePullBackOff` → web `503`.
   - **metrics-server** down (`Metrics API not available`).
   - **traefik** readiness `/ping` (kubelet→pod-IP) timing out → edge `000`.
3. The **node data-path wedge** (kubelet→pod + pod→ClusterIP degraded while the
   API-server tunnel/`kubectl` still works) — same signature as the earlier
   2026-06-04 edge incident — kept the operator from holding its lease even after
   CoreDNS recovered (`Failed to update lock … 10.43.0.1:443`).

## Recovery actions taken (in-cluster, non-gated)
- Freed slots (`releases`/`admin`/`build-gateway` → 0) so the Pending CoreDNS could
  schedule → DNS resolving again (`iogrid-pg-rw` → 10.43.122.43 verified).
- CNPG operator: `priorityClassName: system-cluster-critical` + `strategy: Recreate`
  (it was **surge-deadlocked** at the cap — maxSurge needs a new pod before killing
  the old, impossible at the cap) → operator back to `1/1 Running`.
- Deleted crashlooping harbor-core/jobservice + ImagePullBackOff web pods to force
  clean reconnect/re-pull once DNS was back.
- pg data is **safe throughout** — single instance PVC-backed, `pg_isready` ✓ the
  whole time; only the CNPG-managed endpoint was missing.

## The gated residual (escalated: push + #691)
The node service-network/data-path wedge (kube-proxy/CNI) is **node-level** — the
operator can't hold its API-server lease until it clears. The fix if it does NOT
self-heal (it self-healed in ~8 min in the earlier incident) is a kube-proxy/k3s
networking restart **on the prod node** = the operator-gated action (no solo
control-plane restart on the single node).

## Prevention (raises the priority of the #682 ceiling decision)
1. **CoreDNS, the CNPG operator, and traefik must never be cap-preemptible.** CoreDNS
   already runs `system-cluster-critical`; its *replacement* still couldn't schedule
   because priority can't preempt for "Too many pods" — **only headroom helps**.
   → The cap raise (max-pods runbook) or node 2 is now a RELIABILITY requirement,
   not a convenience: a single DNS blip at the cap cascades to a full outage AND the
   cluster can't self-heal because recovery pods need headroom the cap denies.
2. Operator deployments cap-pinned with `maxSurge:1` self-deadlock — prefer
   `Recreate` for single-replica control components.
3. Harbor-behind-the-edge means any pull during an edge blip strands pods
   (documented pull-loop) — a node-local pull-through cache would break the loop.
