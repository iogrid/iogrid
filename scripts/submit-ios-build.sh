#!/usr/bin/env bash
# submit-ios-build.sh — submit an iOS build to iogrid AS A CUSTOMER (#700/#757).
#
# This is the turnkey "ping is the first iogrid customer" path: ONE API call +
# an API key, then poll status + download the artifact. ZERO SSH, zero access to
# the Mac. The build runs on a real macOS provider (e.g. Hatice's Mac) via the
# iogrid build-gateway -> workloads-svc -> daemon poll dispatch, settles in
# devnet $GRID, and returns the artifact to you over a pre-signed URL.
#
# Usage:
#   IOGRID_API_KEY=<key> ./scripts/submit-ios-build.sh \
#       --repo https://github.com/ping/ping.git \
#       --ref main \
#       --cmd 'xcodebuild -workspace Ping.xcworkspace -scheme Ping -destination "platform=iOS Simulator,name=iPhone 16 Pro" -derivedDataPath /tmp/ping-build build'
#
# Flags (all optional except --cmd; --repo defaults to the iogrid app repo):
#   --base   <url>   build-gateway base URL          (default https://build.iogrid.org)
#   --repo   <url>   git repo the provider clones     (default https://github.com/iogrid/iogrid.git)
#   --ref    <ref>   branch / tag / commit            (default main)
#   --cmd    <str>   the xcodebuild/shell command run after clone+checkout (REQUIRED)
#   --xcode  <ver>   approved Xcode version           (default: server default; see /v1/xcode-versions)
#   --artifact <name> artifact filename to download on success (default: none — just print the record)
#   --no-logs        skip the live SSE log tail
#   --timeout <sec>  give up polling after N seconds  (default 2400 = 40 min)
#
# Auth: the API key is sent as `Authorization: Bearer <key>` (X-Iogrid-Api-Key
# also accepted by the server). Obtain a key from the iogrid customer console /
# billing-svc (or, for the devnet dogfood, the operator provisions one on the
# build-gateway). NEVER commit your key.
#
# Requires: bash, curl, and (optionally) jq for pretty output — falls back to
# grep/sed if jq is absent.
set -euo pipefail

BASE="${IOGRID_BUILD_BASE:-https://build.iogrid.org}"
REPO="https://github.com/iogrid/iogrid.git"
REF="main"
CMD=""
XCODE=""
ARTIFACT=""
TAIL_LOGS=1
TIMEOUT_SECS=2400

while [[ $# -gt 0 ]]; do
  case "$1" in
    --base)     BASE="$2"; shift 2;;
    --repo)     REPO="$2"; shift 2;;
    --ref)      REF="$2"; shift 2;;
    --cmd)      CMD="$2"; shift 2;;
    --xcode)    XCODE="$2"; shift 2;;
    --artifact) ARTIFACT="$2"; shift 2;;
    --no-logs)  TAIL_LOGS=0; shift;;
    --timeout)  TIMEOUT_SECS="$2"; shift 2;;
    -h|--help)  sed -n '2,40p' "$0"; exit 0;;
    *) echo "unknown flag: $1" >&2; exit 2;;
  esac
done

: "${IOGRID_API_KEY:?set IOGRID_API_KEY to your iogrid customer API key}"
if [[ -z "$CMD" ]]; then
  echo "ERROR: --cmd is required (the xcodebuild/shell command to run)" >&2
  exit 2
fi

have_jq() { command -v jq >/dev/null 2>&1; }
# get_field <json> <key> — extract a top-level string field without requiring jq.
get_field() {
  if have_jq; then echo "$1" | jq -r ".$2 // empty"; else
    echo "$1" | grep -o "\"$2\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 | sed 's/.*:[[:space:]]*"\(.*\)"/\1/'
  fi
}

AUTH=(-H "Authorization: Bearer ${IOGRID_API_KEY}")

echo "==> iogrid build-gateway: ${BASE}"
echo "==> discovering approved Xcode versions"
curl -fsS "${BASE}/v1/xcode-versions" "${AUTH[@]}" || true
echo

# Build the JSON submit body. xcode_version is omitted when unset so the server
# applies its default.
xcode_json=""
[[ -n "$XCODE" ]] && xcode_json="\"xcode_version\": $(printf '%s' "$XCODE" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read().strip()))'),"
body=$(cat <<EOF
{
  ${xcode_json}
  "git_url": $(printf '%s' "$REPO" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read().strip()))'),
  "git_ref": $(printf '%s' "$REF" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read().strip()))'),
  "build_command": $(printf '%s' "$CMD" | python3 -c 'import json,sys;print(json.dumps(sys.stdin.read().strip()))')
}
EOF
)

echo "==> POST /v1/builds"
resp=$(curl -fsS -X POST "${BASE}/v1/builds" "${AUTH[@]}" \
  -H 'Content-Type: application/json' --data "$body")
echo "$resp" | { have_jq && jq . || cat; }

BUILD_ID=$(get_field "$resp" build_id)
if [[ -z "$BUILD_ID" ]]; then
  echo "ERROR: no build_id in response — submission failed" >&2
  exit 1
fi
echo "==> build_id: ${BUILD_ID}"

# Tail live logs in the background (best-effort; SSE stream).
if [[ "$TAIL_LOGS" == "1" ]]; then
  echo "==> tailing logs (Ctrl-C to stop the tail; the build keeps running)"
  ( curl -fsS -N "${BASE}/v1/builds/${BUILD_ID}/logs" "${AUTH[@]}" 2>/dev/null \
      | sed -n 's/^data: //p' || true ) &
  LOGS_PID=$!
  trap '[[ -n "${LOGS_PID:-}" ]] && kill "$LOGS_PID" 2>/dev/null || true' EXIT
fi

# Poll status until terminal.
echo "==> polling GET /v1/builds/${BUILD_ID}"
deadline=$(( $(date +%s) + TIMEOUT_SECS ))
status=""
while :; do
  rec=$(curl -fsS "${BASE}/v1/builds/${BUILD_ID}" "${AUTH[@]}")
  status=$(get_field "$rec" status)
  echo "    status=${status}"
  case "$status" in
    succeeded|failed|timed_out|cancelled|rejected) break;;
  esac
  if (( $(date +%s) > deadline )); then
    echo "ERROR: timed out after ${TIMEOUT_SECS}s (last status=${status})" >&2
    exit 1
  fi
  sleep 10
done

[[ -n "${LOGS_PID:-}" ]] && kill "$LOGS_PID" 2>/dev/null || true

echo "==> terminal status: ${status}"
curl -fsS "${BASE}/v1/builds/${BUILD_ID}" "${AUTH[@]}" | { have_jq && jq . || cat; }

if [[ "$status" != "succeeded" ]]; then
  echo "build did not succeed (status=${status})" >&2
  exit 1
fi

# Download the named artifact via a pre-signed URL.
if [[ -n "$ARTIFACT" ]]; then
  echo "==> resolving pre-signed download URL for ${ARTIFACT}"
  pres=$(curl -fsS "${BASE}/v1/builds/${BUILD_ID}/artifacts/${ARTIFACT}" "${AUTH[@]}")
  url=$(get_field "$pres" url)
  if [[ -z "$url" ]]; then
    echo "ERROR: no pre-signed url returned for artifact ${ARTIFACT}" >&2
    echo "$pres"; exit 1
  fi
  out="./${ARTIFACT}"
  echo "==> downloading ${ARTIFACT} -> ${out}"
  curl -fsS -o "$out" "$url"
  ls -lh "$out"
fi

echo "==> done. Build ${BUILD_ID} succeeded; provider settled in devnet \$GRID."
