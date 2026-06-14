// FAITHFUL reproduction of iogrid daemon WG handshake failure.
// Models boringtun_impl.rs::pump(): for each incoming UDP datagram the daemon
// TRIAL-DECAPSULATES against EVERY registered peer's Tunn, in iteration order,
// and forwards the FIRST WriteToNetwork it gets. Each peer Tunn owns its OWN
// RateLimiter (Tunn::new(...,None) builds a default one) with its OWN random
// cookie secret, and the daemon never calls reset_count on any of them.
//
// With >1 registered peer (prod has several bound sessions), once the limiters
// latch under-load (count>10, never reset), the client's init is answered by a
// cookie from peer A on one tick and peer B on the next — the client can only
// remember one cookie, so its MAC2 never matches the responder that processes
// it, and the handshake never completes. Single-peer converges; multi-peer
// thrashes.  This is exactly the prod 64-byte cookie-reply loop captured on the wire.
use boringtun::noise::{Tunn, TunnResult};
use boringtun::x25519::{StaticSecret, PublicKey};
use boringtun::noise::rate_limiter::RateLimiter;
use std::sync::Arc;
use std::net::{IpAddr, Ipv4Addr};

const SRC: Option<IpAddr> = Some(IpAddr::V4(Ipv4Addr::new(10,0,0,2)));

// One "registered peer" on the server: its Tunn expects a specific client key.
struct ServerPeer { tunn: Tunn }

fn make_server_peer(server_priv: &StaticSecret, client_pub: PublicKey, idx: u32) -> ServerPeer {
    ServerPeer { tunn: Tunn::new(server_priv.clone(), client_pub, None, None, idx, None) }
}

// Mirror boringtun_impl.rs: trial-decap across all peers, return first WriteToNetwork bytes.
fn daemon_pump(peers: &mut [ServerPeer], datagram: &[u8]) -> Option<Vec<u8>> {
    let mut out = [0u8; 2048];
    for p in peers.iter_mut() {
        match p.tunn.decapsulate(SRC, datagram, &mut out) {
            TunnResult::WriteToNetwork(o) => return Some(o.to_vec()),
            TunnResult::Done => return Some(Vec::new()),
            _ => continue, // Err => try next peer
        }
    }
    None
}

fn scenario(num_registered_peers: usize, preload: bool, label: &str) {
    let mut rng = rand::rngs::OsRng;
    let server_priv = StaticSecret::random_from_rng(&mut rng);
    let server_pub  = PublicKey::from(&server_priv);

    // The REAL client we want to connect.
    let client_priv = StaticSecret::random_from_rng(&mut rng);
    let client_pub  = PublicKey::from(&client_priv);
    let mut client = Tunn::new(client_priv, server_pub, None, None, 1000, None);

    // Build the server's registered-peer table. Peer[0] is OUR client; the rest
    // are OTHER bound customers (decoys) — exactly the daemon's peer map with
    // several sessions bound. OUR peer is placed LAST so the decoys' cookies are
    // emitted first (worst-case ordering; the HashMap order is arbitrary in prod).
    let mut peers: Vec<ServerPeer> = Vec::new();
    for i in 0..num_registered_peers.saturating_sub(1) {
        let decoy_priv = StaticSecret::random_from_rng(&mut rng);
        let decoy_pub  = PublicKey::from(&decoy_priv);
        peers.push(make_server_peer(&server_priv, decoy_pub, 10+i as u32));
    }
    peers.push(make_server_peer(&server_priv, client_pub, 1)); // OUR peer, last

    // Preload: drive every limiter past the under-load threshold (10), never reset
    // — mirrors a daemon that has been receiving handshake floods for hours.
    if preload {
        for _ in 0..15 {
            let bogus_priv = StaticSecret::random_from_rng(&mut rng);
            let bogus_pub  = PublicKey::from(&bogus_priv);
            let mut flood = Tunn::new(bogus_priv, server_pub, None, None, 9999, None);
            let mut fb = [0u8; 2048];
            if let TunnResult::WriteToNetwork(p) = flood.format_handshake_initiation(&mut fb, false) {
                let pv = p.to_vec();
                let _ = daemon_pump(&mut peers, &pv);
            }
        }
    }

    let mut completed = false;
    let mut cookie_replies = 0u32;
    let mut handshake_responses = 0u32;
    for _ in 0..60u32 {
        let mut ibuf = [0u8; 2048];
        let init = match client.format_handshake_initiation(&mut ibuf, false) {
            TunnResult::WriteToNetwork(p) => p.to_vec(),
            _ => continue,
        };
        if let Some(resp) = daemon_pump(&mut peers, &init) {
            if resp.is_empty() { continue; }
            let mtype = resp[0];
            if mtype == 3 { cookie_replies += 1; }
            if mtype == 2 { handshake_responses += 1; }
            let mut cbuf = [0u8; 2048];
            let _ = client.decapsulate(None, &resp, &mut cbuf);
            if mtype == 2 { completed = true; break; }
        }
    }
    println!("[{}] registered_peers={} preload_underload={} => COMPLETED={}  cookie_replies={}  handshake_responses={}",
             label, num_registered_peers, preload, completed, cookie_replies, handshake_responses);
}


fn scenario_ab(pump_reset: bool, label: &str) {
    let mut rng = rand::rngs::OsRng;
    let server_priv = StaticSecret::random_from_rng(&mut rng);
    let server_pub  = PublicKey::from(&server_priv);
    let client_priv = StaticSecret::random_from_rng(&mut rng);
    let client_pub  = PublicKey::from(&client_priv);
    let mut client = Tunn::new(client_priv, server_pub, None, None, 1000, None);

    // ONE shared external limiter across the server peer(s) — same as a per-peer
    // default would be, but we hold the handle so we can reset_count() it.
    let limiter = Arc::new(RateLimiter::new(&server_pub, 10));
    let mut peers = vec![ ServerPeer { tunn: Tunn::new(server_priv.clone(), client_pub, None, None, 1, Some(limiter.clone())) } ];

    // Preload past under-load threshold (never reset) — prod state.
    for _ in 0..15 {
        let bp = StaticSecret::random_from_rng(&mut rng);
        let bpub = PublicKey::from(&bp);
        let mut fl = Tunn::new(bp, server_pub, None, None, 9999, None);
        let mut fb = [0u8;2048];
        if let TunnResult::WriteToNetwork(p) = fl.format_handshake_initiation(&mut fb, false) {
            let pv=p.to_vec(); let _ = daemon_pump(&mut peers, &pv);
        }
    }

    let mut completed=false; let mut cookies=0u32; let mut resps=0u32;
    for _ in 0..60u32 {
        if pump_reset { std::thread::sleep(std::time::Duration::from_millis(1100)); limiter.reset_count(); }
        let mut ibuf=[0u8;2048];
        let init = match client.format_handshake_initiation(&mut ibuf, false) {
            TunnResult::WriteToNetwork(p)=>p.to_vec(), _=>continue };
        if let Some(resp)=daemon_pump(&mut peers, &init) {
            if resp.is_empty() { continue; }
            let t=resp[0]; if t==3 {cookies+=1;} if t==2 {resps+=1;}
            let mut cb=[0u8;2048]; let _=client.decapsulate(None,&resp,&mut cb);
            if t==2 { completed=true; break; }
        }
    }
    println!("[A/B {}] pump_reset={} => COMPLETED={} cookie_replies={} handshake_responses={}",
             label, pump_reset, completed, cookies, resps);
}

fn main() {
    scenario(1, true,  "single peer, under-load");
    scenario(2, true,  "TWO peers, under-load (prod has several)");
    scenario(4, true,  "FOUR peers, under-load (prod-like)");
    scenario(4, false, "FOUR peers, NOT under-load (fresh daemon, count<10)");
    println!("--- decisive A/B: ONLY difference is whether reset_count() is pumped ---");
    scenario_ab(false, "daemon as-is (no reset_count)");
    scenario_ab(true,  "with reset_count pumped (the fix)");
}
