// iogridd-statusbar — macOS menu-bar UI for the iogridd daemon.
//
// Issue #388 / Phase 2 of EPIC #348. Phase 1 (PR #387) shipped
// Sparkle 2.x background polling; this binary adds the user-facing
// "Check for updates…" trigger plus a graceful "Quit" path.
//
// Lifecycle:
//   1. LaunchAgent `io.iogrid.statusbar.plist` (user/GUI session)
//      execs us on login.
//   2. `NSApplication.shared.run()` enters the Cocoa runloop. We carry
//      `LSUIElement=true` in the host Info.plist (see
//      `installer/macos/app/Contents/Info.plist`), so no Dock icon
//      and no menu bar appears — just our `NSStatusItem`.
//   3. Sparkle's `SPUStandardUpdaterController` is constructed eagerly
//      so the framework's background-check scheduler starts. The same
//      controller's `checkForUpdates(_:)` is wired to the menu item.
//   4. Quit posts a JSON-line `{"cmd":"quit"}` to `~/.iogrid/run/iogridd.sock`
//      (best-effort; see IPC.swift), then terminates ourselves so launchd
//      doesn't keep relaunching the menu-bar UI for a daemon the user
//      explicitly stopped.
//
// SAFETY: All NSStatusItem / NSMenu mutations happen on the main thread
// (Cocoa requirement). The IPC client uses Darwin `socket(2)` syscalls
// from a background DispatchQueue to keep the UI responsive even if the
// daemon's UDS listener is wedged.

import Cocoa
import Sparkle

// Resolve the displayed version from CFBundleShortVersionString. Falls
// back to "dev" if we're running outside an .app bundle (e.g. raw
// `swift run` from the package dir during development).
private func bundleVersionString() -> String {
    let info = Bundle.main.infoDictionary ?? [:]
    if let v = info["CFBundleShortVersionString"] as? String, !v.isEmpty {
        return v
    }
    return "dev"
}

// AppDelegate owns the NSStatusItem + the Sparkle controller. We use an
// explicit delegate (rather than top-level statements) so the Sparkle
// controller lives for the lifetime of the application — otherwise its
// background-check timers would be deallocated immediately.
final class IogriddStatusbarAppDelegate: NSObject, NSApplicationDelegate {
    // Sparkle 2's SPUStandardUpdaterController is the recommended entry
    // point when you don't need fine-grained delegate customisation. It
    // owns an SPUUpdater + an SPUStandardUserDriver and handles the
    // full update prompt UX out of the box, reading SUFeedURL +
    // SUPublicEDKey from the bundle's Info.plist.
    private let updaterController: SPUStandardUpdaterController

    private var statusItem: NSStatusItem!

    override init() {
        // `startingUpdater: true` schedules the first background check
        // immediately (subject to SUScheduledCheckInterval). This matches
        // the headless Phase 1 behaviour — Phase 2's only additive change
        // is the menu-driven manual trigger.
        self.updaterController = SPUStandardUpdaterController(
            startingUpdater: true,
            updaterDelegate: nil,
            userDriverDelegate: nil
        )
        super.init()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        // Status item with variable length so the icon's intrinsic width
        // is respected (vs `.squareLength`, which pins to 22pt).
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)

        if let button = statusItem.button {
            // Template image: SF Symbol "bolt.horizontal.circle" renders
            // as a hollow lightning bolt — a reasonable visual proxy for
            // "compute mesh provider running". Falls back to a unicode
            // glyph if the symbol isn't available on the host OS.
            if let image = NSImage(systemSymbolName: "bolt.horizontal.circle",
                                   accessibilityDescription: "iogrid")
            {
                image.isTemplate = true
                button.image = image
            } else {
                button.title = "io"
            }
            button.toolTip = "iogrid daemon — click for menu"
        }

        statusItem.menu = buildMenu()
    }

    /// Build the menu. Three actionable items + a disabled header that
    /// shows the version so support can read it back without copy/paste.
    private func buildMenu() -> NSMenu {
        let menu = NSMenu()

        // Disabled header: "iogrid is running" + version.
        let header = NSMenuItem(
            title: "iogrid is running",
            action: nil,
            keyEquivalent: ""
        )
        header.isEnabled = false
        menu.addItem(header)
        menu.addItem(NSMenuItem.separator())

        // Check for updates… — drives Sparkle's manual-check flow.
        let checkItem = NSMenuItem(
            title: "Check for updates…",
            action: #selector(checkForUpdates(_:)),
            keyEquivalent: ""
        )
        checkItem.target = self
        menu.addItem(checkItem)

        // About iogridd vX.Y.Z — Cocoa's standard about panel.
        let aboutItem = NSMenuItem(
            title: "About iogridd \(bundleVersionString())",
            action: #selector(showAboutPanel(_:)),
            keyEquivalent: ""
        )
        aboutItem.target = self
        menu.addItem(aboutItem)

        menu.addItem(NSMenuItem.separator())

        // Quit — graceful shutdown of the daemon via UDS, then ourselves.
        // We don't use the default `NSApp.terminate(_:)` selector here
        // because that quits only the menu-bar process; the user
        // expectation when clicking Quit on the iogrid menu is "shut the
        // whole thing down".
        let quitItem = NSMenuItem(
            title: "Quit iogrid",
            action: #selector(quitDaemon(_:)),
            keyEquivalent: "q"
        )
        quitItem.target = self
        menu.addItem(quitItem)

        return menu
    }

    @objc private func checkForUpdates(_ sender: Any?) {
        // SPUStandardUpdaterController validates that we're allowed to
        // check (not currently downloading, not in offline mode, etc.)
        // and shows the standard Sparkle modal. Logged via the framework.
        updaterController.checkForUpdates(sender)
    }

    @objc private func showAboutPanel(_ sender: Any?) {
        NSApp.activate(ignoringOtherApps: true)
        NSApp.orderFrontStandardAboutPanel(sender)
    }

    @objc private func quitDaemon(_ sender: Any?) {
        // Fire-and-forget the daemon quit. The daemon's IPC listener
        // (Rust side, crate `iogrid-core::ipc_mac`) closes the socket
        // after acking; we don't block the UI thread on that.
        DispatchQueue.global(qos: .userInitiated).async {
            let result = IogriddIPC.send(command: "quit")
            // Log to stderr (captured by the LaunchAgent's StandardErrorPath)
            // so we have a trail when debugging quit failures.
            switch result {
            case .success:
                fputs("[iogridd-statusbar] daemon quit ack received\n", stderr)
            case .failure(let err):
                fputs("[iogridd-statusbar] daemon quit IPC failed: \(err)\n", stderr)
            }
            // Always terminate the menu-bar process; if the daemon UDS
            // wasn't reachable the user already wanted to stop, no need
            // to leave the icon dangling.
            DispatchQueue.main.async {
                NSApp.terminate(nil)
            }
        }
    }
}

// Entry point. We avoid the `@main` attribute because some Sparkle 2.6
// builds emit a duplicate-symbol warning under SwiftPM when @main is
// combined with `NSApplication.shared`. Manual `NSApplicationMain`
// equivalent below is bulletproof across Swift 5.9 and 5.10.
let app = NSApplication.shared
let delegate = IogriddStatusbarAppDelegate()
app.delegate = delegate
// Accessory activation policy: menu-bar item, no Dock icon, no app
// switcher entry. Combined with `LSUIElement=true` in Info.plist this
// gives the standard "menulet" appearance.
app.setActivationPolicy(.accessory)
app.run()
