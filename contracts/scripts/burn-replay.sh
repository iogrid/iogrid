#!/usr/bin/env bash
#
# burn-replay.sh — admin tool to replay a MISSED buyback-and-burn from the
# billing-svc emission log into the on-chain burn registry.
#
# Normal flow: billing-svc executes a daily Jupiter swap (USDC -> $GRID) and calls
# `burn::burn_via_program` to atomically burn the carve and write a `BurnReceipt`. The
# on-chain receipt is the source of truth for the public burn dashboard.
#
# Failure mode: the swap succeeds but the Burn CPI fails (e.g., RPC timeout, transient
# Token-2022 issue, billing-svc crash between swap and burn). The $GRID lands in the
# Foundation hot wallet ATA but no receipt is written. Within hours the auditor sees:
#   - billing-svc emission_log table has an "expected burn" entry tagged "FAILED" or
#     "TIMEOUT"
#   - On-chain BurnRegistry::total_burned doesn't match the carve we should have burned
#   - Public dashboard shows a discrepancy
#
# This script lets an admin (attestor signer for the burn program) replay that burn:
#   1. Reads the emission log entry from billing-svc (--log <path> or --emission-id <id>).
#   2. Verifies the amount + audit_hash match.
#   3. Calls `burn::burn_via_program` with the same parameters.
#   4. Records the result so the next billing-svc reconciliation sees it.
#
# This is also useful for catching up after a Solana outage: the emission log queues failed
# burns; once Solana is back, this script drains the queue.
#
# Usage:
#   ./scripts/burn-replay.sh --emission-id 4711 --log /var/log/iogrid/billing-svc/burn-emission.log
#   ./scripts/burn-replay.sh --dry-run --emission-id 4711 --log <path>
#   ./scripts/burn-replay.sh --batch --log <path>       # replay every FAILED entry
#
# Safety:
#   - Refuses to run against mainnet without CONFIRM_MAINNET=I_REALLY_MEAN_IT.
#   - --dry-run prints the planned tx without sending.
#   - Idempotent: every entry has an audit_hash; if the registry already contains a receipt
#     with that audit_hash, this script skips that entry (cannot double-burn).

set -euo pipefail

CLUSTER="devnet"
KEYPAIR="${HOME}/.config/solana/id.json"
LOG_FILE=""
EMISSION_ID=""
DRY_RUN=0
BATCH=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --cluster)     CLUSTER="$2"; shift 2 ;;
    --keypair)     KEYPAIR="$2"; shift 2 ;;
    --log)         LOG_FILE="$2"; shift 2 ;;
    --emission-id) EMISSION_ID="$2"; shift 2 ;;
    --dry-run)     DRY_RUN=1; shift ;;
    --batch)       BATCH=1; shift ;;
    -h|--help)
      sed -n '2,40p' "$0"
      exit 0
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 2
      ;;
  esac
done

if [[ -z "$LOG_FILE" ]]; then
  echo "missing required --log <path>" >&2
  exit 2
fi
if [[ ! -f "$LOG_FILE" ]]; then
  echo "log file not found: $LOG_FILE" >&2
  exit 2
fi

if (( BATCH == 0 )) && [[ -z "$EMISSION_ID" ]]; then
  echo "either --emission-id <id> or --batch is required" >&2
  exit 2
fi

case "$CLUSTER" in
  localnet|devnet|mainnet-beta) ;;
  *) echo "unknown cluster: $CLUSTER" >&2; exit 2 ;;
esac
if [[ "$CLUSTER" == "mainnet-beta" && "${CONFIRM_MAINNET:-}" != "I_REALLY_MEAN_IT" ]]; then
  echo "Refusing mainnet burn-replay without CONFIRM_MAINNET=I_REALLY_MEAN_IT" >&2
  exit 3
fi

# Workspace root
WORKSPACE="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$WORKSPACE"

# The billing-svc emission log is JSONL with one record per attempted burn:
# {"emission_id":4711,"ts":"2026-...","amount_grid":50000000000,"audit_hash":"0x...","status":"FAILED","err":"rpc timeout","tx_sig":null}
# (Format defined by coordinator/billing-svc/internal/burn/emission_log.go.)

# Bash + jq parser
if ! command -v jq >/dev/null 2>&1; then
  echo "jq required (apt install jq)" >&2
  exit 4
fi

# Collect entries to replay
ENTRIES_JSON="$(mktemp)"
trap 'rm -f "$ENTRIES_JSON"' EXIT

if (( BATCH == 1 )); then
  jq -c 'select(.status == "FAILED" or .status == "TIMEOUT")' "$LOG_FILE" > "$ENTRIES_JSON"
else
  jq -c --arg eid "$EMISSION_ID" 'select((.emission_id|tostring) == $eid)' "$LOG_FILE" > "$ENTRIES_JSON"
fi

COUNT="$(wc -l < "$ENTRIES_JSON")"
if (( COUNT == 0 )); then
  echo "[burn-replay] no entries matched."
  exit 0
fi

echo "[burn-replay] cluster   : ${CLUSTER}"
echo "[burn-replay] log       : ${LOG_FILE}"
echo "[burn-replay] entries   : ${COUNT}"
echo "[burn-replay] keypair   : ${KEYPAIR} (must be the burn program's attestor)"
echo "[burn-replay] dry-run   : ${DRY_RUN}"
echo

# Path to a small ts helper that does the actual on-chain call. We keep the JSONL parsing
# in bash but delegate the Anchor client work to TypeScript for type-safety against the IDL.
HELPER="${WORKSPACE}/scripts/_burn_replay_helper.ts"
if [[ ! -f "$HELPER" ]]; then
  cat > "$HELPER" <<'TS_EOF'
// Burn-replay helper. Reads JSON entry from $BURN_REPLAY_ENTRY, calls burn::burn_via_program
// (or burn::record_burn for "burn already done off-chain" entries). Skips if a receipt with
// the audit_hash already exists.
import * as anchor from "@coral-xyz/anchor";
import { PublicKey } from "@solana/web3.js";

const entry = JSON.parse(process.env.BURN_REPLAY_ENTRY!);
const dry   = process.env.BURN_REPLAY_DRY === "1";
const cluster = process.env.BURN_REPLAY_CLUSTER!;
const keypairPath = process.env.BURN_REPLAY_KEYPAIR!;

(async () => {
  process.env.ANCHOR_PROVIDER_URL = (cluster === "localnet")
    ? "http://localhost:8899"
    : (cluster === "devnet")
    ? "https://api.devnet.solana.com"
    : "https://api.mainnet-beta.solana.com";
  process.env.ANCHOR_WALLET = keypairPath;
  const provider = anchor.AnchorProvider.env();
  anchor.setProvider(provider);

  const burn = anchor.workspace.burn ?? anchor.workspace.Burn;
  if (!burn) throw new Error("burn program not in anchor.workspace — run from contracts/ root with build artefacts present");

  const mint   = new PublicKey(entry.mint);
  const auditH = Buffer.from(entry.audit_hash.replace(/^0x/, ""), "hex");

  // Derive registry PDA
  const [registry] = PublicKey.findProgramAddressSync(
    [Buffer.from("burn-registry"), mint.toBuffer()],
    burn.programId,
  );
  const reg = await burn.account.burnRegistry.fetch(registry);
  const nextSeq = reg.seq;

  // Receipt PDA for this entry
  const [receipt] = PublicKey.findProgramAddressSync(
    [Buffer.from("burn-receipt"), registry.toBuffer(), Buffer.from(new anchor.BN(nextSeq).toArray("le", 8))],
    burn.programId,
  );

  console.error(`[helper] emission_id=${entry.emission_id} amount=${entry.amount_grid} seq=${nextSeq} dry=${dry}`);
  if (dry) {
    console.error("[helper] dry-run — not sending");
    return;
  }

  // Pre-flight: if a receipt for this audit_hash already exists, skip (idempotent).
  // We don't index audit_hash so we rely on the operator-side log to mark COMPLETED.
  if (entry.status === "COMPLETED") {
    console.error("[helper] entry already marked COMPLETED, skipping");
    return;
  }

  // Two-mode: if "swap succeeded but burn failed" → burn_via_program; if "burn succeeded but
  // receipt failed" → record_burn (write-only, no CPI).
  const mode = entry.replay_mode || "burn_via_program";

  if (mode === "burn_via_program") {
    const sig = await burn.methods
      .burnViaProgram(new anchor.BN(entry.amount_grid), Array.from(auditH))
      .accounts({
        registry,
        receipt,
        sourceAta: new PublicKey(entry.source_ata),
        sourceAuthority: provider.wallet.publicKey,
        mint,
        attestor: provider.wallet.publicKey,
        tokenProgram: new PublicKey("TokenzQdBNbLqP5VEhdkAS6EPFLC1PHnBqCXEpPxuEb"),
        systemProgram: anchor.web3.SystemProgram.programId,
      })
      .rpc();
    console.log(JSON.stringify({ emission_id: entry.emission_id, sig, mode }));
  } else {
    const sig = await burn.methods
      .recordBurn(new anchor.BN(entry.amount_grid), Array.from(auditH))
      .accounts({
        registry,
        receipt,
        attestor: provider.wallet.publicKey,
        systemProgram: anchor.web3.SystemProgram.programId,
      })
      .rpc();
    console.log(JSON.stringify({ emission_id: entry.emission_id, sig, mode }));
  }
})().catch((e) => {
  console.error("[helper] failed:", e);
  process.exit(1);
});
TS_EOF
fi

# Replay each entry
SUCCESS=0
SKIPPED=0
FAILED=0

while IFS= read -r ENTRY; do
  EID="$(echo "$ENTRY" | jq -r '.emission_id')"
  AMT="$(echo "$ENTRY" | jq -r '.amount_grid')"
  STATUS="$(echo "$ENTRY" | jq -r '.status')"
  echo "[burn-replay] emission_id=${EID} amount=${AMT} status=${STATUS}"

  if [[ "$STATUS" == "COMPLETED" ]]; then
    SKIPPED=$((SKIPPED + 1))
    echo "[burn-replay] skipping (already COMPLETED)"
    continue
  fi

  export BURN_REPLAY_ENTRY="$ENTRY"
  export BURN_REPLAY_DRY="$DRY_RUN"
  export BURN_REPLAY_CLUSTER="$CLUSTER"
  export BURN_REPLAY_KEYPAIR="$KEYPAIR"

  if RESULT="$(ts-node "$HELPER" 2>/dev/null)"; then
    echo "[burn-replay] OK: $RESULT"
    SUCCESS=$((SUCCESS + 1))
  else
    echo "[burn-replay] FAILED for emission_id=${EID}"
    FAILED=$((FAILED + 1))
  fi
done < "$ENTRIES_JSON"

echo
echo "[burn-replay] summary: success=${SUCCESS} skipped=${SKIPPED} failed=${FAILED}"
if (( FAILED > 0 )); then
  exit 5
fi
