#!/usr/bin/env ruby
# add-network-extension-target.rb — adds the PacketTunnelProvider NE
# target + WireGuardKit SwiftPM dep to the Xcode project generated
# by `expo prebuild`.
#
# Why Ruby + xcodeproj gem: this is the canonical, battle-tested API
# surface for Xcode project mutation. The Node `xcode` npm package's
# extension-target + SwiftPM helpers are fragile (no
# PBXTargetDependency wiring, no XCRemoteSwiftPackageReference
# support). CocoaPods itself is Ruby + uses this gem — anything
# else is fighting upstream.
#
# Idempotent: bails cleanly if the target already exists, so CI can
# call this script every build without state poisoning.
#
# Usage:
#   cd mobile/ios
#   ruby scripts/add-network-extension-target.rb
#
# Run AFTER `npx expo prebuild --platform ios --clean` — the script
# requires `ios/iogrid.xcodeproj` to exist + the Swift source +
# entitlements files to be present at `ios/PacketTunnelProvider/`
# (which the Expo config plugin handles).
#
# Refs #568.

require 'xcodeproj'

# ── Configuration ───────────────────────────────────────────────
PROJECT_PATH         = 'ios/iogrid.xcodeproj'
EXTENSION_NAME       = 'PacketTunnelProvider'
EXTENSION_BUNDLE_ID  = 'io.iogrid.app.PacketTunnelProvider'
MAIN_APP_NAME        = 'iogrid'
APP_GROUP            = 'group.io.iogrid.app'
DEPLOYMENT_TARGET    = '16.0'
SWIFT_VERSION        = '5.0'

# WireGuardKit SwiftPM dep — DEFERRED.
#
# wireguard-apple's Package.swift uses Swift 5 manifest syntax that
# fails to compile under Xcode 26 / Swift 6 toolchain ("Invalid
# manifest" at the manifest compilation step). Upstream zx2c4 hasn't
# updated for Swift 6 yet.
#
# Pragmatic pivot for the v1 TestFlight ship: PacketTunnelProvider.swift
# already gates the WG-using code paths behind `#if canImport(WireGuardKit)`
# so the extension target compiles cleanly without the dep linked.
# The tunnel WILL FAIL to start (returns wireGuardKitNotLinked error)
# until WireGuardKit is wired in — but the app reaches TestFlight,
# the UI works, and the toggle / region picker / account ID flows
# are testable.
#
# Wire-in options for the follow-up:
#   1. Wait for upstream wireguard-apple to fix Swift 6 compat
#   2. Vendor a known-good fork (github.com/mullvad/wireguardkit-rs or similar)
#   3. Patch upstream's Package.swift via a custom branch
#
# Tracked in EPIC #566 as the "data plane wire" follow-up.
WIREGUARDKIT_ENABLED = false

# ── Pre-flight ──────────────────────────────────────────────────
unless File.exist?(PROJECT_PATH)
  abort "Project not found at #{PROJECT_PATH} — run `npx expo prebuild --platform ios --clean` first."
end

project = Xcodeproj::Project.open(PROJECT_PATH)

# Idempotency: bail if the target already exists.
if project.native_targets.any? { |t| t.name == EXTENSION_NAME }
  puts "[+] #{EXTENSION_NAME} target already exists — no-op."
  exit 0
end

# ── 1. Create the extension target ─────────────────────────────
puts "[+] Creating #{EXTENSION_NAME} target..."
ext_target = project.new_target(
  :app_extension,        # product type → com.apple.product-type.app-extension
  EXTENSION_NAME,
  :ios,
  DEPLOYMENT_TARGET,
)

# CRITICAL: in xcodeproj 1.x against Xcode 26 pbxproj, `new_target`
# may NOT auto-create the PBXFileReference (product_reference) in
# the Products group — leaving the build with an .appex whose name
# is literally empty. Explicit creation via Products group is the
# only reliable path.
#
# Symptom that prompted this fix: CI commit 7ef04c0 failed with
# 'Multiple commands produce .../Release-iphonesimulator/.appex'
# (note empty basename before .appex).
products_group = project.products_group
if ext_target.product_reference.nil?
  puts "[+] product_reference was nil — creating explicitly under Products group"
  product_ref = products_group.new_reference("#{EXTENSION_NAME}.appex")
  product_ref.name = "#{EXTENSION_NAME}.appex"
  product_ref.path = "#{EXTENSION_NAME}.appex"
  product_ref.include_in_index = '0'
  product_ref.source_tree = 'BUILT_PRODUCTS_DIR'
  product_ref.explicit_file_type = 'wrapper.app-extension'
  ext_target.product_reference = product_ref
else
  # Existing reference — set the canonical attributes anyway, in case
  # xcodeproj left some empty.
  ext_target.product_reference.name = "#{EXTENSION_NAME}.appex"
  ext_target.product_reference.path = "#{EXTENSION_NAME}.appex"
  ext_target.product_reference.include_in_index = '0'
  ext_target.product_reference.source_tree = 'BUILT_PRODUCTS_DIR'
  ext_target.product_reference.explicit_file_type = 'wrapper.app-extension'
end
puts "[+] product_reference: path=#{ext_target.product_reference.path.inspect} name=#{ext_target.product_reference.name.inspect}"

# Bundle identifier + entitlements + Info.plist + Swift version on
# both Debug + Release configurations.
#
# CRITICAL: PRODUCT_NAME + EXECUTABLE_NAME must be LITERAL strings
# ("PacketTunnelProvider") — NOT "$(TARGET_NAME)" variable expansion.
# xcodeproj 1.x against Xcode 26 pbxproj sometimes creates the target
# with the `name` field set on the PBXNativeTarget JSON node but the
# build setting expansion of $(TARGET_NAME) still resolves to empty
# at link/install time. Confirmed via CI diagnostic on commit ece853a:
# product_reference.path = "PacketTunnelProvider.appex" (good) but
# xcodebuild's link command produced "<empty>.appex" because
# EXECUTABLE_NAME defaulted to $(PRODUCT_NAME) which expanded to
# empty. Setting both literally side-steps the variable evaluation
# entirely.
ext_target.build_configurations.each do |bc|
  bc.build_settings.merge!(
    'PRODUCT_NAME'                 => EXTENSION_NAME,
    'EXECUTABLE_NAME'              => EXTENSION_NAME,
    'PRODUCT_BUNDLE_IDENTIFIER'    => EXTENSION_BUNDLE_ID,
    'INFOPLIST_FILE'               => "#{EXTENSION_NAME}/Info.plist",
    'CODE_SIGN_ENTITLEMENTS'       => "#{EXTENSION_NAME}/PacketTunnelProvider.entitlements",
    'SWIFT_VERSION'                => SWIFT_VERSION,
    'IPHONEOS_DEPLOYMENT_TARGET'   => DEPLOYMENT_TARGET,
    'TARGETED_DEVICE_FAMILY'       => '"1,2"',
    # NE extension MUST be SKIP_INSTALL=YES — Xcode embeds it into the
    # main app's PlugIns folder via the Embed App Extensions phase, not
    # as a separate installable. SKIP_INSTALL=NO double-installs and
    # triggers "Multiple commands produce '/Applications/.appex'" on
    # xcodebuild — exactly the failure mode CI surfaced on commit 77b442a.
    'SKIP_INSTALL'                 => 'YES',
    'LD_RUNPATH_SEARCH_PATHS'      => '"$(inherited) @executable_path/Frameworks @executable_path/../../Frameworks"',
    'CODE_SIGN_STYLE'              => 'Manual',  # CI uses fastlane-fetched profile
  )
end

# ── 2. Add Swift source file to the target ─────────────────────
puts "[+] Adding PacketTunnelProvider.swift to the target..."
ext_group = project.main_group.new_group(EXTENSION_NAME, EXTENSION_NAME)
swift_ref = ext_group.new_file('PacketTunnelProvider.swift')
ext_target.add_file_references([swift_ref])

# ── 3. WireGuardKit SwiftPM dep — DEFERRED until upstream Swift 6 compat
if WIREGUARDKIT_ENABLED
  raise 'WireGuardKit wiring is currently disabled — see comment above the flag in this file. Re-enable only when upstream wireguard-apple supports Swift 6.'
end
puts "[+] WireGuardKit SwiftPM dep DEFERRED (upstream Swift 6 compat). Tunnel data plane will surface 'wireGuardKitNotLinked' error until wired."

# ── 4. Embed extension in the main app ─────────────────────────
puts "[+] Embedding #{EXTENSION_NAME} into #{MAIN_APP_NAME}..."
main_target = project.native_targets.find { |t| t.name == MAIN_APP_NAME }
abort "Main target #{MAIN_APP_NAME} not found — is the Expo prebuild output correct?" unless main_target

# Target dependency so building the main app forces the extension build.
main_target.add_dependency(ext_target)

# Embed App Extensions build phase. Match either:
#   - by name "Embed App Extensions" (what Xcode UI creates)
#   - by dst_subfolder_spec == '13' (Plug-ins, raw value in pbxproj)
# Whichever Expo prebuild + earlier runs created.
embed_phase = main_target.build_phases.find do |phase|
  next false unless phase.is_a?(Xcodeproj::Project::Object::PBXCopyFilesBuildPhase)
  phase.name == 'Embed App Extensions' || phase.dst_subfolder_spec == '13'
end

unless embed_phase
  embed_phase = project.new(Xcodeproj::Project::Object::PBXCopyFilesBuildPhase)
  embed_phase.name = 'Embed App Extensions'
  embed_phase.symbol_dst_subfolder_spec = :plug_ins
  # dst_path must be empty for the PlugIns destination — Xcode appends
  # to <wrapper>/PlugIns/ automatically. Any non-empty value here
  # double-nests the appex.
  embed_phase.dst_path = ''
  main_target.build_phases << embed_phase
end

# Link the extension product into the embed phase. The .appex
# product reference is on ext_target.product_reference once the
# target exists.
#
# IDEMPOTENCY GUARD: match on EITHER product_reference identity OR
# file_ref.path matching "<name>.appex". The path check catches the
# case where a prior partial run created an embed entry referencing
# a now-different product_reference UUID (e.g. early exit removed,
# then re-run).
existing_embed = embed_phase.files.find do |f|
  next false unless f.file_ref
  f.file_ref == ext_target.product_reference ||
    (f.file_ref.respond_to?(:path) && f.file_ref.path == "#{EXTENSION_NAME}.appex")
end
unless existing_embed
  embed_file = project.new(Xcodeproj::Project::Object::PBXBuildFile)
  embed_file.file_ref = ext_target.product_reference
  # CodeSignOnCopy is REQUIRED for App Store ingestion. Without it,
  # altool rejects with 'Invalid bundle. The bundle ... is invalid'
  # because the embedded .appex isn't re-signed under the main app's
  # provisioning profile during the copy step. RemoveHeadersOnCopy
  # is belt-and-braces for the .h files Pod targets occasionally
  # leave behind. (Reviewer #568 finding 1, MAJOR.)
  embed_file.settings = { 'ATTRIBUTES' => ['CodeSignOnCopy', 'RemoveHeadersOnCopy'] }
  embed_phase.files << embed_file
end

# ── 5. Main-app entitlements & build settings ──────────────────
# The Expo config plugin already wrote the entitlements file, but
# the build settings on the main target need the CODE_SIGN_ENTITLEMENTS
# pointer + APP_GROUPS for the App Store review process.
main_target.build_configurations.each do |bc|
  bc.build_settings['CODE_SIGN_ENTITLEMENTS'] ||= "#{MAIN_APP_NAME}/#{MAIN_APP_NAME}.entitlements"
end

# ── 6. Save ─────────────────────────────────────────────────────
project.save
puts "[✓] #{EXTENSION_NAME} target added + WireGuardKit SwiftPM dep + embedded into #{MAIN_APP_NAME}."
puts "    Next: open ios/#{MAIN_APP_NAME}.xcworkspace in Xcode to verify, or `cd ios && pod install && xcodebuild build`."
