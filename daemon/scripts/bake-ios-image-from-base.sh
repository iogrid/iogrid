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
#     --xcode-oci ghcr.io/iogrid/iogrid-xcode:latest  # option A (DEFAULT, autonomous):
#       # an oras OCI artifact baked by .github/workflows/mirror-xcode-to-ghcr.yml
#       # (a runner's latest-stable Xcode 26, tar+zstd). No Apple login. Needs
#       # GHCR_USER + GHCR_TOKEN in the env for the private pull.
#       # --- OR ---
#     --xcode-xip /path/to/Xcode.xip                 # option B: a provided .xip
#       # --- OR ---
#     --xcodes-version 26.4                           # option C: xcodes downloads it
#       # (needs XCODES_APPLE_ID + XCODES_PASSWORD in the env; Apple auth)
#     [--keep-sim "iOS 18.2"]   # which iOS runtime to install (default: latest iOS)
#     [--disk-gb 90]            # grow the guest disk to this (sparse) size
#     [--push]                  # push the result to --out's registry
set -euo pipefail

BASE=""; OUT=""; XCODE_OCI=""; XCODE_TAR=""; XCODE_XIP=""; XCODES_VERSION=""; KEEP_SIM="iOS 18.2"; DISK_GB=90; PUSH=0
while [ $# -gt 0 ]; do
  case "$1" in
    --base) BASE="$2"; shift 2 ;;
    --out)  OUT="$2";  shift 2 ;;
    --xcode-oci) XCODE_OCI="$2"; shift 2 ;;
    --xcode-tar) XCODE_TAR="$2"; shift 2 ;;
    --xcode-xip) XCODE_XIP="$2"; shift 2 ;;
    --xcodes-version) XCODES_VERSION="$2"; shift 2 ;;
    --keep-sim) KEEP_SIM="$2"; shift 2 ;;
    --disk-gb) DISK_GB="$2"; shift 2 ;;
    --push) PUSH=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done
[ -n "$BASE" ] && [ -n "$OUT" ] || { echo "usage: --base <img> --out <img> (--xcode-oci <ref> | --xcode-tar <file.tar.zst> | --xcode-xip <f> | --xcodes-version <v>) [--keep-sim ..] [--disk-gb N] [--push]" >&2; exit 1; }
[ -n "$XCODE_OCI" ] || [ -n "$XCODE_TAR" ] || [ -n "$XCODE_XIP" ] || [ -n "$XCODES_VERSION" ] || { echo "need an Xcode source: --xcode-oci <ref> | --xcode-tar <file.tar.zst> | --xcode-xip <file> | --xcodes-version <v>" >&2; exit 1; }
command -v tart >/dev/null 2>&1 || { echo "tart not found" >&2; exit 1; }

VM="iogrid-base-bake-$$"
# cirruslabs base images auto-login admin/admin (same as the xcode images).
# Prepend Homebrew to PATH in every guest command — the base images ship brew
# at /opt/homebrew/bin but a non-interactive ssh shell doesn't load it, so
# `brew install zstd` (etc.) fails with "command not found: brew" otherwise.
ssh_vm() { sshpass -p admin ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null admin@"$1" "export PATH=/opt/homebrew/bin:\$PATH; $2"; }
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
if [ -n "$XCODE_TAR" ]; then
  # A local slim Xcode tarball (Xcode-<ver>.tar.zst from mirror-xcode-to-ghcr.yml,
  # relayed onto this Mac). scp it into the guest, decompress straight into
  # /Applications (stream — no intermediate .tar kept), then first-launch.
  [ -f "$XCODE_TAR" ] || { echo "[bake] --xcode-tar not found: $XCODE_TAR" >&2; exit 1; }
  echo "[bake] copying $(basename "$XCODE_TAR") into the guest…"
  scp_vm "$XCODE_TAR" "$IP" "/tmp/xcode.tar.zst"
  echo "[bake] expanding Xcode into /Applications + first-launch…"
  ssh_vm "$IP" "
    set -e
    command -v zstd >/dev/null 2>&1 || brew install zstd
    sudo rm -rf /Applications/Xcode.app
    zstd -dc /tmp/xcode.tar.zst | sudo tar -C /Applications -xf - && rm -f /tmp/xcode.tar.zst
    sudo xcode-select -s /Applications/Xcode.app
    sudo xcodebuild -license accept
    sudo xcodebuild -runFirstLaunch
  "
elif [ -n "$XCODE_OCI" ]; then
  # Pull the mirrored Xcode OCI artifact INSIDE the guest (it has the grown
  # disk; avoids a triple copy through the host). cirruslabs base images
  # ship Homebrew, so oras/zstd install cleanly.
  : "${GHCR_USER:?set GHCR_USER for the ghcr pull}"; : "${GHCR_TOKEN:?set GHCR_TOKEN}"
  echo "[bake] installing oras+zstd in the guest, pulling $XCODE_OCI…"
  ssh_vm "$IP" "command -v oras >/dev/null 2>&1 || brew install oras; command -v zstd >/dev/null 2>&1 || brew install zstd"
  ssh_vm "$IP" "echo '$GHCR_TOKEN' | oras login ghcr.io -u '$GHCR_USER' --password-stdin"
  echo "[bake] pulling + expanding Xcode (slow — ~15 GB pull, ~35 GB expand)…"
  ssh_vm "$IP" "
    set -e
    cd /tmp && rm -rf xc && mkdir xc && cd xc
    oras pull '$XCODE_OCI'
    TZ=\$(ls Xcode-*.tar.zst | head -1)
    zstd -d -T0 \"\$TZ\" -o xcode.tar && rm -f \"\$TZ\"
    sudo rm -rf /Applications/Xcode.app
    sudo tar -C /Applications -xf xcode.tar && rm -f xcode.tar
    sudo xcode-select -s /Applications/Xcode.app
    sudo xcodebuild -license accept
    sudo xcodebuild -runFirstLaunch
  "
elif [ -n "$XCODE_XIP" ]; then
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
