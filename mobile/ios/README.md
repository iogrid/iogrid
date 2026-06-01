# iogrid iOS — VPN mobile client v1

> EPIC #566. Expo SDK 56 React Native app with a NetworkExtension
> PacketTunnelProvider for WireGuard-over-iOS VPN. Mullvad-style
> anonymous account ID (no email, no password). Seamless roaming on
> WiFi ↔ cellular network changes.

## Status (2026-06-02)

- 7 of 8 EPIC children code-complete (#567 #568 #569 #570 #571 #572 #573)
- #574 (App Store + TestFlight beta crew) parked on operator-action #575
- #576 (WireGuardKit SwiftPM dep) parked on upstream Swift 6 compat
- #577 (post-review polish) parked

## Quickstart for operators

The complete TestFlight bootstrap runbook is at
[`docs/runbooks/mobile-ios-testflight-bootstrap.md`](../../docs/runbooks/mobile-ios-testflight-bootstrap.md).
6 steps, ~40 min real-time from Apple Developer enrollment to
emrahbaysal@gmail.com receiving the TestFlight invite.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  Main app (Expo SDK 56 RN, expo-router stack navigation)    │
│  ├─ src/app/index.tsx — toggle screen                       │
│  ├─ src/app/regions.tsx — region picker (#571)              │
│  ├─ src/app/settings.tsx — account number recovery (#569)   │
│  ├─ src/components/quota-banner.tsx — server-driven (#573)  │
│  └─ src/lib/                                                │
│     ├─ account.ts — Mullvad-style anon ID via Keychain      │
│     └─ coordinator.ts — JSON-over-fetch RPC to vpn-svc      │
└──────────┬──────────────────────────────────────────────────┘
           │ NETunnelProviderManager IPC
┌──────────▼──────────────────────────────────────────────────┐
│  TunnelControl Expo native module (modules/TunnelControl/)  │
│  ├─ ios/TunnelControl.swift — start/stop/sendMessage        │
│  └─ src/index.ts — TS wrapper, EventEmitter for status      │
└──────────┬──────────────────────────────────────────────────┘
           │ NETunnelProviderProtocol.providerConfiguration
┌──────────▼──────────────────────────────────────────────────┐
│  PacketTunnelProvider extension (native/ios/PTP/)            │
│  ├─ PacketTunnelProvider.swift — NEPacketTunnelProvider      │
│  │  ├─ startTunnel — decodes config, starts WireGuardAdapter │
│  │  ├─ NWPathMonitor — #572 seamless roaming                 │
│  │  └─ handleAppMessage — IPC from main app                  │
│  └─ PacketTunnelProvider.entitlements — NE + App Group       │
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

CI orchestrates this in `.github/workflows/mobile-ios-ci.yml`. The
credential-requiring steps are gated by
`if: ${{ env.HAS_APPLE_SECRETS == 'true' }}` — set the 4 Apple
secrets per the runbook and they auto-fire.

## Known gaps v1

| # | Gap | Status |
|---|---|---|
| #574 | First TestFlight upload + tester invite | gated on #575 operator action |
| #575 | Apple Developer secrets (.p8 + key id + issuer + team) | needs operator |
| #576 | WireGuardKit SwiftPM dep — wireguard-apple's Package.swift fails Swift 6 compile | parked, upstream blocker |
| #577 | Post-review polish (URL hardcode, IPC JSON escape, observer leak, etc.) | parked |

## Maestro smoke gate

`.maestro/` holds the 5 numbered flows + a `00-all.yaml` orchestrator
(per vcard convention). CI runs the orchestrator on a booted iOS
simulator before any TestFlight upload — a Maestro fail kills the
upload.

| Flow | Asserts |
|---|---|
| 01-launch | First-launch contract: no login screen, toggle visible, account number present |
| 02-toggle-on | Toggle ON → CONNECTING state transition |
| 03-region-picker | Picker reachable, 'Best (auto)' default selected, search field works |
| 04-settings-account-id | Settings reachable, 'Account number' row present |
| 05-quota-banner | Banner NOT visible on first launch (server hasn't responded yet) |

## UAT walk (Playwright)

Run `expo start --web --port 8081` then drive via Playwright. The
TunnelControl native module has a web stub (`src/index.web.ts`) that
no-ops the iOS-only NEVPNStatusDidChange flow so the JS layer loads
cleanly in browser. See `docs/evidence/566-mobile-uat-walk-2026-06-02/`
for the 4 walk screenshots that flipped #568 to status/completed.
