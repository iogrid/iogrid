//! Inner-packet sink — the seam between the boringtun WG layer and
//! whatever consumes decapsulated inner packets (a userspace TCP stack,
//! a TUN device, a packet-capture buffer for tests, …).
//!
//! VPN-529 ships the WG layer; the inner-packet route (SOCKS5 on the
//! tunnel interface + SNAT to the provider's home IP) is the follow-up
//! PR-B. This trait is the contract those two pieces meet at.

use std::net::{IpAddr, SocketAddr};

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

/// Sink for decapsulated inner packets. Implementations must be
/// `Send + Sync` because the BoringTun UDP pump calls them from the
/// packet-processing task without owning the sink exclusively.
pub trait InnerPacketSink: Send + Sync {
    /// Deliver one decapsulated inner packet. The sink may queue,
    /// drop, or hand off to a userspace stack. Must not block the
    /// caller for long — the UDP pump is on the hot path.
    fn deliver(&self, packet: InnerPacket<'_>);
}

/// Default sink — logs the packet's family + destination + first 16
/// bytes at DEBUG. Used by tests and by the daemon's `routing-real`
/// build until the PR-B inner stack lands.
#[derive(Debug, Default, Clone)]
pub struct LoggingSink;

impl InnerPacketSink for LoggingSink {
    fn deliver(&self, packet: InnerPacket<'_>) {
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
impl InnerPacketSink for RecordingSink {
    fn deliver(&self, packet: InnerPacket<'_>) {
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
