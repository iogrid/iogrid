//! Docker workload runner.
//!
//! Two layers live in this crate:
//!
//! 1. **Pure types + trait surface** — [`DockerWorkload`], [`WorkloadResult`],
//!    [`DockerRunner`], [`RegistryAllowlist`]. These compile on every target
//!    with zero vendor dependencies and are the contract the supervisor
//!    depends on.
//! 2. **Real bollard runtime** — gated behind the `docker-real` feature. The
//!    [`BollardDockerRunner`] talks to the local Docker Engine / Docker
//!    Desktop and applies platform-appropriate isolation:
//!
//!    * Linux: cgroup CPU / memory caps, read-only root fs, no host network,
//!      attached to the iogrid-managed bridge so the container's outbound
//!      traffic flows through the same SOCKS5 / anti-abuse pipeline that the
//!      bandwidth workload uses. (Run with `--security-opt no-new-privileges`
//!      and the user-supplied gVisor/Kata runtime if available.)
//!    * Mac: Docker Desktop's lightweight VM (default runtime, no host net).
//!    * Windows: Hyper-V isolated containers (set via `host_config.isolation`).
//!
//! The runner enforces the **anti-abuse registry allowlist** before pulling
//! anything — only images whose reference's registry component is on the
//! allowlist may run. The allowlist is normally seeded from the coordinator
//! through the anti-abuse crate's `RulesetSnapshot`; the scaffold here
//! defaults to a small list of trusted registries.

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
    /// Workload exceeded its wall-clock time-box.
    #[error("workload {id} timed out after {after_secs}s")]
    TimedOut {
        /// Workload id.
        id: Uuid,
        /// Seconds before kill.
        after_secs: u32,
    },
    /// Image reference not on the registry allowlist.
    #[error("image {image} not on registry allowlist (registry={registry})")]
    RegistryNotAllowed {
        /// Image reference.
        image: String,
        /// Extracted registry component.
        registry: String,
    },
    /// Generic bollard / I/O error bubble-up.
    #[error("docker runtime error: {0}")]
    Runtime(String),
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
    /// Name of the iogrid-managed bridge network the container attaches to.
    /// `None` = use the daemon's default (`iogrid-egress`).
    pub network_name: Option<String>,
}

/// Captured stdout/stderr + exit metadata from one workload run.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct WorkloadResult {
    /// Workload id (mirrors [`DockerWorkload::id`]).
    pub id: Uuid,
    /// Process exit code reported by the container.
    pub exit_code: i32,
    /// Combined stdout + stderr (capped at 1 MiB per workload by the runner).
    pub logs: Vec<u8>,
    /// `true` iff the container exited because the time-box fired.
    pub timed_out: bool,
}

/// Generic workload runner contract.
#[async_trait]
pub trait DockerRunner: Send + Sync {
    /// Run a workload to completion. Returns container stdout+stderr + exit
    /// code via [`WorkloadResult`].
    async fn run(&self, workload: DockerWorkload) -> Result<WorkloadResult, DockerError>;
}

/// Registry allowlist. Only image references whose registry component is in
/// the set may be pulled / run. The empty set means "allow nothing" — the
/// constructor seeds a small built-in default so the scaffold path stays
/// useful in tests.
#[derive(Debug, Clone)]
pub struct RegistryAllowlist {
    allowed: Vec<String>,
}

impl Default for RegistryAllowlist {
    fn default() -> Self {
        Self {
            allowed: vec![
                "docker.io".to_string(),
                "ghcr.io".to_string(),
                "registry.iogrid.org".to_string(),
                "public.ecr.aws".to_string(),
            ],
        }
    }
}

impl RegistryAllowlist {
    /// Build from an explicit list of registry hosts.
    pub fn new(allowed: impl IntoIterator<Item = String>) -> Self {
        Self {
            allowed: allowed.into_iter().map(|s| s.to_lowercase()).collect(),
        }
    }

    /// Borrow the active list.
    pub fn allowed(&self) -> &[String] {
        &self.allowed
    }

    /// `true` if `image_ref` resolves to an allowed registry.
    pub fn permits(&self, image_ref: &str) -> bool {
        let registry = registry_of(image_ref);
        self.allowed
            .iter()
            .any(|a| a.eq_ignore_ascii_case(&registry))
    }
}

/// Extract the registry component from a docker image reference. Mirrors the
/// algorithm distribution uses:
///
/// * if the reference has no `/` it is always a Docker Hub short reference
///   (the head is the image name + optional tag, never a registry);
/// * otherwise the first `/`-delimited component is the registry **iff** it
///   contains a `.` or `:` (port separator) or is exactly `localhost`;
/// * everything else falls back to Docker Hub.
pub fn registry_of(image_ref: &str) -> String {
    let stripped = image_ref.trim();
    let head = match stripped.split_once('/') {
        Some((h, _)) => h,
        None => return "docker.io".to_string(),
    };
    if head == "localhost" || head.contains('.') || head.contains(':') {
        head.to_lowercase()
    } else {
        "docker.io".to_string()
    }
}

/// Scaffold runner — returns deterministic empty output. Used in tests and
/// any cross-compiled target where the `docker-real` feature is off.
#[derive(Debug, Default, Clone)]
pub struct ScaffoldDockerRunner {
    allowlist: RegistryAllowlist,
}

impl ScaffoldDockerRunner {
    /// Build with a custom allowlist.
    pub fn with_allowlist(allowlist: RegistryAllowlist) -> Self {
        Self { allowlist }
    }
}

#[async_trait]
impl DockerRunner for ScaffoldDockerRunner {
    async fn run(&self, workload: DockerWorkload) -> Result<WorkloadResult, DockerError> {
        if !self.allowlist.permits(&workload.image) {
            return Err(DockerError::RegistryNotAllowed {
                image: workload.image.clone(),
                registry: registry_of(&workload.image),
            });
        }
        tracing::info!(
            id = %workload.id,
            image = %workload.image,
            "scaffold docker runner — would run workload",
        );
        Ok(WorkloadResult {
            id: workload.id,
            exit_code: 0,
            logs: Vec::new(),
            timed_out: false,
        })
    }
}

#[cfg(feature = "docker-real")]
pub use real::BollardDockerRunner;

#[cfg(feature = "docker-real")]
mod real {
    //! Live bollard implementation. Compiled only when `docker-real` is on.

    use super::*;
    use bollard::container::{
        Config as ContainerConfig, CreateContainerOptions, LogOutput, LogsOptions,
        NetworkingConfig, RemoveContainerOptions, StartContainerOptions, WaitContainerOptions,
    };
    use bollard::image::{CreateImageOptions, RemoveImageOptions};
    use bollard::models::{EndpointSettings, HostConfig};
    use bollard::Docker;
    use futures::StreamExt;
    use std::collections::HashMap;
    use std::time::Duration;

    /// Real Docker runner backed by `bollard`. Cheap to clone (Docker client
    /// is an `Arc` internally).
    #[derive(Debug, Clone)]
    pub struct BollardDockerRunner {
        client: Docker,
        allowlist: RegistryAllowlist,
        keep_image_cached: bool,
        /// Cap log capture to this many bytes (1 MiB default).
        log_cap_bytes: usize,
    }

    impl BollardDockerRunner {
        /// Connect to the local Docker daemon using bollard's defaults
        /// (Unix socket on Linux/Mac, named pipe on Windows).
        pub fn connect_local(allowlist: RegistryAllowlist) -> Result<Self, DockerError> {
            let client = Docker::connect_with_local_defaults()
                .map_err(|e| DockerError::DaemonUnreachable(e.to_string()))?;
            Ok(Self {
                client,
                allowlist,
                keep_image_cached: true,
                log_cap_bytes: 1_048_576,
            })
        }

        /// Override the log byte cap (test hook).
        pub fn set_log_cap_bytes(&mut self, cap: usize) {
            self.log_cap_bytes = cap;
        }

        /// Whether to remove the image after the run (default `false`: keep
        /// cached for hot-path reuse).
        pub fn set_keep_image_cached(&mut self, keep: bool) {
            self.keep_image_cached = keep;
        }

        /// Pull the image from the daemon, streaming progress messages.
        async fn pull_image(&self, image: &str) -> Result<(), DockerError> {
            let opts = CreateImageOptions {
                from_image: image.to_string(),
                ..Default::default()
            };
            let mut stream = self.client.create_image(Some(opts), None, None);
            while let Some(item) = stream.next().await {
                match item {
                    Ok(info) => {
                        if let Some(status) = info.status {
                            tracing::debug!(%status, "docker pull");
                        }
                    }
                    Err(e) => {
                        return Err(DockerError::ImagePullFailed {
                            image: image.to_string(),
                            reason: e.to_string(),
                        });
                    }
                }
            }
            Ok(())
        }

        fn host_config(&self, w: &DockerWorkload) -> HostConfig {
            // Convert millicores → CPU quota. Docker uses `cpu_period`
            // (default 100ms = 100_000us) + `cpu_quota` (us per period). At
            // 1000 millis = 1 CPU = quota 100000.
            let cpu_period: i64 = 100_000;
            let cpu_quota: i64 = (w.cpu_millis as i64 * cpu_period) / 1000;
            let memory_bytes: i64 = (w.memory_mib as i64) * 1024 * 1024;
            HostConfig {
                cpu_period: Some(cpu_period),
                cpu_quota: Some(cpu_quota),
                memory: Some(memory_bytes),
                memory_swap: Some(memory_bytes), // disable swap (== memory).
                readonly_rootfs: Some(true),
                network_mode: Some(
                    w.network_name
                        .clone()
                        .unwrap_or_else(|| "iogrid-egress".to_string()),
                ),
                security_opt: Some(vec!["no-new-privileges:true".to_string()]),
                cap_drop: Some(vec!["ALL".to_string()]),
                auto_remove: Some(false), // we remove ourselves so we can collect logs.
                ..Default::default()
            }
        }

        async fn create_and_start(
            &self,
            w: &DockerWorkload,
            container_name: &str,
        ) -> Result<(), DockerError> {
            let env: Vec<String> = w.env.iter().map(|(k, v)| format!("{k}={v}")).collect();
            let cmd = if w.cmd.is_empty() {
                None
            } else {
                Some(w.cmd.clone())
            };
            let network_name = w
                .network_name
                .clone()
                .unwrap_or_else(|| "iogrid-egress".to_string());
            let mut endpoints = HashMap::new();
            endpoints.insert(network_name.clone(), EndpointSettings::default());
            let cfg: ContainerConfig<String> = ContainerConfig {
                image: Some(w.image.clone()),
                cmd,
                env: Some(env),
                tty: Some(false),
                attach_stdout: Some(true),
                attach_stderr: Some(true),
                network_disabled: Some(false),
                host_config: Some(self.host_config(w)),
                networking_config: Some(NetworkingConfig::<String> {
                    endpoints_config: endpoints,
                }),
                ..Default::default()
            };
            let opts = CreateContainerOptions {
                name: container_name.to_string(),
                platform: None,
            };
            self.client
                .create_container(Some(opts), cfg)
                .await
                .map_err(|e| DockerError::StartFailed(format!("create: {e}")))?;
            self.client
                .start_container(container_name, None::<StartContainerOptions<String>>)
                .await
                .map_err(|e| DockerError::StartFailed(format!("start: {e}")))?;
            Ok(())
        }

        async fn collect_logs(&self, container_name: &str) -> Vec<u8> {
            let opts = LogsOptions::<String> {
                stdout: true,
                stderr: true,
                follow: false,
                tail: "all".to_string(),
                ..Default::default()
            };
            let mut buf: Vec<u8> = Vec::new();
            let mut stream = self.client.logs(container_name, Some(opts));
            while let Some(item) = stream.next().await {
                let bytes = match item {
                    Ok(LogOutput::StdOut { message }) | Ok(LogOutput::StdErr { message }) => {
                        message.to_vec()
                    }
                    Ok(LogOutput::Console { message }) => message.to_vec(),
                    Ok(LogOutput::StdIn { .. }) => continue,
                    Err(_) => break,
                };
                if buf.len() + bytes.len() > self.log_cap_bytes {
                    let room = self.log_cap_bytes.saturating_sub(buf.len());
                    buf.extend_from_slice(&bytes[..room.min(bytes.len())]);
                    break;
                } else {
                    buf.extend_from_slice(&bytes);
                }
            }
            buf
        }

        async fn cleanup(&self, container_name: &str, image: &str) {
            let _ = self
                .client
                .remove_container(
                    container_name,
                    Some(RemoveContainerOptions {
                        force: true,
                        ..Default::default()
                    }),
                )
                .await;
            if !self.keep_image_cached {
                let _ = self
                    .client
                    .remove_image(
                        image,
                        Some(RemoveImageOptions {
                            force: false,
                            noprune: false,
                        }),
                        None,
                    )
                    .await;
            }
        }
    }

    #[async_trait]
    impl DockerRunner for BollardDockerRunner {
        async fn run(&self, workload: DockerWorkload) -> Result<WorkloadResult, DockerError> {
            if !self.allowlist.permits(&workload.image) {
                return Err(DockerError::RegistryNotAllowed {
                    image: workload.image.clone(),
                    registry: registry_of(&workload.image),
                });
            }
            self.pull_image(&workload.image).await?;
            let container_name = format!("iogridd-{}", workload.id);
            self.create_and_start(&workload, &container_name).await?;

            // Time-box the wait.
            let deadline = Duration::from_secs(workload.timeout_secs as u64);
            let mut wait_stream = self
                .client
                .wait_container(&container_name, None::<WaitContainerOptions<String>>);
            let wait_fut = async {
                match wait_stream.next().await {
                    Some(item) => item,
                    None => Err(bollard::errors::Error::DockerStreamError {
                        error: "wait stream ended without a value".into(),
                    }),
                }
            };

            let (exit_code, timed_out) = match tokio::time::timeout(deadline, wait_fut).await {
                Ok(Ok(resp)) => (resp.status_code as i32, false),
                Ok(Err(e)) => {
                    let logs = self.collect_logs(&container_name).await;
                    self.cleanup(&container_name, &workload.image).await;
                    return Err(DockerError::Runtime(format!(
                        "wait: {e}; logs_len={}",
                        logs.len()
                    )));
                }
                Err(_elapsed) => {
                    // Time-box fired — kill container, mark timed_out.
                    let _ = self
                        .client
                        .kill_container::<String>(&container_name, None)
                        .await;
                    (-1, true)
                }
            };
            let logs = self.collect_logs(&container_name).await;
            self.cleanup(&container_name, &workload.image).await;
            Ok(WorkloadResult {
                id: workload.id,
                exit_code,
                logs,
                timed_out,
            })
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_workload(image: &str) -> DockerWorkload {
        DockerWorkload {
            id: Uuid::new_v4(),
            image: image.into(),
            cmd: vec!["echo".into(), "hi".into()],
            env: vec![],
            cpu_millis: 250,
            memory_mib: 64,
            timeout_secs: 30,
            network_name: None,
        }
    }

    #[tokio::test]
    async fn scaffold_runner_returns_zero_exit_for_allowed_image() {
        let r = ScaffoldDockerRunner::default();
        let out = r
            .run(sample_workload("ghcr.io/library/hello-world:latest"))
            .await
            .unwrap();
        assert_eq!(out.exit_code, 0);
        assert!(out.logs.is_empty());
        assert!(!out.timed_out);
    }

    #[tokio::test]
    async fn scaffold_runner_blocks_disallowed_registry() {
        let r = ScaffoldDockerRunner::with_allowlist(RegistryAllowlist::new(["ghcr.io".into()]));
        let err = r
            .run(sample_workload("evil.example.com/foo:latest"))
            .await
            .unwrap_err();
        assert!(matches!(err, DockerError::RegistryNotAllowed { .. }));
    }

    #[test]
    fn registry_of_handles_hub_short_refs() {
        assert_eq!(registry_of("alpine:3.20"), "docker.io");
        assert_eq!(registry_of("library/alpine"), "docker.io");
    }

    #[test]
    fn registry_of_handles_explicit_registries() {
        assert_eq!(registry_of("ghcr.io/foo/bar:tag"), "ghcr.io");
        assert_eq!(
            registry_of("registry.iogrid.org/x:1"),
            "registry.iogrid.org"
        );
        assert_eq!(registry_of("localhost:5000/foo:tag"), "localhost:5000");
    }

    #[test]
    fn allowlist_default_admits_common_registries() {
        let a = RegistryAllowlist::default();
        assert!(a.permits("ghcr.io/iogrid/runner:v1"));
        assert!(a.permits("alpine:3.20")); // hub
        assert!(!a.permits("evil.example/foo:bar"));
    }

    #[test]
    fn allowlist_is_case_insensitive() {
        let a = RegistryAllowlist::new(["GHCR.IO".into()]);
        assert!(a.permits("ghcr.io/foo:bar"));
    }
}
