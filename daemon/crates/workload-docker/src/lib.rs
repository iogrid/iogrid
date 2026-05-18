//! Docker workload runner.
//!
//! Production impl uses `bollard` (gated behind the `docker-real` feature) to
//! talk to the local Docker daemon (Docker Desktop on Mac/Win, Docker Engine on
//! Linux) and applies platform-appropriate isolation:
//!
//! * Linux: gVisor or Kata Containers runtime
//! * Windows: Hyper-V isolated containers
//! * Mac: Docker Desktop's lightweight VM (default)

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use thiserror::Error;
use uuid::Uuid;

/// Docker workload errors.
#[derive(Debug, Error)]
pub enum DockerError {
    /// Docker daemon not reachable.
    #[error("docker daemon unreachable: {0}")]
    DaemonUnreachable(String),
    /// Image pull failed.
    #[error("image pull failed for {image}: {reason}")]
    ImagePullFailed {
        /// Image reference that failed to pull.
        image: String,
        /// Reason returned by Docker.
        reason: String,
    },
    /// Container failed to start.
    #[error("container start failed: {0}")]
    StartFailed(String),
}

/// One docker workload submission.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DockerWorkload {
    /// Globally unique workload id assigned by the coordinator.
    pub id: Uuid,
    /// Fully-qualified image reference, e.g. `ghcr.io/foo/bar:sha256-...`.
    pub image: String,
    /// Container command override.
    pub cmd: Vec<String>,
    /// Environment variables.
    pub env: Vec<(String, String)>,
    /// CPU quota in millicores.
    pub cpu_millis: u32,
    /// Memory limit in MiB.
    pub memory_mib: u32,
    /// Wall-clock timeout, seconds.
    pub timeout_secs: u32,
}

/// Generic workload runner contract.
#[async_trait]
pub trait DockerRunner: Send + Sync {
    /// Run a workload to completion. Returns container stdout+stderr.
    async fn run(&self, workload: DockerWorkload) -> Result<Vec<u8>, DockerError>;
}

/// Scaffold runner — returns deterministic empty output.
#[derive(Debug, Default, Clone)]
pub struct ScaffoldDockerRunner;

#[async_trait]
impl DockerRunner for ScaffoldDockerRunner {
    async fn run(&self, workload: DockerWorkload) -> Result<Vec<u8>, DockerError> {
        tracing::info!(
            id = %workload.id,
            image = %workload.image,
            "scaffold docker runner — would run workload",
        );
        Ok(Vec::new())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn scaffold_runner_returns_empty() {
        let r = ScaffoldDockerRunner;
        let out = r
            .run(DockerWorkload {
                id: Uuid::new_v4(),
                image: "alpine:3.20".into(),
                cmd: vec!["echo".into(), "hi".into()],
                env: vec![],
                cpu_millis: 250,
                memory_mib: 64,
                timeout_secs: 30,
            })
            .await
            .unwrap();
        assert!(out.is_empty());
    }
}
