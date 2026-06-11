// Expo config plugin — declares NE entitlements on the main app +
// copies the PacketTunnelProvider Swift sources + Info.plist into
// the prebuild output. STOPS SHORT of the Xcode target-creation
// surgery: that step lives in a Ruby xcodeproj post-prebuild
// script (mobile/ios/scripts/add-network-extension-target.rb)
// because the Ruby xcodeproj gem is the canonical, battle-tested
// API for Xcode project mutation — the Node `xcode` package's
// SwiftPM + extension-target surface is unstable + lacks the
// proper PBXTargetDependency wiring for SKIP_INSTALL=NO.
//
// Plugin scope (what runs on `expo prebuild`):
//   1. Main-app entitlements: NE + App Group + Keychain group
//   2. Main-app Info.plist: UIBackgroundModes += fetch
//   3. Copy `native/ios/PacketTunnelProvider/*` into
//      `ios/PacketTunnelProvider/` (Swift source + entitlements)
//   4. Generate the extension's Info.plist
//
// Post-prebuild scope (operator runs ONCE per fresh ios/):
//   5. `cd mobile/ios && ruby scripts/add-network-extension-target.rb`
//      adds the PacketTunnelProvider target, build phases, SwiftPM
//      WireGuardKit dep, and SKIP_INSTALL=NO override.
//
// CI scope (mobile-ios-ci.yml runs after prebuild):
//   6. The same Ruby script runs in CI right after `npx expo
//      prebuild --clean`. Idempotent — the script bails out if the
//      target already exists, so repeated runs on the same ios/
//      tree are no-ops.
//
// Refs #568. Pairs with `native/ios/PacketTunnelProvider/` Swift
// sources + `mobile/ios/scripts/add-network-extension-target.rb`.

import fs from 'fs';
import path from 'path';

import {
  type ConfigPlugin,
  withDangerousMod,
  withEntitlementsPlist,
  withInfoPlist,
} from '@expo/config-plugins';

const EXTENSION_DIR_NAME = 'PacketTunnelProvider';
const APP_GROUP = 'group.io.iogrid.app';

export const withNetworkExtension: ConfigPlugin = (config) => {
  // ── 1. Main-app Info.plist: declare host of an NE extension ────
  config = withInfoPlist(config, (cfg) => {
    const modes = (cfg.modResults.UIBackgroundModes as string[] | undefined) ?? [];
    if (!modes.includes('fetch')) modes.push('fetch');
    cfg.modResults.UIBackgroundModes = modes;
    return cfg;
  });

  // ── 2. Main-app entitlements: NE only for v1.
  // App Group + Keychain access group dropped because Apple's API
  // doesn't expose CREATE on App Groups (404 on POST /v1/appGroups,
  // Spaceship requires Apple ID+password blocked by 2FA). When the
  // founder click-creates the App Group OR WireGuardKit lands and
  // we need shared Keychain, re-add the two lines below.
  config = withEntitlementsPlist(config, (cfg) => {
    cfg.modResults['com.apple.developer.networking.networkextension'] = [
      'packet-tunnel-provider',
    ];
    // cfg.modResults['com.apple.security.application-groups'] = [APP_GROUP];
    // cfg.modResults['keychain-access-groups'] = [
    //   `$(AppIdentifierPrefix)${APP_GROUP}`,
    // ];
    return cfg;
  });

  // ── 3. Copy native sources into ios/PacketTunnelProvider/ ──────
  // withDangerousMod runs arbitrary fs against the prebuild output.
  // Idempotent — file writes overwrite cleanly.
  config = withDangerousMod(config, [
    'ios',
    async (cfg) => {
      const projectRoot = cfg.modRequest.projectRoot;
      const platformProjectRoot = cfg.modRequest.platformProjectRoot;
      const sourceDir = path.join(
        projectRoot,
        'native',
        'ios',
        EXTENSION_DIR_NAME,
      );
      const destDir = path.join(platformProjectRoot, EXTENSION_DIR_NAME);

      if (!fs.existsSync(sourceDir)) {
        // Don't fail the prebuild — just warn. Operator may be
        // running prebuild on a checkout where the native sources
        // haven't been pulled (e.g. shallow clone). The Ruby
        // script will re-verify before adding the target.
        console.warn(
          `[with-network-extension] ${sourceDir} not found — skipping copy. ` +
            `Run \`git submodule update\` or check out the full tree, then re-prebuild.`,
        );
        return cfg;
      }
      fs.mkdirSync(destDir, { recursive: true });
      for (const entry of fs.readdirSync(sourceDir)) {
        const src = path.join(sourceDir, entry);
        const dst = path.join(destDir, entry);
        fs.copyFileSync(src, dst);
      }

      // Generate the extension's Info.plist (declares
      // NSExtensionPointIdentifier — the NE-target marker that Xcode
      // + the App Store reviewer reads).
      const extensionInfoPlist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
\t<key>CFBundleDevelopmentRegion</key>
\t<string>$(DEVELOPMENT_LANGUAGE)</string>
\t<key>CFBundleDisplayName</key>
\t<string>iogrid VPN</string>
\t<key>CFBundleExecutable</key>
\t<string>$(EXECUTABLE_NAME)</string>
\t<key>CFBundleIdentifier</key>
\t<string>$(PRODUCT_BUNDLE_IDENTIFIER)</string>
\t<key>CFBundleInfoDictionaryVersion</key>
\t<string>6.0</string>
\t<key>CFBundleName</key>
\t<string>$(PRODUCT_NAME)</string>
\t<key>CFBundlePackageType</key>
\t<string>$(PRODUCT_BUNDLE_PACKAGE_TYPE)</string>
\t<key>CFBundleShortVersionString</key>
\t<string>${config.version ?? '1.0.0'}</string>
\t<key>CFBundleVersion</key>
\t<string>1</string>
\t<key>NSExtension</key>
\t<dict>
\t\t<key>NSExtensionPointIdentifier</key>
\t\t<string>com.apple.networkextension.packet-tunnel</string>
\t\t<key>NSExtensionPrincipalClass</key>
\t\t<string>$(PRODUCT_MODULE_NAME).PacketTunnelProvider</string>
\t</dict>
</dict>
</plist>
`;
      fs.writeFileSync(path.join(destDir, 'Info.plist'), extensionInfoPlist);
      return cfg;
    },
  ]);

  return config;
};

export default withNetworkExtension;
