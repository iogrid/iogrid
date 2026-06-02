// swift-tools-version:5.9
// The swift-tools-version declares the minimum version of Swift required to build this package.
//
// PATCHED FORK — see ./README.iogrid.md and ../../scripts/vendor-wireguard.sh.
//
// Upstream wireguard-apple ships swift-tools-version:5.3 which Xcode 26's
// Swift 6 toolchain rejects with "Invalid manifest" at the manifest
// compilation step (the 5.x manifest compiler is no longer bundled when
// the project's SWIFT_VERSION is 6.0). Bumping the tools version to 5.9
// — the safest baseline that the Xcode 26 toolchain still parses, and
// which keeps every PackageDescription API we use in scope — unblocks
// `xcodebuild` against the extension target.
//
// We deliberately did NOT bump to 6.0 because:
//   1. 6.0 enables strict-concurrency-by-default which lights up
//      pre-existing data-race warnings throughout WireGuardKit (it was
//      written under @unchecked Sendable assumptions). Upstream needs
//      to opt in to that, not us.
//   2. 5.9 is the floor Xcode 26 still compiles cleanly — every
//      PackageDescription symbol we touch (`Package`, `.target`,
//      `.linkedLibrary`, etc.) is identical.
//
// Re-run `mobile/ios/scripts/vendor-wireguard.sh` to refresh from upstream;
// the script preserves this swift-tools-version pin via a post-pull sed.

import PackageDescription

let package = Package(
    name: "WireGuardKit",
    platforms: [
        .macOS(.v12),
        .iOS(.v15)
    ],
    products: [
        .library(name: "WireGuardKit", targets: ["WireGuardKit"])
    ],
    dependencies: [],
    targets: [
        .target(
            name: "WireGuardKit",
            dependencies: ["WireGuardKitGo", "WireGuardKitC"]
        ),
        .target(
            name: "WireGuardKitC",
            dependencies: [],
            publicHeadersPath: "."
        ),
        .target(
            name: "WireGuardKitGo",
            dependencies: [],
            exclude: [
                "goruntime-boottime-over-monotonic.diff",
                "go.mod",
                "go.sum",
                "api-apple.go",
                "Makefile"
            ],
            publicHeadersPath: ".",
            linkerSettings: [
                .linkedLibrary("wg-go"),
                // iogrid CI bootstraps libwg-go.a into the WireGuardKitGo
                // source dir before xcodebuild runs. Add the source dir to
                // the linker search path so `-lwg-go` resolves. SwiftPM
                // doesn't add SOURCE_DIR to LIBRARY_SEARCH_PATHS automatically
                // (it's expected the lib is in CONFIGURATION_BUILD_DIR).
                // Refs CONTRIBUTING gotcha 31.
                .unsafeFlags(["-L", "Sources/WireGuardKitGo"]),
            ]
        )
    ]
)
