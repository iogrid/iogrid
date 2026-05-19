//! Atomic-replace + rollback for the daemon binary.
//!
//! The flow is documented in `installer/auto-update/README.md`:
//!
//! 1. Download next binary to `<install>/iogridd.new`.
//! 2. Verify SHA-256 + (optional) Ed25519 signature.
//! 3. `rename(2)` over `<install>/iogridd.new` → `<install>/iogridd`
//!    after first copying the current `<install>/iogridd` to
//!    `<install>/iogridd.old`.
//! 4. Send SIGTERM to ourselves. The service manager
//!    (launchd/systemd/sc.exe) restarts us with the new image.
//! 5. The pre-exec wrapper (a tiny shim provided by
//!    `iogrid-platform-{mac,linux,windows}`) restores `iogridd.old`
//!    if the new binary exits within 30 s of first launch.
//!
//! This module is **pure-fs**: no network, no crypto. The worker in
//! `updater::worker` composes verify + stage + replace.

use std::fs;
use std::path::{Path, PathBuf};

use thiserror::Error;

/// Errors raised by the binary-replacement helpers.
#[derive(Debug, Error)]
pub enum BinaryError {
    /// I/O error.
    #[error("filesystem error at {path:?}: {source}")]
    Io {
        /// Path that errored.
        path: PathBuf,
        /// Underlying IO error.
        #[source]
        source: std::io::Error,
    },
    /// Install dir doesn't exist.
    #[error("install dir does not exist: {0:?}")]
    InstallDirMissing(PathBuf),
    /// Current binary doesn't exist.
    #[error("current binary not found at {0:?}")]
    CurrentMissing(PathBuf),
    /// Old (rollback) binary doesn't exist.
    #[error("no rollback binary at {0:?}")]
    OldMissing(PathBuf),
}

fn io_err(path: &Path) -> impl FnOnce(std::io::Error) -> BinaryError + '_ {
    move |e| BinaryError::Io {
        path: path.to_path_buf(),
        source: e,
    }
}

/// Layout of the install dir. Calculated once and passed around.
#[derive(Debug, Clone)]
pub struct InstallLayout {
    /// Directory containing the daemon binary.
    pub dir: PathBuf,
    /// Final binary name (e.g. `iogridd` on Unix, `iogridd.exe` on Windows).
    pub binary_name: String,
}

impl InstallLayout {
    /// Build the layout assuming the running binary lives at `dir/binary_name`.
    pub fn new(dir: PathBuf, binary_name: impl Into<String>) -> Self {
        Self {
            dir,
            binary_name: binary_name.into(),
        }
    }

    /// Path of the currently-running binary.
    pub fn current(&self) -> PathBuf {
        self.dir.join(&self.binary_name)
    }

    /// Path used to stage the download before replacement.
    pub fn staged(&self) -> PathBuf {
        self.dir.join(format!("{}.new", self.binary_name))
    }

    /// Path used to retain the previous binary for rollback.
    pub fn previous(&self) -> PathBuf {
        self.dir.join(format!("{}.old", self.binary_name))
    }
}

/// Write `blob` to `layout.staged()` atomically: the file is created
/// with `.partial` suffix and renamed in-place once the full payload
/// is on disk. This avoids the staged path being half-written if the
/// caller crashes mid-write.
///
/// Sets the unix-execute bit on the staged file (0o755) so the
/// subsequent rename produces an executable binary.
pub fn write_staged(layout: &InstallLayout, blob: &[u8]) -> Result<PathBuf, BinaryError> {
    if !layout.dir.exists() {
        return Err(BinaryError::InstallDirMissing(layout.dir.clone()));
    }
    let staged = layout.staged();
    let partial = layout
        .dir
        .join(format!("{}.new.partial", layout.binary_name));
    fs::write(&partial, blob).map_err(io_err(&partial))?;
    set_executable(&partial)?;
    fs::rename(&partial, &staged).map_err(io_err(&staged))?;
    Ok(staged)
}

/// Atomically replace the current binary with the staged one.
///
/// 1. If a current binary exists, copy it to `iogridd.old`.
/// 2. Rename the staged `iogridd.new` over `iogridd`.
///
/// On Unix `rename(2)` is atomic on the same filesystem. The currently
/// running daemon keeps its open file mapping (inode reference) and
/// will continue to execute the old image until restart.
pub fn replace_with_staged(layout: &InstallLayout) -> Result<(), BinaryError> {
    let cur = layout.current();
    let staged = layout.staged();
    let prev = layout.previous();
    if !staged.exists() {
        return Err(BinaryError::CurrentMissing(staged));
    }
    if cur.exists() {
        // Best-effort copy — if the user removed write perms on `dir`,
        // the rename below will fail anyway and we'll surface that.
        if prev.exists() {
            fs::remove_file(&prev).map_err(io_err(&prev))?;
        }
        fs::copy(&cur, &prev).map_err(io_err(&prev))?;
    }
    fs::rename(&staged, &cur).map_err(io_err(&cur))?;
    set_executable(&cur)?;
    Ok(())
}

/// Restore the `.old` binary over the current binary. Used when the
/// post-update health-check fails.
pub fn rollback(layout: &InstallLayout) -> Result<(), BinaryError> {
    let cur = layout.current();
    let prev = layout.previous();
    if !prev.exists() {
        return Err(BinaryError::OldMissing(prev));
    }
    if cur.exists() {
        fs::remove_file(&cur).map_err(io_err(&cur))?;
    }
    fs::rename(&prev, &cur).map_err(io_err(&cur))?;
    set_executable(&cur)?;
    Ok(())
}

#[cfg(unix)]
fn set_executable(p: &Path) -> Result<(), BinaryError> {
    use std::os::unix::fs::PermissionsExt;
    let mut perm = fs::metadata(p).map_err(io_err(p))?.permissions();
    perm.set_mode(0o755);
    fs::set_permissions(p, perm).map_err(io_err(p))?;
    Ok(())
}

#[cfg(not(unix))]
fn set_executable(_p: &Path) -> Result<(), BinaryError> {
    // No-op on Windows — file extension governs executability.
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn layout(dir: &Path) -> InstallLayout {
        InstallLayout::new(dir.to_path_buf(), "iogridd")
    }

    #[test]
    fn write_staged_writes_with_executable_bit() {
        let tmp = tempfile::tempdir().unwrap();
        let l = layout(tmp.path());
        let staged = write_staged(&l, b"#!/bin/sh\necho hello\n").unwrap();
        assert!(staged.exists());
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode = fs::metadata(&staged).unwrap().permissions().mode();
            assert_eq!(mode & 0o777, 0o755);
        }
    }

    #[test]
    fn replace_renames_and_preserves_old() {
        let tmp = tempfile::tempdir().unwrap();
        let l = layout(tmp.path());
        fs::write(l.current(), b"v1").unwrap();
        write_staged(&l, b"v2").unwrap();
        replace_with_staged(&l).unwrap();
        assert_eq!(fs::read(l.current()).unwrap(), b"v2");
        assert_eq!(fs::read(l.previous()).unwrap(), b"v1");
        assert!(!l.staged().exists());
    }

    #[test]
    fn replace_works_when_no_old_exists_yet() {
        // First run of the updater — there's no `.old`.
        let tmp = tempfile::tempdir().unwrap();
        let l = layout(tmp.path());
        fs::write(l.current(), b"v1").unwrap();
        write_staged(&l, b"v2").unwrap();
        replace_with_staged(&l).unwrap();
        assert_eq!(fs::read(l.current()).unwrap(), b"v2");
        assert!(l.previous().exists());
    }

    #[test]
    fn replace_when_no_current_creates_it() {
        // Brand-new install — no current binary, just stage and rename.
        let tmp = tempfile::tempdir().unwrap();
        let l = layout(tmp.path());
        write_staged(&l, b"v1").unwrap();
        replace_with_staged(&l).unwrap();
        assert_eq!(fs::read(l.current()).unwrap(), b"v1");
        assert!(!l.previous().exists());
    }

    #[test]
    fn rollback_swaps_old_back_in() {
        let tmp = tempfile::tempdir().unwrap();
        let l = layout(tmp.path());
        fs::write(l.current(), b"v1").unwrap();
        write_staged(&l, b"v2").unwrap();
        replace_with_staged(&l).unwrap();
        rollback(&l).unwrap();
        assert_eq!(fs::read(l.current()).unwrap(), b"v1");
        assert!(!l.previous().exists());
    }

    #[test]
    fn rollback_errors_without_old() {
        let tmp = tempfile::tempdir().unwrap();
        let l = layout(tmp.path());
        fs::write(l.current(), b"v1").unwrap();
        let err = rollback(&l).unwrap_err();
        assert!(matches!(err, BinaryError::OldMissing(_)));
    }

    #[test]
    fn write_staged_errors_on_missing_install_dir() {
        let l = InstallLayout::new(PathBuf::from("/nonexistent/iogrid/path"), "iogridd");
        let err = write_staged(&l, b"v1").unwrap_err();
        assert!(matches!(err, BinaryError::InstallDirMissing(_)));
    }
}
