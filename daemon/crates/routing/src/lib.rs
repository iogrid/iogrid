//! Routing — WireGuard tunnel + SOCKS5 acceptor for the bandwidth workload.
//!
//! Production implementation will use:
//!  * `boringtun` (Cloudflare's user-space WireGuard) for the WG dataplane
//!  * `socks5-server` for the SOCKS5 acceptor
//!
//! Both are gated behind the `routing-real` Cargo feature so the workspace
//! `cargo check` runs in seconds in CI and on contributor laptops. Trait
//! shapes here pin the public API the supervisor will consume.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::net::SocketAddr;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use thiserror::Error;

/// All routing errors.
#[derive(Debug, Error)]
pub enum RoutingError {
    /// Local listener could not bind.
    #[error("listener bind failed on {addr}: {source}")]
    BindFailed {
        /// Address we tried to bind.
        addr: SocketAddr,
        /// Underlying I/O error.
        #[source]
        source: std::io::Error,
    },
    /// WireGuard handshake failed.
    #[error("WireGuard handshake failed: {0}")]
    HandshakeFailed(String),
    /// Peer disconnected.
    #[error("peer disconnected")]
    PeerGone,
}

/// Per-peer WireGuard configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WireGuardPeer {
    /// Base64-encoded peer public key.
    pub public_key: String,
    /// `host:port` of the peer endpoint, if known (else `None` for roaming).
    pub endpoint: Option<SocketAddr>,
    /// CIDR ranges we accept from / route to this peer.
    pub allowed_ips: Vec<String>,
    /// Keepalive interval in seconds (0 disables).
    pub persistent_keepalive: u16,
}

/// WireGuard tunnel driver.
#[async_trait]
pub trait Tunnel: Send + Sync {
    /// Start the tunnel. Returns once interface is up.
    async fn start(&self) -> Result<(), RoutingError>;

    /// Stop the tunnel and release the interface.
    async fn stop(&self) -> Result<(), RoutingError>;

    /// Add or update a peer.
    async fn upsert_peer(&self, peer: WireGuardPeer) -> Result<(), RoutingError>;
}

/// SOCKS5 acceptor running on the daemon side.
#[async_trait]
pub trait SocksAcceptor: Send + Sync {
    /// Bind and accept SOCKS5 connections on `addr`. Loops until cancelled.
    async fn serve(&self, addr: SocketAddr) -> Result<(), RoutingError>;
}

/// No-op tunnel — for tests + scaffold compilation without boringtun.
#[derive(Debug, Default, Clone)]
pub struct NoopTunnel;

#[async_trait]
impl Tunnel for NoopTunnel {
    async fn start(&self) -> Result<(), RoutingError> {
        tracing::debug!("noop tunnel start (scaffold)");
        Ok(())
    }
    async fn stop(&self) -> Result<(), RoutingError> {
        Ok(())
    }
    async fn upsert_peer(&self, _peer: WireGuardPeer) -> Result<(), RoutingError> {
        Ok(())
    }
}

/// No-op SOCKS5 acceptor — for tests + scaffold compilation without socks5-server.
#[derive(Debug, Default, Clone)]
pub struct NoopSocksAcceptor;

#[async_trait]
impl SocksAcceptor for NoopSocksAcceptor {
    async fn serve(&self, addr: SocketAddr) -> Result<(), RoutingError> {
        tracing::debug!(%addr, "noop socks acceptor scaffold");
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn noop_tunnel_round_trip() {
        let t = NoopTunnel;
        t.start().await.unwrap();
        t.upsert_peer(WireGuardPeer {
            public_key: "AAAA".into(),
            endpoint: None,
            allowed_ips: vec!["0.0.0.0/0".into()],
            persistent_keepalive: 25,
        })
        .await
        .unwrap();
        t.stop().await.unwrap();
    }

    #[tokio::test]
    async fn noop_socks_serves() {
        let a = NoopSocksAcceptor;
        a.serve("127.0.0.1:0".parse().unwrap()).await.unwrap();
    }
}
