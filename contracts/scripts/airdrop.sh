#!/usr/bin/env bash
#
# airdrop.sh — request devnet SOL airdrops for a Solana keypair.
#
# Devnet airdrops are rate-limited (~2 SOL/min per pubkey by the public faucet, with
# fallback to https://faucet.solana.com which has its own per-IP cap). This script loops
# with backoff to get a deployer / dev wallet funded enough for a contract deploy round
# (~10 SOL is comfortable for the five $GRID programs + IDL accounts).
#
# Usage:
#   ./scripts/airdrop.sh                       # default: 10 SOL to ~/.config/solana/id.json
#   ./scripts/airdrop.sh --amount 20           # target total balance
#   ./scripts/airdrop.sh --keypair ~/.keys/foundation-devnet.json
#   ./scripts/airdrop.sh --pubkey <BASE58>     # airdrop to a specific pubkey (no key needed)
#   ./scripts/airdrop.sh --cluster localnet    # localnet airdrop (no rate limit)
#
# Cluster aliases (per Solana CLI):
#   localnet     -> http://localhost:8899
#   devnet       -> https://api.devnet.solana.com
#   testnet      -> https://api.testnet.solana.com  (do not use; phasing out)
#   mainnet-beta -> https://api.mainnet-beta.solana.com  (no airdrop possible)

set -euo pipefail

CLUSTER="devnet"
KEYPAIR="${HOME}/.config/solana/id.json"
TARGET_BALANCE="10"
PUBKEY=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cluster) CLUSTER="$2"; shift 2 ;;
    --keypair) KEYPAIR="$2"; shift 2 ;;
    --amount)  TARGET_BALANCE="$2"; shift 2 ;;
    --pubkey)  PUBKEY="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,20p' "$0"
      exit 0
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

case "$CLUSTER" in
  localnet|devnet|testnet) ;;
  mainnet-beta)
    echo "Mainnet does not airdrop. Buy SOL on an exchange." >&2
    exit 2
    ;;
  *)
    echo "unknown cluster: $CLUSTER" >&2
    exit 2
    ;;
esac

if [[ -z "$PUBKEY" ]]; then
  if [[ ! -f "$KEYPAIR" ]]; then
    echo "[airdrop] keypair not found: $KEYPAIR" >&2
    exit 3
  fi
  PUBKEY="$(solana-keygen pubkey "$KEYPAIR")"
fi

echo "[airdrop] cluster : ${CLUSTER}"
echo "[airdrop] pubkey  : ${PUBKEY}"
echo "[airdrop] target  : ${TARGET_BALANCE} SOL"

URL_FLAG=("--url" "$CLUSTER")

# Per-airdrop drip size: 2 SOL on devnet (above this is usually rejected), 100 on localnet
DRIP="2"
if [[ "$CLUSTER" == "localnet" ]]; then
  DRIP="100"
fi

ATTEMPTS=0
MAX_ATTEMPTS=30
while true; do
  CURRENT="$(solana "${URL_FLAG[@]}" balance "$PUBKEY" 2>/dev/null | awk '{print $1}')"
  if [[ -z "$CURRENT" ]]; then
    CURRENT="0"
  fi

  DONE="$(awk -v c="$CURRENT" -v t="$TARGET_BALANCE" 'BEGIN{print (c+0 >= t+0)?1:0}')"
  if [[ "$DONE" == "1" ]]; then
    echo "[airdrop] balance ${CURRENT} >= ${TARGET_BALANCE} SOL — done"
    exit 0
  fi

  echo "[airdrop] current ${CURRENT} SOL; requesting ${DRIP}..."
  if solana "${URL_FLAG[@]}" airdrop "$DRIP" "$PUBKEY" >/dev/null 2>&1; then
    echo "[airdrop] airdrop OK"
  else
    echo "[airdrop] airdrop failed — sleeping 30s (rate limit)"
    sleep 30
  fi

  ATTEMPTS=$((ATTEMPTS + 1))
  if (( ATTEMPTS >= MAX_ATTEMPTS )); then
    echo "[airdrop] giving up after ${MAX_ATTEMPTS} attempts. Try https://faucet.solana.com" >&2
    exit 4
  fi

  sleep 5
done
