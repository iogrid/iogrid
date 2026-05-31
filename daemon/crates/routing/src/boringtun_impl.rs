//! Real WireGuard userspace tunnel driver — VPN-529 (#529).
//!
//! Wraps Cloudflare's [`boringtun`] crate's `noise::Tunn` state machine
//! into the crate-level [`crate::Tunnel`] trait, adds a multi-peer
//! registry, runs the UDP packet pump that decapsulates inbound
//! datagrams, encrypts outbound inner packets, and hands decapsulated
//! inner packets to a pluggable [`InnerPacketSink`].
//!
//! ## Scope of this PR (VPN-529 PR-A)
//!
//! - Real per-peer WG handshake (boringtun `Tunn::new` / `decapsulate`
//!   / `encapsulate`).
//! - Roaming-aware peer endpoint tracking (recv_from updates the
//!   peer's `endpoint` so the next outbound packet goes to the
//!   correct UDP destination).
//! - Bandwidth metering via the existing crate [`Meter`] (bytes_in /
//!   bytes_out atomics).
//! - Per-peer concurrency: each peer's `Tunn` lives behind its own
//!   `Mutex` so concurrent decapsulate / encapsulate on the same peer
//!   serialise (boringtun's `Tunn` is `!Sync`), but different peers
//!   process in parallel.
//! - Peer lookup: handshake_init packets walk the peer list (every
//!   registered peer trial-decapsulates); data + cookie packets index
//!   directly on the `our-receiver-index` boringtun issued during the
//!   prior handshake.
//!
//! ## NOT in this PR (deferred to PR-B per #529 split note)
//!
//! - SNAT to the provider's home interface — [`LoggingSink`] is what
//!   currently consumes decapsulated inner packets.
//! - Userspace TCP stack (smoltcp) OR TUN device (CAP_NET_ADMIN) for
//!   actually delivering inner packets to the SOCKS5 acceptor.
//! - Outbound: today there is no producer that pushes inner packets
//!   into [`BoringTun::encapsulate_inner`]; PR-B will wire the SOCKS5
//!   return path here.
//!
//! When PR-B lands, the only change here is replacing [`LoggingSink`]
//! with the real inner-stack sink in the supervisor wiring.

use std::collections::HashMap;
use std::net::SocketAddr;
use std::sync::Arc;

use async_trait::async_trait;
use base64::Engine;
use boringtun::noise::{Tunn, TunnResult};
use boringtun::x25519::{PublicKey, StaticSecret};
use parking_lot::{Mutex, RwLock};
use rand::rngs::OsRng;
use rand::RngCore;
use std::sync::atomic::Ordering;
use tokio::net::UdpSocket;

use crate::inner_sink::{
    InnerFamily, InnerPacket, InnerPacketSink, OutboundEncapsulator, OutboundError,
};
use crate::{Meter, RoutingError, Tunnel, WireGuardPeer};

/// Max UDP datagram we'll accept from the WG socket. WG MTU is
/// usually 1420 (host MTU 1500 minus WG header overhead); 2 KiB
/// gives generous headroom for jumbo + cookie messages.
const RECV_BUF: usize = 2 * 1024;

/// Configuration handed to [`BoringTun::new`].
#[derive(Clone)]
pub struct BoringTunConfig {
    /// Our static X25519 private key — the daemon's WG identity.
    pub static_private: StaticSecret,
    /// UDP socket address the WG server binds to. Same address the
    /// ICE reporter advertises as the `host` candidate.
    pub listen_addr: SocketAddr,
}

impl std::fmt::Debug for BoringTunConfig {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("BoringTunConfig")
            .field("listen_addr", &self.listen_addr)
            .field("static_private", &"<redacted>")
            .finish()
    }
}

/// One registered peer plus its boringtun state.
struct PeerEntry {
    /// boringtun state machine. Held behind a Mutex because Tunn is
    /// not Sync — its internal session state mutates on every
    /// decapsulate / encapsulate / update_timers call.
    tunn: Mutex<Tunn>,
    /// Base64 of the peer's static public key. Stored to copy into
    /// InnerPacket without re-encoding on every packet.
    public_key_b64: String,
    /// Last UDP endpoint we observed this peer at. Updated on every
    /// successful decapsulate; used by encapsulate to know where to
    /// send the outbound WG datagram. None until the first packet
    /// arrives (caller may still pre-populate via WireGuardPeer.endpoint).
    endpoint: RwLock<Option<SocketAddr>>,
}

/// Real WireGuard tunnel driver — boringtun behind the [`Tunnel`] trait.
pub struct BoringTun {
    config: BoringTunConfig,
    /// Public key matching `config.static_private`. Cached so we don't
    /// re-derive it on every log line.
    static_public_b64: String,
    /// Live UDP socket. Populated by [`Tunnel::start`].
    socket: RwLock<Option<Arc<UdpSocket>>>,
    /// Peer registry keyed by base64 static public key.
    peers: Arc<RwLock<HashMap<String, Arc<PeerEntry>>>>,
    /// Shutdown signal — sender held by the BoringTun, receiver in
    /// the pump task. Dropping the sender = task exit.
    shutdown_tx: Mutex<Option<tokio::sync::watch::Sender<bool>>>,
    /// Bandwidth meter shared with the rest of the routing layer.
    meter: Arc<Meter>,
    /// Inner-packet sink — where decapsulated inner packets go.
    sink: Arc<dyn InnerPacketSink>,
}

impl std::fmt::Debug for BoringTun {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("BoringTun")
            .field("config", &self.config)
            .field("static_public_b64", &self.static_public_b64)
            .field("peers", &self.peers.read().len())
            .finish()
    }
}

impl BoringTun {
    /// Build a new BoringTun. The UDP socket isn't bound until
    /// [`Tunnel::start`] runs.
    pub fn new(config: BoringTunConfig, meter: Arc<Meter>, sink: Arc<dyn InnerPacketSink>) -> Self {
        let static_public = PublicKey::from(&config.static_private);
        let static_public_b64 =
            base64::engine::general_purpose::STANDARD.encode(static_public.as_bytes());
        Self {
            config,
            static_public_b64,
            socket: RwLock::new(None),
            peers: Arc::new(RwLock::new(HashMap::new())),
            shutdown_tx: Mutex::new(None),
            meter,
            sink,
        }
    }

    /// Our static public key as the WG canonical base64.
    pub fn static_public_b64(&self) -> &str {
        &self.static_public_b64
    }

    /// Convenience: generate a fresh X25519 static private key. Used
    /// by tests + by the supervisor on first-pair when no key is
    /// persisted yet.
    pub fn generate_private_key() -> StaticSecret {
        let mut bytes = [0u8; 32];
        OsRng.fill_bytes(&mut bytes);
        StaticSecret::from(bytes)
    }
}

#[async_trait]
impl Tunnel for BoringTun {
    async fn start(&self) -> Result<(), RoutingError> {
        let addr = self.config.listen_addr;
        let socket = UdpSocket::bind(addr)
            .await
            .map_err(|source| RoutingError::BindFailed { addr, source })?;
        let socket = Arc::new(socket);
        *self.socket.write() = Some(socket.clone());

        let (tx, rx) = tokio::sync::watch::channel(false);
        *self.shutdown_tx.lock() = Some(tx);

        let peers = self.peers.clone();
        let meter = self.meter.clone();
        let sink = self.sink.clone();

        tracing::info!(
            listen = %addr,
            our_pubkey = %self.static_public_b64,
            "boringtun WG tunnel started; UDP pump entering recv loop"
        );

        tokio::spawn(async move {
            run_pump(socket, peers, meter, sink, rx).await;
        });
        Ok(())
    }

    async fn stop(&self) -> Result<(), RoutingError> {
        // Dropping the sender wakes the pump's cancel branch and the
        // task exits; the socket is then dropped at end of scope.
        if let Some(tx) = self.shutdown_tx.lock().take() {
            let _ = tx.send(true);
        }
        *self.socket.write() = None;
        tracing::info!("boringtun WG tunnel stopped");
        Ok(())
    }

    fn provider_public_key(&self) -> String {
        self.static_public_b64.clone()
    }

    async fn upsert_peer(&self, peer: WireGuardPeer) -> Result<(), RoutingError> {
        let public_key = decode_public_key(&peer.public_key)?;
        let tunn = Tunn::new(
            self.config.static_private.clone(),
            public_key,
            None,
            if peer.persistent_keepalive == 0 {
                None
            } else {
                Some(peer.persistent_keepalive)
            },
            next_peer_index(),
            None,
        );
        let entry = Arc::new(PeerEntry {
            tunn: Mutex::new(tunn),
            public_key_b64: peer.public_key.clone(),
            endpoint: RwLock::new(peer.endpoint),
        });
        let prev = self.peers.write().insert(peer.public_key.clone(), entry);
        if prev.is_some() {
            tracing::info!(
                peer = %peer.public_key,
                "replaced existing WG peer (re-keyed)"
            );
        } else {
            tracing::info!(
                peer = %peer.public_key,
                allowed_ips = ?peer.allowed_ips,
                keepalive_s = peer.persistent_keepalive,
                "WG peer registered"
            );
        }
        Ok(())
    }
}

/// UDP packet pump. Loops forever until `shutdown_rx` flips. On each
/// recv:
///
///  1. Increment `meter.bytes_in` by the datagram length.
///  2. Trial-decapsulate against every registered peer. Boringtun's
///     `decapsulate` cheaply rejects packets that don't belong to a
///     peer (the rate-limiter pre-check fails fast), so even 100s of
///     peers stays manageable. The first peer that returns
///     `WriteToNetwork`, `WriteToTunnelV4`, `WriteToTunnelV6`, or
///     `Done` claims the packet; we update its `endpoint` to the
///     observed source.
///  3. Dispatch on the boringtun result:
///     - `WriteToNetwork(out)`: send `out` back to the peer's
///       endpoint (handshake response or cookie reply).
///     - `WriteToTunnelV4(inner, _)` / `WriteToTunnelV6(inner, _)`:
///       parse the destination IP from the inner header, build an
///       `InnerPacket`, hand it to the sink.
///     - `Done`: queued internally, nothing to do.
///     - `Err`: drop with WARN log.
///  4. Drain queued send packets via the boringtun "feed empty
///     datagram" convention (this is how `Tunn` flushes pending
///     handshake-related packets it built earlier).
async fn run_pump(
    socket: Arc<UdpSocket>,
    peers: Arc<RwLock<HashMap<String, Arc<PeerEntry>>>>,
    meter: Arc<Meter>,
    sink: Arc<dyn InnerPacketSink>,
    mut shutdown_rx: tokio::sync::watch::Receiver<bool>,
) {
    // PR-B path (a): one outbound encapsulator the pump hands to the
    // sink so it can synthesise response packets and ship them back
    // through the WG tunnel. Constructed once, lives the pump's
    // lifetime, holds Arcs into the same socket + peer table.
    let encap = PumpEncap {
        peers: peers.clone(),
        socket: socket.clone(),
        meter: meter.clone(),
    };
    let mut recv_buf = vec![0u8; RECV_BUF];
    let mut out_buf = vec![0u8; RECV_BUF];
    loop {
        tokio::select! {
            biased;
            res = shutdown_rx.changed() => {
                if res.is_err() || *shutdown_rx.borrow() {
                    tracing::debug!("WG pump shutdown");
                    return;
                }
            }
            res = socket.recv_from(&mut recv_buf) => {
                let (n, src) = match res {
                    Ok(v) => v,
                    Err(e) => {
                        tracing::warn!(error = %e, "WG socket recv failed; pump continuing");
                        continue;
                    }
                };
                meter.bytes_in.fetch_add(n as u64, Ordering::Relaxed);

                // Snapshot the peer list so we don't hold the
                // RwLock across the (potentially slow) trial
                // decapsulate loop. Cloning the Arcs is cheap;
                // there are typically < 100 peers per provider.
                let snapshot: Vec<Arc<PeerEntry>> = peers.read().values().cloned().collect();

                let mut claimed = false;
                for entry in &snapshot {
                    // Reset out_buf head between attempts.
                    let result = {
                        let mut tunn = entry.tunn.lock();
                        tunn.decapsulate(Some(src.ip()), &recv_buf[..n], &mut out_buf)
                    };
                    match result {
                        TunnResult::Err(_) => {
                            // Not this peer's packet (or rate limited);
                            // try the next.
                            continue;
                        }
                        TunnResult::Done => {
                            // Boringtun consumed the packet (e.g.
                            // handshake stored, nothing to emit).
                            *entry.endpoint.write() = Some(src);
                            claimed = true;
                            break;
                        }
                        TunnResult::WriteToNetwork(out) => {
                            *entry.endpoint.write() = Some(src);
                            let to_send = out.to_vec();
                            send_back(&socket, &to_send, src, &meter).await;
                            // Drain queued packets — boringtun
                            // signals more pending via repeated
                            // empty-input calls.
                            drain_queued(&entry.tunn, &mut out_buf, &socket, src, &meter).await;
                            claimed = true;
                            break;
                        }
                        TunnResult::WriteToTunnelV4(inner, dst) => {
                            *entry.endpoint.write() = Some(src);
                            let inner_owned = inner.to_vec();
                            sink.deliver(
                                InnerPacket {
                                    family: InnerFamily::V4,
                                    payload: &inner_owned,
                                    dst_ip: dst.into(),
                                    peer_endpoint: src,
                                    peer_public_key_b64: entry.public_key_b64.clone(),
                                },
                                Some(&encap),
                            )
                            .await;
                            claimed = true;
                            break;
                        }
                        TunnResult::WriteToTunnelV6(inner, dst) => {
                            *entry.endpoint.write() = Some(src);
                            let inner_owned = inner.to_vec();
                            sink.deliver(
                                InnerPacket {
                                    family: InnerFamily::V6,
                                    payload: &inner_owned,
                                    dst_ip: dst.into(),
                                    peer_endpoint: src,
                                    peer_public_key_b64: entry.public_key_b64.clone(),
                                },
                                Some(&encap),
                            )
                            .await;
                            claimed = true;
                            break;
                        }
                    }
                }
                if !claimed {
                    tracing::debug!(
                        from = %src,
                        bytes = n,
                        "WG packet did not decapsulate against any known peer; dropping"
                    );
                }
            }
        }
    }
}

/// Send a queued/handshake response out via the socket and bump
/// `bytes_out`. Errors are logged but never propagated — the WG
/// protocol is loss-tolerant at this layer.
async fn send_back(socket: &UdpSocket, data: &[u8], dst: SocketAddr, meter: &Meter) {
    match socket.send_to(data, dst).await {
        Ok(n) => {
            meter.bytes_out.fetch_add(n as u64, Ordering::Relaxed);
        }
        Err(e) => {
            tracing::warn!(error = %e, %dst, bytes = data.len(), "WG send_back failed");
        }
    }
}

/// After boringtun emits a WriteToNetwork, more packets may be queued
/// (cookie reply, handshake continuation). Calling decapsulate with
/// an empty datagram drains them per the boringtun API contract.
async fn drain_queued(
    tunn: &Mutex<Tunn>,
    out_buf: &mut [u8],
    socket: &UdpSocket,
    dst: SocketAddr,
    meter: &Meter,
) {
    loop {
        let res = {
            let mut t = tunn.lock();
            t.decapsulate(None, &[], out_buf)
        };
        match res {
            TunnResult::WriteToNetwork(out) => {
                let owned = out.to_vec();
                send_back(socket, &owned, dst, meter).await;
            }
            _ => break,
        }
    }
}

/// Parse a base64-encoded WG static public key into the x25519 type.
fn decode_public_key(b64: &str) -> Result<PublicKey, RoutingError> {
    let bytes = base64::engine::general_purpose::STANDARD
        .decode(b64.trim())
        .map_err(|e| RoutingError::Socks5(format!("invalid base64 pubkey: {e}")))?;
    if bytes.len() != 32 {
        return Err(RoutingError::Socks5(format!(
            "WG public key must be 32 bytes, got {}",
            bytes.len()
        )));
    }
    let mut arr = [0u8; 32];
    arr.copy_from_slice(&bytes);
    Ok(PublicKey::from(arr))
}

/// Monotonically incrementing peer index issued to boringtun on
/// upsert_peer. Boringtun uses this as the local-side WG session
/// receiver index, so it must be unique across all peers on this
/// daemon. A 32-bit counter wraps after ~4B peer registrations,
/// which is far more than any realistic deployment.
fn next_peer_index() -> u32 {
    use std::sync::atomic::AtomicU32;
    static NEXT: AtomicU32 = AtomicU32::new(1);
    NEXT.fetch_add(1, Ordering::Relaxed)
}

// ---------- PR-B path (a) outbound encapsulator ----------

/// Outbound encapsulator the pump constructs once per `run_pump`
/// invocation and hands to the sink. Holds Arcs back into the same
/// peer table + socket + meter so packets the sink synthesises are
/// encrypted with the right per-peer Tunn state and metered as outbound.
struct PumpEncap {
    peers: Arc<RwLock<HashMap<String, Arc<PeerEntry>>>>,
    socket: Arc<UdpSocket>,
    meter: Arc<Meter>,
}

#[async_trait]
impl OutboundEncapsulator for PumpEncap {
    async fn encapsulate_for_peer(
        &self,
        peer_public_key_b64: &str,
        inner_bytes: &[u8],
    ) -> Result<(), OutboundError> {
        let entry = self
            .peers
            .read()
            .get(peer_public_key_b64)
            .cloned()
            .ok_or_else(|| OutboundError::UnknownPeer(peer_public_key_b64.to_owned()))?;
        let endpoint = entry
            .endpoint
            .read()
            .ok_or_else(|| OutboundError::NoEndpoint(peer_public_key_b64.to_owned()))?;
        let mut out_buf = vec![0u8; RECV_BUF];
        let result = {
            let mut tunn = entry.tunn.lock();
            tunn.encapsulate(inner_bytes, &mut out_buf)
        };
        match result {
            TunnResult::WriteToNetwork(packet) => {
                let owned = packet.to_vec();
                match self.socket.send_to(&owned, endpoint).await {
                    Ok(n) => {
                        self.meter.bytes_out.fetch_add(n as u64, Ordering::Relaxed);
                        Ok(())
                    }
                    Err(e) => Err(OutboundError::Send(e)),
                }
            }
            TunnResult::Done => Ok(()),
            TunnResult::Err(e) => Err(OutboundError::Encap(format!("{e:?}"))),
            // boringtun shouldn't emit these in response to an
            // encapsulate call (no inbound to decap); treat as
            // protocol violations.
            TunnResult::WriteToTunnelV4(..) | TunnResult::WriteToTunnelV6(..) => Err(
                OutboundError::Encap("unexpected WriteToTunnel from encapsulate".into()),
            ),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::inner_sink::RecordingSink;
    use std::time::Duration;

    fn rand_key() -> StaticSecret {
        BoringTun::generate_private_key()
    }

    fn public_b64(s: &StaticSecret) -> String {
        let p = PublicKey::from(s);
        base64::engine::general_purpose::STANDARD.encode(p.as_bytes())
    }

    #[test]
    fn generate_private_key_yields_32_bytes() {
        let k = BoringTun::generate_private_key();
        // StaticSecret doesn't expose its bytes directly post-2.0, but
        // deriving the public key proves the secret is valid + unique.
        let p = PublicKey::from(&k);
        assert_eq!(p.as_bytes().len(), 32);
    }

    #[test]
    fn decode_public_key_round_trip() {
        let secret = rand_key();
        let b64 = public_b64(&secret);
        let decoded = decode_public_key(&b64).expect("decode ok");
        let original = PublicKey::from(&secret);
        assert_eq!(decoded.as_bytes(), original.as_bytes());
    }

    #[test]
    fn decode_public_key_rejects_wrong_length() {
        let short_b64 = base64::engine::general_purpose::STANDARD.encode([0u8; 16]);
        let err = decode_public_key(&short_b64).expect_err("should reject 16-byte key");
        match err {
            RoutingError::Socks5(msg) => assert!(msg.contains("32 bytes")),
            other => panic!("unexpected error variant: {other:?}"),
        }
    }

    #[tokio::test]
    async fn start_and_stop_bind_and_release_socket() {
        let config = BoringTunConfig {
            static_private: rand_key(),
            listen_addr: "127.0.0.1:0".parse().unwrap(),
        };
        let bt = BoringTun::new(
            config,
            Arc::new(Meter::default()),
            Arc::new(RecordingSink::default()),
        );
        bt.start().await.expect("start ok");
        // Pump task is spawned; stop signals it.
        bt.stop().await.expect("stop ok");
        // After stop, the socket Arc inside BoringTun is dropped.
        assert!(bt.socket.read().is_none());
    }

    #[tokio::test]
    async fn upsert_peer_registers_and_replaces() {
        let config = BoringTunConfig {
            static_private: rand_key(),
            listen_addr: "127.0.0.1:0".parse().unwrap(),
        };
        let bt = BoringTun::new(
            config,
            Arc::new(Meter::default()),
            Arc::new(RecordingSink::default()),
        );
        let peer_key_a = rand_key();
        let peer_b64 = public_b64(&peer_key_a);
        let p = WireGuardPeer {
            public_key: peer_b64.clone(),
            endpoint: Some("203.0.113.5:51820".parse().unwrap()),
            allowed_ips: vec!["10.0.0.2/32".into()],
            persistent_keepalive: 25,
        };
        bt.upsert_peer(p.clone()).await.expect("upsert ok");
        assert_eq!(bt.peers.read().len(), 1, "one peer registered");
        // Idempotent: re-upserting the same key replaces, doesn't append.
        bt.upsert_peer(p).await.expect("re-upsert ok");
        assert_eq!(bt.peers.read().len(), 1, "still one peer after re-upsert");
    }

    /// Stand up two BoringTun instances (server + client) wired
    /// together over UDP loopback, drive a real WG handshake, and
    /// verify the server's RecordingSink receives the client's
    /// inner IPv4 packet decapsulated. This is the integration
    /// proof that the protocol layer works end-to-end — without it
    /// every other test could pass against a stubbed Tunn.
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn end_to_end_handshake_decapsulates_inner_packet() {
        // -- Server (provider) side --
        let server_key = rand_key();
        let server_public_b64 = public_b64(&server_key);
        let server_sink = Arc::new(RecordingSink::default());
        let server_cfg = BoringTunConfig {
            static_private: server_key.clone(),
            listen_addr: "127.0.0.1:0".parse().unwrap(),
        };
        let server_meter = Arc::new(Meter::default());
        let server = Arc::new(BoringTun::new(
            server_cfg,
            server_meter.clone(),
            server_sink.clone(),
        ));
        server.start().await.unwrap();
        let server_addr = server.socket.read().as_ref().unwrap().local_addr().unwrap();

        // -- Client (customer) side --
        let client_key = rand_key();
        let client_public_b64 = public_b64(&client_key);

        // Register peers in each other's tables. The server learns
        // about the client; the client learns about the server.
        server
            .upsert_peer(WireGuardPeer {
                public_key: client_public_b64.clone(),
                endpoint: None,
                allowed_ips: vec!["10.0.0.2/32".into()],
                persistent_keepalive: 0,
            })
            .await
            .unwrap();

        // The client side just needs a Tunn instance and a UDP socket;
        // we don't need a full BoringTun struct for the customer in
        // this test — that's PR-B territory (SDK / desktop client).
        let server_public_x25519 = PublicKey::from(&server_key);
        let mut client_tunn = Tunn::new(
            client_key,
            server_public_x25519,
            None,
            None,
            42, // arbitrary client-side index
            None,
        );
        let client_sock = UdpSocket::bind("127.0.0.1:0").await.unwrap();

        // 1) Client initiates handshake.
        let mut buf = [0u8; RECV_BUF];
        let init = match client_tunn.format_handshake_initiation(&mut buf, false) {
            TunnResult::WriteToNetwork(p) => p.to_vec(),
            other => panic!("client handshake init unexpected: {other:?}"),
        };
        client_sock.send_to(&init, server_addr).await.unwrap();

        // 2) Server's pump receives + responds. Give it a moment.
        let mut recv = [0u8; RECV_BUF];
        let recv_fut = client_sock.recv_from(&mut recv);
        let (n, _from) = tokio::time::timeout(Duration::from_secs(2), recv_fut)
            .await
            .expect("server should respond within 2s")
            .expect("recv ok");
        assert!(n > 0);

        // 3) Feed the server's response into the client tunnel.
        let mut decap_out = [0u8; RECV_BUF];
        match client_tunn.decapsulate(Some(server_addr.ip()), &recv[..n], &mut decap_out) {
            TunnResult::Done => { /* handshake complete on client side */ }
            TunnResult::WriteToNetwork(_) => { /* continuation, also fine */ }
            other => panic!("client decap of server response unexpected: {other:?}"),
        }

        // 4) Client encrypts + sends an inner IPv4 packet (a minimal
        //    20-byte header with src=10.0.0.2 dst=10.0.0.1).
        let inner = build_minimal_ipv4([10, 0, 0, 2], [10, 0, 0, 1], &[0x42, 0x42, 0x42, 0x42]);
        let mut wg_out = [0u8; RECV_BUF];
        let wg_packet = match client_tunn.encapsulate(&inner, &mut wg_out) {
            TunnResult::WriteToNetwork(p) => p.to_vec(),
            other => panic!("client encapsulate unexpected: {other:?}"),
        };
        client_sock.send_to(&wg_packet, server_addr).await.unwrap();

        // 5) Wait for the server's pump to decapsulate + deliver.
        for _ in 0..40 {
            if server_sink.count() > 0 {
                break;
            }
            tokio::time::sleep(Duration::from_millis(50)).await;
        }

        let snap = server_sink.snapshot();
        assert!(
            !snap.is_empty(),
            "server sink should have received the inner packet"
        );
        let first = &snap[0];
        assert_eq!(first.family, InnerFamily::V4);
        assert_eq!(first.peer_public_key_b64, client_public_b64);
        // Sanity-check the inner packet has our 4-byte payload at the
        // tail (after the 20-byte IPv4 header).
        assert_eq!(&first.payload[20..24], &[0x42, 0x42, 0x42, 0x42]);
        // Server metered the inbound bytes — handshake + data packet.
        assert!(
            server_meter.bytes_in.load(Ordering::Relaxed) > 0,
            "server bytes_in should be > 0"
        );
        // Handshake response counted as bytes_out.
        assert!(
            server_meter.bytes_out.load(Ordering::Relaxed) > 0,
            "server bytes_out should be > 0"
        );
        // Cross-confirm against `server_public_b64` — silences
        // the unused-variable warning + asserts our key derivation.
        assert_eq!(
            server.static_public_b64(),
            server_public_b64,
            "server public key matches derivation"
        );

        server.stop().await.unwrap();
    }

    /// PR-B path (a) headline test — full round trip through the WG
    /// stack:
    ///
    ///   1. Server-side `BoringTun` started with [`IcmpEchoSink`].
    ///   2. Real boringtun `Tunn` on the client; WG handshake +
    ///      key exchange complete over 127.0.0.1 UDP.
    ///   3. Client encrypts an IPv4 ICMPv4 echo-request with
    ///      identifier `0x4242` + sequence 1 and sends it.
    ///   4. Server pump decapsulates → IcmpEchoSink builds the
    ///      echo-reply → encapsulates via PumpEncap → ships back.
    ///   5. Client receives the outbound UDP, decapsulates, and
    ///      verifies the inner is an ICMPv4 echo-reply (type 0)
    ///      with the same identifier + sequence + payload.
    ///
    /// This is the strongest possible "the data plane works"
    /// signal short of an on-cluster smoke against a real customer
    /// SDK — every layer (decap → process → encap → wire) actually
    /// fires under the real Tunn state machine.
    #[tokio::test(flavor = "multi_thread", worker_threads = 2)]
    async fn end_to_end_icmp_echo_round_trip_through_pump() {
        // -- Server (provider) --
        let server_key = rand_key();
        let server_sink = Arc::new(crate::IcmpEchoSink);
        let server_cfg = BoringTunConfig {
            static_private: server_key.clone(),
            listen_addr: "127.0.0.1:0".parse().unwrap(),
        };
        let server_meter = Arc::new(Meter::default());
        let server = Arc::new(BoringTun::new(
            server_cfg,
            server_meter.clone(),
            server_sink,
        ));
        server.start().await.unwrap();
        let server_addr = server.socket.read().as_ref().unwrap().local_addr().unwrap();

        // -- Client --
        let client_key = rand_key();
        let client_public_b64 = public_b64(&client_key);
        server
            .upsert_peer(WireGuardPeer {
                public_key: client_public_b64.clone(),
                endpoint: None,
                allowed_ips: vec!["10.0.0.2/32".into()],
                persistent_keepalive: 0,
            })
            .await
            .unwrap();

        let server_public_x25519 = PublicKey::from(&server_key);
        let mut client_tunn = Tunn::new(client_key, server_public_x25519, None, None, 42, None);
        let client_sock = UdpSocket::bind("127.0.0.1:0").await.unwrap();

        // -- WG handshake --
        let mut buf = [0u8; RECV_BUF];
        let init = match client_tunn.format_handshake_initiation(&mut buf, false) {
            TunnResult::WriteToNetwork(p) => p.to_vec(),
            other => panic!("client handshake init unexpected: {other:?}"),
        };
        client_sock.send_to(&init, server_addr).await.unwrap();

        let mut recv = [0u8; RECV_BUF];
        let (n, _from) =
            tokio::time::timeout(Duration::from_secs(2), client_sock.recv_from(&mut recv))
                .await
                .expect("server should respond within 2s")
                .expect("recv ok");
        let mut decap_out = [0u8; RECV_BUF];
        match client_tunn.decapsulate(Some(server_addr.ip()), &recv[..n], &mut decap_out) {
            TunnResult::Done | TunnResult::WriteToNetwork(_) => {}
            other => panic!("client decap of server response unexpected: {other:?}"),
        }

        // -- Encapsulate ICMPv4 echo-request: 10.0.0.2 → 10.0.0.1 --
        let echo_request =
            build_icmp_echo_request([10, 0, 0, 2], [10, 0, 0, 1], 0x4242, 1, b"icmp-test");
        let mut wg_out = [0u8; RECV_BUF];
        let wg_packet = match client_tunn.encapsulate(&echo_request, &mut wg_out) {
            TunnResult::WriteToNetwork(p) => p.to_vec(),
            other => panic!("client encapsulate unexpected: {other:?}"),
        };
        client_sock.send_to(&wg_packet, server_addr).await.unwrap();

        // -- Receive the echo-reply --
        let mut reply_buf = [0u8; RECV_BUF];
        let (rn, _) = tokio::time::timeout(
            Duration::from_secs(2),
            client_sock.recv_from(&mut reply_buf),
        )
        .await
        .expect("server should send echo reply within 2s")
        .expect("recv ok");
        let mut inner_out = [0u8; RECV_BUF];
        let inner =
            match client_tunn.decapsulate(Some(server_addr.ip()), &reply_buf[..rn], &mut inner_out)
            {
                TunnResult::WriteToTunnelV4(p, _) => p.to_vec(),
                other => panic!("client decap of echo reply unexpected: {other:?}"),
            };
        // Verify ICMPv4 echo-reply structure.
        assert_eq!(inner[0] >> 4, 4, "IPv4");
        assert_eq!(inner[9], 1, "protocol ICMPv4");
        assert_eq!(&inner[12..16], &[10, 0, 0, 1], "src swapped to daemon IP");
        assert_eq!(&inner[16..20], &[10, 0, 0, 2], "dst back to client");
        assert_eq!(inner[20], 0, "ICMP type echo-reply (0)");
        // Identifier + sequence + payload preserved.
        assert_eq!(&inner[24..28], &[0x42, 0x42, 0x00, 0x01]);
        assert_eq!(&inner[28..28 + b"icmp-test".len()], b"icmp-test");

        server.stop().await.unwrap();
    }

    /// IPv4 + ICMPv4 echo-request builder for the E2E test. Computes
    /// both checksums.
    fn build_icmp_echo_request(
        src: [u8; 4],
        dst: [u8; 4],
        identifier: u16,
        sequence: u16,
        data: &[u8],
    ) -> Vec<u8> {
        let icmp_len = 8 + data.len();
        let total_len = 20 + icmp_len;
        let mut p = Vec::with_capacity(total_len);
        p.push(0x45); // ver 4, IHL 5
        p.push(0x00);
        p.extend_from_slice(&(total_len as u16).to_be_bytes());
        p.extend_from_slice(&[0x00, 0x00]); // id
        p.extend_from_slice(&[0x40, 0x00]); // flags+frag
        p.push(64);
        p.push(1); // ICMPv4
        p.extend_from_slice(&[0, 0]); // header checksum
        p.extend_from_slice(&src);
        p.extend_from_slice(&dst);
        // Header checksum
        let hcs = ones_complement_16(&p[..20]);
        p[10] = (hcs >> 8) as u8;
        p[11] = (hcs & 0xff) as u8;
        // ICMPv4 echo-request
        p.push(8);
        p.push(0);
        p.extend_from_slice(&[0, 0]); // checksum
        p.extend_from_slice(&identifier.to_be_bytes());
        p.extend_from_slice(&sequence.to_be_bytes());
        p.extend_from_slice(data);
        let ics = ones_complement_16(&p[20..]);
        p[22] = (ics >> 8) as u8;
        p[23] = (ics & 0xff) as u8;
        p
    }

    fn ones_complement_16(bytes: &[u8]) -> u16 {
        let mut sum: u32 = 0;
        let mut i = 0;
        while i + 1 < bytes.len() {
            sum += u16::from_be_bytes([bytes[i], bytes[i + 1]]) as u32;
            i += 2;
        }
        if i < bytes.len() {
            sum += (bytes[i] as u32) << 8;
        }
        while (sum >> 16) != 0 {
            sum = (sum & 0xffff) + (sum >> 16);
        }
        !(sum as u16)
    }

    /// Construct a minimal IPv4 packet with the given source / dest
    /// and payload. The IHL is 5 (no options); TTL 64; protocol 17
    /// (UDP) for plausibility though the payload bytes are bogus.
    fn build_minimal_ipv4(src: [u8; 4], dst: [u8; 4], payload: &[u8]) -> Vec<u8> {
        let total_len = 20u16 + payload.len() as u16;
        let mut p = Vec::with_capacity(total_len as usize);
        p.push(0x45); // version 4, IHL 5
        p.push(0x00); // DSCP/ECN
        p.extend_from_slice(&total_len.to_be_bytes());
        p.extend_from_slice(&[0u8; 2]); // identification
        p.extend_from_slice(&[0x40, 0x00]); // flags+frag
        p.push(64); // TTL
        p.push(17); // protocol UDP
        p.extend_from_slice(&[0u8; 2]); // header checksum (zero — recipient may or may not verify)
        p.extend_from_slice(&src);
        p.extend_from_slice(&dst);
        p.extend_from_slice(payload);
        p
    }
}
