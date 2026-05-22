#!/usr/bin/env bash
# identity-svc-jwt-keypair-gen.sh — one-shot offline generator for the
# persistent JWT signing keypair identity-svc reads when JWT_KEYPAIR_AUTOGEN=0.
#
# Closes the manual half of #452. The full chain is:
#
#   1. Run this script offline (founder's laptop, NOT bastion):
#         ./scripts/identity-svc-jwt-keypair-gen.sh ~/iogrid-jwt-keys
#   2. The script writes private.pem (0600) + public.pem (0644) to that dir.
#   3. Base64-encode each file and stage as Kubernetes Secret data:
#         kubectl -n iogrid create secret generic identity-svc-jwt-keypair \
#           --from-file=private.pem=$(pwd)/private.pem \
#           --from-file=public.pem=$(pwd)/public.pem \
#           --dry-run=client -o yaml | kubeseal --controller-namespace sealed-secrets \
#             -o yaml > infra/k8s/base/identity-svc/sealed-jwt-keypair.yaml
#   4. Commit the SealedSecret to git; Flux applies it.
#   5. Update infra/k8s/base/identity-svc/deployment.yaml env:
#         JWT_KEYPAIR_AUTOGEN=0
#         JWT_KEYPAIR_PRIVATE_PATH=/etc/identity-svc/jwt/private.pem
#         JWT_KEYPAIR_PUBLIC_PATH=/etc/identity-svc/jwt/public.pem
#      + mount the secret at /etc/identity-svc/jwt/.
#
# The .pem files NEVER leave the founder's laptop unsealed. The script
# refuses to write to /tmp or any world-readable dir — the dir must be
# user-owned with mode 0700 (or it creates one).
#
# RSA-4096 is chosen over ECDSA so the OpenAPI-published JWKs key
# rotation playbook stays compatible with every JWT validator in the
# ecosystem; the per-request signing cost (~1ms on a 2020+ laptop) is
# negligible against the auth-rate ceilings in identity-svc.

set -euo pipefail

usage() {
    echo "Usage: $0 <target-dir>"
    echo "Example: $0 ~/iogrid-jwt-keys"
    exit 1
}

[ "$#" -eq 1 ] || usage

dir="$1"

if [ -e "$dir" ]; then
    if [ -e "$dir/private.pem" ] || [ -e "$dir/public.pem" ]; then
        echo "❌ Refusing to overwrite — $dir already contains private.pem or public.pem." >&2
        echo "   Pick a fresh empty dir, or rotate the existing keypair via the rotation playbook." >&2
        exit 2
    fi
    perms=$(stat -c '%a' "$dir")
    if [ "$perms" != "700" ]; then
        echo "❌ Target dir $dir has mode $perms; refusing to write a private key into a non-0700 dir." >&2
        echo "   Run: chmod 700 \"$dir\"  (then re-run)." >&2
        exit 3
    fi
else
    mkdir -p "$dir"
    chmod 700 "$dir"
fi

priv="$dir/private.pem"
pub="$dir/public.pem"

echo "Generating RSA-4096 JWT signing keypair → $dir …"
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:4096 -out "$priv"
chmod 600 "$priv"
openssl rsa -in "$priv" -pubout -out "$pub" 2>/dev/null
chmod 644 "$pub"

echo
echo "✅ Wrote:"
echo "   $priv (mode 600)"
echo "   $pub  (mode 644)"
echo
echo "Public key fingerprint (kid candidate):"
openssl rsa -in "$priv" -pubout -outform DER 2>/dev/null | sha256sum | awk '{print "   sha256:" $1}'
echo
echo "Next steps (per #452):"
echo "   1. Seal as Kubernetes Secret identity-svc-jwt-keypair via kubeseal."
echo "   2. Mount at /etc/identity-svc/jwt/ on the identity-svc Deployment."
echo "   3. Set JWT_KEYPAIR_AUTOGEN=0 + JWT_KEYPAIR_{PRIVATE,PUBLIC}_PATH env."
echo "   4. Rotate quarterly via the dual-key overlap window playbook."
