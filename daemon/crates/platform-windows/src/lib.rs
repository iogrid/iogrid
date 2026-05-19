//! Windows-specific bits.
//!
//! Idle detection uses `GetLastInputInfo` from `user32.dll` (Windows-only
//! crate target). Service registration uses SCM (Service Control Manager)
//! APIs; the install/uninstall helpers below shell out to `sc.exe` so we
//! don't need to add the heavy `windows-service` dep just for two calls.

// `forbid(unsafe_code)` would block the Win32 FFI call needed for
// GetLastInputInfo. We confine `unsafe` to the single FFI line and audit it.
#![cfg_attr(not(target_os = "windows"), forbid(unsafe_code))]
#![deny(missing_docs)]

use std::path::PathBuf;

/// Path constants for the Windows Service install.
pub mod paths {
    /// Service name shown in `services.msc`.
    pub const SERVICE_NAME: &str = "iogridd";
    /// Service display name.
    pub const SERVICE_DISPLAY_NAME: &str = "iogrid provider daemon";
    /// Daemon binary install location.
    pub const BINARY: &str = r"C:\Program Files\iogrid\iogridd.exe";
    /// Config file path relative to `%APPDATA%`.
    pub const CONFIG: &str = r"iogrid\config.toml";
    /// Logs dir relative to `%LOCALAPPDATA%`.
    pub const LOG_DIR: &str = r"iogrid\logs";
    /// Daemon state dir relative to `%APPDATA%`.
    pub const STATE_DIR: &str = r"iogrid";
}

/// Idle seconds — see crate docs.
pub fn idle_seconds() -> u64 {
    #[cfg(target_os = "windows")]
    {
        get_last_input_idle_secs().unwrap_or(u64::MAX)
    }
    #[cfg(not(target_os = "windows"))]
    {
        u64::MAX
    }
}

#[cfg(target_os = "windows")]
fn get_last_input_idle_secs() -> Option<u64> {
    use windows_sys::Win32::System::SystemInformation::GetTickCount;
    use windows_sys::Win32::UI::Input::KeyboardAndMouse::{GetLastInputInfo, LASTINPUTINFO};
    let mut info = LASTINPUTINFO {
        cbSize: std::mem::size_of::<LASTINPUTINFO>() as u32,
        dwTime: 0,
    };
    // SAFETY: `info` is a properly-sized, owned struct. `GetLastInputInfo`
    // writes into `dwTime` only. No allocation, no pointer lifetimes
    // crossing the call boundary.
    let ok = unsafe { GetLastInputInfo(&mut info) };
    if ok == 0 {
        return None;
    }
    // SAFETY: `GetTickCount` reads a global counter, no pointer in/out.
    let now = unsafe { GetTickCount() };
    let elapsed_ms = now.wrapping_sub(info.dwTime);
    Some(u64::from(elapsed_ms) / 1000)
}

/// True if this binary was compiled for Windows.
pub const fn is_supported() -> bool {
    cfg!(target_os = "windows")
}

/// Resolve `%APPDATA%` → state dir absolute path.
pub fn state_dir() -> Option<PathBuf> {
    let appdata = std::env::var_os("APPDATA").map(PathBuf::from)?;
    Some(appdata.join(paths::STATE_DIR))
}

/// Install the Windows Service via `sc.exe create`. Returns `Ok(true)` if
/// newly created, `Ok(false)` if already present.
pub fn install_service() -> anyhow::Result<bool> {
    let st = std::process::Command::new("sc.exe")
        .args(["query", paths::SERVICE_NAME])
        .output();
    if let Ok(out) = st {
        if out.status.success() {
            return Ok(false);
        }
    }
    let st = std::process::Command::new("sc.exe")
        .args([
            "create",
            paths::SERVICE_NAME,
            "binPath=",
            paths::BINARY,
            "DisplayName=",
            paths::SERVICE_DISPLAY_NAME,
            "start=",
            "auto",
        ])
        .status();
    match st {
        Ok(s) if s.success() => Ok(true),
        Ok(s) => anyhow::bail!("sc.exe create failed: status {s:?}"),
        Err(e) => anyhow::bail!("failed to exec sc.exe: {e}"),
    }
}

/// Uninstall the Windows Service via `sc.exe delete`.
pub fn uninstall_service() -> anyhow::Result<()> {
    let st = std::process::Command::new("sc.exe")
        .args(["delete", paths::SERVICE_NAME])
        .status()?;
    if !st.success() {
        anyhow::bail!("sc.exe delete failed: status {st:?}");
    }
    Ok(())
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

    #[test]
    fn idle_returns_value() {
        let _ = idle_seconds();
    }
}
