//! Routing — SOCKS5 acceptor + bidirectional TCP relay, with anti-abuse
//! pre-flight gating and bandwidth metering.
//!
//! WireGuard tunnel
//! ----------------
//!
//! Production deployment runs SOCKS5 on the WireGuard tunnel interface — the
//! customer's connection enters via WG, the daemon accepts on the WG IP. The
//! WG dataplane is delegated to `boringtun` (Cloudflare's userspace impl) and
//! lives behind the `routing-real` feature gate; for the minimum-viable
//! shipment we expose the `Tunnel` trait + a `NoopTunnel` and accept SOCKS5
//! on a plain `SocketAddr` instead (which is the conformance test path).
//!
//! SOCKS5
//! ------
//!
//! We hand-rolled a SOCKS5 parser rather than pull `socks5-server` (~5 KLOC
//! of dependency) so the daemon's static-musl binary stays small. We
//! implement the strict NO_AUTH, CONNECT, TCP-only subset that the iogrid
//! customer side actually emits. RFC 1928 §5 reply codes are returned
//! verbatim (0x05 host-unreachable, 0x02 not-allowed-by-rule).

#![forbid(unsafe_code)]
#![deny(missing_docs)]

mod vpn_listener;
pub use vpn_listener::VpnListener;

pub mod ice;
pub use ice::{
    discover_all, discover_host_candidates, discover_srflx_candidate, IceCandidate, IceConfig,
    IceError, RegisterRequest, REPORT_INTERVAL, STUN_TIMEOUT,
};

pub mod health;
pub use health::{
    notify_offline as notify_health_offline, HealthConfig, HealthError, HealthReport, HealthStatus,
    OfflineReport, HEALTH_INTERVAL, SHUTDOWN_BUDGET,
};

use std::net::SocketAddr;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;

use async_trait::async_trait;
use iogrid_anti_abuse::{Filter, FilterRequest, Verdict};
use iogrid_scheduler::SchedulerHandle;
use serde::{Deserialize, Serialize};
use thiserror::Error;
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::{TcpListener, TcpStream};

/// All routing errors.
#[derive(Debug, Error)]
pub enum RoutingError {
    /// Local listener could not bind.
    #[error("listener bind failed on {addr}: {source}")]
    BindFailed {
        /// Address we tried to bind.
        addr: SocketAddr,
        /// Underlying I/O error.
        #[source]
        source: std::io::Error,
    },
    /// WireGuard handshake failed.
    #[error("WireGuard handshake failed: {0}")]
    HandshakeFailed(String),
    /// Peer disconnected.
    #[error("peer disconnected")]
    PeerGone,
    /// SOCKS5 protocol error.
    #[error("socks5 protocol: {0}")]
    Socks5(String),
}

/// Per-peer WireGuard configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WireGuardPeer {
    /// Base64-encoded peer public key.
    pub public_key: String,
    /// `host:port` of the peer endpoint, if known (else `None` for roaming).
    pub endpoint: Option<SocketAddr>,
    /// CIDR ranges we accept from / route to this peer.
    pub allowed_ips: Vec<String>,
    /// Keepalive interval in seconds (0 disables).
    pub persistent_keepalive: u16,
}

/// WireGuard tunnel driver.
#[async_trait]
pub trait Tunnel: Send + Sync {
    /// Start the tunnel. Returns once interface is up.
    async fn start(&self) -> Result<(), RoutingError>;

    /// Stop the tunnel and release the interface.
    async fn stop(&self) -> Result<(), RoutingError>;

    /// Add or update a peer.
    async fn upsert_peer(&self, peer: WireGuardPeer) -> Result<(), RoutingError>;
}

/// SOCKS5 acceptor running on the daemon side.
#[async_trait]
pub trait SocksAcceptor: Send + Sync {
    /// Bind and accept SOCKS5 connections on `addr`. Loops until cancelled.
    async fn serve(&self, addr: SocketAddr) -> Result<(), RoutingError>;
}

/// No-op tunnel — for tests + scaffold compilation without boringtun.
#[derive(Debug, Default, Clone)]
pub struct NoopTunnel;

#[async_trait]
impl Tunnel for NoopTunnel {
    async fn start(&self) -> Result<(), RoutingError> {
        tracing::debug!("noop tunnel start");
        Ok(())
    }
    async fn stop(&self) -> Result<(), RoutingError> {
        Ok(())
    }
    async fn upsert_peer(&self, _peer: WireGuardPeer) -> Result<(), RoutingError> {
        Ok(())
    }
}

/// Metering counters published to the scheduler + audit stream.
#[derive(Debug, Default)]
pub struct Meter {
    /// Bytes received from customer (inbound).
    pub bytes_in: AtomicU64,
    /// Bytes sent to customer (outbound).
    pub bytes_out: AtomicU64,
    /// Total connections accepted.
    pub connections: AtomicU64,
    /// Connections rejected by anti-abuse.
    pub blocked: AtomicU64,
}

impl Meter {
    /// Total bytes (in+out).
    pub fn total_bytes(&self) -> u64 {
        self.bytes_in.load(Ordering::Relaxed) + self.bytes_out.load(Ordering::Relaxed)
    }
}

/// Real SOCKS5 acceptor with anti-abuse + metering.
pub struct Socks5Server<F: Filter + 'static> {
    /// Anti-abuse filter (pre-flight on every CONNECT).
    pub filter: Arc<F>,
    /// Scheduler handle (so we can record bytes + refuse when paused).
    pub scheduler: SchedulerHandle,
    /// Metering counters (snapshot-able from the ui-bridge).
    pub meter: Arc<Meter>,
    /// Customer id to attribute traffic to (one acceptor per customer
    /// session in production; for v1 we accept any customer that
    /// authenticated via WireGuard PSK).
    pub customer_id: String,
    /// Report bytes-in/out to the scheduler in batches of this many bytes
    /// (1 MiB by default per docs/TECH.md).
    pub batch_bytes: u64,
}

impl<F: Filter + 'static> Socks5Server<F> {
    /// Build a new server. Counters start at zero.
    pub fn new(filter: Arc<F>, scheduler: SchedulerHandle, customer_id: String) -> Self {
        Self {
            filter,
            scheduler,
            meter: Arc::new(Meter::default()),
            customer_id,
            batch_bytes: 1_000_000,
        }
    }
}

#[async_trait]
impl<F: Filter + 'static> SocksAcceptor for Socks5Server<F> {
    async fn serve(&self, addr: SocketAddr) -> Result<(), RoutingError> {
        let listener = TcpListener::bind(addr)
            .await
            .map_err(|source| RoutingError::BindFailed { addr, source })?;
        tracing::info!(%addr, "socks5 acceptor listening");
        loop {
            let (sock, peer) = match listener.accept().await {
                Ok(x) => x,
                Err(err) => {
                    tracing::warn!(%err, "accept failed");
                    continue;
                }
            };
            self.meter.connections.fetch_add(1, Ordering::Relaxed);
            let filter = self.filter.clone();
            let scheduler = self.scheduler.clone();
            let meter = self.meter.clone();
            let customer_id = self.customer_id.clone();
            let batch_bytes = self.batch_bytes;
            tokio::spawn(async move {
                if let Err(err) =
                    handle_socks5(sock, filter, scheduler, meter, customer_id, batch_bytes).await
                {
                    tracing::debug!(%peer, %err, "socks5 session ended");
                }
            });
        }
    }
}

async fn handle_socks5<F: Filter + 'static>(
    mut sock: TcpStream,
    filter: Arc<F>,
    scheduler: SchedulerHandle,
    meter: Arc<Meter>,
    customer_id: String,
    batch_bytes: u64,
) -> Result<(), RoutingError> {
    // ---- 1. Method-selection ----
    // VER NMETHODS METHODS...
    let mut hdr = [0u8; 2];
    sock.read_exact(&mut hdr)
        .await
        .map_err(|e| RoutingError::Socks5(format!("read hello: {e}")))?;
    if hdr[0] != 0x05 {
        return Err(RoutingError::Socks5(format!("bad ver {}", hdr[0])));
    }
    let nmeth = hdr[1] as usize;
    let mut methods = vec![0u8; nmeth];
    sock.read_exact(&mut methods)
        .await
        .map_err(|e| RoutingError::Socks5(format!("read methods: {e}")))?;
    // We only accept NO_AUTH (0x00).
    if !methods.contains(&0x00) {
        sock.write_all(&[0x05, 0xFF])
            .await
            .map_err(|e| RoutingError::Socks5(format!("write reject auth: {e}")))?;
        return Err(RoutingError::Socks5("no acceptable method".into()));
    }
    sock.write_all(&[0x05, 0x00])
        .await
        .map_err(|e| RoutingError::Socks5(format!("write accept auth: {e}")))?;

    // ---- 2. Request ----
    // VER CMD RSV ATYP DST.ADDR DST.PORT
    let mut hdr = [0u8; 4];
    sock.read_exact(&mut hdr)
        .await
        .map_err(|e| RoutingError::Socks5(format!("read req hdr: {e}")))?;
    if hdr[0] != 0x05 {
        return Err(RoutingError::Socks5(format!("bad req ver {}", hdr[0])));
    }
    if hdr[1] != 0x01 {
        // CMD must be CONNECT.
        write_socks5_failure(&mut sock, 0x07).await; // command not supported.
        return Err(RoutingError::Socks5(format!(
            "cmd {} not supported",
            hdr[1]
        )));
    }
    let atyp = hdr[3];
    let dest_host = match atyp {
        0x01 => {
            // IPv4
            let mut a = [0u8; 4];
            sock.read_exact(&mut a)
                .await
                .map_err(|e| RoutingError::Socks5(format!("read v4: {e}")))?;
            format!("{}.{}.{}.{}", a[0], a[1], a[2], a[3])
        }
        0x03 => {
            // Domain name
            let mut len = [0u8; 1];
            sock.read_exact(&mut len)
                .await
                .map_err(|e| RoutingError::Socks5(format!("read len: {e}")))?;
            let mut name = vec![0u8; len[0] as usize];
            sock.read_exact(&mut name)
                .await
                .map_err(|e| RoutingError::Socks5(format!("read name: {e}")))?;
            String::from_utf8(name).map_err(|e| RoutingError::Socks5(e.to_string()))?
        }
        0x04 => {
            // IPv6
            let mut a = [0u8; 16];
            sock.read_exact(&mut a)
                .await
                .map_err(|e| RoutingError::Socks5(format!("read v6: {e}")))?;
            let addr: std::net::Ipv6Addr = a.into();
            addr.to_string()
        }
        other => {
            write_socks5_failure(&mut sock, 0x08).await; // addr type not supported.
            return Err(RoutingError::Socks5(format!("atyp {other} not supported")));
        }
    };
    let mut port_buf = [0u8; 2];
    sock.read_exact(&mut port_buf)
        .await
        .map_err(|e| RoutingError::Socks5(format!("read port: {e}")))?;
    let port = u16::from_be_bytes(port_buf);

    // ---- 3. Scheduler gate (refuse when paused) ----
    if !matches!(scheduler.current(), iogrid_scheduler::State::Active) {
        meter.blocked.fetch_add(1, Ordering::Relaxed);
        write_socks5_failure(&mut sock, 0x02).await; // not allowed by ruleset.
        return Err(RoutingError::Socks5("scheduler paused — refusing".into()));
    }

    // ---- 4. Anti-abuse pre-flight ----
    let req = FilterRequest {
        destination_url: format!("{dest_host}:{port}"),
        customer_id: customer_id.clone(),
        port: Some(port),
        content_hash: None,
    };
    match filter.check(&req).await {
        Ok(Verdict::Block { category, detail }) => {
            tracing::info!(category, detail, dest = %dest_host, port, "anti-abuse blocked");
            meter.blocked.fetch_add(1, Ordering::Relaxed);
            write_socks5_failure(&mut sock, 0x02).await;
            return Err(RoutingError::Socks5(format!(
                "blocked: {category} — {detail}"
            )));
        }
        Ok(_) => {}
        Err(e) => {
            tracing::warn!(%e, "anti-abuse check failed open — allowing");
        }
    }

    // ---- 5. Dial upstream ----
    let upstream = match TcpStream::connect((dest_host.as_str(), port)).await {
        Ok(s) => s,
        Err(err) => {
            tracing::info!(%err, dest = %dest_host, port, "upstream connect failed");
            write_socks5_failure(&mut sock, 0x04).await; // host unreachable.
            return Err(RoutingError::Socks5(format!("upstream: {err}")));
        }
    };
    let bound = upstream.local_addr().ok();
    write_socks5_success(&mut sock, bound).await;

    // ---- 6. Relay with metering ----
    relay_with_meter(sock, upstream, scheduler, meter, batch_bytes).await
}

async fn write_socks5_failure(s: &mut TcpStream, rep: u8) {
    let resp = [0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0];
    let _ = s.write_all(&resp).await;
}

async fn write_socks5_success(s: &mut TcpStream, bound: Option<SocketAddr>) {
    // VER REP RSV ATYP BND.ADDR BND.PORT
    let mut resp = vec![0x05, 0x00, 0x00];
    match bound {
        Some(SocketAddr::V4(v4)) => {
            resp.push(0x01);
            resp.extend_from_slice(&v4.ip().octets());
            resp.extend_from_slice(&v4.port().to_be_bytes());
        }
        Some(SocketAddr::V6(v6)) => {
            resp.push(0x04);
            resp.extend_from_slice(&v6.ip().octets());
            resp.extend_from_slice(&v6.port().to_be_bytes());
        }
        None => {
            resp.extend_from_slice(&[0x01, 0, 0, 0, 0, 0, 0]);
        }
    }
    let _ = s.write_all(&resp).await;
}

async fn relay_with_meter(
    client: TcpStream,
    upstream: TcpStream,
    scheduler: SchedulerHandle,
    meter: Arc<Meter>,
    batch_bytes: u64,
) -> Result<(), RoutingError> {
    let (mut cr, mut cw) = client.into_split();
    let (mut ur, mut uw) = upstream.into_split();

    let scheduler1 = scheduler.clone();
    let meter1 = meter.clone();
    let in_dir = tokio::spawn(async move {
        let mut buf = vec![0u8; 16 * 1024];
        let mut pending: u64 = 0;
        loop {
            let n = match cr.read(&mut buf).await {
                Ok(0) => break,
                Ok(n) => n,
                Err(_) => break,
            };
            if uw.write_all(&buf[..n]).await.is_err() {
                break;
            }
            meter1.bytes_in.fetch_add(n as u64, Ordering::Relaxed);
            pending += n as u64;
            if pending >= batch_bytes {
                scheduler1.record_bytes(pending);
                pending = 0;
            }
        }
        if pending > 0 {
            scheduler1.record_bytes(pending);
        }
    });

    let scheduler2 = scheduler.clone();
    let meter2 = meter.clone();
    let out_dir = tokio::spawn(async move {
        let mut buf = vec![0u8; 16 * 1024];
        let mut pending: u64 = 0;
        loop {
            let n = match ur.read(&mut buf).await {
                Ok(0) => break,
                Ok(n) => n,
                Err(_) => break,
            };
            if cw.write_all(&buf[..n]).await.is_err() {
                break;
            }
            meter2.bytes_out.fetch_add(n as u64, Ordering::Relaxed);
            pending += n as u64;
            if pending >= batch_bytes {
                scheduler2.record_bytes(pending);
                pending = 0;
            }
        }
        if pending > 0 {
            scheduler2.record_bytes(pending);
        }
    });

    let _ = tokio::join!(in_dir, out_dir);
    Ok(())
}

/// No-op SOCKS5 acceptor — for the test harness when no filter is wired up.
#[derive(Debug, Default, Clone)]
pub struct NoopSocksAcceptor;

#[async_trait]
impl SocksAcceptor for NoopSocksAcceptor {
    async fn serve(&self, addr: SocketAddr) -> Result<(), RoutingError> {
        tracing::debug!(%addr, "noop socks acceptor");
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use iogrid_anti_abuse::{InMemoryFilter, RulesetSnapshot};
    use std::collections::HashSet;
    use std::time::Duration;
    use tokio::io::{AsyncReadExt, AsyncWriteExt};
    use tokio::net::TcpListener;

    async fn pick_port() -> std::net::SocketAddr {
        let l = TcpListener::bind("127.0.0.1:0").await.unwrap();
        l.local_addr().unwrap()
    }

    #[tokio::test]
    async fn noop_tunnel_round_trip() {
        let t = NoopTunnel;
        t.start().await.unwrap();
        t.upsert_peer(WireGuardPeer {
            public_key: "AAAA".into(),
            endpoint: None,
            allowed_ips: vec!["0.0.0.0/0".into()],
            persistent_keepalive: 25,
        })
        .await
        .unwrap();
        t.stop().await.unwrap();
    }

    #[tokio::test]
    async fn noop_socks_serves() {
        let a = NoopSocksAcceptor;
        a.serve("127.0.0.1:0".parse().unwrap()).await.unwrap();
    }

    #[tokio::test]
    async fn socks5_relays_bytes_with_metering() {
        // 1. Spin up a tiny "upstream" echo server.
        let upstream_listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let upstream_addr = upstream_listener.local_addr().unwrap();
        tokio::spawn(async move {
            if let Ok((mut s, _)) = upstream_listener.accept().await {
                let mut buf = [0u8; 64];
                while let Ok(n) = s.read(&mut buf).await {
                    if n == 0 {
                        return;
                    }
                    if s.write_all(&buf[..n]).await.is_err() {
                        return;
                    }
                }
            }
        });

        // 2. Spin up the SOCKS5 server.
        let filter = Arc::new(InMemoryFilter::new());
        let scheduler = SchedulerHandle::new(iogrid_scheduler::SchedulerConfig {
            idle_only: false,
            ..Default::default()
        });
        scheduler.set_sensors(iogrid_scheduler::SensorSnapshot {
            idle_secs: u64::MAX,
            ..Default::default()
        });
        let server = Socks5Server::new(filter, scheduler.clone(), "test-customer".into());
        let meter = server.meter.clone();
        let socks_addr = pick_port().await;
        tokio::spawn(async move {
            let _ = server.serve(socks_addr).await;
        });
        tokio::time::sleep(Duration::from_millis(50)).await;

        // 3. Connect as a SOCKS5 client and ask to CONNECT to upstream.
        let mut c = TcpStream::connect(socks_addr).await.unwrap();
        c.write_all(&[0x05, 0x01, 0x00]).await.unwrap();
        let mut resp = [0u8; 2];
        c.read_exact(&mut resp).await.unwrap();
        assert_eq!(resp, [0x05, 0x00]);

        // CONNECT to 127.0.0.1:upstream_port (atyp=ipv4).
        let mut req = vec![0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1];
        req.extend_from_slice(&upstream_addr.port().to_be_bytes());
        c.write_all(&req).await.unwrap();
        let mut hdr = [0u8; 4];
        c.read_exact(&mut hdr).await.unwrap();
        assert_eq!(hdr[0], 0x05);
        assert_eq!(hdr[1], 0x00);
        // skip bound addr+port
        if hdr[3] == 0x01 {
            let mut tail = [0u8; 4 + 2];
            c.read_exact(&mut tail).await.unwrap();
        } else if hdr[3] == 0x04 {
            let mut tail = [0u8; 16 + 2];
            c.read_exact(&mut tail).await.unwrap();
        }

        // 4. Send a small payload and read echo.
        c.write_all(b"ping iogrid!").await.unwrap();
        let mut echo = [0u8; 12];
        c.read_exact(&mut echo).await.unwrap();
        assert_eq!(&echo, b"ping iogrid!");

        // 5. Counters reflect traffic.
        tokio::time::sleep(Duration::from_millis(30)).await;
        assert!(meter.bytes_in.load(Ordering::Relaxed) >= 12);
        assert!(meter.bytes_out.load(Ordering::Relaxed) >= 12);
        assert_eq!(meter.connections.load(Ordering::Relaxed), 1);
    }

    #[tokio::test]
    async fn socks5_blocks_via_antiabuse() {
        let filter = Arc::new(InMemoryFilter::new());
        filter.install(
            RulesetSnapshot {
                blocked_ports: vec![25],
                ..Default::default()
            },
            HashSet::new(),
            HashSet::new(),
        );
        let scheduler = SchedulerHandle::new(iogrid_scheduler::SchedulerConfig {
            idle_only: false,
            ..Default::default()
        });
        scheduler.set_sensors(iogrid_scheduler::SensorSnapshot {
            idle_secs: u64::MAX,
            ..Default::default()
        });
        let server = Socks5Server::new(filter, scheduler, "test-customer".into());
        let meter = server.meter.clone();
        let socks_addr = pick_port().await;
        tokio::spawn(async move {
            let _ = server.serve(socks_addr).await;
        });
        tokio::time::sleep(Duration::from_millis(50)).await;

        let mut c = TcpStream::connect(socks_addr).await.unwrap();
        c.write_all(&[0x05, 0x01, 0x00]).await.unwrap();
        let mut resp = [0u8; 2];
        c.read_exact(&mut resp).await.unwrap();

        // CONNECT to 1.2.3.4:25 — should be blocked by port rule.
        let mut req = vec![0x05, 0x01, 0x00, 0x01, 1, 2, 3, 4];
        req.extend_from_slice(&25u16.to_be_bytes());
        c.write_all(&req).await.unwrap();
        let mut hdr = [0u8; 4];
        c.read_exact(&mut hdr).await.unwrap();
        assert_eq!(hdr[0], 0x05);
        assert_eq!(hdr[1], 0x02, "expected REP=0x02 not_allowed_by_ruleset");
        tokio::time::sleep(Duration::from_millis(20)).await;
        assert_eq!(meter.blocked.load(Ordering::Relaxed), 1);
    }

    #[tokio::test]
    async fn socks5_blocks_when_scheduler_paused() {
        let filter = Arc::new(InMemoryFilter::new());
        let scheduler = SchedulerHandle::new(iogrid_scheduler::SchedulerConfig {
            idle_only: false,
            ..Default::default()
        });
        scheduler.set_manual_pause(true); // immediate pause.
        let server = Socks5Server::new(filter, scheduler, "test-customer".into());
        let meter = server.meter.clone();
        let socks_addr = pick_port().await;
        tokio::spawn(async move {
            let _ = server.serve(socks_addr).await;
        });
        tokio::time::sleep(Duration::from_millis(50)).await;

        let mut c = TcpStream::connect(socks_addr).await.unwrap();
        c.write_all(&[0x05, 0x01, 0x00]).await.unwrap();
        let mut resp = [0u8; 2];
        c.read_exact(&mut resp).await.unwrap();

        let mut req = vec![0x05, 0x01, 0x00, 0x01, 1, 2, 3, 4];
        req.extend_from_slice(&80u16.to_be_bytes());
        c.write_all(&req).await.unwrap();
        let mut hdr = [0u8; 4];
        c.read_exact(&mut hdr).await.unwrap();
        assert_eq!(hdr[1], 0x02, "expected refusal because scheduler paused");
        tokio::time::sleep(Duration::from_millis(20)).await;
        assert!(meter.blocked.load(Ordering::Relaxed) >= 1);
    }
}
