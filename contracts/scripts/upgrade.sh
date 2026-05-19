#!/usr/bin/env bash
#
# upgrade.sh — re-deploy one of the $GRID programs using its upgrade authority.
#
# Anchor 0.31 uses Solana's BPF-Loader-Upgradeable program: every program has a Pubkey called
# the "upgrade authority" that can replace the on-chain bytecode without changing the program
# id. By default the deployer keypair is the upgrade authority. In production this should be
# rotated to the Squads multisig PDA (see TOKENOMICS.md §"Treasury custody").
#
# Usage:
#   ./scripts/upgrade.sh <program-name> <cluster> [--keypair <path>]
#
#   program-name   one of: grid-token | emission | vesting | staking | burn
#   cluster        one of: localnet | devnet | mainnet-beta
#   --keypair      override deployer keypair (default: ~/.config/solana/id.json)
#
# Examples:
#   ./scripts/upgrade.sh emission devnet
#   ./scripts/upgrade.sh grid-token mainnet-beta --keypair ~/.keys/foundation-hot-wallet.json
#
# Safety:
#   - Refuses to run if `anchor build` has not been performed (no target/deploy/*.so).
#   - Refuses to run against mainnet-beta unless $CONFIRM_MAINNET=I_REALLY_MEAN_IT.
#   - Logs the resulting Tx signature and bytecode hash so the change can be cross-referenced
#     in the Squads multisig audit log.

set -euo pipefail

if [[ $# -lt 2 ]]; then
  echo "usage: $0 <program-name> <cluster> [--keypair <path>]" >&2
  exit 2
fi

PROGRAM="$1"
CLUSTER="$2"
KEYPAIR="${HOME}/.config/solana/id.json"
shift 2
while [[ $# -gt 0 ]]; do
  case "$1" in
    --keypair)
      KEYPAIR="$2"
      shift 2
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

case "$PROGRAM" in
  grid-token|emission|vesting|staking|burn) ;;
  *)
    echo "unknown program: $PROGRAM (must be one of: grid-token, emission, vesting, staking, burn)" >&2
    exit 2
    ;;
esac

case "$CLUSTER" in
  localnet|devnet|mainnet-beta) ;;
  *)
    echo "unknown cluster: $CLUSTER" >&2
    exit 2
    ;;
esac

if [[ "$CLUSTER" == "mainnet-beta" && "${CONFIRM_MAINNET:-}" != "I_REALLY_MEAN_IT" ]]; then
  echo "Refusing mainnet upgrade without CONFIRM_MAINNET=I_REALLY_MEAN_IT" >&2
  exit 3
fi

# Resolve workspace root (this script lives in contracts/scripts/)
WORKSPACE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$WORKSPACE"

SO_PATH="target/deploy/${PROGRAM//-/_}.so"
if [[ ! -f "$SO_PATH" ]]; then
  echo "Build artefact missing: $SO_PATH" >&2
  echo "Run \`anchor build\` first." >&2
  exit 4
fi

# Extract program id from Anchor.toml for the target cluster
TOML_KEY="programs.${CLUSTER}"
if [[ "$CLUSTER" == "mainnet-beta" ]]; then
  TOML_KEY="programs.mainnet"
elif [[ "$CLUSTER" == "localnet" ]]; then
  TOML_KEY="programs.localnet"
fi

PROGRAM_KEY="${PROGRAM//-/_}"
PROGRAM_ID="$(awk -v section="[$TOML_KEY]" -v key="$PROGRAM_KEY" '
  $0 == section { in_section=1; next }
  /^\[/ { in_section=0 }
  in_section && $1 == key { print $3; exit }
' Anchor.toml | tr -d '"')"

if [[ -z "$PROGRAM_ID" ]]; then
  echo "Could not resolve program id for $PROGRAM on $CLUSTER in Anchor.toml" >&2
  exit 5
fi

echo "Upgrading $PROGRAM ($PROGRAM_ID) on $CLUSTER from $SO_PATH"
echo "Using upgrade authority keypair: $KEYPAIR"
echo

# Hash the .so so the result is reproducible-cross-referenceable
sha256sum "$SO_PATH"

# Solana CLI does the actual upgrade. The keypair must hold the program's upgrade authority.
solana program deploy \
  --url "$CLUSTER" \
  --keypair "$KEYPAIR" \
  --program-id "$PROGRAM_ID" \
  "$SO_PATH"

echo
echo "Upgrade complete for $PROGRAM on $CLUSTER."
echo "Cross-reference the tx signature above against the Squads multisig audit log."
