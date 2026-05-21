# iogridd-statusbar — macOS menu-bar UI

Phase 2-mac of [EPIC #348](https://github.com/iogrid/iogrid/issues/348),
tracked under [issue #388](https://github.com/iogrid/iogrid/issues/388).
Adds a tiny SwiftPM-built `NSStatusItem` app that gives macOS providers
a user-facing "Check for updates…" trigger on top of the Sparkle
background-poll wiring shipped in PR #387.

## Why a separate Swift binary, not embedded in `iogridd` (Rust)

| Concern | Swift + AppKit (chosen) | Rust + `objc2` / `cocoa-rs` (rejected) |
|---|---|---|
| Cocoa runloop required by Sparkle 2 installer XPC | First-class | Hand-rolled `NSApp.run` via objc2; fragile |
| Crash isolation from workload daemon | Yes — separate process | No — panic in UI kills the daemon |
| macOS LaunchAgent topology | Two agents: `org.iogrid.daemon` (Background) + `io.iogrid.statusbar` (Interactive, GUI session) | One agent that has to be both Background AND Interactive, which macOS doesn't model |
| Code surface | ~250 lines Swift, no third-party deps beyond Sparkle | ~600 lines unsafe Rust + objc2 + sparkle-rs (no maintained crate) |
| Cross-compile from Linux | n/a — runs only on macOS hosts, built only in macOS CI | Same — `objc2` needs Apple frameworks |

## Layout

```
installer/macos/statusbar/
├── Package.swift                                # SwiftPM manifest
├── README.md                                    # this file
├── Sparkle.xcframework/                         # GENERATED — wrapped at build time by Makefile
└── Sources/IogriddStatusbar/
    ├── main.swift                               # NSStatusItem + AppDelegate + Sparkle controller
    └── IPC.swift                                # Unix-domain-socket client for ~/.iogrid/run/iogridd.sock
```

The compiled `iogridd-statusbar` Mach-O is dropped into
`iogridd.app/Contents/MacOS/iogridd-statusbar` by
`installer/macos/Makefile`'s `app` target and launched by the new
`io.iogrid.statusbar` LaunchAgent (template at
`installer/macos/launchagents/io.iogrid.statusbar.plist`).

## Build / install loop (local macOS dev)

```bash
# 1. Build the Rust daemon
cd daemon && cargo build --release

# 2. Build the macOS .pkg (also builds the SwiftPM target as a side-effect)
make -C installer/macos all \
    VERSION=0.1.0 ARCH=arm64 \
    DAEMON_BIN=$(pwd)/daemon/target/release/iogridd

# 3. Install the .pkg
sudo installer -pkg installer/macos/dist/iogrid-0.1.0-arm64.pkg -target /

# 4. Observe the menu-bar icon (top-right of screen). Click it → menu →
#    "Check for updates…" should trigger Sparkle's modal.
```

## Wire protocol (IPC.swift ↔ daemon)

Newline-delimited JSON over Unix-domain socket at
`~/.iogrid/run/iogridd.sock`. The Rust server lives in
`daemon/crates/core/src/ipc_mac.rs` and is only compiled on
`target_os = "macos"`.

| Direction | Frame | Meaning |
|---|---|---|
| client → daemon | `{"cmd":"quit"}\n` | Request graceful shutdown |
| daemon → client | `{"ok":true}\n` | Command accepted; daemon will stop |
| daemon → client | `{"ok":false,"error":"unknown command"}\n` | Unrecognised `cmd` field |
| daemon → client | `{"ok":false,"error":"invalid request json: <detail>"}\n` | Frame failed to parse |

Future menu items (e.g. `open-console`, `reload-config`) extend
[`IpcCommand`](../../daemon/crates/core/src/ipc_mac.rs) without
breaking the wire shape.

## CI

`installer-ci` macos-pkg shard runs `make -C installer/macos all`, which
exercises this target end-to-end. The Swift build is the slowest single
step (~30s on a clean M-series runner); SwiftPM's build cache is
preserved across CI runs via the same caching key used for the Rust
target dir.

## Refs

- [issue #388](https://github.com/iogrid/iogrid/issues/388) — this PR's parent
- [issue #348](https://github.com/iogrid/iogrid/issues/348) — auto-update EPIC
- [PR #387](https://github.com/iogrid/iogrid/pull/387) — Phase 1 (Sparkle framework + appcast)
- Sparkle docs: <https://sparkle-project.org/documentation/>
