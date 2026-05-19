#!/bin/bash
# installer/macos/install-iogridd.sh
#
# Phase 0 — Install the iogrid provider daemon (`iogridd`) on macOS.
#
# This is the END-USER / FOUNDER installer for the actual provider
# daemon (the one missing on the Mac per issue #201 Layer 1). It is
# the companion to install-tunnel.sh (which installs the operator-only
# reverse-SSH tunnel — different LaunchAgent, different purpose).
#
# What this script does:
#   1. Detects arch (arm64 / amd64) and downloads the latest pre-built
#      `iogridd-darwin-<arch>` from GitHub Releases.
#   2. Installs the binary at /usr/local/bin/iogridd (or /opt/homebrew/bin/
#      on arm64 if /usr/local is read-only / shadowed).
#   3. Creates ~/.iogrid/config.toml with the coordinator URL.
#   4. Prompts for (or accepts via flag) the pairing token, runs
#      `iogridd pair --token=<...>` against the coordinator.
#   5. Installs the io.iogrid.daemon LaunchAgent in ~/Library/LaunchAgents/.
#   6. Bootstraps + enables + kickstarts the agent via launchctl.
#   7. Verifies the daemon is alive (launchctl list + pgrep + status).
#
# Usage:
#   ./installer/macos/install-iogridd.sh                              # interactive
#   ./installer/macos/install-iogridd.sh --pair-token=<TOKEN>         # CI/automation
#   COORDINATOR_URL=https://api-staging.iogrid.org \
#     ./installer/macos/install-iogridd.sh --pair-token=<TOKEN>       # staging
#
# Idempotent: re-running re-downloads if a newer release is available,
# re-templates the plist, and re-kickstarts the agent. Safe to run on
# every laptop bootstrap.
#
# Issue: https://github.com/iogrid/iogrid/issues/201 (Layer 1)

set -euo pipefail

# -----------------------------------------------------------------------------
# Defaults
# -----------------------------------------------------------------------------
COORDINATOR_URL="${COORDINATOR_URL:-https://api.iogrid.org}"
GH_REPO="${GH_REPO:-iogrid/iogrid}"
LABEL="io.iogrid.daemon"
CONFIG_DIR="$HOME/.iogrid"
CONFIG_FILE="$CONFIG_DIR/config.toml"
LOG_DIR="$CONFIG_DIR/logs"
PAIR_TOKEN=""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLIST_TEMPLATE="$SCRIPT_DIR/io.iogrid.daemon.plist"

log() { printf '[iogrid-daemon] %s\n' "$*"; }
die() { printf '[iogrid-daemon] ERROR: %s\n' "$*" >&2; exit 1; }
warn() { printf '[iogrid-daemon] WARN: %s\n' "$*" >&2; }

# -----------------------------------------------------------------------------
# Arg parse
# -----------------------------------------------------------------------------
for arg in "$@"; do
  case "$arg" in
    --pair-token=*) PAIR_TOKEN="${arg#--pair-token=}" ;;
    --coordinator=*) COORDINATOR_URL="${arg#--coordinator=}" ;;
    --uninstall) UNINSTALL=1 ;;
    -h|--help)
      cat <<EOF
Usage: $0 [options]
  --pair-token=<TOKEN>      pairing token from the coordinator UI
  --coordinator=<URL>       override coordinator URL
                            (default: https://api.iogrid.org)
  --uninstall               remove the daemon + LaunchAgent + config

Environment:
  COORDINATOR_URL           same as --coordinator
  GH_REPO                   GitHub repo for release downloads (default: iogrid/iogrid)
EOF
      exit 0
      ;;
    *) die "unknown argument: $arg (use --help)" ;;
  esac
done

# -----------------------------------------------------------------------------
# Uninstall path
# -----------------------------------------------------------------------------
if [ "${UNINSTALL:-0}" = "1" ]; then
  UID_NUM=$(id -u)
  log "uninstalling $LABEL"
  launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
  rm -f "$HOME/Library/LaunchAgents/${LABEL}.plist"
  rm -f /usr/local/bin/iogridd /opt/homebrew/bin/iogridd 2>/dev/null || true
  log "kept ${CONFIG_DIR} (delete manually if you want a clean slate):"
  log "  rm -rf $CONFIG_DIR"
  log "done."
  exit 0
fi

# -----------------------------------------------------------------------------
# Pre-flight checks
# -----------------------------------------------------------------------------
if [ "$(uname)" != "Darwin" ]; then
  die "this installer is macOS-only (uname=$(uname))"
fi

if ! command -v curl >/dev/null 2>&1; then
  die "curl not found (should be present on every Mac — check \$PATH)"
fi

if [ ! -f "$PLIST_TEMPLATE" ]; then
  die "plist template missing at $PLIST_TEMPLATE — clone the iogrid repo and re-run from inside installer/macos/"
fi

# Detect arch — Apple Silicon vs. Intel.
RAW_ARCH="$(uname -m)"
case "$RAW_ARCH" in
  arm64|aarch64) ARCH="arm64"; INSTALL_DIR="/opt/homebrew/bin" ;;
  x86_64|amd64)  ARCH="amd64"; INSTALL_DIR="/usr/local/bin" ;;
  *) die "unsupported arch: $RAW_ARCH" ;;
esac

# On arm64 we prefer /opt/homebrew/bin if it exists (matches Homebrew default),
# otherwise fall back to /usr/local/bin. On amd64 we always use /usr/local/bin.
if [ "$ARCH" = "arm64" ] && [ ! -d "$INSTALL_DIR" ]; then
  INSTALL_DIR="/usr/local/bin"
fi

INSTALL_PATH="$INSTALL_DIR/iogridd"

log "detected arch: $ARCH (target: $INSTALL_PATH)"
log "coordinator: $COORDINATOR_URL"

# -----------------------------------------------------------------------------
# Download latest release
# -----------------------------------------------------------------------------
RELEASE_API="https://api.github.com/repos/${GH_REPO}/releases/latest"
ASSET_NAME="iogridd-darwin-${ARCH}"

log "querying latest release from $GH_REPO..."
RELEASE_JSON=$(curl -fsSL -H 'Accept: application/vnd.github+json' "$RELEASE_API" 2>/dev/null || true)
if [ -z "$RELEASE_JSON" ]; then
  die "could not query GitHub Releases API. Check network + that $GH_REPO has at least one release."
fi

DOWNLOAD_URL=$(printf '%s' "$RELEASE_JSON" \
  | grep -oE "\"browser_download_url\": \"[^\"]*${ASSET_NAME}[^\"]*\"" \
  | head -1 \
  | sed -E 's/.*"(https[^"]+)"/\1/')

if [ -z "$DOWNLOAD_URL" ]; then
  die "no asset named '$ASSET_NAME' in the latest release. Available assets:
$(printf '%s' "$RELEASE_JSON" | grep -oE '"name": "[^"]+"' | sed -E 's/"name": "([^"]+)"/  \1/')"
fi

RELEASE_TAG=$(printf '%s' "$RELEASE_JSON" | grep -oE '"tag_name": "[^"]+"' | head -1 | sed -E 's/.*"([^"]+)"/\1/')
log "downloading $ASSET_NAME from release $RELEASE_TAG..."

TMP_BIN="$(mktemp -t iogridd.XXXXXX)"
trap 'rm -f "$TMP_BIN"' EXIT
curl -fsSL -o "$TMP_BIN" "$DOWNLOAD_URL"
chmod +x "$TMP_BIN"

# Sanity check
if ! "$TMP_BIN" version >/dev/null 2>&1; then
  warn "downloaded binary does not respond to 'iogridd version' — installing anyway"
fi

# Install (may need sudo for /usr/local/bin on Intel; arm64 Homebrew default
# is user-writeable).
if [ -w "$INSTALL_DIR" ] || [ ! -e "$INSTALL_DIR" ]; then
  mkdir -p "$INSTALL_DIR"
  mv "$TMP_BIN" "$INSTALL_PATH"
else
  log "installing to $INSTALL_PATH (sudo required)..."
  sudo mkdir -p "$INSTALL_DIR"
  sudo mv "$TMP_BIN" "$INSTALL_PATH"
  sudo chmod +x "$INSTALL_PATH"
fi

log "installed: $INSTALL_PATH"

# -----------------------------------------------------------------------------
# Config + logs
# -----------------------------------------------------------------------------
mkdir -p "$CONFIG_DIR" "$LOG_DIR"
chmod 700 "$CONFIG_DIR"

if [ -f "$CONFIG_FILE" ]; then
  log "existing config at $CONFIG_FILE — migrating coordinator_url only"
  # Replace any existing coordinator_url line (or append).
  if grep -q '^coordinator_url' "$CONFIG_FILE"; then
    # Cross-platform sed -i: use a backup ext then remove it.
    sed -i.bak "s|^coordinator_url.*|coordinator_url = \"${COORDINATOR_URL}\"|" "$CONFIG_FILE"
    rm -f "${CONFIG_FILE}.bak"
  else
    printf '\ncoordinator_url = "%s"\n' "$COORDINATOR_URL" >> "$CONFIG_FILE"
  fi
else
  log "writing fresh config at $CONFIG_FILE"
  cat > "$CONFIG_FILE" <<EOF
# iogrid provider daemon config
# Generated by installer/macos/install-iogridd.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ)

coordinator_url = "${COORDINATOR_URL}"

# Pairing token — filled in by \`iogridd pair --token=...\` below.
# Do not edit by hand unless you know what you're doing.
# pair_token = "..."

[provider]
# Display name shown in the coordinator UI. Defaults to hostname.
# display_name = ""

# Opt-in categories. social-intel is required for the Phase 0 LinkedIn
# fetch use case (vCard enrichment); add others as you grow comfortable.
categories = ["bandwidth-proxy"]
# categories = ["bandwidth-proxy", "social-intel"]

[bandwidth]
# Daily upload cap in GB. 0 = unlimited.
daily_cap_gb = 50

[logging]
level = "info"
EOF
  chmod 600 "$CONFIG_FILE"
fi

# -----------------------------------------------------------------------------
# Pairing
# -----------------------------------------------------------------------------
if [ -z "$PAIR_TOKEN" ]; then
  if [ -t 0 ]; then
    printf '[iogrid-daemon] paste pairing token (from %s/dashboard/devices/pair): ' "$COORDINATOR_URL"
    read -r PAIR_TOKEN
  else
    warn "no --pair-token and stdin is not a TTY; skipping pair step"
    warn "run later: $INSTALL_PATH pair --token=<TOKEN>"
  fi
fi

if [ -n "$PAIR_TOKEN" ]; then
  log "pairing with coordinator..."
  if "$INSTALL_PATH" pair --token="$PAIR_TOKEN" --config="$CONFIG_FILE"; then
    log "OK: paired"
  else
    warn "pair command exited non-zero — daemon will still start; re-run pair when ready"
  fi
fi

# -----------------------------------------------------------------------------
# LaunchAgent
# -----------------------------------------------------------------------------
mkdir -p "$HOME/Library/LaunchAgents"
DEST_PLIST="$HOME/Library/LaunchAgents/${LABEL}.plist"

sed \
  -e "s|__USER_HOME__|$HOME|g" \
  -e "s|__USER_NAME__|$(id -un)|g" \
  -e "s|__INSTALL_PATH__|$INSTALL_PATH|g" \
  -e "s|__CONFIG_FILE__|$CONFIG_FILE|g" \
  -e "s|__LOG_DIR__|$LOG_DIR|g" \
  "$PLIST_TEMPLATE" > "$DEST_PLIST"

log "wrote LaunchAgent: $DEST_PLIST"

# -----------------------------------------------------------------------------
# Bootstrap + enable + kickstart
# -----------------------------------------------------------------------------
UID_NUM=$(id -u)
TARGET="gui/$UID_NUM/$LABEL"

launchctl bootout "$TARGET" 2>/dev/null || true
if ! launchctl bootstrap "gui/$UID_NUM" "$DEST_PLIST"; then
  warn "launchctl bootstrap returned non-zero (already loaded?)"
fi
launchctl enable "$TARGET" 2>/dev/null || true
launchctl kickstart -k "$TARGET" 2>/dev/null \
  || warn "launchctl kickstart returned non-zero"

# -----------------------------------------------------------------------------
# Verify
# -----------------------------------------------------------------------------
sleep 2

VERIFY_OK=1
if launchctl list | grep -q "$LABEL"; then
  log "OK: $LABEL is loaded under launchctl"
else
  warn "FAIL: $LABEL did not appear in launchctl list — check $LOG_DIR/iogridd.err.log"
  VERIFY_OK=0
fi

if pgrep -af iogridd >/dev/null 2>&1; then
  log "OK: iogridd process is running"
  pgrep -af iogridd | sed 's/^/  /'
else
  warn "FAIL: no iogridd process visible — tail $LOG_DIR/iogridd.err.log"
  VERIFY_OK=0
fi

if "$INSTALL_PATH" status --config="$CONFIG_FILE" 2>/dev/null | grep -q -i 'connected\|ready\|paired'; then
  log "OK: iogridd status reports healthy"
else
  warn "iogridd status did not report healthy — may still be starting up"
fi

# -----------------------------------------------------------------------------
# Failure-mode recovery table
# -----------------------------------------------------------------------------
if [ "$VERIFY_OK" = "0" ]; then
  cat <<'EOF'

------------------------------------------------------------------------------
Failure-mode recovery table:

  Symptom                            | First thing to try
  -----------------------------------+----------------------------------------
  1. "no asset named iogridd-darwin" | check that the latest GitHub release
                                       in iogrid/iogrid published a Mac
                                       binary; rerun once the release CI
                                       attaches the asset.
  2. launchctl bootstrap exits 5     | service already loaded; run
                                       `launchctl bootout gui/$(id -u)/io.iogrid.daemon`
                                       then re-run this installer.
  3. iogridd not in `pgrep` after 30s| `tail -50 ~/.iogrid/logs/iogridd.err.log`
                                       — usual culprit is a stale pair token
                                       (re-run `iogridd pair --token=...`).
  4. status reports "not paired"     | the pair token was wrong / expired;
                                       grab a new one from the coordinator
                                       UI and run `iogridd pair --token=<NEW>`
                                       (no need to re-run this installer).
  5. SOCKS5 traffic still fails      | coordinator is unreachable. Confirm
                                       https://api.iogrid.org/healthz returns
                                       200 (issue #201 Layer 2/3 must be
                                       complete first; see docs/PHASE0-UNBLOCK.md).

To uninstall: re-run with --uninstall.
------------------------------------------------------------------------------

EOF
  exit 1
fi

log "done. Verify from the coordinator UI:"
log "  ${COORDINATOR_URL}/dashboard/devices  (expect this Mac listed as 'online')"
log ""
log "Tail logs with:"
log "  tail -f $LOG_DIR/iogridd.out.log"
log "  tail -f $LOG_DIR/iogridd.err.log"
log ""
log "To uninstall later:"
log "  $0 --uninstall"
