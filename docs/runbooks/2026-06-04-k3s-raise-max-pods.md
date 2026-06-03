# 2026-06-04 — Raise the k3s kubelet pod cap (110 → 160) on the single prod node

> Operator runbook for the **structural half of [#682](https://github.com/iogrid/iogrid/issues/682)**: the prod node `vmi3116389` runs at ~108 of its **110 allocatable pods** (the k3s/kubelet default). At that ceiling, scheduling fails with `Too many pods`, rollouts deadlock (a surge pod can never schedule), and HPAs that legitimately want more replicas (antiabuse was CPU-saturated at 168% of target wanting 3) silently can't scale.
>
> **This runbook is prepared but NOT executed** — it requires a `systemctl restart k3s` on the **single production node**, which restarts the control plane (~10–30 s of API-server unavailability). Running workloads keep serving through the restart (containerd keeps containers up; pods do **not** restart), but it is an operator-gated action per the no-solo-control-plane-restarts rule. The durable alternative is a **second node** (also fixes memory pressure + the single-node SPOF — see #682 option 1 / #652).

## Pre-flight (from the bastion)

```bash
kubectl get node vmi3116389 -o jsonpath='{.status.allocatable.pods}'   # expect 110
kubectl get pods -A --field-selector=status.phase=Running --no-headers | wc -l   # ~108 = at the wall
curl -s -o /dev/null -w '%{http_code}\n' https://iogrid.org    # 200 before touching anything
```

## Change (ON the cluster node `vmi3116389`, via SSH)

1. Edit (or create) `/etc/rancher/k3s/config.yaml` and add the kubelet arg:

   ```yaml
   kubelet-arg:
     - "max-pods=160"
   ```

   *(160 is safe for the pod network: k3s' default per-node CIDR is a `/24` → 254 pod IPs. Memory remains the next ceiling — see #682; this only removes the artificial pod-count wall.)*

2. Restart k3s (THE control-plane blip — ~10–30 s):

   ```bash
   sudo systemctl restart k3s
   ```

3. Verify from the bastion:

   ```bash
   # kubelet re-registered with the new cap
   kubectl get node vmi3116389 -o jsonpath='{.status.allocatable.pods}'   # expect 160
   # control plane healthy + nothing restarted
   kubectl get pods -A --no-headers | awk '$4>0 {print}' | head            # no new restart counts
   curl -s -o /dev/null -w 'edge %{http_code}\n' https://iogrid.org
   curl -s -o /dev/null -w 'api  %{http_code}\n' https://api.iogrid.org/healthz
   ```

4. Expected immediate effects: the Pending pods blocked on `Too many pods` (e.g. antiabuse's HPA-requested replicas, web's second replica) schedule within a minute; rollout surges work again without the in-place workarounds.

## Rollback

Remove the `kubelet-arg` block (or set it back) and `sudo systemctl restart k3s` again. Pods over 110 keep running (the cap gates *scheduling*, not running pods); the scheduler simply stops admitting new ones.

## Why not just do it from the bastion?

The bastion only holds the kubeconfig — k3s runs on `vmi3116389` (`systemctl is-active k3s` on the bastion: `inactive`). The change is a file edit + service restart on the node itself.

## Context / history

- #682 — node saturation root-cause (110-pod cap, not memory; memory was right-sized separately in `41513d0`).
- #664 — the CPU-requests flavor of the same over-provisioning class.
- The 2026-06-04 incident note on #682 (gateway-bff blip during simultaneous rolls) is exactly the failure mode this cap forces — read it before attempting multi-service rolls at the ceiling.
