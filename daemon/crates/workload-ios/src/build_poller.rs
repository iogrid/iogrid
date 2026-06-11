//! Poll-based iOS-build dispatch (#705).
//!
//! The bidi dispatch stream's server→client push (the `WorkloadAssignment`
//! frame) is dropped by the mothership Traefik edge for a REMOTE daemon —
//! only the first server→client frame (CoordinatorHello) traverses. The
//! VPN data plane works through the SAME edge because its daemon POLLS
//! `/assigned-sessions` (client→server requests traverse fine). This module
//! gives the iOS-build daemon the same escape hatch: it polls
//! `GET /v1/providers/{id}/assigned-workloads` (served by workloads-svc,
//! #714) and runs each newly-assigned build through the [`IosBuildRunner`]
//! (the native host-direct runner on a Mac).
//!
//! Mirrors `iogrid_routing::peer_binder`: same `reqwest::Client` +
//! `tokio::sync::watch` shutdown pattern, dedup guard, per-tick timeout.

use std::collections::HashSet;
use std::sync::Arc;
use std::time::Duration;

use serde::Deserialize;
use uuid::Uuid;

use crate::{run_with_timeout, IosBuildRunner, IosBuildWorkload};

/// How often to poll for new assignments.
pub const POLL_INTERVAL: Duration = Duration::from_secs(10);
/// Per-tick GET budget.
pub const TICK_TIMEOUT: Duration = Duration::from_secs(15);
/// Default wall-clock build time-box when the assignment doesn't pin one.
const DEFAULT_BUILD_TIMEOUT_SECS: u32 = 3600;
const DEFAULT_BOOT_TIMEOUT_SECS: u32 = 600;
const DEFAULT_MEMORY_MIB: u32 = 8192;

/// Config for the build poller.
#[derive(Debug, Clone)]
pub struct BuildPollerConfig {
    /// Provider UUID — appears in the GET URL.
    pub provider_id: String,
    /// Coordinator base URL, e.g. `https://api.iogrid.org`.
    pub coordinator_base_url: String,
}

// (struct/enum docs below satisfy the crate's deny(missing_docs).)

/// One assignment row from `GET /v1/providers/{id}/assigned-workloads`
/// (workloads-svc #714). Mirrors the JSON the handler emits.
#[derive(Debug, Clone, Deserialize)]
pub struct AssignedWorkload {
    /// Per-attempt id (the dispatch attempt that selected this provider).
    pub attempt_id: String,
    /// Coordinator workload id (for log/artifact correlation).
    #[serde(default)]
    pub workload_id: String,
    /// Git repo the build clones.
    #[serde(default)]
    pub repo_url: String,
    /// Branch/commit to check out.
    #[serde(default)]
    pub git_ref: String,
    /// Shell command run inside the checkout (xcodebuild etc.).
    #[serde(default)]
    pub build_command: String,
    /// Tart base image ref (ignored by the native runner).
    #[serde(default)]
    pub tart_image: String,
    /// Pre-signed PUT URL for artifact upload.
    #[serde(default)]
    pub upload_url: String,
    /// Path to the produced artifact inside the build workspace.
    #[serde(default)]
    pub artifact_guest_path: String,
    /// Requested CPU cores.
    #[serde(default)]
    pub cpu: u32,
}

/// Full body of the assigned-workloads poll response.
#[derive(Debug, Clone, Deserialize)]
pub struct AssignedWorkloadsResponse {
    /// Echoed provider id.
    #[serde(default)]
    pub provider_id: String,
    /// Number of assignments returned.
    #[serde(default)]
    pub count: usize,
    /// The pending assignments.
    #[serde(default)]
    pub assignments: Vec<AssignedWorkload>,
}

/// Errors from one poll cycle.
#[derive(Debug, thiserror::Error)]
pub enum PollError {
    /// Transport error issuing the GET.
    #[error("http error: {0}")]
    Http(#[source] reqwest::Error),
    /// The GET exceeded [`TICK_TIMEOUT`].
    #[error("poll timed out")]
    Timeout,
    /// Non-2xx response.
    #[error("bad status: {0}")]
    BadStatus(u16),
    /// Response body didn't decode as the expected JSON.
    #[error("parse error: {0}")]
    Parse(#[source] reqwest::Error),
}

impl AssignedWorkload {
    /// Project a poll row into the runner's [`IosBuildWorkload`]. The
    /// attempt_id is reused as the workload id so logs/artifacts correlate.
    fn to_workload(&self) -> IosBuildWorkload {
        let id = Uuid::parse_str(&self.attempt_id).unwrap_or_else(|_| Uuid::new_v4());
        IosBuildWorkload {
            id,
            tart_image: self.tart_image.clone(),
            repo_url: self.repo_url.clone(),
            git_ref: self.git_ref.clone(),
            build_command: self.build_command.clone(),
            artifact_guest_path: self.artifact_guest_path.clone(),
            upload_url: self.upload_url.clone(),
            cpu: self.cpu.max(1),
            memory_mib: DEFAULT_MEMORY_MIB,
            timeout_secs: DEFAULT_BUILD_TIMEOUT_SECS,
            boot_timeout_secs: DEFAULT_BOOT_TIMEOUT_SECS,
            maestro: None,
        }
    }
}

/// Spawn the poll loop. Shuts down cleanly when `shutdown_rx` flips true.
pub fn spawn_build_poller(
    config: BuildPollerConfig,
    http: reqwest::Client,
    runner: Arc<dyn IosBuildRunner>,
    shutdown_rx: tokio::sync::watch::Receiver<bool>,
) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        run_poll_loop(config, http, runner, shutdown_rx).await;
    })
}

async fn run_poll_loop(
    config: BuildPollerConfig,
    http: reqwest::Client,
    runner: Arc<dyn IosBuildRunner>,
    mut shutdown_rx: tokio::sync::watch::Receiver<bool>,
) {
    let base = config
        .coordinator_base_url
        .trim_end_matches('/')
        .to_string();
    let url = format!(
        "{}/v1/providers/{}/assigned-workloads",
        base, config.provider_id
    );
    let provider_id = config.provider_id.clone();
    // Dedup so a build that's still RUNNING (and thus still listed until the
    // status update lands server-side) isn't started twice.
    let mut started: HashSet<String> = HashSet::new();
    let mut ticker = tokio::time::interval(POLL_INTERVAL);
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
    tracing::info!(url = %url, "iOS-build poller started (#705 poll-based dispatch)");

    loop {
        tokio::select! {
            biased;
            res = shutdown_rx.changed() => {
                if res.is_err() || *shutdown_rx.borrow() {
                    tracing::debug!("build poller shutdown");
                    return;
                }
            }
            _ = ticker.tick() => {
                let resp = match poll_once(&http, &url).await {
                    Ok(r) => r,
                    Err(e) => {
                        tracing::warn!(error = %e, url = %url, "poll assigned-workloads failed; retry next tick");
                        continue;
                    }
                };
                for a in resp.assignments {
                    if started.contains(&a.attempt_id) {
                        continue;
                    }
                    started.insert(a.attempt_id.clone());
                    let runner = Arc::clone(&runner);
                    let attempt = a.attempt_id.clone();
                    let http2 = http.clone();
                    let status_url = format!(
                        "{}/v1/providers/{}/assigned-workloads/{}/status",
                        base, provider_id, attempt
                    );
                    // Run each build concurrently so the poll loop keeps
                    // ticking; the dedup guard prevents a re-start.
                    tokio::spawn(async move {
                        tracing::info!(attempt_id = %attempt, repo = %a.repo_url, "iOS build picked up via poll — running");
                        // Report RUNNING so the assignment drains off the
                        // server's "dispatched" poll list immediately (#705).
                        report_status(&http2, &status_url, "running", 0).await;
                        let (status, code) = match run_with_timeout(runner.as_ref(), a.to_workload()).await {
                            Ok(res) => {
                                tracing::info!(attempt_id = %attempt, exit_code = res.exit_code, "iOS build finished");
                                (if res.exit_code == 0 { "succeeded" } else { "failed" }, res.exit_code)
                            }
                            Err(e) => {
                                tracing::warn!(attempt_id = %attempt, error = %e, "iOS build failed");
                                ("failed", 1)
                            }
                        };
                        report_status(&http2, &status_url, status, code).await;
                    });
                }
            }
        }
    }
}

/// POST the build outcome so the assignment drains off the server's
/// "dispatched" poll list (#705). Best-effort: a failure only means the
/// assignment lingers + is re-served, which the local dedup guard already
/// prevents within this daemon's lifetime — so we log + move on.
async fn report_status(http: &reqwest::Client, url: &str, status: &str, exit_code: i32) {
    let body = serde_json::json!({ "status": status, "exit_code": exit_code });
    match tokio::time::timeout(TICK_TIMEOUT, http.post(url).json(&body).send()).await {
        Ok(Ok(r)) if r.status().is_success() => {}
        Ok(Ok(r)) => {
            tracing::warn!(url = %url, status = %r.status(), "report build status: non-2xx")
        }
        Ok(Err(e)) => tracing::warn!(url = %url, error = %e, "report build status failed"),
        Err(_) => tracing::warn!(url = %url, "report build status timed out"),
    }
}

/// One GET + parse cycle, capped at [`TICK_TIMEOUT`].
async fn poll_once(
    http: &reqwest::Client,
    url: &str,
) -> Result<AssignedWorkloadsResponse, PollError> {
    let fut = http.get(url).send();
    let resp = match tokio::time::timeout(TICK_TIMEOUT, fut).await {
        Ok(Ok(r)) => r,
        Ok(Err(e)) => return Err(PollError::Http(e)),
        Err(_) => return Err(PollError::Timeout),
    };
    if !resp.status().is_success() {
        return Err(PollError::BadStatus(resp.status().as_u16()));
    }
    resp.json::<AssignedWorkloadsResponse>()
        .await
        .map_err(PollError::Parse)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_assigned_workloads_response() {
        let body = r#"{
            "provider_id": "c0138910-9f41-4a05-972f-c6915760e0f0",
            "count": 1,
            "assignments": [{
                "attempt_id": "eebdb9de-044d-483a-af85-a09e09ae8467",
                "workload_id": "wl-1",
                "repo_url": "https://github.com/iogrid/iogrid.git",
                "git_ref": "main",
                "build_command": "xcodebuild build",
                "cpu": 4
            }]
        }"#;
        let r: AssignedWorkloadsResponse = serde_json::from_str(body).expect("parses");
        assert_eq!(r.count, 1);
        assert_eq!(r.assignments.len(), 1);
        let a = &r.assignments[0];
        assert_eq!(a.attempt_id, "eebdb9de-044d-483a-af85-a09e09ae8467");
        assert_eq!(a.build_command, "xcodebuild build");
        // Projection reuses the attempt id as the workload id + applies defaults.
        let w = a.to_workload();
        assert_eq!(w.id.to_string(), a.attempt_id);
        assert_eq!(w.git_ref, "main");
        assert_eq!(w.cpu, 4);
        assert_eq!(w.timeout_secs, DEFAULT_BUILD_TIMEOUT_SECS);
    }

    // End-to-end (in-process): a mock HTTP server returns one assignment;
    // the poller must GET it and invoke the runner with that build command.
    // This is the daemon-side counterpart to the live server-side proof
    // (the prod poll endpoint returning a real assignment).
    #[tokio::test]
    async fn poller_runs_assignment_from_server() {
        use std::sync::Mutex;
        use tokio::io::{AsyncReadExt, AsyncWriteExt};
        use tokio::net::TcpListener;

        struct MockRunner {
            got: Arc<Mutex<Option<String>>>,
        }
        #[async_trait::async_trait]
        impl IosBuildRunner for MockRunner {
            async fn run(
                &self,
                w: IosBuildWorkload,
            ) -> Result<crate::IosBuildResult, crate::IosBuildError> {
                *self.got.lock().unwrap() = Some(w.build_command.clone());
                Ok(crate::IosBuildResult {
                    id: w.id,
                    exit_code: 0,
                    logs: vec![],
                    artifact_local_path: None,
                    timed_out: false,
                    maestro: None,
                })
            }
        }

        // Mock workloads-svc: answer one GET with the assignment JSON.
        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        tokio::spawn(async move {
            if let Ok((mut sock, _)) = listener.accept().await {
                let mut buf = [0u8; 2048];
                let _ = sock.read(&mut buf).await;
                let body = r#"{"count":1,"assignments":[{"attempt_id":"34516759-df8f-42ee-9dbb-eff9e199ab4c","repo_url":"https://github.com/iogrid/iogrid.git","git_ref":"main","build_command":"echo hi"}]}"#;
                let resp = format!(
                    "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                    body.len(),
                    body
                );
                let _ = sock.write_all(resp.as_bytes()).await;
            }
        });

        let got = Arc::new(Mutex::new(None));
        let runner = Arc::new(MockRunner { got: got.clone() });
        let (_tx, rx) = tokio::sync::watch::channel(false);
        // First interval tick fires immediately, so the poll happens at once.
        spawn_build_poller(
            BuildPollerConfig {
                provider_id: "p".into(),
                coordinator_base_url: format!("http://{addr}"),
            },
            reqwest::Client::new(),
            runner,
            rx,
        );

        // The runner should be invoked with the assignment's build command.
        for _ in 0..60 {
            if got.lock().unwrap().is_some() {
                break;
            }
            tokio::time::sleep(Duration::from_millis(50)).await;
        }
        assert_eq!(
            got.lock().unwrap().as_deref(),
            Some("echo hi"),
            "poller should have GET the assignment and run it via the runner"
        );
    }
}
