# wireguard-apple-swift6 (vendored fork)

Vendored Swift-6-compatible fork of [wireguard-apple](https://git.zx2c4.com/wireguard-apple).

Upstream's `Package.swift` declares `swift-tools-version:5.3` which Xcode 26's
Swift 6 toolchain refuses to compile (the 5.x manifest compiler is no longer
bundled — surface: `Invalid manifest` at the manifest compilation step). zx2c4
has not shipped Swift-6 compat upstream as of 2026-06-02, so we vendor + patch.

## What changed vs upstream

1. `Package.swift` swift-tools pin: `5.3` → `5.9` (see header comment in the
   file for why 5.9 and not 6.0).
2. Removed the WireGuardApp client + WireGuard.xcodeproj + Shared +
   WireGuardNetworkExtension targets — we only consume the `WireGuardKit`
   product (plus its WireGuardKitGo / WireGuardKitC bridges) from the
   `mobile/ios/native/ios/PacketTunnelProvider/` extension target.
3. Dropped `.git/`, `.github/`, `sync-translations.sh`, `MOBILECONFIG.md`.

The Sources tree under `Sources/WireGuardKit*` is upstream master (commit
hash captured by the vendor script — see commit history of this directory).

## How to refresh from upstream

```bash
mobile/ios/scripts/vendor-wireguard.sh            # idempotent re-pull
mobile/ios/scripts/vendor-wireguard.sh --force    # full nuke + re-clone
```

The script:
- Shallow-clones `https://git.zx2c4.com/wireguard-apple` (`master` branch
  — zx2c4 still uses `master`, not `main`).
- Re-trims the non-WireGuardKit pieces.
- Re-applies the `swift-tools-version:5.9` pin via awk so the patch is
  deterministic regardless of upstream's exact starting text.
- Warns (non-fatal) if upstream re-introduces `@_implementationOnly`
  imports — that's a Swift-6 deprecation we'll need to patch separately
  when it lands.

## How it's wired into the Xcode project

`mobile/ios/scripts/add-network-extension-target.rb` adds an
`XCRemoteSwiftPackageReference` (despite the misleading name — Xcode
supports local-path SwiftPM deps via the same record type) with
`requirement: { kind: 'kind-1' }` and a relative path pointing at this
directory. Re-running the script is idempotent — it bails if the
package ref already exists.

The extension target `embeds` `wireguard-go.framework` via the Embed
Frameworks build phase (the WireGuardKitGo target ships a static lib
that the C glue translates into the framework Apple's PacketTunnel
sandbox can dlopen).

## License

MIT, preserved as `COPYING` (the same file upstream ships). All credit
to Jason A. Donenfeld and the WireGuard contributors — we contribute
nothing beyond the swift-tools pin.

## Refs

- iogrid issue #586 — vendor fork ticket
- iogrid issue #576 — original "park until upstream fixes Swift 6"
  ticket that this vendor unblocks
- gotcha #4 in `mobile/ios/CONTRIBUTING.md` — the original
  Xcode-26-rejects-upstream symptom
- gotcha #25 in `mobile/ios/CONTRIBUTING.md` — re-vendor procedure
