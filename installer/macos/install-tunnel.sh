#!/bin/bash
# installer/macos/install-tunnel.sh
#
# Phase 0 — Install the iogrid reverse-SSH tunnel LaunchAgent on macOS.
#
# This is OPERATOR-ONLY tooling. End-user iogrid providers do NOT need
# this; the tunnel exists so the bastion-side automation can reach the
# founder's Mac to drive desktop integration work (vCard, design review,
# etc.) during the Phase 0 internal pilot.
#
# See docs/PHASE0-SETUP.md for the operator walkthrough (generating the
# pinned key, registering it with the bastion, etc.).
#
# Usage:
#   ./installer/macos/install-tunnel.sh                  # interactive defaults
#   BASTION_HOST=144.91.121.182 BASTION_USER=openova \
#     REMOTE_PORT=2223 KEY_PATH=$HOME/.iogrid/id_ed25519 \
#     ./installer/macos/install-tunnel.sh
#
# Idempotent: re-running re-templates the plist and re-kickstarts the
# agent — safe to run on every laptop bootstrap.

set -euo pipefail

# --- Defaults (match the founder's working setup, snapshotted 2026-05-19) ---
BASTION_HOST="${BASTION_HOST:-144.91.121.182}"
BASTION_USER="${BASTION_USER:-openova}"
REMOTE_PORT="${REMOTE_PORT:-2223}"
KEY_PATH="${KEY_PATH:-$HOME/.iogrid/id_ed25519}"
LABEL="org.iogrid.tunnel"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="$SCRIPT_DIR/io.iogrid.tunnel.plist"

log() { printf '[iogrid-tunnel] %s\n' "$*"; }
die() { printf '[iogrid-tunnel] ERROR: %s\n' "$*" >&2; exit 1; }

# --- Pre-flight checks ---
if [ "$(uname)" != "Darwin" ]; then
    die "this installer is macOS-only (uname=$(uname))"
fi

if ! command -v autossh >/dev/null 2>&1; then
    die "autossh not found. Install it with: brew install autossh"
fi

if [ ! -f "$TEMPLATE" ]; then
    die "plist template missing at $TEMPLATE"
fi

if [ ! -f "$KEY_PATH" ]; then
    die "ssh key missing at $KEY_PATH — see docs/PHASE0-SETUP.md for keygen steps"
fi

if [ ! -f "$HOME/.iogrid/known_hosts" ]; then
    die "pinned known_hosts missing at $HOME/.iogrid/known_hosts — see docs/PHASE0-SETUP.md"
fi

# Resolve absolute autossh path so launchd picks the same binary the
# operator does. Homebrew arm64 default is /opt/homebrew/bin/autossh;
# Intel default is /usr/local/bin/autossh.
AUTOSSH_BIN="$(command -v autossh)"

mkdir -p "$HOME/Library/LaunchAgents"
mkdir -p "$HOME/.iogrid"

DEST="$HOME/Library/LaunchAgents/${LABEL}.plist"

# --- Template substitution ---
# We replace four placeholders. Using `|` as sed delimiter because
# $HOME paths contain `/`.
sed \
    -e "s|__USER_HOME__|$HOME|g" \
    -e "s|__REMOTE_PORT__|$REMOTE_PORT|g" \
    -e "s|__BASTION_USER__|$BASTION_USER|g" \
    -e "s|__BASTION_HOST__|$BASTION_HOST|g" \
    "$TEMPLATE" > "$DEST"

# If autossh is at a non-default path (e.g. Intel Mac), patch the plist
# to use it. The template ships with /opt/homebrew/bin/autossh which is
# the arm64 Homebrew default.
if [ "$AUTOSSH_BIN" != "/opt/homebrew/bin/autossh" ]; then
    sed -i.bak \
        -e "s|/opt/homebrew/bin/autossh|$AUTOSSH_BIN|g" \
        "$DEST"
    rm -f "$DEST.bak"
fi

log "wrote LaunchAgent: $DEST"
log "  bastion: ${BASTION_USER}@${BASTION_HOST}"
log "  remote port: ${REMOTE_PORT} -> localhost:22"
log "  key: ${KEY_PATH}"
log "  autossh: ${AUTOSSH_BIN}"

# --- Load / reload via launchctl ---
UID_NUM=$(id -u)
TARGET="gui/$UID_NUM/$LABEL"

launchctl bootout "$TARGET" 2>/dev/null || true
if ! launchctl bootstrap "gui/$UID_NUM" "$DEST"; then
    log "WARN: launchctl bootstrap returned non-zero (already loaded?)"
fi
launchctl enable "$TARGET" 2>/dev/null || true
launchctl kickstart -k "$TARGET" 2>/dev/null \
    || log "WARN: launchctl kickstart returned non-zero"

# --- Verify ---
sleep 2
if launchctl list | grep -q "$LABEL"; then
    log "OK: $LABEL is loaded under launchctl"
else
    die "$LABEL did not appear in launchctl list — check $HOME/.iogrid/autossh.err"
fi

if pgrep -f "autossh.*${BASTION_USER}@${BASTION_HOST}" >/dev/null 2>&1; then
    log "OK: autossh process is running"
else
    log "WARN: autossh not yet visible in process list; ThrottleInterval=15 means"
    log "       it may take a few seconds to come up. Tail $HOME/.iogrid/autossh.err"
fi

log "done. Verify from the bastion with:"
log "    ssh -p ${REMOTE_PORT} -i ~/.ssh/<your-key> <macuser>@localhost 'hostname'"
