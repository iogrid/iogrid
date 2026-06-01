#!/usr/bin/env bash
# bootstrap-testflight.sh — one-shot operator command that runs the entire
# TestFlight bootstrap chain in a single invocation. Founder does ONE thing:
# downloads the .p8 API key from App Store Connect + provides 3 numeric IDs.
# This script does everything else.
#
# Refs #566 #574 #575. Replaces the 6-step runbook with a single command.
#
# What it does:
#   1. base64-encodes the .p8 + pushes 4 Apple secrets to GitHub Actions
#      (APP_STORE_CONNECT_PRIVATE_KEY, APP_STORE_CONNECT_KEY_ID,
#       APP_STORE_CONNECT_ISSUER_ID, APPLE_TEAM_ID)
#   2. Triggers an empty commit to fire the mobile-ios-ci pipeline
#   3. Tails the run until TestFlight upload completes
#   4. After Apple processing (~5-15 min), invites emrahbaysal@gmail.com to
#      the vpn-beta external beta group via the App Store Connect API
#
# What the operator must do FIRST (one-time Apple Developer Portal work):
#   - Have Apple Developer Program enrollment active
#   - Register App ID io.iogrid.app with NetworkExtension + App Groups +
#     Personal VPN capabilities (Apple's web UI only)
#   - Register App Group group.io.iogrid.app + link to the App ID
#   - Create the App Store Connect app record for io.iogrid.app (Apple's
#     web UI only — gives you the numeric App ID)
#
# Usage:
#   ./scripts/bootstrap-testflight.sh \
#       <path-to-AuthKey_XXX.p8> \
#       <KEY_ID> \
#       <ISSUER_ID> \
#       <APPLE_TEAM_ID> \
#       <NUMERIC_APP_ID>
#
# Example:
#   ./scripts/bootstrap-testflight.sh \
#       ~/Downloads/AuthKey_9F8K7T6L4M.p8 \
#       9F8K7T6L4M \
#       69a6de70-03db-47e3-e053-5b8c7c11a4d1 \
#       AB12C34DEF \
#       1234567890

set -euo pipefail

P8_PATH="${1:?path to AuthKey_*.p8 file required}"
KEY_ID="${2:?Key ID required (10 alphanumeric chars from App Store Connect)}"
ISSUER_ID="${3:?Issuer ID required (UUID from App Store Connect Keys page)}"
TEAM_ID="${4:?Apple Team ID required (10 alphanumeric chars from developer.apple.com)}"
APP_ID="${5:?numeric App ID required (from App Store Connect app URL)}"

REPO="${REPO:-iogrid/iogrid}"
TESTER_EMAIL="${TESTER_EMAIL:-emrahbaysal@gmail.com}"
TESTER_FIRST="${TESTER_FIRST:-Emrah}"
TESTER_LAST="${TESTER_LAST:-Baysal}"

if [[ ! -f "$P8_PATH" ]]; then
  echo "✗ .p8 file not found at: $P8_PATH" >&2
  exit 1
fi

echo "==> Step 1/4: pushing 4 Apple secrets to $REPO"
gh secret set APP_STORE_CONNECT_PRIVATE_KEY --repo "$REPO" < <(base64 -w0 < "$P8_PATH")
gh secret set APP_STORE_CONNECT_KEY_ID      --repo "$REPO" --body "$KEY_ID"
gh secret set APP_STORE_CONNECT_ISSUER_ID   --repo "$REPO" --body "$ISSUER_ID"
gh secret set APPLE_TEAM_ID                 --repo "$REPO" --body "$TEAM_ID"

echo "  ✓ secrets present:"
gh secret list --repo "$REPO" | grep -E "APPLE|APP_STORE" | sed 's/^/    /'

echo
echo "==> Step 2/4: triggering CI"
cd "$(git rev-parse --show-toplevel)"
git commit --allow-empty -m "ci: trigger mobile-ios-ci after Apple secrets land (bootstrap-testflight.sh)"
git push origin HEAD:main

RUN_ID=$(sleep 5; gh run list --workflow=mobile-ios-ci.yml --repo "$REPO" --limit 1 --json databaseId -q '.[0].databaseId')
echo "  ✓ run $RUN_ID started — https://github.com/$REPO/actions/runs/$RUN_ID"

echo
echo "==> Step 3/4: watching CI to TestFlight upload completion"
if ! gh run watch "$RUN_ID" --repo "$REPO" --exit-status; then
  echo "✗ CI failed — see https://github.com/$REPO/actions/runs/$RUN_ID" >&2
  echo "  Common causes:"
  echo "    - App Store Connect record for io.iogrid.app doesn't exist yet (Step 3 of runbook)"
  echo "    - App ID isn't registered with NE + App Groups + Personal VPN capabilities (Step 2)"
  echo "    - The .p8 key was revoked or has insufficient role (must be Admin)"
  exit 1
fi
echo "  ✓ CI uploaded build to TestFlight"

echo
echo "==> Step 4/4: waiting for Apple to process the build + inviting $TESTER_EMAIL"
echo "  Apple typically processes within 5-15 min after altool finishes."
echo "  Polling App Store Connect API for the build to appear..."

export ASC_KEY_ID="$KEY_ID"
export ASC_ISSUER_ID="$ISSUER_ID"
export ASC_KEY_PATH="$P8_PATH"
export ASC_APP_ID="$APP_ID"

# Poll for up to 20 minutes for the build to be processed
for i in {1..40}; do
  # The invite script bails with "no external beta groups" if vpn-beta
  # doesn't exist. We try it once; on that error, surface a clear next step.
  if ./scripts/invite-testflight-tester.sh "$TESTER_EMAIL" "$TESTER_FIRST" "$TESTER_LAST" 2>&1 | tee /tmp/invite.log; then
    echo
    echo "==> ✅ DONE"
    echo "  $TESTER_EMAIL invited to vpn-beta. TestFlight email arriving in 2-5 min."
    echo "  Install via the TestFlight app on iPhone."
    exit 0
  fi
  if grep -q "no external beta groups" /tmp/invite.log; then
    echo
    echo "  ⚠ External beta group 'vpn-beta' doesn't exist yet."
    echo "  Create it ONCE via:"
    echo "    https://appstoreconnect.apple.com → TestFlight → External Testing"
    echo "    → + → Group name: vpn-beta → Save"
    echo "  Then re-run: ./scripts/invite-testflight-tester.sh $TESTER_EMAIL $TESTER_FIRST $TESTER_LAST"
    exit 1
  fi
  echo "  i=$i — build not visible yet, waiting 30s..."
  sleep 30
done

echo "✗ Apple still hasn't processed the build after 20 min." >&2
echo "  Check App Store Connect → TestFlight → iOS for status." >&2
exit 1
