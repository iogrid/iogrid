//! GPU workload runner.
//!
//! Backend selection is `cfg`-gated:
//!
//! * `cfg(any(target_os = "linux", target_os = "windows"))` + feature
//!   `gpu-real` → run the container via bollard with
//!   `host_config.runtime = "nvidia"` (NVIDIA Container Toolkit). The image
//!   is expected to contain `nvidia-smi` / CUDA libs; the host kernel
//!   module + container toolkit must be pre-installed on the provider
//!   machine (documented in `daemon/README.md`).
//! * `cfg(all(target_os = "macos", target_arch = "aarch64"))` + feature
//!   `gpu-mlx` → real [`MlxRunner`]. The runner pulls the pre-built
//!   `ghcr.io/iogrid/mlx-runtime` image via Docker Desktop, passes the
//!   customer [`MlxSpec`] (model name, vRAM budget, batch size, prompt)
//!   as environment variables, waits for the container to exit, and
//!   surfaces the inference output back to the supervisor. The runner
//!   refuses to start on macOS < 14 (`HostTooOld`) — Apple's MLX framework
//!   requires Sonoma or newer.
//! * Otherwise → [`NoopGpuRunner`] (default-features-off scaffold) or
//!   [`MlxStubGpuRunner`] (compile-time MLX stub on non-supported macOS
//!   builds: returns `BackendUnimplemented` for every workload).
//!
//! The supervisor depends only on the [`GpuRunner`] trait + value types;
//! the concrete backend is opaque, so `cargo check` on every workspace
//! target stays vendor-light.

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
    /// The underlying Docker run failed (for CUDA / MLX-container workloads).
    #[error("docker GPU run failed: {0}")]
    Docker(String),
    /// Host operating-system version is too old for the active backend.
    /// MLX requires macOS 14+ (Sonoma).
    #[error("host too old for {backend}: requires {required}, found {found}")]
    HostTooOld {
        /// Backend slug ("mlx", "nvidia", …).
        backend: &'static str,
        /// Minimum required version (e.g. "macOS 14").
        required: String,
        /// Detected version on this host (e.g. "macOS 13.6").
        found: String,
    },
    /// An MLX assignment is missing its [`MlxSpec`] payload.
    #[error(
        "mlx workload {id} missing MlxSpec (model_name / vram_gb_required / batch_size / prompt)"
    )]
    MlxSpecMissing {
        /// Workload id.
        id: Uuid,
    },
}

/// MLX-specific inference assignment payload.
///
/// Carried inside a [`GpuWorkload`] when the coordinator targets the
/// Apple-Silicon MLX backend. The values are passed to the
/// `mlx-runtime` container as environment variables (`MLX_MODEL`,
/// `MLX_VRAM_GB`, `MLX_BATCH_SIZE`, `MLX_PROMPT`); the container's entry
/// point is responsible for loading the model and writing the inference
/// output to stdout.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct MlxSpec {
    /// Model name to load (e.g. `mlx-community/Llama-3.2-3B-Instruct-4bit`).
    pub model_name: String,
    /// vRAM budget the model is allowed to claim, in whole GiB.
    pub vram_gb_required: u32,
    /// Inference batch size.
    pub batch_size: u32,
    /// Prompt text to feed the model.
    pub prompt: String,
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
    /// Optional MLX inference payload — present iff the coordinator
    /// targeted the macOS MLX backend. The supervisor populates this from
    /// the dispatch envelope; CUDA workloads leave it `None`.
    #[serde(default)]
    pub mlx: Option<MlxSpec>,
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

    /// Report which backend is active (`"nvidia"`, `"mlx"`, `"mlx-stub"`, `"noop"`).
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

/// macOS MLX stub — used when the `gpu-mlx` feature is **off** (or the
/// build target isn't `aarch64-apple-darwin`).
///
/// Always returns [`GpuError::BackendUnimplemented`]. The real runner is
/// [`MlxRunner`], gated on
/// `cfg(all(feature = "gpu-mlx", target_os = "macos", target_arch = "aarch64"))`.
#[derive(Debug, Default, Clone)]
pub struct MlxStubGpuRunner;

#[async_trait]
impl GpuRunner for MlxStubGpuRunner {
    async fn run(&self, workload: GpuWorkload) -> Result<WorkloadResult, GpuError> {
        tracing::warn!(
            id = %workload.id,
            "MLX runner stub — built without `gpu-mlx` feature or off Apple-Silicon",
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

// ---------------------------------------------------------------------------
// MLX: real Apple-Silicon runner. Gated on `gpu-mlx` + macOS + aarch64.
// ---------------------------------------------------------------------------

#[cfg(all(feature = "gpu-mlx", target_os = "macos", target_arch = "aarch64"))]
pub use mlx::{MlxRunner, DEFAULT_MLX_IMAGE, MIN_MACOS_MAJOR};

#[cfg(all(feature = "gpu-mlx", target_os = "macos", target_arch = "aarch64"))]
mod mlx {
    //! Real MLX runner for Apple Silicon.
    //!
    //! Strategy: run an `mlx-runtime` container via Docker Desktop. The
    //! container ships the Apple MLX Python wheels (`mlx`, `mlx-lm`,
    //! `mlx-vlm`) and exposes an entry point that consumes four env vars:
    //!
    //! * `MLX_MODEL`      — model name on Hugging Face hub
    //! * `MLX_VRAM_GB`    — vRAM budget for the loader
    //! * `MLX_BATCH_SIZE` — batch size
    //! * `MLX_PROMPT`     — prompt text
    //!
    //! On exit the container writes the inference output to stdout; the
    //! runner captures the first 1 MiB of stdout+stderr in
    //! [`WorkloadResult::logs`]. The container has no host networking,
    //! read-only root fs, `no-new-privileges`, all caps dropped.
    //!
    //! Lower-level FFI (objc2 → MLX C API) is the planned faster path
    //! (issue #13, follow-up); the Docker route is the initial impl
    //! because (1) it reuses the same anti-abuse pipeline as the CUDA
    //! runner, (2) Apple ships no stable C ABI for MLX yet — only a
    //! Python/Swift surface — and pre-built MLX wheels in a container
    //! avoid every Swift/xcodebuild trap on CI runners.

    use super::*;
    use bollard::container::{
        Config as ContainerConfig, CreateContainerOptions, LogsOptions, RemoveContainerOptions,
        StartContainerOptions, WaitContainerOptions,
    };
    use bollard::image::CreateImageOptions;
    use bollard::models::HostConfig;
    use bollard::Docker;
    use futures_util::StreamExt;

    /// Default pre-built MLX-runtime image. Override with `MlxRunner::with_image`.
    pub const DEFAULT_MLX_IMAGE: &str = "ghcr.io/iogrid/mlx-runtime:latest";

    /// Minimum supported macOS major version. Apple's MLX framework
    /// requires Sonoma (14) or newer.
    pub const MIN_MACOS_MAJOR: u32 = 14;

    /// Cap on captured stdout+stderr per workload (1 MiB).
    const LOG_CAP_BYTES: usize = 1024 * 1024;

    /// Real MLX runner: bollard + a pre-built `mlx-runtime` image.
    #[derive(Debug, Clone)]
    pub struct MlxRunner {
        client: Docker,
        image: String,
    }

    impl MlxRunner {
        /// Connect to the local docker daemon and pin to the default
        /// `mlx-runtime` image. Returns `HostTooOld` if the host is on
        /// macOS < 14.
        pub fn connect_local() -> Result<Self, GpuError> {
            preflight_macos_version()?;
            let client = Docker::connect_with_local_defaults()
                .map_err(|e| GpuError::Docker(e.to_string()))?;
            Ok(Self {
                client,
                image: DEFAULT_MLX_IMAGE.to_string(),
            })
        }

        /// Override the pre-built MLX-runtime image. Useful when the
        /// operator has pre-pulled a private mirror.
        pub fn with_image(mut self, image: impl Into<String>) -> Self {
            self.image = image.into();
            self
        }

        /// Inspect the active image reference (mostly for diagnostics).
        pub fn image(&self) -> &str {
            &self.image
        }
    }

    #[async_trait]
    impl GpuRunner for MlxRunner {
        async fn run(&self, workload: GpuWorkload) -> Result<WorkloadResult, GpuError> {
            let spec = workload
                .mlx
                .as_ref()
                .ok_or(GpuError::MlxSpecMissing { id: workload.id })?
                .clone();

            // Pull the MLX runtime image.
            let opts = CreateImageOptions {
                from_image: self.image.clone(),
                ..Default::default()
            };
            let mut stream = self.client.create_image(Some(opts), None, None);
            while let Some(item) = stream.next().await {
                if let Err(e) = item {
                    return Err(GpuError::Docker(format!("pull: {e}")));
                }
            }

            // Build env: customer assignment + caller-supplied overrides.
            let mut env: Vec<String> = Vec::with_capacity(workload.env.len() + 4);
            env.push(format!("MLX_MODEL={}", spec.model_name));
            env.push(format!("MLX_VRAM_GB={}", spec.vram_gb_required));
            env.push(format!("MLX_BATCH_SIZE={}", spec.batch_size));
            env.push(format!("MLX_PROMPT={}", spec.prompt));
            for (k, v) in &workload.env {
                env.push(format!("{k}={v}"));
            }

            // Apple Silicon: no NVIDIA device request; the MLX-runtime
            // image runs Metal through Docker Desktop's Apple-Virtualisation
            // backend. Hardening mirrors the CUDA path.
            let host = HostConfig {
                network_mode: Some("iogrid-egress".to_string()),
                readonly_rootfs: Some(true),
                security_opt: Some(vec!["no-new-privileges:true".to_string()]),
                cap_drop: Some(vec!["ALL".to_string()]),
                ..Default::default()
            };

            let cfg: ContainerConfig<String> = ContainerConfig {
                image: Some(self.image.clone()),
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
            let container_name = format!("iogridd-mlx-{}", workload.id);
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

            // Drain stdout+stderr (capped) — the inference output is the
            // payload the coordinator forwards back to the customer.
            let mut logs: Vec<u8> = Vec::new();
            let mut log_stream = self.client.logs::<String>(
                &container_name,
                Some(LogsOptions {
                    stdout: true,
                    stderr: true,
                    follow: false,
                    timestamps: false,
                    ..Default::default()
                }),
            );
            while let Some(chunk) = log_stream.next().await {
                match chunk {
                    Ok(o) => {
                        let bytes = o.into_bytes();
                        if logs.len() + bytes.len() > LOG_CAP_BYTES {
                            let take = LOG_CAP_BYTES.saturating_sub(logs.len());
                            logs.extend_from_slice(&bytes[..take]);
                            break;
                        }
                        logs.extend_from_slice(&bytes);
                    }
                    Err(e) => {
                        tracing::warn!(error = %e, "mlx logs stream error");
                        break;
                    }
                }
            }

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
                logs,
                timed_out,
            })
        }
        fn backend(&self) -> &'static str {
            "mlx"
        }
    }

    /// Run `sw_vers -productVersion` and bail with [`GpuError::HostTooOld`]
    /// if the major version is < 14 (Sonoma).
    fn preflight_macos_version() -> Result<(), GpuError> {
        let out = std::process::Command::new("sw_vers")
            .arg("-productVersion")
            .output()
            .map_err(|e| GpuError::VendorError(format!("sw_vers spawn: {e}")))?;
        let version = String::from_utf8_lossy(&out.stdout).trim().to_string();
        let major = version
            .split('.')
            .next()
            .and_then(|s| s.parse::<u32>().ok())
            .unwrap_or(0);
        if major < MIN_MACOS_MAJOR {
            return Err(GpuError::HostTooOld {
                backend: "mlx",
                required: format!("macOS {MIN_MACOS_MAJOR}"),
                found: format!("macOS {version}"),
            });
        }
        Ok(())
    }
}

/// Pick the platform-appropriate backend.
///
/// * Linux/Windows with `gpu-real` → [`NvidiaContainerRunner`] (boxed trait).
/// * macOS aarch64 with `gpu-mlx` → real `MlxRunner` if Docker reachable
///   and the host is on macOS 14+; falls back to [`MlxStubGpuRunner`]
///   otherwise.
/// * Anything else → [`NoopGpuRunner`].
pub fn default_runner() -> Box<dyn GpuRunner> {
    #[cfg(all(feature = "gpu-real", any(target_os = "linux", target_os = "windows")))]
    {
        if let Ok(r) = NvidiaContainerRunner::connect_local() {
            return Box::new(r);
        }
        return Box::new(NoopGpuRunner);
    }
    #[cfg(all(feature = "gpu-mlx", target_os = "macos", target_arch = "aarch64"))]
    {
        match MlxRunner::connect_local() {
            Ok(r) => return Box::new(r),
            Err(e) => {
                tracing::warn!(error = %e, "MLX runner unavailable, falling back to stub");
                return Box::new(MlxStubGpuRunner);
            }
        }
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
            mlx: None,
        }
    }

    fn mlx_sample() -> GpuWorkload {
        GpuWorkload {
            id: Uuid::new_v4(),
            image: "ghcr.io/iogrid/mlx-runtime:latest".into(),
            cmd: vec![],
            env: vec![],
            vram_mib: 8 * 1024,
            timeout_secs: 600,
            mlx: Some(MlxSpec {
                model_name: "mlx-community/Llama-3.2-3B-Instruct-4bit".into(),
                vram_gb_required: 8,
                batch_size: 1,
                prompt: "hello".into(),
            }),
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
        let err = r.run(mlx_sample()).await.unwrap_err();
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
        assert!(["noop", "nvidia", "mlx", "mlx-stub"].contains(&backend));
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

    #[test]
    fn mlx_spec_serde_round_trip() {
        let spec = MlxSpec {
            model_name: "mlx-community/Llama-3.2-3B-Instruct-4bit".into(),
            vram_gb_required: 8,
            batch_size: 4,
            prompt: "Tell me a joke.".into(),
        };
        let s = serde_json::to_string(&spec).expect("serialize");
        let back: MlxSpec = serde_json::from_str(&s).expect("deserialize");
        assert_eq!(back, spec);
    }

    #[test]
    fn gpu_workload_deserializes_without_mlx_field() {
        // Backwards-compat: producers that pre-date the MLX field must
        // still deserialise cleanly (`serde(default)` fills in `None`).
        let json = r#"{
            "id": "00000000-0000-0000-0000-000000000000",
            "image": "ghcr.io/foo/bar:latest",
            "cmd": ["nvidia-smi"],
            "env": [],
            "vram_mib": 4096,
            "timeout_secs": 600
        }"#;
        let w: GpuWorkload = serde_json::from_str(json).expect("parse");
        assert!(w.mlx.is_none());
    }

    #[test]
    fn gpu_workload_with_mlx_round_trips() {
        let w = mlx_sample();
        let s = serde_json::to_string(&w).expect("serialize");
        let back: GpuWorkload = serde_json::from_str(&s).expect("deserialize");
        assert_eq!(back.mlx, w.mlx);
    }

    #[tokio::test]
    async fn mlx_stub_surfaces_workload_without_spec_as_unimplemented() {
        // The stub returns BackendUnimplemented regardless of payload —
        // it never reaches the MlxSpecMissing branch (that's only the
        // real runner). Documented behaviour: the supervisor must not
        // dispatch MLX assignments unless backend()=="mlx".
        let r = MlxStubGpuRunner;
        let err = r.run(sample()).await.unwrap_err();
        assert!(matches!(
            err,
            GpuError::BackendUnimplemented {
                backend: "mlx-stub"
            }
        ));
    }
}
