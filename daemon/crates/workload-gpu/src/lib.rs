//! GPU workload runner.
//!
//! Backend selection is `cfg`-gated:
//!
//! * `cfg(target_os = "linux")` / `cfg(target_os = "windows")` → NVML via
//!   `nvml-wrapper` (behind the `gpu-real` feature).
//! * `cfg(target_os = "macos")` → MLX / Metal via `objc2` (behind the
//!   `gpu-real` feature).
//!
//! The scaffold here provides a `GpuRunner` trait + a no-op implementation
//! that compiles on every target without pulling vendor bindings.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use thiserror::Error;
use uuid::Uuid;

/// GPU workload errors.
#[derive(Debug, Error)]
pub enum GpuError {
    /// No GPU detected at the requested index.
    #[error("no GPU at index {0}")]
    NoSuchDevice(u32),
    /// vRAM allocation refused.
    #[error(
        "vRAM allocation refused: requested {requested_mib} MiB, available {available_mib} MiB"
    )]
    OutOfMemory {
        /// MiB requested.
        requested_mib: u64,
        /// MiB available.
        available_mib: u64,
    },
    /// Underlying vendor SDK error.
    #[error("vendor SDK error: {0}")]
    VendorError(String),
}

/// Describes a GPU job.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GpuWorkload {
    /// Coordinator-assigned id.
    pub id: Uuid,
    /// Container image (the GPU runner still goes through Docker for sandboxing).
    pub image: String,
    /// Per-device vRAM limit in MiB.
    pub vram_mib: u64,
    /// Wall-clock timeout, seconds.
    pub timeout_secs: u32,
}

/// GPU runner contract.
#[async_trait]
pub trait GpuRunner: Send + Sync {
    /// Run a GPU workload to completion.
    async fn run(&self, workload: GpuWorkload) -> Result<Vec<u8>, GpuError>;

    /// Report which backend is active (`"nvml"`, `"mlx"`, `"noop"`).
    fn backend(&self) -> &'static str;
}

/// No-op runner. Available on every platform.
#[derive(Debug, Default, Clone)]
pub struct NoopGpuRunner;

#[async_trait]
impl GpuRunner for NoopGpuRunner {
    async fn run(&self, workload: GpuWorkload) -> Result<Vec<u8>, GpuError> {
        tracing::info!(id = %workload.id, "noop gpu runner — scaffold");
        Ok(Vec::new())
    }
    fn backend(&self) -> &'static str {
        "noop"
    }
}

/// Pick the platform-appropriate backend at compile time.
///
/// Scaffold always returns `NoopGpuRunner`. When `gpu-real` is enabled the
/// `cfg` branches below select the real backend.
pub fn default_runner() -> NoopGpuRunner {
    #[cfg(all(feature = "gpu-real", any(target_os = "linux", target_os = "windows")))]
    {
        // Real impl: return an NvmlGpuRunner wired against nvml-wrapper.
    }
    #[cfg(all(feature = "gpu-real", target_os = "macos"))]
    {
        // Real impl: return an MlxGpuRunner wired against objc2 + Metal.
    }
    NoopGpuRunner
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn noop_runner_works() {
        let r = default_runner();
        assert_eq!(r.backend(), "noop");
        let out = r
            .run(GpuWorkload {
                id: Uuid::new_v4(),
                image: "ghcr.io/foo/bar:latest".into(),
                vram_mib: 4096,
                timeout_secs: 600,
            })
            .await
            .unwrap();
        assert!(out.is_empty());
    }
}
