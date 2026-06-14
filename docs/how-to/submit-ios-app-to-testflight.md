# Submitting an iogrid-built iOS app to TestFlight

The iogrid build-gateway compiles your iOS app on a provider Mac (Xcode 26) and
returns the build artifact. To distribute it to testers via **TestFlight**, you
submit a **signed device build (`.ipa`)** to App Store Connect. This is the
customer-side step that follows `POST /v1/builds` — see
[`submit-ios-build.md`](./submit-ios-build.md) and the
[ping customer guide](./ping-iogrid-customer-paste-prompt.md).

> iogrid's CI uploads *its own* app to TestFlight automatically; the founder runs
> no manual Xcode/Organizer steps. **This guide is for build-gateway customers**
> who want the app they built through iogrid on TestFlight.

## Prerequisites
- Apple Developer account + App Store Connect access for the app's bundle id.
- An **Apple Distribution** signing certificate + an App Store provisioning profile.
- An **ASC API key** (Issuer ID, Key ID, `.p8`) for non-interactive upload.

## 1. Build a *signed device* `.ipa` through the API
A plain `xcodebuild build` yields a **simulator** `.app` — not TestFlight-eligible.
For TestFlight you need a signed **device archive → export**. Submit a build whose
`build_command` archives and exports a signed `.ipa`, supplying your signing
material to the build (e.g. a committed `fastlane match` repo, or base64 cert +
profile decoded in the command):

```sh
xcodebuild -workspace App.xcworkspace -scheme App \
  -configuration Release -destination 'generic/platform=iOS' \
  -archivePath build/App.xcarchive archive \
  CODE_SIGN_IDENTITY="Apple Distribution" \
  PROVISIONING_PROFILE_SPECIFIER="<profile-name>"

xcodebuild -exportArchive -archivePath build/App.xcarchive \
  -exportPath build/ipa -exportOptionsPlist ExportOptions.plist
```

`ExportOptions.plist`: `method=app-store`, your `teamID`, `signingStyle=manual`.

## 2. Download the `.ipa` artifact
```sh
curl -sS https://build.iogrid.org/v1/builds/<build_id>/artifact \
  -H "Authorization: Bearer $IOGRID_API_KEY" -o App.ipa
```

## 3. Upload to App Store Connect (choose one)
- **`xcrun altool`** (scriptable):
  ```sh
  xcrun altool --upload-app -f App.ipa -t ios \
    --apiKey "$ASC_KEY_ID" --apiIssuer "$ASC_ISSUER_ID"
  ```
- **EAS Submit** (Expo apps): `eas submit -p ios --path App.ipa`
- **Xcode → Organizer** → *Distribute App* → *App Store Connect* (GUI).

(`xcrun notarytool` / Transporter also work; `altool` is the simplest CLI path.)

## 4. Assign to testers
App Store Connect → your app → **TestFlight** → wait for the build to reach
`processingState = VALID` → add it to an **Internal** (installs immediately) or
**External** (needs a one-time Beta App Review) tester group. Testers install via
the TestFlight app.

## Notes
- The gateway artifact store is currently in-process — **download promptly**; a
  gateway pod restart drops in-flight artifacts (durable object storage is a
  tracked follow-up).
- For a fully one-command turnkey path, bake signing + export into your repo
  (committed `ExportOptions.plist`, `fastlane match`) so a single `build_command`
  emits the `.ipa`.

Refs: #700 (G2 EPIC), #770 (ping = first customer), #759/#764 (turnkey build-gateway).
