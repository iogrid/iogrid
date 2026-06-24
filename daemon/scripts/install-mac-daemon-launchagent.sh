#!/usr/bin/env bash
# install-mac-daemon-launchagent.sh — make a headless Mac a real iOS-build
# provider WITHOUT iogrid ever having SSH access.
#
# WHY THIS EXISTS: macOS Virtualization.framework refuses to boot a VM unless
# the launching process is inside a GUI (Aqua) login session — it needs that
# secure session to create the VM's HostKey. A daemon started over SSH, as a
# LaunchDaemon, or via nohup is NOT in an Aqua session, so the daemon's
# `tart run` dies with: VZErrorDomain -9 "Failed to create new HostKey"
# (confirmed live 2026-06-12 — every SSH-launched tart run failed this way).
#
# The fix every headless macOS CI uses, and what a provider runs ONCE at
# onboarding (the iogrid installer calls this — the provider never SSHes):
#   1) auto-login ON  -> a GUI session exists after every reboot
#   2) the iogrid daemon runs as a per-user LaunchAgent (Aqua session) -> its
#      `tart run` children inherit the secure session and VMs boot.
#
# Usage:
#   IOGRID_AUTOLOGIN_PASSWORD=<login-pw> [IOGRID_DAEMON_BIN=/path] \
#     ./install-mac-daemon-launchagent.sh --vpn-svc https://api.iogrid.org [daemon-args...]
set -euo pipefail

DAEMON_BIN="${IOGRID_DAEMON_BIN:-/usr/local/bin/iogridd}"
USER_NAME="${SUDO_USER:-$(id -un)}"
UID_NUM="$(id -u "$USER_NAME")"
HOME_DIR="$(dscl . -read "/Users/$USER_NAME" NFSHomeDirectory 2>/dev/null | awk '{print $2}')"
HOME_DIR="${HOME_DIR:-$HOME}"
PLIST="$HOME_DIR/Library/LaunchAgents/io.iogrid.daemon.plist"
LABEL="io.iogrid.daemon"

log() { printf '\033[1;34m[agent-install]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[agent-install] ERROR:\033[0m %s\n' "$*" >&2; exit 1; }

[ "$(uname -s)" = "Darwin" ] || die "macOS only."
[ -x "$DAEMON_BIN" ] || die "daemon binary not found/executable at $DAEMON_BIN (set IOGRID_DAEMON_BIN)."

# ── 1. Auto-login so a GUI session always exists (the VM HostKey needs it) ──
if [ -n "${IOGRID_AUTOLOGIN_PASSWORD:-}" ]; then
  log "enabling auto-login for '$USER_NAME' (a GUI session must survive reboots)…"
  sudo sysadminctl -autologin set -userName "$USER_NAME" -password "$IOGRID_AUTOLOGIN_PASSWORD" \
    && log "auto-login enabled ✓" || die "sysadminctl auto-login failed (FileVault on? auto-login needs it off)."
else
  log "IOGRID_AUTOLOGIN_PASSWORD not set — skipping auto-login enable."
  log "  A GUI session must still exist for VMs to boot. Verify with: launchctl managername"
fi

# ── 2. Write the LaunchAgent plist (runs the daemon IN the Aqua session) ───
log "writing $PLIST"
mkdir -p "$HOME_DIR/Library/LaunchAgents"
{
  printf '<?xml version="1.0" encoding="UTF-8"?>\n'
  printf '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n'
  printf '<plist version="1.0"><dict>\n'
  printf '  <key>Label</key><string>%s</string>\n' "$LABEL"
  printf '  <key>ProgramArguments</key><array>\n'
  printf '    <string>%s</string>\n' "$DAEMON_BIN"
  for a in "$@"; do printf '    <string>%s</string>\n' "$a"; done
  printf '  </array>\n'
  printf '  <key>RunAtLoad</key><true/>\n'
  printf '  <key>KeepAlive</key><true/>\n'
  printf '  <key>StandardOutPath</key><string>%s/Library/Logs/iogridd.log</string>\n' "$HOME_DIR"
  printf '  <key>StandardErrorPath</key><string>%s/Library/Logs/iogridd.log</string>\n' "$HOME_DIR"
  printf '  <key>ProcessType</key><string>Interactive</string>\n'
  printf '</dict></plist>\n'
} > "$PLIST"
chown "$USER_NAME" "$PLIST" 2>/dev/null || true

# ── 3. Bootstrap into the user's GUI (Aqua) domain so tart run gets a session ─
log "loading LaunchAgent into gui/$UID_NUM (the Aqua session)…"
launchctl bootout "gui/$UID_NUM/$LABEL" 2>/dev/null || true
if launchctl bootstrap "gui/$UID_NUM" "$PLIST" 2>/dev/null; then
  log "daemon LaunchAgent loaded ✓ — it now runs in the GUI session; tart run can boot VMs."
else
  log "bootstrap deferred — it will load at next GUI login (auto-login handles reboots)."
fi
log "DONE. The daemon runs in-session; iogrid never needs SSH to this Mac."
