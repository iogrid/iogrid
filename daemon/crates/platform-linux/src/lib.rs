//! Linux-specific bits.
//!
//! Idle detection strategy (in order of preference):
//!  1. `xprintidle(1)` subprocess — works on X11 desktops without pulling X
//!     bindings into our static binary.
//!  2. `loginctl show-session $XDG_SESSION_ID -p IdleSinceHint` — works on
//!     systemd-logind-managed sessions (covers Wayland + GNOME / KDE).
//!  3. Read mtime of `/dev/input/event*` — fallback for desktops without the
//!     above utilities.
//!  4. Headless servers (no display, no logind, no input dev) report
//!     `u64::MAX` — effectively "always idle".
//!
//! No X11/Wayland C bindings — keeps the static-musl build trivial.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::path::PathBuf;
#[cfg(target_os = "linux")]
use std::time::{Duration, SystemTime};

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
    /// Daemon state dir relative to `$HOME` (cert, key, ledger).
    pub const STATE_DIR: &str = ".iogrid";
}

/// systemd user unit body for `iogridd`.
pub const SYSTEMD_USER_UNIT_BODY: &str = r#"[Unit]
Description=iogrid provider daemon
Documentation=https://iogrid.org/docs/daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/iogridd
Restart=on-failure
RestartSec=5s
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=default.target
"#;

/// Idle seconds — see crate docs.
///
/// Always returns *some* value: on platforms / desktops where we cannot
/// detect idle we return `u64::MAX` which is the "treat as idle" semantic.
pub fn idle_seconds() -> u64 {
    #[cfg(target_os = "linux")]
    {
        if let Some(secs) = idle_from_xprintidle() {
            return secs;
        }
        if let Some(secs) = idle_from_loginctl() {
            return secs;
        }
        if let Some(secs) = idle_from_input_devices() {
            return secs;
        }
        u64::MAX
    }
    #[cfg(not(target_os = "linux"))]
    {
        u64::MAX
    }
}

#[cfg(target_os = "linux")]
fn idle_from_xprintidle() -> Option<u64> {
    let out = std::process::Command::new("xprintidle").output().ok()?;
    if !out.status.success() {
        return None;
    }
    let s = String::from_utf8_lossy(&out.stdout);
    let ms: u64 = s.trim().parse().ok()?;
    Some(ms / 1000)
}

#[cfg(target_os = "linux")]
fn idle_from_loginctl() -> Option<u64> {
    let session = std::env::var("XDG_SESSION_ID").ok()?;
    let out = std::process::Command::new("loginctl")
        .args(["show-session", &session, "-p", "IdleSinceHint"])
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    let s = String::from_utf8_lossy(&out.stdout);
    let v = s
        .split('=')
        .nth(1)
        .map(str::trim)
        .and_then(|x| x.parse::<u64>().ok())?;
    if v == 0 {
        return None;
    }
    let now_us = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .ok()?
        .as_micros() as u64;
    Some(now_us.saturating_sub(v) / 1_000_000)
}

#[cfg(target_os = "linux")]
fn idle_from_input_devices() -> Option<u64> {
    let mut newest: Option<SystemTime> = None;
    for entry in std::fs::read_dir("/dev/input").ok()?.flatten() {
        let p = entry.path();
        if !p
            .file_name()
            .and_then(|n| n.to_str())
            .map(|n| n.starts_with("event"))
            .unwrap_or(false)
        {
            continue;
        }
        if let Ok(meta) = entry.metadata() {
            if let Ok(mtime) = meta.modified() {
                if newest.map(|n| mtime > n).unwrap_or(true) {
                    newest = Some(mtime);
                }
            }
        }
    }
    let mtime = newest?;
    Some(
        SystemTime::now()
            .duration_since(mtime)
            .unwrap_or_else(|_| Duration::from_secs(0))
            .as_secs(),
    )
}

/// True if this binary was compiled for Linux.
pub const fn is_supported() -> bool {
    cfg!(target_os = "linux")
}

/// Resolve `$HOME` → systemd user unit absolute path.
pub fn systemd_unit_path() -> Option<PathBuf> {
    let home = std::env::var_os("HOME").map(PathBuf::from)?;
    Some(home.join(paths::SYSTEMD_USER_UNIT))
}

/// Install (write) the systemd user unit if not already present.
///
/// Returns `Ok(true)` if the unit was newly written, `Ok(false)` if it was
/// already present and unchanged.
pub fn install_systemd_unit() -> anyhow::Result<bool> {
    let p = systemd_unit_path()
        .ok_or_else(|| anyhow::anyhow!("HOME unset; cannot install systemd unit"))?;
    if let Some(parent) = p.parent() {
        std::fs::create_dir_all(parent)?;
    }
    if let Ok(existing) = std::fs::read_to_string(&p) {
        if existing == SYSTEMD_USER_UNIT_BODY {
            return Ok(false);
        }
    }
    std::fs::write(&p, SYSTEMD_USER_UNIT_BODY)?;
    Ok(true)
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

    #[test]
    fn systemd_unit_body_has_required_sections() {
        assert!(SYSTEMD_USER_UNIT_BODY.contains("[Unit]"));
        assert!(SYSTEMD_USER_UNIT_BODY.contains("[Service]"));
        assert!(SYSTEMD_USER_UNIT_BODY.contains("[Install]"));
        assert!(SYSTEMD_USER_UNIT_BODY.contains("/usr/local/bin/iogridd"));
    }

    #[test]
    fn idle_seconds_returns_a_value() {
        // We only assert that it doesn't panic; the exact value depends on
        // the test runner's environment (CI is headless => u64::MAX).
        let _ = idle_seconds();
    }
}
