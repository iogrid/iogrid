//! VPN UDP listener for accepting WireGuard packets from customers.
//! This is the provider-side counterpart to the customer's WireGuard tunnel.

use std::net::SocketAddr;
use tokio::net::UdpSocket;
use crate::RoutingError;

/// VPN listener accepts WireGuard packets from customers on a UDP socket.
pub struct VpnListener {
    socket: UdpSocket,
}

impl VpnListener {
    /// Create a new VPN listener on the specified address.
    pub async fn new(addr: SocketAddr) -> Result<Self, RoutingError> {
        let socket = UdpSocket::bind(addr)
            .await
            .map_err(|source| RoutingError::BindFailed { addr, source })?;

        tracing::info!("vpn listener started on {}", addr);
        Ok(Self { socket })
    }

    /// Start accepting WireGuard packets from customers.
    /// This loops indefinitely, processing incoming packets.
    pub async fn serve(&self) -> Result<(), RoutingError> {
        let mut buf = vec![0u8; 4096];
        loop {
            match self.socket.recv_from(&mut buf).await {
                Ok((n, src_addr)) => {
                    // Packet received from customer
                    // In production: decrypt via WireGuard, route to internet, send response
                    // For MVP: just log
                    tracing::debug!("received {} bytes from {}", n, src_addr);

                    // WireGuard packet types:
                    // - 1: handshake init
                    // - 2: handshake response
                    // - 3: transport data
                    // - 4: keepalive
                    if n >= 4 {
                        let pkt_type = u32::from_le_bytes([buf[0], buf[1], buf[2], buf[3]]);
                        match pkt_type {
                            0x01000000 => tracing::debug!("handshake init from {}", src_addr),
                            0x02000000 => tracing::debug!("handshake response from {}", src_addr),
                            0x03000000 => tracing::debug!("transport data from {}, {} bytes", src_addr, n),
                            0x04000000 => tracing::debug!("keepalive from {}", src_addr),
                            _ => tracing::debug!("unknown packet type from {}", src_addr),
                        }
                    }
                }
                Err(e) => {
                    tracing::error!("vpn listener recv error: {}", e);
                    return Err(RoutingError::PeerGone);
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_vpn_listener_bind() {
        let addr: SocketAddr = "127.0.0.1:0".parse().unwrap();
        let listener = VpnListener::new(addr).await;
        assert!(listener.is_ok());
    }
}
