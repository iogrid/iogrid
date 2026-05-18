//! Windows-specific bits.
//!
//! Idle detection uses `GetLastInputInfo` from `user32.dll`. Service registration
//! uses SCM (Service Control Manager) APIs via the `windows-service` crate in
//! the real impl.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

/// Path constants for the Windows Service install.
pub mod paths {
    /// Service name shown in `services.msc`.
    pub const SERVICE_NAME: &str = "iogridd";
    /// Daemon binary install location.
    pub const BINARY: &str = r"C:\Program Files\iogrid\iogridd.exe";
    /// Config file path relative to `%APPDATA%`.
    pub const CONFIG: &str = r"iogrid\config.toml";
    /// Logs dir relative to `%LOCALAPPDATA%`.
    pub const LOG_DIR: &str = r"iogrid\logs";
}

/// Idle seconds — see crate docs.
pub fn idle_seconds() -> u64 {
    #[cfg(target_os = "windows")]
    {
        // Real impl: GetLastInputInfo → (now - LASTINPUTINFO.dwTime) / 1000
        0
    }
    #[cfg(not(target_os = "windows"))]
    {
        0
    }
}

/// True if this binary was compiled for Windows.
pub const fn is_supported() -> bool {
    cfg!(target_os = "windows")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn paths_are_non_empty() {
        assert!(!paths::SERVICE_NAME.is_empty());
        assert!(!paths::BINARY.is_empty());
        assert!(!paths::CONFIG.is_empty());
        assert!(!paths::LOG_DIR.is_empty());
    }

    #[test]
    fn is_supported_matches_target() {
        assert_eq!(is_supported(), cfg!(target_os = "windows"));
    }
}
