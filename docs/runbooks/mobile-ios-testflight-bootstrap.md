# iogrid iOS TestFlight bootstrap — operator runbook

> One-time operator setup to push the iogrid iOS app to TestFlight + invite
> the first beta tester. Pairs with #575 (Apple secrets) + #574 (TestFlight
> beta crew).
>
> After this runbook is complete, every push to `main` that touches
> `mobile/ios/**` auto-builds + auto-uploads to TestFlight via the
> `mobile-ios-ci.yml` workflow.

## Prerequisites (founder must have access to)

| Item | Where to get |
|---|---|
| Apple Developer Program | https://developer.apple.com — paid, enrolled as iogrid |
| App Store Connect access | https://appstoreconnect.apple.com — same Apple ID |
| `io.iogrid.app` App ID with Network Extensions + App Groups + Personal VPN | Apple Developer Portal → Identifiers |
| `group.io.iogrid.app` App Group + linked to the App ID | Apple Developer Portal → Identifiers → App Groups |
| App Store Connect record for the bundle | App Store Connect → My Apps → + → iOS app |

If any of the above is missing, do that FIRST. Each step is ~5 min in the
Apple web UI; collectively ~25 min for a fresh setup.

## Step 1 — App Store Connect API key (3 min)

```
https://appstoreconnect.apple.com → Users and Access → Keys (Integrations tab)
→ Generate API Key
  Name:    iogrid-ci
  Access:  Admin (for first run; downgrade to Developer after smoke is green)
→ Download AuthKey_XXXXXXXXXX.p8   (Apple shows this ONCE — save it!)
→ Note the Key ID (e.g. XXXXXXXXXX) and Issuer ID (UUID at top of the page)
```

## Step 2 — Push the 4 secrets to GitHub Actions (1 min)

```bash
cd ~/repos/iogrid

# .p8 file must be base64-encoded so it survives the YAML quoting in CI
gh secret set APP_STORE_CONNECT_PRIVATE_KEY --repo iogrid/iogrid \
  < <(base64 -w0 < ~/Downloads/AuthKey_XXXXXXXXXX.p8)

gh secret set APP_STORE_CONNECT_KEY_ID    --repo iogrid/iogrid --body "XXXXXXXXXX"
gh secret set APP_STORE_CONNECT_ISSUER_ID --repo iogrid/iogrid --body "00000000-0000-0000-0000-000000000000"
gh secret set APPLE_TEAM_ID               --repo iogrid/iogrid --body "XXXXXXXXXX"

gh secret list --repo iogrid/iogrid | grep -E "APPLE|APP_STORE"   # ✓ 4 entries
```

## Step 3 — EAS project init (one-time, 2 min)

```bash
cd ~/repos/iogrid/mobile/ios
npx eas-cli login                  # sign in to Expo account
npx eas-cli init                   # writes extra.eas.projectId to app.json
git add app.json && git commit -m "chore(mobile/ios): EAS projectId from eas init"
git push
```

## Step 4 — First TestFlight upload (CI does this automatically once Steps 1-3 are done)

Push any change to `mobile/ios/**` on `main`. The `mobile-ios-ci.yml` workflow:
1. Builds the bare iOS project via `expo prebuild`
2. Runs the `add-network-extension-target.rb` Ruby script to add the
   PacketTunnelProvider NE target + WireGuardKit SwiftPM dep
3. Runs Maestro smoke gate on the iOS simulator (4 flows)
4. fastlane sigh fetches/renews the App Store provisioning profile
5. fastlane cert refreshes the distribution cert if needed
6. xcodebuild archives + exports the `.ipa`
7. `xcrun altool` uploads to TestFlight

Watch the run:

```bash
gh run watch $(gh run list --workflow=mobile-ios-ci.yml --limit 1 --json databaseId -q '.[0].databaseId') --repo iogrid/iogrid
```

First build takes ~15 min cold. Subsequent: ~6 min (Pod cache warm).

## Step 5 — Invite emrahbaysal@gmail.com as external beta tester

After Step 4's `altool` upload completes, the build appears in App Store
Connect under TestFlight → iOS within 5-15 minutes (Apple's processing).
Once it shows up:

### Option A — via App Store Connect web UI (recommended for first invite)

```
https://appstoreconnect.apple.com → TestFlight → iogrid → Internal Testing
→ "+" → Add Tester
  Email: emrahbaysal@gmail.com
  First name: Emrah
  Last name: Baysal
→ "Add"
```

Tester gets the TestFlight invite email; tap the link, install the
TestFlight app, install iogrid.

### Option B — via the App Store Connect API (scriptable)

After Steps 1-2 leave you with the `.p8` + Key ID + Issuer ID, you can
script the invite via the included helper:

```bash
cd ~/repos/iogrid/mobile/ios
# Requires: $ASC_KEY_ID, $ASC_ISSUER_ID, and the .p8 file at ~/private_keys/
./scripts/invite-testflight-tester.sh emrahbaysal@gmail.com Emrah Baysal
```

## Step 6 — Founder verifies on iPhone

1. Open the TestFlight invite email on iPhone (Apple ID = emrahbaysal@gmail.com)
2. Tap "View in TestFlight"
3. Tap "Install" — installs the iogrid app
4. Open iogrid
5. ★ Toggle ON ★ → exit IP changes within ~3 seconds (Mullvad-style anon ID
   already generated in Keychain; coordinator picks the best region
   automatically)
6. Tap the region row → see the live list from `GET /v1/vpn/regions`
7. Tap Settings → see your 16-digit account number

When all 7 steps pass, EPIC #566 v1 is end-user verified. Screenshot all 7
and attach to the EPIC.

## What's NOT in this runbook (separate tracks)

- **Android app**: separate v1.1 EPIC
- **Compute customer features** (Docker / GPU / iOS-build): positioning only,
  no code in this EPIC
- **IAP / Stripe upgrade flow**: separate billing initiative once the free
  tier hits real-world usage
- **Sign in with Apple / Google ID-recovery flow**: cut from v1; add when
  anon-loss recovery becomes a real support pattern
