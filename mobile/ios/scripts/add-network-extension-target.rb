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

# WireGuardKit SwiftPM dep — VENDORED FORK (#586).
#
# We vendor a patched copy of wireguard-apple at
# `mobile/ios/vendor/wireguard-apple-swift6/` because upstream's
# Package.swift declares `swift-tools-version:5.3` which Xcode 26's
# Swift 6 toolchain refuses to compile ("Invalid manifest"). See
# `mobile/ios/scripts/vendor-wireguard.sh` for the re-vendoring
# procedure and `vendor/wireguard-apple-swift6/README.iogrid.md` for
# rationale.
#
# Wire-in shape: a local-path XCLocalSwiftPackageReference (Xcode
# supports both XCRemoteSwiftPackageReference and the local-path
# variant via the same xcodeproj API surface) pointing at the vendor
# dir, plus the WireGuardKit product as a XCSwiftPackageProductDependency
# on the extension target. Idempotent — re-running this script after
# the package ref exists is a no-op.
#
# 2026-06-03 — Re-ENABLED (Closes #610). Root cause of CI 26833832718's
# undefined-symbol failure was NOT the symbols the linker named first
# (_threadentry, _x_cgo_init exist in `gcc_darwin_arm64.c` and DO
# compile under any darwin/arm64 build) — the actual missing symbols
# (further down in the ld output that the disable-commit's message
# elided) were:
#
#   "_darwin_arm_init_mach_exception_handler", referenced from:
#       _x_cgo_init in libwg-go.a[arm64][9](000006.o)
#   "_darwin_arm_init_thread_exception_port", referenced from:
#       _threadentry in libwg-go.a[arm64][9](000006.o)
#       _x_cgo_init in libwg-go.a[arm64][9](000006.o)
#
# Those iOS-only shims live in `gcc_signal_ios_nolldb.c` which is
# build-tagged `//go:build !lldb && ios && arm64`. The vendored
# upstream Makefile mapped `iphoneos → ios` but had NO mapping for
# `iphonesimulator`, so the simulator slice was built with GOOS
# defaulting to host = darwin → `gcc_signal_ios_nolldb.c` excluded →
# undefined references at xcodebuild link time.
#
# Fix lives in
# `mobile/ios/vendor/wireguard-apple-swift6/Sources/WireGuardKitGo/Makefile`
# (added `GOOS_iphonesimulator := ios` line). Verified via
# `go list -json runtime/cgo` that GOOS=ios pulls in the missing
# shim file; xcodebuild link is now expected to resolve cleanly.
#
# Ref: golang/go#47228 (same root cause for Mac Catalyst).
WIREGUARDKIT_ENABLED          = true
WIREGUARDKIT_VENDOR_PATH      = '../vendor/wireguard-apple-swift6' # relative to ios/iogrid.xcodeproj
WIREGUARDKIT_PRODUCT          = 'WireGuardKit'

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
    # Plain '1,2' (NO embedded quotes — those caused Xcode 26 to parse
    # the string as two items '"1' and '2"' and warn "value does not
    # contain any device family values compatible with the iOS
    # platform" on the PacketTunnelProvider target).
    'TARGETED_DEVICE_FAMILY'       => '1,2',
    # NE extension MUST be SKIP_INSTALL=YES — Xcode embeds it into the
    # main app's PlugIns folder via the Embed App Extensions phase, not
    # as a separate installable. SKIP_INSTALL=NO double-installs and
    # triggers "Multiple commands produce '/Applications/.appex'" on
    # xcodebuild — exactly the failure mode CI surfaced on commit 77b442a.
    'SKIP_INSTALL'                 => 'YES',
    'LD_RUNPATH_SEARCH_PATHS'      => '"$(inherited) @executable_path/Frameworks @executable_path/../../Frameworks"',
    'CODE_SIGN_STYLE'              => 'Manual',  # CI uses fastlane-fetched profile
    # Per-target provisioning profile — the extension has its own bundle ID
    # (io.iogrid.app.PacketTunnelProvider) so it needs its own profile.
    # Using a global xcodebuild PROVISIONING_PROFILE_SPECIFIER flag would
    # incorrectly apply the main app's profile to the extension; setting
    # it here in the project file lets xcodebuild pick the right one
    # per target without a command-line override.
    'PROVISIONING_PROFILE_SPECIFIER' => 'iogrid PacketTunnelProvider App Store',
    'CODE_SIGN_IDENTITY'             => 'Apple Distribution',
    # DEVELOPMENT_TEAM must be set per-target for manual signing to work
    # without the global xcodebuild flag (Xcode 26 ignores team inheritance
    # from the project-level settings for new app-extension targets).
    'DEVELOPMENT_TEAM'               => ENV.fetch('APPLE_TEAM_ID', ''),
  )
end

# ── 2. Add Swift source files to the target ────────────────────
#
# Track 3 (#587) split the implementation across three files:
#   - PacketTunnelProvider.swift (lifecycle)
#   - WGTunnel.swift (providerConfig → WireGuardKit.TunnelConfiguration)
#   - Stats.swift (codable + App Group UserDefaults bridge)
#
# All three MUST be added to the extension target, else the build
# fails with "Cannot find 'WGTunnel' / 'StatsParser' in scope" once
# WireGuardKitC.h compiles cleanly. We additionally only add files
# that actually exist on disk so the script stays robust against
# future single-file refactors.
EXTENSION_SOURCES = [
  'PacketTunnelProvider.swift',
  'WGTunnel.swift',
  'Stats.swift',
].freeze

ext_group = project.main_group.new_group(EXTENSION_NAME, EXTENSION_NAME)
ext_dir = File.join(File.dirname(PROJECT_PATH), EXTENSION_NAME)
EXTENSION_SOURCES.each do |fname|
  abs = File.join(ext_dir, fname)
  unless File.exist?(abs)
    puts "[!] #{fname} not found at #{abs} — skipping (likely the Expo plugin only copied PacketTunnelProvider.swift; vendor-wireguard.sh or the prebuild step needs updating)"
    next
  end
  puts "[+] Adding #{fname} to the target..."
  swift_ref = ext_group.new_file(fname)
  ext_target.add_file_references([swift_ref])
end

# ── 3. WireGuardKit SwiftPM dep (vendored local-path fork) ─────
if WIREGUARDKIT_ENABLED
  puts "[+] Adding WireGuardKit local SwiftPM dep from #{WIREGUARDKIT_VENDOR_PATH}..."

  # Idempotency: only add the package reference if one with the same
  # path isn't already present. xcodeproj's `package_references`
  # array holds both XCRemote and XCLocal variants; we match on
  # `path` (XCLocalSwiftPackageReference) or `relative_path` (older
  # gem versions sometimes use a different attribute name).
  existing_ref = project.root_object.package_references.find do |ref|
    ref_attrs = ref.respond_to?(:to_hash) ? ref.to_hash : {}
    matches_path = ref_attrs['relativePath'] == WIREGUARDKIT_VENDOR_PATH ||
                   (ref.respond_to?(:relative_path) && ref.relative_path == WIREGUARDKIT_VENDOR_PATH) ||
                   (ref.respond_to?(:path) && ref.path == WIREGUARDKIT_VENDOR_PATH)
    matches_path
  end

  if existing_ref
    puts "[+] WireGuardKit local package reference already present — no-op"
    pkg_ref = existing_ref
  else
    # XCLocalSwiftPackageReference is the canonical type for path-based
    # SwiftPM deps in modern xcodeproj gem versions. Older gems (<1.22)
    # exposed only XCRemoteSwiftPackageReference and required the
    # local-path shape via a `requirement: { kind: 'kind-1' }` hack —
    # we fall back to that if the class isn't defined.
    if defined?(Xcodeproj::Project::Object::XCLocalSwiftPackageReference)
      pkg_ref = project.new(Xcodeproj::Project::Object::XCLocalSwiftPackageReference)
      pkg_ref.relative_path = WIREGUARDKIT_VENDOR_PATH
    else
      pkg_ref = project.new(Xcodeproj::Project::Object::XCRemoteSwiftPackageReference)
      # Local-path indicator on older gems — repositoryURL set to a
      # file:// path that resolves relative to the .xcodeproj.
      pkg_ref.repositoryURL = WIREGUARDKIT_VENDOR_PATH
      pkg_ref.requirement = { 'kind' => 'kind-1' } # local-path marker
    end
    project.root_object.package_references << pkg_ref
  end

  # Add the WireGuardKit product as a dependency on the extension target.
  # Idempotent: skip if an entry for this product already exists on the
  # target.
  already_linked = ext_target.package_product_dependencies.any? do |dep|
    dep.respond_to?(:product_name) && dep.product_name == WIREGUARDKIT_PRODUCT
  end

  unless already_linked
    product_dep = project.new(Xcodeproj::Project::Object::XCSwiftPackageProductDependency)
    product_dep.product_name = WIREGUARDKIT_PRODUCT
    product_dep.package = pkg_ref
    ext_target.package_product_dependencies << product_dep

    # Link the product into the extension target's Frameworks build
    # phase so the linker sees -lWireGuardKit at archive time.
    frameworks_phase = ext_target.frameworks_build_phase
    build_file = project.new(Xcodeproj::Project::Object::PBXBuildFile)
    build_file.product_ref = product_dep
    frameworks_phase.files << build_file
  end

  # Embed wireguard-go.framework into the extension's Embed Frameworks
  # build phase. WireGuardAdapter dlopens wireguard-go at runtime, and
  # the NE sandbox refuses to load anything not present in the .appex's
  # Frameworks dir — SwiftPM resolves the static lib but it's the
  # framework wrapper that must be embedded. The framework itself is
  # produced by SwiftPM's resource processing during the WireGuardKitGo
  # target build; we just need an Embed Frameworks phase on the
  # extension that copies it.
  embed_frameworks = ext_target.build_phases.find do |phase|
    phase.is_a?(Xcodeproj::Project::Object::PBXCopyFilesBuildPhase) &&
      phase.dst_subfolder_spec == '10' # 10 = Frameworks
  end
  unless embed_frameworks
    embed_frameworks = project.new(Xcodeproj::Project::Object::PBXCopyFilesBuildPhase)
    embed_frameworks.name = 'Embed Frameworks'
    embed_frameworks.symbol_dst_subfolder_spec = :frameworks
    embed_frameworks.dst_path = ''
    ext_target.build_phases << embed_frameworks
  end
  puts "[+] Embed Frameworks phase present on extension target (Frameworks subfolder spec)."

  puts "[+] WireGuardKit SwiftPM dep wired into #{EXTENSION_NAME} target."
else
  puts "[+] WireGuardKit wiring skipped (flag disabled)."
end

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
  # Set per-target signing settings here so the archive step can drop
  # its global xcodebuild PROVISIONING_PROFILE_SPECIFIER flag. The
  # global flag would incorrectly apply the main app's profile to
  # the extension target; setting per-target in the project file is
  # the only way Xcode lets each target pick its correct profile.
  bc.build_settings['CODE_SIGN_STYLE'] = 'Manual'
  bc.build_settings['CODE_SIGN_IDENTITY'] = 'Apple Distribution'
  bc.build_settings['PROVISIONING_PROFILE_SPECIFIER'] = 'iogrid App Store'
  bc.build_settings['DEVELOPMENT_TEAM'] = ENV.fetch('APPLE_TEAM_ID', '')
end

# ── 6. Save ─────────────────────────────────────────────────────
project.save
puts "[✓] #{EXTENSION_NAME} target added + WireGuardKit SwiftPM dep + embedded into #{MAIN_APP_NAME}."
puts "    Next: open ios/#{MAIN_APP_NAME}.xcworkspace in Xcode to verify, or `cd ios && pod install && xcodebuild build`."
