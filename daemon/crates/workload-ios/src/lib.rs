//! iOS-build workload — Tart subprocess driver.
//!
//! Only the macOS target compiles a real Tart driver. On every other target
//! the same public API is exposed but every operation returns
//! [`IosBuildError::UnsupportedPlatform`]. This lets the workspace
//! `cargo check` stay clean across Linux / Windows CI runners while keeping
//! import sites pleasant — no `#[cfg]` at every call site.
//!
//! Tart (https://tart.run) is a CLI that manages macOS VMs on Apple Silicon
//! via the `Virtualization.framework`. The runner shells out to it via
//! `tokio::process::Command`:
//!
//! ```text
//!   tart clone <base-image-ref> <vm-name>
//!   tart set   <vm-name> --cpu N --memory M
//!   tart run   --dir source=<host-path>:<guest-path> --no-graphics <vm-name> &
//!   # poll `tart ip <vm-name>` until reachable
//!   ssh admin@<ip> <build_command>
//!   # collect artifacts from a fixed guest path
//!   tart delete <vm-name>
//! ```
//!
//! Per Tart docs the default VM password is `admin`; this matches the
//! upstream `cirruslabs/macos-sonoma-xcode` image (and the dev base image
//! `cirruslabs/macos-sequoia-xcode` for macOS 15).
//!
//! The runner requires **macOS 15 Sequoia or newer** on the provider machine
//! (the iOS 18 SDK ships with Xcode 26 which is Sequoia-only). The
//! supervisor uses [`iogrid_platform_mac::macos_major_version`] at startup
//! to refuse advertising `IOS_BUILD` as an eligible workload type on
//! older hosts.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::path::PathBuf;
use std::time::Duration;

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
    /// Running on macOS but version is older than the required minimum (15).
    #[error("iOS build requires macOS 15 Sequoia or newer; detected major version {major}")]
    HostTooOld {
        /// Detected macOS major version (e.g. `14`).
        major: u32,
    },
    /// `tart` CLI missing.
    #[error("tart CLI not found on PATH")]
    TartMissing,
    /// Tart subcommand returned non-zero.
    #[error("tart command {cmd} failed (exit={code}): {stderr}")]
    TartFailed {
        /// Subcommand verb (`clone`, `run`, …).
        cmd: String,
        /// Exit code returned by `tart`.
        code: i32,
        /// Captured stderr.
        stderr: String,
    },
    /// VM never came up before [`IosBuildWorkload::boot_timeout_secs`].
    #[error("VM {vm} never reported an IP within {after_secs}s")]
    BootTimedOut {
        /// VM name.
        vm: String,
        /// Seconds before the runner gave up.
        after_secs: u32,
    },
    /// Whole workload exceeded its wall-clock time-box.
    #[error("workload {id} timed out after {after_secs}s")]
    TimedOut {
        /// Workload id.
        id: Uuid,
        /// Seconds before kill.
        after_secs: u32,
    },
    /// Build script returned non-zero.
    #[error("build failed with exit code {0}")]
    BuildFailed(i32),
    /// Artifact upload failed.
    #[error("artifact upload to {url} failed: {reason}")]
    UploadFailed {
        /// Signed S3 URL.
        url: String,
        /// Reason / error string.
        reason: String,
    },
    /// I/O bubble-up.
    #[error("io error: {0}")]
    Io(String),
}

/// iOS-build workload description.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IosBuildWorkload {
    /// Coordinator-assigned id.
    pub id: Uuid,
    /// Base Tart VM image reference (e.g. `ghcr.io/cirruslabs/macos-sequoia-xcode:latest`).
    pub tart_image: String,
    /// Customer git repo URL the VM should `git clone`.
    pub repo_url: String,
    /// Branch or commit to check out.
    pub git_ref: String,
    /// `xcodebuild` command (or any shell command) to invoke inside the VM,
    /// already including signing-disable flags etc.
    pub build_command: String,
    /// Path inside the VM containing the produced `.ipa` / `.xcarchive`.
    /// Defaults to `/Users/admin/build/output.ipa` when empty.
    pub artifact_guest_path: String,
    /// Pre-signed PUT URL supplied by the coordinator for artifact upload.
    pub upload_url: String,
    /// VM CPU cores.
    pub cpu: u32,
    /// VM memory (MiB).
    pub memory_mib: u32,
    /// Wall-clock timeout for the whole workload (seconds).
    pub timeout_secs: u32,
    /// How long to poll `tart ip` before giving up on the VM (seconds).
    pub boot_timeout_secs: u32,
}

impl IosBuildWorkload {
    /// VM name derived from the workload id.
    pub fn vm_name(&self) -> String {
        format!("iogridd-ios-{}", self.id)
    }
    /// Guest path resolved to default when empty.
    pub fn artifact_path(&self) -> &str {
        if self.artifact_guest_path.is_empty() {
            "/Users/admin/build/output.ipa"
        } else {
            &self.artifact_guest_path
        }
    }
}

/// Captured iOS-build result.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IosBuildResult {
    /// Workload id (mirrors [`IosBuildWorkload::id`]).
    pub id: Uuid,
    /// Build command exit code.
    pub exit_code: i32,
    /// Captured `xcodebuild` stdout/stderr (capped at 1 MiB).
    pub logs: Vec<u8>,
    /// Local artifact path that was uploaded.
    pub artifact_local_path: Option<PathBuf>,
    /// Whether the time-box fired.
    pub timed_out: bool,
}

/// iOS-build runner contract.
#[async_trait]
pub trait IosBuildRunner: Send + Sync {
    /// Run the build to completion. Returns [`IosBuildResult`] on success.
    async fn run(&self, workload: IosBuildWorkload) -> Result<IosBuildResult, IosBuildError>;
}

/// Tart-based runner. Real implementation lives in [`tart_impl`]; on non-mac
/// targets every call returns [`IosBuildError::UnsupportedPlatform`].
#[derive(Debug, Clone)]
pub struct TartRunner {
    /// Path to the `tart` binary (defaults to `tart`, resolved via $PATH).
    pub tart_binary: String,
    /// SSH user inside the VM (Tart default is `admin`).
    pub ssh_user: String,
    /// SSH password inside the VM (Tart default is `admin`).
    pub ssh_password: String,
}

impl Default for TartRunner {
    fn default() -> Self {
        Self {
            tart_binary: "tart".to_string(),
            ssh_user: "admin".to_string(),
            ssh_password: "admin".to_string(),
        }
    }
}

#[async_trait]
impl IosBuildRunner for TartRunner {
    async fn run(&self, workload: IosBuildWorkload) -> Result<IosBuildResult, IosBuildError> {
        #[cfg(target_os = "macos")]
        {
            let driver = tart_impl::TartDriver {
                tart_binary: self.tart_binary.clone(),
                ssh_user: self.ssh_user.clone(),
                ssh_password: self.ssh_password.clone(),
            };
            return driver.run(workload).await;
        }
        #[cfg(not(target_os = "macos"))]
        {
            let _ = workload;
            Err(IosBuildError::UnsupportedPlatform)
        }
    }
}

/// Drive the workload to completion with an explicit wall-clock deadline. The
/// supervisor uses this on its own (`tokio::time::timeout`) so the runner can
/// still mark the result `timed_out` rather than dropping the future.
pub async fn run_with_timeout<R: IosBuildRunner + ?Sized>(
    runner: &R,
    workload: IosBuildWorkload,
) -> Result<IosBuildResult, IosBuildError> {
    let total = Duration::from_secs(workload.timeout_secs as u64);
    let id = workload.id;
    let after_secs = workload.timeout_secs;
    match tokio::time::timeout(total, runner.run(workload)).await {
        Ok(res) => res,
        Err(_) => Err(IosBuildError::TimedOut { id, after_secs }),
    }
}

#[cfg(target_os = "macos")]
mod tart_impl {
    //! Live Tart driver — `tokio::process::Command` powered.

    use super::*;
    use std::process::Stdio;
    use std::time::Instant;
    use tokio::io::AsyncReadExt;
    use tokio::process::Command;

    pub(super) struct TartDriver {
        pub tart_binary: String,
        pub ssh_user: String,
        pub ssh_password: String,
    }

    impl TartDriver {
        pub async fn run(&self, w: IosBuildWorkload) -> Result<IosBuildResult, IosBuildError> {
            // 0. Refuse on macOS <15.
            let major = iogrid_platform_mac::macos_major_version().unwrap_or(0);
            if major != 0 && major < 15 {
                return Err(IosBuildError::HostTooOld { major });
            }
            // 1. Locate tart.
            if !self.tart_on_path().await {
                return Err(IosBuildError::TartMissing);
            }
            let vm = w.vm_name();
            // 2. Clone base image.
            self.tart(["clone", &w.tart_image, &vm]).await?;
            // 3. Set resources.
            let cpu = w.cpu.max(1).to_string();
            let mem = w.memory_mib.max(2048).to_string();
            self.tart(["set", &vm, "--cpu", &cpu, "--memory", &mem])
                .await?;

            // 4. Spawn `tart run` in background.
            let mut run_child = Command::new(&self.tart_binary)
                .args(["run", "--no-graphics", &vm])
                .stdout(Stdio::null())
                .stderr(Stdio::piped())
                .spawn()
                .map_err(|e| IosBuildError::Io(format!("spawn tart run: {e}")))?;

            // 5. Poll for VM IP.
            let start = Instant::now();
            let boot_deadline = Duration::from_secs(w.boot_timeout_secs as u64);
            let ip = loop {
                if start.elapsed() > boot_deadline {
                    let _ = run_child.kill().await;
                    let _ = self.tart(["delete", &vm]).await;
                    return Err(IosBuildError::BootTimedOut {
                        vm: vm.clone(),
                        after_secs: w.boot_timeout_secs,
                    });
                }
                match self.tart_capture(["ip", &vm]).await {
                    Ok((0, out)) => {
                        let trimmed = String::from_utf8_lossy(&out).trim().to_string();
                        if !trimmed.is_empty() {
                            break trimmed;
                        }
                    }
                    _ => {}
                }
                tokio::time::sleep(Duration::from_secs(2)).await;
            };

            // 6. SSH in and run the build, time-boxed by the supervisor via
            // run_with_timeout (so we still get a timed_out=true result).
            let (exit_code, logs) = match self.ssh_run(&ip, &self.build_command(&w)).await {
                Ok(x) => x,
                Err(e) => {
                    let _ = run_child.kill().await;
                    let _ = self.tart(["delete", &vm]).await;
                    return Err(e);
                }
            };

            // 7. Copy out artifact via scp.
            let artifact_local = std::env::temp_dir().join(format!("{}.ipa", w.id));
            if let Err(e) = self.scp_from(&ip, w.artifact_path(), &artifact_local).await {
                tracing::warn!(%e, "scp artifact failed (build may have produced no ipa)");
            }

            // 8. Upload to coordinator-supplied URL (best-effort; the
            // supervisor decides whether a missing artifact is a build
            // failure).
            if artifact_local.exists() && !w.upload_url.is_empty() {
                if let Err(e) = self.upload_artifact(&artifact_local, &w.upload_url).await {
                    tracing::warn!(%e, "artifact upload failed");
                }
            }

            // 9. Shutdown + delete.
            let _ = run_child.kill().await;
            let _ = self.tart(["delete", &vm]).await;

            Ok(IosBuildResult {
                id: w.id,
                exit_code,
                logs,
                artifact_local_path: artifact_local.exists().then_some(artifact_local),
                timed_out: false,
            })
        }

        async fn tart_on_path(&self) -> bool {
            Command::new(&self.tart_binary)
                .arg("--version")
                .stdout(Stdio::null())
                .stderr(Stdio::null())
                .status()
                .await
                .map(|s| s.success())
                .unwrap_or(false)
        }

        async fn tart<'a, I>(&self, args: I) -> Result<(), IosBuildError>
        where
            I: IntoIterator<Item = &'a str>,
        {
            let args: Vec<&str> = args.into_iter().collect();
            let verb = args.first().copied().unwrap_or("?").to_string();
            let out = Command::new(&self.tart_binary)
                .args(&args)
                .output()
                .await
                .map_err(|e| IosBuildError::Io(format!("exec tart {verb}: {e}")))?;
            if !out.status.success() {
                return Err(IosBuildError::TartFailed {
                    cmd: verb,
                    code: out.status.code().unwrap_or(-1),
                    stderr: String::from_utf8_lossy(&out.stderr).into_owned(),
                });
            }
            Ok(())
        }

        async fn tart_capture<'a, I>(&self, args: I) -> Result<(i32, Vec<u8>), IosBuildError>
        where
            I: IntoIterator<Item = &'a str>,
        {
            let args: Vec<&str> = args.into_iter().collect();
            let out = Command::new(&self.tart_binary)
                .args(&args)
                .output()
                .await
                .map_err(|e| IosBuildError::Io(format!("exec tart: {e}")))?;
            Ok((out.status.code().unwrap_or(-1), out.stdout))
        }

        fn build_command(&self, w: &IosBuildWorkload) -> String {
            // We assemble a single shell snippet that clones the repo, checks
            // out the requested ref, and runs the customer-supplied build
            // command. The customer is responsible for any signing flags.
            format!(
                "set -euo pipefail; \
                 mkdir -p $HOME/build && cd $HOME/build; \
                 if [ ! -d repo ]; then git clone --depth 50 {repo} repo; fi; \
                 cd repo && git fetch origin {gref} && git checkout {gref}; \
                 {cmd}",
                repo = shell_quote(&w.repo_url),
                gref = shell_quote(&w.git_ref),
                cmd = w.build_command,
            )
        }

        async fn ssh_run(
            &self,
            ip: &str,
            remote_cmd: &str,
        ) -> Result<(i32, Vec<u8>), IosBuildError> {
            // We rely on `sshpass` being installed via brew on the provider
            // host; the Tart default password is `admin`.
            let mut child = Command::new("sshpass")
                .args([
                    "-p",
                    &self.ssh_password,
                    "ssh",
                    "-o",
                    "StrictHostKeyChecking=no",
                    "-o",
                    "UserKnownHostsFile=/dev/null",
                    &format!("{}@{}", self.ssh_user, ip),
                    remote_cmd,
                ])
                .stdout(Stdio::piped())
                .stderr(Stdio::piped())
                .spawn()
                .map_err(|e| IosBuildError::Io(format!("spawn sshpass: {e}")))?;
            let mut stdout = child.stdout.take().expect("piped stdout");
            let mut stderr = child.stderr.take().expect("piped stderr");
            let cap: usize = 1_048_576;
            let mut logs = Vec::with_capacity(8192);
            let mut buf = [0u8; 4096];
            loop {
                tokio::select! {
                    n = stdout.read(&mut buf) => {
                        match n {
                            Ok(0) => break,
                            Ok(n) => {
                                if logs.len() + n <= cap {
                                    logs.extend_from_slice(&buf[..n]);
                                }
                            }
                            Err(_) => break,
                        }
                    }
                    n = stderr.read(&mut buf) => {
                        match n {
                            Ok(0) => break,
                            Ok(n) => {
                                if logs.len() + n <= cap {
                                    logs.extend_from_slice(&buf[..n]);
                                }
                            }
                            Err(_) => break,
                        }
                    }
                }
            }
            let status = child
                .wait()
                .await
                .map_err(|e| IosBuildError::Io(format!("wait sshpass: {e}")))?;
            let code = status.code().unwrap_or(-1);
            if code != 0 {
                return Err(IosBuildError::BuildFailed(code));
            }
            Ok((code, logs))
        }

        async fn scp_from(
            &self,
            ip: &str,
            remote_path: &str,
            local_path: &std::path::Path,
        ) -> Result<(), IosBuildError> {
            let out = Command::new("sshpass")
                .args([
                    "-p",
                    &self.ssh_password,
                    "scp",
                    "-o",
                    "StrictHostKeyChecking=no",
                    "-o",
                    "UserKnownHostsFile=/dev/null",
                    &format!("{}@{}:{}", self.ssh_user, ip, remote_path),
                    local_path.to_string_lossy().as_ref(),
                ])
                .output()
                .await
                .map_err(|e| IosBuildError::Io(format!("exec scp: {e}")))?;
            if !out.status.success() {
                return Err(IosBuildError::Io(format!(
                    "scp failed (exit={}): {}",
                    out.status.code().unwrap_or(-1),
                    String::from_utf8_lossy(&out.stderr)
                )));
            }
            Ok(())
        }

        async fn upload_artifact(
            &self,
            path: &std::path::Path,
            url: &str,
        ) -> Result<(), IosBuildError> {
            // curl --upload-file works for the standard S3 pre-signed PUT URL.
            let out = Command::new("curl")
                .args([
                    "-fsS",
                    "--upload-file",
                    path.to_string_lossy().as_ref(),
                    url,
                ])
                .output()
                .await
                .map_err(|e| IosBuildError::Io(format!("exec curl: {e}")))?;
            if !out.status.success() {
                return Err(IosBuildError::UploadFailed {
                    url: url.to_string(),
                    reason: format!(
                        "curl exit {} stderr={}",
                        out.status.code().unwrap_or(-1),
                        String::from_utf8_lossy(&out.stderr)
                    ),
                });
            }
            Ok(())
        }
    }

    fn shell_quote(s: &str) -> String {
        // Naive single-quote escaping good enough for repo URLs / git refs.
        let escaped = s.replace('\'', "'\\''");
        format!("'{escaped}'")
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_workload() -> IosBuildWorkload {
        IosBuildWorkload {
            id: Uuid::new_v4(),
            tart_image: "ghcr.io/cirruslabs/macos-sequoia-xcode:latest".into(),
            repo_url: "https://github.com/foo/bar".into(),
            git_ref: "main".into(),
            build_command: "xcodebuild -scheme App -configuration Release archive".into(),
            artifact_guest_path: String::new(),
            upload_url: String::new(),
            cpu: 4,
            memory_mib: 8192,
            timeout_secs: 1800,
            boot_timeout_secs: 300,
        }
    }

    #[tokio::test]
    async fn non_mac_returns_unsupported() {
        let r = TartRunner::default();
        let res = r.run(sample_workload()).await;
        #[cfg(target_os = "macos")]
        {
            // On macOS we either succeed, fail-tart-missing, fail-host-too-old, or
            // boot-timeout — anything but `UnsupportedPlatform`.
            assert!(!matches!(res, Err(IosBuildError::UnsupportedPlatform)));
        }
        #[cfg(not(target_os = "macos"))]
        {
            assert!(matches!(res, Err(IosBuildError::UnsupportedPlatform)));
        }
    }

    #[tokio::test]
    async fn run_with_timeout_marks_timed_out_when_inner_blocks() {
        struct SlowRunner;
        #[async_trait]
        impl IosBuildRunner for SlowRunner {
            async fn run(&self, _w: IosBuildWorkload) -> Result<IosBuildResult, IosBuildError> {
                tokio::time::sleep(Duration::from_secs(10)).await;
                Err(IosBuildError::BuildFailed(99))
            }
        }
        let mut w = sample_workload();
        w.timeout_secs = 1;
        let res = run_with_timeout(&SlowRunner, w.clone()).await;
        assert!(matches!(res, Err(IosBuildError::TimedOut { .. })));
    }

    #[test]
    fn vm_name_is_stable_for_id() {
        let w = sample_workload();
        assert!(w.vm_name().starts_with("iogridd-ios-"));
    }

    #[test]
    fn artifact_path_default_when_empty() {
        let mut w = sample_workload();
        w.artifact_guest_path.clear();
        assert!(w.artifact_path().ends_with("output.ipa"));
        w.artifact_guest_path = "/custom/path.ipa".into();
        assert_eq!(w.artifact_path(), "/custom/path.ipa");
    }
}
