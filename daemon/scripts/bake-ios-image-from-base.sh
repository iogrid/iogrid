#!/usr/bin/env bash
# bake-ios-image-from-base.sh — build an iogrid iOS-build Tart image by
# starting from the SLIM cirruslabs *base* image (macOS, no Xcode, ~25 GB)
# and installing ONLY Xcode + the iOS SDK + one iOS simulator runtime INTO
# the guest. The opposite of bake-slim-ios-image.sh (#724), which pulls the
# ~80 GB *full* Xcode image and strips it down.
#
# WHY THIS EXISTS (the disk reality, 2026-06-11): the full cirruslabs
# xcode images are 63–66 GB COMPRESSED (≈90 GB on disk) — they don't fit on
# a real provider Mac. Measured on the dog-food Mac (Hatice's M1): even
# after cleanup it had ~66 GB free, so `tart pull macos-sequoia-xcode` can
# never complete there. The base image (25.4 GB compressed) DOES fit, so we
# add Xcode on top. This is the path that lets ordinary Macs participate.
# (ADR 0001 + #728 lazy-loading is the longer-term footprint win.)
#
# WHERE THIS RUNS: on the provider Mac itself (Apple Silicon, macOS 13+),
# or any Apple-Silicon Mac with ~60 GB free. Produces a local image; push
# to a private registry so other providers pull the finished result once.
#
# Sequoia guest on a Sonoma host is supported by Virtualization.framework /
# Tart (tart.run/faq). The guest's Xcode is independent of the host's.
#
# STATUS: authored from the Tart + Xcode CLI contracts; the non-Xcode legs
# (clone/boot/ssh/strip/tag) are testable as soon as the base image is
# pulled. The Xcode-install leg needs an Xcode source (see --xcode-* below)
# — that is the one operator-gated input. Treat sizes as targets until
# measured on a real bake.
#
# Usage:
#   bake-ios-image-from-base.sh \
#     --base ghcr.io/cirruslabs/macos-sequoia-base:latest \
#     --out  ghcr.io/emrahbaysal/iogrid-ios-builder:16.4 \
#     --xcode-xip /path/to/Xcode_16.4.xip          # option A: a provided .xip
#       # --- OR ---
#     --xcodes-version 16.4                          # option B: xcodes downloads it
#       # (needs XCODES_APPLE_ID + XCODES_PASSWORD in the env; Apple auth)
#     [--keep-sim "iOS 18.2"]   # which iOS runtime to install (default: latest iOS)
#     [--disk-gb 90]            # grow the guest disk to this (sparse) size
#     [--push]                  # push the result to --out's registry
set -euo pipefail

BASE=""; OUT=""; XCODE_XIP=""; XCODES_VERSION=""; KEEP_SIM="iOS 18.2"; DISK_GB=90; PUSH=0
while [ $# -gt 0 ]; do
  case "$1" in
    --base) BASE="$2"; shift 2 ;;
    --out)  OUT="$2";  shift 2 ;;
    --xcode-xip) XCODE_XIP="$2"; shift 2 ;;
    --xcodes-version) XCODES_VERSION="$2"; shift 2 ;;
    --keep-sim) KEEP_SIM="$2"; shift 2 ;;
    --disk-gb) DISK_GB="$2"; shift 2 ;;
    --push) PUSH=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done
[ -n "$BASE" ] && [ -n "$OUT" ] || { echo "usage: --base <img> --out <img> (--xcode-xip <f> | --xcodes-version <v>) [--keep-sim ..] [--disk-gb N] [--push]" >&2; exit 1; }
[ -n "$XCODE_XIP" ] || [ -n "$XCODES_VERSION" ] || { echo "need an Xcode source: --xcode-xip <file> OR --xcodes-version <v> (+ XCODES_APPLE_ID/XCODES_PASSWORD)" >&2; exit 1; }
command -v tart >/dev/null 2>&1 || { echo "tart not found" >&2; exit 1; }

VM="iogrid-base-bake-$$"
# cirruslabs base images auto-login admin/admin (same as the xcode images).
ssh_vm() { sshpass -p admin ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null admin@"$1" "$2"; }
scp_vm() { sshpass -p admin scp -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "$1" admin@"$2":"$3"; }
cleanup() { tart delete "$VM" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "[bake] pulling base $BASE (one-time ~25 GB — fits a provider Mac)…"
tart pull "$BASE"
echo "[bake] cloning → $VM ; growing disk to ${DISK_GB} GB (sparse — only used blocks cost)"
tart clone "$BASE" "$VM"
tart set "$VM" --disk-size "$DISK_GB"

echo "[bake] booting $VM headless…"
tart run --no-graphics "$VM" >/dev/null 2>&1 &
RUN_PID=$!
IP=""
for _ in $(seq 1 90); do IP="$(tart ip "$VM" 2>/dev/null || true)"; [ -n "$IP" ] && break; sleep 2; done
[ -n "$IP" ] || { echo "[bake] VM never got an IP" >&2; exit 1; }
echo "[bake] guest up at $IP"

# ── Install Xcode into the guest ──────────────────────────────────────────
if [ -n "$XCODE_XIP" ]; then
  [ -f "$XCODE_XIP" ] || { echo "[bake] --xcode-xip not found: $XCODE_XIP" >&2; exit 1; }
  echo "[bake] copying $(basename "$XCODE_XIP") into the guest (~8 GB)…"
  scp_vm "$XCODE_XIP" "$IP" "/tmp/Xcode.xip"
  echo "[bake] expanding + installing Xcode (slow — xip expand + first-launch component install)…"
  ssh_vm "$IP" "
    set -e
    cd /tmp && xip --expand Xcode.xip && rm -f Xcode.xip
    sudo rm -rf /Applications/Xcode.app
    sudo mv /tmp/Xcode*.app /Applications/Xcode.app
    sudo xcode-select -s /Applications/Xcode.app
    sudo xcodebuild -license accept
    sudo xcodebuild -runFirstLaunch
  "
else
  echo "[bake] installing Xcode $XCODES_VERSION via xcodes (Apple auth)…"
  : "${XCODES_APPLE_ID:?set XCODES_APPLE_ID}"; : "${XCODES_PASSWORD:?set XCODES_PASSWORD}"
  ssh_vm "$IP" "command -v xcodes >/dev/null || brew install xcodesorg/made/xcodes"
  ssh_vm "$IP" "XCODES_APPLE_ID='$XCODES_APPLE_ID' XCODES_PASSWORD='$XCODES_PASSWORD' xcodes install '$XCODES_VERSION' --experimental-unxip --select"
  ssh_vm "$IP" "sudo xcodebuild -license accept && sudo xcodebuild -runFirstLaunch"
fi

# ── Install ONLY the iOS platform + the one simulator runtime we keep ──────
echo "[bake] installing the iOS platform + '$KEEP_SIM' runtime; nothing else…"
ssh_vm "$IP" "xcodebuild -downloadPlatform iOS" || true
ssh_vm "$IP" "
  want='$KEEP_SIM'
  if ! xcrun simctl runtime list 2>/dev/null | grep -q \"\$want\"; then
    xcodebuild -downloadPlatform iOS -buildVersion \"\${want#iOS }\" 2>/dev/null || true
  fi
  xcrun simctl delete unavailable || true
"
# Strip anything non-iOS Xcode dragged in (defense-in-depth — base shouldn't
# have them, but -downloadPlatform can pull extras).
ssh_vm "$IP" "
  XC=\$(xcode-select -p)
  for p in AppleTVOS AppleTVSimulator WatchOS WatchSimulator XROS XRSimulator; do
    sudo rm -rf \"\$XC/Platforms/\$p.platform\" 2>/dev/null || true
  done
  sudo rm -rf ~/Library/Caches/* /Library/Caches/* /tmp/Xcode* 2>/dev/null || true
"

echo "[bake] shutting the guest down cleanly…"
ssh_vm "$IP" "sudo shutdown -h now" || true
wait "$RUN_PID" 2>/dev/null || true

echo "[bake] tagging → $OUT (measure real size: tart list)"
tart clone "$VM" "$OUT"
if [ "$PUSH" -eq 1 ]; then
  echo "[bake] pushing $OUT (requires tart login to the registry)…"
  tart push "$OUT"
fi
echo "[bake] DONE. iOS-build image: $OUT"
