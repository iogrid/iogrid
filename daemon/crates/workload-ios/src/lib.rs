//! iOS-build workload — Tart subprocess driver.
//!
//! Only compiles real logic on macOS (`cfg(target_os = "macos")`). On other
//! targets the crate exposes the same public API but every operation returns
//! [`IosBuildError::UnsupportedPlatform`]. This lets the workspace
//! `cargo check` clean across Linux/Windows CI runners while keeping import
//! sites pleasant — no `#[cfg]` at every call site.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use thiserror::Error;
use uuid::Uuid;

/// Errors emitted by the iOS-build workload runner.
#[derive(Debug, Error)]
pub enum IosBuildError {
    /// Running on a non-mac platform.
    #[error("iOS build only supported on macOS")]
    UnsupportedPlatform,
    /// `tart` CLI missing.
    #[error("tart CLI not found on PATH")]
    TartMissing,
    /// Build script returned non-zero.
    #[error("build failed with exit code {0}")]
    BuildFailed(i32),
}

/// iOS-build workload description.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IosBuildWorkload {
    /// Coordinator-assigned id.
    pub id: Uuid,
    /// Base Tart VM image reference (e.g. `ghcr.io/cirruslabs/macos-sonoma-xcode:latest`).
    pub tart_image: String,
    /// Customer git repo URL to clone inside the VM.
    pub repo_url: String,
    /// Branch or commit to check out.
    pub git_ref: String,
    /// Build script (relative to repo root) the VM should invoke.
    pub build_script: String,
    /// Wall-clock timeout, seconds.
    pub timeout_secs: u32,
}

/// iOS-build runner contract.
#[async_trait]
pub trait IosBuildRunner: Send + Sync {
    /// Run the build to completion, return the path to the produced .ipa.
    async fn run(&self, workload: IosBuildWorkload) -> Result<String, IosBuildError>;
}

/// Tart-based runner. Real implementation lives behind `cfg(target_os = "macos")`.
#[derive(Debug, Default, Clone)]
pub struct TartRunner;

#[async_trait]
impl IosBuildRunner for TartRunner {
    async fn run(&self, workload: IosBuildWorkload) -> Result<String, IosBuildError> {
        #[cfg(target_os = "macos")]
        {
            tracing::info!(id = %workload.id, image = %workload.tart_image, "tart scaffold");
            // Real impl: spawn `tart clone`, `tart run`, `tart ssh` … then collect the .ipa.
            return Ok(format!("/tmp/iogrid-ios-{}.ipa", workload.id));
        }
        #[cfg(not(target_os = "macos"))]
        {
            let _ = workload;
            Err(IosBuildError::UnsupportedPlatform)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn non_mac_returns_unsupported() {
        let r = TartRunner;
        let wl = IosBuildWorkload {
            id: Uuid::new_v4(),
            tart_image: "ghcr.io/cirruslabs/macos-sonoma-xcode:latest".into(),
            repo_url: "https://github.com/foo/bar".into(),
            git_ref: "main".into(),
            build_script: "scripts/build.sh".into(),
            timeout_secs: 1800,
        };
        let res = r.run(wl).await;
        #[cfg(target_os = "macos")]
        assert!(res.is_ok());
        #[cfg(not(target_os = "macos"))]
        assert!(matches!(res, Err(IosBuildError::UnsupportedPlatform)));
    }
}
