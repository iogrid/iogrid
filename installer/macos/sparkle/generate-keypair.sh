#!/usr/bin/env bash
# Generate a fresh Sparkle EdDSA (ed25519) keypair using `generate_keys` from
# the Sparkle dist's bin/ directory. Used for:
#   - first-time setup of the iogrid-sparkle-signing K8s Secret
#   - key rotation (see installer/macos/sparkle/README.md#rotating)
#
# Usage:
#   ./generate-keypair.sh <DEST_DIR>
#
# Output (under DEST_DIR/):
#   privkey.ed25519   — base64-encoded ed25519 seed; goes into OpenBao
#   pubkey.ed25519    — base64-encoded ed25519 pubkey; checked into repo at
#                       installer/macos/sparkle/pubkey.ed25519 + embedded into
#                       Info.plist's SUPublicEDKey on every .pkg build.
set -euo pipefail

DEST_DIR="${1:-}"
if [ -z "$DEST_DIR" ]; then
    echo "usage: $0 <DEST_DIR>" >&2
    exit 2
fi
mkdir -p "$DEST_DIR"

# Sparkle ships `generate_keys` in the bin/ directory of the framework
# tarball. We unpack a fresh copy here rather than depending on a globally
# installed Sparkle, so this script works on a clean dev machine.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERSION="$(cat "$SCRIPT_DIR/SPARKLE_VERSION" | tr -d '[:space:]')"
WORK_DIR="$(mktemp -d -t sparkle-keygen.XXXXXX)"
trap 'rm -rf "$WORK_DIR"' EXIT

echo "[sparkle-keygen] fetching Sparkle ${VERSION} for generate_keys"
curl --proto '=https' --tlsv1.2 -sSfL \
    "https://github.com/sparkle-project/Sparkle/releases/download/${VERSION}/Sparkle-${VERSION}.tar.xz" \
    -o "$WORK_DIR/sparkle.tar.xz"
tar -C "$WORK_DIR" -xJf "$WORK_DIR/sparkle.tar.xz"

GENERATE_KEYS="$WORK_DIR/bin/generate_keys"
if [ ! -x "$GENERATE_KEYS" ]; then
    echo "[sparkle-keygen] FATAL: generate_keys not found at $GENERATE_KEYS" >&2
    exit 1
fi

# generate_keys writes the keypair into the macOS Keychain by default; for
# headless CI/repo use, the `-f <dir>` form writes to disk instead.
"$GENERATE_KEYS" -f "$DEST_DIR" >&2

if [ ! -s "$DEST_DIR/eddsa_priv.pem" ] || [ ! -s "$DEST_DIR/eddsa_pub.pem" ]; then
    echo "[sparkle-keygen] FATAL: expected key files not created in $DEST_DIR" >&2
    ls -la "$DEST_DIR" >&2
    exit 1
fi

# Sparkle's sign_update reads the base64-encoded seed directly via -s. Extract
# from the PEM to a compact base64 file for easier wiring into the K8s Secret.
mv "$DEST_DIR/eddsa_priv.pem" "$DEST_DIR/privkey.ed25519"
mv "$DEST_DIR/eddsa_pub.pem"  "$DEST_DIR/pubkey.ed25519"

echo "[sparkle-keygen] OK"
echo "[sparkle-keygen]   public key  -> $DEST_DIR/pubkey.ed25519"
echo "[sparkle-keygen]   private key -> $DEST_DIR/privkey.ed25519  (NEVER commit!)"
echo
echo "Next steps:"
echo "  1. cp $DEST_DIR/pubkey.ed25519 installer/macos/sparkle/pubkey.ed25519"
echo "  2. store $DEST_DIR/privkey.ed25519 in OpenBao as kv/iogrid/sparkle/privkey.ed25519"
echo "  3. confirm external-secrets projects iogrid-sparkle-signing.signing-key from that path"
echo "  4. DELETE $DEST_DIR/privkey.ed25519 from local disk"
