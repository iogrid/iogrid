//! Automatic iOS-build toolchain provisioning (#700).
//!
//! `auto_runner()` silently falls back to the native host-direct runner when
//! `tart` isn't on PATH — which is exactly how a fresh provider Mac ends up
//! building against whatever Xcode the host happens to have (the version
//! mismatch). This module makes provisioning AUTOMATIC: before an iOS build
//! runs, the daemon checks whether the Tart toolchain (the CLI + the pinned
//! Xcode image) is ready and, if not, invokes `provision-mac-provider.sh`
//! (which installs Tart + pulls the image). The host never installs Xcode by
//! hand.
//!
//! The provisioning *invocation* needs a real Apple-Silicon Mac to run; the
//! DECISION logic ([`decide`]) is pure + unit-tested so the wiring is correct
//! regardless.

use std::path::Path;
use std::process::Command;

/// Whether the iOS-build toolchain is ready, and if not, what's missing.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ToolchainStatus {
    /// Tart present + the requested Xcode image cached — good to build.
    Ready,
    /// Not an Apple-Silicon macOS host — can't run Tart VMs at all.
    NotSupported,
    /// Tart CLI not installed.
    TartMissing,
    /// Tart present but the requested Xcode image isn't cached yet.
    ImageMissing,
}

/// Pure decision from the three observable facts. Unit-tested; the real
/// [`toolchain_status`] just feeds it live checks.
pub fn decide(
    is_apple_silicon_macos: bool,
    tart_present: bool,
    image_cached: bool,
) -> ToolchainStatus {
    if !is_apple_silicon_macos {
        return ToolchainStatus::NotSupported;
    }
    if !tart_present {
        return ToolchainStatus::TartMissing;
    }
    if !image_cached {
        return ToolchainStatus::ImageMissing;
    }
    ToolchainStatus::Ready
}

/// Live toolchain status for a given Xcode image ref (e.g.
/// `ghcr.io/cirruslabs/macos-sequoia-xcode:16.4`).
pub fn toolchain_status(xcode_image: &str) -> ToolchainStatus {
    decide(
        is_apple_silicon_macos(),
        tart_present(),
        image_cached(xcode_image),
    )
}

/// Errors from the provisioning attempt.
#[derive(Debug, thiserror::Error)]
pub enum ProvisionError {
    /// Host can't run Tart VMs (Intel / non-macOS).
    #[error("host is not an Apple-Silicon Mac — iOS-build provisioning unsupported")]
    NotSupported,
    /// The provisioning script wasn't found where expected.
    #[error("provisioning script not found at {0}")]
    ScriptMissing(String),
    /// The provisioning script exited non-zero.
    #[error("provisioning script failed (exit {0})")]
    ScriptFailed(i32),
    /// Couldn't launch the script.
    #[error("provisioning script could not be launched: {0}")]
    Spawn(#[source] std::io::Error),
}

/// Ensure the Tart toolchain is provisioned for `xcode_image`. No-op when
/// already [`ToolchainStatus::Ready`]. Otherwise runs `script_path` (the
/// shipped `provision-mac-provider.sh`) which installs Tart + pulls the image.
/// Heavy (one-time ~35-80 GB pull) — callers should run it off the hot path.
pub fn ensure_provisioned(
    xcode_image: &str,
    xcode_version: &str,
    script_path: &Path,
) -> Result<(), ProvisionError> {
    match toolchain_status(xcode_image) {
        ToolchainStatus::Ready => Ok(()),
        ToolchainStatus::NotSupported => Err(ProvisionError::NotSupported),
        ToolchainStatus::TartMissing | ToolchainStatus::ImageMissing => {
            if !script_path.exists() {
                return Err(ProvisionError::ScriptMissing(
                    script_path.display().to_string(),
                ));
            }
            tracing::info!(
                image = %xcode_image,
                "iOS-build toolchain not ready — running provision-mac-provider.sh (one-time)"
            );
            let status = Command::new("bash")
                .arg(script_path)
                .arg("--xcode")
                .arg(xcode_version)
                .status()
                .map_err(ProvisionError::Spawn)?;
            if status.success() {
                Ok(())
            } else {
                Err(ProvisionError::ScriptFailed(status.code().unwrap_or(-1)))
            }
        }
    }
}

fn is_apple_silicon_macos() -> bool {
    cfg!(all(target_os = "macos", target_arch = "aarch64"))
}

fn tart_present() -> bool {
    Command::new("tart")
        .arg("--version")
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false)
}

fn image_cached(xcode_image: &str) -> bool {
    // `tart list` prints one VM/image ref per line; the source image appears
    // when it has been pulled.
    Command::new("tart")
        .arg("list")
        .output()
        .map(|o| {
            String::from_utf8_lossy(&o.stdout)
                .lines()
                .any(|l| l.split_whitespace().any(|t| t == xcode_image))
        })
        .unwrap_or(false)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    #[test]
    fn decide_covers_every_case() {
        assert_eq!(decide(false, false, false), ToolchainStatus::NotSupported);
        assert_eq!(decide(false, true, true), ToolchainStatus::NotSupported);
        assert_eq!(decide(true, false, false), ToolchainStatus::TartMissing);
        assert_eq!(decide(true, true, false), ToolchainStatus::ImageMissing);
        assert_eq!(decide(true, true, true), ToolchainStatus::Ready);
    }

    #[test]
    fn ensure_provisioned_missing_script_is_a_clear_error() {
        // On a non-Apple-Silicon CI host, status is NotSupported → that error.
        // On a Mac without the script, ScriptMissing. Either way it must NOT
        // silently succeed or panic.
        let res = ensure_provisioned(
            "ghcr.io/cirruslabs/macos-sequoia-xcode:16.4",
            "16.4",
            &PathBuf::from("/nonexistent/provision-mac-provider.sh"),
        );
        assert!(res.is_err(), "must surface a clear error, got {res:?}");
    }
}
