#!/usr/bin/env bash
#
# local-validator.sh — boot solana-test-validator with the five $GRID programs deployed.
#
# Anchor's built-in `anchor localnet` is fine for one-off `anchor test` runs but doesn't
# leave a long-running validator that other developer tools (the coordinator billing-svc
# pointed at localhost:8899, web frontend RPC, manual `solana program show` queries) can
# attach to between tests.
#
# This script:
#   1. Verifies build artefacts exist (runs anchor build if not + --build was passed).
#   2. Kills any existing solana-test-validator (port 8899 / 8900).
#   3. Boots a fresh validator with the five programs preloaded via --bpf-program.
#   4. Funds a `dev` keypair (creates one at ~/.config/solana/iogrid-dev.json if absent).
#   5. Streams the validator log to /tmp/iogrid-validator.log and tails the tail.
#
# Usage:
#   ./scripts/local-validator.sh           # boot, assuming already built
#   ./scripts/local-validator.sh --build   # anchor build first
#   ./scripts/local-validator.sh --clean   # rm -rf test-ledger before boot
#   ./scripts/local-validator.sh --quiet   # suppress tailing
#
# Stop:
#   pkill -f solana-test-validator   # or kill -TERM <pid>
#
# Env overrides:
#   IOGRID_VALIDATOR_RPC_PORT   default 8899
#   IOGRID_VALIDATOR_WS_PORT    default 8900
#   IOGRID_VALIDATOR_FAUCET     default 9900
#   IOGRID_VALIDATOR_LOG        default /tmp/iogrid-validator.log
#   IOGRID_VALIDATOR_LEDGER     default ./test-ledger

set -euo pipefail

BUILD=0
CLEAN=0
QUIET=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --build) BUILD=1; shift ;;
    --clean) CLEAN=1; shift ;;
    --quiet) QUIET=1; shift ;;
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

# Resolve workspace root (this script lives in contracts/scripts/)
WORKSPACE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$WORKSPACE"

RPC_PORT="${IOGRID_VALIDATOR_RPC_PORT:-8899}"
WS_PORT="${IOGRID_VALIDATOR_WS_PORT:-8900}"
FAUCET_PORT="${IOGRID_VALIDATOR_FAUCET:-9900}"
LOG_FILE="${IOGRID_VALIDATOR_LOG:-/tmp/iogrid-validator.log}"
LEDGER_DIR="${IOGRID_VALIDATOR_LEDGER:-${WORKSPACE}/test-ledger}"
DEV_KEYPAIR="${HOME}/.config/solana/iogrid-dev.json"

PROGRAMS=(grid_token emission vesting staking burn)

if (( BUILD == 1 )); then
  echo "[local-validator] anchor build (full)..."
  anchor build
fi

# Verify build artefacts
MISSING=()
for p in "${PROGRAMS[@]}"; do
  so="target/deploy/${p}.so"
  if [[ ! -f "$so" ]]; then
    MISSING+=("$so")
  fi
done
if (( ${#MISSING[@]} > 0 )); then
  echo "[local-validator] Missing build artefacts:" >&2
  for m in "${MISSING[@]}"; do echo "  - $m" >&2; done
  echo "Run \`./scripts/local-validator.sh --build\` or \`anchor build\` first." >&2
  exit 4
fi

# Extract program IDs from Anchor.toml's [programs.localnet] section
declare -A PROG_IDS
while IFS= read -r line; do
  key="$(echo "$line" | awk '{print $1}')"
  val="$(echo "$line" | awk -F'"' '{print $2}')"
  if [[ -n "$key" && -n "$val" ]]; then
    PROG_IDS[$key]="$val"
  fi
done < <(awk '/^\[programs\.localnet\]/{f=1; next} /^\[/{f=0} f && /=/ {print}' Anchor.toml)

for p in "${PROGRAMS[@]}"; do
  if [[ -z "${PROG_IDS[$p]:-}" ]]; then
    echo "[local-validator] Could not resolve program id for $p in [programs.localnet]" >&2
    exit 5
  fi
done

# Kill any existing validator on our port
if lsof -t -iTCP:"$RPC_PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "[local-validator] killing existing validator on :${RPC_PORT}"
  lsof -t -iTCP:"$RPC_PORT" -sTCP:LISTEN | xargs -r kill -TERM
  sleep 2
fi

# Optionally clean the ledger
if (( CLEAN == 1 )) && [[ -d "$LEDGER_DIR" ]]; then
  echo "[local-validator] cleaning ${LEDGER_DIR}"
  rm -rf "$LEDGER_DIR"
fi

# Ensure dev keypair exists (used by developers to interact with the local validator)
if [[ ! -f "$DEV_KEYPAIR" ]]; then
  echo "[local-validator] generating dev keypair at ${DEV_KEYPAIR}"
  mkdir -p "$(dirname "$DEV_KEYPAIR")"
  solana-keygen new --no-bip39-passphrase -s -o "$DEV_KEYPAIR" >/dev/null
fi
DEV_PUBKEY="$(solana-keygen pubkey "$DEV_KEYPAIR")"

# Build --bpf-program arg list
BPF_ARGS=()
for p in "${PROGRAMS[@]}"; do
  BPF_ARGS+=(--bpf-program "${PROG_IDS[$p]}" "target/deploy/${p}.so")
done

# Boot
echo "[local-validator] launching solana-test-validator"
echo "  rpc       : http://localhost:${RPC_PORT}"
echo "  ws        : ws://localhost:${WS_PORT}"
echo "  faucet    : http://localhost:${FAUCET_PORT}"
echo "  ledger    : ${LEDGER_DIR}"
echo "  log       : ${LOG_FILE}"
echo "  dev pubkey: ${DEV_PUBKEY}"
echo

# Token-2022 needs to be cloneable from mainnet for fidelity (matches Anchor.toml's clone)
# but solana-test-validator ships with it built-in via --no-bpf-jit defaults; we still pass
# the clone arg for parity with `anchor test`.
solana-test-validator \
  --reset \
  --quiet \
  --ledger "$LEDGER_DIR" \
  --rpc-port "$RPC_PORT" \
  --faucet-port "$FAUCET_PORT" \
  --limit-ledger-size 50000000 \
  --clone-upgradeable-program TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb \
  --url https://api.mainnet-beta.solana.com \
  "${BPF_ARGS[@]}" \
  >"$LOG_FILE" 2>&1 &
VALIDATOR_PID=$!

echo "[local-validator] validator pid: ${VALIDATOR_PID}"

# Wait for RPC ready
for i in $(seq 1 60); do
  if curl -fs "http://localhost:${RPC_PORT}/health" >/dev/null 2>&1; then
    echo "[local-validator] RPC ready (took ${i}s)"
    break
  fi
  if (( i == 60 )); then
    echo "[local-validator] RPC did not come up in 60s — see ${LOG_FILE}" >&2
    exit 6
  fi
  sleep 1
done

# Fund the dev keypair
solana --url "http://localhost:${RPC_PORT}" airdrop 100 "$DEV_PUBKEY" >/dev/null
echo "[local-validator] airdropped 100 SOL to ${DEV_PUBKEY}"

# Verify each program is on-chain
echo
echo "[local-validator] on-chain program inventory:"
for p in "${PROGRAMS[@]}"; do
  pid="${PROG_IDS[$p]}"
  if solana --url "http://localhost:${RPC_PORT}" program show "$pid" >/dev/null 2>&1; then
    echo "  OK   ${p}   ${pid}"
  else
    echo "  MISS ${p}   ${pid}"
  fi
done

echo
echo "[local-validator] ready. To stop: pkill -f solana-test-validator"
echo "[local-validator] dev keypair  : ${DEV_KEYPAIR}"
echo "[local-validator] dev pubkey   : ${DEV_PUBKEY}"

if (( QUIET == 0 )); then
  echo "[local-validator] tailing ${LOG_FILE} (Ctrl-C to detach; validator keeps running)"
  tail -n 0 -f "$LOG_FILE"
fi
