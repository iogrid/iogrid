//! Inner-packet sink — the seam between the boringtun WG layer and
//! whatever consumes decapsulated inner packets (a userspace TCP stack,
//! a TUN device, a packet-capture buffer for tests, …).
//!
//! VPN-529 PR-A shipped the WG layer. This PR-B path (a) introduces
//! the `OutboundEncapsulator` trait + makes [`InnerPacketSink::deliver`]
//! async so a sink can synthesise response packets and ship them back
//! through the WG tunnel. [`IcmpEchoSink`] is the proof-of-life impl
//! that responds to IPv4 ICMP echo-request packets — a customer can
//! `ping <daemon-tunnel-ip>` and get a reply, demonstrating the full
//! decap → process → encap → send-back round trip.
//!
//! The transparent NAT / SOCKS5 / per-flow TCP relay piece that the
//! `curl ifconfig.me` demo needs is its own follow-up issue (PR-B-2);
//! that impl plugs into the same seam this PR introduces.

use std::net::{IpAddr, SocketAddr};

use async_trait::async_trait;

/// Address family of a decapsulated inner packet.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum InnerFamily {
    /// IPv4 inner packet.
    V4,
    /// IPv6 inner packet.
    V6,
}

/// A single decapsulated inner packet plus the metadata the sink needs
/// to route it. `peer_endpoint` is the UDP source the WG packet arrived
/// from (used by NAT-roaming aware sinks); `peer_public_key_b64` is
/// the WG static public key the packet authenticated against (used by
/// per-customer billing + filter rules).
#[derive(Debug)]
pub struct InnerPacket<'a> {
    /// Address family.
    pub family: InnerFamily,
    /// The inner packet bytes (post-WG decrypt). Includes the IP header.
    pub payload: &'a [u8],
    /// Inner-packet destination IP, parsed from the IP header by boringtun.
    pub dst_ip: IpAddr,
    /// UDP source the outer WG packet came from. Useful for the sink
    /// to update its routing table on a roaming peer.
    pub peer_endpoint: SocketAddr,
    /// Base64-encoded WG public key of the originating peer.
    pub peer_public_key_b64: String,
}

/// What a sink can ask the WG layer to do when it wants to send a
/// packet back to the customer. Implemented by `BoringTun`; the trait
/// keeps the sink decoupled from the concrete WG impl so tests can
/// supply a recording double.
///
/// Used by [`IcmpEchoSink`] (PR-B path (a)) and by the upcoming
/// transparent-NAT inner-stack sink (PR-B-2).
#[async_trait]
pub trait OutboundEncapsulator: Send + Sync {
    /// Encapsulate `inner_bytes` (a fully-formed IPv4 or IPv6 inner
    /// packet) for the peer identified by `peer_public_key_b64`, and
    /// send the resulting WG UDP datagram back to the peer's last
    /// known endpoint. Errors are reflected in the return so the
    /// caller can log + meter; the WG protocol is loss-tolerant.
    async fn encapsulate_for_peer(
        &self,
        peer_public_key_b64: &str,
        inner_bytes: &[u8],
    ) -> Result<(), OutboundError>;
}

/// Errors emitted by [`OutboundEncapsulator::encapsulate_for_peer`].
#[derive(Debug, thiserror::Error)]
pub enum OutboundError {
    /// No peer registered with that pubkey.
    #[error("no peer registered for pubkey {0}")]
    UnknownPeer(String),
    /// Peer has no known endpoint (handshake hasn't happened yet
    /// for outbound-initiated paths).
    #[error("peer endpoint unknown for {0}")]
    NoEndpoint(String),
    /// boringtun returned a protocol-level error during encapsulate.
    #[error("encapsulate failed: {0}")]
    Encap(String),
    /// UDP send failed.
    #[error("udp send failed: {0}")]
    Send(#[source] std::io::Error),
}

/// Sink for decapsulated inner packets. Implementations must be
/// `Send + Sync` because the BoringTun UDP pump calls them from the
/// packet-processing task without owning the sink exclusively.
///
/// `deliver` is async so a sink can synthesise + ship response
/// packets back through the tunnel (see [`IcmpEchoSink`]).
#[async_trait]
pub trait InnerPacketSink: Send + Sync {
    /// Deliver one decapsulated inner packet. Sinks that just observe
    /// (log, count) should return promptly; sinks that synthesise
    /// responses use `encapsulator` to ship them.
    ///
    /// `encapsulator` is supplied by the WG layer rather than held
    /// inside the sink so the sink can be constructed cheaply without
    /// a circular `Arc`. `None` is passed by callers that don't
    /// support outbound (e.g. the existing LoggingSink integration
    /// tests).
    async fn deliver(
        &self,
        packet: InnerPacket<'_>,
        encapsulator: Option<&(dyn OutboundEncapsulator + 'static)>,
    );
}

/// Default sink — logs the packet's family + destination + first 16
/// bytes at DEBUG. Used by tests and by the daemon's `routing-real`
/// build until the transparent-NAT inner stack lands.
#[derive(Debug, Default, Clone)]
pub struct LoggingSink;

#[async_trait]
impl InnerPacketSink for LoggingSink {
    async fn deliver(
        &self,
        packet: InnerPacket<'_>,
        _encapsulator: Option<&(dyn OutboundEncapsulator + 'static)>,
    ) {
        let head: Vec<String> = packet
            .payload
            .iter()
            .take(16)
            .map(|b| format!("{b:02x}"))
            .collect();
        tracing::debug!(
            family = ?packet.family,
            dst = %packet.dst_ip,
            peer = %packet.peer_public_key_b64,
            endpoint = %packet.peer_endpoint,
            bytes = packet.payload.len(),
            head = %head.join(""),
            "inner packet decapsulated"
        );
    }
}

/// Test sink that records every delivered packet so the BoringTun
/// integration tests can assert ordering + counts without parsing
/// the wire format themselves. Used only behind `#[cfg(test)]`.
#[cfg(test)]
#[derive(Debug, Default)]
pub struct RecordingSink {
    inner: std::sync::Mutex<Vec<RecordedPacket>>,
}

/// One snapshot recorded by [`RecordingSink`].
#[cfg(test)]
#[derive(Debug, Clone)]
pub struct RecordedPacket {
    /// Family — copied from the InnerPacket.
    pub family: InnerFamily,
    /// Inner packet bytes — owned copy so the test can assert later.
    pub payload: Vec<u8>,
    /// Destination IP.
    pub dst_ip: IpAddr,
    /// Source UDP endpoint.
    pub peer_endpoint: SocketAddr,
    /// Peer public key (base64).
    pub peer_public_key_b64: String,
}

#[cfg(test)]
#[async_trait]
impl InnerPacketSink for RecordingSink {
    async fn deliver(
        &self,
        packet: InnerPacket<'_>,
        _encapsulator: Option<&(dyn OutboundEncapsulator + 'static)>,
    ) {
        self.inner.lock().unwrap().push(RecordedPacket {
            family: packet.family,
            payload: packet.payload.to_vec(),
            dst_ip: packet.dst_ip,
            peer_endpoint: packet.peer_endpoint,
            peer_public_key_b64: packet.peer_public_key_b64,
        });
    }
}

#[cfg(test)]
impl RecordingSink {
    /// Number of packets recorded so far.
    pub fn count(&self) -> usize {
        self.inner.lock().unwrap().len()
    }

    /// Snapshot the recorded packets. Returns a copy so callers don't
    /// hold the mutex past their assertion.
    pub fn snapshot(&self) -> Vec<RecordedPacket> {
        self.inner.lock().unwrap().clone()
    }
}

// =============================================================
// PR-B path (a): ICMP-echo sink — proof-of-life round-trip
// =============================================================

/// Sink that recognises IPv4 ICMP echo-request packets and replies in
/// kind. Demonstrates the full decap → process → encap → send-back
/// loop without yet pulling in a userspace TCP stack (which lives in
/// the follow-up PR-B-2).
///
/// A customer with an established WG tunnel pinging the daemon's
/// tunnel IP will see ICMP echo-reply responses — the cleanest
/// possible end-to-end "the data plane works" signal.
///
/// IPv6 ICMPv6 echo-request handling is left as a follow-up because
/// the checksum includes the IPv6 pseudo-header which adds bookkeeping
/// that's out of scope for the proof-of-life.
#[derive(Debug, Default, Clone)]
pub struct IcmpEchoSink;

#[async_trait]
impl InnerPacketSink for IcmpEchoSink {
    async fn deliver(
        &self,
        packet: InnerPacket<'_>,
        encapsulator: Option<&(dyn OutboundEncapsulator + 'static)>,
    ) {
        if packet.family != InnerFamily::V4 {
            return;
        }
        let enc = match encapsulator {
            Some(e) => e,
            None => return,
        };
        let reply = match build_icmpv4_echo_reply(packet.payload) {
            Some(r) => r,
            None => return,
        };
        let pubkey = packet.peer_public_key_b64.clone();
        if let Err(e) = enc.encapsulate_for_peer(&pubkey, &reply).await {
            tracing::warn!(error = %e, peer = %pubkey, "icmp echo reply send failed");
        } else {
            tracing::debug!(peer = %pubkey, "icmp echo reply sent");
        }
    }
}

/// Build an IPv4 ICMP echo-reply for the given echo-request payload.
/// Returns `None` if `payload` isn't a valid IPv4 ICMPv4 echo-request.
///
/// The reply: swaps src/dst IPs, flips ICMP type 8 → 0, leaves the
/// identifier + sequence number + data intact, recomputes both the
/// ICMP checksum and the IPv4 header checksum. TTL is set fresh to 64
/// (replies don't inherit the request's decremented TTL).
fn build_icmpv4_echo_reply(payload: &[u8]) -> Option<Vec<u8>> {
    if payload.len() < 20 {
        return None;
    }
    let version_ihl = payload[0];
    if version_ihl >> 4 != 4 {
        return None;
    }
    let ihl_words = (version_ihl & 0x0f) as usize;
    let ihl_bytes = ihl_words * 4;
    if ihl_bytes < 20 || payload.len() < ihl_bytes {
        return None;
    }
    if payload[9] != 1 {
        // protocol != ICMPv4
        return None;
    }
    let icmp = &payload[ihl_bytes..];
    if icmp.len() < 8 {
        return None;
    }
    if icmp[0] != 8 {
        // ICMP type != echo-request
        return None;
    }

    // Build the reply: copy of the request with the header tweaked.
    let mut reply = payload.to_vec();
    // Swap src + dst IPs (bytes 12..16 and 16..20).
    let mut tmp = [0u8; 4];
    tmp.copy_from_slice(&payload[12..16]);
    reply[12..16].copy_from_slice(&payload[16..20]);
    reply[16..20].copy_from_slice(&tmp);
    // Fresh TTL — replies shouldn't inherit the customer's
    // decremented TTL, which would otherwise drop on long return paths.
    reply[8] = 64;
    // Zero the IPv4 header checksum before recomputing.
    reply[10] = 0;
    reply[11] = 0;
    let ipv4_csum = ones_complement_sum_16(&reply[..ihl_bytes]);
    reply[10] = (ipv4_csum >> 8) as u8;
    reply[11] = (ipv4_csum & 0xff) as u8;
    // ICMP: flip type 8 → 0; zero the checksum field; recompute.
    reply[ihl_bytes] = 0;
    reply[ihl_bytes + 2] = 0;
    reply[ihl_bytes + 3] = 0;
    let icmp_csum = ones_complement_sum_16(&reply[ihl_bytes..]);
    reply[ihl_bytes + 2] = (icmp_csum >> 8) as u8;
    reply[ihl_bytes + 3] = (icmp_csum & 0xff) as u8;
    Some(reply)
}

/// Internet checksum (RFC 1071): one's complement sum over 16-bit
/// words. Handles odd-byte-length tails by padding with a zero.
fn ones_complement_sum_16(bytes: &[u8]) -> u16 {
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

#[cfg(test)]
mod icmp_tests {
    use super::*;
    use std::sync::Mutex;

    /// A minimal IPv4 + ICMPv4 echo-request: 10.0.0.2 → 10.0.0.1,
    /// id=0x4242, seq=1, payload "abc".
    fn build_echo_request() -> Vec<u8> {
        let payload_data = b"abc";
        let icmp_len = 8 + payload_data.len();
        let total_len = 20 + icmp_len;
        let mut p = Vec::with_capacity(total_len);
        p.push(0x45); // ver 4, IHL 5
        p.push(0x00); // dscp/ecn
        p.extend_from_slice(&(total_len as u16).to_be_bytes());
        p.extend_from_slice(&[0x00, 0x00]); // id
        p.extend_from_slice(&[0x40, 0x00]); // flags+frag
        p.push(64); // TTL
        p.push(1); // protocol ICMP
        p.extend_from_slice(&[0, 0]); // checksum placeholder
        p.extend_from_slice(&[10, 0, 0, 2]); // src
        p.extend_from_slice(&[10, 0, 0, 1]); // dst
        let csum = ones_complement_sum_16(&p[..20]);
        p[10] = (csum >> 8) as u8;
        p[11] = (csum & 0xff) as u8;
        // ICMPv4 echo request
        p.push(8); // type 8 = echo request
        p.push(0); // code
        p.extend_from_slice(&[0, 0]); // checksum placeholder
        p.extend_from_slice(&[0x42, 0x42]); // identifier
        p.extend_from_slice(&[0x00, 0x01]); // sequence
        p.extend_from_slice(payload_data);
        let icmp_csum = ones_complement_sum_16(&p[20..]);
        p[22] = (icmp_csum >> 8) as u8;
        p[23] = (icmp_csum & 0xff) as u8;
        p
    }

    #[test]
    fn build_reply_swaps_addresses_and_flips_type() {
        let req = build_echo_request();
        let reply = build_icmpv4_echo_reply(&req).expect("valid request");
        // Total length unchanged.
        assert_eq!(reply.len(), req.len());
        // Swapped src/dst.
        assert_eq!(&reply[12..16], &[10, 0, 0, 1]);
        assert_eq!(&reply[16..20], &[10, 0, 0, 2]);
        // Type flipped 8 → 0.
        assert_eq!(reply[20], 0);
        // Identifier + sequence + data preserved.
        assert_eq!(&reply[24..28], &[0x42, 0x42, 0x00, 0x01]);
        assert_eq!(&reply[28..31], b"abc");
        // Fresh TTL.
        assert_eq!(reply[8], 64);
    }

    #[test]
    fn reply_ipv4_checksum_validates() {
        let req = build_echo_request();
        let reply = build_icmpv4_echo_reply(&req).unwrap();
        // The IPv4 checksum field should be such that the sum over
        // the whole header (including the checksum) is 0xffff →
        // i.e. ones_complement_sum_16 returns 0 when checksum is in.
        assert_eq!(ones_complement_sum_16(&reply[..20]), 0);
    }

    #[test]
    fn reply_icmp_checksum_validates() {
        let req = build_echo_request();
        let reply = build_icmpv4_echo_reply(&req).unwrap();
        assert_eq!(ones_complement_sum_16(&reply[20..]), 0);
    }

    #[test]
    fn rejects_non_ipv4() {
        // First nibble != 4 → return None.
        let mut bogus = vec![0x65u8; 32];
        bogus[9] = 1;
        assert!(build_icmpv4_echo_reply(&bogus).is_none());
    }

    #[test]
    fn rejects_non_icmp_protocol() {
        let mut req = build_echo_request();
        req[9] = 6; // protocol TCP
        assert!(build_icmpv4_echo_reply(&req).is_none());
    }

    #[test]
    fn rejects_non_echo_request_type() {
        let mut req = build_echo_request();
        req[20] = 0; // already echo-reply, not request
        assert!(build_icmpv4_echo_reply(&req).is_none());
    }

    /// Recording outbound encapsulator for the sink integration test.
    #[derive(Default)]
    struct RecordingEncap {
        sent: Mutex<Vec<(String, Vec<u8>)>>,
    }
    #[async_trait]
    impl OutboundEncapsulator for RecordingEncap {
        async fn encapsulate_for_peer(
            &self,
            peer_public_key_b64: &str,
            inner_bytes: &[u8],
        ) -> Result<(), OutboundError> {
            self.sent
                .lock()
                .unwrap()
                .push((peer_public_key_b64.to_owned(), inner_bytes.to_vec()));
            Ok(())
        }
    }

    #[tokio::test]
    async fn sink_replies_to_echo_request_via_encapsulator() {
        let sink = IcmpEchoSink;
        let enc = RecordingEncap::default();
        let req = build_echo_request();
        let pkt = InnerPacket {
            family: InnerFamily::V4,
            payload: &req,
            dst_ip: "10.0.0.1".parse().unwrap(),
            peer_endpoint: "203.0.113.5:51820".parse().unwrap(),
            peer_public_key_b64: "peerKey==".into(),
        };
        sink.deliver(pkt, Some(&enc)).await;
        let sent = enc.sent.lock().unwrap();
        assert_eq!(sent.len(), 1, "exactly one outbound packet");
        assert_eq!(sent[0].0, "peerKey==");
        // The recorded outbound bytes should be a valid echo-reply.
        let reply = &sent[0].1;
        assert_eq!(reply[20], 0, "ICMP type = echo-reply");
        assert_eq!(&reply[12..16], &[10, 0, 0, 1]);
        assert_eq!(&reply[16..20], &[10, 0, 0, 2]);
    }

    #[tokio::test]
    async fn sink_ignores_non_v4_packets() {
        let sink = IcmpEchoSink;
        let enc = RecordingEncap::default();
        let req = build_echo_request(); // v4 bytes, but we lie about family
        let pkt = InnerPacket {
            family: InnerFamily::V6,
            payload: &req,
            dst_ip: "::1".parse().unwrap(),
            peer_endpoint: "203.0.113.5:51820".parse().unwrap(),
            peer_public_key_b64: "peerKey==".into(),
        };
        sink.deliver(pkt, Some(&enc)).await;
        assert_eq!(enc.sent.lock().unwrap().len(), 0);
    }

    #[tokio::test]
    async fn sink_no_op_when_encapsulator_absent() {
        // LoggingSink-equivalent posture: nothing to ship to.
        let sink = IcmpEchoSink;
        let req = build_echo_request();
        let pkt = InnerPacket {
            family: InnerFamily::V4,
            payload: &req,
            dst_ip: "10.0.0.1".parse().unwrap(),
            peer_endpoint: "203.0.113.5:51820".parse().unwrap(),
            peer_public_key_b64: "peerKey==".into(),
        };
        sink.deliver(pkt, None).await;
        // No panic, no obs — just confirms the absent-encapsulator
        // path is safe.
    }
}
