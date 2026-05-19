#!/usr/bin/env bash
# build.sh — emit .deb + .rpm + .apk via nfpm for a given arch.
#
# Inputs (env or CLI args):
#   IOGRID_VERSION   semver string (e.g. 0.1.0). Default: 0.1.0.
#   IOGRID_ARCH      amd64 | arm64. Default: amd64.
#   IOGRID_BIN       path to a built iogridd binary. REQUIRED.
#   OUT_DIR          where to drop packages. Default: ../dist.
#
# Optional signing (gated on env):
#   COSIGN_KEY_FILE         file path; if set, cosign sign-blob each
#                           artifact. Pubkey embedded at update-check
#                           time.
#   RPM_SIGNING_KEY_FILE    path to GPG private key for rpm sigs.
#   RPM_SIGNING_KEY_ID      GPG key id matching the file.
#   APK_SIGNING_KEY_FILE    path to RSA private key for apk sigs.
#   APK_SIGNING_KEY_NAME    key name for apk index signing.
#
# CI flow: nfpm Docker image (goreleaser/nfpm:latest) has nfpm + cosign
# preinstalled. installer-ci.yml mounts the repo, runs this script for
# {amd64, arm64} × {deb, rpm, apk}.

set -euo pipefail

# Resolve repo root.
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$HERE"

IOGRID_VERSION="${IOGRID_VERSION:-0.1.0}"
IOGRID_ARCH="${IOGRID_ARCH:-amd64}"
IOGRID_BIN="${IOGRID_BIN:-}"
OUT_DIR="${OUT_DIR:-$HERE/../dist}"

if [ -z "$IOGRID_BIN" ] || [ ! -x "$IOGRID_BIN" ]; then
    echo "[linux-pkg] FATAL: IOGRID_BIN must point to a built iogridd binary." >&2
    echo "[linux-pkg]   IOGRID_BIN=$IOGRID_BIN" >&2
    exit 2
fi

# Empty defaults so nfpm doesn't fail on missing env interpolation in
# the rpm:signature: block when the secrets aren't present.
export IOGRID_VERSION IOGRID_ARCH IOGRID_BIN
export RPM_SIGNING_KEY_FILE="${RPM_SIGNING_KEY_FILE:-}"
export RPM_SIGNING_KEY_ID="${RPM_SIGNING_KEY_ID:-}"
export APK_SIGNING_KEY_FILE="${APK_SIGNING_KEY_FILE:-}"
export APK_SIGNING_KEY_NAME="${APK_SIGNING_KEY_NAME:-}"

mkdir -p "$OUT_DIR"

if ! command -v nfpm >/dev/null 2>&1; then
    echo "[linux-pkg] FATAL: nfpm not found in PATH." >&2
    echo "[linux-pkg]   Install: brew install nfpm   OR   go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest" >&2
    exit 3
fi

# nfpm v2.41.3 does NOT auto-interpolate ${IOGRID_*} env vars in the
# config file — that's an upstream limitation. We pre-render the YAML
# via envsubst (gettext package) into a temp file before invoking nfpm.
if ! command -v envsubst >/dev/null 2>&1; then
    echo "[linux-pkg] FATAL: envsubst not found. Install: apt install gettext" >&2
    exit 5
fi
rendered=$(mktemp -t nfpm-rendered.XXXXXX.yaml)
trap 'rm -f "$rendered"' EXIT
envsubst < nfpm.yaml > "$rendered"

build_one() {
    fmt="$1"
    out_file="$2"
    echo "[linux-pkg] packaging $fmt -> $out_file"
    nfpm pkg --packager "$fmt" --config "$rendered" --target "$out_file"
}

case "$IOGRID_ARCH" in
    amd64) deb_arch="amd64"; rpm_arch="x86_64"; apk_arch="x86_64" ;;
    arm64) deb_arch="arm64"; rpm_arch="aarch64"; apk_arch="aarch64" ;;
    *) echo "[linux-pkg] unsupported arch $IOGRID_ARCH"; exit 4 ;;
esac

build_one deb "$OUT_DIR/iogrid_${IOGRID_VERSION}_${deb_arch}.deb"
build_one rpm "$OUT_DIR/iogrid-${IOGRID_VERSION}-1.${rpm_arch}.rpm"
build_one apk "$OUT_DIR/iogrid_${IOGRID_VERSION}_${apk_arch}.apk"

# Cosign blob sigs (optional). The signed bundle lives alongside each
# .deb/.rpm/.apk. Auto-update verifies these before swapping binaries.
if [ -n "${COSIGN_KEY_FILE:-}" ] && [ -f "$COSIGN_KEY_FILE" ]; then
    if command -v cosign >/dev/null 2>&1; then
        for f in "$OUT_DIR"/iogrid*; do
            [ -f "$f" ] || continue
            echo "[linux-pkg] cosign sign-blob $f"
            cosign sign-blob \
                --key "$COSIGN_KEY_FILE" \
                --output-signature "$f.sig" \
                --yes \
                "$f"
        done
    else
        echo "[linux-pkg] WARN: COSIGN_KEY_FILE set but cosign binary missing — skipping sigs"
    fi
else
    echo "[linux-pkg] COSIGN_KEY_FILE unset — skipping signing (CI artifact)"
fi

# Always emit a SHA256SUMS file. Auto-updater consumes this in lieu of
# (or in addition to) cosign sigs.
( cd "$OUT_DIR" && sha256sum iogrid* 2>/dev/null | grep -v '\.sig$' > SHA256SUMS || true )

echo "[linux-pkg] done. Artifacts in $OUT_DIR:"
ls -lh "$OUT_DIR"
