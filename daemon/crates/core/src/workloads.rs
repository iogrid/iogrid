//! Workload dispatch routing for the supervisor.
//!
//! The transport crate delivers `DispatchFrame::Assignment` envelopes whose
//! `workload_type` is one of `BANDWIDTH | DOCKER | GPU | IOS_BUILD`. The
//! supervisor decodes the per-type `payload_json` into the runner-specific
//! workload struct and dispatches to the matching runner crate.
//!
//! Concrete responsibilities of [`WorkloadRouter`]:
//!
//! 1. Decode the assignment payload + JSON-deserialise into the runner type.
//! 2. Allocate an [`ActiveAssignment`] tracking entry (workload_id →
//!    abort_handle + started_at) so we can revoke / report progress.
//! 3. Spawn the runner future on the tokio runtime and stream its
//!    `Update` frames back through the supplied
//!    [`tokio::sync::mpsc::Sender<DispatchFrame>`].
//! 4. On `Cancel` / `Drain` / `Paused`: abort all in-flight runners and emit
//!    one final `Update { status = "cancelled" }` per workload.
//!
//! The router is intentionally generic over the runner types so tests can
//! plug in mock impls.

use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;

use chrono::Utc;
use iogrid_scheduler::{SchedulerHandle, State as SchedState};
use iogrid_transport::DispatchFrame;
use iogrid_workload_docker::{DockerRunner, DockerWorkload, ScaffoldDockerRunner};
use iogrid_workload_gpu::{GpuRunner, GpuWorkload, NoopGpuRunner};
use iogrid_workload_ios::{IosBuildRunner, IosBuildWorkload};
use parking_lot::Mutex;
use serde::Deserialize;
use tokio::sync::mpsc;
use tokio::task::AbortHandle;

/// Slug values for the `workload_type` field of `DispatchFrame::Assignment`.
pub mod workload_type {
    /// Bandwidth-share workload (handled by routing crate, not this router).
    pub const BANDWIDTH: &str = "BANDWIDTH";
    /// Docker container workload.
    pub const DOCKER: &str = "DOCKER";
    /// GPU container workload.
    pub const GPU: &str = "GPU";
    /// macOS / iOS xcodebuild workload via Tart.
    pub const IOS_BUILD: &str = "IOS_BUILD";
}

/// One in-flight workload — tracked so we can revoke on pause / drain.
#[derive(Debug)]
pub struct ActiveAssignment {
    /// Coordinator-assigned workload id.
    pub workload_id: String,
    /// Per-attempt id.
    pub attempt_id: String,
    /// Workload type slug.
    pub workload_type: String,
    /// When the supervisor accepted the assignment.
    pub started_at: Instant,
    /// Abort handle on the runner task.
    pub abort: AbortHandle,
}

/// Sharable in-memory registry of active assignments.
#[derive(Debug, Default, Clone)]
pub struct ActiveRegistry {
    inner: Arc<Mutex<HashMap<String, ActiveAssignment>>>,
}

impl ActiveRegistry {
    /// Insert a new active assignment.
    pub fn insert(&self, a: ActiveAssignment) {
        self.inner.lock().insert(a.workload_id.clone(), a);
    }
    /// Remove the entry for `workload_id` (called when the runner finishes).
    pub fn remove(&self, workload_id: &str) -> Option<ActiveAssignment> {
        self.inner.lock().remove(workload_id)
    }
    /// Drain all entries — used on `Drain` / pause.
    pub fn drain_all(&self) -> Vec<ActiveAssignment> {
        let mut g = self.inner.lock();
        g.drain().map(|(_, v)| v).collect()
    }
    /// Number of in-flight workloads.
    pub fn len(&self) -> usize {
        self.inner.lock().len()
    }
    /// `true` if there are no in-flight workloads.
    pub fn is_empty(&self) -> bool {
        self.inner.lock().is_empty()
    }
    /// Look up the workload_id for a given attempt_id. O(n) scan — n is
    /// bounded by max_concurrent (≤16 in practice). Used by TunnelManager
    /// to attribute bytes_in/bytes_out to the correct workload on close.
    pub fn workload_id_for_attempt(&self, attempt_id: &str) -> Option<String> {
        self.inner
            .lock()
            .values()
            .find(|a| a.attempt_id == attempt_id)
            .map(|a| a.workload_id.clone())
    }

    /// Snapshot the in-flight workload ids (for heartbeat reporting).
    pub fn snapshot(&self) -> Vec<(String, String, String)> {
        self.inner
            .lock()
            .values()
            .map(|a| {
                (
                    a.workload_id.clone(),
                    a.attempt_id.clone(),
                    a.workload_type.clone(),
                )
            })
            .collect()
    }
}

/// Trait erasure for the runner trio. The router holds one `Arc` of each;
/// tests can plug in stubs that record what was dispatched without spinning
/// up a real Docker daemon / Tart VM.
#[derive(Clone)]
pub struct WorkloadRouterRunners {
    /// Docker runner.
    pub docker: Arc<dyn DockerRunner>,
    /// GPU runner.
    pub gpu: Arc<dyn GpuRunner>,
    /// iOS-build runner.
    pub ios: Arc<dyn IosBuildRunner>,
}

impl std::fmt::Debug for WorkloadRouterRunners {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("WorkloadRouterRunners").finish()
    }
}

impl WorkloadRouterRunners {
    /// Build the default scaffold trio (works on every target, no vendor
    /// deps). The production wiring swaps these out for the bollard /
    /// nvidia / Tart-backed concrete types when the `*-real` features are on.
    /// The iOS runner is host-aware: Tart when the CLI is present, the
    /// native host-direct runner otherwise (see `auto_runner`).
    pub fn scaffold() -> Self {
        Self {
            docker: Arc::new(ScaffoldDockerRunner::default()),
            gpu: Arc::new(NoopGpuRunner),
            ios: iogrid_workload_ios::auto_runner(),
        }
    }
}

/// Workload router — decodes assignment frames, dispatches to the right
/// runner, and surfaces status updates back to the coordinator.
pub struct WorkloadRouter {
    runners: WorkloadRouterRunners,
    registry: ActiveRegistry,
    /// Outbound channel — daemon → coordinator dispatch frames.
    outbound: mpsc::Sender<DispatchFrame>,
    /// Scheduler (so we can refuse new assignments while paused).
    scheduler: SchedulerHandle,
}

impl std::fmt::Debug for WorkloadRouter {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("WorkloadRouter")
            .field("active", &self.registry.len())
            .finish()
    }
}

impl WorkloadRouter {
    /// New router.
    pub fn new(
        runners: WorkloadRouterRunners,
        outbound: mpsc::Sender<DispatchFrame>,
        scheduler: SchedulerHandle,
    ) -> Self {
        Self {
            runners,
            registry: ActiveRegistry::default(),
            outbound,
            scheduler,
        }
    }

    /// Borrow the active registry (for the heartbeat pump / UI bridge).
    pub fn registry(&self) -> ActiveRegistry {
        self.registry.clone()
    }

    /// Handle a single dispatch frame received from the coordinator.
    pub async fn handle(&self, frame: DispatchFrame) {
        match frame {
            DispatchFrame::Assignment {
                workload_id,
                attempt_id,
                workload_type,
                deadline_rfc3339: _,
                dispatch_token: _,
                payload_json,
            } => {
                // iOS builds are delivered AND driven by the poll path
                // (build_poller, #705) — the canonical, reliable dispatch for
                // them since the stream drops their assignments at the edge.
                // The stream path must neither run nor reject an iOS build:
                //   * running it here too would double-build the same attempt
                //     (the poll picks up the identical Assignment row), and
                //   * the old scheduler-pause gate below rejected it with
                //     "scheduler_paused" (a bandwidth/idle-sharing policy that
                //     has no business blocking customer-paid compute). That
                //     terminal rejection raced ahead of the poll's success, so
                //     the build showed "rejected/-1" and $GRID never settled.
                // Leave it to the poll; the stream stays out of the way (#740).
                if workload_type == workload_type::IOS_BUILD {
                    return;
                }
                if !matches!(self.scheduler.current(), SchedState::Active) {
                    self.send_rejection(&workload_id, &attempt_id, "scheduler_paused")
                        .await;
                    return;
                }
                self.dispatch_assignment(workload_id, attempt_id, workload_type, payload_json)
                    .await;
            }
            DispatchFrame::Cancel { workload_id } => {
                self.cancel_one(&workload_id, "cancelled_by_coordinator")
                    .await;
            }
            DispatchFrame::Drain => {
                self.revoke_all("drain").await;
            }
            DispatchFrame::Ping { .. } | DispatchFrame::CoordinatorHello { .. } => {}
            // Outbound-only variants — should never be received.
            DispatchFrame::DaemonHello { .. } | DispatchFrame::Update { .. } => {}
            // Tunnel frames are routed by the supervisor's dispatch loop
            // directly into the TunnelManager BEFORE reaching this router
            // (see core/src/lib.rs around the daemon_side.rx loop). They
            // appear here only on the in-process loopback fast path used
            // by tests; safe to ignore — the loopback doesn't have a
            // TunnelManager wired so any byte forwarding would be a no-op
            // anyway.
            DispatchFrame::TunnelOpen { .. }
            | DispatchFrame::TunnelData { .. }
            | DispatchFrame::TunnelClose { .. } => {}
        }
    }

    /// Revoke every in-flight assignment with the supplied reason slug. Used
    /// when the scheduler flips to `Paused`.
    pub async fn revoke_all(&self, reason: &str) {
        let drained = self.registry.drain_all();
        for entry in drained {
            entry.abort.abort();
            let update = DispatchFrame::Update {
                workload_id: entry.workload_id,
                attempt_id: entry.attempt_id,
                status: "cancelled".into(),
                observed_at_rfc3339: Utc::now().to_rfc3339(),
                note: Some(format!("revoked: {reason}")),
                bytes_in: 0,
                bytes_out: 0,
                exit_code: -1,
                logs_s3_key: None,
                rejection_reason: Some(reason.to_string()),
            };
            let _ = self.outbound.send(update).await;
        }
    }

    async fn cancel_one(&self, workload_id: &str, reason: &str) {
        if let Some(entry) = self.registry.remove(workload_id) {
            entry.abort.abort();
            let _ = self
                .outbound
                .send(DispatchFrame::Update {
                    workload_id: entry.workload_id,
                    attempt_id: entry.attempt_id,
                    status: "cancelled".into(),
                    observed_at_rfc3339: Utc::now().to_rfc3339(),
                    note: Some(format!("cancel: {reason}")),
                    bytes_in: 0,
                    bytes_out: 0,
                    exit_code: -1,
                    logs_s3_key: None,
                    rejection_reason: Some(reason.to_string()),
                })
                .await;
        }
    }

    async fn dispatch_assignment(
        &self,
        workload_id: String,
        attempt_id: String,
        workload_type: String,
        payload_json: String,
    ) {
        let wt = workload_type.as_str();
        let registry = self.registry.clone();
        let outbound = self.outbound.clone();
        let workload_id_for_log = workload_id.clone();
        let attempt_id_for_log = attempt_id.clone();

        let task = match wt {
            workload_type::DOCKER => {
                let docker = self.runners.docker.clone();
                match decode_or_reject::<DockerWorkload>(&payload_json) {
                    Ok(w) => tokio::spawn(async move {
                        let res = docker.run(w).await;
                        finish_update(
                            outbound,
                            workload_id_for_log,
                            attempt_id_for_log,
                            &res.as_ref().map(|r| r.exit_code).unwrap_or(-1),
                            res.as_ref().map(|r| r.timed_out).unwrap_or(false),
                            res.as_ref().err().map(|e| e.to_string()),
                            workload_type::DOCKER,
                        )
                        .await;
                    }),
                    Err(e) => {
                        self.send_rejection(&workload_id, &attempt_id, &e).await;
                        return;
                    }
                }
            }
            workload_type::GPU => {
                let gpu = self.runners.gpu.clone();
                match decode_or_reject::<GpuWorkload>(&payload_json) {
                    Ok(w) => tokio::spawn(async move {
                        let res = gpu.run(w).await;
                        finish_update(
                            outbound,
                            workload_id_for_log,
                            attempt_id_for_log,
                            &res.as_ref().map(|r| r.exit_code).unwrap_or(-1),
                            res.as_ref().map(|r| r.timed_out).unwrap_or(false),
                            res.as_ref().err().map(|e| e.to_string()),
                            workload_type::GPU,
                        )
                        .await;
                    }),
                    Err(e) => {
                        self.send_rejection(&workload_id, &attempt_id, &e).await;
                        return;
                    }
                }
            }
            workload_type::IOS_BUILD => {
                let ios = self.runners.ios.clone();
                match decode_or_reject::<IosBuildWorkload>(&payload_json) {
                    Ok(w) => tokio::spawn(async move {
                        let res = iogrid_workload_ios::run_with_timeout(&*ios, w).await;
                        finish_update(
                            outbound,
                            workload_id_for_log,
                            attempt_id_for_log,
                            &res.as_ref().map(|r| r.exit_code).unwrap_or(-1),
                            res.as_ref().map(|r| r.timed_out).unwrap_or(false),
                            res.as_ref().err().map(|e| e.to_string()),
                            workload_type::IOS_BUILD,
                        )
                        .await;
                    }),
                    Err(e) => {
                        self.send_rejection(&workload_id, &attempt_id, &e).await;
                        return;
                    }
                }
            }
            other => {
                self.send_rejection(
                    &workload_id,
                    &attempt_id,
                    &format!("unsupported_workload_type:{other}"),
                )
                .await;
                return;
            }
        };

        // Acknowledge that we accepted the assignment.
        let _ = self
            .outbound
            .send(DispatchFrame::Update {
                workload_id: workload_id.clone(),
                attempt_id: attempt_id.clone(),
                status: "running".into(),
                observed_at_rfc3339: Utc::now().to_rfc3339(),
                note: None,
                bytes_in: 0,
                bytes_out: 0,
                exit_code: 0,
                logs_s3_key: None,
                rejection_reason: None,
            })
            .await;

        registry.insert(ActiveAssignment {
            workload_id,
            attempt_id,
            workload_type,
            started_at: Instant::now(),
            abort: task.abort_handle(),
        });
    }

    async fn send_rejection(&self, workload_id: &str, attempt_id: &str, reason: &str) {
        let _ = self
            .outbound
            .send(DispatchFrame::Update {
                workload_id: workload_id.to_string(),
                attempt_id: attempt_id.to_string(),
                status: "rejected".into(),
                observed_at_rfc3339: Utc::now().to_rfc3339(),
                note: Some(reason.to_string()),
                bytes_in: 0,
                bytes_out: 0,
                exit_code: -1,
                logs_s3_key: None,
                rejection_reason: Some(reason.to_string()),
            })
            .await;
    }
}

fn decode_or_reject<'a, T>(payload_json: &'a str) -> Result<T, String>
where
    T: Deserialize<'a>,
{
    serde_json::from_str::<T>(payload_json).map_err(|e| format!("payload_decode_failed:{e}"))
}

async fn finish_update(
    outbound: mpsc::Sender<DispatchFrame>,
    workload_id: String,
    attempt_id: String,
    exit_code: &i32,
    timed_out: bool,
    err: Option<String>,
    _wt: &'static str,
) {
    let status = if let Some(e) = &err {
        if e.contains("TimedOut") || timed_out {
            "timed_out"
        } else {
            "failed"
        }
    } else if *exit_code == 0 {
        "succeeded"
    } else {
        "failed"
    };
    let _ = outbound
        .send(DispatchFrame::Update {
            workload_id,
            attempt_id,
            status: status.into(),
            observed_at_rfc3339: Utc::now().to_rfc3339(),
            note: err,
            bytes_in: 0,
            bytes_out: 0,
            exit_code: *exit_code,
            logs_s3_key: None,
            rejection_reason: None,
        })
        .await;
}

#[cfg(test)]
mod tests {
    use super::*;
    use iogrid_scheduler::SchedulerConfig;
    use iogrid_workload_docker::WorkloadResult;
    use iogrid_workload_ios::TartRunner;
    use std::sync::atomic::{AtomicU32, Ordering};

    fn paused_scheduler() -> SchedulerHandle {
        let h = SchedulerHandle::new(SchedulerConfig {
            idle_only: false,
            ..Default::default()
        });
        h.set_manual_pause(true);
        h
    }

    fn active_scheduler() -> SchedulerHandle {
        let h = SchedulerHandle::new(SchedulerConfig {
            idle_only: false,
            ..Default::default()
        });
        h.set_sensors(iogrid_scheduler::SensorSnapshot {
            idle_secs: u64::MAX,
            ..Default::default()
        });
        h
    }

    struct CountingDocker {
        calls: Arc<AtomicU32>,
    }
    #[async_trait::async_trait]
    impl DockerRunner for CountingDocker {
        async fn run(
            &self,
            workload: DockerWorkload,
        ) -> Result<WorkloadResult, iogrid_workload_docker::DockerError> {
            self.calls.fetch_add(1, Ordering::SeqCst);
            Ok(WorkloadResult {
                id: workload.id,
                exit_code: 0,
                logs: b"hello".to_vec(),
                timed_out: false,
            })
        }
    }

    fn router_with_counter(
        scheduler: SchedulerHandle,
    ) -> (
        WorkloadRouter,
        Arc<AtomicU32>,
        mpsc::Receiver<DispatchFrame>,
    ) {
        let calls = Arc::new(AtomicU32::new(0));
        let runners = WorkloadRouterRunners {
            docker: Arc::new(CountingDocker {
                calls: calls.clone(),
            }),
            gpu: Arc::new(NoopGpuRunner),
            ios: Arc::new(TartRunner::default()),
        };
        let (tx, rx) = mpsc::channel::<DispatchFrame>(32);
        (WorkloadRouter::new(runners, tx, scheduler), calls, rx)
    }

    #[tokio::test]
    async fn paused_scheduler_rejects_assignment() {
        let (router, calls, mut rx) = router_with_counter(paused_scheduler());
        router
            .handle(DispatchFrame::Assignment {
                workload_id: "w1".into(),
                attempt_id: "a1".into(),
                workload_type: "DOCKER".into(),
                deadline_rfc3339: "now".into(),
                dispatch_token: "".into(),
                payload_json: "{}".into(),
            })
            .await;
        let got = rx.recv().await.unwrap();
        match got {
            DispatchFrame::Update {
                status,
                rejection_reason,
                ..
            } => {
                assert_eq!(status, "rejected");
                assert_eq!(rejection_reason.as_deref(), Some("scheduler_paused"));
            }
            other => panic!("expected Update; got {other:?}"),
        }
        assert_eq!(calls.load(Ordering::SeqCst), 0);
    }

    // #740: the stream path must stay out of the way for iOS builds — they are
    // owned by the poll path (build_poller). With the scheduler paused, an
    // IOS_BUILD assignment on the stream must produce NO frame at all: no
    // "scheduler_paused" rejection (the split-brain that beat the poll's
    // success and left $GRID unsettled) and no run (which would double-build
    // the same attempt the poll already picks up).
    #[tokio::test]
    async fn stream_ignores_ios_build_assignment() {
        let (router, calls, mut rx) = router_with_counter(paused_scheduler());
        router
            .handle(DispatchFrame::Assignment {
                workload_id: "w1".into(),
                attempt_id: "a1".into(),
                workload_type: "IOS_BUILD".into(),
                deadline_rfc3339: "now".into(),
                dispatch_token: "".into(),
                payload_json: "{}".into(),
            })
            .await;
        // No outbound frame: the poll path, not the stream, drives iOS builds.
        assert!(
            rx.try_recv().is_err(),
            "stream must emit no frame for an iOS build (poll owns it)"
        );
        // And it never invoked a runner.
        assert_eq!(calls.load(Ordering::SeqCst), 0);
    }

    #[tokio::test]
    async fn active_docker_assignment_runs() {
        let (router, calls, mut rx) = router_with_counter(active_scheduler());
        let payload = serde_json::to_string(&DockerWorkload {
            id: uuid::Uuid::new_v4(),
            image: "ghcr.io/foo/bar:latest".into(),
            cmd: vec!["echo".into()],
            env: vec![],
            cpu_millis: 100,
            memory_mib: 64,
            timeout_secs: 30,
            network_name: None,
        })
        .unwrap();
        router
            .handle(DispatchFrame::Assignment {
                workload_id: "w1".into(),
                attempt_id: "a1".into(),
                workload_type: "DOCKER".into(),
                deadline_rfc3339: "now".into(),
                dispatch_token: "".into(),
                payload_json: payload,
            })
            .await;
        // Expect "running" then "succeeded".
        let f1 = rx.recv().await.unwrap();
        let f2 = tokio::time::timeout(std::time::Duration::from_secs(2), rx.recv())
            .await
            .unwrap()
            .unwrap();
        let (s1, s2) = match (f1, f2) {
            (
                DispatchFrame::Update { status: s1, .. },
                DispatchFrame::Update { status: s2, .. },
            ) => (s1, s2),
            x => panic!("got {x:?}"),
        };
        assert_eq!(s1, "running");
        assert_eq!(s2, "succeeded");
        assert_eq!(calls.load(Ordering::SeqCst), 1);
    }

    #[tokio::test]
    async fn unknown_workload_type_rejected() {
        let (router, _calls, mut rx) = router_with_counter(active_scheduler());
        router
            .handle(DispatchFrame::Assignment {
                workload_id: "w2".into(),
                attempt_id: "a2".into(),
                workload_type: "WAT".into(),
                deadline_rfc3339: "now".into(),
                dispatch_token: "".into(),
                payload_json: "{}".into(),
            })
            .await;
        let got = rx.recv().await.unwrap();
        match got {
            DispatchFrame::Update {
                status,
                rejection_reason,
                ..
            } => {
                assert_eq!(status, "rejected");
                assert!(rejection_reason
                    .unwrap()
                    .starts_with("unsupported_workload_type"));
            }
            x => panic!("unexpected {x:?}"),
        }
    }

    #[tokio::test]
    async fn revoke_all_aborts_and_emits_cancel_updates() {
        let scheduler = active_scheduler();
        // Slow runner so we have time to revoke.
        struct SlowDocker;
        #[async_trait::async_trait]
        impl DockerRunner for SlowDocker {
            async fn run(
                &self,
                workload: DockerWorkload,
            ) -> Result<WorkloadResult, iogrid_workload_docker::DockerError> {
                tokio::time::sleep(std::time::Duration::from_secs(60)).await;
                Ok(WorkloadResult {
                    id: workload.id,
                    exit_code: 0,
                    logs: vec![],
                    timed_out: false,
                })
            }
        }
        let runners = WorkloadRouterRunners {
            docker: Arc::new(SlowDocker),
            gpu: Arc::new(NoopGpuRunner),
            ios: Arc::new(TartRunner::default()),
        };
        let (tx, mut rx) = mpsc::channel::<DispatchFrame>(32);
        let router = WorkloadRouter::new(runners, tx, scheduler);

        let payload = serde_json::to_string(&DockerWorkload {
            id: uuid::Uuid::new_v4(),
            image: "ghcr.io/foo/bar:latest".into(),
            cmd: vec![],
            env: vec![],
            cpu_millis: 100,
            memory_mib: 64,
            timeout_secs: 999,
            network_name: None,
        })
        .unwrap();
        router
            .handle(DispatchFrame::Assignment {
                workload_id: "w-slow".into(),
                attempt_id: "a-slow".into(),
                workload_type: "DOCKER".into(),
                deadline_rfc3339: "now".into(),
                dispatch_token: "".into(),
                payload_json: payload,
            })
            .await;
        // Consume the "running" update.
        let _ = rx.recv().await.unwrap();
        assert_eq!(router.registry().len(), 1);

        router.revoke_all("test").await;
        // Should now receive a "cancelled" update.
        let f = tokio::time::timeout(std::time::Duration::from_secs(1), rx.recv())
            .await
            .unwrap()
            .unwrap();
        match f {
            DispatchFrame::Update { status, .. } => assert_eq!(status, "cancelled"),
            x => panic!("unexpected {x:?}"),
        }
        assert!(router.registry().is_empty());
    }
}
