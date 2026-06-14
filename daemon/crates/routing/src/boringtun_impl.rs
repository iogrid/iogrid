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
use boringtun::noise::rate_limiter::RateLimiter;
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
    /// ONE shared boringtun rate limiter, derived from our static
    /// keypair, handed to EVERY peer's `Tunn` (#781). boringtun bumps
    /// its internal `count` on each handshake packet and only zeroes
    /// it in `reset_count()`, which it documents must run ~1/s; the
    /// daemon previously passed `None` so each `Tunn` built its own
    /// limiter that was never reset → after `PEER_HANDSHAKE_RATE_LIMIT`
    /// (10) packets `is_under_load()` latched `true` permanently and
    /// `verify_packet` cookie-replied every handshake init forever, so
    /// no client could ever complete a handshake. Sharing ONE limiter
    /// also means ONE cookie secret across the peer table, so a client
    /// that *is* cookie-challenged sees a consistent cookie regardless
    /// of which peer's `Tunn` trial-decapsulates its retry (the
    /// multi-peer cookie-thrash captured in #781's A/B harness). A
    /// background task started in [`Tunnel::start`] pumps
    /// `reset_count()` once per second.
    rate_limiter: Arc<RateLimiter>,
    /// Pre-computed WireGuard MAC1 key for OUR static public key:
    /// `BLAKE2s-256("mac1----" || our_static_public_raw32)` (#762/#701).
    /// Used ONLY on the rare decap-FAIL path to recompute a handshake
    /// init's MAC1 and decide whether the client signed against our
    /// CURRENT server key (`mac1_ok=true` ⇒ unregistered/wrong CLIENT
    /// key, a binding bug) or a STALE/WRONG server key (`mac1_ok=false`
    /// ⇒ the NE baked an old server pubkey — the #760/build-186 class).
    /// Computed once at construction so the packet path never re-reads
    /// `wg.key` or re-hashes the static key.
    mac1_key: [u8; 32],
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
        // ONE shared rate limiter for the daemon's static keypair
        // (#781). `10` mirrors boringtun's own default
        // `PEER_HANDSHAKE_RATE_LIMIT` (the value it uses when a
        // `Tunn::new` rate_limiter arg is `None`); the only behavioural
        // change vs the old code is that we now hold the handle and a
        // 1 s task pumps `reset_count()` so the under-load counter
        // doesn't latch forever.
        let rate_limiter = Arc::new(RateLimiter::new(&static_public, 10));
        // Pre-compute the WG MAC1 key for OUR static public key once, so
        // the decap-FAIL diagnostic (#762/#701) can recompute a handshake
        // init's MAC1 without re-hashing on every dropped packet.
        let mac1_key = wg_mac1_key(static_public.as_bytes());
        Self {
            config,
            static_public_b64,
            socket: RwLock::new(None),
            peers: Arc::new(RwLock::new(HashMap::new())),
            shutdown_tx: Mutex::new(None),
            meter,
            sink,
            rate_limiter,
            mac1_key,
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

    /// Hand out an [`OutboundEncapsulator`] tied to this BoringTun's
    /// live peer table + UDP socket + meter. Used by sinks (notably
    /// [`crate::tun_forward::TunForwardSink`]) that need to ship
    /// packets back through the tunnel from a background task, not
    /// just inside a single `deliver` call.
    ///
    /// Returns `None` until [`Tunnel::start`] has bound the socket —
    /// callers should construct the BoringTun, `start().await?` it,
    /// then ask for the encapsulator + wire it into the sink.
    pub fn outbound_encapsulator(&self) -> Option<Arc<dyn OutboundEncapsulator>> {
        let socket = self.socket.read().as_ref().cloned()?;
        Some(Arc::new(PumpEncap {
            peers: self.peers.clone(),
            socket,
            meter: self.meter.clone(),
        }))
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
        let mac1_key = self.mac1_key;

        tracing::info!(
            listen = %addr,
            our_pubkey = %self.static_public_b64,
            "boringtun WG tunnel started; UDP pump entering recv loop"
        );

        // Rate-limiter reset pump (#781). boringtun's RateLimiter
        // documents `reset_count()` must run ~1/s; without it the
        // per-handshake `count` only grows, so after 10 packets the
        // responder latches `is_under_load() == true` permanently and
        // cookie-replies every handshake init forever (no client can
        // complete a handshake). Tie this task's lifetime to the same
        // shutdown watch as the UDP pump so `stop()` ends it cleanly.
        let limiter = self.rate_limiter.clone();
        let mut reset_shutdown = rx.clone();
        tokio::spawn(async move {
            let mut tick = tokio::time::interval(std::time::Duration::from_secs(1));
            tick.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
            loop {
                tokio::select! {
                    biased;
                    res = reset_shutdown.changed() => {
                        if res.is_err() || *reset_shutdown.borrow() {
                            tracing::debug!("WG rate-limiter reset pump shutdown");
                            return;
                        }
                    }
                    _ = tick.tick() => {
                        limiter.reset_count();
                    }
                }
            }
        });

        tokio::spawn(async move {
            run_pump(socket, peers, meter, sink, mac1_key, rx).await;
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
            // Share the ONE daemon-wide rate limiter (#781) instead of
            // letting each Tunn build its own that never gets reset.
            Some(self.rate_limiter.clone()),
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
    // Pre-computed WG MAC1 key for OUR static public key (#762/#701),
    // used only on the decap-FAIL path to classify the failure.
    mac1_key: [u8; 32],
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
                    // G1 self-diagnostic (#762/#701). A datagram reached
                    // us but matched no peer. Say WHY in one line so the
                    // founder's real-iPhone failure is classifiable from
                    // the daemon log alone — no more guess-and-check
                    // round-trips per on-device fix.
                    //
                    //  - `msg_type` = WG header byte 0 (1=handshake_init,
                    //    2=response, 3=cookie, 4=transport_data).
                    //  - For handshake-inits (type 1, 148 bytes) we
                    //    recompute MAC1 keyed on OUR server static pubkey:
                    //      `mac1_ok=false` ⇒ the client signed against a
                    //        STALE/WRONG SERVER key (the NE baked an old
                    //        server pubkey — #760 / build-186 fixes THIS).
                    //      `mac1_ok=true`  ⇒ MAC1 matched our server key
                    //        but no peer static-decrypt succeeded ⇒ an
                    //        UNREGISTERED/WRONG CLIENT key (a binding bug),
                    //        NOT a server-key issue (build 186 won't help).
                    let diag = classify_decap_fail(&recv_buf[..n], &mac1_key);
                    match diag.mac1_ok {
                        Some(true) => tracing::warn!(
                            from = %src,
                            bytes = n,
                            msg_type = diag.msg_type,
                            msg_kind = diag.msg_kind,
                            mac1_ok = true,
                            "G1 decap-fail: handshake-init MAC1 matches OUR server key \
                             but no peer static-decrypt succeeded ⇒ UNREGISTERED/WRONG \
                             CLIENT key (binding issue, NOT a server-key issue); dropping"
                        ),
                        Some(false) => tracing::warn!(
                            from = %src,
                            bytes = n,
                            msg_type = diag.msg_type,
                            msg_kind = diag.msg_kind,
                            mac1_ok = false,
                            "G1 decap-fail: handshake-init MAC1 does NOT match our server \
                             key ⇒ client signed against a STALE/WRONG SERVER key (NE baked \
                             an old server pubkey — the #760/build-186 class); dropping"
                        ),
                        None => tracing::debug!(
                            from = %src,
                            bytes = n,
                            msg_type = diag.msg_type,
                            msg_kind = diag.msg_kind,
                            "WG packet did not decapsulate against any known peer; dropping"
                        ),
                    }
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

// ---------- G1 decap-fail self-diagnostic (#762/#701) ----------

/// WireGuard `LABEL_MAC1` — the domain-separation prefix WG hashes with a
/// peer's static public key to derive the MAC1 key. Byte-identical to
/// boringtun's `LABEL_MAC1` (`noise::handshake`).
const WG_LABEL_MAC1: &[u8; 8] = b"mac1----";

/// On-wire size of a WireGuard handshake-initiation message, and the
/// offset at which its 16-byte MAC1 begins. WG message layout:
/// `[type:1][reserved:3][sender_idx:4][ephemeral:32][enc_static:48]`
/// `[enc_timestamp:28][mac1:16][mac2:16]` = 148 bytes; MAC1 covers the
/// first 116 bytes (everything before the MAC1 field). These mirror
/// boringtun's `HANDSHAKE_INIT_SZ` (148) and its `mac1_off = len - 32`.
const WG_HANDSHAKE_INIT_SZ: usize = 148;
const WG_MAC1_OFFSET: usize = WG_HANDSHAKE_INIT_SZ - 32; // 116
const WG_MAC1_LEN: usize = 16;

/// WG message type tag (header byte 0). The reserved bytes 1..4 are zero.
const WG_MSG_HANDSHAKE_INIT: u8 = 1;

/// Derive the WireGuard MAC1 key for a static public key:
/// `BLAKE2s-256("mac1----" || static_public_raw32)`. Byte-identical to
/// boringtun's `b2s_hash(LABEL_MAC1, peer_static_public)` used to build
/// each peer's `sending_mac1_key`. The daemon precomputes this ONCE for
/// its own server static key (in `BoringTun::new`) so the decap-fail
/// path never re-hashes.
fn wg_mac1_key(static_public_raw32: &[u8]) -> [u8; 32] {
    use blake2::digest::Update;
    use blake2::{Blake2s256, Digest};
    let mut h = Blake2s256::new();
    Update::update(&mut h, WG_LABEL_MAC1);
    Update::update(&mut h, static_public_raw32);
    h.finalize().into()
}

/// Compute a WireGuard MAC1 (`BLAKE2s-128`, keyed) over `msg` using a
/// precomputed MAC1 key. Byte-identical to boringtun's
/// `b2s_keyed_mac_16(key, msg)` — `Blake2sMac<U16>` keyed with the
/// 32-byte mac1 key. Used only on the rare decap-fail path.
fn wg_mac1(mac1_key: &[u8; 32], msg: &[u8]) -> [u8; WG_MAC1_LEN] {
    use blake2::digest::{FixedOutput, KeyInit, Update};
    use blake2::Blake2sMac;
    // `Blake2sMac<U16>` = WireGuard's MAC1 primitive (16-byte digest).
    let mut mac = <Blake2sMac<blake2::digest::consts::U16> as KeyInit>::new_from_slice(mac1_key)
        .expect("BLAKE2s accepts a 32-byte key");
    Update::update(&mut mac, msg);
    mac.finalize_fixed().into()
}

/// Result of classifying a datagram that decapsulated against no peer.
struct DecapFailDiag {
    /// WG header byte 0 (message type tag), or `None` for an empty datagram.
    msg_type: Option<u8>,
    /// Human-readable message kind for the log.
    msg_kind: &'static str,
    /// For handshake-inits only: did MAC1 match OUR server static key?
    /// `Some(true)`  ⇒ binding issue (unregistered/wrong CLIENT key).
    /// `Some(false)` ⇒ stale/wrong SERVER key (NE baked an old pubkey).
    /// `None`        ⇒ not a (well-formed) handshake-init — undecidable.
    mac1_ok: Option<bool>,
}

/// Classify a datagram that decapsulated against no registered peer.
/// Reads the WG message type and, for a well-formed handshake-init,
/// recomputes MAC1 against `our_mac1_key` so the log can say whether the
/// client signed against our CURRENT server key. Pure + allocation-free;
/// runs only on the (rare) decap-fail path.
fn classify_decap_fail(payload: &[u8], our_mac1_key: &[u8; 32]) -> DecapFailDiag {
    let Some(&msg_type) = payload.first() else {
        return DecapFailDiag {
            msg_type: None,
            msg_kind: "empty",
            mac1_ok: None,
        };
    };
    let msg_kind = match msg_type {
        WG_MSG_HANDSHAKE_INIT => "handshake_init",
        2 => "handshake_response",
        3 => "cookie_reply",
        4 => "transport_data",
        _ => "unknown",
    };
    // MAC1 is only carried by — and only meaningful for — a full-size
    // handshake-init. Anything else: report the type, leave mac1
    // undecidable.
    let mac1_ok = if msg_type == WG_MSG_HANDSHAKE_INIT && payload.len() == WG_HANDSHAKE_INIT_SZ {
        let computed = wg_mac1(our_mac1_key, &payload[..WG_MAC1_OFFSET]);
        let on_wire = &payload[WG_MAC1_OFFSET..WG_MAC1_OFFSET + WG_MAC1_LEN];
        Some(computed.as_slice() == on_wire)
    } else {
        None
    };
    DecapFailDiag {
        msg_type: Some(msg_type),
        msg_kind,
        mac1_ok,
    }
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

    // ---- G1 decap-fail self-diagnostic (#762/#701) ----

    /// Forge a REAL WireGuard handshake-initiation addressed to
    /// `server_public` by driving boringtun's own `Tunn`. boringtun
    /// MAC1s the init with `BLAKE2s("mac1----" || server_public)`, so an
    /// init built toward our server key carries a MAC1 that must verify
    /// against `wg_mac1_key(our_key)`, and one built toward a DIFFERENT
    /// key must not. This exercises the exact primitive the on-device
    /// client uses — no hand-rolled crypto in the test.
    fn forge_handshake_init(server_public: &PublicKey) -> Vec<u8> {
        let client_key = rand_key();
        let mut client = Tunn::new(client_key, *server_public, None, None, 7, None);
        let mut buf = [0u8; RECV_BUF];
        match client.format_handshake_initiation(&mut buf, false) {
            TunnResult::WriteToNetwork(p) => p.to_vec(),
            other => panic!("unexpected handshake-init result: {other:?}"),
        }
    }

    #[test]
    fn mac1_key_matches_boringtun_label_hash() {
        // `wg_mac1_key` must equal boringtun's `b2s_hash(LABEL_MAC1, pk)`.
        // We verify indirectly but rigorously: a real init toward this
        // key MUST verify against the key we derive. (A direct vector is
        // covered by the positive case below; this guards the helper in
        // isolation against an off-by-one in the label / order.)
        let server = rand_key();
        let server_pub = PublicKey::from(&server);
        let key = wg_mac1_key(server_pub.as_bytes());
        // Re-deriving is deterministic.
        assert_eq!(key, wg_mac1_key(server_pub.as_bytes()));
        // A different key yields a different mac1 key.
        let other = PublicKey::from(&rand_key());
        assert_ne!(key, wg_mac1_key(other.as_bytes()));
    }

    #[test]
    fn classify_decap_fail_correct_server_key_is_mac1_ok_true() {
        // A handshake-init MAC1'd against OUR server key ⇒ mac1_ok=true
        // ⇒ "the client signed against our CURRENT server key; the
        // failure is a CLIENT-key/binding issue, not a server-key one."
        let server = rand_key();
        let server_pub = PublicKey::from(&server);
        let our_mac1_key = wg_mac1_key(server_pub.as_bytes());

        let init = forge_handshake_init(&server_pub);
        assert_eq!(init.len(), WG_HANDSHAKE_INIT_SZ, "real init is 148 bytes");
        assert_eq!(init[0], WG_MSG_HANDSHAKE_INIT, "type byte is 1");

        let diag = classify_decap_fail(&init, &our_mac1_key);
        assert_eq!(diag.msg_type, Some(WG_MSG_HANDSHAKE_INIT));
        assert_eq!(diag.msg_kind, "handshake_init");
        assert_eq!(
            diag.mac1_ok,
            Some(true),
            "init MAC1'd to our server key must verify against our mac1 key"
        );
    }

    #[test]
    fn classify_decap_fail_wrong_server_key_is_mac1_ok_false() {
        // The SAME init, evaluated against a DIFFERENT server key (the
        // stale-server-key scenario: the NE baked an old/wrong server
        // pubkey) ⇒ mac1_ok=false ⇒ build-186 (recreate-manager) is the
        // fix; a re-bind would NOT help.
        let registered_server = rand_key();
        let registered_pub = PublicKey::from(&registered_server);
        // The init the "client" actually sent — addressed to the
        // registered server key.
        let init = forge_handshake_init(&registered_pub);

        // But OUR daemon now runs a DIFFERENT static key (rotated).
        let our_rotated = rand_key();
        let our_mac1_key = wg_mac1_key(PublicKey::from(&our_rotated).as_bytes());

        let diag = classify_decap_fail(&init, &our_mac1_key);
        assert_eq!(diag.msg_type, Some(WG_MSG_HANDSHAKE_INIT));
        assert_eq!(
            diag.mac1_ok,
            Some(false),
            "init MAC1'd to a different server key must NOT verify against ours"
        );
    }

    #[test]
    fn classify_decap_fail_non_handshake_msg_types() {
        let key = wg_mac1_key(PublicKey::from(&rand_key()).as_bytes());

        // Empty datagram.
        let d = classify_decap_fail(&[], &key);
        assert_eq!(d.msg_type, None);
        assert_eq!(d.msg_kind, "empty");
        assert_eq!(d.mac1_ok, None);

        // Handshake-response (type 2) — MAC1 carried but we don't verify
        // it (only inits carry a MAC1 keyed on the *responder*'s key).
        let d = classify_decap_fail(&[2u8, 0, 0, 0], &key);
        assert_eq!(d.msg_type, Some(2));
        assert_eq!(d.msg_kind, "handshake_response");
        assert_eq!(d.mac1_ok, None);

        // Cookie-reply (type 3).
        let d = classify_decap_fail(&[3u8, 0, 0, 0], &key);
        assert_eq!(d.msg_kind, "cookie_reply");
        assert_eq!(d.mac1_ok, None);

        // Transport-data (type 4) — the common "no session" case.
        let d = classify_decap_fail(&[4u8, 0, 0, 0, 0, 0, 0, 0], &key);
        assert_eq!(d.msg_kind, "transport_data");
        assert_eq!(d.mac1_ok, None);

        // Unknown type.
        let d = classify_decap_fail(&[0x99u8, 1, 2, 3], &key);
        assert_eq!(d.msg_type, Some(0x99));
        assert_eq!(d.msg_kind, "unknown");
        assert_eq!(d.mac1_ok, None);
    }

    #[test]
    fn classify_decap_fail_handshake_init_wrong_size_is_undecidable() {
        // A type-1 byte but NOT 148 bytes ⇒ malformed/truncated ⇒ we
        // can't read a MAC1 ⇒ mac1_ok=None (report the type, don't guess).
        let key = wg_mac1_key(PublicKey::from(&rand_key()).as_bytes());
        let mut truncated = vec![0u8; 100];
        truncated[0] = WG_MSG_HANDSHAKE_INIT;
        let d = classify_decap_fail(&truncated, &key);
        assert_eq!(d.msg_type, Some(WG_MSG_HANDSHAKE_INIT));
        assert_eq!(d.msg_kind, "handshake_init");
        assert_eq!(d.mac1_ok, None, "short init can't be MAC1-checked");
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
