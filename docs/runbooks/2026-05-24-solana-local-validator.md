# 2026-05-24 — Solana local validator for $GRID Phase-0

> Bypass the external-faucet bottleneck (issue #345) by running
> `solana-test-validator` as a StatefulSet inside the cluster. Faucet is
> unlimited, RPC is reachable on `solana-validator.iogrid.svc:8899`, and
> the $GRID SPL token is minted by a one-shot Job that persists the
> wallet + mint address into a Secret consumed by billing-svc.

## TL;DR — one command

```bash
kubectl apply -k infra/k8s/base/solana-validator
```

Waits for the StatefulSet to be Ready, then the Job:
1. Generates a fresh payer keypair (only on first run).
2. Airdrops 100 SOL to the payer from the local faucet.
3. Mints a fresh $GRID SPL token (decimals 9 to mirror mainnet design).
4. Creates an associated token account + mints 1B GRID to the treasury.
5. Persists `SOLANA_PAYER_KEYPAIR` + `GRID_TOKEN_MINT_ADDRESS` + `SOLANA_PAYER_PUBKEY` into Secret `iogrid-solana-payout`.

Then point billing-svc at the new endpoints:

```bash
kubectl -n iogrid set env deploy/billing-svc \
  SOLANA_RPC_URL=http://solana-validator:8899

kubectl -n iogrid patch deploy/billing-svc --type=json -p='[
  {"op":"add","path":"/spec/template/spec/containers/0/envFrom/-","value":{"secretRef":{"name":"iogrid-solana-payout"}}}
]'

kubectl -n iogrid rollout restart deploy/billing-svc
```

Verify the log flip:

```bash
kubectl -n iogrid logs deploy/billing-svc --tail=20 | grep -i solana
# Should show: INFO solana: live mode wallet=… mint=…
# (Was: WARN solana: stub mode)
```

## Cluster capacity note

The validator StatefulSet requests 200Mi memory; the bootstrap Job needs
a brief 128Mi for the keypair/airdrop/mint sequence. On a single-node
phase-0 cluster at >75% memory the pod stays `Pending` with
`Insufficient memory`. Two options:

1. **Wait** — once another workload scales down, the scheduler will
   place it automatically (StatefulSet + Job already applied; no action
   needed).
2. **Free a slot** — evict a non-critical pod, e.g.
   `kubectl -n talentmesh scale deploy/stt-streaming-service --replicas=0`
   (the heaviest non-iogrid consumer per `kubectl top pods -A`).

When the cluster grows to 2+ nodes the constraint disappears; this
runbook documents the manifest, not a node-add requirement.

## Why local validator instead of devnet

| Path | Cost | Founder-physical? | Unblocks? |
|---|---|---|---|
| Public devnet faucet | Free | YES — Turnstile in browser | Only after faucet click; rate-limited |
| Helius devnet (API key) | Free tier | Founder API key | After key in Secret |
| Local validator (this) | 5Gi PVC + 512Mi RAM | **NO** | Immediately, unlimited |

For Phase-0 demo of the $GRID earnings flow, ledger state doesn't have to be devnet/mainnet — it has to be a real SPL token on a real Solana RPC the billing-svc can transfer against. Local validator is that.

Migration to devnet/mainnet later is a config flip: change `SOLANA_RPC_URL`, re-bootstrap the payer + token via the same Job pattern.

## Refs

- [#345](https://github.com/iogrid/iogrid/issues/345) Solana devnet bootstrap — bypassed via local validator
- [#274](https://github.com/iogrid/iogrid/issues/274) billing-svc Solana stub-mode exit — closed by this
- [#309](https://github.com/iogrid/iogrid/issues/309) EPIC: hatice sees real $GRID
