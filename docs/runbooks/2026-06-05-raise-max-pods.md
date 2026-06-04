# Runbook — raise the node pod-count cap (#682)

The single node `vmi3116389` runs at `capacity.pods=110` and is at it (~112
scheduled). The trim analysis on #682 proved there is **no self-service slot to
free**: 0 completed/evicted pods to reap, every iogrid Deployment at 1 replica
except `web`=3 (memory-justified at 126% of its HPA target), and the over-claimed
HPA maxReplicas (10/20) are unreachable at the cap. Right-sizing resource
*requests* frees CPU/memory but **not pod slots** — the cap is a hard *count*.

This is operator-gated (no-solo-control-plane on the single prod node). Steps:

## Option A — raise max-pods (fast; gives slots if memory allows)

k3s sets the kubelet's `max-pods` via the server config. On the node:

```bash
# 1. add/raise the kubelet max-pods (k3s reads this file at start)
sudo tee -a /etc/rancher/k3s/config.yaml >/dev/null <<'YAML'
kubelet-arg:
  - "max-pods=200"
YAML
# 2. restart k3s (brief control-plane blip; data-plane pods keep running)
sudo systemctl restart k3s
# 3. verify
kubectl get node vmi3116389 -o jsonpath='{.status.capacity.pods}{"\n"}'   # expect 200
```

**Caveat — memory.** The node is also memory-pressured (`web` at 126%). More pod
slots only help if the node has RAM headroom for the new pods; otherwise pods
schedule then OOM. Check before raising:

```bash
kubectl describe node vmi3116389 | grep -A4 'Allocated resources'   # memory requests vs allocatable
free -h                                                              # actual free RAM on the node
```

If memory requests are already near allocatable, raising max-pods alone will not
help — go to Option B.

## Option B — add a second node (durable; gives slots AND memory)

Needs Hetzner (or other) creds — see #652-adjacent infra. Once a 2nd node joins,
the CoreDNS-HA hardening staged in `infra/k8s/dns-hardening/` (PDB + 2 replicas,
#692) can be applied so the #691 recovery-pod cascade can never recur.

## Why this matters (not just convenience)

The #691 outage was a *recovery* pod (CoreDNS) that couldn't reschedule at the
cap → DNS cascade → CNPG lease loss → API 503. Headroom is the resilience fix.
The `iogrid-serving` PriorityClass + the web PDB (`44bccb2f`) are already in place
as mitigations; this runbook is the structural fix.

## After raising

```bash
kubectl get pods -A --field-selector=status.phase=Pending   # any stuck pods now schedule
kubectl apply -f infra/k8s/dns-hardening/                    # CoreDNS HA, post-headroom (#692)
```
