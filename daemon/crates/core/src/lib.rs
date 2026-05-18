//! iogridd core supervisor.
//!
//! Owns the tokio runtime and the lifecycle of every subsystem (transport,
//! routing, ui-bridge, scheduler, anti-abuse, workload runners).  Other
//! crates deliberately depend on this crate's public types for events /
//! config / state so the daemon binary stays a thin assembly.
//!
//! Concrete responsibilities:
//!
//! 1. Load `DaemonConfig` from `~/.iogrid/config.toml` (creating defaults
//!    on first boot).
//! 2. Initialise the scheduler + anti-abuse filter + ui-bridge state.
//! 3. Spawn the platform-specific idle-detection / sysinfo poller.
//! 4. Spawn the transport reconnect loop + heartbeat publisher.
//! 5. Spawn the ui-bridge listener on 127.0.0.1:7777.
//! 6. Park on `tokio::signal::ctrl_c` until shutdown, then JoinSet-graceful.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use tokio::task::JoinSet;

pub use iogrid_anti_abuse::{Filter, InMemoryFilter, RulesetSnapshot};
pub use iogrid_scheduler::{PauseReason as SchedPauseReason, SchedulerConfig, SchedulerHandle};
pub use iogrid_transport::ConnectConfig as TransportConfig;
pub use iogrid_ui_bridge::{
    AuditEvent, BridgeState, DaemonStateView, EarningsView, PairHandler, PairRequest, PairResponse,
};

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

/// Reasons the scheduler may pause the daemon. Re-exposed here so external
/// callers don't need to depend on the scheduler crate directly.
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

/// Daemon configuration loaded from disk on startup and hot-reloadable from
/// the UI bridge.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DaemonConfig {
    /// Provider id assigned at pairing time (empty on first boot).
    #[serde(default)]
    pub provider_id: String,
    /// Coordinator URL (gRPC over mTLS).
    pub coordinator_url: String,
    /// Daemon state dir — holds cert.pem, key.pem, config.toml, ledger.
    pub state_dir: PathBuf,
    /// UI bridge listen address (loopback only).
    pub ui_listen: std::net::SocketAddr,
    /// SOCKS5 acceptor address (bound to the WireGuard interface in prod).
    pub socks_listen: std::net::SocketAddr,
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
    /// Heartbeat cadence (seconds).
    pub heartbeat_secs: u64,
    /// Anti-abuse filter refresh cadence (seconds).
    pub filter_refresh_secs: u64,
}

impl Default for DaemonConfig {
    fn default() -> Self {
        let home = std::env::var_os("HOME")
            .or_else(|| std::env::var_os("USERPROFILE"))
            .map(PathBuf::from)
            .unwrap_or_else(|| PathBuf::from("/var/lib/iogrid"));
        Self {
            provider_id: String::new(),
            coordinator_url: "https://coordinator.iogrid.org:443".to_string(),
            state_dir: home.join(".iogrid"),
            ui_listen: "127.0.0.1:7777".parse().expect("static loopback"),
            socks_listen: "127.0.0.1:7878".parse().expect("static loopback"),
            bandwidth_cap_gb: 50,
            cpu_cap_pct: 30,
            memory_cap_pct: 25,
            idle_threshold_secs: 300,
            idle_only: true,
            heartbeat_secs: 5,
            filter_refresh_secs: 300,
        }
    }
}

impl DaemonConfig {
    /// Path of the config TOML file on disk.
    pub fn config_path(&self) -> PathBuf {
        self.state_dir.join("config.toml")
    }

    /// Load config from disk; if missing, write defaults and return them.
    pub fn load_or_init(state_dir: &Path) -> anyhow::Result<Self> {
        std::fs::create_dir_all(state_dir)?;
        let path = state_dir.join("config.toml");
        if path.exists() {
            let body = std::fs::read_to_string(&path)?;
            let cfg: DaemonConfig = toml::from_str(&body)?;
            Ok(cfg)
        } else {
            let cfg = DaemonConfig {
                state_dir: state_dir.to_path_buf(),
                ..DaemonConfig::default()
            };
            cfg.save()?;
            Ok(cfg)
        }
    }

    /// Persist this config to `state_dir/config.toml`.
    pub fn save(&self) -> anyhow::Result<()> {
        std::fs::create_dir_all(&self.state_dir)?;
        let body = toml::to_string_pretty(self)?;
        std::fs::write(self.config_path(), body)?;
        Ok(())
    }

    /// Derive a scheduler config from the daemon config.
    pub fn scheduler(&self) -> SchedulerConfig {
        SchedulerConfig {
            bandwidth_cap_gb: self.bandwidth_cap_gb,
            cpu_cap_pct: self.cpu_cap_pct,
            memory_cap_pct: self.memory_cap_pct,
            idle_threshold_secs: self.idle_threshold_secs,
            idle_only: self.idle_only,
            calendar: Vec::new(),
        }
    }
}

/// Platform-specific idle source for the scheduler poller.
#[derive(Debug, Default, Clone, Copy)]
pub struct PlatformIdleSource;

impl iogrid_scheduler::IdleSource for PlatformIdleSource {
    fn idle_seconds(&self) -> u64 {
        #[cfg(target_os = "linux")]
        {
            iogrid_platform_linux::idle_seconds()
        }
        #[cfg(target_os = "macos")]
        {
            iogrid_platform_mac::idle_seconds()
        }
        #[cfg(target_os = "windows")]
        {
            iogrid_platform_windows::idle_seconds()
        }
        #[cfg(not(any(target_os = "linux", target_os = "macos", target_os = "windows")))]
        {
            u64::MAX
        }
    }
}

/// Supervisor — owns the tokio runtime and subsystem joinset.
pub struct Supervisor {
    config: DaemonConfig,
    state: SupervisorState,
    scheduler: SchedulerHandle,
    filter: Arc<InMemoryFilter>,
    bridge: BridgeState,
}

impl std::fmt::Debug for Supervisor {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Supervisor")
            .field("state", &self.state)
            .field("coordinator", &self.config.coordinator_url)
            .field("ui_listen", &self.config.ui_listen)
            .finish()
    }
}

impl Supervisor {
    /// Build a supervisor with the supplied config.
    pub fn new(config: DaemonConfig) -> Self {
        let scheduler = SchedulerHandle::new(config.scheduler());
        let filter = Arc::new(InMemoryFilter::new());
        let bridge = BridgeState::default()
            .with_scheduler(scheduler.clone())
            .with_filter(filter.clone());
        bridge.set(DaemonStateView {
            state: "starting".into(),
            version: env!("CARGO_PKG_VERSION").to_string(),
            coordinator_url: config.coordinator_url.clone(),
            ..Default::default()
        });
        Self {
            config,
            state: SupervisorState::Starting,
            scheduler,
            filter,
            bridge,
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

    /// Borrowed scheduler handle.
    pub fn scheduler(&self) -> &SchedulerHandle {
        &self.scheduler
    }

    /// Borrowed filter.
    pub fn filter(&self) -> Arc<InMemoryFilter> {
        self.filter.clone()
    }

    /// Borrowed UI bridge state.
    pub fn bridge(&self) -> &BridgeState {
        &self.bridge
    }

    /// Drive the supervisor to completion. Returns when shutdown is
    /// requested (SIGINT / SIGTERM on Unix, Ctrl+C on Windows).
    pub async fn run(mut self) -> anyhow::Result<()> {
        tracing::info!(
            coordinator = %self.config.coordinator_url,
            ui_listen = %self.config.ui_listen,
            socks_listen = %self.config.socks_listen,
            "iogridd supervisor starting",
        );
        self.state = SupervisorState::Connected;
        let mut tasks: JoinSet<anyhow::Result<()>> = JoinSet::new();

        // Idle + sysinfo poller.
        let h_poll = iogrid_scheduler::spawn_poller(
            self.scheduler.clone(),
            PlatformIdleSource,
            Duration::from_secs(5),
        );
        tasks.spawn(async move {
            h_poll.await.map_err(anyhow::Error::from)?;
            Ok(())
        });

        // UI bridge.
        let bridge = self.bridge.clone();
        let ui_listen = self.config.ui_listen;
        tasks.spawn(async move {
            iogrid_ui_bridge::serve(ui_listen, bridge)
                .await
                .map_err(anyhow::Error::from)
        });

        // Heartbeat pump — in-memory sink until the supervisor wires the
        // real gRPC stub (follow-up PR).
        let provider_id = if self.config.provider_id.is_empty() {
            "unpaired".to_string()
        } else {
            self.config.provider_id.clone()
        };
        let sink = Arc::new(iogrid_transport::MemSink::default());
        let h_hb = iogrid_transport::spawn_heartbeat_pump(
            provider_id.clone(),
            self.scheduler.clone(),
            sink.clone(),
            Duration::from_secs(self.config.heartbeat_secs),
        );
        tasks.spawn(async move {
            h_hb.await.map_err(anyhow::Error::from)?;
            Ok(())
        });

        // Anti-abuse refresher — coordinator-backed source. Without a paired
        // identity it will simply re-emit empty bundles until pairing
        // completes; that's the desired fail-closed default.
        let ruleset_source = Arc::new(iogrid_transport::ruleset::CoordinatorRulesetSource::new());
        let h_aa = iogrid_anti_abuse::spawn_refresher(
            self.filter.as_ref().clone(),
            ruleset_source,
            Duration::from_secs(self.config.filter_refresh_secs),
        );
        tasks.spawn(async move {
            h_aa.await.map_err(anyhow::Error::from)?;
            Ok(())
        });

        // Block on Ctrl+C / SIGTERM. We don't kill in-flight workloads —
        // the JoinSet drains on drop and tasks see the cancellation token.
        wait_for_shutdown().await;
        tracing::info!("iogridd shutdown requested");
        tasks.shutdown().await;
        Ok(())
    }
}

async fn wait_for_shutdown() {
    #[cfg(unix)]
    {
        use tokio::signal::unix::{signal, SignalKind};
        let mut term = signal(SignalKind::terminate()).expect("install SIGTERM handler");
        let mut intr = signal(SignalKind::interrupt()).expect("install SIGINT handler");
        tokio::select! {
            _ = term.recv() => {}
            _ = intr.recv() => {}
        }
    }
    #[cfg(not(unix))]
    {
        let _ = tokio::signal::ctrl_c().await;
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
        assert!(c.socks_listen.ip().is_loopback());
        assert!(c.coordinator_url.starts_with("https://"));
    }

    #[test]
    fn supervisor_starts_in_starting_state() {
        let sup = Supervisor::new(DaemonConfig::default());
        assert_eq!(sup.state(), SupervisorState::Starting);
    }

    #[test]
    fn config_round_trips_through_toml() {
        let dir = tempfile::tempdir().unwrap();
        let p = dir.path();
        let cfg1 = DaemonConfig::load_or_init(p).unwrap();
        assert!(p.join("config.toml").exists());
        let cfg2 = DaemonConfig::load_or_init(p).unwrap();
        assert_eq!(cfg1.bandwidth_cap_gb, cfg2.bandwidth_cap_gb);
        assert_eq!(cfg1.state_dir, p);
    }

    #[test]
    fn scheduler_handle_reflects_config() {
        let cfg = DaemonConfig {
            bandwidth_cap_gb: 123,
            cpu_cap_pct: 42,
            ..Default::default()
        };
        let sup = Supervisor::new(cfg);
        let s = sup.scheduler().config();
        assert_eq!(s.bandwidth_cap_gb, 123);
        assert_eq!(s.cpu_cap_pct, 42);
    }
}
