# `$GRID` SPL token — deploy + treasury procedures

Refs [iogrid/iogrid#595](https://github.com/iogrid/iogrid/issues/595) (Track 5
/ EPIC [#581](https://github.com/iogrid/iogrid/issues/581)).

## Locked model

| Field | Value |
|---|---|
| Symbol | `GRID` |
| Name | `iogrid` |
| Decimals | `9` |
| Initial supply | `1_000_000_000` ($1e9$ tokens, $1e18$ atomic units) |
| Mint authority | treasury keypair (devnet) → Squads multisig (mainnet) |
| Freeze authority | `null` (never freezable — explicit transparency property) |
| Token program | legacy SPL Token (`Tokenkeg…`) — same wire format as Token-2022 for TransferChecked / BurnChecked, simpler for devnet and Jupiter compatibility |
| Metadata URI | `https://iogrid.org/grid-token.json` |
| Pricing | `0.001 GRID / GB` of VPN traffic |
| Provider share | `85 %` of consumed GRID per session |
| iogrid commission | `15 %` |
| Settlement | Server-side DB escrow; batched on-chain transfer every 5 min by `settlement-worker` |
| Burn | `2 %` daily buyback-and-burn from iogrid commission pool (lives in `billing-svc/internal/solana/burn.go`) |

## Deploy procedure (devnet)

```bash
cd solana/grid
pnpm install
./seed-treasury.sh devnet
```

What happens, in order:

1. Generates `$TREASURY_KEYPAIR_PATH` (default `~/.config/solana/grid-treasury.json`) — *back this up immediately*, the keypair IS the treasury.
2. Requests a 2 SOL devnet airdrop for the treasury so it can pay for the mint + ATA + metadata transactions.
3. Calls `deploy.ts` which:
   - `createMint(decimals=9, mintAuthority=treasury, freezeAuthority=null)`
   - `getOrCreateAssociatedTokenAccount(treasury, mint)` → treasury ATA
   - `mintTo(treasuryATA, 1_000_000_000 * 1e9)` — full supply, one txn
   - `createMetadataAccountV3(name='iogrid', symbol='GRID', uri='https://iogrid.org/grid-token.json')`
4. Prints a JSON summary — copy that into `docs/SOLANA-ADDRESSES.md` (devnet block).

## Deploy procedure (mainnet — DEFERRED)

Per [#581](https://github.com/iogrid/iogrid/issues/581) Locked Model, mainnet
deploy is a **separate followup with founder sign-off**. The script gates on
an `I understand` typed acknowledgement, but more importantly:

- Mainnet treasury MUST be a Squads multisig (see
  `coordinator/services/billing-svc/internal/solana/multisig.go` for the
  Phase-2 signer wrap).
- Mainnet deploy needs a real `https://iogrid.org/grid-token.json` served by
  the web app (logo PNG + description JSON pinned to IPFS for fallback).
- The `LOCK_MINT_AUTHORITY=1` flag should be flipped on the first mainnet
  deploy run so the supply becomes immutable; devnet keeps it on the
  treasury so we can re-mint after re-orgs.

## Files

```
solana/grid/
├── deploy.ts         — TS deploy script (web3.js + spl-token + Metaplex umi)
├── seed-treasury.sh  — wrapper: generate keypair → fund → run deploy
├── package.json      — pnpm deps (web3.js, spl-token, mpl-token-metadata)
└── README.md         — this file
```

## Envs touched at runtime

- `TREASURY_KEYPAIR_PATH` — local path to a Solana CLI keypair JSON
  (`solana-keygen new -o …`). NEVER commit this; the file lives in
  `~/.config/solana/` or a sealed-secrets entry in `infra/k8s/billing-svc/`.
- `SOLANA_RPC_URL` — override (default `clusterApiUrl('devnet')`). Production
  uses Helius (`https://mainnet.helius-rpc.com/?api-key=…`).
- `GRID_MINT_ADDRESS` — re-use an existing mint instead of creating a new one
  (idempotent re-runs).
- `LOCK_MINT_AUTHORITY=1` — null out the mint authority once the initial
  supply is in place. Use on the first mainnet run; never on devnet.

## Faucet (devnet only)

Once the mint exists, `billing-svc` exposes a devnet-only faucet:

```
POST /v1/devnet/faucet
{"wallet_address":"<base58 pubkey>"}
→ 200 {"amount_grid":"100","signature":"…"}
→ 429 {"error":"rate_limited"} (1 mint per wallet per hour)
```

The faucet refuses to operate when `IOGRID_CLUSTER != "devnet"`, so even an
accidental prod deploy is safe.

## Pre-mainnet hardening checklist

- [ ] Replace single-key treasury with Squads multisig (founder + 1
      iogrid-multisig key, threshold 2-of-2).
- [ ] Pin `grid-token.json` to IPFS + commit the CIDs to this README.
- [ ] Flip `LOCK_MINT_AUTHORITY=1` on the mainnet run.
- [ ] Move `TREASURY_PRIVATE_KEY_PATH` to a HSM-backed Kubernetes secret
      (Vault transit-engine or KMS-encrypted Sealed Secret).
- [ ] Document settlement-worker mainnet RPC quota in `docs/RUNBOOKS.md`.
- [ ] DEX listing follow-up: Jupiter quote API picks up the mint
      automatically once it has on-chain liquidity (Raydium pool) — file a
      Raydium farm proposal at TGE-day.
