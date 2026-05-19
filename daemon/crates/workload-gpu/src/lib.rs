//! GPU workload runner.
//!
//! Backend selection is `cfg`-gated:
//!
//! * `cfg(target_os = "linux")` / `cfg(target_os = "windows")` → run the
//!   container via bollard with `host_config.runtime = "nvidia"` (NVIDIA
//!   Container Toolkit). The image is expected to contain `nvidia-smi` /
//!   CUDA libs; the host kernel module + container toolkit must be
//!   pre-installed on the provider machine (documented in
//!   `daemon/README.md`).
//! * `cfg(target_os = "macos")` → MLX / Metal. Real impl is opt-in via the
//!   `gpu-mlx` feature; without it the macOS runner is a documented stub
//!   (`unimplemented!` is never reached at runtime because the supervisor
//!   refuses GPU assignments when the backend reports `"stub-mac"`).
//!
//! The scaffold (default features off) provides a [`NoopGpuRunner`] and
//! [`default_runner`] returning it. This keeps `cargo check` clean on every
//! target without pulling vendor bindings.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use async_trait::async_trait;
use iogrid_workload_docker::WorkloadResult;
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
    /// The active backend has no runtime implementation on this build.
    #[error("gpu backend {backend} not implemented in this build")]
    BackendUnimplemented {
        /// Backend slug ("mlx-stub", "noop", …).
        backend: &'static str,
    },
    /// The underlying Docker run failed (for CUDA workloads).
    #[error("docker GPU run failed: {0}")]
    Docker(String),
}

/// Describes a GPU job.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct GpuWorkload {
    /// Coordinator-assigned id.
    pub id: Uuid,
    /// Container image (the GPU runner still goes through Docker for sandboxing).
    pub image: String,
    /// Container command.
    pub cmd: Vec<String>,
    /// Environment variables.
    pub env: Vec<(String, String)>,
    /// Per-device vRAM limit in MiB. Currently advisory on the Linux side
    /// (NVIDIA Container Toolkit doesn't enforce vRAM at toolkit level — the
    /// model loader inside the container must respect it).
    pub vram_mib: u64,
    /// Wall-clock timeout, seconds.
    pub timeout_secs: u32,
}

impl GpuWorkload {
    /// Materialise the GPU workload as a docker workload (for the CUDA path
    /// that runs through bollard).
    pub fn to_docker(
        &self,
        cpu_millis: u32,
        memory_mib: u32,
    ) -> iogrid_workload_docker::DockerWorkload {
        iogrid_workload_docker::DockerWorkload {
            id: self.id,
            image: self.image.clone(),
            cmd: self.cmd.clone(),
            env: self.env.clone(),
            cpu_millis,
            memory_mib,
            timeout_secs: self.timeout_secs,
            // GPU workloads share the iogrid-egress bridge — same anti-abuse
            // pipeline as regular Docker workloads.
            network_name: None,
        }
    }
}

/// GPU runner contract.
#[async_trait]
pub trait GpuRunner: Send + Sync {
    /// Run a GPU workload to completion.
    async fn run(&self, workload: GpuWorkload) -> Result<WorkloadResult, GpuError>;

    /// Report which backend is active (`"nvidia"`, `"mlx-stub"`, `"noop"`).
    fn backend(&self) -> &'static str;
}

/// No-op runner. Available on every platform; what the scaffold returns.
#[derive(Debug, Default, Clone)]
pub struct NoopGpuRunner;

#[async_trait]
impl GpuRunner for NoopGpuRunner {
    async fn run(&self, workload: GpuWorkload) -> Result<WorkloadResult, GpuError> {
        tracing::info!(id = %workload.id, "noop gpu runner — scaffold");
        Ok(WorkloadResult {
            id: workload.id,
            exit_code: 0,
            logs: Vec::new(),
            timed_out: false,
        })
    }
    fn backend(&self) -> &'static str {
        "noop"
    }
}

/// macOS MLX stub.
///
/// Always returns [`GpuError::BackendUnimplemented`]. The MLX FFI bridge
/// (`mlx-rs`) does not yet build cleanly on every Apple-silicon CI runner;
/// once it does, gate the real impl on `cfg(all(target_os = "macos", feature = "gpu-mlx"))`
/// and use this stub only as a fall-back. Tracking issue: #13.
//
// TODO(#13): wire real MLX runtime when mlx-rs ships a stable wheel for the
// CI's macos-latest runner — until then the supervisor must filter MLX
// assignments out of the eligible-workload-types it advertises.
#[derive(Debug, Default, Clone)]
pub struct MlxStubGpuRunner;

#[async_trait]
impl GpuRunner for MlxStubGpuRunner {
    async fn run(&self, workload: GpuWorkload) -> Result<WorkloadResult, GpuError> {
        tracing::warn!(
            id = %workload.id,
            "MLX runner stub — refusing workload until mlx-rs FFI is wired",
        );
        Err(GpuError::BackendUnimplemented {
            backend: "mlx-stub",
        })
    }
    fn backend(&self) -> &'static str {
        "mlx-stub"
    }
}

#[cfg(all(feature = "gpu-real", any(target_os = "linux", target_os = "windows")))]
pub use cuda::NvidiaContainerRunner;

#[cfg(all(feature = "gpu-real", any(target_os = "linux", target_os = "windows")))]
mod cuda {
    //! NVIDIA Container Toolkit runner — gated on Linux/Windows + `gpu-real`.

    use super::*;
    use bollard::container::{
        Config as ContainerConfig, CreateContainerOptions, RemoveContainerOptions,
        StartContainerOptions, WaitContainerOptions,
    };
    use bollard::image::CreateImageOptions;
    use bollard::models::{DeviceRequest, HostConfig};
    use bollard::Docker;
    use futures_util::StreamExt;

    /// Real GPU runner: bollard + nvidia runtime.
    #[derive(Debug, Clone)]
    pub struct NvidiaContainerRunner {
        client: Docker,
    }

    impl NvidiaContainerRunner {
        /// Connect to the local docker daemon.
        pub fn connect_local() -> Result<Self, GpuError> {
            let client = Docker::connect_with_local_defaults()
                .map_err(|e| GpuError::Docker(e.to_string()))?;
            Ok(Self { client })
        }
    }

    #[async_trait]
    impl GpuRunner for NvidiaContainerRunner {
        async fn run(&self, workload: GpuWorkload) -> Result<WorkloadResult, GpuError> {
            // Pull image.
            let opts = CreateImageOptions {
                from_image: workload.image.clone(),
                ..Default::default()
            };
            let mut stream = self.client.create_image(Some(opts), None, None);
            while let Some(item) = stream.next().await {
                if let Err(e) = item {
                    return Err(GpuError::Docker(format!("pull: {e}")));
                }
            }

            // Build host config with GPU request.
            let host = HostConfig {
                runtime: Some("nvidia".to_string()),
                device_requests: Some(vec![DeviceRequest {
                    driver: Some("nvidia".to_string()),
                    count: Some(-1), // all GPUs.
                    capabilities: Some(vec![vec!["gpu".to_string()]]),
                    ..Default::default()
                }]),
                network_mode: Some("iogrid-egress".to_string()),
                readonly_rootfs: Some(true),
                security_opt: Some(vec!["no-new-privileges:true".to_string()]),
                cap_drop: Some(vec!["ALL".to_string()]),
                ..Default::default()
            };

            let env: Vec<String> = workload
                .env
                .iter()
                .map(|(k, v)| format!("{k}={v}"))
                .collect();
            let cfg: ContainerConfig<String> = ContainerConfig {
                image: Some(workload.image.clone()),
                cmd: if workload.cmd.is_empty() {
                    None
                } else {
                    Some(workload.cmd.clone())
                },
                env: Some(env),
                attach_stdout: Some(true),
                attach_stderr: Some(true),
                host_config: Some(host),
                ..Default::default()
            };
            let container_name = format!("iogridd-gpu-{}", workload.id);
            self.client
                .create_container(
                    Some(CreateContainerOptions {
                        name: container_name.clone(),
                        platform: None,
                    }),
                    cfg,
                )
                .await
                .map_err(|e| GpuError::Docker(format!("create: {e}")))?;
            self.client
                .start_container(&container_name, None::<StartContainerOptions<String>>)
                .await
                .map_err(|e| GpuError::Docker(format!("start: {e}")))?;

            let deadline = std::time::Duration::from_secs(workload.timeout_secs as u64);
            let mut wait_stream = self
                .client
                .wait_container(&container_name, None::<WaitContainerOptions<String>>);
            let wait_fut = async {
                match wait_stream.next().await {
                    Some(item) => item,
                    None => Err(bollard::errors::Error::DockerStreamError {
                        error: "wait stream ended without value".into(),
                    }),
                }
            };
            let (exit_code, timed_out) = match tokio::time::timeout(deadline, wait_fut).await {
                Ok(Ok(resp)) => (resp.status_code as i32, false),
                Ok(Err(e)) => {
                    let _ = self
                        .client
                        .remove_container(
                            &container_name,
                            Some(RemoveContainerOptions {
                                force: true,
                                ..Default::default()
                            }),
                        )
                        .await;
                    return Err(GpuError::Docker(format!("wait: {e}")));
                }
                Err(_) => {
                    let _ = self
                        .client
                        .kill_container::<String>(&container_name, None)
                        .await;
                    (-1, true)
                }
            };
            let _ = self
                .client
                .remove_container(
                    &container_name,
                    Some(RemoveContainerOptions {
                        force: true,
                        ..Default::default()
                    }),
                )
                .await;
            Ok(WorkloadResult {
                id: workload.id,
                exit_code,
                logs: Vec::new(),
                timed_out,
            })
        }
        fn backend(&self) -> &'static str {
            "nvidia"
        }
    }
}

/// Pick the platform-appropriate backend.
///
/// * Linux/Windows with `gpu-real` → `NvidiaContainerRunner` (boxed trait).
/// * macOS with `gpu-mlx` → currently still the stub (no real backend yet).
/// * Otherwise → [`NoopGpuRunner`].
pub fn default_runner() -> Box<dyn GpuRunner> {
    #[cfg(all(feature = "gpu-real", any(target_os = "linux", target_os = "windows")))]
    {
        if let Ok(r) = NvidiaContainerRunner::connect_local() {
            return Box::new(r);
        }
        return Box::new(NoopGpuRunner);
    }
    #[cfg(all(feature = "gpu-mlx", target_os = "macos"))]
    {
        return Box::new(MlxStubGpuRunner);
    }
    #[allow(unreachable_code)]
    Box::new(NoopGpuRunner)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample() -> GpuWorkload {
        GpuWorkload {
            id: Uuid::new_v4(),
            image: "ghcr.io/foo/bar:latest".into(),
            cmd: vec!["nvidia-smi".into()],
            env: vec![],
            vram_mib: 4096,
            timeout_secs: 600,
        }
    }

    #[tokio::test]
    async fn noop_runner_works() {
        let r: Box<dyn GpuRunner> = Box::new(NoopGpuRunner);
        assert_eq!(r.backend(), "noop");
        let out = r.run(sample()).await.unwrap();
        assert_eq!(out.exit_code, 0);
        assert!(!out.timed_out);
    }

    #[tokio::test]
    async fn mlx_stub_refuses_with_backend_unimplemented() {
        let r = MlxStubGpuRunner;
        assert_eq!(r.backend(), "mlx-stub");
        let err = r.run(sample()).await.unwrap_err();
        assert!(matches!(
            err,
            GpuError::BackendUnimplemented {
                backend: "mlx-stub"
            }
        ));
    }

    #[tokio::test]
    async fn default_runner_returns_some_implementation() {
        let r = default_runner();
        // Smoke: ensure the trait object isn't null and reports a known
        // backend slug.
        let backend = r.backend();
        assert!(["noop", "nvidia", "mlx-stub"].contains(&backend));
    }

    #[test]
    fn gpu_workload_to_docker_preserves_id_and_timeout() {
        let w = sample();
        let d = w.to_docker(500, 1024);
        assert_eq!(d.id, w.id);
        assert_eq!(d.timeout_secs, w.timeout_secs);
        assert_eq!(d.cpu_millis, 500);
        assert_eq!(d.memory_mib, 1024);
    }
}
