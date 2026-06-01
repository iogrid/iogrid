// Expo config plugin — wires the PacketTunnelProvider NE target.
//
// What this plugin does at `expo prebuild`:
//
//   1. Adds a new Xcode target "PacketTunnelProvider" of type
//      `com.apple.product-type.app-extension` (subtype
//      `com.apple.networkextension.packet-tunnel`).
//   2. Copies the Swift sources from `native/ios/PacketTunnelProvider/`
//      into the generated `ios/PacketTunnelProvider/` directory and
//      adds them to the extension target's build phase.
//   3. Writes the extension's `Info.plist` declaring the NS extension
//      point (`com.apple.networkextension.packet-tunnel`).
//   4. Writes the extension's `PacketTunnelProvider.entitlements`
//      file with the NE entitlement + App Group identifier so the
//      extension shares Keychain access with the main app (this is
//      how the customer WG private key gets from the main app's
//      Keychain-stored config into the extension process).
//   5. Embeds the WireGuardKit Swift package via SwiftPM in the
//      extension target (NOT the main app — the WG code runs in the
//      NE process, not the host app).
//   6. Adds the App Group + NE entitlements to the MAIN app target
//      too (already declared in app.json, but the plugin makes sure
//      both targets agree).
//
// Why a config plugin instead of editing the generated Xcode project
// by hand: Expo prebuild blows away `ios/` on every run. Any hand
// edit would be lost. The plugin makes the modification idempotent +
// version-controlled.
//
// Refs #568. Pairs with `native/ios/PacketTunnelProvider/` Swift
// sources.

import { type ConfigPlugin, withInfoPlist, withXcodeProject } from '@expo/config-plugins';

const EXTENSION_TARGET_NAME = 'PacketTunnelProvider';
const EXTENSION_BUNDLE_ID = 'io.iogrid.app.PacketTunnelProvider';
const APP_GROUP = 'group.io.iogrid.app';

export const withNetworkExtension: ConfigPlugin = (config) => {
  // 1. Make sure the main app's Info.plist declares it embeds an
  //    extension and gives the extension's bundle ID a stable
  //    UIBackgroundModes entry. Without this, iOS won't let the main
  //    app start/stop the tunnel via NETunnelProviderManager.
  config = withInfoPlist(config, (cfg) => {
    cfg.modResults.NSExtensionAttributes = cfg.modResults.NSExtensionAttributes ?? {};
    // The host app itself doesn't need NSExtension keys — those go in
    // the EXTENSION's Info.plist, written by step 3 below via
    // withXcodeProject. We only ensure UIBackgroundModes for
    // background-fetch-style tunnel persistence here.
    const modes = (cfg.modResults.UIBackgroundModes as string[] | undefined) ?? [];
    if (!modes.includes('fetch')) modes.push('fetch');
    cfg.modResults.UIBackgroundModes = modes;
    return cfg;
  });

  // 2-6. Xcode project surgery. The actual target creation lives in
  // `addNetworkExtensionTarget` — separated so the file stays scannable.
  config = withXcodeProject(config, (cfg) => {
    addNetworkExtensionTarget(cfg.modResults, cfg.modRequest.platformProjectRoot);
    return cfg;
  });

  return config;
};

// addNetworkExtensionTarget — pure Xcode-project mutator.
//
// The xcode npm package's `addTarget` + `addBuildPhase` APIs are the
// only stable way to create a fresh Xcode target from a Node script.
// Everything else (PBXNativeTarget JSON surgery) breaks across Xcode
// versions.
function addNetworkExtensionTarget(project: any, _platformRoot: string): void {
  // Idempotency: bail out if the target already exists. `expo prebuild`
  // can run multiple times in a session; we must not double-add.
  const existingTarget = project.pbxTargetByName(EXTENSION_TARGET_NAME);
  if (existingTarget) {
    return;
  }

  // The actual target-creation code lives in the partner skill's
  // `with-network-extension-target.ts` (forthcoming with the
  // WireGuardKit + Swift source ship in #568). This stub keeps the
  // plugin file present + lint-clean so app.json's plugins array
  // resolves; the real Xcode mutation lands in the same PR as the
  // PacketTunnelProvider.swift source so the changes are atomic.
  //
  // Why land the stub first: app.json must reference the plugin path
  // OR the prebuild step in CI errors with "plugin not found". A
  // tested-zero-effect stub > a missing plugin file.
  //
  // No-op in this commit, real surgery in the next.
}

export default withNetworkExtension;
