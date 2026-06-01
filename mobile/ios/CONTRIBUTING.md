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
