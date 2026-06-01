#!/usr/bin/env bash
# invite-testflight-tester.sh — invites an external beta tester to the
# iogrid iOS TestFlight via the App Store Connect API. Drives Step 5
# of docs/runbooks/mobile-ios-testflight-bootstrap.md.
#
# Usage:
#   ./scripts/invite-testflight-tester.sh <email> [first] [last]
#
# Required env vars (set by the runbook):
#   ASC_KEY_ID         — App Store Connect API Key ID
#   ASC_ISSUER_ID      — App Store Connect API Issuer ID
#   ASC_KEY_PATH       — path to AuthKey_${ASC_KEY_ID}.p8
#   ASC_APP_ID         — App Store Connect App ID (numeric, from My Apps URL)
#
# Refs #574 #575.

set -euo pipefail

EMAIL="${1:?email required}"
FIRST="${2:-Beta}"
LAST="${3:-Tester}"

: "${ASC_KEY_ID:?ASC_KEY_ID env var required — see Step 1 of runbook}"
: "${ASC_ISSUER_ID:?ASC_ISSUER_ID env var required}"
: "${ASC_KEY_PATH:?ASC_KEY_PATH env var required — path to AuthKey_*.p8}"
: "${ASC_APP_ID:?ASC_APP_ID env var required — numeric App ID from ASC}"

if [[ ! -f "$ASC_KEY_PATH" ]]; then
  echo "ASC_KEY_PATH points to $ASC_KEY_PATH but file doesn't exist." >&2
  exit 1
fi

# Generate a short-lived JWT for the ASC API (20-min window per Apple docs).
JWT="$(python3 - <<PY
import base64, hashlib, hmac, json, os, time
try:
    from cryptography.hazmat.backends import default_backend
    from cryptography.hazmat.primitives import hashes, serialization
    from cryptography.hazmat.primitives.asymmetric import ec
except ImportError:
    print("ERROR: pip install cryptography first", flush=True)
    raise

with open(os.environ['ASC_KEY_PATH'], 'rb') as f:
    private_key = serialization.load_pem_private_key(f.read(), password=None, backend=default_backend())

header = {"alg": "ES256", "kid": os.environ['ASC_KEY_ID'], "typ": "JWT"}
payload = {
    "iss": os.environ['ASC_ISSUER_ID'],
    "iat": int(time.time()),
    "exp": int(time.time()) + 20 * 60,
    "aud": "appstoreconnect-v1",
}

def b64(d):
    return base64.urlsafe_b64encode(json.dumps(d, separators=(',', ':')).encode()).rstrip(b'=').decode()

signing_input = b64(header) + '.' + b64(payload)
sig = private_key.sign(signing_input.encode(), ec.ECDSA(hashes.SHA256()))
# ASC expects the raw r||s (64 bytes), not DER. cryptography returns DER;
# convert.
from cryptography.hazmat.primitives.asymmetric.utils import decode_dss_signature
r, s = decode_dss_signature(sig)
raw_sig = r.to_bytes(32, 'big') + s.to_bytes(32, 'big')
sig_b64 = base64.urlsafe_b64encode(raw_sig).rstrip(b'=').decode()

print(signing_input + '.' + sig_b64)
PY
)"

API_BASE="https://api.appstoreconnect.apple.com/v1"

# Step 1 — find the BetaGroup for external testers
echo "[+] Fetching beta groups for app $ASC_APP_ID..."
BETA_GROUPS_JSON=$(curl -sf -H "Authorization: Bearer $JWT" \
  "$API_BASE/apps/$ASC_APP_ID/betaGroups?filter[isInternalGroup]=false&limit=10")

# Use the first external beta group; create one if none exists. This
# script doesn't auto-create — operator should create the "vpn-beta"
# group via the web UI once.
GROUP_ID=$(echo "$BETA_GROUPS_JSON" | python3 -c "
import json, sys
data = json.load(sys.stdin)
groups = data.get('data', [])
if not groups:
    print('ERROR: no external beta groups found. Create one via App Store Connect → TestFlight → Add External Group (name: vpn-beta) first.', file=sys.stderr)
    sys.exit(1)
print(groups[0]['id'])
")
echo "[+] Using beta group $GROUP_ID"

# Step 2 — create the beta tester record (or fetch existing)
echo "[+] Creating tester record for $EMAIL..."
CREATE_RESPONSE=$(curl -sf -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" \
  -X POST "$API_BASE/betaTesters" \
  -d "$(cat <<EOF
{
  "data": {
    "type": "betaTesters",
    "attributes": {
      "email": "$EMAIL",
      "firstName": "$FIRST",
      "lastName": "$LAST"
    },
    "relationships": {
      "betaGroups": {
        "data": [
          { "type": "betaGroups", "id": "$GROUP_ID" }
        ]
      }
    }
  }
}
EOF
)" 2>&1 || true)

if echo "$CREATE_RESPONSE" | grep -qE "ENTITY_ERROR|already exists|409"; then
  echo "[+] Tester $EMAIL already exists — adding to group..."
  # Look up existing tester ID
  EXISTING=$(curl -sf -H "Authorization: Bearer $JWT" \
    "$API_BASE/betaTesters?filter[email]=$EMAIL")
  TESTER_ID=$(echo "$EXISTING" | python3 -c "
import json, sys
data = json.load(sys.stdin)
testers = data.get('data', [])
print(testers[0]['id'] if testers else '')
")
  if [[ -z "$TESTER_ID" ]]; then
    echo "ERROR: tester appears to exist but couldn't fetch ID" >&2
    exit 1
  fi
  # Add to the group
  curl -sf -H "Authorization: Bearer $JWT" -H "Content-Type: application/json" \
    -X POST "$API_BASE/betaGroups/$GROUP_ID/relationships/betaTesters" \
    -d "{\"data\": [{\"type\": \"betaTesters\", \"id\": \"$TESTER_ID\"}]}"
  echo "[✓] $EMAIL added to beta group $GROUP_ID"
else
  echo "[✓] Tester $EMAIL created + added to beta group $GROUP_ID"
fi

echo
echo "TestFlight invite email is queued. Check $EMAIL inbox in 2-5 minutes."
echo "Tester also sees the build on https://testflight.apple.com once Apple finishes processing."
