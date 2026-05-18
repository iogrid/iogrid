//! Transport layer — bidirectional gRPC stream to the iogrid coordinator.
//!
//! The real implementation will compile the protos under `proto/iogrid/**` via
//! `tonic-build` in a `build.rs` and expose typed stubs here. For the scaffold
//! we ship a transport-shaped struct that exposes the canonical surface
//! (connect / send / receive / close) on top of `tonic::transport::Channel`.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::path::PathBuf;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// All errors the transport layer surfaces upward.
#[derive(Debug, Error)]
pub enum TransportError {
    /// Coordinator URL failed to parse.
    #[error("invalid coordinator URL: {0}")]
    InvalidUrl(String),
    /// Identity bundle (cert + key) missing or unreadable.
    #[error("missing identity bundle at {path}: {source}")]
    MissingIdentity {
        /// Path that was attempted.
        path: PathBuf,
        /// Underlying I/O error.
        #[source]
        source: std::io::Error,
    },
    /// Connection to coordinator failed.
    #[error("coordinator unreachable: {0}")]
    Unreachable(String),
}

/// Configuration the supervisor hands to [`CoordinatorClient::connect`].
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConnectConfig {
    /// `https://coordinator.iogrid.org:443` style.
    pub coordinator_url: String,
    /// PEM bundle containing the provider's identity cert + key.
    pub identity_pem: PathBuf,
    /// Optional CA bundle override (otherwise system roots).
    pub ca_pem: Option<PathBuf>,
    /// Maximum reconnect backoff (capped per docs/TECH.md ‑ 60s).
    pub max_backoff: Duration,
}

impl Default for ConnectConfig {
    fn default() -> Self {
        Self {
            coordinator_url: "https://coordinator.iogrid.org:443".to_string(),
            identity_pem: PathBuf::from("/var/lib/iogrid/identity.pem"),
            ca_pem: None,
            max_backoff: Duration::from_secs(60),
        }
    }
}

/// Coordinator gRPC client — bidi stream over mTLS.
///
/// Scaffold: stores the config and reports a `Disconnected` state. The real
/// `connect()` will load the identity PEM, build a `tonic::transport::Channel`
/// with `rustls`, open the bidi stream and drive it to completion under a
/// `tokio::task`.
#[derive(Debug)]
pub struct CoordinatorClient {
    cfg: ConnectConfig,
    state: ClientState,
}

/// Connection state.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ClientState {
    /// Never connected.
    Disconnected,
    /// Currently connected, bidi stream alive.
    Connected,
    /// Connection terminated, will retry per backoff.
    Reconnecting,
}

impl CoordinatorClient {
    /// Build a client. Does NOT open the network connection — call
    /// [`Self::connect`] to actually dial.
    pub fn new(cfg: ConnectConfig) -> Self {
        Self {
            cfg,
            state: ClientState::Disconnected,
        }
    }

    /// Current connection state.
    pub fn state(&self) -> ClientState {
        self.state
    }

    /// Borrowed config.
    pub fn config(&self) -> &ConnectConfig {
        &self.cfg
    }

    /// Open the bidi gRPC stream.
    ///
    /// Scaffold: returns `Ok(())` without I/O if `identity_pem` parses to a
    /// non-empty path AND `coordinator_url` looks URL-shaped. The real
    /// implementation will load the PEM, build the tonic Channel and spawn
    /// the bidi pump.
    pub async fn connect(&mut self) -> Result<(), TransportError> {
        if !self.cfg.coordinator_url.starts_with("https://") {
            return Err(TransportError::InvalidUrl(self.cfg.coordinator_url.clone()));
        }
        // Real impl: read PEM, build TLS config, dial channel.
        self.state = ClientState::Connected;
        tracing::info!(
            coordinator = %self.cfg.coordinator_url,
            "coordinator client connected (scaffold)"
        );
        Ok(())
    }

    /// Tear down the connection.
    pub async fn close(&mut self) {
        self.state = ClientState::Disconnected;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn default_config_is_https() {
        let c = ConnectConfig::default();
        assert!(c.coordinator_url.starts_with("https://"));
        assert_eq!(c.max_backoff, Duration::from_secs(60));
    }

    #[tokio::test]
    async fn connect_rejects_plaintext_url() {
        let cfg = ConnectConfig {
            coordinator_url: "http://insecure.example".into(),
            ..ConnectConfig::default()
        };
        let mut c = CoordinatorClient::new(cfg);
        let err = c.connect().await.unwrap_err();
        assert!(matches!(err, TransportError::InvalidUrl(_)));
    }

    #[tokio::test]
    async fn scaffold_connect_marks_connected() {
        let mut c = CoordinatorClient::new(ConnectConfig::default());
        c.connect().await.expect("scaffold connect ok");
        assert_eq!(c.state(), ClientState::Connected);
        c.close().await;
        assert_eq!(c.state(), ClientState::Disconnected);
    }
}
