# Operator runbook — #574 App Store privacy nutrition + external Beta link

> One-shot operator walk to clear the LAST blocker on iogrid mobile v1
> public ship: App Privacy nutrition labels + the TestFlight external
> beta public-link toggle. Both are App Store Connect web-UI gestures —
> Apple's API does not expose `appPrivacy` writes, so the founder has
> to click. Budget: 10 minutes end-to-end.
>
> Companion to `docs/runbooks/mobile-ios-testflight-bootstrap.md`
> (one-time TestFlight wiring) and the auto-submit step in
> `.github/workflows/mobile-ios-ci.yml` (commits `bd72776` + `ed5865e`,
> Beta App Review submission is already CI-automated).

## Why this exists

| Gate | UI? | API? | Required for |
|---|---|---|---|
| App Privacy nutrition labels | App Store Connect → App Privacy | NOT writeable via API | App Store full submission + external Beta public link |
| External Beta group + public link | TestFlight → External Testing | Group create is API-able, but the public-link toggle is UI-only | Distributing the build to ≤10,000 testers without Apple-ID gating |
| Beta App Review submission | TestFlight → Build → "Submit for Review" | **Already automated** via `betaAppReviewSubmissions` POST in `mobile-ios-ci.yml` | External Beta (each first build of a new version) |
| Privacy + EULA URLs | App Store Connect → App Information | Set via API in CI | Both submissions; Apple's reviewer fetches them |

Internal Beta installs (founder on the `vpn-internal` group) work
**without any of the above** — that path is already live; this runbook
is only needed to widen distribution to external testers + the App
Store itself.

## Pre-flight (30 sec)

```bash
# (1) Confirm the privacy + EULA URLs return 200 from the public web.
curl -sIL https://iogrid.org/legal/mobile-privacy | head -1
curl -sIL https://iogrid.org/legal/mobile-eula    | head -1
```

Expected: both return `HTTP/2 200`. If either is 404, the Flux
deploy of commit `6481c40` hasn't rolled — wait for the next
`infra(web): deploy ...` commit on `main` and retry. The pages are
templated in `web/src/app/legal/mobile-privacy/page.tsx` and
`web/src/app/legal/mobile-eula/page.tsx`.

```bash
# (2) Confirm the latest TestFlight build is PROCESSED.
gh run list --workflow=mobile-ios-ci.yml --limit 3 --repo iogrid/iogrid
# → the most recent SUCCESS run is the build Apple is reviewing
```

## Step 1 — App Privacy nutrition labels (4 min)

```
https://appstoreconnect.apple.com → My Apps → iogrid → App Privacy
→ "Get Started"   (or "Edit" if you've been here before)
```

Apple asks: **"Does your app collect any data from this app?"**

→ Click **"No, we do not collect data from this app"** → **Save**.

### Why "No" is correct

The iogrid mobile (iOS) app:

- ships **no third-party analytics SDK** (no Firebase, Amplitude,
  Segment, Sentry, etc. — verified in `mobile/ios/package.json`)
- ships **no ad SDK** and **never requests IDFA** (no
  `NSUserTrackingUsageDescription` in `mobile/ios/app.json`)
- stores **only** the Apple-sign-in token in the device Keychain
  (local-only, never leaves the device for our analytics)
- routes traffic through residential peers but **does not log,
  inspect, or sell** that traffic (see
  `web/src/app/legal/mobile-privacy/page.tsx` — the public policy
  Apple's reviewer will read)

TestFlight diagnostic + crash reports are **Apple's collection**, not
iogrid's — those don't change the answer (Apple's nutrition-label FAQ
explicitly carves this out).

After save, the App Privacy panel should read:
**"Data Not Collected"** with a single line "The developer does not
collect any data from this app."

## Step 2 — External Beta group "vpn-beta" + public link (3 min)

```
https://appstoreconnect.apple.com → My Apps → iogrid → TestFlight
→ External Testing  (left sidebar)
```

### 2a. Create the group if missing

```
→ "+" next to "External Testing" → New Group
  Name: vpn-beta
→ Create
```

### 2b. Attach the latest build

```
→ vpn-beta → Builds tab → "+"
→ Pick the most-recent PROCESSED build (version 1.0.0, build = run number)
→ Apple prompts for "What to Test" + "Test Information" → fill in:
  What to Test: "VPN client backed by residential peers. Connect → toggle Personal VPN on → traffic egresses through a paired iogrid daemon."
  Email:        emrahbaysal@gmail.com
  Privacy URL:  https://iogrid.org/legal/mobile-privacy
  License URL:  https://iogrid.org/legal/mobile-eula
→ Submit for Beta Review (if not auto-submitted by CI already)
```

The CI step `Submit build for external Beta Review` in
`.github/workflows/mobile-ios-ci.yml` (lines ~980-1100) PATCHes
`betaAppReviewDetails` + POSTs `betaAppReviewSubmissions` on every
build, so by the time you reach this step the build is usually
already in `WAITING_FOR_REVIEW`. If it shows
`MISSING_REQUIRED_DATA`, the most common cause is the
`privacyPolicyUrl` not being readable yet — go back to pre-flight
step (1).

### 2c. Toggle the public link ON

Once Apple flips the build to **"Ready to Test"** (1-4 hours after
Beta Review submission, see Step 4):

```
→ vpn-beta → "Public Link" toggle  (top right of the group page)
→ Enable
→ Optionally set a tester cap (default 10,000 — leave as-is for v1)
→ Copy the URL: https://testflight.apple.com/join/<token>
```

That URL goes onto:

- `iogrid.org/vpn` ("Get the iOS beta" CTA)
- `iogrid.org/welcome` mobile-section
- Twitter / X bio + product-hunt launch post

Up to 10,000 testers can install via that link **without an Apple-ID
allowlist**. The link can be revoked at any time from the same toggle.

## Step 3 — Skip: Beta App Review submission

Already automated. Don't touch the "Submit for Review" button in the
UI unless CI is failing — see the
`Submit build for external Beta Review` step in
`.github/workflows/mobile-ios-ci.yml`. The CI step:

1. PATCHes `appInfoLocalizations` so `privacyPolicyUrl` is non-null
   (Apple's gate on `betaAppReviewDetails` persistence — see
   CONTRIBUTING.md gotcha 26).
2. PATCHes `betaAppReviewDetails` with `contactEmail`,
   `contactPhone`, `notes`, `demoAccountRequired=false`.
3. Polls the just-uploaded build by `version=RUN_NUMBER` until it
   leaves `PROCESSING`.
4. POSTs `betaAppReviewSubmissions` referencing that build.

If the CI step warns `MISSING_REQUIRED_DATA`, the next run usually
self-heals once the privacy URL is live. No founder action.

## Step 4 — Apple's review path + typical timing

| Stage | Where to watch | Typical |
|---|---|---|
| Build upload → PROCESSED | App Store Connect → TestFlight → iOS | 5-15 min |
| `WAITING_FOR_BETA_REVIEW` → `IN_BETA_REVIEW` | TestFlight → build row | 0-2 h |
| `IN_BETA_REVIEW` → `APPROVED` (build "Ready to Test") | TestFlight → build row | 1-4 h on a first build; ~minutes on subsequent versions if no new permissions added |
| Public link reachable + installs working | open the URL on an iPhone | immediate after Step 2c |
| Full App Store review (when you click "Submit for Review" on the App Store side, not Beta) | App Store Connect → App Store → 1.0 Prepare for Submission | 1-3 days for first submission, 1-24 h for updates |

Rejections (rare for a VPN app with `NEPacketTunnelProvider` clearly
declared): Apple emails the App Store Connect account. Common reasons
for a VPN app:

- Missing `NEPacketTunnelProvider` justification — already in
  `mobile/ios/app.json` `infoPlist.NEProviderClasses`.
- "Demo account required" answered Yes but no creds provided — CI
  forces this to `false`.
- Privacy nutrition label mismatch — re-do Step 1 with the same "No"
  answer and re-submit.

## Step 5 — Post-approval verification

```bash
# Public link returns HTML on a non-Apple-ID-allowlisted device:
curl -sIL "https://testflight.apple.com/join/<token>" | head -3

# Internal smoke: install via the link on a phone NOT signed into
# the iogrid Apple Developer team account. Tap → TestFlight installs
# → open iogrid → Connect → traffic-test ifconfig.me returns a
# residential exit IP (one of the paired daemons from
# docs/runbooks/vpn/operator-paired-daemon.md).
```

Once the smoke is green, post the link in #vpn-beta channel and
flip issue #574 to `status/uat`. Founder closes after Apple's
TestFlight email arrives confirming "Ready to Test".

## Rollback

The public link can be **revoked instantly** by toggling
"Public Link" OFF on the vpn-beta group page. Existing installed
builds keep working until their 90-day TestFlight expiry, but no
new installs can happen.

If the App Privacy nutrition answer needs to change (e.g. if we
add Sentry later), re-do Step 1 and re-submit for review — Apple
re-reviews any privacy-label change.

## Reference

- `web/src/app/legal/mobile-privacy/page.tsx` — the public privacy policy Apple's reviewer reads.
- `web/src/app/legal/mobile-eula/page.tsx` — the iogrid-specific EULA overrides on top of Apple's stdeula.
- `.github/workflows/mobile-ios-ci.yml` (step "Submit build for external Beta Review") — the CI auto-submit.
- `docs/runbooks/mobile-ios-testflight-bootstrap.md` — one-time TestFlight wiring (prerequisite).
- CONTRIBUTING.md gotcha 26 — the `privacyPolicyUrl` → `betaAppReviewDetails` ordering gate root-cause memo.
