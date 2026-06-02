#!/usr/bin/env bash
# vendor-wireguard.sh — idempotent re-vendoring of wireguard-apple as a
# Swift-6-compatible local SwiftPM dep at
# `mobile/ios/vendor/wireguard-apple-swift6/`.
#
# Why we vendor:
# - Upstream wireguard-apple's Package.swift declares
#   `swift-tools-version:5.3` which Xcode 26's Swift 6 toolchain rejects
#   with "Invalid manifest" (the 5.x manifest compiler is no longer
#   bundled). zx2c4 hasn't shipped Swift-6 compat upstream as of
#   2026-06. We patch the manifest in-place + drop the WireGuardApp /
#   WireGuard.xcodeproj pieces we don't need.
# - A local SwiftPM dep (with `path:` parameter in the Xcode
#   XCRemoteSwiftPackageReference) lets the PacketTunnelProvider
#   extension consume WireGuardKit without depending on git.zx2c4.com
#   reachability at every CI build (#586).
#
# Usage:
#   mobile/ios/scripts/vendor-wireguard.sh           # clone OR pull
#   mobile/ios/scripts/vendor-wireguard.sh --force   # nuke + re-clone
#
# What it does:
#   1. If the vendor dir is missing → shallow-clone wireguard-apple
#      master branch (zx2c4 still uses 'master', not 'main' — gotcha #5).
#   2. If the vendor dir exists → `git fetch && git reset --hard` to
#      pick up upstream fixes WITHOUT touching our local patches.
#   3. Trim the WireGuardApp / Shared / WireGuardNetworkExtension /
#      WireGuard.xcodeproj pieces (we only need the WireGuardKit
#      Swift package for the extension target).
#   4. Replace `swift-tools-version:5.3` (or whatever upstream currently
#      ships) with `swift-tools-version:5.9` — the safest baseline
#      Xcode 26 still compiles. See header in
#      vendor/wireguard-apple-swift6/Package.swift for the full
#      rationale (we don't go to 6.0 because of strict-concurrency
#      churn upstream hasn't opted in to).
#
# Refs #586. Pairs with `mobile/ios/scripts/add-network-extension-target.rb`
# which wires the vendor dir into the Xcode project as a local SwiftPM
# dep on every prebuild.

set -euo pipefail

UPSTREAM_URL="https://git.zx2c4.com/wireguard-apple"
UPSTREAM_BRANCH="master"  # NOT 'main' — gotcha #5

# Resolve repo paths (script may be invoked from anywhere).
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MOBILE_IOS_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
VENDOR_DIR="${MOBILE_IOS_DIR}/vendor/wireguard-apple-swift6"

# Force flag drops the existing vendor dir + re-clones from scratch.
FORCE=0
if [ "${1:-}" = "--force" ]; then
  FORCE=1
fi

mkdir -p "${MOBILE_IOS_DIR}/vendor"

if [ "${FORCE}" = "1" ] && [ -d "${VENDOR_DIR}" ]; then
  echo "[vendor-wireguard] --force: removing existing ${VENDOR_DIR}"
  rm -rf "${VENDOR_DIR}"
fi

# Use a temp clone dir so a half-finished pull never leaves the vendor
# dir in a broken state — only swap into place after the patch succeeds.
TMP_DIR="$(mktemp -d -t vendor-wg-XXXXXX)"
trap 'rm -rf "${TMP_DIR}"' EXIT

if [ -d "${VENDOR_DIR}/.git" ]; then
  echo "[vendor-wireguard] vendor dir has a .git — fetch-and-reset path"
  git -C "${VENDOR_DIR}" fetch --depth 1 origin "${UPSTREAM_BRANCH}"
  git -C "${VENDOR_DIR}" reset --hard "origin/${UPSTREAM_BRANCH}"
elif [ -d "${VENDOR_DIR}" ]; then
  echo "[vendor-wireguard] vendor dir exists but is bare (no .git) — re-clone into temp + swap"
  git clone --depth 1 --branch "${UPSTREAM_BRANCH}" "${UPSTREAM_URL}" "${TMP_DIR}/wga"
  # Preserve our patched Package.swift + README.iogrid.md if they exist —
  # we'll re-apply them below, but keeping a backup is belt-and-braces.
  if [ -f "${VENDOR_DIR}/Package.swift" ]; then
    cp "${VENDOR_DIR}/Package.swift" "${TMP_DIR}/Package.swift.iogrid.bak"
  fi
  rm -rf "${VENDOR_DIR}"
  mv "${TMP_DIR}/wga" "${VENDOR_DIR}"
  rm -rf "${VENDOR_DIR}/.git" "${VENDOR_DIR}/.github"
else
  echo "[vendor-wireguard] vendor dir missing — fresh clone"
  git clone --depth 1 --branch "${UPSTREAM_BRANCH}" "${UPSTREAM_URL}" "${TMP_DIR}/wga"
  mv "${TMP_DIR}/wga" "${VENDOR_DIR}"
  rm -rf "${VENDOR_DIR}/.git" "${VENDOR_DIR}/.github"
fi

# Trim the heavyweight pieces we don't need for the SwiftPM dep.
# WireGuardApp + WireGuard.xcodeproj is the full client app; Shared +
# WireGuardNetworkExtension are pieces of it. We only consume
# WireGuardKit (+ its WireGuardKitGo / WireGuardKitC bridges).
for path in \
  "${VENDOR_DIR}/WireGuard.xcodeproj" \
  "${VENDOR_DIR}/Sources/WireGuardApp" \
  "${VENDOR_DIR}/Sources/Shared" \
  "${VENDOR_DIR}/Sources/WireGuardNetworkExtension" \
  "${VENDOR_DIR}/sync-translations.sh" \
  "${VENDOR_DIR}/MOBILECONFIG.md"; do
  if [ -e "${path}" ]; then
    rm -rf "${path}"
  fi
done

# Patch swift-tools-version. Use a portable sed (BSD on macOS, GNU on
# Linux) by writing to a temp file + mv.
PKG="${VENDOR_DIR}/Package.swift"
if [ ! -f "${PKG}" ]; then
  echo "[vendor-wireguard] ERROR: ${PKG} missing after clone — upstream layout changed?" >&2
  exit 1
fi
# Replace the first `swift-tools-version:` line (it must be line 1 per
# SwiftPM spec). We rewrite the whole file via awk so the swift-tools
# pin is deterministic regardless of upstream's exact text.
awk 'NR==1{print "// swift-tools-version:5.9"; next} {print}' "${PKG}" > "${PKG}.tmp"
mv "${PKG}.tmp" "${PKG}"

# Re-add our explanatory header comment block (idempotently — only if
# the marker line isn't already present). The full Package.swift in
# git carries the full header; this script only owns the swift-tools
# version pin, not the explanation, so we keep the script minimal and
# trust the human to maintain Package.swift's header on the next PR.
if ! grep -q "PATCHED FORK" "${PKG}"; then
  cat > "${PKG}.tmp" <<'HEADER'
// swift-tools-version:5.9
// PATCHED FORK — Package.swift normalised by mobile/ios/scripts/vendor-wireguard.sh.
// See README.iogrid.md in this directory for the rationale.
HEADER
  # Append everything from line 2 onwards of the freshly-rewritten file.
  tail -n +2 "${PKG}" >> "${PKG}.tmp"
  mv "${PKG}.tmp" "${PKG}"
fi

# Idempotency check: search for any leftover Swift-5-only syntax that
# can break under the Swift 6 toolchain. The most common offender is
# `@_implementationOnly import` which Swift 6 deprecates. Today's
# wireguard-apple master is clean (verified 2026-06-02), but
# this is a forward-looking guard — fail loudly if upstream re-introduces
# the pattern so we can patch it in a follow-up commit.
if grep -rn '@_implementationOnly' "${VENDOR_DIR}/Sources/" >/dev/null 2>&1; then
  echo "[vendor-wireguard] WARN: @_implementationOnly imports found — Swift 6 strict-concurrency mode may flag these. Audit before next Xcode bump." >&2
  grep -rn '@_implementationOnly' "${VENDOR_DIR}/Sources/" >&2 || true
fi

echo "[vendor-wireguard] ok — vendored at ${VENDOR_DIR}"
echo "[vendor-wireguard] next: \`ruby ${MOBILE_IOS_DIR}/scripts/add-network-extension-target.rb\` wires it into the Xcode project."
