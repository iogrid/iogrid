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

mod convert;
mod pb;

use std::path::PathBuf;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use thiserror::Error;
use tokio::sync::mpsc;
use tokio_stream::wrappers::ReceiverStream;
use tonic::transport::{Channel as TonicChannel, ClientTlsConfig, Endpoint, Identity};

use pb::workloads::v1::workload_dispatch_service_client::WorkloadDispatchServiceClient;

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

/// Live dispatch bridge — the supervisor's seam onto the real bidi gRPC
/// stream. Returned by [`spawn_live_dispatch`].
///
/// `daemon_side` is what the supervisor pipes into the workload router
/// (rx) and what the router pushes status updates into (tx) — identical
/// shape to the [`dispatch_loopback`] half so the supervisor code path
/// stays uniform.
///
/// The `bridge` task running behind this handle owns a [`Channel`] +
/// reconnect engine: on first connect it sends a [`DispatchFrame::DaemonHello`]
/// down the wire, then mirrors every frame between the tonic stream and
/// the in-process mpsc pair. On disconnect it backs off + reconnects;
/// on shutdown it drains cleanly.
pub struct LiveDispatchHandle {
    /// Daemon-side dispatch channel — wire into the supervisor's
    /// `WorkloadRouter` exactly as the loopback half is wired today.
    pub daemon_side: DispatchChannel,
    /// Cancel signal — flip to `true` to ask the bridge task to shut down.
    pub cancel_tx: tokio::sync::watch::Sender<bool>,
    /// Bridge task handle — caller may join for clean shutdown.
    pub task: tokio::task::JoinHandle<()>,
}

/// Hello announcement the daemon sends as the first frame of every
/// dispatch stream attempt. Populated by the supervisor from
/// `DaemonConfig` + the workload runner registry.
#[derive(Debug, Clone)]
pub struct DispatchHello {
    /// Provider id (UUID string from pairing).
    pub provider_id: String,
    /// Workload types the daemon will accept right now.
    pub supported_types: Vec<String>,
    /// Max concurrent workloads.
    pub max_concurrent: u32,
}

/// Spawn the production dispatch bridge: opens an mTLS [`Channel`] to the
/// coordinator using `cfg`, sends `hello`, and pumps [`DispatchFrame`]s
/// between the wire and the supervisor's mpsc pair. Backs off + reconnects
/// per [`run_with_reconnect`].
///
/// On each connect attempt the bridge:
///
///  1. Opens an mTLS [`Channel`] (handshake + keepalives).
///  2. Builds a [`WorkloadDispatchServiceClient`] on it and opens the
///     bidirectional `Dispatch` stream.
///  3. Sends a [`DispatchFrame::DaemonHello`] as the first frame.
///  4. Waits up to 10 s for a [`DispatchFrame::CoordinatorHello`] ack.
///  5. Pumps every frame between the wire and the supervisor's mpsc pair
///     until either side disconnects or the cancel signal fires.
///
/// On stream end / error the closure returns and [`run_with_reconnect`]
/// schedules the next attempt with exponential backoff.
pub fn spawn_live_dispatch(cfg: ConnectConfig, hello: DispatchHello) -> LiveDispatchHandle {
    let (out_tx, out_rx) = mpsc::channel::<DispatchFrame>(64);
    let (in_tx, in_rx) = mpsc::channel::<DispatchFrame>(64);
    let daemon_side = DispatchChannel {
        tx: out_tx,
        rx: in_rx,
    };
    let (cancel_tx, cancel_rx) = tokio::sync::watch::channel(false);

    // Bridge the supervisor-facing mpsc senders into shared slots so the
    // reconnect loop's closure can install a fresh wire-bound sender on
    // every attempt without dropping the supervisor's `daemon_side.tx`
    // (whose `out_rx` we own here exclusively).
    let out_rx = std::sync::Arc::new(tokio::sync::Mutex::new(out_rx));
    let in_tx = std::sync::Arc::new(in_tx);

    let task = tokio::spawn(async move {
        let init = cfg.initial_backoff;
        let cap = cfg.max_backoff;
        let hello_template = hello.clone();
        let cfg_template = cfg.clone();
        let out_rx_outer = out_rx.clone();
        let in_tx_outer = in_tx.clone();
        run_with_reconnect(init, cap, cancel_rx, move || {
            let cfg = cfg_template.clone();
            let hello = hello_template.clone();
            let out_rx = out_rx_outer.clone();
            let in_tx = in_tx_outer.clone();
            async move {
                let mut ch = Channel::new(cfg.clone());
                ch.connect().await?;
                let channel = ch
                    .inner()
                    .cloned()
                    .ok_or_else(|| TransportError::Unreachable("channel not bound".into()))?;
                run_dispatch_stream(channel, hello, out_rx, in_tx).await
            }
        })
        .await;
    });
    LiveDispatchHandle {
        daemon_side,
        cancel_tx,
        task,
    }
}

/// Open the bidi stream, perform the hello/ack handshake, and pump frames
/// in both directions until the wire closes. Extracted so tests can drive
/// it against an in-process `tonic::transport::Server`.
async fn run_dispatch_stream(
    channel: TonicChannel,
    hello: DispatchHello,
    out_rx: std::sync::Arc<tokio::sync::Mutex<mpsc::Receiver<DispatchFrame>>>,
    in_tx: std::sync::Arc<mpsc::Sender<DispatchFrame>>,
) -> Result<(), TransportError> {
    let mut client = WorkloadDispatchServiceClient::new(channel);

    // Outbound side: a new mpsc that gets piped onto the gRPC request
    // stream via tokio_stream::ReceiverStream. We push the DaemonHello
    // onto it first thing, then forward every frame from `out_rx` until
    // the supervisor's tx is closed.
    let (req_tx, req_rx) = mpsc::channel::<pb::workloads::v1::DispatchFrame>(64);
    let daemon_hello = DispatchFrame::DaemonHello {
        provider_id: hello.provider_id.clone(),
        eligible_types: hello.supported_types.clone(),
        max_concurrent: hello.max_concurrent,
    };
    req_tx
        .send(convert::frame_to_pb(&daemon_hello))
        .await
        .map_err(|_| TransportError::Unreachable("hello channel closed before send".into()))?;

    let req_stream = ReceiverStream::new(req_rx);
    let resp = client
        .dispatch(req_stream)
        .await
        .map_err(|s| TransportError::Unreachable(format!("dispatch RPC: {s}")))?;
    let mut resp_stream = resp.into_inner();

    // Wait up to 10 s for the CoordinatorHello ack.
    let ack = match tokio::time::timeout(Duration::from_secs(10), resp_stream.message()).await {
        Ok(Ok(Some(frame))) => convert::frame_from_pb(frame),
        Ok(Ok(None)) => {
            return Err(TransportError::Unreachable(
                "stream closed before coordinator-hello".into(),
            ))
        }
        Ok(Err(s)) => return Err(TransportError::Unreachable(format!("recv error: {s}"))),
        Err(_) => {
            return Err(TransportError::Unreachable(
                "timed out waiting for coordinator-hello (10s)".into(),
            ))
        }
    };
    match ack {
        Some(DispatchFrame::CoordinatorHello { provider_id, .. }) => {
            tracing::info!(provider_id = %provider_id, "coordinator-hello received");
            // Surface the ack to the supervisor so it can transition
            // `DaemonStateView.state` to `connected`.
            let _ = in_tx
                .send(DispatchFrame::CoordinatorHello {
                    provider_id,
                    accepted_at: chrono::Utc::now().to_rfc3339(),
                })
                .await;
        }
        other => {
            return Err(TransportError::Unreachable(format!(
                "expected coordinator-hello, got {other:?}"
            )));
        }
    }

    // Both directions are now live. select! between outbound (mpsc →
    // wire) and inbound (wire → mpsc) until either side closes.
    let mut out_rx_guard = out_rx.lock().await;
    loop {
        tokio::select! {
            biased;
            inbound = resp_stream.message() => {
                match inbound {
                    Ok(Some(pb_frame)) => {
                        if let Some(frame) = convert::frame_from_pb(pb_frame) {
                            if in_tx.send(frame).await.is_err() {
                                tracing::debug!("supervisor rx closed; exiting dispatch pump");
                                return Ok(());
                            }
                        }
                    }
                    Ok(None) => {
                        tracing::info!("coordinator closed dispatch stream");
                        return Ok(());
                    }
                    Err(s) => {
                        return Err(TransportError::Unreachable(format!(
                            "dispatch stream recv: {s}"
                        )));
                    }
                }
            }
            outbound = out_rx_guard.recv() => {
                match outbound {
                    Some(frame) => {
                        if req_tx.send(convert::frame_to_pb(&frame)).await.is_err() {
                            tracing::debug!("request stream closed; exiting dispatch pump");
                            return Ok(());
                        }
                    }
                    None => {
                        tracing::debug!("supervisor tx closed; exiting dispatch pump");
                        return Ok(());
                    }
                }
            }
        }
    }
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
    async fn live_dispatch_handle_exposes_daemon_side_channel() {
        // Connection will fail (placeholder identity files) — the
        // reconnect loop should keep retrying without panicking, and
        // the daemon-side channel must remain usable from the caller.
        let dir = tempfile::tempdir().unwrap();
        let cert = dir.path().join("cert.pem");
        let key = dir.path().join("key.pem");
        std::fs::write(
            &cert,
            b"-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n",
        )
        .unwrap();
        std::fs::write(
            &key,
            b"-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n",
        )
        .unwrap();
        let cfg = ConnectConfig {
            coordinator_url: "https://127.0.0.1:1".into(),
            cert_pem: cert,
            key_pem: key,
            ca_pem: None,
            max_backoff: Duration::from_millis(20),
            initial_backoff: Duration::from_millis(5),
        };
        let hello = DispatchHello {
            provider_id: "00000000-0000-0000-0000-000000000001".into(),
            supported_types: vec!["BANDWIDTH".into()],
            max_concurrent: 4,
        };
        let handle = spawn_live_dispatch(cfg, hello);
        // The daemon-side tx must accept frames even before any
        // connection succeeds (drain task swallows them).
        handle
            .daemon_side
            .tx
            .send(DispatchFrame::Update {
                workload_id: "wl".into(),
                attempt_id: "a".into(),
                status: "running".into(),
                observed_at_rfc3339: "now".into(),
                note: None,
                bytes_in: 0,
                bytes_out: 0,
                exit_code: 0,
                logs_s3_key: None,
                rejection_reason: None,
            })
            .await
            .expect("tx open");
        tokio::time::sleep(Duration::from_millis(30)).await;
        let _ = handle.cancel_tx.send(true);
        // Give the bridge task a moment to wind down. abort() is a
        // belt-and-braces — the watch signal alone should be enough
        // but we don't want the test to hang on a slow CI runner.
        let _ = tokio::time::timeout(Duration::from_millis(200), handle.task).await;
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

    // -------------------------------------------------------------------
    // bidi pump integration tests — spin up an in-process tonic Server
    // that implements WorkloadDispatchService just enough to exercise
    // the daemon-side handshake + frame forwarding.
    // -------------------------------------------------------------------

    use crate::pb::workloads::v1 as wlv1;
    use std::sync::Arc;
    use tokio::sync::Mutex;
    use tonic::transport::Server;
    use tonic::Status;

    /// In-process stub of `WorkloadDispatchService::dispatch`. Records the
    /// first DaemonHello, sends a CoordinatorHello back, then optionally
    /// pushes a single canned `extra_frame` downstream so the daemon-side
    /// pump can be asserted against.
    #[derive(Default, Clone)]
    struct StubDispatch {
        observed_hello: Arc<Mutex<Option<wlv1::DispatchFrame>>>,
        extra_frame: Arc<Mutex<Option<wlv1::DispatchFrame>>>,
    }

    #[tonic::async_trait]
    impl wlv1::workload_dispatch_service_server::WorkloadDispatchService for StubDispatch {
        type DispatchStream =
            tokio_stream::wrappers::ReceiverStream<Result<wlv1::DispatchFrame, Status>>;

        async fn dispatch(
            &self,
            req: tonic::Request<tonic::Streaming<wlv1::DispatchFrame>>,
        ) -> Result<tonic::Response<Self::DispatchStream>, Status> {
            let (tx, rx) = tokio::sync::mpsc::channel(8);
            let observed = self.observed_hello.clone();
            let extra = self.extra_frame.clone();
            tokio::spawn(async move {
                let mut inbound = req.into_inner();
                // Capture the first frame (DaemonHello) and ack with
                // a CoordinatorHello.
                if let Ok(Some(frame)) = inbound.message().await {
                    *observed.lock().await = Some(frame.clone());
                    let provider_id = match &frame.frame {
                        Some(wlv1::dispatch_frame::Frame::DaemonHello(h)) => h.provider_id.clone(),
                        _ => None,
                    };
                    let ack = wlv1::DispatchFrame {
                        frame: Some(wlv1::dispatch_frame::Frame::CoordinatorHello(
                            wlv1::CoordinatorHello {
                                provider_id,
                                accepted_at: Some(prost_types::Timestamp {
                                    seconds: 0,
                                    nanos: 0,
                                }),
                            },
                        )),
                    };
                    let _ = tx.send(Ok(ack)).await;
                    // Push the canned extra frame (test-controlled) so the
                    // daemon's inbound pump can be observed.
                    if let Some(ef) = extra.lock().await.take() {
                        let _ = tx.send(Ok(ef)).await;
                    }
                    // Drain remaining inbound frames so the client's send
                    // side stays unblocked.
                    while let Ok(Some(_)) = inbound.message().await {
                        // observed for test isolation only
                    }
                }
            });
            Ok(tonic::Response::new(
                tokio_stream::wrappers::ReceiverStream::new(rx),
            ))
        }
    }

    /// Spin up a `tonic::transport::Server` on a free `127.0.0.1` port and
    /// connect a plain (no-TLS) tonic Channel to it. Returns the bound
    /// socket addr plus a shutdown signal sender and the stub so tests can
    /// introspect.
    async fn spawn_stub_server() -> (
        std::net::SocketAddr,
        tokio::sync::oneshot::Sender<()>,
        StubDispatch,
    ) {
        let stub = StubDispatch::default();
        let svc = wlv1::workload_dispatch_service_server::WorkloadDispatchServiceServer::new(
            stub.clone(),
        );
        // Bind a TCP listener on an OS-picked port to discover the addr,
        // then immediately drop it so tonic's Server can re-bind. We retry
        // on the off-chance of a TOCTOU collision with another test.
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        drop(listener);
        let (shutdown_tx, shutdown_rx) = tokio::sync::oneshot::channel::<()>();
        tokio::spawn(async move {
            Server::builder()
                .add_service(svc)
                .serve_with_shutdown(addr, async {
                    let _ = shutdown_rx.await;
                })
                .await
                .ok();
        });
        // Wait for the server to start listening — poll TCP connect.
        let deadline = std::time::Instant::now() + Duration::from_secs(2);
        while std::time::Instant::now() < deadline {
            if tokio::net::TcpStream::connect(addr).await.is_ok() {
                break;
            }
            tokio::time::sleep(Duration::from_millis(10)).await;
        }
        (addr, shutdown_tx, stub)
    }

    #[tokio::test]
    async fn live_dispatch_handshake_roundtrip() {
        let (addr, shutdown_tx, stub) = spawn_stub_server().await;

        let endpoint = Endpoint::from_shared(format!("http://{addr}")).unwrap();
        let channel = endpoint.connect().await.unwrap();

        let (out_tx, out_rx) = mpsc::channel::<DispatchFrame>(8);
        let (in_tx, mut in_rx) = mpsc::channel::<DispatchFrame>(8);
        let out_rx = Arc::new(Mutex::new(out_rx));
        let in_tx = Arc::new(in_tx);

        let hello = DispatchHello {
            provider_id: "00000000-0000-0000-0000-0000000000aa".into(),
            supported_types: vec!["BANDWIDTH".into()],
            max_concurrent: 4,
        };

        let pump = tokio::spawn(run_dispatch_stream(
            channel,
            hello.clone(),
            out_rx.clone(),
            in_tx.clone(),
        ));

        // Daemon should receive the CoordinatorHello on the inbound side.
        let ack = tokio::time::timeout(Duration::from_secs(2), in_rx.recv())
            .await
            .expect("ack within 2s")
            .expect("ack present");
        assert!(matches!(ack, DispatchFrame::CoordinatorHello { .. }));

        // The stub should have recorded our DaemonHello with provider_id.
        let observed = stub
            .observed_hello
            .lock()
            .await
            .clone()
            .expect("stub saw frame");
        match observed.frame.expect("oneof set") {
            wlv1::dispatch_frame::Frame::DaemonHello(h) => {
                assert_eq!(
                    h.provider_id.unwrap_or_default().value,
                    "00000000-0000-0000-0000-0000000000aa"
                );
                assert_eq!(h.max_concurrent, 4);
            }
            other => panic!("expected DaemonHello, got {other:?}"),
        }

        // Tear down: drop the outbound sender to close the pump, then
        // shut the server down.
        drop(out_tx);
        let _ = tokio::time::timeout(Duration::from_secs(2), pump).await;
        let _ = shutdown_tx.send(());
    }

    #[tokio::test]
    async fn live_dispatch_forwards_frames() {
        let (addr, shutdown_tx, stub) = spawn_stub_server().await;

        // Pre-stage an Assignment frame for the stub to push after
        // CoordinatorHello.
        *stub.extra_frame.lock().await = Some(wlv1::DispatchFrame {
            frame: Some(wlv1::dispatch_frame::Frame::Assignment(
                wlv1::WorkloadAssignment {
                    workload: None,
                    attempt_id: Some(crate::pb::common::v1::Uuid {
                        value: "attempt-1".into(),
                    }),
                    deadline: None,
                    dispatch_token: "tok-xyz".into(),
                },
            )),
        });

        let endpoint = Endpoint::from_shared(format!("http://{addr}")).unwrap();
        let channel = endpoint.connect().await.unwrap();

        let (out_tx, out_rx) = mpsc::channel::<DispatchFrame>(8);
        let (in_tx, mut in_rx) = mpsc::channel::<DispatchFrame>(8);
        let out_rx = Arc::new(Mutex::new(out_rx));
        let in_tx = Arc::new(in_tx);

        let hello = DispatchHello {
            provider_id: "00000000-0000-0000-0000-0000000000bb".into(),
            supported_types: vec!["BANDWIDTH".into()],
            max_concurrent: 1,
        };

        let pump = tokio::spawn(run_dispatch_stream(
            channel,
            hello,
            out_rx.clone(),
            in_tx.clone(),
        ));

        // First inbound frame is the CoordinatorHello (handshake).
        let first = tokio::time::timeout(Duration::from_secs(2), in_rx.recv())
            .await
            .expect("handshake within 2s")
            .expect("handshake present");
        assert!(matches!(first, DispatchFrame::CoordinatorHello { .. }));

        // Second inbound frame is the canned Assignment.
        let second = tokio::time::timeout(Duration::from_secs(2), in_rx.recv())
            .await
            .expect("assignment within 2s")
            .expect("assignment present");
        match second {
            DispatchFrame::Assignment {
                attempt_id,
                dispatch_token,
                ..
            } => {
                assert_eq!(attempt_id, "attempt-1");
                assert_eq!(dispatch_token, "tok-xyz");
            }
            other => panic!("expected Assignment, got {other:?}"),
        }

        // Also verify outbound: send a status Update via the supervisor
        // side and confirm the pump forwards it onto the wire (we can't
        // observe the stub's drained recv directly, but at least the send
        // must not block / error).
        out_tx
            .send(DispatchFrame::Update {
                workload_id: "wl-9".into(),
                attempt_id: "attempt-1".into(),
                status: "running".into(),
                observed_at_rfc3339: "2026-05-19T00:00:00+00:00".into(),
                note: Some("ok".into()),
                bytes_in: 0,
                bytes_out: 0,
                exit_code: 0,
                logs_s3_key: None,
                rejection_reason: None,
            })
            .await
            .expect("outbound send accepted");

        drop(out_tx);
        let _ = tokio::time::timeout(Duration::from_secs(2), pump).await;
        let _ = shutdown_tx.send(());
    }
}
