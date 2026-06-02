# $GRID Solana Addresses

Canonical registry of on-chain addresses for the `$GRID` SPL token + the
iogrid coordinator's settlement infrastructure. Refs
[iogrid/iogrid#595](https://github.com/iogrid/iogrid/issues/595) (Track 5 /
EPIC [#581](https://github.com/iogrid/iogrid/issues/581)).

> **Source of truth.** When any of these change, edit THIS file in the same
> commit as the code/env that consumes them. The settlement-worker
> deployment reads `GRID_TOKEN_MINT_ADDRESS` + `TREASURY_PRIVATE_KEY_PATH`
> from env; an address drift here vs the cluster's `billing-svc` secret is a
> P0 incident.

## Devnet (active for staging)

| Field | Value | Status |
|---|---|---|
| Cluster | `devnet` | active |
| RPC | `https://api.devnet.solana.com` (or Helius free tier via `SOLANA_RPC_URL`) | |
| Mint address (`GRID_TOKEN_MINT_ADDRESS`) | _filled in after first `seed-treasury.sh devnet` run_ | pending first deploy |
| Token program | `TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA` (legacy SPL) | |
| Decimals | `9` | |
| Initial supply | `1_000_000_000` ($1e9$ $GRID, $1e18$ atomic) | |
| Mint authority | treasury keypair (`~/.config/solana/grid-treasury.json` on operator workstation) | retained for re-mint until `LOCK_MINT_AUTHORITY=1` |
| Freeze authority | `null` | non-negotiable |
| Treasury pubkey | _from `solana-keygen pubkey` on the keypair above_ | pending first deploy |
| Treasury ATA | _from `getOrCreateAssociatedTokenAccount(treasury, mint)`_ | pending first deploy |
| Metadata PDA | _from Metaplex `findMetadataPda(mint)`_ | pending first deploy |
| Metadata URI | `https://iogrid.org/grid-token.json` | static |

To populate the rows above, run:

```bash
cd solana/grid && ./seed-treasury.sh devnet
```

then commit the printed `mint_address`, `treasury`, `treasury_ata`,
`metadata_pda` values into this file (replacing the _pending_ markers).

## Mainnet-beta (deferred)

Per [#581 Locked Model](https://github.com/iogrid/iogrid/issues/581#locked-model),
mainnet deploy is a **separate followup with founder sign-off**.

| Field | Value |
|---|---|
| Cluster | `mainnet-beta` |
| RPC | `https://mainnet.helius-rpc.com/?api-key=…` |
| Mint address | **TBD** — do not deploy without explicit founder approval |
| Treasury | Squads multisig (`SquadsMpl…`), threshold 2-of-2 |

## Audit log of address changes

| Date | Cluster | Actor | Change | PR |
|---|---|---|---|---|
| _pending_ | devnet | _operator_ | initial mint deploy via `seed-treasury.sh` | _filed via #595 / Track 5 PR_ |

## Cross-references

- Deploy script: [`solana/grid/deploy.ts`](../solana/grid/deploy.ts)
- Treasury seed wrapper: [`solana/grid/seed-treasury.sh`](../solana/grid/seed-treasury.sh)
- Faucet handler (devnet only): [`coordinator/services/billing-svc/internal/server/handlers/faucet_devnet.go`](../coordinator/services/billing-svc/internal/server/handlers/faucet_devnet.go)
- Settlement worker: [`coordinator/services/billing-svc/cmd/settlement-worker/main.go`](../coordinator/services/billing-svc/cmd/settlement-worker/main.go)
- Token metadata JSON (served by web/): _follow-up issue — host `https://iogrid.org/grid-token.json` from web/ public assets_
