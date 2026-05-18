//! macOS-specific bits.
//!
//! Two responsibilities:
//!  * IOKit-based idle detection (HID idle time via `IOHIDSystem`).
//!  * LaunchAgent install / paths (`~/Library/LaunchAgents/org.iogrid.plist`).
//!
//! Real implementation uses `core-foundation` + `objc2` to bridge IOKit; the
//! scaffold returns a deterministic 0-seconds idle reading on every platform
//! so the workspace `cargo check` is clean cross-target.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

/// Path constants for the LaunchAgent install.
pub mod paths {
    /// LaunchAgent plist relative to `$HOME`.
    pub const LAUNCH_AGENT_PLIST: &str = "Library/LaunchAgents/org.iogrid.plist";
    /// Daemon binary install location.
    pub const BINARY: &str = "/usr/local/iogrid/iogridd";
    /// Config file relative to `$HOME`.
    pub const CONFIG: &str = "Library/Application Support/iogrid/config.toml";
    /// Logs dir relative to `$HOME`.
    pub const LOG_DIR: &str = "Library/Logs/iogrid";
}

/// Return the number of seconds the user has been idle (no HID activity).
///
/// On non-mac targets this returns `0` — caller should `cfg`-gate or check
/// [`is_supported`].
pub fn idle_seconds() -> u64 {
    #[cfg(target_os = "macos")]
    {
        // Real impl: call IOServiceGetMatchingService("IOHIDSystem") + read
        // CFNumber `HIDIdleTime` (nanoseconds), convert to seconds.
        0
    }
    #[cfg(not(target_os = "macos"))]
    {
        0
    }
}

/// True if this binary was compiled for macOS.
pub const fn is_supported() -> bool {
    cfg!(target_os = "macos")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn paths_are_non_empty() {
        assert!(!paths::LAUNCH_AGENT_PLIST.is_empty());
        assert!(!paths::BINARY.is_empty());
        assert!(!paths::CONFIG.is_empty());
        assert!(!paths::LOG_DIR.is_empty());
    }

    #[test]
    fn idle_returns_unsigned() {
        let _ = idle_seconds();
    }

    #[test]
    fn is_supported_matches_target() {
        assert_eq!(is_supported(), cfg!(target_os = "macos"));
    }
}
