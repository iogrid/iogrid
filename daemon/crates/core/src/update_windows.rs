//! Windows in-place updater — hands off to Squirrel.Windows' `Update.exe`.
//!
//! Background: PR #401 ships [`Update.exe`](https://github.com/Squirrel/Squirrel.Windows)
//! (Squirrel's renamed `Setup.exe`) inside the iogrid `.msi` at install
//! root `C:\Program Files\iogrid\Update.exe`. The supervisor's job in
//! this module is to invoke it once per day with
//!
//! ```text
//! Update.exe --update <FEED_URL>
//! ```
//!
//! so the daemon's user gets the side-by-side `app-X.Y.Z\iogridd.exe`
//! convention Squirrel manages, instead of the cross-platform
//! in-process atomic-replace path under [`crate::updater`]. The
//! cross-platform path remains the fallback for Linux + macOS today.
//!
//! Important: `Update.exe` is a long-running child that downloads
//! deltas + stages a new app dir; running it **synchronously** from the
//! supervisor would block the runtime, and worse, when Update.exe
//! decides to relaunch the parent it sends the parent a termination
//! signal. So we spawn it detached (`kill_on_drop(false)`) and wait
//! for the exit status from a dedicated tokio task. The supervisor
//! itself stays on its main loop.
//!
//! Tracking: iogrid#399 (this module), iogrid#401 (Update.exe ship),
//! iogrid#348 (EPIC parent).

#![cfg(any(windows, doc, test))]

use std::path::PathBuf;

/// Outcome of a single `Update.exe --update <FEED>` invocation.
///
/// The Squirrel exit-code convention is the source of truth:
/// `0` means "ran to completion" — but Squirrel also exits 0 when there
/// was nothing to do, so we differentiate via the new-version log
/// line on stdout, parsed by [`parse_update_status`].
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum UpdateStatus {
    /// A new version was downloaded + staged. `version` is the
    /// semver string Squirrel emitted (e.g. "0.2.7").
    Applied {
        /// The newly-staged semver string.
        version: String,
    },
    /// Update.exe ran but reported no newer version available.
    None,
    /// Update.exe failed (non-zero exit, spawn failure, or feed
    /// unreachable). The string is the diagnostic for the audit log.
    Error(String),
}

/// Where on disk we expect `Update.exe` to live.
///
/// Resolution order:
///   1. Sibling of the current executable (`current_exe()` parent).
///      This matches both production (.msi places iogridd.exe and
///      Update.exe in the same directory) and Squirrel's own
///      side-by-side convention where `Update.exe` sits one level
///      ABOVE the per-version `app-X.Y.Z\` directory.
///   2. Fallback hard-coded path `C:\Program Files\iogrid\Update.exe`
///      for the rare case the running daemon is being invoked via a
///      symlink and `current_exe()` resolves to the symlink target.
///
/// On non-Windows the fallback returns the `/usr/local/...` path used
/// by the cross-platform fallback; callers should treat the path as
/// hint-only there.
#[cfg(windows)]
pub fn locate_update_exe() -> PathBuf {
    if let Ok(exe) = std::env::current_exe() {
        if let Some(parent) = exe.parent() {
            let sibling = parent.join("Update.exe");
            if sibling.exists() {
                return sibling;
            }
            // Squirrel convention: when the running iogridd.exe lives at
            // `...\app-X.Y.Z\iogridd.exe`, Update.exe sits one level up
            // at `...\Update.exe`.
            if let Some(grandparent) = parent.parent() {
                let above = grandparent.join("Update.exe");
                if above.exists() {
                    return above;
                }
            }
        }
    }
    PathBuf::from(r"C:\Program Files\iogrid\Update.exe")
}

/// Stub for non-Windows targets. Returns a placeholder path; the
/// real invocation path is `#[cfg(windows)]`-gated, so this is only
/// reachable from the unit tests below that exercise the pure parser.
#[cfg(not(windows))]
pub fn locate_update_exe() -> PathBuf {
    PathBuf::from("/nonexistent/Update.exe")
}

/// Parse the stdout / status combo of one `Update.exe --update <FEED>`
/// invocation into an [`UpdateStatus`].
///
/// Squirrel.Windows' `Update.exe` writes a one-line summary to stdout
/// in one of two shapes:
///
///   * `New version available: 0.2.7` — followed by progress logs.
///   * `No updates available` — when the local app version matches HEAD.
///
/// On any other line, with a zero exit code, we conservatively report
/// `None` (Squirrel exited cleanly with no actionable new version).
/// A non-zero exit code is always [`UpdateStatus::Error`].
///
/// Kept as a free function so it can be tested cross-platform without
/// requiring a Windows host or a real Update.exe binary.
pub fn parse_update_status(exit_code: i32, stdout: &str) -> UpdateStatus {
    if exit_code != 0 {
        return UpdateStatus::Error(format!(
            "Update.exe exited with code {exit_code}; stdout={}",
            stdout.lines().last().unwrap_or("<empty>")
        ));
    }
    for line in stdout.lines() {
        let trimmed = line.trim();
        if let Some(rest) = trimmed.strip_prefix("New version available:") {
            let v = rest.trim().to_string();
            if !v.is_empty() {
                return UpdateStatus::Applied { version: v };
            }
        }
        if trimmed.eq_ignore_ascii_case("No updates available") {
            return UpdateStatus::None;
        }
    }
    UpdateStatus::None
}

/// Spawn `Update.exe --update <feed_url>` and await its outcome.
///
/// Detached child — we set `kill_on_drop(false)` so that if the
/// supervisor is shut down mid-update, the OS keeps the update
/// running to completion. Update.exe is the one process that needs
/// to survive the daemon's exit (it relaunches iogridd post-update).
///
/// Returns:
///   * [`UpdateStatus::Applied`] when Squirrel reports a new version
///     was downloaded + staged. The Windows SCM will pick up the
///     new `iogridd.exe` on next service restart (Squirrel updates
///     the `current` symlink atomically).
///   * [`UpdateStatus::None`] when Squirrel ran but had nothing to do.
///   * [`UpdateStatus::Error`] for spawn failures + non-zero exits.
///     Logged at WARN; the caller continues; next tick retries.
#[cfg(windows)]
pub async fn check_for_updates(feed_url: &str) -> UpdateStatus {
    let exe = locate_update_exe();
    if !exe.exists() {
        return UpdateStatus::Error(format!(
            "Update.exe not found at expected paths (looked under current_exe parent and {})",
            exe.display()
        ));
    }
    tracing::info!(
        feed = %feed_url,
        update_exe = %exe.display(),
        "spawning Update.exe --update for in-place update check",
    );
    let mut cmd = tokio::process::Command::new(&exe);
    cmd.arg("--update")
        .arg(feed_url)
        .kill_on_drop(false)
        .stdin(std::process::Stdio::null())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped());
    let output = match cmd.output().await {
        Ok(o) => o,
        Err(e) => {
            return UpdateStatus::Error(format!(
                "failed to spawn Update.exe at {}: {e}",
                exe.display()
            ));
        }
    };
    let exit_code = output.status.code().unwrap_or(-1);
    let stdout = String::from_utf8_lossy(&output.stdout);
    let status = parse_update_status(exit_code, &stdout);
    match &status {
        UpdateStatus::Applied { version } => tracing::info!(
            new_version = %version,
            "Update.exe staged a new iogridd version; service restart will activate it",
        ),
        UpdateStatus::None => tracing::debug!("Update.exe reported no new version available"),
        UpdateStatus::Error(msg) => tracing::warn!(error = %msg, "Update.exe invocation failed"),
    }
    status
}

/// Non-Windows stub. The supervisor never calls this on Linux/macOS
/// (the call-site is `#[cfg(target_os = "windows")]`); we keep the
/// symbol exported so test code that wants to exercise parse logic
/// can compile cross-platform.
#[cfg(not(windows))]
pub async fn check_for_updates(_feed_url: &str) -> UpdateStatus {
    UpdateStatus::Error("Windows-only updater path called on non-Windows host".into())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_applied_emits_version() {
        let stdout = "Connecting to feed...\nNew version available: 0.2.7\nDownloading delta...\n";
        match parse_update_status(0, stdout) {
            UpdateStatus::Applied { version } => assert_eq!(version, "0.2.7"),
            other => panic!("expected Applied, got {other:?}"),
        }
    }

    #[test]
    fn parse_no_update_is_none() {
        let stdout = "Connecting to feed...\nNo updates available\n";
        assert_eq!(parse_update_status(0, stdout), UpdateStatus::None);
    }

    #[test]
    fn parse_nonzero_exit_is_error() {
        let stdout = "Feed unreachable\n";
        match parse_update_status(1, stdout) {
            UpdateStatus::Error(msg) => assert!(msg.contains("exited with code 1"), "got: {msg}"),
            other => panic!("expected Error, got {other:?}"),
        }
    }

    #[test]
    fn parse_empty_stdout_zero_exit_is_none() {
        assert_eq!(parse_update_status(0, ""), UpdateStatus::None);
    }

    #[test]
    fn parse_applied_trims_whitespace_around_version() {
        let stdout = "New version available:    0.3.0   \n";
        match parse_update_status(0, stdout) {
            UpdateStatus::Applied { version } => assert_eq!(version, "0.3.0"),
            other => panic!("expected Applied, got {other:?}"),
        }
    }

    #[test]
    fn parse_applied_blank_version_falls_back_to_none() {
        // Defensive: if Squirrel ever emits the prefix without a version,
        // don't pretend an update happened.
        let stdout = "New version available:   \n";
        assert_eq!(parse_update_status(0, stdout), UpdateStatus::None);
    }

    #[test]
    fn parse_no_updates_is_case_insensitive() {
        let stdout = "no updates available\n";
        assert_eq!(parse_update_status(0, stdout), UpdateStatus::None);
    }
}
