//! End-to-end WG handshake A/B proof harness for #781, driven by a REAL
//! `wg` KERNEL client (the surrounding shell brings the client up in a
//! netns). Reproduces the EXACT prod failure shape and proves the fix on
//! the wire.
//!
//! The prod daemon registers SEVERAL bound peers. Its UDP pump
//! trial-decapsulates an inbound handshake init against EVERY peer's
//! `Tunn` in turn and ships the FIRST `WriteToNetwork` it gets back to
//! the client (see `boringtun_impl.rs::run_pump`).
//!
//!  * LEGACY arm (`NO_RESET=1`): models the pre-#781 code — each peer's
//!    `Tunn` owns its OWN `RateLimiter` (`Tunn::new(.., None)`) with its
//!    OWN cookie secret, and NOTHING ever calls `reset_count()`. Once a
//!    flood latches every limiter under-load, the FIRST decoy peer
//!    answers each init with a cookie reply (WG type 0x03) bound to its
//!    own secret; the real peer never gets to emit a handshake response,
//!    and the client's cookie-retry never matches the decoy that
//!    processes the next init → handshake NEVER completes (`0 B
//!    received`). This is the prod `0300 0000` cookie loop.
//!
//!  * FIXED arm (default): the REAL [`iogrid_routing::BoringTun`] — ONE
//!    shared `RateLimiter` across all peers (so one cookie secret) plus a
//!    1 s `reset_count()` pump. The shared limiter un-latches, the real
//!    peer emits a handshake RESPONSE (type 0x02), and the kernel
//!    client's inner ICMP echo is answered by [`IcmpEchoSink`] →
//!    `latest handshake` + non-zero `transfer: received`.
//!
//! Throwaway keys only (daemon key passed in `DAEMON_PRIV_B64`, NOT the
//! prod key); spare port; never touches prod. Refs #781.

use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use base64::Engine;
use boringtun::noise::{Tunn, TunnResult};
use boringtun::x25519::{PublicKey, StaticSecret};
use parking_lot::Mutex;
use tokio::net::UdpSocket;

use iogrid_routing::{BoringTun, BoringTunConfig, IcmpEchoSink, Meter, Tunnel, WireGuardPeer};

/// Number of OTHER bound customer peers the daemon has registered
/// alongside the real client — prod has several. Decoys precede the real
/// peer in iteration order (worst case; the real HashMap order is
/// arbitrary), so a latched decoy answers first.
const DECOY_PEERS: usize = 3;

#[tokio::main(flavor = "multi_thread", worker_threads = 4)]
async fn main() {
    let b64 = base64::engine::general_purpose::STANDARD;

    let priv_b64 = std::env::var("DAEMON_PRIV_B64").expect("DAEMON_PRIV_B64 env required");
    let priv_bytes: [u8; 32] = b64
        .decode(priv_b64.trim())
        .expect("priv b64")
        .try_into()
        .expect("32 bytes");
    let static_private = StaticSecret::from(priv_bytes);
    let static_public = PublicKey::from(&static_private);

    let client_pub_b64 = std::env::args().nth(1).expect("arg1 = client pubkey b64");
    let port: u16 = std::env::args()
        .nth(2)
        .expect("arg2 = listen port")
        .parse()
        .expect("port");
    let listen_addr: SocketAddr = format!("0.0.0.0:{port}").parse().unwrap();
    let preload_target: SocketAddr = format!("127.0.0.1:{port}").parse().unwrap();

    if std::env::var("NO_RESET").is_ok() {
        eprintln!(
            "[harness] NO_RESET: LEGACY (pre-#781) multi-peer pump — per-peer limiters, never reset"
        );
        run_legacy_broken(static_private, &client_pub_b64, listen_addr).await;
        return;
    }

    // ---- FIXED arm: the real BoringTun (ONE shared limiter + reset pump) ----
    let meter = Arc::new(Meter::default());
    let bt = Arc::new(BoringTun::new(
        BoringTunConfig {
            static_private: static_private.clone(),
            listen_addr,
        },
        meter.clone(),
        Arc::new(IcmpEchoSink),
    ));
    bt.start().await.expect("boringtun start");
    eprintln!(
        "[harness] FIXED daemon up on {listen_addr}, static_pubkey={}",
        b64.encode(static_public.as_bytes())
    );

    // Register DECOY peers first, then the real client — same multi-peer
    // table prod has. With the fix these all share ONE limiter.
    for _ in 0..DECOY_PEERS {
        let decoy_pub = PublicKey::from(&BoringTun::generate_private_key());
        bt.upsert_peer(WireGuardPeer {
            public_key: b64.encode(decoy_pub.as_bytes()),
            endpoint: None,
            allowed_ips: vec!["10.98.0.0/16".into()],
            persistent_keepalive: 0,
        })
        .await
        .expect("upsert decoy");
    }
    bt.upsert_peer(WireGuardPeer {
        public_key: client_pub_b64.clone(),
        endpoint: None,
        allowed_ips: vec!["10.99.0.2/32".into()],
        persistent_keepalive: 0,
    })
    .await
    .expect("upsert client");

    // Preload the shared limiter past the under-load threshold (prod's
    // hours-latched state). The 1 s reset pump zeroes it before the
    // kernel client's handshake, so the client still completes.
    preload_under_load(preload_target, &static_public).await;
    eprintln!("[harness] preload burst done — shared limiter driven past under-load threshold");

    eprintln!("[harness] idling; awaiting kernel wg client handshake. SIGTERM to stop.");
    loop {
        tokio::time::sleep(Duration::from_secs(3600)).await;
    }
}

/// Flood our own listener with bogus handshake inits to drive the
/// responder's rate limiter(s) past the under-load threshold.
async fn preload_under_load(server_addr: SocketAddr, server_pub: &PublicKey) {
    let sock = UdpSocket::bind("127.0.0.1:0").await.unwrap();
    for _ in 0..60u32 {
        let mut t = Tunn::new(
            BoringTun::generate_private_key(),
            *server_pub,
            None,
            None,
            9999,
            None,
        );
        let mut buf = [0u8; 2048];
        if let TunnResult::WriteToNetwork(p) = t.format_handshake_initiation(&mut buf, false) {
            let _ = sock.send_to(p, server_addr).await;
        }
        tokio::time::sleep(Duration::from_millis(3)).await;
    }
}

/// LEGACY (pre-#781) responder: a multi-peer table where each peer's
/// `Tunn` owns its OWN default limiter (`Tunn::new(.., None)`) and
/// NOTHING resets them. Trial-decapsulates across all peers and ships
/// the first `WriteToNetwork` — exactly `run_pump`. After the flood
/// latches the limiters under-load, the first decoy cookie-replies every
/// init and the real peer never completes. Control arm proving the bug.
async fn run_legacy_broken(
    static_private: StaticSecret,
    client_pub_b64: &str,
    listen_addr: SocketAddr,
) {
    let b64 = base64::engine::general_purpose::STANDARD;
    let static_public = PublicKey::from(&static_private);
    let client_pub_bytes: [u8; 32] = b64
        .decode(client_pub_b64.trim())
        .unwrap()
        .try_into()
        .unwrap();
    let client_pub = PublicKey::from(client_pub_bytes);

    let sock = Arc::new(UdpSocket::bind(listen_addr).await.expect("bind legacy"));
    eprintln!(
        "[harness] LEGACY daemon up on {listen_addr}, static_pubkey={}",
        b64.encode(static_public.as_bytes())
    );

    // Build the peer table: DECOY peers first (own limiter each), then
    // the real client (own limiter). Order matters — decoys answer first.
    let mut peers: Vec<Mutex<Tunn>> = Vec::new();
    for i in 0..DECOY_PEERS {
        let decoy_pub = PublicKey::from(&BoringTun::generate_private_key());
        peers.push(Mutex::new(Tunn::new(
            static_private.clone(),
            decoy_pub,
            None,
            None,
            10 + i as u32,
            None, // own limiter — never reset (the bug)
        )));
    }
    peers.push(Mutex::new(Tunn::new(
        static_private.clone(),
        client_pub,
        None,
        None,
        1,
        None, // own limiter — never reset (the bug)
    )));
    let peers = Arc::new(peers);

    // Flood every limiter under-load by trial-decapping bogus inits
    // through the whole table (same path the pump takes).
    for _ in 0..60u32 {
        let mut t = Tunn::new(
            BoringTun::generate_private_key(),
            static_public,
            None,
            None,
            9999,
            None,
        );
        let mut buf = [0u8; 2048];
        if let TunnResult::WriteToNetwork(p) = t.format_handshake_initiation(&mut buf, false) {
            let pv = p.to_vec();
            let mut out = [0u8; 2048];
            for peer in peers.iter() {
                let mut g = peer.lock();
                if let TunnResult::WriteToNetwork(_) =
                    g.decapsulate(Some(listen_addr.ip()), &pv, &mut out)
                {
                    break; // pump returns first WriteToNetwork
                }
            }
        }
    }
    eprintln!(
        "[harness] LEGACY preload done — {} peer limiters latched under-load, NO reset pump",
        peers.len()
    );

    // Pump: trial-decap across peers, ship first WriteToNetwork.
    let mut recv = [0u8; 2048];
    let mut out = [0u8; 2048];
    loop {
        let (n, src) = match sock.recv_from(&mut recv).await {
            Ok(v) => v,
            Err(_) => continue,
        };
        for peer in peers.iter() {
            let res = {
                let mut g = peer.lock();
                g.decapsulate(Some(src.ip()), &recv[..n], &mut out)
            };
            match res {
                TunnResult::WriteToNetwork(o) => {
                    let _ = sock.send_to(o, src).await;
                    break;
                }
                TunnResult::Done
                | TunnResult::WriteToTunnelV4(..)
                | TunnResult::WriteToTunnelV6(..) => break,
                TunnResult::Err(_) => continue,
            }
        }
    }
}
