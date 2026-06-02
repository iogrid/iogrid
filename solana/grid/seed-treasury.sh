#!/usr/bin/env bash
# seed-treasury.sh — bootstrap the $GRID treasury keypair + run the deploy.
#
# Refs iogrid/iogrid#595 (Track 5 / EPIC #581).
#
# What it does
# ============
#
#   1. Generates a fresh treasury keypair at $TREASURY_KEYPAIR_PATH if one
#      doesn't already exist (default ~/.config/solana/grid-treasury.json).
#   2. Funds it with a devnet airdrop (devnet only).
#   3. Runs `pnpm tsx deploy.ts` which creates the mint, ATA, full 1B
#      supply, and Metaplex metadata.
#   4. Prints a paste-ready block to drop into docs/SOLANA-ADDRESSES.md.
#
# Pre-mainnet, treasury MUST be moved to a Squads multisig — this script's
# one-keypair flow is acceptable only for devnet staging.
#
# Usage
# =====
#
#   ./seed-treasury.sh devnet      # default
#   ./seed-treasury.sh mainnet-beta
#
set -euo pipefail
CLUSTER="${1:-devnet}"
TREASURY_KEYPAIR_PATH="${TREASURY_KEYPAIR_PATH:-$HOME/.config/solana/grid-treasury.json}"

if [[ "$CLUSTER" != "devnet" && "$CLUSTER" != "mainnet-beta" ]]; then
  echo "usage: $0 [devnet|mainnet-beta]" >&2
  exit 1
fi

if [[ "$CLUSTER" == "mainnet-beta" ]]; then
  echo
  echo "######################################################################"
  echo "# WARNING: mainnet-beta seed path                                    #"
  echo "# The treasury keypair MUST be a Squads multisig signer in prod.     #"
  echo "# This script's single-keypair flow is for devnet ONLY.              #"
  echo "# Press Ctrl-C now if you didn't mean to run this on mainnet.        #"
  echo "######################################################################"
  echo
  read -r -p "type 'I understand' to continue: " ack
  [[ "$ack" == "I understand" ]] || exit 1
fi

# 1) Treasury keypair
mkdir -p "$(dirname "$TREASURY_KEYPAIR_PATH")"
if [[ ! -f "$TREASURY_KEYPAIR_PATH" ]]; then
  echo "generating treasury keypair → $TREASURY_KEYPAIR_PATH"
  solana-keygen new --no-bip39-passphrase --silent -o "$TREASURY_KEYPAIR_PATH"
else
  echo "re-using existing treasury keypair at $TREASURY_KEYPAIR_PATH"
fi
TREASURY_PUBKEY="$(solana-keygen pubkey "$TREASURY_KEYPAIR_PATH")"
echo "treasury pubkey: $TREASURY_PUBKEY"

# 2) Devnet airdrop (no-op on mainnet)
if [[ "$CLUSTER" == "devnet" ]]; then
  echo "requesting 2 SOL devnet airdrop…"
  solana airdrop 2 "$TREASURY_PUBKEY" --url https://api.devnet.solana.com || true
fi

# 3) Install + deploy
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"
if [[ ! -d node_modules ]]; then
  echo "installing deps with pnpm…"
  pnpm install
fi
TREASURY_KEYPAIR_PATH="$TREASURY_KEYPAIR_PATH" \
  pnpm tsx deploy.ts --cluster "$CLUSTER"

echo
echo "=== copy the JSON above into docs/SOLANA-ADDRESSES.md (.${CLUSTER} block) ==="
