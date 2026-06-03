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
| Mint address (`GRID_TOKEN_MINT_ADDRESS`) | `BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR` | **DEPLOYED 2026-06-03** (#629) |
| Token program | `TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb` (**Token-2022**, matches canonical TOKENOMICS spec) | |
| Decimals | `9` | verified `spl-token display` |
| Initial supply | `1_000_000` $GRID test supply (`1e15` atomic) — devnet test float, NOT the 1B cap | |
| Mint authority | devnet treasury `8EaS5sf4dT7SzEFNYKC1PD5C3PcKhPnEnHCYjSZ1hcpb` | retained for re-mint on devnet |
| Freeze authority | `null` | non-negotiable — verified `(not set)` |
| Treasury pubkey | `8EaS5sf4dT7SzEFNYKC1PD5C3PcKhPnEnHCYjSZ1hcpb` | |
| Treasury ATA | `4u9JDrLVBkBL2vLx691egahEDrzNMaQxNZ6B3M334dM3` | 1,000,000 $GRID balance |
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
| 2026-06-03 | devnet | hatiyildiz | initial Token-2022 mint `BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR` via `spl-token create-token --program-2022 --decimals 9`; minted 1,000,000 $GRID test supply to treasury ATA so Ping integration is testable end-to-end | #629 |

> **Note on tooling:** this devnet mint was created with `spl-token
> create-token --program-2022` (Token-2022), matching the canonical
> `docs/TOKENOMICS.md` spec, rather than the `seed-treasury.sh` /
> `deploy.ts` legacy-SPL (Tokenkeg) path referenced below. Re-align
> `deploy.ts` to Token-2022 + Metaplex Token-2022 metadata before any
> mainnet deploy (follow-up).

## Cross-references

- Deploy script: [`solana/grid/deploy.ts`](../solana/grid/deploy.ts)
- Treasury seed wrapper: [`solana/grid/seed-treasury.sh`](../solana/grid/seed-treasury.sh)
- Faucet handler (devnet only): [`coordinator/services/billing-svc/internal/server/handlers/faucet_devnet.go`](../coordinator/services/billing-svc/internal/server/handlers/faucet_devnet.go)
- Settlement worker: [`coordinator/services/billing-svc/cmd/settlement-worker/main.go`](../coordinator/services/billing-svc/cmd/settlement-worker/main.go)
- Token metadata JSON (served by web/): _follow-up issue — host `https://iogrid.org/grid-token.json` from web/ public assets_
