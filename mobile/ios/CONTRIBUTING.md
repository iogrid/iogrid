# Contributing — mobile/ios

> Engineering notes for the iogrid iOS app. Captures the build-
> pipeline gotchas this codebase has hit, so future sessions don't
> re-learn them the hard way.

## Build pipeline mental model

The build is NOT plain `expo build`. The NetworkExtension target
requires post-prebuild Xcode-project surgery via Ruby. The order:

1. `npx expo prebuild --platform ios --no-install --clean`
   - Regenerates `ios/` from `app.json` + config plugins
   - `plugins/with-network-extension.ts` copies Swift sources + entitlements + writes the extension's Info.plist
2. `ruby scripts/add-network-extension-target.rb`
   - Adds the PacketTunnelProvider Xcode target via `xcodeproj` gem
   - Creates the embed phase + product reference
   - Sets canonical build settings (SKIP_INSTALL=YES, INFOPLIST_FILE, CODE_SIGN_ENTITLEMENTS, etc.)
3. `cd ios && pod install`
   - Pods integration. The Podfile's `post_install` block MUST skip extension targets (`target.product_type == 'com.apple.product-type.app-extension'`) when forcing SKIP_INSTALL=NO; setting NO on extensions causes double-install.
4. `xcodebuild build -workspace ios/iogrid.xcworkspace -scheme iogrid`

CI in `.github/workflows/mobile-ios-ci.yml` orchestrates 1-4 + the
TestFlight signing path.

## Gotchas (in the order this session hit them)

### 1. `xcode` (Node) vs `xcodeproj` (Ruby) for extension targets

The Node `xcode` package's `addTarget` API doesn't fully wire
extension targets — no XCSwiftPackageProductDependency helper, no
PBXTargetDependency embed phase, no SKIP_INSTALL coordination. Use
the Ruby `xcodeproj` gem (what CocoaPods uses internally).

### 2. setup-node `cache: 'npm'` needs `cache-dependency-path`

Default looks for `package-lock.json` at the repo root. With the
app at `mobile/ios/`, the workflow MUST set
`cache-dependency-path: mobile/ios/package-lock.json` or
setup-node fails before any other step runs.

### 3. GitHub Actions forbids `secrets.*` in step-level `if:`

Direct `if: ${{ secrets.X != '' }}` is rejected with "workflow file
issue". Indirect via job-level env:
```yaml
env:
  HAS_APPLE_SECRETS: ${{ secrets.APP_STORE_CONNECT_PRIVATE_KEY != '' && 'true' || 'false' }}
# then in steps:
if: ${{ env.HAS_APPLE_SECRETS == 'true' }}
```

### 4. WireGuardKit + Xcode 26 / Swift 6 = no go

wireguard-apple's `Package.swift` uses Swift 5 manifest syntax that
fails to compile under Xcode 26's Swift 6 toolchain. Surface:
```
xcodebuild: error: Could not resolve package dependencies:
  Invalid manifest (compiled with: ["/Applications/Xcode_26.3.app/.../swiftc", ...])
```
zx2c4 hasn't shipped Swift 6 compat upstream. Workaround: DEFER
WireGuardKit. The PacketTunnelProvider Swift file is gated behind
`#if canImport(WireGuardKit)` so it compiles without the dep
linked; the tunnel just fails with `wireGuardKitNotLinked` at
start time. Tracked in #576.

### 5. zx2c4 uses `master`, not `main`

When we DO eventually wire WireGuardKit:
```ruby
WIREGUARD_BRANCH = 'master'  # NOT 'main' — old-school naming
```

### 6. Extension `.appex` product reference must be explicit

`xcodeproj` 1.x against Xcode 26 pbxproj may leave the
`product_reference` nil after `new_target(:app_extension, ...)`.
Result: build emits `.appex` with empty basename, xcodebuild rejects
with "Multiple commands produce '.../Release-iphonesimulator/.appex'".
Fix: create the PBXFileReference explicitly in
`project.products_group` with name/path/source_tree/explicit_file_type
set. See `scripts/add-network-extension-target.rb` for the canonical
pattern.

### 7. NE extension must have SKIP_INSTALL=YES

App extensions are embedded into the main app via the Embed App
Extensions copy phase; they must NOT be separately installed at
`/Applications/<name>.appex`. SKIP_INSTALL=NO on the extension
target triggers "Multiple commands produce '/Applications/.appex'"
during xcodebuild's install step. The Podfile post_install patch
that sets SKIP_INSTALL=NO globally MUST exclude extension targets:
```ruby
next if target.product_type == 'com.apple.product-type.app-extension'
```

### 8. Embed phase needs CodeSignOnCopy

Without `'CodeSignOnCopy'` in the embed file's ATTRIBUTES, the
embedded .appex isn't re-signed under the main app's provisioning
profile during the copy step. App Store ingestion fails with
"Invalid bundle. The bundle ... is invalid." Always include it.

### 9. Native modules on web target

The TunnelControl native module (NETunnelProviderManager bridge) is
iOS-only. For Playwright UAT via `expo start --web`, provide a
`.web.ts` stub that no-ops the iOS APIs but keeps the same JS
shape so the toggle screen renders. Set the package's `main` field
to `src/index` (no extension) so Metro's platform-extension resolver
picks `.web.ts` vs `.ts` based on target.

### 10. expo-secure-store accessGroup field name

The TypeScript type for `SecureStoreOptions` uses `accessGroup`,
NOT `keychainAccessGroup` (which is what Swift uses for the
underlying Apple API). Easy to confuse since the Keychain layer
calls it `kSecAttrAccessGroup`.

### 11. Network.NWPath vs NetworkExtension.NWPath

Importing both `Network` and `NetworkExtension` frameworks in the
same Swift file (which the PacketTunnelProvider extension must do)
makes `NWPath` ambiguous. Use the fully qualified `Network.NWPath`
for the modern Swift Network framework type. `NWPathMonitor` itself
is unambiguous (only Network framework has it).

### 12. NWPath.Status has no rawValue

`os_log(..., type: .info, newPath.status.rawValue)` fails to
compile under iOS 26 SDK — `Network.NWPath.Status` is a plain
`@frozen public enum` without `rawValue`. Format as a string:
`"\(newPath.status)"` with the `%{public}@` format specifier.

### 13. TARGETED_DEVICE_FAMILY embedded quotes

Setting `TARGETED_DEVICE_FAMILY` via xcodeproj as `'"1,2"'` (a
string containing embedded quotes) causes Xcode 26 to parse it as
two items `'"1'` and `'2"'` — the embedded quotes become part of
the value. Use plain `'1,2'` without quotes.

### 14. Expo SDK 56 .icon (Icon Composer) format breaks Xcode 26 actool

`"ios": { "icon": "./assets/expo.icon" }` in app.json points at a
`.icon` directory (the new Icon Composer format). Xcode 26's actool
chokes on it during the asset catalog compile step — silently runs
for ~7 minutes then errors with "unable to open dependencies file"
on `assetcatalog_dependencies_thinned`. Workaround: drop the iOS
icon override, let Expo fall back to the top-level
`"icon": "./assets/images/icon.png"` which uses the legacy
Images.xcassets format.

### 15. Root `.gitignore` blanket `*.png` excludes asset images

The root iogrid `.gitignore` has `*.png` (for docker / tart image
bloat). Mobile asset PNGs (icon, splash, adaptive icons, favicon)
must be force-added via `git add -f` or the `.gitignore` updated
to exempt `mobile/ios/assets/`. Otherwise prebuild fails with
`ENOENT: open ./assets/images/icon.png`.

### 16. Local Expo native module — `expo-module.config.json` uses `apple` not `ios`

SDK 56's `expo-modules-autolinking` rejects `platforms: ["ios"]` and
silently doesn't discover the package. Correct shape:
```json
{ "platforms": ["apple"], "apple": { "modules": ["MyModule"] } }
```

### 17. Local Expo native module — podspec MUST live at `<module>/ios/`

`expo-modules-autolinking resolve` looks for the podspec at
`<package-root>/ios/<podName>.podspec`. If you put it at the package
root, autolinking finds the module via `search` but `resolve` returns
empty pods → no Pod gets generated → "Cannot find native module" at
runtime. Always: `<module>/ios/<podName>.podspec`.

### 18. Podspec `source_files` is relative to the podspec's directory

If the podspec lives at `<module>/ios/<podName>.podspec`, then
`s.source_files = "ios/**/*.{h,m,swift}"` resolves to `<module>/ios/ios/...`
which doesn't exist → empty Pod target → `error: no such module
'MyModule'` at ExpoModulesProvider.swift:N. Use `s.source_files =
"*.{h,m,swift}"` to pick up files in the same dir as the podspec.

### 19. Maestro `assertVisible` doesn't accept `timeout:`

Yields `Unknown Property: timeout at .maestro/01-launch.yaml:-1:-1`.
Maestro's default polling interval (~30s) is adequate. Drop all
`timeout: <ms>` lines from `assertVisible` / `assertNotVisible`
directives.

### 20. Maestro `launchApp` doesn't accept `arguments:`

That's an Espresso / XCTest convention, not Maestro. `clearState`
+ `clearKeychain` already produce a fresh-state launch. Just use
plain `- launchApp`.

### 21. Maestro `assertVisible: "string"` is REGEX, not literal

The short form treats the string as a regex pattern. Parens become
capture groups, dots match any char, `*` and `+` are quantifiers.
A label like `"Best (auto)"` becomes the regex `Best (auto)` which
matches "Best auto" but NOT "Best (auto)" with literal parens —
the gate then fails on rendered text that visibly IS there.

Workarounds (pick one):
- Match a regex-safe substring: `- assertVisible: "Best"`
- Escape the metachars: `- assertVisible: "Best \\(auto\\)"` (YAML
  double-escape — the on-wire regex is `Best \(auto\)`)
- Use the long form which (per current Maestro 2.6) is still regex
  too: `- assertVisible:\n    text: "Best"`
- Prefer testID assertions when the label has metachars:
  `- assertVisible:\n    id: "region-row-auto"`

### 21b. text assertions FAIL even with substring when iOS a11y collapses parent+children

Companion to 21. Maestro's `textRegex` query reads rendered text
nodes. If you have a Pressable wrapping a child Text:

```jsx
<Pressable testID="row" onPress={...}>
  <Text>Label here</Text>
</Pressable>
```

iOS collapses this into a single accessibility element (the
Pressable) with a computed accessibilityLabel from the joined
children's text. But Maestro's `textRegex` doesn't see that
computed label — it queries rendered text nodes, and the child
Text is hidden behind the Pressable's collapsed a11y wrapper.

`assertVisible: id: "row"` works (testID is queryable).
`assertVisible: "Label"` FAILS even though the text visibly renders.

Resolution: PREFER testID assertions for any Pressable-wrapped
content. Text assertions for nav header titles, screen titles, or
standalone Text without a Pressable parent.

### 22. CONNECTING state visibility for the smoke gate

The PacketTunnelProvider start() rejects fast on simulator (no NE
host). The JS try/catch reverts state OFF→CONNECTING→OFF in <50ms
— faster than Maestro's polling. Hold CONNECTING visible 3000ms
in the catch path (src/app/index.tsx `holdConnectingVisible`).
Real UX improvement (Mullvad does the same) and the smoke gate's
`assertVisible: "CONNECTING"` becomes deterministic. The 3000ms
margin also covers `takeScreenshot` latency + the second tap that
toggles back through DISCONNECTING.

### 23. Maestro `- back` is unreliable on iOS for Expo Router stacks

Maestro's `backPressCommand` "completes" but doesn't actually pop
the Expo Router stack — the on-screen state stays whatever the
previous navigation put there. Confirmed via commands.json trace
on CI iter 9: backPressCommand status=COMPLETED followed by
assertVisible vpn-toggle status=FAILED (vpn-toggle is on /index,
which we should have popped back to).

Likely cause: Maestro's iOS back action triggers a swipe-from-left-
edge gesture, which only works if the screen has the standard nav
gesture enabled. Expo Router's default Stack may have a small back
button instead, and the swipe doesn't catch.

Workarounds:
- Cold-restart the app between dependent flows:
  `- launchApp:\n    stopApp: true`  ← brings app down + up,
   reaches the root route
- Use explicit `- tapOn:\n    text: "<previous-screen-title>"` if
  the nav-bar back button text is queryable
- Restructure flows so each is self-contained with `clearState`
  + `clearKeychain` + `launchApp` (slowest but most deterministic)

For iogrid: flows 03 + 04 had `- back` housekeeping at the end —
dropped both, flow 04 cold-restarts. Flow 05 already self-contained.

### 24. Keychain search list must NOT append login keychain

macos-latest runners are sometimes reused across jobs. The login
keychain can carry stale "Apple Distribution: HATICE YILDIZ BAYSAL"
certs from prior CI runs on sibling team projects (vcard / cinova /
ping all share the Dynolabs Apple Developer team). When xcodebuild
manual signing looks up `CODE_SIGN_IDENTITY="Apple Distribution"`
it matches by GENERIC NAME — and picks the FIRST one in the search
list.

If the workflow appends our fresh keychain to the existing user
search list (`security list-keychains -d user -s "$KEYCHAIN_PATH"
$(security list-keychains -d user)`), xcodebuild may pick a stale
cert from the login keychain instead of the freshly fastlane-cert
one. Archive then fails: `Signing certificate ... serial XXX is
not valid for code signing. It may have been revoked or expired.`

Fix: REPLACE the search list with only our new keychain:
```bash
security list-keychains -d user -s "$KEYCHAIN_PATH"
```

iter 11 (run 26789507723) carries the fix. Confirmed by iter 10
failure: fastlane created cert 9NK3S33W6K but xcodebuild signed
with a DIFFERENT serial 1CE647F4992DFD0CEAA4528443705912.

## Iterating CI locally

You can't iterate Xcode 26 builds on the bastion (no macOS). The
fastest feedback loop:

1. Push to a feature branch
2. `gh run watch $(gh run list --workflow=mobile-ios-ci.yml --limit 1 --json databaseId -q '.[0].databaseId')`
3. On failure: `gh run view <id> --log-failed | grep error:`
4. Fix + push + repeat

Average wall time per iteration: ~4-5 min once CocoaPods cache is warm.

### 25. Age-based cert pre-revoke (cross-project safety)

The Dynolabs Apple Developer team is shared across iogrid / vcard /
cinova / ping. Each project's CI has its own pre-revoke loop that
clears stale Distribution certs before fastlane cert mints a fresh
one (Apple caps Distribution certs at 2 per team).

Naive pre-revoke deletes ALL distribution certs. That works in
isolation but BREAKS when sibling project's CI is in-flight: their
just-created cert gets revoked mid-archive, xcodebuild's manual
signing then fails with "Signing certificate ... is not valid. It
may have been revoked or expired" using a cert serial that doesn't
match what fastlane cert just created (because Apple revoked it in
between).

Concrete failure mode caught 2026-06-02 across iogrid iter 10/12
runs `26788950651` + `26789892395` — three different cert serials
across three iterations, each killed mid-archive by a sibling's
pre-revoke step.

Fix: filter pre-revoke by age. Apple's `certs.expirationDate` is
set to creation + 1 year; recent in-flight certs have
expirationDate close to (now + 365 days). Skip certs younger than
60 minutes (expirationDate > now + 365d − 60min). Truly-stale
certs (hours old) get cleaned up; sibling in-flights are spared.

```python
threshold = now + datetime.timedelta(days=365) - datetime.timedelta(minutes=60)
for cert in dist_certs:
    exp = datetime.datetime.fromisoformat(cert['attributes']['expirationDate'])
    if exp >= threshold:
        print(f"  SKIP {cert['id']} — sibling CI in-flight")
        continue
    revoke(cert['id'])
```

Cross-port the same logic to vcard / cinova / ping workflows
(commits cae2af1, 1e5ff1e, 46406b5 on those repos).

Trade-off: when 2 fresh certs already exist on the team (one per
in-flight CI), a third project's CI hits Apple's 2-cert limit at
fastlane cert. That's preferable to nuking each other's certs
mid-archive. The 60-min window self-heals.

### 26. NEVER call DELETE /v1/betaTesters — cross-app blast radius

Apple's ASC API exposes `betaTesters` as a per-team-per-email
record. ONE row per email, shared across ALL apps in the team.
`DELETE /v1/betaTesters/{id}` removes the row entirely — and with
it, every app's beta-group association for that email.

Symptom caught 2026-06-02 during iogrid #575 UAT recovery:
founder reported "only iogrid is there" in TestFlight on iPhone —
all 10 other team apps gone (vCard, Cinova, Ping, 6 Bank Dhofar
apps, Phrasely). Earlier iteration in `fix-575-deep.yml` called
DELETE on 3 "stale" tester records to clean up before recreate.
Those records were the canonical ones for the other 10 apps;
deleting them revoked founder's TestFlight access across the team.

Rules:

1. **NEVER call `DELETE /v1/betaTesters/{id}`** in any workflow or
   script. It's a destructive cross-app operation.

2. **For removing from one specific group**, use the relationships
   endpoint:
   `DELETE /v1/betaGroups/{group_id}/relationships/betaTesters`
   with payload `{data: [{type: 'betaTesters', id: tester_id}]}`.
   Removes the link, keeps the record.

3. **For adding team members as internal testers**:
   - First PATCH the user to `allAppsVisible=true` via
     `PATCH /v1/users/{user_id}`
   - Then POST `/v1/betaTesters` with single `betaGroups`
     relationship per call. Apple auto-resolves to the team-member
     record.
   - Do NOT include `apps` relationship in the POST
     (`ENTITY_ERROR.RELATIONSHIP.NOT_ALLOWED`).

4. **Recovery if you accidentally DELETEd**: restore via
   `restore-v3-direct-add.yml` pattern from the 2026-06-02 session
   — PATCH user.allAppsVisible=true, then POST betaTesters with
   single betaGroups rel per internal group (one POST per group,
   HTTP 201 means re-linked).

### 27. External Beta Review needs `privacyPolicyUrl` on the en-US appInfoLocalization

Apple's `POST /v1/betaAppReviewSubmissions` returns
`MISSING_REQUIRED_DATA` when the app's en-US `appInfoLocalization`
has no `privacyPolicyUrl` set, even though the build artefact
itself has nothing wrong. Worse, the symptom is silent on the
route the iogrid CI was using before: `PATCH
/v1/betaAppReviewDetails` returns 200 with the new `notes` /
`contactEmail` / `contactPhone` fields, but a follow-up GET reads
them back as `null`. The server is silently rejecting the changes
because the app-info localization is missing the privacy URL.

Caught 2026-06-02 while implementing issue #600 (Track 5 / EPIC
#581). Build 61 was uploaded fine but every Beta Review submission
retry kept returning 422, and the `fix-575-v*` series of
PATCH-different-fields workflows all failed the same way because
none of them touched the App Info side.

**The correct order of operations for a new app**:

1. `PATCH /v1/appInfoLocalizations/{en-US-id}` with
   `privacyPolicyUrl=https://iogrid.org/legal/mobile-privacy`. The
   URL must resolve to actual content (Apple HEAD-checks it);
   coordinate with the web team if the route 404s.
2. `PATCH /v1/betaAppReviewDetails/{id}` with the FULL attribute
   set (`contactEmail`, `contactFirstName`, `contactLastName`,
   `contactPhone`, `demoAccountRequired`, `notes`) in a single
   request. Partial PATCHes silently no-op for some fields.
3. Verify by reading back `betaAppReviewDetails` — if any of
   `contactEmail` / `contactPhone` / `notes` is still null, the
   privacy URL is still mid-propagation; retry in the next CI
   run rather than blocking the rest of the pipeline.
4. `POST /v1/betaAppReviewSubmissions` with the build relationship.
   201 = created, 409 = already submitted (idempotent — treat as
   success). Poll `betaReviewState` for `IN_BETA_REVIEW`
   transition; APPROVED requires human review and lands hours
   later.

The `Submit build for external Beta Review` step in
`.github/workflows/mobile-ios-ci.yml` encodes all four operations
+ their failure modes; the new `.github/workflows/diagnose-beta-review.yml`
(workflow_dispatch) runs a no-side-effect probe that prints the
FULL Apple response body so future regressions can be diffed
against the live state in a single CI run.

### 28. Apple validator rejects `+447700900000` (UK Ofcom test range) as `INVALID_PHONE_NUMBER`

Even though `+447700900000` is the official Ofcom test number,
Apple's `PATCH /v1/betaAppReviewDetails` returns HTTP 409
`ENTITY_ERROR.ATTRIBUTE.INVALID.INVALID_PHONE_NUMBER` with
detail "The format of the phone number is invalid". The US test
range `+1 415 555 01XX` is accepted. Use that for any non-PII
contactPhone needed for review-details submission. Caught
2026-06-02 on the second iteration of #600's fix.

### 29. Xcode 26 strict module imports need `#include <sys/types.h>` for unsigned typedefs

Xcode 26's Swift 6 toolchain compiles C/C++ headers in strict
module-imports mode. Headers that use `u_int32_t`, `u_char`,
`u_short` etc. without explicitly including `<sys/types.h>` fail
to emit the `.pcm` module file with:

```
declaration of 'u_int32_t' must be imported from module
'_DarwinFoundation1.unsigned_types.u_int32_t' before it is required
```

Vendored upstream C headers (here: WireGuardKit's
`Sources/WireGuardKitC/WireGuardKitC.h`) are the most common
casualty — they used to compile fine on Xcode 15 because the
older Clang module compiler implicitly synthesized the unsigned
typedefs.

**Fix**: add `#include <sys/types.h>` at the top of any header
that uses `u_int*_t` / `u_char` / `u_short` / `caddr_t` and was
written before Xcode 26. Don't patch upstream — patch the
vendored copy (`mobile/ios/vendor/...`) so re-vendoring from
upstream re-applies the fix via `mobile/ios/scripts/vendor-wireguard.sh`.

### 30. add-network-extension-target.rb must register ALL source files

When Track 3 split PacketTunnelProvider.swift into three files
(`PacketTunnelProvider.swift` + `WGTunnel.swift` + `Stats.swift`),
only the root file was registered into the Xcode extension
target via `ext_target.add_file_references`. Result: the build
silently dropped `WGTunnel` + `Stats` from the appex compile
sources, producing "Cannot find 'WGTunnel' in scope" errors at
the `Build app for iOS Simulator` step.

Fix: enumerate every Swift file in
`mobile/ios/native/ios/PacketTunnelProvider/` and register each
one. The Expo plugin's `withDangerousMod` already copies the
whole directory recursively, so the files exist on disk in
`ios/PacketTunnelProvider/` after prebuild — the Ruby script
just has to add them to the target's Compile Sources phase.

### 31. WireGuardKitGo's libwg-go.a must be on the linker search path

The vendored WireGuardKit Package.swift declares
`.linkedLibrary("wg-go")` on its WireGuardKitGo target. SwiftPM
does NOT build this library — it's a Go static library produced
by `Sources/WireGuardKitGo/Makefile` via cross-compiling Go to
both x86_64 + arm64 slices via cgo, then lipo'd into a fat .a.

CI must run that Make step BEFORE xcodebuild, AND the resulting
`libwg-go.a` must land on a path that ld searches. Three workable
locations (in order of robustness):

1. **`/usr/local/lib/libwg-go.a`** — default ld search path
   on macOS. Works without any xcconfig changes. Use this for
   CI. (Caveat: needs `sudo cp` on the runner.)
2. **`Sources/WireGuardKitGo/libwg-go.a`** — SwiftPM's target
   source dir. Theoretically xcodebuild adds this to
   LIBRARY_SEARCH_PATHS for `.linkedLibrary` resolution, but
   in practice we found it NOT searched (CI 26831852258 failed
   with `ld: library 'wg-go' not found` despite the file
   existing there).
3. **`Sources/WireGuardKitGo/x86_64/libwg-go.a` + arm64**
   per-arch dirs — overkill for our use case since we lipo into
   a fat .a; mentioned for completeness.

The Make step needs to be run for BOTH `iphonesimulator` (for the
sim Maestro gate) and `iphoneos` (for the archive step), with
matching ARCHS per platform. The simulator build runs first; the
archive overwrites `/usr/local/lib/libwg-go.a` with the device
slice just before xcodebuild archives.

## Maestro flows

Numbered + orchestrated via `00-all.yaml` (vcard convention — never
parallelize, scenarios reuse state). Add a new flow as `06-…yaml`
and add it to `00-all.yaml`. Use `takeScreenshot:` directives for
CI artifact evidence — they land in `$RUNNER_TEMP/maestro-screenshots/`
and get uploaded by the workflow's artifact step.

## TestFlight readiness check

Before pushing a commit that's supposed to land in TestFlight,
verify locally:

```bash
cd mobile/ios
npx tsc --noEmit                                                 # JS layer typecheck
node scripts/check-account-derivation.mjs                        # account ID determinism
# Maestro can only be run on macOS with Xcode — defer to CI
```
