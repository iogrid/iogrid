#!/usr/bin/env bash
#
# devnet-deploy.sh — deploy the five $GRID programs to Solana devnet with confirmation.
#
# Devnet deploys are MANUAL and BUDGETED — every deploy spends ~3-5 SOL from the deployer
# keypair (devnet SOL is free via airdrop but capped at 2 SOL/minute per pubkey). This
# script:
#   1. Verifies build artefacts exist for all five programs.
#   2. Verifies the deployer keypair is funded (≥10 SOL).
#   3. Prompts for an explicit "yes-to-devnet" confirmation including a checksum of the .so
#      files so you cannot accidentally re-deploy stale bytecode.
#   4. Deploys each program with `solana program deploy --program-id <pid>` so the program
#      ID stays stable across rebuilds (matches the [programs.devnet] section of Anchor.toml).
#   5. Optionally pushes IDLs to the on-chain IDL account via `anchor idl init/upgrade`
#      (delegated to scripts/idl-publish.sh for symmetry with mainnet).
#
# Usage:
#   ./scripts/devnet-deploy.sh                              # interactive
#   ./scripts/devnet-deploy.sh --program grid-token         # single program
#   ./scripts/devnet-deploy.sh --keypair ~/.keys/foundation-devnet.json
#   ./scripts/devnet-deploy.sh --no-idl                     # skip IDL upload
#   CONFIRM=I_AGREE ./scripts/devnet-deploy.sh              # bypass interactive confirm
#
# Cleanup:
#   solana program close <PROGRAM_ID> --bypass-warning --url devnet  # frees rent
#
# Safety:
#   - Will not run against mainnet-beta or localnet.
#   - Refuses if deployer keypair < 10 SOL.
#   - Logs sha256 of every .so deployed for post-hoc audit.

set -euo pipefail

PROGRAMS=(grid-token emission vesting staking burn)
SINGLE_PROGRAM=""
KEYPAIR="${HOME}/.config/solana/id.json"
SKIP_IDL=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --program)
      SINGLE_PROGRAM="$2"
      shift 2
      ;;
    --keypair)
      KEYPAIR="$2"
      shift 2
      ;;
    --no-idl)
      SKIP_IDL=1
      shift
      ;;
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

if [[ -n "$SINGLE_PROGRAM" ]]; then
  # narrow the list
  FOUND=0
  for p in "${PROGRAMS[@]}"; do
    if [[ "$p" == "$SINGLE_PROGRAM" ]]; then
      FOUND=1
      PROGRAMS=("$SINGLE_PROGRAM")
      break
    fi
  done
  if (( FOUND == 0 )); then
    echo "unknown program: $SINGLE_PROGRAM (must be one of: grid-token, emission, vesting, staking, burn)" >&2
    exit 2
  fi
fi

# Workspace root
WORKSPACE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$WORKSPACE"

# Check deployer balance
DEPLOYER_PUBKEY="$(solana-keygen pubkey "$KEYPAIR")"
BAL_SOL="$(solana --url devnet --keypair "$KEYPAIR" balance "$DEPLOYER_PUBKEY" 2>/dev/null | awk '{print $1}')"
if [[ -z "$BAL_SOL" ]]; then
  echo "[devnet-deploy] could not read balance for ${DEPLOYER_PUBKEY}" >&2
  exit 3
fi
echo "[devnet-deploy] deployer  : ${DEPLOYER_PUBKEY}"
echo "[devnet-deploy] balance   : ${BAL_SOL} SOL"

# Use awk for fractional comparison; we want >= 10 SOL
INSUFFICIENT="$(awk -v b="$BAL_SOL" 'BEGIN{print (b<10)?1:0}')"
if [[ "$INSUFFICIENT" == "1" ]]; then
  echo "[devnet-deploy] balance < 10 SOL — request devnet airdrop:" >&2
  echo "  solana airdrop 5 ${DEPLOYER_PUBKEY} --url devnet  (rate-limited; may need a few tries)" >&2
  echo "  or fund from a faucet: https://faucet.solana.com" >&2
  exit 4
fi

# Verify artefacts
declare -a ARTEFACTS
declare -a HASHES
for p in "${PROGRAMS[@]}"; do
  pkey="${p//-/_}"
  so="target/deploy/${pkey}.so"
  if [[ ! -f "$so" ]]; then
    echo "[devnet-deploy] missing artefact: $so" >&2
    echo "Run \`anchor build\` first." >&2
    exit 5
  fi
  ARTEFACTS+=("$so")
  HASHES+=("$(sha256sum "$so" | awk '{print $1}')")
done

# Extract devnet program IDs
declare -A DEVNET_IDS
while IFS= read -r line; do
  key="$(echo "$line" | awk '{print $1}')"
  val="$(echo "$line" | awk -F'"' '{print $2}')"
  if [[ -n "$key" && -n "$val" ]]; then
    DEVNET_IDS[$key]="$val"
  fi
done < <(awk '/^\[programs\.devnet\]/{f=1; next} /^\[/{f=0} f && /=/ {print}' Anchor.toml)

echo
echo "[devnet-deploy] pre-flight summary:"
printf "  %-12s %-12s %-44s %s\n" "PROGRAM" "ARTEFACT" "PROGRAM ID" "SHA256"
for i in "${!PROGRAMS[@]}"; do
  p="${PROGRAMS[$i]}"
  pkey="${p//-/_}"
  pid="${DEVNET_IDS[$pkey]:-MISSING}"
  printf "  %-12s %-12s %-44s %s\n" "$p" "${ARTEFACTS[$i]##*/}" "$pid" "${HASHES[$i]:0:16}"
done

# Confirmation gate
if [[ "${CONFIRM:-}" != "I_AGREE" ]]; then
  echo
  read -r -p "Type 'I_AGREE' to proceed: " ANSWER
  if [[ "$ANSWER" != "I_AGREE" ]]; then
    echo "[devnet-deploy] aborted." >&2
    exit 6
  fi
fi

# Deploy each
for i in "${!PROGRAMS[@]}"; do
  p="${PROGRAMS[$i]}"
  pkey="${p//-/_}"
  pid="${DEVNET_IDS[$pkey]}"
  so="${ARTEFACTS[$i]}"
  echo
  echo "[devnet-deploy] deploying ${p} -> ${pid}"
  solana program deploy \
    --url devnet \
    --keypair "$KEYPAIR" \
    --program-id "$pid" \
    "$so"
done

# Optional IDL push
if (( SKIP_IDL == 0 )); then
  echo
  echo "[devnet-deploy] publishing IDLs (delegating to ./scripts/idl-publish.sh)"
  "${WORKSPACE}/scripts/idl-publish.sh" --cluster devnet --keypair "$KEYPAIR"
fi

echo
echo "[devnet-deploy] DONE."
echo "[devnet-deploy] cross-reference SHA256s above against your release artefact for audit:"
for i in "${!PROGRAMS[@]}"; do
  echo "  ${PROGRAMS[$i]}  ${HASHES[$i]}"
done
