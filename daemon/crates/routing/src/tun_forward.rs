//! TUN-forwarding inner-packet sink — provider-side data plane that
//! turns decapsulated WG packets into real outbound traffic and routes
//! the replies back through the tunnel. Closes the loop for issue
//! #529 path c — the `curl ifconfig.me` end-state demo.
//!
//! ## What it does
//!
//! 1. Opens `/dev/net/tun` and creates a Linux TUN device (default name
//!    `iogrid-tun0`) with IFF_NO_PI so the bytes on the fd are raw IP
//!    packets, no protocol-info prefix.
//! 2. Assigns the provider's inner-tunnel IP (`10.66.0.1/16`) via
//!    `ip addr add`, brings the link up via `ip link set up`. Static
//!    `/16` because the customer SDK uses `10.66.0.2/16`; multi-customer
//!    IP allocation is a coordinator follow-up.
//! 3. Enables `net.ipv4.ip_forward=1` so the kernel actually routes
//!    between the TUN and the WAN interface.
//! 4. Installs `iptables -t nat -A POSTROUTING -o <wan> -j MASQUERADE`
//!    so the kernel rewrites the customer's inner src IP (`10.66.0.2`)
//!    to the daemon's WAN IP on the way out. Without this, return
//!    packets from `ifconfig.me` would route to `10.66.0.2` which is
//!    unreachable from the public internet.
//! 5. On `InnerPacketSink::deliver` — writes the decapsulated packet
//!    bytes straight to the TUN fd. The kernel sees a packet arriving
//!    on `iogrid-tun0`, looks up its routing table, NATs, forwards out
//!    the WAN interface. Records the customer's WG pubkey + inner src
//!    IP so the read loop knows where to send replies.
//! 6. A background tokio task does the reverse direction: reads from
//!    the TUN fd whenever the kernel has a packet for `10.66.0.2`,
//!    looks up which WG peer owns that inner IP, calls the
//!    `OutboundEncapsulator` to ship the bytes back through the tunnel.
//!
//! ## Lifecycle
//!
//! Two-phase setup to break the chicken-and-egg between BoringTun and
//! the sink (BoringTun owns the encapsulator; the sink needs the
//! encapsulator to ship replies):
//!
//!   1. `TunForwardSink::new(...)` — opens TUN, configures IP + NAT,
//!      returns an `Arc<Self>` ready to receive `deliver` calls.
//!   2. `Arc::clone(&sink).attach_encapsulator(enc)` — spawns the
//!      TUN read loop bound to `enc`. Call this AFTER constructing the
//!      BoringTun (the encapsulator).
//!
//! Drop semantics: dropping the Arc closes the TUN fd, which makes the
//! read loop's next syscall return EOF and exit. The iptables MASQUERADE
//! rule is intentionally left in place — restart cycles don't re-add
//! identical rules (the install step is `iptables -C` first, then `-A`).
//!
//! ## Cross-platform
//!
//! Linux-only — `/dev/net/tun` is Linux. macOS uses `utun` (different
//! ioctls, different IFF flags) and Windows uses Wintun (entirely
//! different ABI). The module gates the entire implementation behind
//! `#[cfg(target_os = "linux")]` with a stub `cfg(not)` shim that
//! returns "not supported" so the daemon still compiles on macOS dev
//! boxes — the supervisor falls back to `LoggingSink`.

#![allow(missing_docs)]
// /dev/net/tun handling needs raw libc syscalls — open + ioctl + read +
// write on the bare fd. Allowed only inside this module; the rest of
// the routing crate stays unsafe-free per the crate-level `deny`.
#![allow(unsafe_code)]

use std::io;
use std::sync::Arc;

use async_trait::async_trait;

use crate::inner_sink::{InnerFamily, InnerPacket, InnerPacketSink, OutboundEncapsulator};

/// Default TUN interface name. Operators can override via
/// `VPN_TUN_IFNAME` env var if they have a conflict.
pub const DEFAULT_TUN_IFNAME: &str = "iogrid-tun0";

/// Default provider inner-tunnel CIDR. Matches `CustomerInnerCIDR` in
/// the Go SDK (`sdks/go/vpn/tunnel_route_linux.go`).
pub const PROVIDER_INNER_CIDR: &str = "10.66.0.1/16";

/// Errors emitted during TUN setup. All variants surface the operator-
/// observable cause; setup failure is fatal because falling back to
/// `LoggingSink` would silently break the demo.
#[derive(Debug, thiserror::Error)]
pub enum TunSetupError {
    #[error("opening /dev/net/tun: {0}")]
    OpenDev(#[source] io::Error),
    #[error("TUNSETIFF ioctl on {0}: {1}")]
    SetIff(String, #[source] io::Error),
    #[error("configuring interface via `ip`: {0}")]
    IpConfigure(String),
    #[error("enabling ip_forward: {0}")]
    IpForward(#[source] io::Error),
    #[error("installing iptables MASQUERADE rule: {0}")]
    Masquerade(String),
    #[error("TUN forward sink is Linux-only; got target_os={0}")]
    UnsupportedPlatform(&'static str),
}

#[cfg(target_os = "linux")]
mod imp {
    use super::*;

    use std::collections::HashMap;
    use std::net::Ipv4Addr;
    use std::os::fd::{AsRawFd, FromRawFd, OwnedFd, RawFd};
    use std::process::Command;

    use parking_lot::RwLock;
    use tokio::io::unix::AsyncFd;
    use tokio::io::Interest;

    // Linux TUN ioctl + flag values from <linux/if.h> + <linux/if_tun.h>.
    // Hard-coded rather than pulled via a bindgen build dep — these
    // values have been stable for 20+ years and the daemon ships a
    // static musl binary where bindgen would be a build wart.
    const IFNAMSIZ: usize = 16;
    const IFF_TUN: libc::c_short = 0x0001;
    const IFF_NO_PI: libc::c_short = 0x1000;
    // _IOW('T', 202, int) — TUNSETIFF, from <linux/if_tun.h>.
    const TUNSETIFF: libc::c_ulong = 0x4004_54ca;

    #[repr(C)]
    struct IfReq {
        ifr_name: [u8; IFNAMSIZ],
        ifr_flags: libc::c_short,
        // Padded to match the full union size of struct ifreq so the
        // ioctl call sees enough space — pre-zeroed, kernel reads only
        // the prefix it cares about for TUNSETIFF.
        _pad: [u8; 22],
    }

    /// Linux TUN device handle. Holds the raw fd both as a writable
    /// `RawFd` (for `deliver`) and wrapped in an `AsyncFd` (for the
    /// read loop). Two ways to express the same fd because the
    /// async-read API needs ownership but `deliver` needs a fast
    /// non-async write path — we never close the fd from either side
    /// while the sink is alive.
    pub struct TunDevice {
        async_fd: AsyncFd<OwnedFd>,
    }

    impl TunDevice {
        fn raw_fd(&self) -> RawFd {
            self.async_fd.get_ref().as_raw_fd()
        }
    }

    /// Linux impl of [`super::TunForwardSink`]. See module docs.
    pub struct TunForwardSinkImpl {
        tun: Arc<TunDevice>,
        /// Inner-IP → WG-pubkey routing table (#695). Populated on the
        /// `deliver` path from each customer's inner *source* IP; the read
        /// loop looks up each return packet's inner *destination* IP to
        /// ship it to the right customer. Replaces the old single-customer
        /// `bound_peer`, so N customers on one provider no longer
        /// cross-route. Self-populating: the first packet from a customer
        /// registers its inner IP.
        peer_map: RwLock<HashMap<Ipv4Addr, String>>,
        ifname: String,
    }

    impl TunForwardSinkImpl {
        pub fn new(
            ifname: &str,
            inner_cidr: &str,
            wan_iface: &str,
        ) -> Result<Arc<Self>, TunSetupError> {
            let fd = open_tun_fd(ifname)?;
            configure_interface(ifname, inner_cidr)?;
            enable_ip_forward()?;
            install_masquerade(wan_iface)?;
            install_forward(ifname, wan_iface)?;

            // Wrap the raw fd as an OwnedFd then as an AsyncFd for tokio.
            // Set non-blocking so the AsyncFd readable() / try_io flow
            // doesn't accidentally block the worker thread on slow reads.
            unsafe {
                let flags = libc::fcntl(fd, libc::F_GETFL, 0);
                if flags >= 0 {
                    let _ = libc::fcntl(fd, libc::F_SETFL, flags | libc::O_NONBLOCK);
                }
            }
            let owned = unsafe { OwnedFd::from_raw_fd(fd) };
            let async_fd = AsyncFd::with_interest(owned, Interest::READABLE)
                .map_err(|e| TunSetupError::SetIff(ifname.to_string(), e))?;
            let tun = Arc::new(TunDevice { async_fd });

            tracing::info!(
                ifname,
                inner_cidr,
                wan_iface,
                "TUN forward sink ready (decap → kernel → MASQUERADE → WAN)"
            );

            Ok(Arc::new(Self {
                tun,
                peer_map: RwLock::new(HashMap::new()),
                ifname: ifname.to_string(),
            }))
        }

        pub fn attach_encapsulator(self: &Arc<Self>, enc: Arc<dyn OutboundEncapsulator>) {
            let me = Arc::clone(self);
            tokio::spawn(async move {
                me.read_loop(enc).await;
            });
        }

        async fn read_loop(self: Arc<Self>, enc: Arc<dyn OutboundEncapsulator>) {
            tracing::info!(ifname = %self.ifname, "TUN read loop entering recv loop");
            loop {
                let mut guard = match self.tun.async_fd.readable().await {
                    Ok(g) => g,
                    Err(e) => {
                        tracing::error!(error = %e, "TUN AsyncFd readable() failed; exiting read loop");
                        return;
                    }
                };
                let mut buf = [0u8; 2048];
                let fd = self.tun.raw_fd();
                let res = guard.try_io(|_| {
                    let n =
                        unsafe { libc::read(fd, buf.as_mut_ptr() as *mut libc::c_void, buf.len()) };
                    if n < 0 {
                        Err(io::Error::last_os_error())
                    } else {
                        Ok(n as usize)
                    }
                });
                let nbytes = match res {
                    Ok(Ok(n)) if n > 0 => n,
                    Ok(Ok(_)) => continue, // EOF-ish; loop and re-await
                    Ok(Err(e)) => {
                        if e.kind() == io::ErrorKind::WouldBlock {
                            continue;
                        }
                        tracing::warn!(error = %e, "TUN read failed");
                        continue;
                    }
                    Err(_would_block) => continue,
                };

                // Route by the return packet's inner destination IP (#695):
                // look up which customer owns that inner IP. Drop packets
                // for an unknown inner IP (no customer has sent from it yet,
                // or it's non-IPv4).
                let peer = ipv4_dst(&buf[..nbytes])
                    .and_then(|dst| self.peer_map.read().get(&dst).cloned());
                let Some(pubkey) = peer else {
                    continue;
                };
                if let Err(e) = enc.encapsulate_for_peer(&pubkey, &buf[..nbytes]).await {
                    tracing::warn!(error = %e, peer = %pubkey, "TUN→WG encapsulate failed");
                }
            }
        }
    }

    #[async_trait]
    impl InnerPacketSink for TunForwardSinkImpl {
        async fn deliver(
            &self,
            packet: InnerPacket<'_>,
            _enc: Option<&(dyn OutboundEncapsulator + 'static)>,
        ) {
            // IPv6 inner traffic is not in scope for the v1 demo; the
            // kernel would forward it correctly but MASQUERADE only
            // covers IPv4 NAT and most residential providers don't
            // have public-IPv6 routing into the home anyway. Drop.
            if packet.family != InnerFamily::V4 {
                return;
            }
            // Record inner-source-IP → pubkey so the read loop can route
            // return packets to the right customer (#695). Self-populating:
            // the first packet from each customer registers its inner IP.
            if let Some(src) = ipv4_src(packet.payload) {
                let mut map = self.peer_map.write();
                if map.get(&src).map(String::as_str) != Some(packet.peer_public_key_b64.as_str()) {
                    map.insert(src, packet.peer_public_key_b64.clone());
                    tracing::info!(
                        peer = %packet.peer_public_key_b64,
                        inner_src = %src,
                        "TUN sink routing-table entry added (#695)"
                    );
                }
            }
            let fd = self.tun.raw_fd();
            // Blocking write on a non-blocking fd — fine for inner
            // packets because the TUN device has a kernel-side tx
            // queue and write() returns immediately unless that queue
            // is full (rare for VPN traffic shapes).
            let n = unsafe {
                libc::write(
                    fd,
                    packet.payload.as_ptr() as *const libc::c_void,
                    packet.payload.len(),
                )
            };
            if n < 0 {
                let e = io::Error::last_os_error();
                if e.kind() != io::ErrorKind::WouldBlock {
                    tracing::warn!(error = %e, "TUN write failed (decap → kernel path)");
                }
            }
        }
    }

    fn open_tun_fd(ifname: &str) -> Result<RawFd, TunSetupError> {
        if ifname.len() >= IFNAMSIZ {
            return Err(TunSetupError::SetIff(
                ifname.to_string(),
                io::Error::new(io::ErrorKind::InvalidInput, "ifname too long"),
            ));
        }
        let fd = unsafe { libc::open(c"/dev/net/tun".as_ptr(), libc::O_RDWR) };
        if fd < 0 {
            return Err(TunSetupError::OpenDev(io::Error::last_os_error()));
        }
        let mut req: IfReq = unsafe { std::mem::zeroed() };
        let name_bytes = ifname.as_bytes();
        req.ifr_name[..name_bytes.len()].copy_from_slice(name_bytes);
        req.ifr_flags = IFF_TUN | IFF_NO_PI;
        let r = unsafe { libc::ioctl(fd, TUNSETIFF, &mut req) };
        if r < 0 {
            let e = io::Error::last_os_error();
            unsafe {
                libc::close(fd);
            }
            return Err(TunSetupError::SetIff(ifname.to_string(), e));
        }
        Ok(fd)
    }

    fn configure_interface(ifname: &str, inner_cidr: &str) -> Result<(), TunSetupError> {
        // `ip addr add` is idempotent-ish: returns exit 2 + EEXIST on
        // re-add. We treat any "exists" outcome as success so the
        // daemon restart path doesn't have to track prior state.
        let addr_status = Command::new("ip")
            .args(["addr", "add", inner_cidr, "dev", ifname])
            .status();
        match addr_status {
            Ok(s) if s.success() => {}
            Ok(s) => {
                // Exit code 2 from `ip` is "file exists" — fine on restart.
                if s.code() != Some(2) {
                    return Err(TunSetupError::IpConfigure(format!(
                        "ip addr add {} dev {} → exit {:?}",
                        inner_cidr,
                        ifname,
                        s.code()
                    )));
                }
            }
            Err(e) => {
                return Err(TunSetupError::IpConfigure(format!(
                    "spawning `ip addr add`: {e}"
                )));
            }
        }
        let up_status = Command::new("ip")
            .args(["link", "set", ifname, "up"])
            .status();
        match up_status {
            Ok(s) if s.success() => Ok(()),
            Ok(s) => Err(TunSetupError::IpConfigure(format!(
                "ip link set {} up → exit {:?}",
                ifname,
                s.code()
            ))),
            Err(e) => Err(TunSetupError::IpConfigure(format!(
                "spawning `ip link set ... up`: {e}"
            ))),
        }
    }

    fn enable_ip_forward() -> Result<(), TunSetupError> {
        std::fs::write("/proc/sys/net/ipv4/ip_forward", "1\n").map_err(TunSetupError::IpForward)
    }

    fn install_masquerade(wan_iface: &str) -> Result<(), TunSetupError> {
        // -C checks if the rule already exists; -A appends if not.
        // Two invocations together = idempotent installation; the
        // restart path doesn't pile up duplicate rules.
        let check = Command::new("iptables")
            .args([
                "-t",
                "nat",
                "-C",
                "POSTROUTING",
                "-o",
                wan_iface,
                "-j",
                "MASQUERADE",
            ])
            .status();
        match check {
            Ok(s) if s.success() => {
                tracing::debug!(wan_iface, "MASQUERADE already installed");
                return Ok(());
            }
            Ok(_) => {} // rule not present; install
            Err(e) => {
                return Err(TunSetupError::Masquerade(format!(
                    "spawning `iptables -C`: {e}"
                )));
            }
        }
        let add = Command::new("iptables")
            .args([
                "-t",
                "nat",
                "-A",
                "POSTROUTING",
                "-o",
                wan_iface,
                "-j",
                "MASQUERADE",
            ])
            .status();
        match add {
            Ok(s) if s.success() => {
                tracing::info!(
                    wan_iface,
                    "installed iptables MASQUERADE rule on POSTROUTING"
                );
                Ok(())
            }
            Ok(s) => Err(TunSetupError::Masquerade(format!(
                "iptables -A POSTROUTING → exit {:?}",
                s.code()
            ))),
            Err(e) => Err(TunSetupError::Masquerade(format!(
                "spawning `iptables -A`: {e}"
            ))),
        }
    }

    /// Install FORWARD ACCEPT rules so decapsulated tunnel traffic is
    /// actually routed tun↔WAN. MASQUERADE alone is NOT enough on hosts
    /// whose FORWARD chain defaults to DROP (Docker, k8s/CNI, hardened
    /// hosts): the WG handshake succeeds but no customer byte egresses
    /// (#699 — "handshake OK, zero egress", proven live). Idempotent
    /// (`-C` then `-I`), so restarts don't pile up duplicates.
    fn install_forward(tun_iface: &str, wan_iface: &str) -> Result<(), TunSetupError> {
        // (a) tun → WAN (customer egress); (b) WAN → tun for the return
        // traffic of established/related conntrack flows.
        let rules: [&[&str]; 2] = [
            &["-i", tun_iface, "-o", wan_iface, "-j", "ACCEPT"],
            &[
                "-i",
                wan_iface,
                "-o",
                tun_iface,
                "-m",
                "state",
                "--state",
                "RELATED,ESTABLISHED",
                "-j",
                "ACCEPT",
            ],
        ];
        for rule in rules {
            let mut check = vec!["-C", "FORWARD"];
            check.extend_from_slice(rule);
            let exists = Command::new("iptables")
                .args(&check)
                .status()
                .map(|s| s.success())
                .unwrap_or(false);
            if exists {
                continue;
            }
            let mut add = vec!["-I", "FORWARD", "1"];
            add.extend_from_slice(rule);
            let st = Command::new("iptables").args(&add).status().map_err(|e| {
                TunSetupError::Masquerade(format!("spawning `iptables -I FORWARD`: {e}"))
            })?;
            if !st.success() {
                return Err(TunSetupError::Masquerade(format!(
                    "iptables -I FORWARD → exit {:?}",
                    st.code()
                )));
            }
        }
        tracing::info!(
            tun_iface,
            wan_iface,
            "installed FORWARD ACCEPT rules (tun↔WAN) — egress works on FORWARD-DROP hosts (#699)"
        );
        Ok(())
    }

    /// Parse the IPv4 **source** address from a raw inner packet (the
    /// customer's inner-tunnel IP). `None` for non-IPv4 or short buffers.
    fn ipv4_src(buf: &[u8]) -> Option<Ipv4Addr> {
        if buf.len() >= 20 && buf[0] >> 4 == 4 {
            Some(Ipv4Addr::new(buf[12], buf[13], buf[14], buf[15]))
        } else {
            None
        }
    }

    /// Parse the IPv4 **destination** address from a raw return packet (the
    /// inner IP the reply is bound for). `None` for non-IPv4 or short.
    fn ipv4_dst(buf: &[u8]) -> Option<Ipv4Addr> {
        if buf.len() >= 20 && buf[0] >> 4 == 4 {
            Some(Ipv4Addr::new(buf[16], buf[17], buf[18], buf[19]))
        } else {
            None
        }
    }

    #[cfg(test)]
    mod parse_tests {
        use super::{ipv4_dst, ipv4_src};
        use std::net::Ipv4Addr;

        // Minimal 20-byte IPv4 header: version/IHL=0x45, src@12, dst@16.
        fn pkt(src: [u8; 4], dst: [u8; 4]) -> Vec<u8> {
            let mut b = vec![0u8; 20];
            b[0] = 0x45;
            b[12..16].copy_from_slice(&src);
            b[16..20].copy_from_slice(&dst);
            b
        }

        #[test]
        fn parses_src_and_dst() {
            let b = pkt([10, 66, 81, 2], [1, 1, 1, 1]);
            assert_eq!(ipv4_src(&b), Some(Ipv4Addr::new(10, 66, 81, 2)));
            assert_eq!(ipv4_dst(&b), Some(Ipv4Addr::new(1, 1, 1, 1)));
        }

        #[test]
        fn rejects_non_ipv4_and_short() {
            let mut b = pkt([10, 66, 0, 2], [8, 8, 8, 8]);
            b[0] = 0x60; // IPv6 version nibble
            assert_eq!(ipv4_src(&b), None);
            assert_eq!(ipv4_dst(&b), None);
            assert_eq!(ipv4_src(&[0x45, 0, 0]), None); // too short
        }

        #[test]
        fn multi_customer_distinct_dst() {
            // The #695 win: two return packets resolve to two different
            // inner IPs → two different customers (no cross-routing).
            let a = pkt([1, 1, 1, 1], [10, 66, 0, 5]);
            let c = pkt([1, 1, 1, 1], [10, 66, 0, 9]);
            assert_eq!(ipv4_dst(&a), Some(Ipv4Addr::new(10, 66, 0, 5)));
            assert_eq!(ipv4_dst(&c), Some(Ipv4Addr::new(10, 66, 0, 9)));
            assert_ne!(ipv4_dst(&a), ipv4_dst(&c));
        }
    }
}

#[cfg(target_os = "linux")]
pub use imp::TunForwardSinkImpl as TunForwardSink;

#[cfg(not(target_os = "linux"))]
mod imp {
    use super::*;

    /// Non-Linux stub. Construction always fails so the supervisor can
    /// log + fall back to LoggingSink without polluting the call site
    /// with platform conditionals.
    pub struct TunForwardSinkImpl;

    impl TunForwardSinkImpl {
        pub fn new(
            _ifname: &str,
            _inner_cidr: &str,
            _wan_iface: &str,
        ) -> Result<Arc<Self>, TunSetupError> {
            Err(TunSetupError::UnsupportedPlatform(std::env::consts::OS))
        }

        pub fn attach_encapsulator(self: &Arc<Self>, _enc: Arc<dyn OutboundEncapsulator>) {}
    }

    #[async_trait]
    impl InnerPacketSink for TunForwardSinkImpl {
        async fn deliver(
            &self,
            _packet: InnerPacket<'_>,
            _enc: Option<&(dyn OutboundEncapsulator + 'static)>,
        ) {
        }
    }
}

#[cfg(not(target_os = "linux"))]
pub use imp::TunForwardSinkImpl as TunForwardSink;
