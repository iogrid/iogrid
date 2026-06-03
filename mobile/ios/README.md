# iogrid iOS — consume-only VPN client

> EPIC #566 / #581. Expo SDK 56 React Native app with a NetworkExtension
> PacketTunnelProvider for WireGuard-over-iOS VPN. Mobile is **consume-only**
> (VPN) per App Store policy — providers run the desktop daemon, not the
> phone. Seamless roaming on WiFi ↔ cellular network changes.

## Status

- On **TestFlight** (external beta group `vpn-beta`, public invite
  link below). Build number auto-increments per CI run via EAS
  (`autoIncrement: buildNumber`) — current TestFlight build is 137+.
- **Sign in with Apple** is the sole auth path on iOS (no email/password,
  no Google on iOS) — wired end-to-end against identity-svc, not a stub.
- **Universal Links** active: `applinks:iogrid.org` declared in
  `app.json` associatedDomains; AASA served from the web app.
- **Ping payment** integration via Universal Links
  `https://ping.cash/approve` + SPL Approve (delegate). See
  [`src/lib/wallets/ping-pay.ts`](src/lib/wallets/ping-pay.ts). The
  return bounce uses the registered `iogrid://` scheme.

### Account model

Apple ID + Ping wallet IS the identity (the earlier Mullvad-style
"anonymous account number" model was rejected — see
[`docs/ux-wireframes-v2.md`](docs/ux-wireframes-v2.md)):

- **Auth**: Sign in with Apple (mandatory on iOS). Google arrives later
  on Android.
- **Identity**: Apple ID → maps to an iogrid account on first launch.
  Apple's identity token is POSTed to identity-svc, validated against
  Apple's JWKS, exchanged for an iogrid session JWT + refresh token,
  persisted in iOS Keychain (App Group scoped so the NetworkExtension
  can read it).
- **Wallet / payment**: $GRID balance held in a Ping wallet; VPN top-ups
  burn $GRID via Ping's SPL-Approve surface.

### $GRID token

- SPL Token-2022, **9 decimals** (NEVER 6). 250 $GRID → 250_000_000_000
  atomic units.
- Mainnet mint NOT deployed. Devnet mint:
  `BaQvWwb1wUGvWJXPEUbLEwPeeYMd4sKvp2S7obzTWorR` (in `app.json`
  `extra.iogridTokenMintDevnet`). `extra.iogridTokenMint` (mainnet) is
  empty until Track 5 ships.

### Apple Developer Portal

| Resource | Value |
|---|---|
| App Store Connect App ID | `6775617937` |
| Main bundle ID | `io.iogrid.app` (resource `CZVDX99A2L`) — capabilities: NETWORK_EXTENSIONS, APP_GROUPS, PERSONAL_VPN |
| Extension bundle ID | `io.iogrid.app.PacketTunnelProvider` (resource `D48F7P2J6L`) — capabilities: NETWORK_EXTENSIONS, APP_GROUPS |
| App Group | `group.io.iogrid.app` |
| External beta group | `vpn-beta` |
| Public TestFlight invite | https://testflight.apple.com/join/jHPTNj9P |

CI signs with a fresh fastlane-cert Distribution cert (regenerated each
run) and a `iogrid App Store` provisioning profile regenerated via
fastlane sigh. The four App Store Connect secrets
(`APP_STORE_CONNECT_KEY_ID`, `APP_STORE_CONNECT_ISSUER_ID`,
`APP_STORE_CONNECT_PRIVATE_KEY`, `APPLE_TEAM_ID`) plus the signing
secrets are set in the iogrid repo. When present, the credential-gated
CI steps fire automatically (`if: env.HAS_APPLE_SECRETS == 'true'`).

The complete bootstrap runbook is at
[`docs/runbooks/mobile-ios-testflight-bootstrap.md`](../../docs/runbooks/mobile-ios-testflight-bootstrap.md).

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Main app (Expo SDK 56 RN, expo-router)                      │
│  ├─ src/app/(onboarding)/ — welcome, privacy,                │
│  │     sign-in-with-apple, connect-wallet                    │
│  ├─ src/app/index.tsx — VPN toggle screen                    │
│  ├─ src/app/regions.tsx — region picker                      │
│  ├─ src/app/topup.tsx — $GRID top-up (Ping Approve launch)   │
│  ├─ src/app/settings.tsx — account + session                │
│  └─ src/lib/                                                 │
│     ├─ auth.ts — Sign in with Apple + session persistence    │
│     ├─ coordinator.ts — JSON-over-fetch RPC to vpn-svc        │
│     ├─ grid_balance.ts — Solana RPC $GRID balance            │
│     └─ wallets/ — Ping bind + ping-pay SPL Approve           │
└──────────┬──────────────────────────────────────────────────┘
           │ NETunnelProviderManager IPC
┌──────────▼──────────────────────────────────────────────────┐
│  TunnelControl Expo native module (modules/TunnelControl/)  │
│  ├─ ios/TunnelControl.swift — start/stop/sendMessage        │
│  └─ src/index.ts — TS wrapper, EventEmitter for status       │
└──────────┬──────────────────────────────────────────────────┘
           │ NETunnelProviderProtocol.providerConfiguration
┌──────────▼──────────────────────────────────────────────────┐
│  PacketTunnelProvider extension (native/ios/PacketTunnelProvider/) │
│  ├─ PacketTunnelProvider.swift — NEPacketTunnelProvider      │
│  ├─ NWPathMonitor — seamless roaming                         │
│  └─ handleAppMessage — IPC from main app                     │
└──────────┬──────────────────────────────────────────────────┘
           │ WireGuard outer UDP, AllowedIPs=0.0.0.0/0
┌──────────▼──────────────────────────────────────────────────┐
│  Residential provider (iogridd, Rust daemon)                 │
│  ├─ TunForwardSink — decap → kernel TUN → MASQUERADE → WAN   │
│  └─ Customer's traffic exits via the provider's home IP      │
└──────────────────────────────────────────────────────────────┘
```

## Build pipeline

Expo prebuild generates the bare iOS project from `app.json` + the
config plugins. The NetworkExtension target is then added via a
post-prebuild Ruby script — the Node `xcode` package's
extension-target API is incomplete; `xcodeproj` (Ruby) is the
canonical surface (CocoaPods uses it).

```
expo prebuild                                # generates ios/
plugins/with-network-extension.ts            # copies Swift + entitlements
scripts/add-network-extension-target.rb      # adds Xcode target + embed phase
pod install                                  # CocoaPods integration
xcodebuild archive                           # full build
fastlane sigh + altool                       # provision + upload to TestFlight
```

CI orchestrates this in `.github/workflows/mobile-ios-ci.yml`. See
[`CONTRIBUTING.md`](CONTRIBUTING.md) for the full set of Xcode 26 /
Swift 6 / Expo SDK 56 build gotchas this codebase has hit.

## Maestro smoke gate

`.maestro/` holds numbered flows + a `00-all.yaml` orchestrator. CI
runs the orchestrator on a booted iOS simulator before any TestFlight
upload — a Maestro fail kills the upload. Use `extendedWaitUntil` for
waits (`assertVisible` has no `timeout` key — see CONTRIBUTING gotcha
19) and prefer testID assertions over text on Pressable-wrapped content
(gotcha 21/21b).

## UAT walk (Playwright)

Run `expo start --web --port 8081` then drive via Playwright. The
TunnelControl native module has a web stub (`src/index.web.ts`) that
no-ops the iOS-only NEVPNStatusDidChange flow so the JS layer loads
cleanly in browser.
