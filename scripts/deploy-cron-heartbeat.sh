#!/usr/bin/env bash
# deploy-cron-heartbeat.sh — emit a one-line freshness verdict for the iogrid
# image-reroll deploy cron, so a SILENTLY-DEAD deploy path can never again
# freeze prod unnoticed (the #799 residual).
#
# THE FAILURE IT GUARDS (#799): the image-reroll cron
# (`scripts/iogrid-image-reroll-cron.sh`, `5,20,35,50 * * * *`) is the ONLY
# merged-PR → prod path (CI has no cluster creds; iogrid has no own Flux
# Kustomization). It was found NEVER INSTALLED → coordinator prod silently froze
# at May-21 images for ~25 days. Nothing alerted. This heartbeat makes the
# freshness of that cron VISIBLE on the founder-facing dashboard.
#
# HOW IT'S SURFACED: the DoD-dashboard refresher
# (`/home/openova/bin/refresh-dod-dashboard.sh`, every 15 min — an INDEPENDENT
# cron, so it keeps running even if the reroll cron dies) calls this and appends
# the emitted line into `docs/ledger/TRACKER.md`, which is committed+pushed to
# main and is what the founder reads. A separate watchdog cron means a dead
# deploy cron is reported by a process that is NOT itself dead.
#
# CONTRACT: prints exactly ONE markdown line to stdout. The reroll cron appends
# to its log every tick (even on a no-op), so the log's MTIME is a true
# heartbeat of "the cron fired".
#   * log missing               → 🔴 NEVER INSTALLED  (the exact #799 failure)
#   * log mtime older than STALE → 🔴 STALLED          (cron stopped firing)
#   * fresh                      → ✓ healthy
#
# Usage:
#   bash scripts/deploy-cron-heartbeat.sh [LOG_PATH] [STALE_SECONDS]
# Defaults: LOG_PATH=/tmp/iogrid-image-reroll.log  STALE_SECONDS=1500 (25 min;
# the cron runs every 15 min, so >25 min unambiguously means it stopped).
set -uo pipefail

LOG="${1:-${IOGRID_REROLL_LOG:-/tmp/iogrid-image-reroll.log}}"
STALE="${2:-${IOGRID_REROLL_STALE_SECONDS:-1500}}"

if [[ ! -e "$LOG" ]]; then
  printf '🔴 **DEPLOY CRON NEVER INSTALLED** — image-reroll log `%s` is missing; merged PRs are NOT reaching prod (this is the exact #799 freeze). Fix: `crontab -l` must contain the `iogrid-image-reroll-cron.sh` line (`5,20,35,50 * * * *`).\n' "$LOG"
  exit 0
fi

now=$(date +%s)
mtime=$(stat -c %Y "$LOG" 2>/dev/null || stat -f %m "$LOG" 2>/dev/null || echo 0)
age=$(( now - mtime ))
age_min=$(( age / 60 ))

if (( age > STALE )); then
  printf '🔴 **DEPLOY CRON STALLED** — image-reroll last ran %dm ago (> %dm threshold); merged PRs are NOT reaching prod. Fix: verify `crontab -l` has the `iogrid-image-reroll-cron.sh` line and check `/tmp/iogrid-image-reroll.log`.\n' "$age_min" "$(( STALE / 60 ))"
else
  printf '✓ deploy-cron healthy (image-reroll last ran %dm ago)\n' "$age_min"
fi
