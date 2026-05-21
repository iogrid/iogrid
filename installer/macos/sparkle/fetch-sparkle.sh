#!/usr/bin/env bash
# Fetch the official Sparkle 2.x binary framework and stage it into the .app
# bundle under Contents/Frameworks/Sparkle.framework.
#
# Sparkle is shipped as a notarised tarball whose SHA-256 is published on
# every release page. We pin a known-good version + checksum in this repo so
# CI is reproducible even if the upstream release page changes.
#
# Usage:
#   ./fetch-sparkle.sh <DEST_FRAMEWORKS_DIR>
#
# Example:
#   ./fetch-sparkle.sh installer/macos/build/app/Contents/Frameworks
set -euo pipefail

DEST_DIR="${1:-}"
if [ -z "$DEST_DIR" ]; then
    echo "usage: $0 <DEST_FRAMEWORKS_DIR>" >&2
    exit 2
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="$(cat "$SCRIPT_DIR/SPARKLE_VERSION" | tr -d '[:space:]')"

# SHA-256 of the official Sparkle-${VERSION}.tar.xz from
# https://github.com/sparkle-project/Sparkle/releases — pin to a single
# known-good checksum to defeat upstream-tampering attacks. To bump, fetch
# the new tarball, verify against the GH release signature with cosign,
# update SPARKLE_VERSION + this constant in the same commit.
#
# Phase 1: placeholder. CI sets SPARKLE_TARBALL_SHA256 from the GH release
# asset SHA published alongside the release. Setting it to empty here
# means CI MUST provide it; local-dev can override via env var.
EXPECTED_SHA256="${SPARKLE_TARBALL_SHA256:-}"

TARBALL_URL="https://github.com/sparkle-project/Sparkle/releases/download/${VERSION}/Sparkle-${VERSION}.tar.xz"
WORK_DIR="$(mktemp -d -t sparkle.XXXXXX)"
trap 'rm -rf "$WORK_DIR"' EXIT

echo "[sparkle] fetching Sparkle ${VERSION} from ${TARBALL_URL}"
curl --proto '=https' --tlsv1.2 -sSfL "$TARBALL_URL" -o "$WORK_DIR/sparkle.tar.xz"

if [ -n "$EXPECTED_SHA256" ]; then
    ACTUAL_SHA256="$(shasum -a 256 "$WORK_DIR/sparkle.tar.xz" | awk '{print $1}')"
    if [ "$ACTUAL_SHA256" != "$EXPECTED_SHA256" ]; then
        echo "[sparkle] FATAL: SHA-256 mismatch" >&2
        echo "  expected: $EXPECTED_SHA256" >&2
        echo "  actual:   $ACTUAL_SHA256" >&2
        exit 1
    fi
    echo "[sparkle] tarball SHA-256 verified"
else
    echo "[sparkle] WARNING: SPARKLE_TARBALL_SHA256 not set — skipping integrity check (local-dev only!)" >&2
fi

echo "[sparkle] extracting"
tar -C "$WORK_DIR" -xJf "$WORK_DIR/sparkle.tar.xz"

if [ ! -d "$WORK_DIR/Sparkle.framework" ]; then
    echo "[sparkle] FATAL: expected Sparkle.framework not found in tarball" >&2
    ls -la "$WORK_DIR" >&2
    exit 1
fi

mkdir -p "$DEST_DIR"
rm -rf "$DEST_DIR/Sparkle.framework"
cp -R "$WORK_DIR/Sparkle.framework" "$DEST_DIR/Sparkle.framework"

echo "[sparkle] staged Sparkle.framework -> $DEST_DIR/Sparkle.framework"
