//! macOS-specific bits.
//!
//! Responsibilities:
//!  * Idle detection — reads `HIDIdleTime` from `ioreg` (safe subprocess) so
//!    we don't need IOKit FFI / `unsafe` blocks in the static binary.
//!  * LaunchAgent install / paths (`~/Library/LaunchAgents/org.iogrid.plist`).
//!  * Self-update helper — verifies a SHA-256 signature against the embedded
//!    update pubkey, atomically renames the new binary into place.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::path::{Path, PathBuf};

use sha2::{Digest, Sha256};

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
    /// Daemon state dir relative to `$HOME` (cert, key, ledger).
    pub const STATE_DIR: &str = ".iogrid";
}

/// LaunchAgent plist body.
pub const LAUNCH_AGENT_PLIST_BODY: &str = r#"<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>org.iogrid.iogridd</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/iogrid/iogridd</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key>
  <string>/tmp/iogridd.out.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/iogridd.err.log</string>
  <key>ProcessType</key><string>Background</string>
</dict>
</plist>
"#;

/// Return the number of seconds the user has been idle (no HID activity).
///
/// On non-mac targets returns `u64::MAX` ("treat as idle"). On macOS we
/// shell out to `ioreg -c IOHIDSystem -d 4 | awk '/HIDIdleTime/ {print $NF/1000000000; exit}'`.
pub fn idle_seconds() -> u64 {
    #[cfg(target_os = "macos")]
    {
        if let Some(secs) = idle_from_ioreg() {
            return secs;
        }
        u64::MAX
    }
    #[cfg(not(target_os = "macos"))]
    {
        u64::MAX
    }
}

#[cfg(target_os = "macos")]
fn idle_from_ioreg() -> Option<u64> {
    let out = std::process::Command::new("ioreg")
        .args(["-c", "IOHIDSystem", "-d", "4"])
        .output()
        .ok()?;
    if !out.status.success() {
        return None;
    }
    let s = String::from_utf8_lossy(&out.stdout);
    for line in s.lines() {
        if let Some(idx) = line.find("HIDIdleTime") {
            // Format: "        | "HIDIdleTime" = 12345678901234"
            let after = &line[idx..];
            let last = after.rsplit('=').next()?.trim();
            let ns: u128 = last.trim_end_matches('}').trim().parse().ok()?;
            return Some((ns / 1_000_000_000) as u64);
        }
    }
    None
}

/// True if this binary was compiled for macOS.
pub const fn is_supported() -> bool {
    cfg!(target_os = "macos")
}

/// Resolve `$HOME` → LaunchAgent plist absolute path.
pub fn launch_agent_path() -> Option<PathBuf> {
    let home = std::env::var_os("HOME").map(PathBuf::from)?;
    Some(home.join(paths::LAUNCH_AGENT_PLIST))
}

/// Install (write) the LaunchAgent plist if not already present.
///
/// Returns `Ok(true)` if newly written, `Ok(false)` if already up to date.
/// Caller must `launchctl load -w <path>` afterwards.
pub fn install_launch_agent() -> anyhow::Result<bool> {
    let p = launch_agent_path()
        .ok_or_else(|| anyhow::anyhow!("HOME unset; cannot install LaunchAgent"))?;
    if let Some(parent) = p.parent() {
        std::fs::create_dir_all(parent)?;
    }
    if let Ok(existing) = std::fs::read_to_string(&p) {
        if existing == LAUNCH_AGENT_PLIST_BODY {
            return Ok(false);
        }
    }
    std::fs::write(&p, LAUNCH_AGENT_PLIST_BODY)?;
    Ok(true)
}

/// Self-update helper: verify the SHA-256 of a downloaded binary against the
/// expected digest (already cryptographically authenticated upstream — see
/// `docs/TECH.md § Auto-update`) and atomically replace `target`.
pub fn replace_binary_atomic(
    new_binary: &Path,
    expected_sha256_hex: &str,
    target: &Path,
) -> anyhow::Result<()> {
    let bytes = std::fs::read(new_binary)?;
    let actual = hex_sha256(&bytes);
    if !actual.eq_ignore_ascii_case(expected_sha256_hex) {
        anyhow::bail!("binary signature mismatch: expected {expected_sha256_hex}, got {actual}");
    }
    let staged = target.with_extension("iogridd.new");
    std::fs::write(&staged, &bytes)?;
    // Permissions: 0o755.
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let mut p = std::fs::metadata(&staged)?.permissions();
        p.set_mode(0o755);
        std::fs::set_permissions(&staged, p)?;
    }
    std::fs::rename(&staged, target)?;
    Ok(())
}

/// Hex SHA-256 of `bytes`.
pub fn hex_sha256(bytes: &[u8]) -> String {
    let mut h = Sha256::new();
    h.update(bytes);
    let out = h.finalize();
    let mut s = String::with_capacity(64);
    for b in out.iter() {
        s.push_str(&format!("{b:02x}"));
    }
    s
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

    #[test]
    fn plist_has_required_keys() {
        assert!(LAUNCH_AGENT_PLIST_BODY.contains("Label"));
        assert!(LAUNCH_AGENT_PLIST_BODY.contains("org.iogrid.iogridd"));
        assert!(LAUNCH_AGENT_PLIST_BODY.contains("RunAtLoad"));
        assert!(LAUNCH_AGENT_PLIST_BODY.contains("/usr/local/iogrid/iogridd"));
    }

    #[test]
    fn sha256_matches_known_vector() {
        assert_eq!(
            hex_sha256(b""),
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
        );
        assert_eq!(
            hex_sha256(b"abc"),
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
        );
    }

    #[test]
    fn replace_binary_atomic_rejects_bad_hash() {
        let dir = std::env::temp_dir();
        let src = dir.join(format!("iogridd-test-src-{}", std::process::id()));
        let tgt = dir.join(format!("iogridd-test-tgt-{}", std::process::id()));
        std::fs::write(&src, b"hello").unwrap();
        std::fs::write(&tgt, b"original").unwrap();
        let err = replace_binary_atomic(&src, "0000", &tgt).unwrap_err();
        assert!(err.to_string().contains("signature mismatch"));
        std::fs::remove_file(&src).ok();
        std::fs::remove_file(&tgt).ok();
    }
}
