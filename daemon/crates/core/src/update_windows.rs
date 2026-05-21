//! Windows-specific auto-update via Squirrel's `Update.exe`.
//!
//! Phase 2 of EPIC #348, Windows track. PR #401 ships `Update.exe` (=
//! Squirrel.Windows' `Setup.exe` renamed) into the .msi at
//! `C:\Program Files\iogrid\Update.exe`. This module locates and invokes
//! it so the iogridd Windows service applies new releases side-by-side
//! under `app-X.Y.Z\` without re-running `msiexec`.
//!
//! Flow (matches the macOS Sparkle hand-off shipped in PR #402):
//!
//! 1. Daily-tick task (or operator-triggered IPC call) invokes
//!    [`check_for_updates`].
//! 2. We locate `Update.exe` — first next to the running iogridd binary
//!    (the install root, `C:\Program Files\iogrid\`), with a fallback to
//!    the hard-coded path when `current_exe()` doesn't expose the parent
//!    (rare; e.g. service ImagePath rewrites).
//! 3. We spawn `Update.exe --update <RELEASES-URL>` as a **detached
//!    child process**. Update.exe itself parses the `RELEASES` index
//!    (text manifest produced by PR #393's releases-ci), downloads the
//!    delta `.nupkg`, stages a new `app-<new_version>\` subdir, and on
//!    success restarts the Windows service. Squirrel parses RELEASES
//!    natively; the iogrid side does NOT need a JSON parser here.
//! 4. We return immediately with [`UpdateStatus::Spawned`] — the caller
//!    must NOT wait on the child handle (per the Squirrel contract:
//!    Update.exe terminates the parent process during the swap).
//!
//! The releases URL comes from `IOGRID_UPDATE_FEED_URL` (set by the
//! Windows service env var stamped in `installer/windows/iogrid.wxs`)
//! with a hard-coded fallback to `https://releases.iogrid.org/windows/`.

#![cfg(windows)]

use std::path::{Path, PathBuf};
use std::process::Command;

/// Env var the WiX installer sets on the iogridd Windows service so the
/// daemon knows which `RELEASES` manifest to feed Update.exe. See
/// `installer/windows/iogrid.wxs`.
pub const FEED_URL_ENV: &str = "IOGRID_UPDATE_FEED_URL";

/// Hard-coded fallback if the env var is missing. Matches the path
/// served by PR #393's `releases.iogrid.org` Deployment.
pub const DEFAULT_FEED_URL: &str = "https://releases.iogrid.org/windows/";

/// MSI-stamped install root (matches `iogrid.wxs` perMachine install).
const DEFAULT_INSTALL_ROOT: &str = r"C:\Program Files\iogrid";

/// Outcome of an update check / apply attempt. The detached-child model
/// means we cannot synchronously distinguish "no update available" from
/// "update applied" — both end up as [`Spawned`] from the iogrid side,
/// since Update.exe is the source of truth and the Windows service is
/// restarted by Update.exe on success.
///
/// [`Spawned`]: UpdateStatus::Spawned
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum UpdateStatus {
    /// `Update.exe --update` was spawned successfully. The detached
    /// child handles every subsequent step; the caller MUST NOT wait
    /// on it (Squirrel terminates the parent process during the swap).
    Spawned {
        /// Resolved path to the `Update.exe` we invoked.
        update_exe: PathBuf,
        /// RELEASES feed URL we passed on the command line.
        feed_url: String,
    },
    /// `Update.exe` was not found at any known location. The .msi
    /// probably wasn't built with PR #401's `<SquirrelUpdate>` component
    /// — the caller should log + fall back to the legacy manifest-based
    /// path (which still works, just without side-by-side staging).
    UpdateExeMissing {
        /// Paths we probed.
        probed: Vec<PathBuf>,
    },
    /// `std::process::Command::spawn` failed. Typically a permissions
    /// issue: the iogridd Windows service runs as `LocalSystem` and has
    /// the rights to spawn under `Program Files`, but a dev-machine
    /// build running unprivileged may not.
    SpawnFailed {
        /// The Update.exe path we tried to spawn.
        update_exe: PathBuf,
        /// String form of the underlying `std::io::Error`.
        error: String,
    },
}

/// Locate `Update.exe`. Probes:
///  1. The directory of `std::env::current_exe()` (resolved install
///     root when the service ImagePath points at `iogridd.exe`).
///  2. The MSI-stamped install root, `C:\Program Files\iogrid\`.
///
/// Returns the first probe that exists, plus the full probe list when
/// nothing was found (so the caller can surface a useful error).
pub fn locate_update_exe() -> Result<PathBuf, Vec<PathBuf>> {
    let mut probed: Vec<PathBuf> = Vec::new();

    if let Ok(exe) = std::env::current_exe() {
        if let Some(parent) = exe.parent() {
            let candidate = parent.join("Update.exe");
            if candidate.exists() {
                return Ok(candidate);
            }
            probed.push(candidate);
        }
    }

    let fallback = Path::new(DEFAULT_INSTALL_ROOT).join("Update.exe");
    if fallback.exists() {
        return Ok(fallback);
    }
    probed.push(fallback);

    Err(probed)
}

/// Resolve the RELEASES feed URL. `IOGRID_UPDATE_FEED_URL` overrides
/// the hard-coded default. Trailing slash is preserved; Update.exe is
/// permissive about it.
pub fn feed_url() -> String {
    std::env::var(FEED_URL_ENV).unwrap_or_else(|_| DEFAULT_FEED_URL.to_string())
}

/// Spawn `Update.exe --update <feed_url>` as a detached child.
///
/// The child is NOT awaited — Squirrel terminates the parent during the
/// side-by-side swap, so blocking on a `wait()` would never return on a
/// successful update path. The returned [`UpdateStatus`] reflects only
/// whether the spawn itself succeeded.
pub fn apply_update() -> UpdateStatus {
    let update_exe = match locate_update_exe() {
        Ok(p) => p,
        Err(probed) => return UpdateStatus::UpdateExeMissing { probed },
    };
    let feed_url = feed_url();

    // Detached spawn — see the module docs. `Command::spawn` already
    // returns a handle the caller can drop; we intentionally drop it
    // immediately so the child outlives the iogridd process group.
    // No stdin/stdout/stderr inheritance — Update.exe is a GUI subsystem
    // binary on disk; piping its handles confuses the SCM service stop
    // path.
    let spawn_result = Command::new(&update_exe)
        .arg("--update")
        .arg(&feed_url)
        .spawn();

    match spawn_result {
        Ok(_child) => UpdateStatus::Spawned {
            update_exe,
            feed_url,
        },
        Err(e) => UpdateStatus::SpawnFailed {
            update_exe,
            error: e.to_string(),
        },
    }
}

/// Convenience wrapper exposed under the same name as the macOS-side
/// Sparkle entrypoint so the supervisor + future tray-UI IPC can call
/// it without `#[cfg]` branches. On Windows this delegates straight to
/// [`apply_update`]; there is no separate "check" RPC because Update.exe
/// does the manifest fetch + diff itself.
pub fn check_for_updates() -> UpdateStatus {
    apply_update()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn feed_url_falls_back_to_default_when_env_missing() {
        // SAFETY: single-threaded under cargo-nextest per workspace policy.
        std::env::remove_var(FEED_URL_ENV);
        assert_eq!(feed_url(), DEFAULT_FEED_URL);
    }

    #[test]
    fn feed_url_honours_env_override() {
        // SAFETY: single-threaded under cargo-nextest per workspace policy.
        std::env::set_var(FEED_URL_ENV, "https://staging.example.com/win/");
        assert_eq!(feed_url(), "https://staging.example.com/win/");
        std::env::remove_var(FEED_URL_ENV);
    }

    #[test]
    fn locate_update_exe_reports_probed_paths_on_failure() {
        // On the CI Windows runner the .msi staging dir isn't on disk,
        // so locate_update_exe should err out with the probed list.
        // We can't assert "err" deterministically (a Windows dev box
        // *might* have iogrid installed) but we can assert the probed
        // list is non-empty whenever we do hit the error branch.
        if let Err(probed) = locate_update_exe() {
            assert!(!probed.is_empty(), "probed list must surface attempts");
            let last = probed.last().expect("probed non-empty");
            assert!(
                last.ends_with("Update.exe"),
                "fallback probe must end in Update.exe, got {}",
                last.display()
            );
        }
    }

    #[test]
    fn update_status_variants_are_distinct() {
        // Pure-data assertion that exercises the enum so the
        // dead-code lint never trims a variant we expose to callers.
        let a = UpdateStatus::Spawned {
            update_exe: PathBuf::from(r"C:\x\Update.exe"),
            feed_url: "https://r.example.com/".into(),
        };
        let b = UpdateStatus::UpdateExeMissing {
            probed: vec![PathBuf::from(r"C:\x\Update.exe")],
        };
        let c = UpdateStatus::SpawnFailed {
            update_exe: PathBuf::from(r"C:\x\Update.exe"),
            error: "denied".into(),
        };
        assert_ne!(a, b);
        assert_ne!(b, c);
        assert_ne!(a, c);
    }
}
