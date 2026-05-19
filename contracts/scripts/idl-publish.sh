#!/usr/bin/env bash
#
# idl-publish.sh — upload the IDL for each $GRID program to its on-chain IDL account.
#
# Anchor 0.31 supports `anchor idl init` and `anchor idl upgrade` to store the IDL JSON
# on-chain in an account derived from the program ID. This lets wallets (Phantom, Backpack,
# Solflare), block explorers (Solana Explorer, Solscan), and tools (Anchor's TS client,
# Helius's webhook decoder) decode the program's instructions and account data automatically.
#
# Without an on-chain IDL, wallets show "Unknown program" + a raw byte payload — bad UX for
# end users signing token-related transactions. With it, wallets show "$GRID emission program:
# claim_epoch (epoch_id=42, billing_signer=...)" — readable and verifiable.
#
# Usage:
#   ./scripts/idl-publish.sh --cluster devnet
#   ./scripts/idl-publish.sh --cluster mainnet-beta --keypair ~/.keys/foundation-multisig.json
#   ./scripts/idl-publish.sh --cluster devnet --program emission   # single program
#   ./scripts/idl-publish.sh --cluster devnet --upgrade            # use idl upgrade (existing)
#
# Mainnet behaviour:
#   - Requires CONFIRM_MAINNET=I_REALLY_MEAN_IT env var.
#   - The IDL is signed by the IDL authority. By default the IDL authority is the deployer
#     keypair; on mainnet, transfer the IDL authority to the Squads multisig before publish.
#
# Cleanup / authority rotation:
#   anchor idl set-authority --provider.cluster <c> --program-id <pid> --new-authority <pk>
#   anchor idl erase-authority ...   # closes the IDL account; use with caution

set -euo pipefail

PROGRAMS=(grid_token emission vesting staking burn)
CLUSTER=""
KEYPAIR="${HOME}/.config/solana/id.json"
MODE="init"  # init | upgrade
SINGLE_PROGRAM=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cluster) CLUSTER="$2"; shift 2 ;;
    --keypair) KEYPAIR="$2"; shift 2 ;;
    --program) SINGLE_PROGRAM="${2//-/_}"; shift 2 ;;
    --upgrade) MODE="upgrade"; shift ;;
    --init)    MODE="init"; shift ;;
    -h|--help)
      sed -n '2,30p' "$0"
      exit 0
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

case "$CLUSTER" in
  localnet|devnet|mainnet-beta) ;;
  "")
    echo "missing required --cluster <localnet|devnet|mainnet-beta>" >&2
    exit 2
    ;;
  *)
    echo "unknown cluster: $CLUSTER" >&2
    exit 2
    ;;
esac

if [[ "$CLUSTER" == "mainnet-beta" && "${CONFIRM_MAINNET:-}" != "I_REALLY_MEAN_IT" ]]; then
  echo "Refusing mainnet IDL publish without CONFIRM_MAINNET=I_REALLY_MEAN_IT" >&2
  exit 3
fi

# Workspace root
WORKSPACE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$WORKSPACE"

# Determine which Anchor.toml section holds the program IDs
SECTION="programs.${CLUSTER}"
if [[ "$CLUSTER" == "mainnet-beta" ]]; then
  SECTION="programs.mainnet"
fi

declare -A IDS
while IFS= read -r line; do
  key="$(echo "$line" | awk '{print $1}')"
  val="$(echo "$line" | awk -F'"' '{print $2}')"
  if [[ -n "$key" && -n "$val" ]]; then
    IDS[$key]="$val"
  fi
done < <(awk -v s="[${SECTION}]" '$0==s{f=1; next} /^\[/{f=0} f && /=/ {print}' Anchor.toml)

# Narrow to a single program if requested
if [[ -n "$SINGLE_PROGRAM" ]]; then
  FOUND=0
  for p in "${PROGRAMS[@]}"; do
    if [[ "$p" == "$SINGLE_PROGRAM" ]]; then
      FOUND=1
      PROGRAMS=("$SINGLE_PROGRAM")
      break
    fi
  done
  if (( FOUND == 0 )); then
    echo "unknown program: $SINGLE_PROGRAM" >&2
    exit 2
  fi
fi

for p in "${PROGRAMS[@]}"; do
  idl="target/idl/${p}.json"
  pid="${IDS[$p]:-}"
  if [[ -z "$pid" ]]; then
    echo "[idl-publish] could not resolve program id for ${p} in [${SECTION}]" >&2
    exit 4
  fi
  if [[ ! -f "$idl" ]]; then
    echo "[idl-publish] missing IDL artefact: $idl" >&2
    echo "Run \`anchor build\` first." >&2
    exit 5
  fi
  echo
  echo "[idl-publish] ${p} -> ${pid}  (${idl})"
  sha256sum "$idl"

  ANCHOR_CLUSTER="$CLUSTER"
  if [[ "$ANCHOR_CLUSTER" == "mainnet-beta" ]]; then
    ANCHOR_CLUSTER="mainnet"
  fi

  case "$MODE" in
    init)
      anchor idl init \
        --provider.cluster "$ANCHOR_CLUSTER" \
        --provider.wallet "$KEYPAIR" \
        --filepath "$idl" \
        "$pid" || {
          echo "[idl-publish] init failed — retrying with upgrade (account may already exist)"
          anchor idl upgrade \
            --provider.cluster "$ANCHOR_CLUSTER" \
            --provider.wallet "$KEYPAIR" \
            --filepath "$idl" \
            "$pid"
        }
      ;;
    upgrade)
      anchor idl upgrade \
        --provider.cluster "$ANCHOR_CLUSTER" \
        --provider.wallet "$KEYPAIR" \
        --filepath "$idl" \
        "$pid"
      ;;
  esac
done

echo
echo "[idl-publish] DONE. Verify on Solscan / Anchor IDL Explorer:"
for p in "${PROGRAMS[@]}"; do
  echo "  ${p}: https://explorer.solana.com/address/${IDS[$p]}/anchor-program?cluster=${CLUSTER}"
done
