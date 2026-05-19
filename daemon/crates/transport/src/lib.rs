//! Transport layer — bidirectional gRPC stream to the iogrid coordinator.
//!
//! This crate wires the daemon to the coordinator over mTLS-secured gRPC.
//! Two long-lived bidirectional streams are maintained:
//!
//!  * **heartbeats** — `providers-svc.SchedulingService/StreamHeartbeats`
//!    The daemon emits a [`Heartbeat`] every ~5s carrying the current
//!    `SchedulerState` + `CurrentUsageSnapshot`.
//!  * **dispatch** — `workloads-svc.WorkloadDispatchService/Dispatch`
//!    Bidi pump for `DispatchFrame` envelopes (assignments, status updates,
//!    drains, cancellations).
//!
//! Implementation strategy
//! ----------------------
//!
//! We keep the wire-level types as small handwritten Rust structs that mirror
//! the proto shapes (see `proto/iogrid/providers/v1/scheduling.proto` and
//! `proto/iogrid/workloads/v1/dispatch.proto`).  This avoids forcing the
//! daemon CI matrix to ship a `protoc` binary while leaving an obvious seam
//! for a follow-up PR that swaps these for tonic-build-generated types
//! one-for-one.
//!
//! The reconnect engine ([`run_with_reconnect`]) and the mTLS channel
//! builder ([`Channel::connect`]) are fully real — these are the hard parts
//! of "always-on daemon talks to coordinator over an unreliable internet".

#![forbid(unsafe_code)]
#![deny(missing_docs)]

pub mod identity;
pub mod ruleset;

use std::path::PathBuf;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use thiserror::Error;
use tokio::sync::mpsc;
use tonic::transport::{Channel as TonicChannel, ClientTlsConfig, Endpoint, Identity};

/// All errors the transport layer surfaces upward.
#[derive(Debug, Error)]
pub enum TransportError {
    /// Coordinator URL failed to parse.
    #[error("invalid coordinator URL: {0}")]
    InvalidUrl(String),
    /// Identity bundle (cert + key) missing or unreadable.
    #[error("missing identity bundle at {path}: {source}")]
    MissingIdentity {
        /// Path that was attempted.
        path: PathBuf,
        /// Underlying I/O error.
        #[source]
        source: std::io::Error,
    },
    /// Generic identity-bundle error (parse / format).
    #[error("identity bundle invalid: {0}")]
    InvalidIdentity(String),
    /// Connection to coordinator failed.
    #[error("coordinator unreachable: {0}")]
    Unreachable(String),
    /// TLS setup failed.
    #[error("tls configuration failed: {0}")]
    TlsError(String),
}

/// Configuration the supervisor hands to [`Channel::connect`].
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectConfig {
    /// `https://coordinator.iogrid.org:443` style.
    pub coordinator_url: String,
    /// PEM file containing the daemon's identity cert.
    pub cert_pem: PathBuf,
    /// PEM file containing the daemon's identity private key.
    pub key_pem: PathBuf,
    /// Optional CA bundle override (otherwise system roots).
    pub ca_pem: Option<PathBuf>,
    /// Maximum reconnect backoff (capped per docs/TECH.md ‑ 60s).
    pub max_backoff: Duration,
    /// Initial reconnect backoff.
    pub initial_backoff: Duration,
}

impl Default for ConnectConfig {
    fn default() -> Self {
        let home = dirs_home().unwrap_or_else(|| PathBuf::from("/var/lib/iogrid"));
        Self {
            coordinator_url: "https://coordinator.iogrid.org:443".to_string(),
            cert_pem: home.join(".iogrid/cert.pem"),
            key_pem: home.join(".iogrid/key.pem"),
            ca_pem: None,
            max_backoff: Duration::from_secs(60),
            initial_backoff: Duration::from_secs(1),
        }
    }
}

fn dirs_home() -> Option<PathBuf> {
    std::env::var_os("HOME")
        .or_else(|| std::env::var_os("USERPROFILE"))
        .map(PathBuf::from)
}

/// Heartbeat frame mirroring `iogrid.providers.v1.Heartbeat`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Heartbeat {
    /// Provider id (UUID string).
    pub provider_id: String,
    /// State slug — matches scheduler enum slugs.
    pub state: String,
    /// Current CPU%.
    pub cpu_pct: u8,
    /// Current memory%.
    pub memory_pct: u8,
    /// Idle seconds.
    pub idle_secs: u64,
    /// Bandwidth bytes this month.
    pub bandwidth_bytes_this_month: u64,
    /// Monotonic sequence number.
    pub sequence: u64,
    /// Wall-clock at emission (RFC3339).
    pub emitted_at: String,
}

/// Server-side ack for a heartbeat.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct HeartbeatAck {
    /// If true, the daemon should refetch scheduling config.
    pub config_changed: bool,
    /// If true, coordinator wants us to pause for operations reasons.
    pub operations_pause: bool,
}

/// Dispatch frame — one of the oneof arms in proto's `DispatchFrame`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub enum DispatchFrame {
    /// Daemon → coordinator hello.
    DaemonHello {
        /// Provider id.
        provider_id: String,
        /// Eligible workload types this daemon can run.
        eligible_types: Vec<String>,
        /// Max concurrent workloads.
        max_concurrent: u32,
    },
    /// Coordinator → daemon ack of hello.
    CoordinatorHello {
        /// Provider id sanity-check.
        provider_id: String,
        /// Server's accepted_at timestamp.
        accepted_at: String,
    },
    /// Coordinator → daemon: take this workload.
    Assignment {
        /// Workload id.
        workload_id: String,
        /// Per-attempt id.
        attempt_id: String,
        /// Workload type ("BANDWIDTH"/"DOCKER"/"GPU"/"IOS_BUILD").
        workload_type: String,
        /// Deadline timestamp.
        deadline_rfc3339: String,
        /// Dispatch JWT.
        dispatch_token: String,
        /// Type-specific JSON payload.
        payload_json: String,
    },
    /// Daemon → coordinator: status of an attempt.
    Update {
        /// Workload id.
        workload_id: String,
        /// Attempt id.
        attempt_id: String,
        /// Status slug ("queued"/"dispatched"/"running"/"succeeded"/"failed"/"timed_out"/"cancelled"/"rejected").
        status: String,
        /// Observed-at timestamp.
        observed_at_rfc3339: String,
        /// Optional note.
        note: Option<String>,
        /// Bytes-in counter (bandwidth workloads).
        bytes_in: u64,
        /// Bytes-out counter (bandwidth workloads).
        bytes_out: u64,
        /// Process exit code.
        exit_code: i32,
        /// Logs S3 key (terminal updates).
        logs_s3_key: Option<String>,
        /// Rejection reason slug (when status = "rejected").
        rejection_reason: Option<String>,
    },
    /// Coordinator → daemon: cancel a workload.
    Cancel {
        /// Workload id to cancel.
        workload_id: String,
    },
    /// Liveness ping.
    Ping {
        /// Server-side timestamp.
        at_rfc3339: String,
    },
    /// Coordinator → daemon: drain (no new assignments, finish in-flight, disconnect).
    Drain,
}

/// Connection state.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ClientState {
    /// Never connected.
    Disconnected,
    /// Currently connected, bidi stream alive.
    Connected,
    /// Connection terminated, will retry per backoff.
    Reconnecting,
}

/// Coordinator gRPC channel — owns a tonic `Channel` once connected.
#[derive(Debug)]
pub struct Channel {
    cfg: ConnectConfig,
    state: ClientState,
    channel: Option<TonicChannel>,
}

impl Channel {
    /// Build a client. Does NOT open the network connection — call
    /// [`Self::connect`] to actually dial.
    pub fn new(cfg: ConnectConfig) -> Self {
        Self {
            cfg,
            state: ClientState::Disconnected,
            channel: None,
        }
    }

    /// Current connection state.
    pub fn state(&self) -> ClientState {
        self.state
    }

    /// Borrowed config.
    pub fn config(&self) -> &ConnectConfig {
        &self.cfg
    }

    /// Borrow the underlying tonic channel (if connected).
    pub fn inner(&self) -> Option<&TonicChannel> {
        self.channel.as_ref()
    }

    /// Open the gRPC channel with mTLS.
    ///
    /// Loads cert + key PEMs, builds a tonic `ClientTlsConfig`, dials the
    /// coordinator endpoint, and stores the resulting channel for callers
    /// that want to construct gRPC stubs against it.
    pub async fn connect(&mut self) -> Result<(), TransportError> {
        if !self.cfg.coordinator_url.starts_with("https://") {
            return Err(TransportError::InvalidUrl(self.cfg.coordinator_url.clone()));
        }
        let cert = read_pem(&self.cfg.cert_pem)?;
        let key = read_pem(&self.cfg.key_pem)?;
        let identity = Identity::from_pem(cert, key);
        let mut tls = ClientTlsConfig::new().identity(identity);
        if let Some(ca_path) = &self.cfg.ca_pem {
            let ca = read_pem(ca_path)?;
            tls = tls.ca_certificate(tonic::transport::Certificate::from_pem(ca));
        }
        let ep = Endpoint::from_shared(self.cfg.coordinator_url.clone())
            .map_err(|e| TransportError::InvalidUrl(e.to_string()))?
            .tls_config(tls)
            .map_err(|e| TransportError::TlsError(e.to_string()))?
            .timeout(Duration::from_secs(10))
            .tcp_keepalive(Some(Duration::from_secs(30)))
            .http2_keep_alive_interval(Duration::from_secs(15))
            .keep_alive_while_idle(true);
        let ch = ep
            .connect()
            .await
            .map_err(|e| TransportError::Unreachable(e.to_string()))?;
        self.channel = Some(ch);
        self.state = ClientState::Connected;
        tracing::info!(
            coordinator = %self.cfg.coordinator_url,
            "coordinator mTLS channel connected"
        );
        Ok(())
    }

    /// Tear down the connection.
    pub async fn close(&mut self) {
        self.channel = None;
        self.state = ClientState::Disconnected;
    }
}

fn read_pem(p: &std::path::Path) -> Result<Vec<u8>, TransportError> {
    std::fs::read(p).map_err(|source| TransportError::MissingIdentity {
        path: p.to_path_buf(),
        source,
    })
}

/// Reconnect-loop driver. Caller supplies a `connect_once` closure that
/// returns `Ok(())` on success and `Err(_)` on failure. Backoff doubles
/// from `initial_backoff` up to `max_backoff` (1s → 2 → 4 → 8 → 16 → 30 → 60).
/// On success, backoff resets.
pub async fn run_with_reconnect<F, Fut>(
    initial: Duration,
    cap: Duration,
    cancel: tokio::sync::watch::Receiver<bool>,
    mut connect_once: F,
) where
    F: FnMut() -> Fut,
    Fut: std::future::Future<Output = Result<(), TransportError>>,
{
    let mut backoff = initial;
    let mut cancel = cancel;
    loop {
        if *cancel.borrow() {
            return;
        }
        tokio::select! {
            res = connect_once() => match res {
                Ok(()) => {
                    backoff = initial;
                    tracing::debug!("connect_once returned Ok — stream closed cleanly, will reconnect");
                }
                Err(err) => {
                    tracing::warn!(%err, ?backoff, "coordinator stream failed, backing off");
                }
            },
            _ = cancel.changed() => {
                if *cancel.borrow() { return; }
            }
        }
        tokio::select! {
            _ = tokio::time::sleep(backoff) => {}
            _ = cancel.changed() => {
                if *cancel.borrow() { return; }
            }
        }
        backoff = (backoff * 2).min(cap);
    }
}

/// Heartbeat sink — abstracts where heartbeats are written so the supervisor
/// can plug in either the real `SchedulingService.StreamHeartbeats` client or
/// a test channel.
#[async_trait::async_trait]
pub trait HeartbeatSink: Send + Sync {
    /// Push one heartbeat. Returns the server ack.
    async fn push(&self, hb: Heartbeat) -> Result<HeartbeatAck, TransportError>;
}

/// In-memory heartbeat sink — used by tests.
#[derive(Debug, Clone, Default)]
pub struct MemSink {
    /// Heartbeats received so far.
    pub received: std::sync::Arc<parking_lot_compat::Mutex<Vec<Heartbeat>>>,
}

#[async_trait::async_trait]
impl HeartbeatSink for MemSink {
    async fn push(&self, hb: Heartbeat) -> Result<HeartbeatAck, TransportError> {
        self.received.lock().push(hb);
        Ok(HeartbeatAck::default())
    }
}

/// Spawn the heartbeat publisher. Polls the scheduler handle every `interval`
/// and pushes a [`Heartbeat`] into the sink. Returns the task handle.
pub fn spawn_heartbeat_pump<S: HeartbeatSink + 'static>(
    provider_id: String,
    scheduler: iogrid_scheduler::SchedulerHandle,
    sink: std::sync::Arc<S>,
    interval: Duration,
) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        let mut ticker = tokio::time::interval(interval);
        let mut seq: u64 = 0;
        loop {
            ticker.tick().await;
            seq = seq.wrapping_add(1);
            let st = scheduler.refresh();
            let sensors = scheduler.sensors();
            let state_slug = match &st {
                iogrid_scheduler::State::Active => "active".to_string(),
                iogrid_scheduler::State::Paused(r) => format!("paused_{}", r.slug()),
            };
            let hb = Heartbeat {
                provider_id: provider_id.clone(),
                state: state_slug,
                cpu_pct: sensors.cpu_used_pct,
                memory_pct: sensors.memory_used_pct,
                idle_secs: sensors.idle_secs,
                bandwidth_bytes_this_month: sensors.bandwidth_used_bytes,
                sequence: seq,
                emitted_at: chrono::Utc::now().to_rfc3339(),
            };
            match sink.push(hb).await {
                Ok(ack) => {
                    if ack.operations_pause {
                        scheduler.set_operations_pause(true);
                    } else {
                        scheduler.set_operations_pause(false);
                    }
                }
                Err(err) => {
                    tracing::warn!(%err, "heartbeat push failed; will retry next tick");
                }
            }
        }
    })
}

/// Dispatch channel — daemon-side bidi tx/rx pair.
///
/// `tx` is what the supervisor pushes status updates into; `rx` is where
/// coordinator-originated [`DispatchFrame`] arrive (Assignment, Cancel, Drain,
/// Ping). Production wires these to a tonic streaming RPC; tests can plug
/// in a pair directly.
pub struct DispatchChannel {
    /// Outbound (daemon → coordinator) sender.
    pub tx: mpsc::Sender<DispatchFrame>,
    /// Inbound (coordinator → daemon) receiver.
    pub rx: mpsc::Receiver<DispatchFrame>,
}

/// Construct an in-process loop-back dispatch channel for testing. Returns
/// the daemon side + a mirror handle the test can drive as if it were the
/// coordinator.
pub fn dispatch_loopback() -> (DispatchChannel, DispatchChannel) {
    let (a_tx, b_rx) = mpsc::channel(64);
    let (b_tx, a_rx) = mpsc::channel(64);
    (
        DispatchChannel { tx: a_tx, rx: a_rx },
        DispatchChannel { tx: b_tx, rx: b_rx },
    )
}

/// Tiny mutex shim — avoids pulling parking_lot into the public API of this crate.
mod parking_lot_compat {
    /// Mutex wrapper.
    #[derive(Debug, Default)]
    pub struct Mutex<T>(std::sync::Mutex<T>);

    impl<T> Mutex<T> {
        /// Acquire the lock (panics if poisoned, which only happens on a
        /// previous thread crash).
        pub fn lock(&self) -> std::sync::MutexGuard<'_, T> {
            self.0.lock().expect("transport MemSink mutex poisoned")
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_config_uses_iogrid_dir() {
        let c = ConnectConfig::default();
        assert!(c.coordinator_url.starts_with("https://"));
        assert!(c.cert_pem.to_string_lossy().contains(".iogrid"));
        assert!(c.key_pem.to_string_lossy().contains(".iogrid"));
        assert_eq!(c.max_backoff, Duration::from_secs(60));
    }

    #[tokio::test]
    async fn connect_rejects_plaintext_url() {
        let cfg = ConnectConfig {
            coordinator_url: "http://insecure.example".into(),
            ..ConnectConfig::default()
        };
        let mut c = Channel::new(cfg);
        let err = c.connect().await.unwrap_err();
        assert!(matches!(err, TransportError::InvalidUrl(_)));
    }

    #[tokio::test]
    async fn connect_reports_missing_identity() {
        let cfg = ConnectConfig {
            coordinator_url: "https://nope.invalid".into(),
            cert_pem: "/no/such/cert".into(),
            key_pem: "/no/such/key".into(),
            ..ConnectConfig::default()
        };
        let mut c = Channel::new(cfg);
        let err = c.connect().await.unwrap_err();
        assert!(matches!(err, TransportError::MissingIdentity { .. }));
    }

    #[tokio::test]
    async fn reconnect_backoff_doubles_then_caps() {
        // Connect-once that always fails — observe the backoff sequence by
        // watching how many times the closure ran during a fixed wall-time.
        let calls = std::sync::Arc::new(std::sync::atomic::AtomicU32::new(0));
        let calls_c = calls.clone();
        let (cancel_tx, cancel_rx) = tokio::sync::watch::channel(false);
        let task = tokio::spawn(async move {
            run_with_reconnect(
                Duration::from_millis(5),
                Duration::from_millis(40),
                cancel_rx,
                move || {
                    let c = calls_c.clone();
                    async move {
                        c.fetch_add(1, std::sync::atomic::Ordering::SeqCst);
                        Err::<(), _>(TransportError::Unreachable("test".into()))
                    }
                },
            )
            .await
        });
        tokio::time::sleep(Duration::from_millis(200)).await;
        cancel_tx.send(true).unwrap();
        let _ = task.await;
        let n = calls.load(std::sync::atomic::Ordering::SeqCst);
        // 5 + 10 + 20 + 40 + 40 + ... within 200ms => expect 4-7 calls.
        assert!(n >= 3, "expected ≥3 attempts, got {n}");
    }

    #[tokio::test]
    async fn heartbeat_pump_emits_with_state() {
        let h = iogrid_scheduler::SchedulerHandle::new(iogrid_scheduler::SchedulerConfig {
            idle_only: false,
            ..Default::default()
        });
        // Provide some sensors so refresh has work to do.
        h.set_sensors(iogrid_scheduler::SensorSnapshot {
            cpu_used_pct: 10,
            memory_used_pct: 20,
            idle_secs: 100,
            bandwidth_used_gb: 0,
            bandwidth_used_bytes: 0,
        });
        let sink = std::sync::Arc::new(MemSink::default());
        let task = spawn_heartbeat_pump(
            "00000000-0000-0000-0000-000000000001".into(),
            h.clone(),
            sink.clone(),
            Duration::from_millis(20),
        );
        tokio::time::sleep(Duration::from_millis(70)).await;
        task.abort();
        let n = sink.received.lock().len();
        assert!(n >= 2, "expected ≥2 heartbeats in 70ms, got {n}");
        let last = sink.received.lock().last().unwrap().clone();
        assert_eq!(last.cpu_pct, 10);
        assert_eq!(last.state, "active");
    }

    #[tokio::test]
    async fn dispatch_loopback_round_trips() {
        let (mut a, mut b) = dispatch_loopback();
        a.tx.send(DispatchFrame::DaemonHello {
            provider_id: "p".into(),
            eligible_types: vec!["BANDWIDTH".into()],
            max_concurrent: 3,
        })
        .await
        .unwrap();
        let got = b.rx.recv().await.unwrap();
        assert!(matches!(got, DispatchFrame::DaemonHello { .. }));
        b.tx.send(DispatchFrame::CoordinatorHello {
            provider_id: "p".into(),
            accepted_at: "now".into(),
        })
        .await
        .unwrap();
        let got = a.rx.recv().await.unwrap();
        assert!(matches!(got, DispatchFrame::CoordinatorHello { .. }));
    }
}
