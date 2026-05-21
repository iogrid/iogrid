#!/usr/bin/env bash
# Apply infra/dynadot/iogrid-org-records.json to Dynadot via set_dns.
#
# Dynadot's set_dns command requires the FULL desired record set in
# one HTTPS GET. Anything not in this call is removed from the zone.
# Source of truth is infra/dynadot/iogrid-org-records.json — edit it,
# run this script, verify with `dig @1.1.1.1 iogrid.org A`.
#
# Allowlisting: Dynadot's API allowlists by client IP. The mothership
# Contabo IP (45.151.123.50) is allowlisted; the bastion is not. So we
# kubectl-run a one-shot pod on the mothership which inherits the
# node's outbound IP.
#
# Reads creds from secret/dynadot-api-credentials in openova-system.
#
# Usage:
#   ./scripts/dynadot-apply.sh             # dry-run, prints the URL only
#   ./scripts/dynadot-apply.sh --apply     # actually fires set_dns
#   ./scripts/dynadot-apply.sh --verify    # post-apply dig check (no API)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RECORDS_FILE="$REPO_ROOT/infra/dynadot/iogrid-org-records.json"
MODE="${1:-dry-run}"

if [[ ! -f "$RECORDS_FILE" ]]; then
  echo "FATAL: $RECORDS_FILE missing" >&2
  exit 1
fi

DOMAIN="$(jq -r .domain "$RECORDS_FILE")"

if [[ "$MODE" == "--verify" ]]; then
  # Derive the host list from the records file so newly-added subdomains
  # are exercised automatically — prevents the #410 class of bug where
  # `admin` was appended to the JSON + the Certificate SAN list but the
  # verify block hand-rolled subdomains and silently missed it (so the
  # operator running `--verify` got a green check while cert-manager's
  # HTTP-01 self-check was 0-resolving `admin.$DOMAIN` and the iogrid.org
  # apex was serving Traefik's default self-signed cert).
  EXPECTED="$(jq -r .main.value "$RECORDS_FILE")"
  rc=0
  echo "=== Public-resolver lookup for $DOMAIN + all subdomains (expecting $EXPECTED) ==="
  hosts=("$DOMAIN")
  while IFS= read -r sub; do hosts+=("$sub.$DOMAIN"); done < <(jq -r '.subdomains[].subdomain' "$RECORDS_FILE")
  for h in "${hosts[@]}"; do
    answer="$(dig +short "$h" A @1.1.1.1 | head -1)"
    if [[ -z "$answer" ]]; then
      printf '%-30s %s\n' "$h" "MISSING — zone push pending or record not present"
      rc=1
    elif [[ "$answer" != "$EXPECTED" ]]; then
      printf '%-30s %s\n' "$h" "DRIFT — got $answer, expected $EXPECTED"
      rc=1
    else
      printf '%-30s %s\n' "$h" "$answer"
    fi
  done
  exit "$rc"
fi

# Build the query string. Dynadot set_dns2 indexed shape:
#   command=set_dns2
#   domain=<apex>
#   ttl=<zone-wide TTL>          # applied to all records in this call
#   main_record_type0=a          # apex's primary A record
#   main_record0=45.151.123.50
#   main_recordx0=0              # priority slot, 0 for non-MX
#   subdomain0=www               # subdomain entry 0
#   sub_record_type0=a
#   sub_record0=45.151.123.50
#   sub_recordx0=0
#   subdomain1=api
#   ...
#
# NOTE: set_dns (no suffix) was deprecated; api3 returns "unknown
# command" for it as of 2026-05. set_dns2 is the only supported writer.

MAIN_TYPE="$(jq -r '.main.record_type | ascii_downcase' "$RECORDS_FILE")"
MAIN_VAL="$(jq -r .main.value "$RECORDS_FILE")"
ZONE_TTL="$(jq -r .main.ttl "$RECORDS_FILE")"

QS="command=set_dns2&domain=$DOMAIN&ttl=$ZONE_TTL"
QS+="&main_record_type0=$MAIN_TYPE"
QS+="&main_record0=$MAIN_VAL"
QS+="&main_recordx0=0"

N="$(jq '.subdomains | length' "$RECORDS_FILE")"
for ((i=0; i<N; i++)); do
  S_NAME="$(jq -r ".subdomains[$i].subdomain" "$RECORDS_FILE")"
  S_TYPE="$(jq -r ".subdomains[$i].record_type | ascii_downcase" "$RECORDS_FILE")"
  S_VAL="$(jq -r ".subdomains[$i].value" "$RECORDS_FILE")"
  QS+="&subdomain$i=$S_NAME"
  QS+="&sub_record_type$i=$S_TYPE"
  QS+="&sub_record$i=$S_VAL"
  QS+="&sub_recordx$i=0"
done

if [[ "$MODE" == "dry-run" ]]; then
  echo "DRY RUN — would call:"
  echo "  https://api.dynadot.com/api3.json?key=<REDACTED>&$QS"
  echo
  echo "Re-run with --apply to actually fire."
  exit 0
fi

if [[ "$MODE" != "--apply" ]]; then
  echo "Unknown mode: $MODE (expected dry-run | --apply | --verify)" >&2
  exit 1
fi

POD_NAME="dynadot-apply-iogrid-$(date +%s)"
echo "=== Applying $DOMAIN DNS via mothership pod $POD_NAME ==="

KEY="$(kubectl get secret -n openova-system dynadot-api-credentials -o jsonpath='{.data.api-key}' | base64 -d)"

kubectl run "$POD_NAME" \
  --rm -i --restart=Never \
  --image=alpine:3.20 --image-pull-policy=IfNotPresent \
  --env="KEY=$KEY" \
  --env="QS=$QS" -- sh -c '
apk add --no-cache curl jq >/dev/null 2>&1
echo "=== set_dns ==="
curl -sS "https://api.dynadot.com/api3.json?key=$KEY&$QS" | jq .
echo "=== post-apply get_dns ==="
curl -sS "https://api.dynadot.com/api3.json?key=$KEY&command=get_dns&domain=iogrid.org" | jq .
'

echo
echo "Done. DNS propagation typically <5min for new zones. Verify with:"
echo "  $0 --verify"
