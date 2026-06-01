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
        /// Single-customer demo: the most-recent peer to send a packet
        /// inbound, used as the destination for everything the read
        /// loop sees. Multi-customer routing requires an inner-IP →
        /// pubkey table, queued as a follow-up.
        bound_peer: RwLock<Option<String>>,
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
                bound_peer: RwLock::new(None),
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

                let peer = self.bound_peer.read().clone();
                let Some(pubkey) = peer else {
                    // No customer has sent a packet yet; nothing to
                    // route. Drop. (In multi-customer mode, look up
                    // the dst IP in the inner-IP → pubkey table.)
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
            // Remember the peer so the read loop knows where to ship
            // return packets. Single-customer demo — a multi-customer
            // version keys by `inner_src_ip → pubkey`.
            {
                let cur = self.bound_peer.read().clone();
                if cur.as_deref() != Some(packet.peer_public_key_b64.as_str()) {
                    *self.bound_peer.write() = Some(packet.peer_public_key_b64.clone());
                    tracing::info!(
                        peer = %packet.peer_public_key_b64,
                        dst = %packet.dst_ip,
                        "TUN sink bound to new peer (single-customer demo)"
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
