# Solana / $GRID — Phase 0 operator runbook

This is the founder/operator runbook for getting `billing-svc` out of
stub mode by minting a Phase-0 **devnet** $GRID SPL token and wiring
the resulting mint address + hot-wallet keypair into the Kubernetes
Secret that `billing-svc` reads at boot.

> **Phase 0 = devnet only.** No real money moves. The mainnet TGE
> (Token Generation Event) is end-state work tracked in
> [EPIC #87](https://github.com/iogrid/iogrid/issues/87) and described
> in [docs/BUSINESS-STRATEGY.md §4 (Currency model — $GRID + fiat hybrid)](./BUSINESS-STRATEGY.md#4-currency-model--grid--fiat-hybrid).
>
> Mainnet wiring will replace the single-sig hot-wallet keypair below
> with a [Squads Protocol](https://squads.so) 2-of-3 multisig — see
> `coordinator/services/billing-svc/internal/solana/multisig.go`.

## What this runbook gives you

When you finish:

- `billing-svc` log line on startup changes from
  `WARN solana: stub mode (GRID_TOKEN_MINT_ADDRESS or hot-wallet path unset)`
  to
  `INFO solana: live mode wallet=... mint=... token_program=...`.
- The in-process daily cron starts: `INFO solana: daily cron starting`.
- The cron's first tick runs the previous-day window. In Phase 0 the
  `usage_event` table is empty so `RunDailySwapAndDistribute`
  short-circuits at `grand == 0 → return nil`. **No on-chain calls
  happen** until the mothership starts ingesting metering events.
  This is the intended Phase 0 behaviour: code paths are exercised,
  logs prove the wiring, but the network sees zero TXs.

## Prerequisites

A workstation with:

- `solana` CLI ≥ 1.18.x — install via
  `sh -c "$(curl -sSfL https://release.solana.com/stable/install)"`.
- `spl-token` CLI (ships with the Solana CLI tarball).
- `kubectl` pointed at the mothership cluster (the same kubeconfig used
  in [docs/PHASE0-UNBLOCK.md](./PHASE0-UNBLOCK.md)).
- Internet egress to `https://api.devnet.solana.com`.

## Step 1 — generate the hot-wallet keypair

The hot wallet is the single signer for all Phase 0 payouts + burns.

```bash
# Output the 64-byte keypair to a tmpfile. Do not check this into git.
solana-keygen new --no-bip39-passphrase --force \
  -o /tmp/iogrid-payout.json

# Verify the file looks right (array of 64 integers).
head -c 80 /tmp/iogrid-payout.json && echo
# expect: [12,34,56,...]

# Print the pubkey — needed for the airdrop + mint-authority below.
HOT_WALLET=$(solana-keygen pubkey /tmp/iogrid-payout.json)
echo "hot-wallet pubkey: $HOT_WALLET"
```

## Step 2 — fund the hot wallet on devnet

Devnet SOL is free; airdrop the operating balance the wallet needs to
pay rent (`spl-token create-token` costs ≈ 0.0014 SOL) and tx fees.

```bash
solana airdrop 5 "$HOT_WALLET" --url https://api.devnet.solana.com

# Verify the balance landed.
solana balance "$HOT_WALLET" --url https://api.devnet.solana.com
# expect: 5 SOL
```

> If the public devnet faucet is rate-limited, retry with the
> per-request limit (`solana airdrop 1 …`) up to 5 times, or use
> [https://faucet.solana.com](https://faucet.solana.com).

## Step 3 — mint the devnet $GRID token

`$GRID` is minted under the [Token-2022](https://spl.solana.com/token-2022)
program in production (transfer hooks, metadata extensions). For Phase 0
we use Token-2022 too so the chainClient picks the right program-id
without per-environment config drift.

```bash
MINT_OUTPUT=$(spl-token create-token \
  --program-id TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb \
  --url https://api.devnet.solana.com \
  --decimals 9 \
  --fee-payer /tmp/iogrid-payout.json \
  --mint-authority "$HOT_WALLET")

# Extract the mint address from spl-token's stdout.
GRID_MINT=$(echo "$MINT_OUTPUT" | awk '/Creating token/ {print $3}')
echo "GRID devnet mint: $GRID_MINT"
```

**Save `$GRID_MINT`** — you'll paste it into the Kubernetes Secret in
Step 5 and commit it to the public `docs/TRANSPARENCY/*.md` quarterly
report so providers can verify the mint address out-of-band.

> The hot wallet is both the **mint authority** and the **freeze
> authority** in Phase 0. Mainnet TGE flips both to the Squads
> multisig.

## Step 4 (optional) — pre-mint $GRID supply for testing

For a non-empty Jupiter quote round-trip on devnet you'll want some
$GRID in the hot wallet's Associated Token Account. Skip this step if
you only want the "log proves wiring" outcome.

```bash
spl-token create-account "$GRID_MINT" \
  --url https://api.devnet.solana.com \
  --fee-payer /tmp/iogrid-payout.json \
  --owner "$HOT_WALLET"

spl-token mint "$GRID_MINT" 1000000 \
  --url https://api.devnet.solana.com \
  --fee-payer /tmp/iogrid-payout.json \
  --mint-authority /tmp/iogrid-payout.json

spl-token balance "$GRID_MINT" \
  --url https://api.devnet.solana.com \
  --owner "$HOT_WALLET"
# expect: 1000000
```

## Step 5 — create the Kubernetes Secret

The Secret carries two keys:

| Key                       | Type   | Read by billing-svc as                             |
|---------------------------|--------|----------------------------------------------------|
| `keypair_json`            | file   | `SOLANA_HOT_WALLET_KEYPAIR_PATH=/var/run/solana/keypair.json` |
| `grid_token_mint_address` | string | env `GRID_TOKEN_MINT_ADDRESS`                       |

```bash
# Confirm the Secret doesn't already exist (delete & recreate if so).
kubectl -n iogrid get secret iogrid-solana-payout 2>/dev/null \
  && kubectl -n iogrid delete secret iogrid-solana-payout

kubectl -n iogrid create secret generic iogrid-solana-payout \
  --from-file=keypair_json=/tmp/iogrid-payout.json \
  --from-literal=grid_token_mint_address="$GRID_MINT"

# Wipe the local keypair file.
shred -u /tmp/iogrid-payout.json

# Verify both keys are present.
kubectl -n iogrid get secret iogrid-solana-payout \
  -o jsonpath='{.data}' | jq 'keys'
# expect: ["grid_token_mint_address", "keypair_json"]
```

## Step 6 — roll billing-svc + verify the logs

```bash
kubectl -n iogrid rollout restart deploy/billing-svc
kubectl -n iogrid rollout status  deploy/billing-svc --timeout=2m
```

Check the new log lines:

```bash
kubectl -n iogrid logs deploy/billing-svc --since=2m | grep -iE 'solana|cron'
```

Expected output (substituting your real pubkey + mint):

```
INFO solana: live mode wallet=8xQ4...e3rT mint=GRId...xN9w token_program=TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb multisig_mode=single-sig
INFO solana: daily cron starting schedule="5 0 * * *" live=true
INFO solana daily loop: no revenue for window start=2026-05-19T00:00:00Z
```

If you instead see:

```
WARN solana: stub mode (GRID_TOKEN_MINT_ADDRESS or hot-wallet path unset)
```

…then the Secret was not picked up. Re-check:

```bash
kubectl -n iogrid get pod -l app.kubernetes.io/name=billing-svc \
  -o jsonpath='{.items[0].spec.containers[0].env}' | jq
# expect a GRID_TOKEN_MINT_ADDRESS entry with valueFrom.secretKeyRef.name=iogrid-solana-payout

kubectl -n iogrid exec deploy/billing-svc -- ls -l /var/run/solana
# expect: -r-------- 1 65532 65532 ... keypair.json
```

## Step 7 — bind providers to the devnet wallet

For Phase 0 we don't run real payouts on devnet — but providers should
still see their wallet address in the dashboard. `identity-svc` exposes
a Solana wallet-bind endpoint (Sign-In-With-Solana). The web UI is
tracked in EPIC #87.

If you want to exercise the swap path end-to-end on devnet, fund a few
provider pubkeys (a `solana-keygen new` per provider) with airdropped
SOL, bind them via the wallet-connect button on `app.iogrid.org`, and
then insert a synthetic `usage_event` row so the cron has something
to distribute against:

```sql
-- Connect to the iogrid Postgres (kubectl port-forward svc/iogrid-pg-rw 5432).
INSERT INTO usage_event (id, provider_id, customer_id, kind, amount_cents, started_at, ended_at)
VALUES (gen_random_uuid(), '<PROVIDER_UUID>', '<CUSTOMER_UUID>', 'PROXY_REQ', 100, now() - interval '1 day', now() - interval '1 day');
```

Then trigger the cron manually by restarting billing-svc (the
on-boot run-for-yesterday tick fires every Pod start). Tail the logs:

```bash
kubectl -n iogrid logs deploy/billing-svc -f | grep -iE 'jupiter|payout|burn|swap'
```

Expected lines (devnet Jupiter quote, may 404 if the LP pool doesn't
exist on devnet — that's fine, the row transitions to FAILED and is
retried on the next tick):

```
INFO solana: quoting jupiter outputMint=GRId...xN9w usd_cents=100
INFO solana: tx submitted signature=...
INFO solana: payout confirmed provider_id=... amount_lamports=...
```

## Security notes

- **Never commit `/tmp/iogrid-payout.json` or its contents.** This repo
  is public. The Secret object stays in-cluster; the only place the
  64-byte keypair touches disk on the cluster is the projected file
  at `/var/run/solana/keypair.json` (mode 0400, owner 65532, container
  filesystem is read-only).
- The hot wallet's pubkey is fine to share publicly (it's how anyone
  verifies on-chain activity). The `keypair_json` payload is the
  full ed25519 keypair = full spend authority.
- Rotate the hot wallet by repeating Steps 1–6 (the new keypair
  replaces the old one in the Secret; old funds need to be swept to
  the new pubkey before rotation).

## Mainnet flip

When the iogrid Foundation is ready for the TGE:

1. Create the production mint under Token-2022 with the **Squads
   multisig** as both mint and freeze authority (NOT a single-sig
   keypair).
2. Set `SQUADS_MULTISIG_PUBKEY` in `billing-svc-secrets` — the Go
   layer detects this and routes every write through the Squads vault
   (see `internal/solana/multisig.go`).
3. Replace `SOLANA_RPC_URL` in `billing-svc-config` from
   `https://api.devnet.solana.com` to a paid mainnet RPC
   (Helius / Triton / your own).
4. Set `BURN_VIA_INCINERATOR=false` so the daily burn uses a real
   `BurnChecked` instruction (cheaper, on-chain accounting is
   exact).
5. Audit the mint authority + freeze authority addresses match the
   multisig's vault PDA before anyone sends real funds.

The Go layer needs **no code changes** between devnet and mainnet —
the entire diff is operator-provided configuration.
