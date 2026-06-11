#!/usr/bin/env bash
# bake-slim-ios-image.sh — produce a SLIM iogrid iOS-build Tart image (~35 GB)
# from the cirruslabs full image (~80 GB) by stripping the non-iOS simulator
# runtimes + SDKs (watchOS/tvOS/visionOS) that an iOS build never touches.
# Halves every provider's disk footprint (ADR 0001).
#
# WHERE THIS RUNS: a BUILD machine (an Apple Silicon Mac with ~120 GB free —
# it needs the full base + working space at bake time). Providers then pull
# only the slim ~35 GB result. This is NOT run on provider Macs.
#
# STATUS: authored from the Tart CLI contract; NOT yet executed end-to-end
# (no Mac with ~120 GB free was available at authoring time — see ADR 0001's
# dog-food note). Validate on a build Mac before publishing. Treat the size
# numbers as targets, not guarantees, until measured.
#
# Usage:
#   bake-slim-ios-image.sh \
#     --base ghcr.io/cirruslabs/macos-sequoia-xcode:16.4 \
#     --out  ghcr.io/emrahbaysal/iogrid-ios-builder:16.4-slim \
#     [--keep-sim "iOS 18.2"]   # which iOS runtime to retain (default: latest iOS)
#     [--push]                  # push the result (else leave it local)
set -euo pipefail

BASE=""; OUT=""; KEEP_SIM=""; PUSH=0
while [ $# -gt 0 ]; do
  case "$1" in
    --base) BASE="$2"; shift 2 ;;
    --out)  OUT="$2";  shift 2 ;;
    --keep-sim) KEEP_SIM="$2"; shift 2 ;;
    --push) PUSH=1; shift ;;
    *) echo "unknown arg: $1" >&2; exit 1 ;;
  esac
done
[ -n "$BASE" ] && [ -n "$OUT" ] || { echo "usage: --base <img> --out <img> [--keep-sim ..] [--push]" >&2; exit 1; }
command -v tart >/dev/null 2>&1 || { echo "tart not found (brew install cirruslabs/cli/tart)" >&2; exit 1; }

VM="iogrid-slim-bake-$$"
ssh_vm() { sshpass -p admin ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null admin@"$1" "$2"; }
cleanup() { tart delete "$VM" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "[bake] pulling base $BASE (one-time ~80 GB)…"
tart pull "$BASE"
echo "[bake] cloning → $VM"
tart clone "$BASE" "$VM"

echo "[bake] booting $VM headless…"
tart run --no-graphics "$VM" >/dev/null 2>&1 &
RUN_PID=$!
# Wait for the guest to get an IP (the cirruslabs image auto-logins admin/admin).
IP=""
for _ in $(seq 1 60); do IP="$(tart ip "$VM" 2>/dev/null || true)"; [ -n "$IP" ] && break; sleep 2; done
[ -n "$IP" ] || { echo "[bake] VM never got an IP" >&2; exit 1; }
echo "[bake] guest up at $IP — stripping non-iOS platforms…"

# 1. delete every simulator runtime that isn't the iOS one we keep.
#    `xcrun simctl runtime list -j` is the source of truth; we delete tvOS/
#    watchOS/visionOS + any iOS runtime other than --keep-sim.
ssh_vm "$IP" "xcrun simctl delete unavailable || true"
ssh_vm "$IP" "
  for id in \$(xcrun simctl runtime list -j | python3 -c '
import json,sys
keep=\"\"\"$KEEP_SIM\"\"\".strip()
d=json.load(sys.stdin)
for k,v in d.items():
    name=v.get(\"name\",\"\")
    plat=v.get(\"platformIdentifier\",\"\")
    is_ios = \"iphonesimulator\" in plat or name.startswith(\"iOS\")
    if (not is_ios) or (keep and is_ios and keep not in name):
        print(v.get(\"runtimeIdentifier\") or k)
'); do echo deleting runtime \$id; xcrun simctl runtime delete \"\$id\" || true; done
"
# 2. remove non-iOS platform SDKs Xcode ships (watchOS/tvOS/xrOS Simulator +
#    device platforms). These live under the active Xcode's Platforms dir.
ssh_vm "$IP" "
  XC=\$(xcode-select -p)
  for p in AppleTVOS AppleTVSimulator WatchOS WatchSimulator XROS XRSimulator DriverKit; do
    sudo rm -rf \"\$XC/Platforms/\$p.platform\" 2>/dev/null || true
  done
  sudo rm -rf ~/Library/Caches/* /Library/Caches/* 2>/dev/null || true
"
echo "[bake] stripped. shutting the guest down cleanly…"
ssh_vm "$IP" "sudo shutdown -h now" || true
wait "$RUN_PID" 2>/dev/null || true

echo "[bake] tagging → $OUT"
# Re-tag the stopped, slimmed VM as the output image.
tart push "$VM" "$OUT" --populate-cache >/dev/null 2>&1 || tart clone "$VM" "$OUT"
if [ "$PUSH" -eq 1 ]; then
  echo "[bake] pushing $OUT (requires `tart login` to the registry)…"
  tart push "$OUT"
fi
echo "[bake] DONE. Slim image: $OUT  (measure actual size with: tart list)"
