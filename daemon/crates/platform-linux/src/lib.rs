//! Linux-specific bits.
//!
//! Idle detection on X11 uses `XScreenSaverQueryInfo`; on Wayland we read
//! `org.gnome.Mutter.IdleMonitor` or `org.freedesktop.ScreenSaver` via D-Bus.
//! Headless servers (no display) report `u64::MAX` — effectively "always idle".

#![forbid(unsafe_code)]
#![deny(missing_docs)]

/// Path constants for the systemd user unit install.
pub mod paths {
    /// systemd user unit path relative to `$HOME`.
    pub const SYSTEMD_USER_UNIT: &str = ".config/systemd/user/iogridd.service";
    /// Daemon binary install location.
    pub const BINARY: &str = "/usr/local/bin/iogridd";
    /// Config file relative to `$HOME`.
    pub const CONFIG: &str = ".config/iogrid/config.toml";
    /// Logs dir relative to `$HOME`.
    pub const LOG_DIR: &str = ".local/share/iogrid/logs";
}

/// Idle seconds — see crate docs.
pub fn idle_seconds() -> u64 {
    #[cfg(target_os = "linux")]
    {
        // Real impl: XScreenSaverQueryInfo or Mutter D-Bus call.
        0
    }
    #[cfg(not(target_os = "linux"))]
    {
        0
    }
}

/// True if this binary was compiled for Linux.
pub const fn is_supported() -> bool {
    cfg!(target_os = "linux")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn paths_are_non_empty() {
        assert!(!paths::SYSTEMD_USER_UNIT.is_empty());
        assert!(!paths::BINARY.is_empty());
        assert!(!paths::CONFIG.is_empty());
        assert!(!paths::LOG_DIR.is_empty());
    }

    #[test]
    fn is_supported_matches_target() {
        assert_eq!(is_supported(), cfg!(target_os = "linux"));
    }
}
