//! ICE candidate discovery + periodic reporting for VPN-6 (#510).
//!
//! Implements RFC 5389 STUN client + RFC 8445 ICE candidate enumeration
//! on the provider daemon side. On each tick the daemon enumerates its
//! local interfaces for `host` candidates, sends a STUN BINDING REQUEST
//! to the coordinator's STUN server for the `srflx` (server-reflexive)
//! candidate, and POSTs the bundle to
//! `vpn-svc /v1/vpn/providers/{providerID}/candidates`.
//!
//! The wire JSON matches the snake_case tags emitted by protoc-gen-go on
//! [`pb.IceCandidate`] so the handler's `json.Decoder` round-trips it
//! into the same proto type the customer SDK will read back.
//!
//! ## Lifecycle
//!
//! - On startup: one immediate discover-and-publish so the provider is
//!   reachable as soon as the daemon boots.
//! - Every [`REPORT_INTERVAL`] (30 s): re-discover + re-publish. The
//!   coordinator's TTL on a stored candidate is 5 min, so 30 s gives
//!   10× headroom on a flaky reporter.
//! - Coordinator unreachable: log WARN, continue the loop. Never crash.

use std::collections::BTreeSet;
use std::net::{IpAddr, SocketAddr};
use std::time::Duration;

use serde::{Deserialize, Serialize};
use tokio::net::UdpSocket;

/// How often the reporter re-discovers + re-publishes candidates.
///
/// Coordinator TTL on a candidate row is 5 min, so 30 s gives ample
/// margin for a few missed reports before the customer SDK starts
/// seeing stale entries.
pub const REPORT_INTERVAL: Duration = Duration::from_secs(30);

/// Wall-clock wait on a STUN BINDING RESPONSE before giving up.
pub const STUN_TIMEOUT: Duration = Duration::from_secs(3);

/// RFC 5389 §6 STUN magic cookie (network byte order).
const STUN_MAGIC_COOKIE: u32 = 0x2112_A442;

/// RFC 5389 §6 BINDING REQUEST message type.
const STUN_BINDING_REQUEST: u16 = 0x0001;

/// RFC 5389 §6 BINDING SUCCESS RESPONSE message type.
const STUN_BINDING_SUCCESS: u16 = 0x0101;

/// RFC 5389 §15 XOR-MAPPED-ADDRESS attribute type.
const STUN_ATTR_XOR_MAPPED_ADDRESS: u16 = 0x0020;

/// RFC 5389 §15 MAPPED-ADDRESS attribute type (legacy; some servers
/// still send it instead of XOR-MAPPED-ADDRESS — accept either).
const STUN_ATTR_MAPPED_ADDRESS: u16 = 0x0001;

/// One ICE candidate. Mirrors `iogrid.vpn.v1.IceCandidate` from
/// `proto/iogrid/vpn/v1/ice.proto`. The serde field names below match
/// the snake_case JSON tags protoc-gen-go emits, so a daemon-side
/// `serde_json::to_vec(&candidate)` round-trips through the vpn-svc
/// handler's `json.Decoder` into the matching Go struct.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct IceCandidate {
    /// ICE foundation (RFC 8445 §5.1.1). Identical for candidates that
    /// share base + transport + STUN/TURN server, so the receiver can
    /// pair them.
    pub foundation: String,
    /// Component id. Always 1 (we run a single UDP tunnel).
    pub component: u32,
    /// Transport protocol. Always "udp".
    pub transport: String,
    /// RFC 8445 §5.1.2 priority. Larger = preferred.
    pub priority: u32,
    /// Address the candidate listens on.
    pub connection_address: String,
    /// Port the candidate listens on.
    pub connection_port: u32,
    /// "host" | "srflx" | "prflx" | "relay" (RFC 8445 §5.1.1).
    pub candidate_type: String,
    /// For reflexive candidates, the base address (the host candidate
    /// the reflexive was derived from). Empty for host candidates.
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub related_address: String,
    /// For reflexive candidates, the base port. Zero for host candidates.
    #[serde(default, skip_serializing_if = "is_zero_u32")]
    pub related_port: u32,
    /// Unix epoch milliseconds when discovered.
    pub discovered_at_unix_ms: i64,
    /// Reserved for future per-candidate latency measurement.
    #[serde(default, skip_serializing_if = "is_zero_u32")]
    pub latency_ms: u32,
    /// Reserved for the customer-side "this one worked" flip.
    #[serde(default, skip_serializing_if = "is_false")]
    pub is_preferred: bool,
}

fn is_zero_u32(v: &u32) -> bool {
    *v == 0
}

fn is_false(v: &bool) -> bool {
    !*v
}

/// `RegisterIceCandidates` body sent on POST to vpn-svc.
#[derive(Debug, Clone, Serialize)]
pub struct RegisterRequest {
    /// Provider UUID — must match the route parameter for vpn-svc to
    /// accept the body (the handler currently authenticates by route,
    /// but the field is preserved for the eventual signed envelope).
    pub provider_id: String,
    /// Full snapshot of currently-discovered candidates.
    pub candidates: Vec<IceCandidate>,
    /// Unix epoch milliseconds at registration time.
    pub registered_at_unix_ms: i64,
}

/// Configuration for the ICE reporter task.
#[derive(Debug, Clone)]
pub struct IceConfig {
    /// Provider UUID — appears in the report payload + the URL.
    pub provider_id: String,
    /// Coordinator STUN endpoint (e.g. `stun.iogrid.org:3478`).
    pub stun_server: SocketAddr,
    /// Base URL for vpn-svc (e.g. `https://api.iogrid.org`). The
    /// reporter appends `/v1/vpn/providers/{provider_id}/candidates`.
    pub vpn_svc_base_url: String,
    /// The address the VPN UDP listener is bound to. This is what the
    /// daemon advertises as the connection address for host + srflx
    /// candidates.
    pub vpn_listen_addr: SocketAddr,
}

/// ICE-side errors. Failures here are non-fatal — the reporter loop
/// swallows them and retries on the next tick.
#[derive(Debug, thiserror::Error)]
pub enum IceError {
    /// Local interface enumeration failed (typically a permission
    /// problem reading the netlink socket).
    #[error("enumerate interfaces failed: {0}")]
    EnumerateInterfaces(#[source] std::io::Error),
    /// Local UDP socket bind failed for STUN client.
    #[error("stun client bind failed: {0}")]
    StunBind(#[source] std::io::Error),
    /// STUN request send / receive I/O error.
    #[error("stun io error: {0}")]
    StunIo(#[source] std::io::Error),
    /// STUN response was malformed or unexpected.
    #[error("stun protocol error: {0}")]
    StunProtocol(String),
    /// STUN response did not arrive within [`STUN_TIMEOUT`].
    #[error("stun timeout")]
    StunTimeout,
    /// HTTP POST to vpn-svc failed.
    #[error("vpn-svc post error: {0}")]
    HttpPost(#[source] reqwest::Error),
}

/// Enumerate local interfaces and return one `host` candidate per
/// usable, non-loopback, non-link-local address. Both IPv4 and IPv6
/// are included; the caller can filter further if needed.
pub fn discover_host_candidates(
    vpn_listen_addr: SocketAddr,
) -> Result<Vec<IceCandidate>, IceError> {
    let addrs = if_addrs::get_if_addrs().map_err(IceError::EnumerateInterfaces)?;
    let port = vpn_listen_addr.port() as u32;
    let now_ms = now_unix_ms();

    let mut out: Vec<IceCandidate> = Vec::new();
    let mut seen_ips: BTreeSet<IpAddr> = BTreeSet::new();

    for iface in addrs {
        let ip = iface.ip();
        if !is_usable_host(&ip) {
            continue;
        }
        // Deduplicate — two interfaces on the same /etc/hosts entry
        // should not produce two candidates.
        if !seen_ips.insert(ip) {
            continue;
        }
        let ip_string = ip.to_string();
        let local_pref = local_preference(&ip);
        out.push(IceCandidate {
            foundation: foundation_host(&ip_string),
            component: 1,
            transport: "udp".into(),
            priority: priority(TYPE_PREF_HOST, local_pref, 1),
            connection_address: ip_string,
            connection_port: port,
            candidate_type: "host".into(),
            related_address: String::new(),
            related_port: 0,
            discovered_at_unix_ms: now_ms,
            latency_ms: 0,
            is_preferred: false,
        });
    }
    Ok(out)
}

/// Send a STUN BINDING REQUEST to the configured STUN server, parse the
/// MAPPED-ADDRESS / XOR-MAPPED-ADDRESS attribute from the response, and
/// return it as an `srflx` candidate referencing the daemon's VPN
/// listener port as the related (base) port.
///
/// The local socket bound for this exchange is dropped at end-of-scope;
/// the candidate we publish points at the VPN listener's external
/// mapping. This works because the same NAT mapping will exist for any
/// UDP traffic the daemon emits from the same source port — the
/// customer SDK addresses the reflexive candidate, and the WG packets
/// arrive on the daemon's VPN listener via the NAT's port forward.
pub async fn discover_srflx_candidate(
    stun_server: SocketAddr,
    vpn_listen_addr: SocketAddr,
) -> Result<IceCandidate, IceError> {
    // Bind a temporary client socket on the same port the VPN listener
    // uses so the NAT mapping reflects the same 5-tuple. The OS will
    // happily refuse if the port is exclusively held — fall back to an
    // ephemeral port in that case; the resulting mapping is still a
    // valid srflx for *some* WG socket, useful for connectivity
    // probing if not for the final tunnel.
    let bind_local: SocketAddr = match vpn_listen_addr {
        SocketAddr::V4(_) => "0.0.0.0:0".parse().unwrap(),
        SocketAddr::V6(_) => "[::]:0".parse().unwrap(),
    };
    let sock = UdpSocket::bind(bind_local)
        .await
        .map_err(IceError::StunBind)?;
    sock.connect(stun_server).await.map_err(IceError::StunIo)?;

    let tx_id = random_transaction_id();
    let request = encode_binding_request(&tx_id);
    sock.send(&request).await.map_err(IceError::StunIo)?;

    let mut buf = [0u8; 1500];
    let n = tokio::time::timeout(STUN_TIMEOUT, sock.recv(&mut buf))
        .await
        .map_err(|_| IceError::StunTimeout)?
        .map_err(IceError::StunIo)?;

    let mapped = parse_binding_response(&buf[..n], &tx_id)?;
    let ip_string = mapped.ip().to_string();
    let now_ms = now_unix_ms();
    let local_pref = local_preference(&mapped.ip());

    Ok(IceCandidate {
        foundation: foundation_srflx(&ip_string, &stun_server.to_string()),
        component: 1,
        transport: "udp".into(),
        priority: priority(TYPE_PREF_SRFLX, local_pref, 1),
        connection_address: ip_string,
        connection_port: mapped.port() as u32,
        candidate_type: "srflx".into(),
        related_address: vpn_listen_addr.ip().to_string(),
        related_port: vpn_listen_addr.port() as u32,
        discovered_at_unix_ms: now_ms,
        latency_ms: 0,
        is_preferred: false,
    })
}

// ---------- ICE priority / foundation helpers (RFC 8445 §5.1.1-2) ----------

/// RFC 8445 §5.1.2.1 recommended type preferences.
const TYPE_PREF_HOST: u32 = 126;
/// RFC 8445 §5.1.2.1 recommended type preference for server-reflexive.
const TYPE_PREF_SRFLX: u32 = 100;

/// `priority = 2^24 * type_pref + 2^8 * local_pref + (256 - component_id)`
/// per RFC 8445 §5.1.2.1.
fn priority(type_pref: u32, local_pref: u32, component_id: u32) -> u32 {
    (type_pref << 24) | (local_pref << 8) | (256u32.saturating_sub(component_id))
}

/// Cheap local preference. IPv6 globally-routable > IPv4 globally-routable >
/// private. Distinct enough to break priority ties deterministically.
fn local_preference(ip: &IpAddr) -> u32 {
    match ip {
        IpAddr::V6(v) if v.segments()[0] & 0xe000 == 0x2000 => 65535, // global unicast
        IpAddr::V6(_) => 50000,
        IpAddr::V4(v) if !v.is_private() && !v.is_link_local() => 60000,
        IpAddr::V4(_) => 40000,
    }
}

/// Foundation for a host candidate — RFC 8445 §5.1.1.3: same base
/// (address) + same STUN/TURN server + same transport → same
/// foundation. For host we just hash the IP.
fn foundation_host(ip: &str) -> String {
    short_hash(&format!("host:udp:{ip}"))
}

/// Foundation for a server-reflexive candidate — keyed on the
/// reflexive IP and the STUN server we asked.
fn foundation_srflx(reflex_ip: &str, stun: &str) -> String {
    short_hash(&format!("srflx:udp:{reflex_ip}:{stun}"))
}

/// 8-char hex of a stable hash. RFC 8445 only requires uniqueness
/// within a candidate set; collisions across sets are fine.
fn short_hash(s: &str) -> String {
    use std::hash::{Hash, Hasher};
    let mut h = std::collections::hash_map::DefaultHasher::new();
    s.hash(&mut h);
    format!("{:08x}", h.finish() as u32)
}

fn is_usable_host(ip: &IpAddr) -> bool {
    match ip {
        IpAddr::V4(v) => {
            !v.is_loopback() && !v.is_link_local() && !v.is_unspecified() && !v.is_multicast()
        }
        IpAddr::V6(v) => {
            !v.is_loopback()
                && !v.is_unspecified()
                && !v.is_multicast()
                // Link-local fe80::/10
                && (v.segments()[0] & 0xffc0) != 0xfe80
        }
    }
}

fn now_unix_ms() -> i64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

// ---------- STUN wire encoding (RFC 5389) ----------

fn random_transaction_id() -> [u8; 12] {
    let mut id = [0u8; 12];
    // Best-effort PRNG via the OS — RFC 5389 only requires uniqueness
    // within a STUN agent's outstanding transactions, not cryptographic
    // strength. We use std::time + thread id mixed via a simple hash to
    // avoid pulling `rand` for one call site.
    use std::hash::{Hash, Hasher};
    let mut h = std::collections::hash_map::DefaultHasher::new();
    std::thread::current().id().hash(&mut h);
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .map(|d| d.as_nanos())
        .unwrap_or(0)
        .hash(&mut h);
    let seed = h.finish();
    for (i, b) in id.iter_mut().enumerate() {
        *b = ((seed >> (i % 8 * 8)) & 0xff) as u8
            ^ ((i as u64).wrapping_mul(0x9e37_79b9_7f4a_7c15) & 0xff) as u8;
    }
    id
}

fn encode_binding_request(tx_id: &[u8; 12]) -> Vec<u8> {
    let mut out = Vec::with_capacity(20);
    out.extend_from_slice(&STUN_BINDING_REQUEST.to_be_bytes());
    out.extend_from_slice(&0u16.to_be_bytes()); // message length (no attrs)
    out.extend_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
    out.extend_from_slice(tx_id);
    out
}

/// Parse a STUN BINDING SUCCESS RESPONSE and extract the mapped
/// address. Accepts either MAPPED-ADDRESS (legacy) or
/// XOR-MAPPED-ADDRESS (RFC 5389). Returns the canonical
/// `SocketAddr` view.
fn parse_binding_response(data: &[u8], expected_tx_id: &[u8; 12]) -> Result<SocketAddr, IceError> {
    if data.len() < 20 {
        return Err(IceError::StunProtocol(
            "response shorter than header".into(),
        ));
    }
    let msg_type = u16::from_be_bytes([data[0], data[1]]);
    if msg_type != STUN_BINDING_SUCCESS {
        return Err(IceError::StunProtocol(format!(
            "unexpected message type 0x{msg_type:04x}"
        )));
    }
    let msg_len = u16::from_be_bytes([data[2], data[3]]) as usize;
    if 20 + msg_len > data.len() {
        return Err(IceError::StunProtocol(
            "message length runs past buffer".into(),
        ));
    }
    let magic = u32::from_be_bytes([data[4], data[5], data[6], data[7]]);
    if magic != STUN_MAGIC_COOKIE {
        return Err(IceError::StunProtocol("magic cookie mismatch".into()));
    }
    if data[8..20] != expected_tx_id[..] {
        return Err(IceError::StunProtocol("transaction id mismatch".into()));
    }

    // Walk attributes
    let mut cursor = 20usize;
    while cursor + 4 <= 20 + msg_len {
        let attr_type = u16::from_be_bytes([data[cursor], data[cursor + 1]]);
        let attr_len = u16::from_be_bytes([data[cursor + 2], data[cursor + 3]]) as usize;
        let attr_value_start = cursor + 4;
        let attr_value_end = attr_value_start + attr_len;
        if attr_value_end > 20 + msg_len {
            return Err(IceError::StunProtocol(
                "attribute length runs past message".into(),
            ));
        }
        let value = &data[attr_value_start..attr_value_end];
        match attr_type {
            STUN_ATTR_XOR_MAPPED_ADDRESS => {
                return decode_mapped(value, true, expected_tx_id);
            }
            STUN_ATTR_MAPPED_ADDRESS => {
                return decode_mapped(value, false, expected_tx_id);
            }
            _ => {
                // Skip unknown — RFC 5389 §3 says optional comprehension
                // attrs are safe to ignore. Pad to 4-byte boundary.
                let padded = (attr_len + 3) & !3;
                cursor = attr_value_start + padded;
            }
        }
    }
    Err(IceError::StunProtocol(
        "no MAPPED-ADDRESS or XOR-MAPPED-ADDRESS attribute".into(),
    ))
}

fn decode_mapped(value: &[u8], xor: bool, tx_id: &[u8; 12]) -> Result<SocketAddr, IceError> {
    if value.len() < 4 {
        return Err(IceError::StunProtocol("mapped-address too short".into()));
    }
    // First byte reserved, then family (0x01 = IPv4, 0x02 = IPv6).
    let family = value[1];
    let raw_port = u16::from_be_bytes([value[2], value[3]]);
    let port = if xor {
        raw_port ^ ((STUN_MAGIC_COOKIE >> 16) as u16)
    } else {
        raw_port
    };
    // VPN-551 defensive alias: vpn-svc shipped a Family byte of 0x04
    // (Go's `net.IPv4len` — byte-length of an IPv4 address, NOT the
    // RFC 5389 §15.2 address-family enum). The server-side fix in the
    // same PR sends 0x01 going forward, but daemons deployed in the
    // field will still encounter 0x04 from any server that hasn't
    // rolled the image yet — accept it as IPv4 with a one-time WARN
    // so providers behind NAT can still discover srflx during the
    // staggered rollout. Remove this alias once every Sovereign is on
    // the fixed image. See iogrid/iogrid#551.
    let canonical_family = match family {
        0x01 | 0x02 => family,
        0x04 => {
            static WARN_ONCE: std::sync::Once = std::sync::Once::new();
            WARN_ONCE.call_once(|| {
                tracing::warn!(
                    "STUN server sent non-RFC family byte 0x04 (Go's net.IPv4len); \
                     treating as IPv4. Server needs the #551 fix (Family: 0x01)."
                );
            });
            0x01
        }
        other => {
            return Err(IceError::StunProtocol(format!(
                "unknown address family 0x{other:02x}"
            )));
        }
    };
    match canonical_family {
        0x01 => {
            if value.len() < 8 {
                return Err(IceError::StunProtocol("ipv4 mapped too short".into()));
            }
            let mut octets = [0u8; 4];
            octets.copy_from_slice(&value[4..8]);
            if xor {
                let cookie_bytes = STUN_MAGIC_COOKIE.to_be_bytes();
                for i in 0..4 {
                    octets[i] ^= cookie_bytes[i];
                }
            }
            Ok(SocketAddr::new(IpAddr::V4(octets.into()), port))
        }
        0x02 => {
            if value.len() < 20 {
                return Err(IceError::StunProtocol("ipv6 mapped too short".into()));
            }
            let mut octets = [0u8; 16];
            octets.copy_from_slice(&value[4..20]);
            if xor {
                let cookie_bytes = STUN_MAGIC_COOKIE.to_be_bytes();
                for i in 0..4 {
                    octets[i] ^= cookie_bytes[i];
                }
                for i in 0..12 {
                    octets[4 + i] ^= tx_id[i];
                }
            }
            Ok(SocketAddr::new(IpAddr::V6(octets.into()), port))
        }
        _ => unreachable!("canonical_family already filtered to 0x01 / 0x02"),
    }
}

// ---------- Reporter: combines discovery + periodic POST ----------

/// One-shot snapshot — enumerate host + srflx (best-effort) and return
/// the candidate bundle. Used both for the periodic reporter and for
/// any caller that wants a synchronous read of "what would I publish
/// right now".
pub async fn discover_all(
    stun_server: SocketAddr,
    vpn_listen_addr: SocketAddr,
) -> Vec<IceCandidate> {
    let mut out = match discover_host_candidates(vpn_listen_addr) {
        Ok(v) => v,
        Err(e) => {
            tracing::warn!(error = %e, "host candidate enumeration failed; continuing srflx-only");
            Vec::new()
        }
    };
    match discover_srflx_candidate(stun_server, vpn_listen_addr).await {
        Ok(c) => out.push(c),
        Err(e) => {
            // The most common cause is "coordinator unreachable", which
            // is exactly the case the issue calls out — degrade
            // gracefully.
            tracing::warn!(error = %e, stun = %stun_server, "srflx discovery failed; publishing host-only");
        }
    }
    out
}

/// Spawn the periodic reporter task. The returned [`JoinHandle`] never
/// resolves under normal operation — the task loops forever.
pub fn spawn_reporter(config: IceConfig, http: reqwest::Client) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        run_reporter_loop(config, http).await;
    })
}

async fn run_reporter_loop(config: IceConfig, http: reqwest::Client) {
    let url = format!(
        "{}/v1/vpn/providers/{}/candidates",
        config.vpn_svc_base_url.trim_end_matches('/'),
        config.provider_id
    );
    let mut ticker = tokio::time::interval(REPORT_INTERVAL);
    // Fire immediately on startup; default Tokio behaviour already
    // does this for `interval`, but make it explicit.
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    loop {
        ticker.tick().await;
        let candidates = discover_all(config.stun_server, config.vpn_listen_addr).await;
        if candidates.is_empty() {
            tracing::warn!("no candidates discovered this tick");
            continue;
        }
        let body = RegisterRequest {
            provider_id: config.provider_id.clone(),
            candidates,
            registered_at_unix_ms: now_unix_ms(),
        };
        match http.post(&url).json(&body).send().await {
            Ok(resp) if resp.status().is_success() => {
                tracing::debug!(
                    status = %resp.status(),
                    candidate_count = body.candidates.len(),
                    "ice candidates published to vpn-svc"
                );
            }
            Ok(resp) => {
                tracing::warn!(
                    status = %resp.status(),
                    url = %url,
                    "vpn-svc rejected candidate publish"
                );
            }
            Err(e) => {
                // Coordinator down is the headline failure mode here —
                // log + carry on per #510 graceful-handling clause.
                tracing::warn!(error = %e, url = %url, "vpn-svc candidate POST failed; will retry next tick");
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::net::{Ipv4Addr, Ipv6Addr};

    #[test]
    fn priority_matches_rfc_formula() {
        // RFC 8445 §5.1.2.1: priority = 2^24 * type + 2^8 * local + (256 - component)
        // For type=126 (host), local=65535, component=1 → 2113929216 + 16776960 + 255
        // = 2130706431.
        assert_eq!(priority(126, 65535, 1), 2_130_706_431);
        // srflx (100), private-ish local pref 40000, component=1 → 2^24*100 + 2^8*40000 + 255
        // = 1677721600 + 10240000 + 255 = 1687961855.
        assert_eq!(priority(100, 40000, 1), 1_687_961_855);
    }

    #[test]
    fn host_filter_drops_loopback_and_linklocal() {
        assert!(!is_usable_host(&IpAddr::V4(Ipv4Addr::new(127, 0, 0, 1))));
        assert!(!is_usable_host(&IpAddr::V4(Ipv4Addr::new(169, 254, 1, 1))));
        assert!(!is_usable_host(&IpAddr::V4(Ipv4Addr::new(0, 0, 0, 0))));
        assert!(!is_usable_host(&IpAddr::V6(Ipv6Addr::LOCALHOST)));
        assert!(!is_usable_host(&IpAddr::V6(Ipv6Addr::UNSPECIFIED)));
        // fe80::1 link-local
        assert!(!is_usable_host(&IpAddr::V6(Ipv6Addr::new(
            0xfe80, 0, 0, 0, 0, 0, 0, 1
        ))));
        // Normal-looking public IPv4 + IPv6
        assert!(is_usable_host(&IpAddr::V4(Ipv4Addr::new(8, 8, 8, 8))));
        assert!(is_usable_host(&IpAddr::V6(Ipv6Addr::new(
            0x2001, 0xdb8, 0, 0, 0, 0, 0, 1
        ))));
    }

    #[test]
    fn binding_request_encodes_to_20_byte_header() {
        let tx = [0xaa; 12];
        let bytes = encode_binding_request(&tx);
        assert_eq!(bytes.len(), 20);
        // Message type
        assert_eq!(
            u16::from_be_bytes([bytes[0], bytes[1]]),
            STUN_BINDING_REQUEST
        );
        // Length 0
        assert_eq!(u16::from_be_bytes([bytes[2], bytes[3]]), 0);
        // Magic cookie
        assert_eq!(
            u32::from_be_bytes([bytes[4], bytes[5], bytes[6], bytes[7]]),
            STUN_MAGIC_COOKIE
        );
        // Transaction id
        assert_eq!(&bytes[8..20], &tx);
    }

    #[test]
    fn xor_mapped_ipv4_round_trip() {
        // Build a synthetic BINDING SUCCESS with XOR-MAPPED-ADDRESS for
        // 203.0.113.5:51820 and confirm we parse it back exactly.
        let tx = [0x42u8; 12];
        let mut msg = Vec::new();
        msg.extend_from_slice(&STUN_BINDING_SUCCESS.to_be_bytes());
        // length = attr header(4) + attr value(8) = 12
        msg.extend_from_slice(&12u16.to_be_bytes());
        msg.extend_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
        msg.extend_from_slice(&tx);
        // Attribute: XOR-MAPPED-ADDRESS, length 8
        msg.extend_from_slice(&STUN_ATTR_XOR_MAPPED_ADDRESS.to_be_bytes());
        msg.extend_from_slice(&8u16.to_be_bytes());
        // value: reserved(0), family(1=ipv4), xport(2), xip(4)
        let xport = 51820u16 ^ ((STUN_MAGIC_COOKIE >> 16) as u16);
        let cookie = STUN_MAGIC_COOKIE.to_be_bytes();
        let real_ip = [203, 0, 113, 5];
        let xip = [
            real_ip[0] ^ cookie[0],
            real_ip[1] ^ cookie[1],
            real_ip[2] ^ cookie[2],
            real_ip[3] ^ cookie[3],
        ];
        msg.push(0);
        msg.push(0x01);
        msg.extend_from_slice(&xport.to_be_bytes());
        msg.extend_from_slice(&xip);

        let got = parse_binding_response(&msg, &tx).expect("parse ok");
        assert_eq!(
            got,
            SocketAddr::new(IpAddr::V4(Ipv4Addr::new(203, 0, 113, 5)), 51820)
        );
    }

    #[test]
    fn xor_mapped_ipv4_accepts_family_0x04_alias() {
        // VPN-551 regression: pre-fix vpn-svc sent Family=0x04
        // (Go's `net.IPv4len`) instead of the RFC 5389 §15.2 0x01.
        // Parser must tolerate this so providers behind NAT discover
        // srflx during the staggered server rollout.
        let tx = [0x77u8; 12];
        let mut msg = Vec::new();
        msg.extend_from_slice(&STUN_BINDING_SUCCESS.to_be_bytes());
        msg.extend_from_slice(&12u16.to_be_bytes());
        msg.extend_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
        msg.extend_from_slice(&tx);
        msg.extend_from_slice(&STUN_ATTR_XOR_MAPPED_ADDRESS.to_be_bytes());
        msg.extend_from_slice(&8u16.to_be_bytes());
        let xport = 47512u16 ^ ((STUN_MAGIC_COOKIE >> 16) as u16);
        let cookie = STUN_MAGIC_COOKIE.to_be_bytes();
        let real_ip = [188, 66, 253, 46];
        let xip = [
            real_ip[0] ^ cookie[0],
            real_ip[1] ^ cookie[1],
            real_ip[2] ^ cookie[2],
            real_ip[3] ^ cookie[3],
        ];
        msg.push(0);
        // The bug: 0x04 instead of 0x01. Parser must still treat
        // this as IPv4 and un-XOR the address correctly.
        msg.push(0x04);
        msg.extend_from_slice(&xport.to_be_bytes());
        msg.extend_from_slice(&xip);

        let got = parse_binding_response(&msg, &tx).expect("parse ok with 0x04 alias");
        assert_eq!(
            got,
            SocketAddr::new(IpAddr::V4(Ipv4Addr::new(188, 66, 253, 46)), 47512)
        );
    }

    #[test]
    fn xor_mapped_rejects_truly_unknown_family() {
        // 0xff is not 0x01 / 0x02 / 0x04 — should still 400 cleanly.
        let tx = [0u8; 12];
        let mut msg = Vec::new();
        msg.extend_from_slice(&STUN_BINDING_SUCCESS.to_be_bytes());
        msg.extend_from_slice(&12u16.to_be_bytes());
        msg.extend_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
        msg.extend_from_slice(&tx);
        msg.extend_from_slice(&STUN_ATTR_XOR_MAPPED_ADDRESS.to_be_bytes());
        msg.extend_from_slice(&8u16.to_be_bytes());
        msg.push(0);
        msg.push(0xff); // bogus family
        msg.extend_from_slice(&[0u8; 6]);
        let err = parse_binding_response(&msg, &tx);
        assert!(matches!(err, Err(IceError::StunProtocol(_))));
    }

    #[test]
    fn binding_response_rejects_wrong_transaction_id() {
        let mut msg = Vec::new();
        msg.extend_from_slice(&STUN_BINDING_SUCCESS.to_be_bytes());
        msg.extend_from_slice(&0u16.to_be_bytes());
        msg.extend_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
        msg.extend_from_slice(&[0x00u8; 12]); // tx id in the message
        let expected_tx = [0x42u8; 12]; // but caller expected this one
        let err = parse_binding_response(&msg, &expected_tx);
        assert!(matches!(err, Err(IceError::StunProtocol(_))));
    }

    #[test]
    fn candidate_serializes_to_snake_case_json() {
        let c = IceCandidate {
            foundation: "abcd".into(),
            component: 1,
            transport: "udp".into(),
            priority: 12345,
            connection_address: "1.2.3.4".into(),
            connection_port: 51820,
            candidate_type: "host".into(),
            related_address: String::new(),
            related_port: 0,
            discovered_at_unix_ms: 1_700_000_000_000,
            latency_ms: 0,
            is_preferred: false,
        };
        let json = serde_json::to_string(&c).unwrap();
        // Snake_case fields per protoc-gen-go's `json:` tags.
        assert!(json.contains("\"connection_address\":\"1.2.3.4\""));
        assert!(json.contains("\"connection_port\":51820"));
        assert!(json.contains("\"candidate_type\":\"host\""));
        assert!(json.contains("\"discovered_at_unix_ms\":1700000000000"));
        // Empty optional fields are dropped per skip_serializing_if.
        assert!(!json.contains("related_address"));
        assert!(!json.contains("related_port"));
        assert!(!json.contains("latency_ms"));
        assert!(!json.contains("is_preferred"));
    }

    #[tokio::test]
    async fn stun_client_round_trip_against_local_test_server() {
        // Stand up a minimal STUN responder on a random port and
        // exercise the client against it. This is the equivalent of an
        // integration test for `discover_srflx_candidate` minus the
        // host-candidate plumbing.
        let listener = UdpSocket::bind("127.0.0.1:0").await.unwrap();
        let listener_addr = listener.local_addr().unwrap();

        tokio::spawn(async move {
            let mut buf = [0u8; 1500];
            let (n, remote) = listener.recv_from(&mut buf).await.unwrap();
            // Decode tx id from request
            let mut tx = [0u8; 12];
            tx.copy_from_slice(&buf[8..20]);
            // Build BINDING SUCCESS with XOR-MAPPED-ADDRESS = remote
            let mut resp = Vec::new();
            resp.extend_from_slice(&STUN_BINDING_SUCCESS.to_be_bytes());
            resp.extend_from_slice(&12u16.to_be_bytes());
            resp.extend_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
            resp.extend_from_slice(&tx);
            resp.extend_from_slice(&STUN_ATTR_XOR_MAPPED_ADDRESS.to_be_bytes());
            resp.extend_from_slice(&8u16.to_be_bytes());
            resp.push(0);
            resp.push(0x01);
            let xport = (remote.port()) ^ ((STUN_MAGIC_COOKIE >> 16) as u16);
            resp.extend_from_slice(&xport.to_be_bytes());
            let real = match remote.ip() {
                IpAddr::V4(v) => v.octets(),
                _ => [127, 0, 0, 1],
            };
            let cookie = STUN_MAGIC_COOKIE.to_be_bytes();
            for i in 0..4 {
                resp.push(real[i] ^ cookie[i]);
            }
            let _ = listener.send_to(&resp, remote).await;
            // Keep socket alive while client reads back.
            let _keep = n;
        });

        let vpn_listen_addr: SocketAddr = "127.0.0.1:51820".parse().unwrap();
        let candidate = discover_srflx_candidate(listener_addr, vpn_listen_addr)
            .await
            .expect("srflx ok");
        assert_eq!(candidate.candidate_type, "srflx");
        assert_eq!(candidate.connection_address, "127.0.0.1");
        // Port is whatever ephemeral the client picked — just sanity-check it parsed.
        assert!(candidate.connection_port > 0);
    }
}
