// swift-tools-version:5.9
//
// iogridd-statusbar — macOS status-bar (NSStatusItem) wrapper around the
// Sparkle 2 updater + a tiny Unix-domain-socket client that talks to the
// headless `iogridd` daemon over `~/.iogrid/run/iogridd.sock`.
//
// This is the user-facing trigger for `Check for updates…` (issue #388,
// Phase 2 of EPIC #348). Phase 1 shipped Sparkle background polling; this
// target adds the menu-bar item that hosts the three actions:
//
//   1. Check for updates…   — SPUStandardUpdaterController.checkForUpdates
//   2. About iogridd vX.Y.Z — NSApp.orderFrontStandardAboutPanel
//   3. Quit                 — JSON-line `{"cmd":"quit"}` over the UDS
//
// Build output: `iogridd-statusbar` Mach-O binary that the installer's
// Makefile drops into `iogridd.app/Contents/MacOS/iogridd-statusbar`. It
// shares the bundle (and therefore Sparkle.framework + the SUFeedURL /
// SUPublicEDKey Info.plist keys) with the existing Rust daemon binary —
// the .app is the Sparkle update host; the LaunchAgent for the daemon
// still launches `/usr/local/iogrid/iogridd` headlessly, and a second
// LaunchAgent (`io.iogrid.statusbar.plist`, user-session) launches THIS
// binary on user login.
//
// Why a separate SwiftPM target and not embedded in Rust via `objc2`:
//   * The Rust daemon stays a pure background service. Running NSApp on
//     its main thread would force LaunchAgent ↔ GUI-session coupling that
//     macOS LaunchAgents don't guarantee (SessionType vs ProcessType).
//   * Sparkle 2's installer XPC services need a real Cocoa runloop —
//     trivial in Swift, fragile in Rust+objc2.
//   * Crash isolation: a panic in the menu-bar app cannot take down the
//     workload-bearing daemon.
//
// Why no third-party deps:
//   * Sparkle is loaded as a binary framework (Sparkle.framework already
//     bundled by PR #387) via a `binaryTarget` here. SwiftPM is happy
//     consuming an .xcframework or a plain .framework — we use the
//     framework that's staged at `../app/Contents/Frameworks/Sparkle.framework`
//     in CI by `installer/macos/sparkle/fetch-sparkle.sh`.
//
// See installer/macos/statusbar/README.md for the build + install loop.

import PackageDescription

let package = Package(
    name: "iogridd-statusbar",
    platforms: [
        // macOS 13 Ventura is the LSMinimumSystemVersion in
        // installer/macos/app/Contents/Info.plist; keep the SwiftPM
        // floor aligned so we don't accidentally pull APIs that
        // wouldn't link on the supported install base.
        .macOS(.v13),
    ],
    products: [
        .executable(name: "iogridd-statusbar", targets: ["IogriddStatusbar"]),
    ],
    targets: [
        .executableTarget(
            name: "IogriddStatusbar",
            dependencies: [
                "Sparkle",
            ],
            path: "Sources/IogriddStatusbar",
            // Codesign-friendly: no resource bundles; the Info.plist
            // lives in the parent iogridd.app/Contents/Info.plist (which
            // is what Sparkle reads anyway — the framework looks at the
            // host bundle, not at SwiftPM's resource bundle).
            resources: [],
            // Link Sparkle by framework name; the .app's
            // @executable_path/../Frameworks/Sparkle.framework is on the
            // runtime rpath at install time (set by the LDFLAGS below).
            linkerSettings: [
                .linkedFramework("Cocoa"),
                .linkedFramework("Sparkle"),
                .unsafeFlags([
                    "-Xlinker", "-rpath",
                    "-Xlinker", "@executable_path/../Frameworks",
                ]),
            ]
        ),
        // Sparkle as a local binary framework. The path is resolved at
        // `swift build` time; CI calls `installer/macos/sparkle/fetch-sparkle.sh`
        // to drop Sparkle.framework alongside the .app before building this
        // target, then symlinks it into `installer/macos/statusbar/Sparkle.xcframework`
        // via the Makefile so SwiftPM can find it.
        //
        // We use a .xcframework wrapper (single-arch is fine — the
        // wrapper is just a directory with Info.plist + the .framework
        // inside) rather than referencing the .framework directly, because
        // SwiftPM's binaryTarget only accepts .xcframework / .artifactbundle
        // paths, not bare .framework paths.
        .binaryTarget(
            name: "Sparkle",
            path: "Sparkle.xcframework"
        ),
    ]
)
