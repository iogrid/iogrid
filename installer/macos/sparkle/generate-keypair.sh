#!/usr/bin/env bash
# Generate a fresh Sparkle EdDSA (ed25519) keypair. Used for:
#   - first-time setup of the iogrid-sparkle-signing K8s Secret
#   - key rotation (see installer/macos/sparkle/README.md#rotating)
#   - CI smoke test of generate-appcast.sh on every PR
#
# Usage:
#   ./generate-keypair.sh <DEST_DIR>
#
# Output (under DEST_DIR/):
#   privkey.ed25519   — base64-encoded ed25519 seed; what sign_update -s reads.
#                       Goes into OpenBao as kv/iogrid/sparkle/privkey.ed25519.
#   pubkey.ed25519    — base64-encoded ed25519 pubkey. Checked into repo at
#                       installer/macos/sparkle/pubkey.ed25519 + embedded into
#                       Info.plist's SUPublicEDKey on every .pkg build.
#
# Implementation note (2026-05-21 fix): the previous version invoked
# `generate_keys -f <dir>` from the Sparkle 2.6.x dist, expecting it to
# write eddsa_priv.pem + eddsa_pub.pem into <dir>. Sparkle 2.x changed
# `-f` semantics — it now expects a FILE path containing an existing key
# to convert/export, not a directory to write a fresh key into. The
# default `generate_keys` invocation (no args) creates the key in the
# macOS Keychain, which doesn't work in headless CI.
#
# To unblock both CI smoke + dev workstations without depending on the
# Sparkle binary at all, we generate the ed25519 keypair directly via
# `openssl genpkey` and extract the raw seed + pubkey from the PKCS-8 PEM.
# `sign_update -s <base64-seed>` consumes the seed identically — Sparkle's
# wire format is just raw ed25519, the framework binary was only a
# convenience wrapper.

set -euo pipefail

DEST_DIR="${1:-}"
if [ -z "$DEST_DIR" ]; then
    echo "usage: $0 <DEST_DIR>" >&2
    exit 2
fi
mkdir -p "$DEST_DIR"

if ! command -v openssl >/dev/null 2>&1; then
    echo "[sparkle-keygen] FATAL: openssl not on PATH (required for ed25519 keygen)" >&2
    exit 1
fi

WORK_DIR="$(mktemp -d -t sparkle-keygen.XXXXXX)"
trap 'rm -rf "$WORK_DIR"' EXIT

echo "[sparkle-keygen] generating ed25519 keypair via openssl"

# 1. Generate raw ed25519 private key in PKCS-8 PEM.
openssl genpkey -algorithm ed25519 -out "$WORK_DIR/privkey.pem"

# 2. Derive the public key PEM.
openssl pkey -in "$WORK_DIR/privkey.pem" -pubout -out "$WORK_DIR/pubkey.pem"

# 3. Extract the raw 32-byte ed25519 SEED from the PKCS-8 envelope.
#    The PKCS-8 ed25519 wrapper is `30 2e 02 01 00 30 05 06 03 2b 65 70 04 22 04 20 <32-byte-seed>`
#    so the seed is the last 32 bytes of the DER. We use `openssl asn1parse` to
#    isolate the inner OCTET STRING and base64-encode it.
PRIV_DER=$(openssl pkey -in "$WORK_DIR/privkey.pem" -outform DER | xxd -p -c 1000)
# Seed is the last 64 hex chars (32 bytes) of the DER.
SEED_HEX="${PRIV_DER: -64}"
echo "$SEED_HEX" | xxd -r -p | base64 > "$DEST_DIR/privkey.ed25519"

# 4. Extract the raw 32-byte ed25519 PUBLIC KEY from the SubjectPublicKeyInfo.
#    Wrapper is `30 2a 30 05 06 03 2b 65 70 03 21 00 <32-byte-pubkey>` so
#    pubkey is the last 32 bytes of the DER.
PUB_DER=$(openssl pkey -in "$WORK_DIR/privkey.pem" -pubout -outform DER | xxd -p -c 1000)
PUB_HEX="${PUB_DER: -64}"
echo "$PUB_HEX" | xxd -r -p | base64 > "$DEST_DIR/pubkey.ed25519"

# 5. Sanity check — both files should be non-empty base64.
for f in privkey.ed25519 pubkey.ed25519; do
    if [ ! -s "$DEST_DIR/$f" ]; then
        echo "[sparkle-keygen] FATAL: $DEST_DIR/$f is empty" >&2
        exit 1
    fi
    # base64-decoded length should be 32 bytes.
    LEN=$(base64 -d < "$DEST_DIR/$f" | wc -c | tr -d ' ')
    if [ "$LEN" != "32" ]; then
        echo "[sparkle-keygen] FATAL: $DEST_DIR/$f decodes to $LEN bytes, expected 32" >&2
        exit 1
    fi
done

echo "[sparkle-keygen] OK"
echo "[sparkle-keygen]   public key  -> $DEST_DIR/pubkey.ed25519"
echo "[sparkle-keygen]   private key -> $DEST_DIR/privkey.ed25519  (NEVER commit!)"
echo
echo "Next steps:"
echo "  1. cp $DEST_DIR/pubkey.ed25519 installer/macos/sparkle/pubkey.ed25519"
echo "  2. store $DEST_DIR/privkey.ed25519 in OpenBao as kv/iogrid/sparkle/privkey.ed25519"
echo "  3. confirm external-secrets projects iogrid-sparkle-signing.signing-key from that path"
echo "  4. DELETE $DEST_DIR/privkey.ed25519 from local disk"
