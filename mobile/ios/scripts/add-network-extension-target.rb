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

# WireGuardKit SwiftPM dep. Tracks the `master` branch of
# wireguard-apple — zx2c4 uses old-school branch naming (master not
# main) AND ships unversioned, so neither a release tag nor `main`
# resolves. Confirmed against git.zx2c4.com/wireguard-apple on the
# CI's xcodebuild package-resolve step.
WIREGUARD_REPO   = 'https://git.zx2c4.com/wireguard-apple'
WIREGUARD_BRANCH = 'master'

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

# Bundle identifier + entitlements + Info.plist + Swift version on
# both Debug + Release configurations.
ext_target.build_configurations.each do |bc|
  bc.build_settings.merge!(
    'PRODUCT_BUNDLE_IDENTIFIER'    => EXTENSION_BUNDLE_ID,
    'INFOPLIST_FILE'               => "#{EXTENSION_NAME}/Info.plist",
    'CODE_SIGN_ENTITLEMENTS'       => "#{EXTENSION_NAME}/PacketTunnelProvider.entitlements",
    'SWIFT_VERSION'                => SWIFT_VERSION,
    'IPHONEOS_DEPLOYMENT_TARGET'   => DEPLOYMENT_TARGET,
    'TARGETED_DEVICE_FAMILY'       => '"1,2"',
    'SKIP_INSTALL'                 => 'NO',
    'LD_RUNPATH_SEARCH_PATHS'      => '"$(inherited) @executable_path/Frameworks @executable_path/../../Frameworks"',
    'CODE_SIGN_STYLE'              => 'Manual',  # CI uses fastlane-fetched profile
  )
end

# ── 2. Add Swift source file to the target ─────────────────────
puts "[+] Adding PacketTunnelProvider.swift to the target..."
ext_group = project.main_group.new_group(EXTENSION_NAME, EXTENSION_NAME)
swift_ref = ext_group.new_file('PacketTunnelProvider.swift')
ext_target.add_file_references([swift_ref])

# ── 3. WireGuardKit SwiftPM dep ─────────────────────────────────
puts "[+] Adding WireGuardKit SwiftPM dependency..."
package_ref = project.root_object.package_references.find do |pr|
  pr.respond_to?(:repositoryURL) && pr.repositoryURL == WIREGUARD_REPO
end

unless package_ref
  package_ref = project.new(Xcodeproj::Project::Object::XCRemoteSwiftPackageReference)
  package_ref.repositoryURL = WIREGUARD_REPO
  package_ref.requirement = {
    'kind'   => 'branch',
    'branch' => WIREGUARD_BRANCH,
  }
  project.root_object.package_references << package_ref
end

# Add WireGuardKit product → extension target.
product_dep = project.new(Xcodeproj::Project::Object::XCSwiftPackageProductDependency)
product_dep.product_name = 'WireGuardKit'
product_dep.package = package_ref

ext_target.package_product_dependencies ||= []
unless ext_target.package_product_dependencies.any? { |d| d.product_name == 'WireGuardKit' }
  ext_target.package_product_dependencies << product_dep
end

# Also add to the Frameworks build phase so Xcode actually links it.
frameworks_phase = ext_target.frameworks_build_phase
build_file = project.new(Xcodeproj::Project::Object::PBXBuildFile)
build_file.product_ref = product_dep
frameworks_phase.files << build_file

# ── 4. Embed extension in the main app ─────────────────────────
puts "[+] Embedding #{EXTENSION_NAME} into #{MAIN_APP_NAME}..."
main_target = project.native_targets.find { |t| t.name == MAIN_APP_NAME }
abort "Main target #{MAIN_APP_NAME} not found — is the Expo prebuild output correct?" unless main_target

# Target dependency so building the main app forces the extension build.
main_target.add_dependency(ext_target)

# Embed App Extensions build phase.
embed_phase = main_target.build_phases.find do |phase|
  phase.respond_to?(:dst_subfolder_spec) && phase.dst_subfolder_spec == '13'
end

unless embed_phase
  embed_phase = project.new(Xcodeproj::Project::Object::PBXCopyFilesBuildPhase)
  embed_phase.name = 'Embed App Extensions'
  embed_phase.symbol_dst_subfolder_spec = :plug_ins
  main_target.build_phases << embed_phase
end

# Link the extension product into the embed phase. The .appex
# product reference is on ext_target.product_reference once the
# target exists.
embed_file = project.new(Xcodeproj::Project::Object::PBXBuildFile)
embed_file.file_ref = ext_target.product_reference
embed_file.settings = { 'ATTRIBUTES' => ['RemoveHeadersOnCopy'] }
embed_phase.files << embed_file

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
