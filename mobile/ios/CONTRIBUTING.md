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
