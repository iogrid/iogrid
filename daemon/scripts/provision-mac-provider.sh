#!/usr/bin/env bash
# provision-mac-provider.sh — one-shot toolchain setup for an iogrid iOS-build
# provider. This is what makes "any Mac owner plugs in and earns" true WITHOUT
# the owner ever installing Xcode by hand: builds run in ephemeral Tart VMs
# from a pre-baked image (Xcode + SDK + simulators baked in), so the host only
# needs Tart + disk. Invoked by the daemon on first iOS-build dispatch (or run
# manually during onboarding). Idempotent — safe to re-run.
#
# Usage:
#   provision-mac-provider.sh [--xcode <version>] [--check-only]
#
#   --xcode <v>    Pre-pull this Xcode image (default: latest). Must match an
#                  entry in coordinator/services/build-gateway/internal/xcode.
#   --check-only   Validate host prereqs + report; install/pull nothing.
#
# Exit codes: 0 ok · 10 not Apple Silicon · 11 macOS too old · 12 low disk ·
#             13 tart install failed · 14 image pull failed.
set -euo pipefail

XCODE_VERSION="latest"
CHECK_ONLY=0
# Minimum free space for the cached image (~60-80 GB) + an ephemeral clone.
MIN_FREE_GB=90
# Image map mirrors build-gateway/internal/xcode/versions.go. Keep in sync.
# Tahoe + Xcode 26 is the default: Expo SDK 56 / RN 0.85 needs the iOS 26 SDK
# (Swift 6.2), which only Xcode 26 ships. Cirrus Labs bakes Xcode + SDK +
# simulators into these images, so the provider's daemon just pulls one — no
# Apple ID, no manual Xcode install, ever.
declare -a KNOWN_VERSIONS=("latest" "26" "26.1" "26.1.1" "16.2" "16.1" "16.0" "15.4" "15.3" "15.2")
image_for() {
  case "$1" in
    latest)   echo "ghcr.io/cirruslabs/macos-tahoe-xcode:latest" ;;
    26)       echo "ghcr.io/cirruslabs/macos-tahoe-xcode:26" ;;
    26.1)     echo "ghcr.io/cirruslabs/macos-tahoe-xcode:26.1" ;;
    26.1.1)   echo "ghcr.io/cirruslabs/macos-tahoe-xcode:26.1.1" ;;
    16.2)     echo "ghcr.io/cirruslabs/macos-sequoia-xcode:16.2" ;;
    16.1)     echo "ghcr.io/cirruslabs/macos-sequoia-xcode:16.1" ;;
    16.0)     echo "ghcr.io/cirruslabs/macos-sequoia-xcode:16.0" ;;
    15.4)     echo "ghcr.io/cirruslabs/macos-sonoma-xcode:15.4" ;;
    15.3)     echo "ghcr.io/cirruslabs/macos-sonoma-xcode:15.3" ;;
    15.2)     echo "ghcr.io/cirruslabs/macos-sonoma-xcode:15.2" ;;
    *)        return 1 ;;
  esac
}

while [ $# -gt 0 ]; do
  case "$1" in
    --xcode) XCODE_VERSION="$2"; shift 2 ;;
    --check-only) CHECK_ONLY=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done

log()  { printf '\033[1;34m[provision]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[provision] WARN:\033[0m %s\n' "$*"; }
die()  { printf '\033[1;31m[provision] ERROR:\033[0m %s\n' "$2" >&2; exit "$1"; }

# ── 1. Apple Silicon (hard requirement — macOS VMs need it) ──────────────
if [ "$(uname -s)" != "Darwin" ]; then die 10 "not macOS (uname=$(uname -s))"; fi
if [ "$(uname -m)" != "arm64" ]; then
  die 10 "Intel Mac detected. iOS-build providers require Apple Silicon (M1/M2/M3/M4): Tart can only virtualize macOS on Apple Silicon."
fi
log "host: Apple Silicon Mac ✓"

# ── 2. macOS 13+ (Virtualization.framework + recent Tart) ────────────────
macos_major="$(sw_vers -productVersion | cut -d. -f1)"
if [ "${macos_major:-0}" -lt 13 ]; then
  die 11 "macOS $(sw_vers -productVersion) is too old; need macOS 13 (Ventura)+."
fi
log "host: macOS $(sw_vers -productVersion) ✓"

# ── 3. Disk: the REAL cost (image ~60-80 GB + clone). Not Xcode. ─────────
free_gb="$(df -g / | awk 'NR==2 {print $4}')"
if [ "${free_gb:-0}" -lt "$MIN_FREE_GB" ]; then
  warn "only ${free_gb} GB free on / — recommend ≥ ${MIN_FREE_GB} GB for the Xcode VM image + an ephemeral clone."
  [ "$CHECK_ONLY" -eq 1 ] || die 12 "insufficient free disk (${free_gb} GB < ${MIN_FREE_GB} GB). Free space, then re-run."
else
  log "host: ${free_gb} GB free ✓"
fi

# Resolve the image up front so --check-only validates the version too.
if ! IMAGE="$(image_for "$XCODE_VERSION")"; then
  die 1 "unknown Xcode version '$XCODE_VERSION'. Known: ${KNOWN_VERSIONS[*]}"
fi
log "target image: $IMAGE"

if [ "$CHECK_ONLY" -eq 1 ]; then
  log "check-only: host is eligible to be an iOS-build provider."
  exit 0
fi

# ── 3b. Native-runner toolchain (#832) ───────────────────────────────────
# When the host runs the NATIVE iOS-build runner (no Tart VM — the case for a
# trusted dev Mac, e.g. Hatice's), builds use the HOST's CocoaPods + Ruby and
# leak per-build scratch under $TMPDIR. Two recurring env regressions block all
# builds unless provisioned durably:
#   (a) `pod` resolves to a stale gem-installed CocoaPods 1.11.3 under system
#       Ruby 2.6 in /usr/local/bin (ahead of Homebrew on /etc/paths). 1.11.3 on
#       Ruby 2.6 crashes `pod install` on Enumerable#filter_map (Ruby ≥2.7).
#   (b) the native runner's $TMPDIR/iogridd-ios-<uuid> scratch never gets
#       cleaned → disk creeps to ENOSPC and every build fails.
# This block makes the host correct for native builds; idempotent.
if command -v brew >/dev/null 2>&1; then
  # (a) Ensure a Homebrew Ruby ≥2.7 + CocoaPods, and make brew's `pod` win.
  if ! brew list ruby >/dev/null 2>&1; then
    log "installing Homebrew Ruby (≥2.7, for CocoaPods)…"
    brew install ruby || warn "brew install ruby failed — CocoaPods may fall back to system Ruby 2.6"
  fi
  BREW_PREFIX="$(brew --prefix 2>/dev/null || echo /opt/homebrew)"
  if ! "$BREW_PREFIX/opt/ruby/bin/gem" list -i cocoapods >/dev/null 2>&1; then
    log "installing CocoaPods under Homebrew Ruby…"
    brew install cocoapods || warn "brew install cocoapods failed"
  fi
  # Neutralise a stale /usr/local/bin CocoaPods so brew's wins on PATH. (The
  # daemon's native runner also prepends /opt/homebrew/bin in its preamble, but
  # disabling the shadow makes ANY shell on the host correct too.)
  for f in pod sandbox-pod xcodeproj; do
    if [ -e "/usr/local/bin/$f" ]; then
      head -1 "/usr/local/bin/$f" 2>/dev/null | grep -q "Ruby.framework/Versions/2.6" && {
        log "disabling stale system-Ruby /usr/local/bin/$f (shadows Homebrew CocoaPods)…"
        sudo mv "/usr/local/bin/$f" "/usr/local/bin/$f.disabled-ruby26" 2>/dev/null \
          || warn "could not rename /usr/local/bin/$f (need sudo); ensure /opt/homebrew/bin precedes it on PATH"
      }
    fi
  done
  if /bin/bash -lc 'export PATH=/opt/homebrew/bin:$PATH; pod --version' >/dev/null 2>&1; then
    log "pod $(/bin/bash -lc 'export PATH=/opt/homebrew/bin:$PATH; pod --version' 2>/dev/null) (Homebrew Ruby) ✓"
  else
    warn "pod not runnable under Homebrew Ruby — check 'brew install cocoapods'"
  fi
else
  warn "Homebrew not found — native CocoaPods builds will use system Ruby 2.6 and break on filter_map."
fi

# (b) Install the scratch-prune cron so native-runner build dirs can't fill the
# disk (rm $TMPDIR/iogridd-ios-* older than 60 min when no build is running).
PRUNE="$HOME/bin/iogridd-scratch-prune.sh"
mkdir -p "$HOME/bin"
cat > "$PRUNE" <<'PRUNE_EOF'
#!/bin/bash
# iogrid #832: prune stale native iOS-build scratch the NativeRunner leaves in
# $TMPDIR (iogridd-ios-<uuid>). Skips while a build is actively running.
set -u
TMP="${TMPDIR:-/tmp}"
if pgrep -qf 'xcodebuild' || pgrep -qf 'pod install'; then exit 0; fi
find "$TMP" -maxdepth 1 -type d -name 'iogridd-ios-*' -mmin +60 -prune -exec rm -rf {} + 2>/dev/null
find "$TMP" -maxdepth 1 -type d -name 'iogrid-*-dd'   -mmin +120 -prune -exec rm -rf {} + 2>/dev/null
exit 0
PRUNE_EOF
chmod +x "$PRUNE"
if ! crontab -l 2>/dev/null | grep -q 'iogridd-scratch-prune'; then
  log "installing scratch-prune cron (*/30)…"
  ( crontab -l 2>/dev/null; echo '*/30 * * * * /bin/bash -lc "$HOME/bin/iogridd-scratch-prune.sh" >> /tmp/iogridd-scratch-prune.log 2>&1' ) | crontab - \
    || warn "could not install scratch-prune cron — install it manually"
else
  log "scratch-prune cron already installed ✓"
fi

# ── 4. Tart — install if missing (the only host dependency) ──────────────
if ! command -v tart >/dev/null 2>&1; then
  log "installing Tart (cirruslabs/cli/tart) via Homebrew…"
  if ! command -v brew >/dev/null 2>&1; then
    die 13 "Homebrew not found. Install from https://brew.sh, then re-run (or install Tart from https://tart.run)."
  fi
  brew install cirruslabs/cli/tart || die 13 "tart install failed"
fi
log "tart $(tart --version 2>/dev/null || echo present) ✓"

# ── 4b. GUI (Aqua) session — THE non-obvious hard requirement ────────────
# macOS Virtualization.framework refuses to boot a VM unless the launching
# process lives in a GUI (Aqua) login session: it needs that secure session to
# create the VM's HostKey. A daemon started over SSH, as a LaunchDaemon, or via
# nohup is NOT in an Aqua session, so `tart run` dies with
#   VZErrorDomain Code=-9 "security error" … "Failed to create new HostKey".
# (Confirmed live 2026-06-12: every SSH-launched `tart run` failed exactly
# this way; the daemon must run in-session instead.) This is why a real
# provider never needs to give iogrid SSH access — the daemon IS the in-session
# agent. The two requirements, which every headless macOS CI also uses:
#   (a) auto-login ON, so a GUI session exists after every reboot;
#   (b) the iogrid daemon runs as a per-user LaunchAgent (Aqua session), so its
#       `tart run` children inherit the secure session.
if launchctl managername 2>/dev/null | grep -qiE 'aqua|gui'; then
  log "GUI (Aqua) login session detected ✓ — Tart can create VM HostKeys here."
else
  warn "NOT in a GUI session. Tart cannot boot VMs from here (HostKey error)."
  warn "The iogrid daemon must run as a per-user LaunchAgent inside an auto-login"
  warn "GUI session — not over SSH / LaunchDaemon / nohup. Onboarding must:"
  warn "  1) enable auto-login:  sudo sysadminctl -autologin set -userName <you> -password <pw>"
  warn "  2) install the daemon LaunchAgent at ~/Library/LaunchAgents/io.iogrid.daemon.plist"
  warn "     (RunAtLoad=true) and 'launchctl bootstrap gui/\$(id -u) <plist>'."
  warn "Without a GUI session the host cannot run iOS-build workloads."
  [ "$CHECK_ONLY" -eq 1 ] || die 15 "no GUI session — set up auto-login + the daemon LaunchAgent (see above), then re-run."
fi

# ── 5. Pre-pull the Xcode image so the first real build doesn't pay for it ─
if tart list 2>/dev/null | awk '{print $NF}' | grep -qx "$IMAGE"; then
  log "image already cached locally ✓"
else
  log "pulling $IMAGE (one-time, ~60-80 GB — this takes a while)…"
  tart pull "$IMAGE" || die 14 "image pull failed (check ghcr.io reachability + disk)"
  log "image pulled ✓"
fi

log "DONE — this Mac is a ready iOS-build provider. Builds run in throwaway"
log "Tart VMs from $IMAGE; the host never installs Xcode."
