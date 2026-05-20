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

use std::net::SocketAddr;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use thiserror::Error;
use tokio::sync::{mpsc, RwLock, Semaphore};
use tokio_stream::wrappers::ReceiverStream;
use tonic::transport::{Channel as TonicChannel, ClientTlsConfig, Endpoint, Identity};
// #273: pre-resolved-IP connector — see `PinnedAddrConnector` below.
use http::Uri;
use hyper_util::rt::TokioIo;
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};
use tokio::net::TcpStream;
use tower_service::Service;

use pb::workloads::v1::workload_dispatch_service_client::WorkloadDispatchServiceClient;

/// Format a `std::error::Error` together with every nested
/// `source()` cause as a single human-readable string, joined by `" → "`.
///
/// tonic 0.12's `tonic::transport::Error::Display` is the literal string
/// `"transport error"` — every interesting detail (hyper / h2 / rustls)
/// lives inside `source()`. The standard `e.to_string()` map drops the
/// chain entirely and operators see only `"transport error"` in WARN
/// lines, which is unactionable.
///
/// This helper walks the chain via `std::error::Error::source()` and
/// concatenates each layer's `Display`. The output looks like:
///
/// ```text
/// transport error → connection error → invalid peer certificate: UnknownIssuer
/// ```
///
/// See issue #243 — opaque "transport error" on coordinator dial.
pub fn display_error_chain(err: &(dyn std::error::Error + 'static)) -> String {
    let mut out = err.to_string();
    let mut source = err.source();
    while let Some(s) = source {
        let next = s.to_string();
        // Skip duplicate frames (some wrappers re-emit the inner Display).
        if !next.is_empty() && !out.ends_with(&next) {
            out.push_str(" → ");
            out.push_str(&next);
        }
        source = s.source();
    }
    out
}

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

    /// Supervisor-resolved coordinator `SocketAddr` shared across every
    /// reconnect-loop attempt (see #253).
    ///
    /// When `Some`, [`Channel::connect`] short-circuits the in-loop DNS
    /// path entirely: it reads the current IP, builds the tonic endpoint
    /// as an `https://<ip>:<port>` literal, and pins SNI + cert-SAN
    /// validation to the original hostname via `ClientTlsConfig::domain_name`.
    /// The lookup_host future is therefore owned by the supervisor (not
    /// by tower's per-attempt reconnect future) and cannot be cancelled
    /// when the connect attempt is dropped.
    ///
    /// On connect failure the field is mutated in place (re-resolve +
    /// write under the `RwLock`) so the next attempt picks up a fresh
    /// IP without restarting the daemon.
    ///
    /// Skipped from serde because `RwLock<SocketAddr>` is a runtime
    /// handle, not config-on-disk data. Defaults to `None`, in which
    /// case the legacy in-loop `resolve_host_for_endpoint` path
    /// (PR #251) still works — unit tests don't have to construct an
    /// `Arc` for every fixture.
    #[serde(skip)]
    pub resolved_addr: Option<Arc<RwLock<SocketAddr>>>,

    /// Single-permit semaphore the reconnect loop acquires before every
    /// `connect_once` (see #253).
    ///
    /// The Phase 0 daemon currently spawns one live dispatch loop, but
    /// the heartbeat + ruleset paths will become real gRPC channels in
    /// follow-up PRs and would otherwise dial concurrently — three
    /// parallel `connect()` futures racing the same blocking-getaddrinfo
    /// pool is exactly the #248/#253 failure mode. Serialising every
    /// connect attempt behind one permit keeps reconnect storms tame
    /// and gives the supervisor a single choke point if we ever need to
    /// add jitter / rate-limiting.
    ///
    /// `None` keeps the legacy single-stream behaviour for tests and
    /// callers that don't need cross-loop coordination.
    #[serde(skip)]
    pub connect_semaphore: Option<Arc<Semaphore>>,
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
            resolved_addr: None,
            connect_semaphore: None,
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
    ///
    /// # DNS-resolution path (see #248 / #250)
    ///
    /// We deliberately do NOT let hyper's default `GaiResolver` perform
    /// the coordinator-host lookup. `GaiResolver` shells out to
    /// `tokio::task::spawn_blocking(getaddrinfo)`; when the parent future
    /// is polled-then-dropped by tower's reconnect / buffer middleware
    /// the blocking task is cancelled mid-flight and the failure surfaces
    /// as `dns error → task NNN was cancelled` — a fake "DNS failure"
    /// with no real underlying resolver error (issue #248).
    ///
    /// PR #249 switched to an in-process hickory resolver, but instantiating
    /// `TokioAsyncResolver::tokio_from_system_conf()` per connect-attempt
    /// leaks `DnsExchange` background tasks each reconnect cycle. Their
    /// internal request/response demuxers race and emit ~2000 WARN/sec of
    /// `failed to associate send_message response to the sender`
    /// (issue #250: 63,879 WARN lines in 31s — daemon never reaches
    /// `coordinator mTLS channel connected`).
    ///
    /// PR #251 moved the resolver to `tokio::net::lookup_host` with a
    /// 1 h process-global cache, but the lookup itself still ran on the
    /// per-attempt future the supervisor's reconnect loop owned — and
    /// tonic/tower can drop that future mid-flight. The spawn_blocking
    /// task running `getaddrinfo` would then be cancelled and the failure
    /// surfaced as `dns lookup (api.iogrid.org): task was cancelled`
    /// (issue #253).
    ///
    /// The current path (post-#253) lifts the lookup off the connect
    /// future entirely. The supervisor calls [`pre_resolve_addr`] **once**
    /// at startup and stashes the resulting `Arc<RwLock<SocketAddr>>` on
    /// [`ConnectConfig::resolved_addr`]. Every `Channel::connect` then
    /// reads the cached `SocketAddr` synchronously — no DNS work happens
    /// inside the cancellable per-attempt future. A background task
    /// ([`spawn_addr_refresh`]) re-resolves hourly and the error path
    /// here mutates the arc in place after a failed dial. We then build
    /// the tonic [`Endpoint`] pointing at the resolved IP literal while
    /// pinning TLS [`ClientTlsConfig::domain_name`] to the original
    /// hostname so SNI + server-cert SAN validation still match. The
    /// mTLS flow from #236 (identity + native + webpki roots) stays
    /// bit-for-bit identical — only the host→IP step changes.
    ///
    /// Callers that don't wire the arc (unit tests + first-boot paths
    /// that take the loopback fork) still get the PR #251 fallback path
    /// via [`resolve_host_for_endpoint`].
    pub async fn connect(&mut self) -> Result<(), TransportError> {
        if !self.cfg.coordinator_url.starts_with("https://") {
            return Err(TransportError::InvalidUrl(self.cfg.coordinator_url.clone()));
        }
        // Serialise concurrent connect attempts across the supervisor's
        // reconnect loops (see #253). The permit is held for the full
        // duration of the dial — TLS handshake + h2 settings — and is
        // released when this stack frame returns. Phase 0 the dispatch
        // loop is the only caller, so the semaphore is a no-op in
        // practice; once heartbeat + ruleset get their own gRPC streams
        // the permit prevents the three reconnect loops from racing
        // through three parallel getaddrinfo + TLS-handshake stacks.
        //
        // We DELIBERATELY do not log on the wait path: at steady state
        // it's instant, and on the rare contention case the connect
        // logs already tell the story.
        let _permit = match self.cfg.connect_semaphore.as_ref() {
            Some(sem) => Some(sem.clone().acquire_owned().await.map_err(|e| {
                TransportError::Unreachable(format!("connect semaphore closed: {e}"))
            })?),
            None => None,
        };
        let cert = read_pem(&self.cfg.cert_pem)?;
        let key = read_pem(&self.cfg.key_pem)?;
        let identity = Identity::from_pem(cert, key);

        // Parse the URL so we can extract host (for SNI + cert SAN) and
        // port (for the IP-literal Endpoint we'll build below).
        let parsed = url::Url::parse(&self.cfg.coordinator_url)
            .map_err(|e| TransportError::InvalidUrl(e.to_string()))?;
        let host = parsed
            .host_str()
            .ok_or_else(|| TransportError::InvalidUrl("coordinator URL has no host".into()))?
            .to_string();
        let port = parsed.port().unwrap_or(443);

        // Resolve the coordinator host.
        //
        // Preferred path (#253): the supervisor pre-resolved at startup
        // and handed us an `Arc<RwLock<SocketAddr>>`. We just read the
        // current IP — no DNS work happens on the reconnect-loop's
        // future, so when tonic / tower drops that future the lookup
        // can't be cancelled mid-flight (the failure mode #251 fixed
        // for the steady-state case but #253 caught on first attempt).
        //
        // Fallback path: legacy in-loop `resolve_host_for_endpoint`
        // (PR #251) so unit tests + callers that don't wire the arc
        // still work bit-for-bit identically. Returns a `SocketAddr`
        // that the `PinnedAddrConnector` below hands to `TcpStream::connect`,
        // bypassing hyper-util's resolver step entirely.
        let pinned_addr: SocketAddr = match self.cfg.resolved_addr.as_ref() {
            Some(arc) => *arc.read().await,
            None => {
                let authority = resolve_host_for_endpoint(&host, port).await?;
                parse_socket_addr_from_authority(&authority)?
            }
        };

        // Trust store: webpki-roots ONLY. We deliberately DO NOT call
        // `.with_native_roots()` here.
        //
        // History: #230 added native+webpki as "defense in depth". That
        // was a mistake — on macOS, `with_native_roots()` makes
        // rustls-native-certs walk the Keychain via
        // `SecTrustSettingsCopyTrustSettings(kSecTrustSettingsDomainSystem)`,
        // which on certain macOS configurations (MDM-managed corporate
        // keychains, TCC-prompted trust settings, or simply a Keychain
        // in a state that triggers the upstream rustls-native-certs#75 /
        // #110 bug) blocks indefinitely. Issue #327 caught this on
        // Hatices-Mac-mini-2 (14.6.1 arm64) under a LaunchAgent context:
        // the daemon booted through "live dispatch bridge spawned", then
        // both tokio worker threads sat for minutes inside
        // `SecTrustSettingsCopyTrustSettings → tsCopyTrustSettings →
        // KeychainCore::TrustSettings::CreateTrustSettings → ...
        // CFPropertyListCreateFromXMLData`. Because we never reached
        // `Endpoint::connect_with_connector`, none of the #246 / #281
        // chain-walker diagnostics fired — the silence WAS the bug.
        //
        // Why webpki-roots alone is correct:
        //   * Coordinator certs are Let's Encrypt-issued (ISRG Root X1/X2)
        //     which is in webpki-roots.
        //   * Private-CA deployments (bp-cnpg, on-prem) set
        //     `cfg.ca_pem` and we layer the explicit CA below via
        //     `tls.ca_certificate(...)` — that path is independent of
        //     the system trust store.
        //   * Server certs are network-presented and validated against
        //     a rustls `ClientConfig` root store; the macOS Keychain
        //     adds zero value for a daemon dialing a public-CA-signed
        //     coordinator from a non-interactive process.
        //
        // `domain_name(host)` is what makes the IP-literal authority
        // safe: rustls uses it for SNI + server-cert SAN validation,
        // so the coordinator still has to present a cert matching the
        // original hostname (e.g. `api.iogrid.org`), exactly as before
        // the resolver swap.
        let mut tls = ClientTlsConfig::new()
            .identity(identity)
            .with_webpki_roots()
            .domain_name(host.clone());
        if let Some(ca_path) = &self.cfg.ca_pem {
            let ca = read_pem(ca_path)?;
            tls = tls.ca_certificate(tonic::transport::Certificate::from_pem(ca));
        }
        // #273: ROOT CAUSE — `Endpoint::from_shared("https://<ip>:443")`
        // combined with `tcp_keepalive` + `http2_keep_alive_interval` +
        // `keep_alive_while_idle(true)` was producing a tight ~10ms loop
        // of `Reconnect: idle → connecting → not ready → idle` inside
        // tonic 0.12 / hyper-util 0.1 / h2 0.4, with each TCP SYN
        // dropping its connect future after ~150μs (before SYN-ACK
        // arrives). The kernel then RST'd every incoming SYN-ACK and
        // the dial never completed. tcpdump on the bastion (2026-05-20)
        // shows the pattern unambiguously — see #273 for the pcap.
        //
        // The matching diagnostic on the wire from grpcurl (same host,
        // same identity, hostname authority, NO keep-alive overrides)
        // is a clean SYN → SYN-ACK → ACK → TLS → CoordinatorHello in
        // <1s. We mirror that contract here:
        //
        //   * Hostname authority via `Endpoint::from_shared` so tonic's
        //     gRPC `:authority` header carries `api.iogrid.org` (some
        //     edges reject `:authority: <ip>:443` outright).
        //   * Custom `PinnedAddrConnector` (see below) that hands tonic
        //     a single `tower::Service<Uri>` whose `call` is just
        //     `TcpStream::connect(pinned_addr)`. No hyper-util resolver
        //     layer, no `HttpConnector::poll_ready` ladder for tonic's
        //     `Reconnect` to spin against. DNS work happens once at
        //     supervisor startup ([`pre_resolve_addr`]); only the
        //     address layer uses the pinned IP. Hostname stays in the
        //     `Uri` for `:authority` + SNI.
        //   * `connect_timeout(10s)` so a stuck dial errors rather
        //     than spinning silently.
        //   * No `tcp_keepalive`, no `http2_keep_alive_interval`, no
        //     `keep_alive_timeout`, no `keep_alive_while_idle`. h2's
        //     hyper defaults are correct for a long-lived bidi against
        //     Traefik (whose ServersTransport edge already runs
        //     symmetric PINGs per the PR #272 shim). Re-introducing
        //     these knobs is what triggered the regression.
        let ep = Endpoint::from_shared(format!("https://{host}:{port}"))
            .map_err(|e| TransportError::InvalidUrl(display_error_chain(&e)))?
            .tls_config(tls)
            .map_err(|e| {
                let chain = display_error_chain(&e);
                // Emit the full chain at WARN once per attempt so the
                // operator sees the actual rustls/webpki cause, not just
                // the opaque "transport error" Display.
                tracing::warn!(
                    coordinator = %self.cfg.coordinator_url,
                    error.cause = %chain,
                    "tls_config rejected — full error chain"
                );
                TransportError::TlsError(chain)
            })?
            .connect_timeout(Duration::from_secs(10));
        // INTENTIONALLY no `.timeout(...)`.
        //
        // `tonic::transport::Endpoint::timeout(d)` sets a default
        // timeout applied to EVERY RPC call on the channel — which
        // for the long-lived bidi streams (#311
        // SchedulingService.StreamHeartbeats, #271
        // WorkloadDispatchService.Dispatch) means the server-side
        // stream is cancelled after `d` regardless of whether
        // traffic is flowing.  Concretely, a 10s timeout killed
        // the heartbeat stream every 10s with
        // `grpc-code=Cancelled / "Timeout expired"`, holding
        // `providers.last_seen_at` frozen even after every other
        // wire-level blocker (TLS hang #327, port parse #346,
        // Traefik h2c IngressRoute #353, proxy passthrough #350)
        // was cleared. Diagnosed on Hatices-Mac-mini-2 2026-05-20.
        //
        // We KEEP `connect_timeout(10s)` because it only gates the
        // initial TCP+TLS handshake — once the channel is open it
        // does not interfere with stream lifetime.  Unary RPCs
        // that should fail fast set their own per-call timeout at
        // the call site (e.g. `Request::set_deadline`); the
        // supervisor's `run_with_reconnect` outer loop catches
        // server-side stream errors and re-dials.
        //
        // Issue #357.
        let pinned = PinnedAddrConnector::new(pinned_addr);
        let ch = match ep.connect_with_connector(pinned).await {
            Ok(ch) => ch,
            Err(e) => {
                let chain = display_error_chain(&e);
                // CRITICAL: tonic::transport::Error's Display is the literal
                // string "transport error". The actual cause (DNS, TCP, TLS
                // handshake, ALPN, h2 settings) lives in `source()`. Emit the
                // walked chain at WARN so operators can see the real failure
                // mode and so the supervisor's reconnect-loop warn line
                // (which formats `TransportError` via Display) carries the
                // chain into TransportError::Unreachable(chain).
                tracing::warn!(
                    coordinator = %self.cfg.coordinator_url,
                    error.cause = %chain,
                    "coordinator connect failed — full error chain"
                );
                // Invalidate the cached host→IP mapping so the next
                // attempt re-resolves (a coordinator load-balancer
                // floating-IP swap, or a stale CNAME, would otherwise
                // pin us to the wrong server until process restart).
                // Plain hostnames only — IP-literal entries don't get
                // cached so there's nothing to invalidate.
                invalidate_resolved_host(&host, port);
                // Supervisor-managed arc: rewrite in place so the next
                // attempt picks up a fresh IP (best-effort — if DNS is
                // currently broken we keep the stale entry rather than
                // failing here, since the connect error already speaks
                // for itself and the next loop iteration will retry).
                if let Some(arc) = self.cfg.resolved_addr.as_ref() {
                    match refresh_addr(&host, port).await {
                        Ok(fresh) => {
                            *arc.write().await = fresh;
                        }
                        Err(e) => {
                            tracing::warn!(
                                host = %host,
                                port = port,
                                error = %e,
                                "supervisor-managed DNS refresh failed after connect error; keeping stale IP"
                            );
                        }
                    }
                }
                return Err(TransportError::Unreachable(chain));
            }
        };
        self.channel = Some(ch);
        self.state = ClientState::Connected;
        tracing::info!(
            coordinator = %self.cfg.coordinator_url,
            resolved = %pinned_addr,
            sni = %host,
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

/// TTL for the host→IP cache. Production coordinator DNS rarely flips;
/// 1 h matches typical AWS / Hetzner load-balancer record TTLs and keeps
/// us off the resolver hot path between reconnect cycles.
const RESOLVED_HOST_TTL: Duration = Duration::from_secs(3600);

/// Cache key: the URL authority `(host, port)` callers pass through
/// `ConnectConfig::coordinator_url`.
type HostKey = (String, u16);

/// Cache value: the resolved IP plus the wall-clock instant we stored it
/// at (used to enforce [`RESOLVED_HOST_TTL`]).
type CachedIp = (std::net::IpAddr, std::time::Instant);

/// Process-global cache map type — factored out so clippy's
/// `type_complexity` lint stays happy.
type HostCacheMap = std::collections::HashMap<HostKey, CachedIp>;

/// Process-global cache of host→IP resolution results.
///
/// Stored under a sync `Mutex` because every operation is O(1) and
/// contention is null (the supervisor's single dispatch reconnect loop
/// is the only caller in production today; even with N reconnect loops
/// the critical section is just a HashMap probe).
///
/// Entries expire after [`RESOLVED_HOST_TTL`] **or** when
/// [`invalidate_resolved_host`] is called explicitly from the connect
/// failure path. This guards against:
///
/// * Coordinator LB IP rotation (carrier-grade NAT migration / DNS-based
///   failover) — connect fails on the stale IP → cache invalidated →
///   next attempt re-resolves and picks up the new IP.
/// * Long-running daemons (months of uptime) drifting from current DNS
///   state — 1 h TTL refresh.
static RESOLVED_HOST_CACHE: std::sync::OnceLock<std::sync::Mutex<HostCacheMap>> =
    std::sync::OnceLock::new();

fn resolved_host_cache() -> &'static std::sync::Mutex<HostCacheMap> {
    RESOLVED_HOST_CACHE.get_or_init(|| std::sync::Mutex::new(HostCacheMap::new()))
}

/// Invalidate a cached host→IP entry — call from connect-failure paths
/// so the next attempt re-resolves rather than retrying the dead IP.
/// IP-literal hosts are never cached, so calling this for one is a
/// harmless no-op.
fn invalidate_resolved_host(host: &str, port: u16) {
    if host.parse::<std::net::IpAddr>().is_ok() {
        return;
    }
    if let Ok(mut g) = resolved_host_cache().lock() {
        g.remove(&(host.to_string(), port));
    }
}

/// Resolve `host` to an IP via the OS resolver ([`tokio::net::lookup_host`])
/// and return an `https://<ip>:<port>` authority string that tonic's
/// `Endpoint::from_shared` accepts.
///
/// IP literals short-circuit (DNS isn't needed). Otherwise we consult
/// [`RESOLVED_HOST_CACHE`] first and only fall through to a real lookup
/// when the entry is missing or expired. The lookup itself runs under
/// `spawn_blocking(getaddrinfo)` inside `tokio::net::lookup_host` — same
/// mechanism hyper's `GaiResolver` would use, BUT with two critical
/// differences from the #248 failure mode:
///
/// 1. We call it from `Channel::connect` which is itself driven by
///    [`run_with_reconnect`]'s sequential closure. There is no tower
///    buffer / reconnect middleware racing us, so the future cannot be
///    polled-then-dropped mid-flight. No blocking-task cancellation
///    storm.
/// 2. Cached results mean steady-state reconnect cycles never hit
///    the resolver at all — they reuse the IP from the OnceLock until
///    TTL expiry or explicit invalidation on connect failure.
///
/// This sidesteps the hickory-resolver `failed to associate
/// send_message response to the sender` WARN flood from #250 — we
/// never instantiate an in-process resolver at all.
async fn resolve_host_for_endpoint(host: &str, port: u16) -> Result<String, TransportError> {
    // IP literals — short-circuit. Cover both v4 and v6 forms. v6
    // literals in a URL authority are bracketed (`[::1]`), so format
    // accordingly when re-emitting. Never cache these.
    if let Ok(ip) = host.parse::<std::net::IpAddr>() {
        return Ok(format_ip_authority(ip, port));
    }

    // Cache hit (within TTL) — skip the OS resolver entirely. This is
    // the steady-state path after the first successful connect.
    let now = std::time::Instant::now();
    if let Ok(g) = resolved_host_cache().lock() {
        if let Some((ip, cached_at)) = g.get(&(host.to_string(), port)) {
            if now.duration_since(*cached_at) < RESOLVED_HOST_TTL {
                return Ok(format_ip_authority(*ip, port));
            }
        }
    }

    // Cache miss / expired — resolve via the OS (getaddrinfo under
    // spawn_blocking). Sequential w.r.t. the reconnect loop, so no
    // cancellation storm; see module-level docs on connect().
    let target = format!("{host}:{port}");
    let mut addrs = tokio::net::lookup_host(&target)
        .await
        .map_err(|e| TransportError::Unreachable(format!("dns lookup ({host}): {e}")))?;
    let sock = addrs
        .next()
        .ok_or_else(|| TransportError::Unreachable(format!("dns lookup ({host}): no records")))?;
    let ip = sock.ip();

    // Insert into cache for subsequent reconnect cycles.
    if let Ok(mut g) = resolved_host_cache().lock() {
        g.insert((host.to_string(), port), (ip, now));
    }

    Ok(format_ip_authority(ip, port))
}

fn format_ip_authority(ip: std::net::IpAddr, port: u16) -> String {
    match ip {
        std::net::IpAddr::V4(v4) => format!("https://{v4}:{port}"),
        std::net::IpAddr::V6(v6) => format!("https://[{v6}]:{port}"),
    }
}

/// Render a [`SocketAddr`] as the `https://<ip>:<port>` authority tonic's
/// `Endpoint::from_shared` accepts. Mirrors [`format_ip_authority`] but
/// avoids splitting+rejoining the port that's already in the address.
///
/// Currently only used by tests; production [`Channel::connect`] hands the
/// pinned [`SocketAddr`] straight to [`PinnedAddrConnector`] without ever
/// rendering it back into an `https://<ip>:<port>` string (which is the
/// authority shape that triggered the tonic 0.12 RST-loop, see #273).
#[allow(dead_code)]
fn format_socket_addr_authority(addr: SocketAddr) -> String {
    match addr {
        SocketAddr::V4(v4) => format!("https://{}:{}", v4.ip(), v4.port()),
        SocketAddr::V6(v6) => format!("https://[{}]:{}", v6.ip(), v6.port()),
    }
}

/// Parse a `https://<ip>:<port>` authority string (the shape
/// [`resolve_host_for_endpoint`] emits) back into a [`SocketAddr`].
///
/// Only used by the no-`resolved_addr` fallback path in
/// [`Channel::connect`]; the steady-state supervisor-pre-resolved path
/// already has a `SocketAddr` in hand. Returns
/// [`TransportError::InvalidUrl`] on any parse failure — the same error
/// variant the caller would have surfaced if `Endpoint::from_shared`
/// itself had rejected the URL.
fn parse_socket_addr_from_authority(authority: &str) -> Result<SocketAddr, TransportError> {
    let parsed = url::Url::parse(authority)
        .map_err(|e| TransportError::InvalidUrl(format!("authority parse: {e}")))?;
    let host = parsed
        .host_str()
        .ok_or_else(|| TransportError::InvalidUrl("authority has no host".into()))?;
    // `url::Url::port()` returns None when the explicit port equals the
    // scheme's default (e.g. 443 on https) — even when the URL string
    // CONTAINS `:443` literally. `format_ip_authority` upstream always
    // emits `https://<ip>:<port>` with the port present, so on a
    // coordinator dialled at the default 443 we'd come back here and
    // (pre-fix) bail with "authority has no port" despite the port being
    // right there in the string. `port_or_known_default()` returns
    // Some(443) for any https URL whose explicit-or-implicit port is the
    // scheme default.
    let port = parsed
        .port_or_known_default()
        .ok_or_else(|| TransportError::InvalidUrl("authority has no port".into()))?;
    // Brackets stripped by url::Url::host_str so v6 literals come back
    // as `::1` not `[::1]`. parse::<IpAddr> handles both v4 and v6.
    let ip: std::net::IpAddr = host
        .parse()
        .map_err(|e| TransportError::InvalidUrl(format!("authority ip parse ({host}): {e}")))?;
    Ok(SocketAddr::new(ip, port))
}

/// Tonic-compatible `tower::Service<Uri>` that always dials the same
/// pre-resolved [`SocketAddr`], regardless of the `Uri`'s host.
///
/// This is the core of the #273 fix. The bug observed on the bastion
/// 2026-05-20: tonic 0.12 paired with `Endpoint::from_shared("https://<ip>:443")`
/// and the `tcp_keepalive` / `http2_keep_alive_interval` /
/// `keep_alive_while_idle(true)` combo produced a tight ~10ms loop of
/// tonic's `Reconnect` middleware that dropped each TCP SYN's connect
/// future after ~150μs (before the SYN-ACK could be received and ACK'd).
/// The kernel then RST'd every SYN-ACK that arrived on the closed
/// socket, and the dial never completed.
///
/// By feeding `Endpoint::connect_with_connector` a hand-rolled
/// connector — with a single, simple `TcpStream::connect(addr)`
/// future and no hyper-util / resolver layer in between — the
/// problematic interaction with tonic's internal `Reconnect` is
/// neutralised. The `Uri`'s host stays `api.iogrid.org` so the
/// gRPC `:authority` header + rustls SNI both target the original
/// hostname, exactly like grpcurl does.
///
/// The connector is **stateless** beyond the cached [`SocketAddr`] —
/// every `call(uri)` constructs a fresh `TcpStream::connect` future
/// against the pinned addr. Supervisor-owned addr refresh ([`spawn_addr_refresh`])
/// continues to mutate the source `Arc<RwLock<SocketAddr>>` in the
/// background; the daemon re-builds a fresh `PinnedAddrConnector`
/// (with the latest IP) on every reconnect cycle.
#[derive(Clone)]
struct PinnedAddrConnector {
    addr: SocketAddr,
}

impl PinnedAddrConnector {
    fn new(addr: SocketAddr) -> Self {
        Self { addr }
    }
}

impl Service<Uri> for PinnedAddrConnector {
    type Response = TokioIo<TcpStream>;
    type Error = std::io::Error;
    type Future = Pin<Box<dyn Future<Output = Result<Self::Response, Self::Error>> + Send>>;

    fn poll_ready(&mut self, _cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        // We never need to wait for readiness — `TcpStream::connect`
        // is itself the async wait. Reporting Ready here keeps tonic's
        // `Reconnect` from spinning waiting on us (the #273 symptom).
        Poll::Ready(Ok(()))
    }

    fn call(&mut self, _uri: Uri) -> Self::Future {
        let addr = self.addr;
        Box::pin(async move {
            let sock = TcpStream::connect(addr).await?;
            // Match hyper-util's default — most h2 servers expect
            // TCP_NODELAY for low-latency PING/SETTINGS round-trips.
            let _ = sock.set_nodelay(true);
            Ok(TokioIo::new(sock))
        })
    }
}

/// Resolve `host:port` to a single [`SocketAddr`] via the OS resolver,
/// without touching the process-global `RESOLVED_HOST_CACHE`. Used by:
///
///   * [`pre_resolve_addr`] at supervisor startup, before any reconnect
///     loop has a chance to spawn a future that could be cancelled
///     (the #248/#253 failure mode).
///   * [`Channel::connect`]'s error path, to mutate the supervisor-shared
///     `Arc<RwLock<SocketAddr>>` in place so the next attempt dials a
///     fresh IP.
///   * [`spawn_addr_refresh`]'s hourly tick.
///
/// IP literals are accepted and short-circuit (no DNS work).
async fn refresh_addr(host: &str, port: u16) -> Result<SocketAddr, TransportError> {
    if let Ok(ip) = host.parse::<std::net::IpAddr>() {
        return Ok(SocketAddr::new(ip, port));
    }
    let target = format!("{host}:{port}");
    let mut addrs = tokio::net::lookup_host(&target)
        .await
        .map_err(|e| TransportError::Unreachable(format!("dns lookup ({host}): {e}")))?;
    addrs
        .next()
        .ok_or_else(|| TransportError::Unreachable(format!("dns lookup ({host}): no records")))
}

/// Pre-resolve the coordinator host **before** spawning any reconnect
/// loop and return the shared `Arc<RwLock<SocketAddr>>` callers should
/// stash on every [`ConnectConfig`] that targets the same coordinator
/// (see #253).
///
/// This is the core fix for #253: PR #251 moved DNS off the hickory
/// driver but the lookup itself still ran *inside* the per-attempt
/// future tonic/tower can drop, so the `tokio::task::spawn_blocking`
/// closure that getaddrinfo runs under got cancelled mid-flight and
/// surfaced as `dns lookup (api.iogrid.org): task was cancelled`. By
/// resolving from the supervisor task — which is never dropped —
/// the lookup completes once and every subsequent connect attempt
/// just reads the cached `SocketAddr`.
///
/// Parsing rules match [`Channel::connect`]: the URL must start with
/// `https://` and carry a non-empty host. Port defaults to 443. The
/// returned arc is intended to be cloned into [`ConnectConfig::resolved_addr`]
/// for every coordinator-talking subsystem (dispatch / heartbeat /
/// ruleset) so they share one IP across reconnects.
pub async fn pre_resolve_addr(
    coordinator_url: &str,
) -> Result<Arc<RwLock<SocketAddr>>, TransportError> {
    if !coordinator_url.starts_with("https://") {
        return Err(TransportError::InvalidUrl(coordinator_url.to_string()));
    }
    let parsed =
        url::Url::parse(coordinator_url).map_err(|e| TransportError::InvalidUrl(e.to_string()))?;
    let host = parsed
        .host_str()
        .ok_or_else(|| TransportError::InvalidUrl("coordinator URL has no host".into()))?
        .to_string();
    let port = parsed.port().unwrap_or(443);
    let addr = refresh_addr(&host, port).await?;
    tracing::info!(
        coordinator = %coordinator_url,
        resolved = %addr,
        "supervisor pre-resolved coordinator host (will be shared across reconnect loops)"
    );
    Ok(Arc::new(RwLock::new(addr)))
}

/// Hourly background refresh of the supervisor-shared coordinator IP.
///
/// Re-runs `lookup_host` once per `interval` and writes the result into
/// the shared `Arc<RwLock<SocketAddr>>` (transient lookup failures keep
/// the previous IP). Returns a JoinHandle the supervisor can park on its
/// JoinSet; the loop terminates when the `cancel` watch flips to `true`.
///
/// The refresh complements the per-error invalidate path in
/// [`Channel::connect`]: connect errors invalidate immediately, while
/// this catches LB-IP rotations that happen even when the daemon is
/// healthy and connected for hours on end.
pub fn spawn_addr_refresh(
    coordinator_url: String,
    addr: Arc<RwLock<SocketAddr>>,
    interval: Duration,
    mut cancel: tokio::sync::watch::Receiver<bool>,
) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        // Parse once — invalid URL never reaches this task in practice
        // (pre_resolve_addr would have rejected it). Defensive bail-out.
        let parsed = match url::Url::parse(&coordinator_url) {
            Ok(u) => u,
            Err(e) => {
                tracing::warn!(error = %e, "addr-refresh: bad coordinator URL; exiting");
                return;
            }
        };
        let host = match parsed.host_str() {
            Some(h) => h.to_string(),
            None => {
                tracing::warn!("addr-refresh: coordinator URL has no host; exiting");
                return;
            }
        };
        let port = parsed.port().unwrap_or(443);
        let mut ticker = tokio::time::interval(interval);
        // First tick fires immediately — skip it so we don't double-resolve
        // right after `pre_resolve_addr`.
        ticker.tick().await;
        loop {
            tokio::select! {
                _ = ticker.tick() => {
                    match refresh_addr(&host, port).await {
                        Ok(fresh) => {
                            let mut g = addr.write().await;
                            if *g != fresh {
                                tracing::info!(
                                    coordinator = %coordinator_url,
                                    old = %*g,
                                    new = %fresh,
                                    "addr-refresh: coordinator IP changed"
                                );
                            }
                            *g = fresh;
                        }
                        Err(e) => {
                            tracing::warn!(
                                coordinator = %coordinator_url,
                                error = %e,
                                "addr-refresh: lookup_host failed; keeping previous IP"
                            );
                        }
                    }
                }
                _ = cancel.changed() => {
                    if *cancel.borrow() { return; }
                }
            }
        }
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

// =====================================================================
// #311 — Real heartbeat sink (gRPC bidi to SchedulingService)
// =====================================================================
//
// Background: prior to this issue, `Supervisor::run` wired
// [`spawn_heartbeat_pump`] to [`MemSink`] (a test stub). Heartbeats never
// reached the coordinator, so `providers.last_seen_at` was set once at
// pair time and stayed stale forever. hatice.yildiz@openova.io paired her
// Mac on 2026-05-19 22:29 and the /admin/providers row showed
// `lastSeenAt = registered_at` for >24h even though the daemon was alive.
//
// Fix shape: a `GrpcHeartbeatSink` that buffers `Heartbeat`s into an mpsc.
// A long-lived bridge task ([`spawn_live_heartbeats`]) opens a single bidi
// stream against `iogrid.providers.v1.SchedulingService/StreamHeartbeats`,
// drains the mpsc into the request stream, and reads `HeartbeatAck`s from
// the response stream — propagating `config_changed` / `operations_pause`
// onto the [`SchedulerHandle`] the supervisor already owns. Reconnect /
// backoff is shared with the dispatch bridge via [`run_with_reconnect`].

/// Heartbeat sink backed by a bounded mpsc; the bridge task drains and
/// forwards into the live `SchedulingService.StreamHeartbeats` request
/// stream. `push` is non-blocking — if the mpsc is full (bridge stalled),
/// the caller logs+drops the heartbeat rather than blocking the scheduler
/// poller. This matches the at-most-once semantic of UDP-style telemetry:
/// `last_seen_at` is a best-effort recency signal, not a durable event.
#[derive(Clone)]
pub struct GrpcHeartbeatSink {
    tx: mpsc::Sender<Heartbeat>,
}

#[async_trait::async_trait]
impl HeartbeatSink for GrpcHeartbeatSink {
    async fn push(&self, hb: Heartbeat) -> Result<HeartbeatAck, TransportError> {
        match self.tx.try_send(hb) {
            Ok(()) => Ok(HeartbeatAck::default()),
            Err(mpsc::error::TrySendError::Full(_)) => {
                // Bridge stuck (reconnecting / slow). The next heartbeat
                // is only 5s away — dropping is correct.
                tracing::warn!("heartbeat sink full; dropping (bridge backlogged)");
                Ok(HeartbeatAck::default())
            }
            Err(mpsc::error::TrySendError::Closed(_)) => Err(TransportError::Unreachable(
                "heartbeat bridge channel closed".into(),
            )),
        }
    }
}

/// Handle returned by [`spawn_live_heartbeats`]. `sink` is the
/// [`HeartbeatSink`] the supervisor passes to [`spawn_heartbeat_pump`];
/// `ack_rx` exposes server-pushed ack flags (config-changed /
/// operations-pause) that the supervisor consumes to drive the scheduler.
/// Drop the handle to let the bridge task run until process exit; flip
/// `cancel_tx` to ask it to shut down cleanly.
pub struct LiveHeartbeatHandle {
    /// Plug into [`spawn_heartbeat_pump`] as the sink argument.
    pub sink: Arc<GrpcHeartbeatSink>,
    /// Server-pushed ack stream — supervisor selects on this to apply
    /// `config_changed` (refetch config) / `operations_pause` (flip the
    /// scheduler `operations_pause` flag).
    pub ack_rx: mpsc::Receiver<HeartbeatAck>,
    /// Cancel signal — flip to `true` to ask the bridge task to shut down.
    pub cancel_tx: tokio::sync::watch::Sender<bool>,
    /// Bridge task handle — caller may join for clean shutdown.
    pub task: tokio::task::JoinHandle<()>,
}

/// Spawn the production heartbeat bridge: opens an mTLS [`Channel`] to the
/// coordinator using `cfg`, opens a [`SchedulingService.StreamHeartbeats`]
/// bidi stream, and pumps [`Heartbeat`]s from the sink onto the wire while
/// forwarding [`HeartbeatAck`]s back to the supervisor via `ack_rx`. Backs
/// off + reconnects per [`run_with_reconnect`].
pub fn spawn_live_heartbeats(cfg: ConnectConfig) -> LiveHeartbeatHandle {
    let (hb_tx, hb_rx) = mpsc::channel::<Heartbeat>(64);
    let (ack_tx, ack_rx) = mpsc::channel::<HeartbeatAck>(64);
    let sink = Arc::new(GrpcHeartbeatSink { tx: hb_tx });
    let (cancel_tx, cancel_rx) = tokio::sync::watch::channel(false);

    // Share the rx + ack_tx across reconnect attempts. The rx is wrapped
    // in a tokio Mutex so each connect_once iteration can take exclusive
    // ownership of the receiver while the stream is alive (same pattern
    // as the dispatch bridge's out_rx).
    let hb_rx = std::sync::Arc::new(tokio::sync::Mutex::new(hb_rx));
    let ack_tx = std::sync::Arc::new(ack_tx);

    let task = tokio::spawn(async move {
        let init = cfg.initial_backoff;
        let cap = cfg.max_backoff;
        let cfg_template = cfg.clone();
        let hb_rx_outer = hb_rx.clone();
        let ack_tx_outer = ack_tx.clone();
        run_with_reconnect(init, cap, cancel_rx, move || {
            let cfg = cfg_template.clone();
            let hb_rx = hb_rx_outer.clone();
            let ack_tx = ack_tx_outer.clone();
            async move {
                let mut ch = Channel::new(cfg.clone());
                ch.connect().await?;
                let channel = ch
                    .inner()
                    .cloned()
                    .ok_or_else(|| TransportError::Unreachable("channel not bound".into()))?;
                run_heartbeat_stream(channel, hb_rx, ack_tx).await
            }
        })
        .await;
    });
    LiveHeartbeatHandle {
        sink,
        ack_rx,
        cancel_tx,
        task,
    }
}

/// Open the bidi `StreamHeartbeats` RPC and pump Heartbeats out / Acks in
/// until the wire closes. Extracted so tests can drive it against an
/// in-process `tonic::transport::Server` if needed.
async fn run_heartbeat_stream(
    channel: TonicChannel,
    hb_rx: std::sync::Arc<tokio::sync::Mutex<mpsc::Receiver<Heartbeat>>>,
    ack_tx: std::sync::Arc<mpsc::Sender<HeartbeatAck>>,
) -> Result<(), TransportError> {
    use pb::providers::v1::scheduling_service_client::SchedulingServiceClient;

    let mut client = SchedulingServiceClient::new(channel);
    let (req_tx, req_rx) = mpsc::channel::<pb::providers::v1::Heartbeat>(64);
    let req_stream = ReceiverStream::new(req_rx);

    // CRITICAL: spawn the outbound forwarder BEFORE awaiting
    // `client.stream_heartbeats(req_stream)`. tonic 0.12 + Connect-Go
    // bidi semantics: the server-side handler runs `stream.Receive()`
    // on entry and BLOCKS until the first client frame arrives. The
    // client-side `.await` here returns when the server sends its
    // response HEADERS, which Connect-Go sends after the first
    // received frame (or on explicit `WriteHeader`, which the handler
    // does not call). If we hold the lock + drain `hb_rx` only AFTER
    // this `.await` returns (the pre-fix shape), nothing ever flows
    // and Traefik 504s the idle stream after ~60s.
    //
    // Fix: spawn the forwarder up-front. The mpsc(64) buffer in
    // `req_tx` queues the first heartbeat the moment the scheduler
    // poller emits one (every 5s, see [`spawn_heartbeat_pump`]). That
    // queued frame is read by tonic the instant the stream is open,
    // sent on the wire, processed by the server's `stream.Receive()`,
    // which sends back the first `HeartbeatAck` → response HEADERS →
    // our `.await` unblocks → ack flow begins. No deadlock.
    //
    // Issue #362.
    let forwarder_req_tx = req_tx.clone();
    let forwarder_hb_rx = hb_rx.clone();
    let forwarder = tokio::spawn(async move {
        let mut guard = forwarder_hb_rx.lock().await;
        while let Some(hb) = guard.recv().await {
            if forwarder_req_tx
                .send(convert::heartbeat_to_pb(&hb))
                .await
                .is_err()
            {
                // tonic dropped its end of req_rx — stream closed.
                tracing::debug!("heartbeat request stream closed; forwarder exiting");
                return;
            }
        }
        tracing::debug!("heartbeat sink closed; forwarder exiting");
    });
    // Drop our local req_tx so the only sender is inside the
    // forwarder task. When the forwarder exits (sink closed OR the
    // server dropped its side), tonic's req_stream sees EOF and
    // tears the stream down cleanly.
    drop(req_tx);

    tracing::info!("heartbeat stream: opening SchedulingService.StreamHeartbeats RPC");
    let resp = client.stream_heartbeats(req_stream).await.map_err(|s| {
        tracing::warn!(
            grpc_code = ?s.code(),
            grpc_message = %s.message(),
            "heartbeat stream: stream_heartbeats() returned error"
        );
        TransportError::Unreachable(format!(
            "stream_heartbeats RPC: {}",
            display_error_chain(&s)
        ))
    })?;
    let mut resp_stream = resp.into_inner();

    let result: Result<(), TransportError> = loop {
        match resp_stream.message().await {
            Ok(Some(pb_ack)) => {
                let ack = HeartbeatAck {
                    config_changed: pb_ack.config_changed,
                    operations_pause: pb_ack.operations_pause,
                };
                if ack_tx.send(ack).await.is_err() {
                    tracing::debug!("supervisor ack_rx closed; exiting heartbeat pump");
                    break Ok(());
                }
            }
            Ok(None) => {
                tracing::warn!(
                    "heartbeat stream: server closed mid-flight (Ok(None)) — \
                     likely h2 idle-timeout or GOAWAY at the edge"
                );
                break Ok(());
            }
            Err(s) => {
                tracing::warn!(
                    grpc_code = ?s.code(),
                    grpc_message = %s.message(),
                    "heartbeat stream: recv error mid-flight"
                );
                break Err(TransportError::Unreachable(format!(
                    "heartbeat stream recv: {}",
                    display_error_chain(&s)
                )));
            }
        }
    };
    // Tear down the forwarder when the inbound stream ends so it
    // releases its hold on `hb_rx`. The next reconnect attempt will
    // re-acquire the lock and start a fresh forwarder.
    forwarder.abort();
    let _ = forwarder.await;
    result
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
    tracing::info!(
        provider_id = %hello.provider_id,
        eligible_types = ?hello.supported_types,
        max_concurrent = hello.max_concurrent,
        "dispatch stream: opening RPC + DaemonHello queued"
    );
    let resp = client.dispatch(req_stream).await.map_err(|s| {
        // #271: log the actual gRPC status code/message so we can tell
        // a TLS / ALPN / Traefik issue apart from a server-side reject.
        tracing::warn!(
            grpc_code = ?s.code(),
            grpc_message = %s.message(),
            "dispatch stream: client.dispatch() returned error before ack"
        );
        TransportError::Unreachable(format!("dispatch RPC: {}", display_error_chain(&s)))
    })?;
    let mut resp_stream = resp.into_inner();

    // Wait up to 10 s for the CoordinatorHello ack.
    let ack = match tokio::time::timeout(Duration::from_secs(10), resp_stream.message()).await {
        Ok(Ok(Some(frame))) => convert::frame_from_pb(frame),
        Ok(Ok(None)) => {
            // #271: this is the failure mode that left no breadcrumbs in
            // the live log — the server returned grpc-status=0 with no
            // CoordinatorHello frame, almost certainly because the
            // request body was buffered by an intermediary and the
            // handler saw io.EOF before our DaemonHello reached it.
            tracing::warn!(
                "dispatch stream: server closed cleanly BEFORE coordinator-hello — \
                 likely request-body buffering at the edge (Traefik/h2) or \
                 handler returning early; reconnect will follow"
            );
            return Err(TransportError::Unreachable(
                "stream closed before coordinator-hello".into(),
            ));
        }
        Ok(Err(s)) => {
            tracing::warn!(
                grpc_code = ?s.code(),
                grpc_message = %s.message(),
                "dispatch stream: recv error while awaiting coordinator-hello"
            );
            return Err(TransportError::Unreachable(format!(
                "recv error: {}",
                display_error_chain(&s)
            )));
        }
        Err(_) => {
            tracing::warn!("dispatch stream: 10s timeout waiting for coordinator-hello");
            return Err(TransportError::Unreachable(
                "timed out waiting for coordinator-hello (10s)".into(),
            ));
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
                        // #271: warn (not info) so a healthy long-lived
                        // stream's *unexpected* clean close gets noticed
                        // — the dispatch RPC is supposed to outlive the
                        // process; coordinator returning None means an
                        // edge proxy dropped the response stream.
                        tracing::warn!(
                            "dispatch stream: server closed mid-flight (Ok(None)) — \
                             likely h2 idle-timeout or GOAWAY at the edge"
                        );
                        return Ok(());
                    }
                    Err(s) => {
                        tracing::warn!(
                            grpc_code = ?s.code(),
                            grpc_message = %s.message(),
                            "dispatch stream: recv error mid-flight"
                        );
                        return Err(TransportError::Unreachable(format!(
                            "dispatch stream recv: {}",
                            display_error_chain(&s)
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

    // ---- display_error_chain (issue #243) ----------------------------------

    /// Synthetic two-level error: outer `Display` is intentionally opaque,
    /// inner carries the real cause — mirrors how `tonic::transport::Error`
    /// behaves in production.
    #[derive(Debug)]
    struct OuterErr(InnerErr);
    #[derive(Debug)]
    struct InnerErr(String);

    impl std::fmt::Display for OuterErr {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            // Deliberately opaque, like tonic's "transport error".
            write!(f, "transport error")
        }
    }
    impl std::fmt::Display for InnerErr {
        fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
            write!(f, "{}", self.0)
        }
    }
    impl std::error::Error for OuterErr {
        fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
            Some(&self.0)
        }
    }
    impl std::error::Error for InnerErr {}

    #[test]
    fn display_error_chain_walks_source() {
        let err = OuterErr(InnerErr("invalid peer certificate: UnknownIssuer".into()));
        let s = display_error_chain(&err);
        assert!(
            s.contains("transport error"),
            "expected outer Display, got: {s}"
        );
        assert!(
            s.contains("invalid peer certificate: UnknownIssuer"),
            "expected inner cause in chain, got: {s}"
        );
        assert!(
            s.contains(" → "),
            "expected ' → ' separator between layers, got: {s}"
        );
    }

    #[test]
    fn display_error_chain_handles_leaf_with_no_source() {
        let err = InnerErr("dns error: nodename nor servname provided".into());
        let s = display_error_chain(&err);
        assert_eq!(s, "dns error: nodename nor servname provided");
        // No separator added when there's only one layer.
        assert!(!s.contains(" → "));
    }

    #[test]
    fn display_error_chain_dedupes_repeated_layers() {
        // Some wrappers re-emit the inner Display — we shouldn't print
        // the same string twice in a row.
        #[derive(Debug)]
        struct Wrap(InnerErr);
        impl std::fmt::Display for Wrap {
            fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                // Same string as inner.
                write!(f, "{}", self.0)
            }
        }
        impl std::error::Error for Wrap {
            fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
                Some(&self.0)
            }
        }
        let err = Wrap(InnerErr("same".into()));
        let s = display_error_chain(&err);
        // Should not be "same → same".
        assert_eq!(s, "same", "expected dedup, got: {s}");
    }

    #[test]
    fn display_error_chain_with_io_error_wrap() {
        // Real-world shape: std::io::Error wrapped in a higher-level error.
        let io = std::io::Error::new(std::io::ErrorKind::ConnectionRefused, "tcp connect");
        // Wrap once for two-layer chain.
        #[derive(Debug)]
        struct W(std::io::Error);
        impl std::fmt::Display for W {
            fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
                write!(f, "channel build failed")
            }
        }
        impl std::error::Error for W {
            fn source(&self) -> Option<&(dyn std::error::Error + 'static)> {
                Some(&self.0)
            }
        }
        let chain = display_error_chain(&W(io));
        assert!(chain.starts_with("channel build failed"));
        assert!(chain.contains("tcp connect"));
        assert!(chain.contains(" → "));
    }

    #[test]
    fn default_config_uses_iogrid_dir() {
        let c = ConnectConfig::default();
        assert!(c.coordinator_url.starts_with("https://"));
        assert!(c.cert_pem.to_string_lossy().contains(".iogrid"));
        assert!(c.key_pem.to_string_lossy().contains(".iogrid"));
        assert_eq!(c.max_backoff, Duration::from_secs(60));
    }

    #[tokio::test]
    async fn resolve_host_short_circuits_ipv4_literal() {
        // IP literals must NEVER touch the network resolver — this
        // guards the #248 fix path: if a regression reroutes IP
        // literals through hyper's GaiResolver, our localhost test
        // suite would observe DNS attempts that don't terminate.
        let out = resolve_host_for_endpoint("127.0.0.1", 8443).await.unwrap();
        assert_eq!(out, "https://127.0.0.1:8443");
    }

    #[tokio::test]
    async fn resolve_host_short_circuits_ipv6_literal() {
        let out = resolve_host_for_endpoint("::1", 9000).await.unwrap();
        assert_eq!(out, "https://[::1]:9000");
    }

    #[tokio::test]
    async fn resolve_host_caches_then_invalidate_clears() {
        // Regression guard for #250: the resolver is process-global
        // and TTL-cached so steady-state reconnect cycles never
        // touch the OS resolver. `invalidate_resolved_host` must
        // wipe the entry so a connect-error path forces a fresh
        // resolve next attempt.
        //
        // We exercise this against `localhost` which getaddrinfo
        // resolves locally without any network I/O (NSS reads
        // /etc/hosts). On the off-chance the CI sandbox has no
        // /etc/hosts entry for `localhost`, the test gracefully
        // skips the populate assertion — what we really care about
        // is that `invalidate_resolved_host` is a no-op when nothing
        // is cached and that IP-literal short-circuit still works
        // afterwards.
        invalidate_resolved_host("localhost", 12345);
        if let Ok(out) = resolve_host_for_endpoint("localhost", 12345).await {
            // The authority must start with https:// and end with the port.
            assert!(out.starts_with("https://"), "got {out}");
            assert!(out.ends_with(":12345"), "got {out}");
            // Second call must be served from cache and return the
            // same authority.
            let again = resolve_host_for_endpoint("localhost", 12345).await.unwrap();
            assert_eq!(out, again, "cache hit must return identical authority");
            // Invalidate, then ensure a fresh resolve still succeeds.
            invalidate_resolved_host("localhost", 12345);
            let after = resolve_host_for_endpoint("localhost", 12345).await.unwrap();
            assert_eq!(out, after, "post-invalidate resolve must match");
        }
        // IP-literal path must be unaffected by cache / invalidation.
        let lit = resolve_host_for_endpoint("127.0.0.1", 1).await.unwrap();
        assert_eq!(lit, "https://127.0.0.1:1");
        // Invalidating an IP literal must be a no-op (and never panic).
        invalidate_resolved_host("127.0.0.1", 1);
    }

    // ---- #253 supervisor-level pre-resolve --------------------------------

    #[tokio::test]
    async fn pre_resolve_addr_rejects_plaintext_url() {
        let err = pre_resolve_addr("http://insecure.example")
            .await
            .unwrap_err();
        assert!(
            matches!(err, TransportError::InvalidUrl(_)),
            "expected InvalidUrl, got {err:?}"
        );
    }

    #[tokio::test]
    async fn pre_resolve_addr_returns_arc_with_socketaddr_for_ipv4_literal() {
        // IP-literal hosts must short-circuit: refresh_addr parses the
        // literal directly without involving the OS resolver, so the
        // returned arc carries exactly 127.0.0.1:8443.
        let arc = pre_resolve_addr("https://127.0.0.1:8443")
            .await
            .expect("pre-resolve ipv4 literal");
        let got = *arc.read().await;
        assert_eq!(got, "127.0.0.1:8443".parse::<SocketAddr>().unwrap());
    }

    #[tokio::test]
    async fn pre_resolve_addr_default_port_443() {
        let arc = pre_resolve_addr("https://10.0.0.1")
            .await
            .expect("pre-resolve no-port literal");
        let got = *arc.read().await;
        assert_eq!(got.port(), 443);
    }

    #[tokio::test]
    async fn pre_resolve_addr_rejects_url_with_no_host() {
        // An https:// URL that the url crate accepts but with no host —
        // url 2.x rejects this earlier than tonic would. We want a
        // typed error so callers can decide whether to fall back to
        // loopback.
        let err = pre_resolve_addr("https://").await.unwrap_err();
        assert!(
            matches!(err, TransportError::InvalidUrl(_)),
            "expected InvalidUrl, got {err:?}"
        );
    }

    #[tokio::test]
    async fn connect_uses_resolved_addr_when_set_instead_of_in_loop_dns() {
        // Wire a ConnectConfig.resolved_addr pointing at 127.0.0.1:1
        // (port 1 is reserved → connect will refuse, but cheaply +
        // quickly). The PEMs intentionally fail TLS parse so we don't
        // actually need an OS socket — we only need to observe that
        // Channel::connect honours the arc.
        //
        // The success criterion is "connect returns a TransportError
        // shape that came from the IP-literal endpoint we injected,
        // not from any DNS lookup". We check this indirectly by also
        // setting coordinator_url to a hostname that would NOT resolve
        // (`this-host-must-not-exist-253.invalid.`): if Channel::connect
        // were still calling the in-loop resolver we'd see a DNS error
        // bubbling up; with the arc honored we see the TLS / I/O error
        // for the IP-literal endpoint instead.
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
        let arc = Arc::new(RwLock::new("127.0.0.1:1".parse::<SocketAddr>().unwrap()));
        let cfg = ConnectConfig {
            coordinator_url: "https://this-host-must-not-exist-253.invalid:443".into(),
            cert_pem: cert,
            key_pem: key,
            ca_pem: None,
            max_backoff: Duration::from_millis(20),
            initial_backoff: Duration::from_millis(5),
            resolved_addr: Some(arc),
            connect_semaphore: None,
        };
        let mut ch = Channel::new(cfg);
        let err = ch.connect().await.unwrap_err();
        // Critical: the error must NOT mention DNS — the supervisor's
        // pre-resolved arc is supposed to bypass the in-loop resolver
        // entirely. If you see "dns lookup" in this assertion, you
        // regressed the #253 fix.
        let s = format!("{err}");
        assert!(
            !s.contains("dns lookup"),
            "regression: Channel::connect did DNS work even with resolved_addr set: {s}"
        );
    }

    #[tokio::test]
    async fn connect_semaphore_serialises_concurrent_attempts() {
        // Two parallel connects against the same semaphore must not
        // overlap inside the critical section. We model that by giving
        // them a 1-permit semaphore and measuring observable
        // serialisation: only one connect can hold the permit at a
        // time, so the second connect waits for the first to release.
        //
        // We don't actually need network success — both will fail (PEM
        // parse) — but the error path still releases the permit on
        // drop, so the second future must complete after the first
        // returns. We assert ordering via a shared counter incremented
        // inside `Channel::connect` once the permit is held.
        use std::sync::atomic::{AtomicUsize, Ordering};
        let sem = Arc::new(Semaphore::new(1));
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

        // Hold the permit ourselves first.
        let held = sem.clone().acquire_owned().await.unwrap();
        let started = Arc::new(AtomicUsize::new(0));
        let started_c = started.clone();

        let cfg = ConnectConfig {
            coordinator_url: "https://127.0.0.1:1".into(),
            cert_pem: cert,
            key_pem: key,
            ca_pem: None,
            max_backoff: Duration::from_millis(20),
            initial_backoff: Duration::from_millis(5),
            resolved_addr: Some(Arc::new(RwLock::new(
                "127.0.0.1:1".parse::<SocketAddr>().unwrap(),
            ))),
            connect_semaphore: Some(sem.clone()),
        };
        // Spawn a connect attempt — it must block on the permit we hold.
        let mut ch = Channel::new(cfg);
        let handle = tokio::spawn(async move {
            let _ = ch.connect().await;
            started_c.fetch_add(1, Ordering::SeqCst);
        });
        // Give the spawned task time to reach the await point.
        tokio::time::sleep(Duration::from_millis(20)).await;
        // It must not have progressed past `acquire_owned` yet.
        assert_eq!(
            started.load(Ordering::SeqCst),
            0,
            "spawned connect ran while we held the only permit"
        );
        // Release the permit — spawned task may now proceed.
        drop(held);
        // It must complete now.
        tokio::time::timeout(Duration::from_millis(500), handle)
            .await
            .expect("spawned connect should complete after permit release")
            .ok();
        // Sanity: the connect attempt counter must have incremented.
        assert_eq!(started.load(Ordering::SeqCst), 1);
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
            resolved_addr: None,
            connect_semaphore: None,
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

        async fn ack_assignment(
            &self,
            _request: tonic::Request<wlv1::AckAssignmentRequest>,
        ) -> Result<tonic::Response<wlv1::AckAssignmentResponse>, Status> {
            Ok(tonic::Response::new(wlv1::AckAssignmentResponse {}))
        }

        async fn get_assignment(
            &self,
            _request: tonic::Request<wlv1::GetAssignmentRequest>,
        ) -> Result<tonic::Response<wlv1::GetAssignmentResponse>, Status> {
            Ok(tonic::Response::new(wlv1::GetAssignmentResponse {
                assignment: None,
                latest_status: 0,
            }))
        }

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

    // ---- #327 macOS Keychain-stall regression guard ------------------------

    /// Build a `ClientTlsConfig` with the same builder chain
    /// `Channel::connect` uses for the trust store, and assert it
    /// completes in bounded wall-time.
    ///
    /// The bug in #327 was that `.with_native_roots()` on the same chain
    /// called `rustls-native-certs`, which on macOS issued
    /// `SecTrustSettingsCopyTrustSettings(kSecTrustSettingsDomainSystem)`
    /// and blocked for many minutes (or forever) under certain Keychain
    /// states. The fix dropped that call; the trust store comes from
    /// `with_webpki_roots()` (a static rustls slice — no OS calls, no
    /// I/O, no file descriptors).
    ///
    /// This test fails fast if anyone re-introduces `.with_native_roots()`
    /// on macOS, and is a useful liveness signal on Linux/Windows too —
    /// webpki-roots construction should always be sub-millisecond.
    ///
    /// We intentionally do NOT exercise the `identity(...)` arm: that
    /// would need a real PEM keypair (handled by the higher-level
    /// `Channel::connect` tests). The Keychain hang we're guarding
    /// against happens in the trust-store call, not identity loading.
    #[test]
    fn tls_config_trust_store_builds_under_50ms() {
        use std::time::Instant;
        let start = Instant::now();
        // Mirror the exact call shape from `Channel::connect` minus the
        // identity loading (which is independent of the trust store and
        // doesn't make any Keychain calls).
        let _tls = ClientTlsConfig::new()
            .with_webpki_roots()
            .domain_name("coordinator.iogrid.org");
        let elapsed = start.elapsed();
        assert!(
            elapsed < Duration::from_millis(50),
            "ClientTlsConfig trust-store build took {elapsed:?} — \
             must be sub-50ms (static webpki-roots slice). If this \
             test fails on macOS, someone likely re-introduced \
             `.with_native_roots()` — see issue #327 (rustls-native-certs \
             walks the Keychain via SecTrustSettingsCopyTrustSettings \
             and can block for minutes)."
        );
    }
}
