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
    /// No usable local Xcode toolchain (native runner; `xcode-select -p` failed).
    #[error("no usable Xcode toolchain on host (xcode-select -p failed)")]
    XcodeMissing,
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
    /// Optional post-build Maestro simulator walkthrough. When `None` the
    /// runner stops after `xcodebuild` + artifact upload. When `Some`, the
    /// runner additionally boots an iOS simulator inside the same VM,
    /// installs the freshly built `.app`, and runs the Maestro suite,
    /// copying the JUnit report + screenshots out as extra artifacts.
    #[serde(default)]
    pub maestro: Option<MaestroWalkthrough>,
}

/// Post-build Maestro walkthrough configuration. All paths are *inside the
/// guest VM* unless noted. The runner shells these into `simctl` + `maestro`
/// over the same `sshpass ssh` channel the build used.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MaestroWalkthrough {
    /// Guest path to the built simulator `.app` bundle to install
    /// (e.g. `$HOME/build/repo/mobile/ios/build/Build/Products/Release-iphonesimulator/iogrid.app`).
    /// When empty the runner uses [`MaestroWalkthrough::DEFAULT_APP_GLOB`]
    /// to locate the first `*.app` under the build's `Release-iphonesimulator`
    /// products dir.
    #[serde(default)]
    pub app_guest_path: String,
    /// Bundle identifier of the installed app (used by `simctl terminate`
    /// between the outer restart-loop attempts). Defaults to
    /// `io.iogrid.app` when empty.
    #[serde(default)]
    pub app_bundle_id: String,
    /// Guest path to the Maestro master flow to run with
    /// `maestro test --format junit`. Defaults to
    /// `$HOME/build/repo/mobile/ios/.maestro/00-all.yaml` when empty.
    #[serde(default)]
    pub flow_guest_path: String,
}

impl MaestroWalkthrough {
    /// Glob used to locate the simulator `.app` when `app_guest_path` is empty.
    pub const DEFAULT_APP_GLOB: &'static str =
        "$HOME/build/repo/mobile/ios/build/Build/Products/Release-iphonesimulator/*.app";
    /// Default Maestro master flow path.
    pub const DEFAULT_FLOW: &'static str = "$HOME/build/repo/mobile/ios/.maestro/00-all.yaml";
    /// Default bundle id (matches `appId` in `.maestro/00-all.yaml`).
    pub const DEFAULT_BUNDLE_ID: &'static str = "io.iogrid.app";

    /// Bundle id resolved to the default when empty.
    pub fn bundle_id(&self) -> &str {
        if self.app_bundle_id.is_empty() {
            Self::DEFAULT_BUNDLE_ID
        } else {
            &self.app_bundle_id
        }
    }
    /// Flow path resolved to the default when empty.
    pub fn flow_path(&self) -> &str {
        if self.flow_guest_path.is_empty() {
            Self::DEFAULT_FLOW
        } else {
            &self.flow_guest_path
        }
    }
    /// App-bundle locator: an explicit path when set, otherwise the glob.
    pub fn app_locator(&self) -> &str {
        if self.app_guest_path.is_empty() {
            Self::DEFAULT_APP_GLOB
        } else {
            &self.app_guest_path
        }
    }
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
    /// Post-build Maestro walkthrough outcome, if one was requested.
    #[serde(default)]
    pub maestro: Option<MaestroResult>,
}

/// Outcome of the post-build Maestro simulator walkthrough.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MaestroResult {
    /// `maestro test` exit code from the attempt that decided the result
    /// (`0` = all flows green). `-1` when Maestro never ran (e.g. the
    /// simulator failed to boot before any attempt).
    pub exit_code: i32,
    /// How many outer restart-loop attempts were spent (1..=3).
    pub attempts: u32,
    /// Local path the JUnit report was copied to, if it was produced.
    pub junit_local_path: Option<PathBuf>,
    /// Local directory the screenshots were copied to, if any were produced.
    pub screenshots_local_dir: Option<PathBuf>,
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

/// Native (host-direct) runner — runs the clone + build straight on the
/// provider Mac with its locally installed Xcode, no VM. The platform
/// posture is VM/pod isolation everywhere; this is the explicit
/// macOS-native exception for hosts that have an Xcode toolchain but no
/// `tart` (e.g. cannot fit the ~60 GiB VM base image on disk, or run
/// macOS 14 where the Tart runner's Sequoia requirement isn't met).
#[derive(Debug, Clone)]
pub struct NativeRunner {
    /// Root under which per-build workspaces are created
    /// (`<work_root>/iogridd-ios-<id>`). Defaults to the OS temp dir.
    pub work_root: PathBuf,
}

impl Default for NativeRunner {
    fn default() -> Self {
        Self {
            work_root: std::env::temp_dir(),
        }
    }
}

#[async_trait]
impl IosBuildRunner for NativeRunner {
    async fn run(&self, workload: IosBuildWorkload) -> Result<IosBuildResult, IosBuildError> {
        #[cfg(target_os = "macos")]
        {
            let driver = native_impl::NativeDriver {
                work_root: self.work_root.clone(),
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

/// Pick the runner this host can actually drive: [`TartRunner`] when the
/// `tart` CLI is on PATH (full-VM isolation — the production posture),
/// [`NativeRunner`] otherwise. Non-macOS hosts get a runner that errors
/// with [`IosBuildError::UnsupportedPlatform`]; the capability gate keeps
/// them from ever being assigned IOS_BUILD work in the first place.
pub fn auto_runner() -> std::sync::Arc<dyn IosBuildRunner> {
    let tart_present = std::process::Command::new("tart")
        .arg("--version")
        .stdout(std::process::Stdio::null())
        .stderr(std::process::Stdio::null())
        .status()
        .map(|s| s.success())
        .unwrap_or(false);
    if tart_present {
        std::sync::Arc::new(TartRunner::default())
    } else {
        tracing::info!("tart not on PATH — using native host-direct iOS-build runner");
        std::sync::Arc::new(NativeRunner::default())
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
                if let Ok((0, out)) = self.tart_capture(["ip", &vm]).await {
                    let trimmed = String::from_utf8_lossy(&out).trim().to_string();
                    if !trimmed.is_empty() {
                        break trimmed;
                    }
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

            // 8b. Optional Maestro simulator walkthrough — boot a sim inside
            // the same VM, install the freshly built `.app`, run the Maestro
            // suite, and copy the JUnit report + screenshots out as extra
            // artifacts. Best-effort and gated on the build having succeeded:
            // a non-zero `xcodebuild` exit already returns earlier via
            // ssh_run's BuildFailed, so reaching here means exit_code == 0.
            let maestro_result = match &w.maestro {
                Some(cfg) => match self.maestro_walkthrough(&ip, &w, cfg).await {
                    Ok(r) => Some(r),
                    Err(e) => {
                        tracing::warn!(%e, "maestro walkthrough failed (non-fatal)");
                        Some(MaestroResult {
                            exit_code: -1,
                            attempts: 0,
                            junit_local_path: None,
                            screenshots_local_dir: None,
                        })
                    }
                },
                None => None,
            };

            // 9. Shutdown + delete.
            let _ = run_child.kill().await;
            let _ = self.tart(["delete", &vm]).await;

            Ok(IosBuildResult {
                id: w.id,
                exit_code,
                logs,
                artifact_local_path: artifact_local.exists().then_some(artifact_local),
                timed_out: false,
                maestro: maestro_result,
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

        /// Run the post-build Maestro walkthrough inside the already-booted
        /// VM (`ip`). Returns a [`MaestroResult`]; never returns an error for
        /// a *flow* failure (those are reported via `exit_code`), only for
        /// transport/ssh failures the caller can't act on.
        async fn maestro_walkthrough(
            &self,
            ip: &str,
            w: &IosBuildWorkload,
            cfg: &MaestroWalkthrough,
        ) -> Result<MaestroResult, IosBuildError> {
            // The walkthrough runs entirely guest-side in one shell snippet:
            // pick a sim device type + runtime, create+boot, install the
            // built .app, then run Maestro under an OUTER RESTART LOOP gated
            // on the stale-XCTest-handle "App crashed or stopped" signature
            // (per mobile/ios/CONTRIBUTING.md gotcha 23 + the project-memory
            // `feedback_maestro_stale_xctest_handle`: a mid-suite relaunch
            // leaves the XCUIApplication handle stale -> every snapshot fails
            // XCTest error 10001, which Maestro maps to a fatal AppCrash;
            // in-session `retry:` wrappers inherit the same stale handle and
            // lose 3/3, so the ONLY cure is a fresh `maestro` invocation).
            // We do NOT pass any `timeout:` to assertVisible — Maestro 2.6
            // rejects it suite-wide (gotcha 19); flows use `extendedWaitUntil`
            // instead. Artifacts (junit + screenshots) are written to fixed
            // guest paths and scp'd out below.
            let script = maestro_remote_script(cfg);
            tracing::info!(%ip, "running maestro walkthrough inside VM");
            // Maestro + the XCUITest driver spin-up are slow; the supervisor's
            // outer `run_with_timeout` still bounds total wall-clock, so a hung
            // sim can't run forever. We capture exit code + logs here and parse
            // the attempt count from the script's own bookkeeping line.
            let (code, logs) = self.ssh_run_capture(ip, &script).await?;
            let attempts = parse_maestro_attempts(&logs);

            // Copy the JUnit report + screenshots dir out of the guest. Both
            // are best-effort: a sim that never booted produces neither.
            let junit_local = std::env::temp_dir().join(format!("{}-maestro-junit.xml", w.id));
            let junit_ok = self
                .scp_from(ip, MAESTRO_GUEST_JUNIT, &junit_local)
                .await
                .is_ok();

            let screenshots_local =
                std::env::temp_dir().join(format!("{}-maestro-screenshots", w.id));
            let _ = tokio::fs::create_dir_all(&screenshots_local).await;
            // `scp -r` the whole screenshots dir; tolerate "no such file".
            let screenshots_ok = self
                .scp_recursive_from(ip, MAESTRO_GUEST_SCREENSHOT_DIR, &screenshots_local)
                .await
                .is_ok();

            Ok(MaestroResult {
                exit_code: code,
                attempts,
                junit_local_path: junit_ok.then_some(junit_local),
                screenshots_local_dir: screenshots_ok.then_some(screenshots_local),
            })
        }

        /// Like [`ssh_run`] but does NOT treat a non-zero remote exit as a
        /// hard [`IosBuildError::BuildFailed`] — the Maestro walkthrough
        /// reports flow failures via the captured exit code instead.
        async fn ssh_run_capture(
            &self,
            ip: &str,
            remote_cmd: &str,
        ) -> Result<(i32, Vec<u8>), IosBuildError> {
            let out = Command::new("sshpass")
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
                .output()
                .await
                .map_err(|e| IosBuildError::Io(format!("spawn sshpass (maestro): {e}")))?;
            let mut logs = out.stdout;
            logs.extend_from_slice(&out.stderr);
            Ok((out.status.code().unwrap_or(-1), logs))
        }

        async fn scp_recursive_from(
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
                    "-r",
                    "-o",
                    "StrictHostKeyChecking=no",
                    "-o",
                    "UserKnownHostsFile=/dev/null",
                    &format!("{}@{}:{}", self.ssh_user, ip, remote_path),
                    local_path.to_string_lossy().as_ref(),
                ])
                .output()
                .await
                .map_err(|e| IosBuildError::Io(format!("exec scp -r: {e}")))?;
            if !out.status.success() {
                return Err(IosBuildError::Io(format!(
                    "scp -r failed (exit={}): {}",
                    out.status.code().unwrap_or(-1),
                    String::from_utf8_lossy(&out.stderr)
                )));
            }
            Ok(())
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
            let mut out_buf = [0u8; 4096];
            let mut err_buf = [0u8; 4096];
            let mut out_done = false;
            let mut err_done = false;
            while !(out_done && err_done) {
                tokio::select! {
                    n = stdout.read(&mut out_buf), if !out_done => {
                        match n {
                            Ok(0) => out_done = true,
                            Ok(n) => {
                                if logs.len() + n <= cap {
                                    logs.extend_from_slice(&out_buf[..n]);
                                }
                            }
                            Err(_) => out_done = true,
                        }
                    }
                    n = stderr.read(&mut err_buf), if !err_done => {
                        match n {
                            Ok(0) => err_done = true,
                            Ok(n) => {
                                if logs.len() + n <= cap {
                                    logs.extend_from_slice(&err_buf[..n]);
                                }
                            }
                            Err(_) => err_done = true,
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

    pub(super) fn shell_quote(s: &str) -> String {
        // Naive single-quote escaping good enough for repo URLs / git refs.
        let escaped = s.replace('\'', "'\\''");
        format!("'{escaped}'")
    }

    /// Fixed guest path the walkthrough writes the JUnit report to.
    pub(super) const MAESTRO_GUEST_JUNIT: &str = "/tmp/iogrid-maestro/maestro-junit.xml";
    /// Fixed guest dir the walkthrough writes Maestro screenshots to.
    pub(super) const MAESTRO_GUEST_SCREENSHOT_DIR: &str = "/tmp/iogrid-maestro/screenshots";

    /// Build the guest-side shell snippet that boots a simulator, installs
    /// the built `.app`, and runs the Maestro suite under the outer
    /// restart loop. The script is intentionally `set +e` around Maestro so
    /// a flow failure is captured in the exit code rather than aborting.
    pub(super) fn maestro_remote_script(cfg: &MaestroWalkthrough) -> String {
        // NOTE: `app_locator` / `flow_path` may contain a `$HOME`-prefixed
        // glob — we deliberately do NOT shell-quote those so the guest shell
        // expands them. `bundle_id` is a plain reverse-DNS string (safe).
        let app_locator = cfg.app_locator();
        let flow_path = cfg.flow_path();
        let bundle_id = cfg.bundle_id();
        format!(
            r#"set -uo pipefail
SCREEN_DIR={screen_dir}
JUNIT_OUT={junit_out}
mkdir -p "$SCREEN_DIR" "$(dirname "$JUNIT_OUT")"

# Resolve the built simulator .app (explicit path or first glob match).
APP=$(ls -d {app_locator} 2>/dev/null | head -n1 || true)
if [ -z "$APP" ] || [ ! -d "$APP" ]; then
  echo "maestro: no .app found at {app_locator}" >&2
  exit 64
fi
echo "maestro: app=$APP"

# Pick the newest available iPhone device type + iOS runtime.
DEVICE_TYPE=$(xcrun simctl list devicetypes -j | python3 -c "
import json,sys
d=json.load(sys.stdin)
for t in d['devicetypes']:
    if 'iPhone-16' in t['identifier'] or 'iPhone-15' in t['identifier']:
        print(t['identifier']); break
")
RUNTIME=$(xcrun simctl list runtimes -j | python3 -c "
import json,sys
d=json.load(sys.stdin)
ios=[r for r in d['runtimes'] if r.get('isAvailable') and 'iOS' in r.get('name','')]
ios.sort(key=lambda r:r['version'],reverse=True)
print(ios[0]['identifier'] if ios else '')
")
if [ -z "$DEVICE_TYPE" ] || [ -z "$RUNTIME" ]; then
  echo "maestro: no usable simulator device-type/runtime" >&2
  exit 65
fi

UDID=$(xcrun simctl create "iogrid-maestro-$$" "$DEVICE_TYPE" "$RUNTIME")
echo "maestro: udid=$UDID"
cleanup() {{ xcrun simctl delete "$UDID" >/dev/null 2>&1 || true; }}
trap cleanup EXIT
xcrun simctl boot "$UDID" || true
xcrun simctl bootstatus "$UDID" -b || true
xcrun simctl install "$UDID" "$APP"

export MAESTRO_DRIVER_STARTUP_TIMEOUT=600000
PATH="$HOME/.maestro/bin:$PATH"; export PATH

# OUTER RESTART LOOP — a fresh `maestro` invocation is the ONLY cure for the
# stale XCUITest handle (error 10001 -> fatal AppCrash). Re-run only on the
# 'App crashed or stopped' signature so real assertion failures don't burn
# two more full chains. (CONTRIBUTING gotcha 23; memory
# feedback_maestro_stale_xctest_handle.)
MAESTRO_EXIT=1
for ATTEMPT in 1 2 3; do
  echo "maestro-attempt=$ATTEMPT"
  xcrun simctl terminate "$UDID" {bundle_id} >/dev/null 2>&1 || true
  sleep 3
  set +e
  ( cd "$SCREEN_DIR" && maestro --device "$UDID" test \
      --format=junit --output="$JUNIT_OUT" \
      {flow_path} ) 2>&1 | tee "/tmp/iogrid-maestro/stdout-$ATTEMPT.log"
  MAESTRO_EXIT=${{PIPESTATUS[0]}}
  set -e
  [ "$MAESTRO_EXIT" -eq 0 ] && break
  grep -q "App crashed or stopped" "/tmp/iogrid-maestro/stdout-$ATTEMPT.log" || break
done
echo "maestro-exit=$MAESTRO_EXIT"
exit "$MAESTRO_EXIT"
"#,
            screen_dir = MAESTRO_GUEST_SCREENSHOT_DIR,
            junit_out = MAESTRO_GUEST_JUNIT,
            app_locator = app_locator,
            flow_path = flow_path,
            bundle_id = bundle_id,
        )
    }

    /// Parse the `maestro-attempt=N` bookkeeping lines from the captured
    /// walkthrough logs, returning the highest attempt index seen (1..=3),
    /// or `0` when Maestro never started.
    pub(super) fn parse_maestro_attempts(logs: &[u8]) -> u32 {
        let text = String::from_utf8_lossy(logs);
        text.lines()
            .filter_map(|l| l.trim().strip_prefix("maestro-attempt="))
            .filter_map(|n| n.trim().parse::<u32>().ok())
            .max()
            .unwrap_or(0)
    }
}

#[cfg(target_os = "macos")]
mod native_impl {
    //! Host-direct driver — clone + `xcodebuild` straight on the provider
    //! Mac. Mirrors the Tart driver's step semantics (same shell-snippet
    //! shape, same best-effort artifact/upload, the identical Maestro
    //! script) minus the VM: the walkthrough runs locally and its /tmp
    //! output paths are read directly instead of `scp`'d out.

    use super::tart_impl::{
        maestro_remote_script, parse_maestro_attempts, shell_quote, MAESTRO_GUEST_JUNIT,
        MAESTRO_GUEST_SCREENSHOT_DIR,
    };
    use super::*;
    use std::process::Stdio;
    use tokio::io::AsyncReadExt;
    use tokio::process::Command;

    pub(super) struct NativeDriver {
        pub work_root: PathBuf,
    }

    impl NativeDriver {
        pub async fn run(&self, w: IosBuildWorkload) -> Result<IosBuildResult, IosBuildError> {
            // 0. A usable Xcode toolchain is the native equivalent of the
            // VM base image: hard-require it up front.
            if !xcode_present().await {
                return Err(IosBuildError::XcodeMissing);
            }

            // 1. Per-build workspace.
            let ws = self.work_root.join(format!("iogridd-ios-{}", w.id));
            tokio::fs::create_dir_all(&ws)
                .await
                .map_err(|e| IosBuildError::Io(format!("mkdir {}: {e}", ws.display())))?;

            // 2. Clone + checkout + build — same snippet shape as the Tart
            // driver's in-VM command, rooted at the workspace instead of the
            // VM's $HOME/build. Non-zero exit returns BuildFailed (parity
            // with ssh_run).
            let snippet = format!(
                "set -euo pipefail; cd {ws}; \
                 if [ ! -d repo ]; then git clone --depth 50 {repo} repo; fi; \
                 cd repo && git fetch origin {gref} && git checkout {gref}; \
                 {cmd}",
                ws = shell_quote(&ws.to_string_lossy()),
                repo = shell_quote(&w.repo_url),
                gref = shell_quote(&w.git_ref),
                cmd = w.build_command,
            );
            let (exit_code, logs) = run_local(&snippet).await?;

            // 3. Artifact: relative paths resolve against the repo checkout;
            // absolute paths are used as-is. (No VM-convention default — a
            // native workload spec must say where its artifact lands.)
            let artifact_local = {
                let p = std::path::Path::new(w.artifact_path());
                if p.is_absolute() {
                    p.to_path_buf()
                } else {
                    ws.join("repo").join(p)
                }
            };

            // 4. Upload (best-effort, same as the Tart driver's step 8).
            if artifact_local.exists() && !w.upload_url.is_empty() {
                if let Err(e) = upload_artifact(&artifact_local, &w.upload_url).await {
                    tracing::warn!(%e, "artifact upload failed");
                }
            }

            // 5. Optional Maestro walkthrough — the same script the Tart
            // driver ships into the VM, run locally against the host's
            // simctl + maestro.
            let maestro_result = match &w.maestro {
                Some(cfg) => match self.maestro_walkthrough(&w, cfg).await {
                    Ok(r) => Some(r),
                    Err(e) => {
                        tracing::warn!(%e, "maestro walkthrough failed (non-fatal)");
                        Some(MaestroResult {
                            exit_code: -1,
                            attempts: 0,
                            junit_local_path: None,
                            screenshots_local_dir: None,
                        })
                    }
                },
                None => None,
            };

            Ok(IosBuildResult {
                id: w.id,
                exit_code,
                logs,
                artifact_local_path: artifact_local.exists().then_some(artifact_local),
                timed_out: false,
                maestro: maestro_result,
            })
        }

        async fn maestro_walkthrough(
            &self,
            w: &IosBuildWorkload,
            cfg: &MaestroWalkthrough,
        ) -> Result<MaestroResult, IosBuildError> {
            let script = maestro_remote_script(cfg);
            tracing::info!(id = %w.id, "running maestro walkthrough natively on host");
            let (code, logs) = run_local_no_fail(&script).await?;
            let attempts = parse_maestro_attempts(&logs);
            let junit = std::path::Path::new(MAESTRO_GUEST_JUNIT);
            let screenshots = std::path::Path::new(MAESTRO_GUEST_SCREENSHOT_DIR);
            Ok(MaestroResult {
                exit_code: code,
                attempts,
                junit_local_path: junit.exists().then(|| junit.to_path_buf()),
                screenshots_local_dir: screenshots.exists().then(|| screenshots.to_path_buf()),
            })
        }
    }

    /// `true` when a local Xcode toolchain is selected (`xcode-select -p`
    /// exits 0).
    async fn xcode_present() -> bool {
        Command::new("xcode-select")
            .arg("-p")
            .stdout(Stdio::null())
            .stderr(Stdio::null())
            .status()
            .await
            .map(|s| s.success())
            .unwrap_or(false)
    }

    /// Run `snippet` via `/bin/bash -lc`; non-zero exit is
    /// [`IosBuildError::BuildFailed`] (parity with the Tart driver's
    /// `ssh_run`).
    async fn run_local(snippet: &str) -> Result<(i32, Vec<u8>), IosBuildError> {
        let (code, logs) = run_local_no_fail(snippet).await?;
        if code != 0 {
            return Err(IosBuildError::BuildFailed(code));
        }
        Ok((code, logs))
    }

    /// Run `snippet` via `/bin/bash -lc`, capturing interleaved
    /// stdout+stderr capped at 1 MiB; the exit code is returned, never an
    /// error (Maestro flow failures are data, not transport errors).
    async fn run_local_no_fail(snippet: &str) -> Result<(i32, Vec<u8>), IosBuildError> {
        const CAP: usize = 1024 * 1024;
        let mut child = Command::new("/bin/bash")
            .args(["-lc", snippet])
            .stdin(Stdio::null())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped())
            .spawn()
            .map_err(|e| IosBuildError::Io(format!("spawn build shell: {e}")))?;
        let mut logs: Vec<u8> = Vec::new();
        let mut out = child.stdout.take();
        let mut err = child.stderr.take();
        let mut out_buf = [0u8; 8192];
        let mut err_buf = [0u8; 8192];
        let (mut out_done, mut err_done) = (out.is_none(), err.is_none());
        while !(out_done && err_done) {
            tokio::select! {
                r = async { out.as_mut().unwrap().read(&mut out_buf).await }, if !out_done => {
                    match r {
                        Ok(0) => out_done = true,
                        Ok(n) => {
                            if logs.len() + n <= CAP {
                                logs.extend_from_slice(&out_buf[..n]);
                            }
                        }
                        Err(_) => out_done = true,
                    }
                }
                r = async { err.as_mut().unwrap().read(&mut err_buf).await }, if !err_done => {
                    match r {
                        Ok(0) => err_done = true,
                        Ok(n) => {
                            if logs.len() + n <= CAP {
                                logs.extend_from_slice(&err_buf[..n]);
                            }
                        }
                        Err(_) => err_done = true,
                    }
                }
            }
        }
        let status = child
            .wait()
            .await
            .map_err(|e| IosBuildError::Io(format!("wait build shell: {e}")))?;
        Ok((status.code().unwrap_or(-1), logs))
    }

    /// `curl --upload-file` to the coordinator's pre-signed PUT URL (same
    /// mechanism as the Tart driver).
    async fn upload_artifact(path: &std::path::Path, url: &str) -> Result<(), IosBuildError> {
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
            maestro: None,
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

    #[test]
    fn maestro_walkthrough_defaults_resolve() {
        let m = MaestroWalkthrough {
            app_guest_path: String::new(),
            app_bundle_id: String::new(),
            flow_guest_path: String::new(),
        };
        assert_eq!(m.bundle_id(), MaestroWalkthrough::DEFAULT_BUNDLE_ID);
        assert_eq!(m.flow_path(), MaestroWalkthrough::DEFAULT_FLOW);
        assert_eq!(m.app_locator(), MaestroWalkthrough::DEFAULT_APP_GLOB);
        assert!(m.flow_path().ends_with("00-all.yaml"));
        assert_eq!(m.bundle_id(), "io.iogrid.app");
    }

    #[test]
    fn maestro_walkthrough_explicit_overrides() {
        let m = MaestroWalkthrough {
            app_guest_path: "/explicit/iogrid.app".into(),
            app_bundle_id: "com.example.app".into(),
            flow_guest_path: "/explicit/flow.yaml".into(),
        };
        assert_eq!(m.app_locator(), "/explicit/iogrid.app");
        assert_eq!(m.bundle_id(), "com.example.app");
        assert_eq!(m.flow_path(), "/explicit/flow.yaml");
    }

    #[test]
    fn workload_deserializes_without_maestro_field() {
        // The coordinator's convert.rs JSON path omits `maestro` for builds
        // that don't request a walkthrough; `#[serde(default)]` must accept
        // the older shape.
        let j = r#"{
            "id":"00000000-0000-0000-0000-000000000000",
            "tart_image":"img","repo_url":"r","git_ref":"main",
            "build_command":"x","artifact_guest_path":"","upload_url":"",
            "cpu":4,"memory_mib":8192,"timeout_secs":1800,"boot_timeout_secs":300
        }"#;
        let w: IosBuildWorkload = serde_json::from_str(j).expect("decode without maestro");
        assert!(w.maestro.is_none());
    }

    #[test]
    fn workload_deserializes_with_maestro_field() {
        let j = r#"{
            "id":"00000000-0000-0000-0000-000000000000",
            "tart_image":"img","repo_url":"r","git_ref":"main",
            "build_command":"x","artifact_guest_path":"","upload_url":"",
            "cpu":4,"memory_mib":8192,"timeout_secs":1800,"boot_timeout_secs":300,
            "maestro":{"app_bundle_id":"io.iogrid.app"}
        }"#;
        let w: IosBuildWorkload = serde_json::from_str(j).expect("decode with maestro");
        let m = w.maestro.expect("maestro present");
        assert_eq!(m.bundle_id(), "io.iogrid.app");
        // Omitted sub-fields fall back to defaults.
        assert_eq!(m.flow_path(), MaestroWalkthrough::DEFAULT_FLOW);
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn maestro_script_encodes_outer_restart_loop_and_gotchas() {
        let m = MaestroWalkthrough {
            app_guest_path: String::new(),
            app_bundle_id: String::new(),
            flow_guest_path: String::new(),
        };
        let s = tart_impl::maestro_remote_script(&m);
        // Outer restart loop over 3 attempts.
        assert!(s.contains("for ATTEMPT in 1 2 3"));
        // Gated on the stale-XCTest "App crashed or stopped" signature.
        assert!(s.contains("App crashed or stopped"));
        // simctl create/boot/install present.
        assert!(s.contains("simctl create"));
        assert!(s.contains("simctl boot"));
        assert!(s.contains("simctl install"));
        // maestro junit run present.
        assert!(s.contains("--format=junit"));
        assert!(s.contains("00-all.yaml"));
        // No assertVisible timeout key smuggled in.
        assert!(!s.contains("timeout:"));
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn parse_maestro_attempts_reads_highest() {
        let logs = b"maestro-attempt=1\nfoo\nmaestro-attempt=2\nmaestro-exit=0\n";
        assert_eq!(tart_impl::parse_maestro_attempts(logs), 2);
        assert_eq!(tart_impl::parse_maestro_attempts(b"no markers"), 0);
    }
}
