#!/usr/bin/env bash
# verify-devnet.sh — exercise the Ping approve-verification path against a
# REAL on-chain devnet $GRID transaction. Refs iogrid/iogrid#629.
#
# What it does
# ============
#   1. (optional) Creates a real on-chain devnet tx: an SPL Token-2022
#      transfer of $GRID from the treasury to a throwaway test account,
#      capturing the transaction SIGNATURE. Skipped if GRID_DEVNET_SIG is
#      already exported (re-verify an existing tx without spending).
#   2. Runs the guarded jest integration test
#      (mobile/ios/src/lib/wallets/__tests__/ping-pay-devnet.test.ts) which
#      calls the SAME `verifyApprovalBestEffort()` getTransaction RPC logic
#      the mobile app runs on a Ping success-bounce, and asserts the result
#      is 'confirmed'.
#
# GUARDRAIL: DEVNET ONLY. This script hard-aborts unless `solana config get`
# reports the devnet RPC. It NEVER targets mainnet. This is test/verification
# work, not a financial action.
#
# Usage
# =====
#   # Re-verify a known devnet signature (no on-chain spend):
#   GRID_DEVNET_SIG=<sig> ./verify-devnet.sh
#
#   # Create a fresh devnet transfer + verify it (needs treasury keypair):
#   TREASURY_KEYPAIR_PATH=/tmp/devnet-deploy/treasury.json ./verify-devnet.sh
#
set -euo pipefail

MINT="${GRID_TOKEN_MINT_ADDRESS:-BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR}"
RPC="${EXPO_PUBLIC_SOLANA_RPC_URL:-https://api.devnet.solana.com}"
AMOUNT="${GRID_VERIFY_AMOUNT:-250}"

# Resolve solana CLI (may live under the default install dir).
if ! command -v solana >/dev/null 2>&1; then
  export PATH="$HOME/.local/share/solana/install/active_release/bin:$PATH"
fi

# ── GUARDRAIL: devnet only ────────────────────────────────────────────────
CONFIGURED_RPC="$(solana config get | awk '/RPC URL/{print $3}')"
if [[ "$CONFIGURED_RPC" != "https://api.devnet.solana.com" ]]; then
  echo "ABORT: solana config RPC is '$CONFIGURED_RPC', not devnet." >&2
  echo "       Run: solana config set --url https://api.devnet.solana.com" >&2
  exit 1
fi
echo "[verify-devnet] devnet confirmed (RPC=$CONFIGURED_RPC)"
echo "[verify-devnet] \$GRID mint: $MINT"

# ── Step 1: produce a real on-chain devnet tx (unless one is supplied) ────
if [[ -z "${GRID_DEVNET_SIG:-}" ]]; then
  TREASURY="${TREASURY_KEYPAIR_PATH:-/tmp/devnet-deploy/treasury.json}"
  if [[ ! -f "$TREASURY" ]]; then
    echo "ABORT: no GRID_DEVNET_SIG and no treasury keypair at $TREASURY." >&2
    echo "       Either export GRID_DEVNET_SIG=<existing sig> to re-verify," >&2
    echo "       or set TREASURY_KEYPAIR_PATH to a funded devnet treasury." >&2
    exit 1
  fi
  RECIP="${GRID_TEST_RECIPIENT_KEYPAIR:-/tmp/devnet-deploy/grid-test-recipient.json}"
  if [[ ! -f "$RECIP" ]]; then
    solana-keygen new --no-bip39-passphrase --silent -o "$RECIP"
  fi
  RECIP_PUB="$(solana-keygen pubkey "$RECIP")"
  echo "[verify-devnet] transferring $AMOUNT \$GRID treasury -> $RECIP_PUB"
  OUT="$(spl-token transfer "$MINT" "$AMOUNT" "$RECIP_PUB" \
    --fee-payer "$TREASURY" --allow-unfunded-recipient 2>&1)"
  echo "$OUT"
  GRID_DEVNET_SIG="$(echo "$OUT" | awk '/Signature:/{print $2}')"
  if [[ -z "$GRID_DEVNET_SIG" ]]; then
    echo "ABORT: could not capture a transfer signature." >&2
    exit 1
  fi
fi
echo "[verify-devnet] verifying signature: $GRID_DEVNET_SIG"

# ── Step 2: run the guarded jest integration test against the real tx ─────
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IOS_DIR="$HERE/../../mobile/ios"
cd "$IOS_DIR"
GRID_DEVNET_SIG="$GRID_DEVNET_SIG" \
EXPO_PUBLIC_SOLANA_RPC_URL="$RPC" \
  npx jest --config jest.config.js ping-pay-devnet

echo
echo "[verify-devnet] DONE — signature $GRID_DEVNET_SIG verified 'confirmed' on devnet."
