//! iogridd core supervisor.
//!
//! Owns the tokio runtime, top-level state machine and the lifecycle of every
//! subsystem (transport, routing, ui-bridge, workload runners). Other crates
//! deliberately depend on this crate's public types for events / config / state
//! so the daemon binary stays a thin assembly.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::path::PathBuf;

use serde::{Deserialize, Serialize};

/// Top-level supervisor state. Mirrors the public dashboard chip in the web UI.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum SupervisorState {
    /// Process has started but has not yet paired with the coordinator.
    Starting,
    /// Connected, idle — workloads will be accepted if scheduler says Active.
    Connected,
    /// Currently executing one or more workloads.
    Active,
    /// Scheduler says Paused — see [`PauseReason`] for the cause.
    Paused,
    /// Fatal error — daemon will exit after flushing audit log.
    Faulted,
}

/// Reasons the scheduler may pause the daemon. Mirrors `iogrid-scheduler`.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum PauseReason {
    /// Bandwidth cap reached for the current billing window.
    BandwidthCapReached,
    /// CPU cap reached.
    CpuCapReached,
    /// User is currently active (idle-only mode).
    UserActive,
    /// Outside the provider's configured active calendar window.
    OutsideCalendarWindow,
    /// Provider toggled "paused" from the web UI.
    ManuallyPaused,
}

/// Daemon configuration loaded from disk on startup and hot-reloadable from the
/// UI bridge. Fields are intentionally minimal in the scaffold; later commits
/// extend in-place.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DaemonConfig {
    /// Coordinator URL (gRPC over mTLS).
    pub coordinator_url: String,
    /// Path to the daemon identity cert + private key bundle.
    pub identity_pem: PathBuf,
    /// UI bridge listen address (loopback only).
    pub ui_listen: std::net::SocketAddr,
    /// Bandwidth cap, gigabytes per billing window.
    pub bandwidth_cap_gb: u64,
    /// CPU cap, percent of total system CPU.
    pub cpu_cap_pct: u8,
    /// Memory cap, percent of total system RAM.
    pub memory_cap_pct: u8,
    /// Only accept work when user has been idle for at least this many seconds.
    pub idle_threshold_secs: u64,
    /// If true, idle-detection gate is enforced.
    pub idle_only: bool,
}

impl Default for DaemonConfig {
    fn default() -> Self {
        Self {
            coordinator_url: "https://coordinator.iogrid.org:443".to_string(),
            identity_pem: PathBuf::from("/var/lib/iogrid/identity.pem"),
            ui_listen: "127.0.0.1:7777".parse().expect("static loopback"),
            bandwidth_cap_gb: 50,
            cpu_cap_pct: 30,
            memory_cap_pct: 25,
            idle_threshold_secs: 300,
            idle_only: true,
        }
    }
}

/// Supervisor — owns the tokio runtime and subsystem joinset.
///
/// This is a scaffold; the real supervisor will wire up the subsystem futures
/// and run them under a single JoinSet, restarting on error per the documented
/// supervision tree.
#[derive(Debug)]
pub struct Supervisor {
    config: DaemonConfig,
    state: SupervisorState,
}

impl Supervisor {
    /// Build a supervisor with the supplied config.
    pub fn new(config: DaemonConfig) -> Self {
        Self {
            config,
            state: SupervisorState::Starting,
        }
    }

    /// Current state.
    pub fn state(&self) -> SupervisorState {
        self.state
    }

    /// Borrowed config.
    pub fn config(&self) -> &DaemonConfig {
        &self.config
    }

    /// Drive the supervisor to completion.
    ///
    /// Scaffold: spawns no subsystems yet; returns immediately. The real
    /// implementation will block until shutdown is requested.
    pub async fn run(mut self) -> anyhow::Result<()> {
        tracing::info!(
            coordinator = %self.config.coordinator_url,
            "iogridd supervisor scaffold started",
        );
        self.state = SupervisorState::Connected;
        // TODO(#9): join transport / routing / ui-bridge / scheduler tasks.
        Ok(())
    }
}

/// Initialise structured logging from `RUST_LOG` (defaults to `info`).
pub fn init_tracing() {
    let filter = tracing_subscriber::EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| tracing_subscriber::EnvFilter::new("info"));
    let _ = tracing_subscriber::fmt()
        .with_env_filter(filter)
        .json()
        .try_init();
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn config_defaults_are_sane() {
        let c = DaemonConfig::default();
        assert!(c.bandwidth_cap_gb > 0);
        assert!(c.cpu_cap_pct <= 100);
        assert!(c.memory_cap_pct <= 100);
        assert!(c.ui_listen.ip().is_loopback());
    }

    #[test]
    fn supervisor_starts_in_starting_state() {
        let sup = Supervisor::new(DaemonConfig::default());
        assert_eq!(sup.state(), SupervisorState::Starting);
    }

    #[tokio::test]
    async fn supervisor_run_completes_in_scaffold() {
        let sup = Supervisor::new(DaemonConfig::default());
        sup.run().await.expect("scaffold run never errors");
    }
}
