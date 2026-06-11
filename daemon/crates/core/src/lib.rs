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

pub mod tunnel;
pub mod vpn_wiring;
pub mod workloads;

use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

use serde::{Deserialize, Serialize};
use tokio::task::JoinSet;

pub use iogrid_anti_abuse::{Filter, InMemoryFilter, RulesetSnapshot};
pub use iogrid_scheduler::{PauseReason as SchedPauseReason, SchedulerConfig, SchedulerHandle};
pub use iogrid_transport::ConnectConfig as TransportConfig;
pub use iogrid_transport::DispatchFrame;
pub use iogrid_ui_bridge::{
    AuditEvent, BridgeState, DaemonStateView, EarningsView, PairHandler, PairRequest, PairResponse,
    UpdateCheckOutcome, UpdateHandler,
};
pub use workloads::{ActiveAssignment, ActiveRegistry, WorkloadRouter, WorkloadRouterRunners};

pub mod pair;
pub mod pair_handler;
pub mod updater;

pub use pair_handler::SupervisorPairHandler;

// macOS-only IPC server for the status-bar menu app (issue #388 /
// EPIC #348 Phase 2-mac). Compiled out on every other target so the
// public surface stays identical and Linux / Windows builds carry
// zero extra deps.
#[cfg(target_os = "macos")]
pub mod ipc_mac;

// Windows-only Update.exe driver (issue #399 / EPIC #348 Phase 2-win).
// Compiled out elsewhere so non-Windows targets stay slim.
#[cfg(windows)]
pub mod update_windows;

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
    /// Auto-update knobs. Disabled by default; provider opts in via
    /// config.toml or the `/account/updates` web UI.
    #[serde(default)]
    pub updater: updater::UpdateConfig,

    /// VPN-542 (#542): VPN module configuration. `None` means
    /// disabled — pure SOCKS5 mode (the legacy default). The CLI
    /// flags `--vpn-svc / --vpn-listen-addr / --stun-server /
    /// --region` populate this when present.
    #[serde(default)]
    pub vpn: VpnConfig,
}

/// VPN-side configuration. Populated from the `--vpn-svc` etc. CLI
/// flags or from `config.toml`'s `[vpn]` table. When `vpn_svc_url`
/// is empty the supervisor skips spawning the VPN modules entirely.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct VpnConfig {
    /// vpn-svc base URL (e.g. `https://api.iogrid.org`). Empty
    /// string disables the VPN modules.
    #[serde(default)]
    pub vpn_svc_url: String,
    /// UDP address the boringtun WG server binds to. Defaults to
    /// `0.0.0.0:51820` (the IANA-registered WG port).
    #[serde(default = "default_vpn_listen_addr")]
    pub vpn_listen_addr: String,
    /// STUN server endpoint for srflx candidate discovery (#538 LB
    /// front). Defaults to `stun.iogrid.org:3478`.
    #[serde(default = "default_stun_server")]
    pub stun_server: String,
    /// Region slug we advertise on register + health POSTs. Empty
    /// string defaults to `us-east-1` at register time.
    #[serde(default)]
    pub region: String,
    /// #557: manually-configured public IP to publish as an extra host
    /// ICE candidate. Use when the daemon sits behind a UDP load
    /// balancer / static port-forward whose external address can't
    /// be derived from local interface enumeration AND the STUN
    /// srflx path is unavailable. Empty = disabled.
    #[serde(default)]
    pub public_ip: String,
    /// #529 path c: Linux WAN interface the TunForwardSink installs
    /// iptables MASQUERADE on. Empty = `eth0` (matches stock GH runner
    /// + most Hetzner provider images).
    ///
    /// Set this when the provider host has a different uplink (e.g.
    /// `wlan0` on a residential box, `enp4s0` on a workstation).
    #[serde(default)]
    pub wan_iface: String,
    /// #529 path c: name of the TUN device the daemon creates for
    /// inner-packet forwarding. Empty = `iogrid-tun0`. Override only
    /// if the host already has an interface with that name.
    #[serde(default)]
    pub tun_ifname: String,
}

fn default_vpn_listen_addr() -> String {
    "0.0.0.0:51820".to_string()
}
fn default_stun_server() -> String {
    "stun.iogrid.org:3478".to_string()
}

impl Default for VpnConfig {
    fn default() -> Self {
        Self {
            vpn_svc_url: String::new(),
            vpn_listen_addr: default_vpn_listen_addr(),
            stun_server: default_stun_server(),
            region: String::new(),
            public_ip: String::new(),
            wan_iface: String::new(),
            tun_ifname: String::new(),
        }
    }
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
            updater: updater::UpdateConfig::default(),
            vpn: VpnConfig::default(),
        }
    }
}

impl DaemonConfig {
    /// Path of the config TOML file on disk.
    pub fn config_path(&self) -> PathBuf {
        self.state_dir.join("config.toml")
    }

    /// Load config from disk; if missing, write defaults and return them.
    ///
    /// In addition, when the `IOGRID_SCHEDULER_PROFILE` environment variable
    /// is set to a recognised value (currently `headless`), the scheduler
    /// fields are overridden with that profile's defaults and the result is
    /// persisted back to `config.toml`. This means subsequent boots on the
    /// same machine pick up the correct profile even without re-setting the
    /// env var. Unrecognised values are ignored (laptop defaults stick).
    /// See iogrid#268.
    pub fn load_or_init(state_dir: &Path) -> anyhow::Result<Self> {
        std::fs::create_dir_all(state_dir)?;
        let path = state_dir.join("config.toml");
        let (mut cfg, existed) = if path.exists() {
            let body = std::fs::read_to_string(&path)?;
            let cfg: DaemonConfig = toml::from_str(&body)?;
            (cfg, true)
        } else {
            let cfg = DaemonConfig {
                state_dir: state_dir.to_path_buf(),
                ..DaemonConfig::default()
            };
            (cfg, false)
        };

        // Apply IOGRID_SCHEDULER_PROFILE override (if set + recognised).
        // We do this after the on-disk load so that a one-shot env-var run
        // permanently flips the persisted scheduler fields, matching the
        // contract described in iogrid#268.
        let profile_changed = match std::env::var("IOGRID_SCHEDULER_PROFILE") {
            Ok(p) => cfg.apply_scheduler_profile(&p),
            Err(_) => false,
        };

        if !existed || profile_changed {
            cfg.save()?;
        }
        Ok(cfg)
    }

    /// Overlay the scheduler-related fields from a profile (e.g. `"headless"`).
    ///
    /// Returns `true` if any of the four scheduler fields actually changed,
    /// so the caller can decide whether to re-persist `config.toml`.
    /// Unrecognised / empty profile names are no-ops and return `false`.
    pub fn apply_scheduler_profile(&mut self, profile: &str) -> bool {
        // Only flip the four scheduler-owned fields. Everything else
        // (coordinator URL, listen addresses, provider id, updater) stays
        // exactly as loaded so we don't clobber operator overrides.
        let sched = SchedulerConfig::default_for_profile(profile);

        // `default_for_profile` returns laptop defaults for any unrecognised
        // value, but we don't want to silently re-stamp a config that the
        // operator may have hand-edited. So bail unless the caller asked for
        // an explicitly-recognised non-default profile.
        let recognised = matches!(profile, "headless");
        if !recognised {
            return false;
        }

        let mut changed = false;
        if self.bandwidth_cap_gb != sched.bandwidth_cap_gb {
            self.bandwidth_cap_gb = sched.bandwidth_cap_gb;
            changed = true;
        }
        if self.cpu_cap_pct != sched.cpu_cap_pct {
            self.cpu_cap_pct = sched.cpu_cap_pct;
            changed = true;
        }
        if self.memory_cap_pct != sched.memory_cap_pct {
            self.memory_cap_pct = sched.memory_cap_pct;
            changed = true;
        }
        if self.idle_threshold_secs != sched.idle_threshold_secs {
            self.idle_threshold_secs = sched.idle_threshold_secs;
            changed = true;
        }
        if self.idle_only != sched.idle_only {
            self.idle_only = sched.idle_only;
            changed = true;
        }
        changed
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

/// Workload-type slugs this daemon advertises in its `DaemonHello`.
///
/// Every paired daemon advertises `BANDWIDTH` (Phase 0 proxy/bandwidth
/// path). A macOS host additionally advertises `IOS_BUILD` when it can run
/// either iOS-build runner: macOS 15+ for the Tart VM runner, or any macOS
/// with a usable local Xcode for the native host-direct runner. The
/// [`iogrid_platform_mac::supports_ios_build`] gate encodes that check; on
/// non-macOS targets the platform-mac crate isn't even linked, so the
/// `#[cfg]` arm below compiles to the bandwidth-only list.
pub fn eligible_workload_types() -> Vec<String> {
    // `mut` is only exercised on the macOS arm; suppress the Linux/Windows
    // unused-mut warning rather than fork the whole body per-platform.
    #[cfg_attr(not(target_os = "macos"), allow(unused_mut))]
    let mut types = vec!["BANDWIDTH".to_string()];
    #[cfg(target_os = "macos")]
    {
        if iogrid_platform_mac::supports_ios_build() {
            types.push("IOS_BUILD".to_string());
        }
    }
    types
}

/// Supervisor — owns the tokio runtime and subsystem joinset.
pub struct Supervisor {
    config: DaemonConfig,
    state: SupervisorState,
    scheduler: SchedulerHandle,
    filter: Arc<InMemoryFilter>,
    bridge: BridgeState,
    runners: WorkloadRouterRunners,
    // Holds the dispatch-bridge watch sender for the lifetime of the
    // supervisor so the bridge's `run_with_reconnect` cancel-receiver
    // doesn't see a closed channel and spin-loop. See #482.
    dispatch_cancel: Option<tokio::sync::watch::Sender<bool>>,
    // #705: holds the iOS-build poller's shutdown sender for the daemon's
    // lifetime (same rationale as dispatch_cancel). None until spawned.
    build_poller_cancel: Option<tokio::sync::watch::Sender<bool>>,
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
        Self::with_runners(config, WorkloadRouterRunners::scaffold())
    }

    /// Build a supervisor wired against a specific runner trio (used by
    /// tests + future `*-real` feature flags).
    pub fn with_runners(config: DaemonConfig, runners: WorkloadRouterRunners) -> Self {
        let scheduler = SchedulerHandle::new(config.scheduler());
        let filter = Arc::new(InMemoryFilter::new());
        let bridge = BridgeState::default()
            .with_scheduler(scheduler.clone())
            .with_filter(filter.clone());
        // #438 piece 3 — supervisor PairHandler. POST /pair now mints +
        // persists a bearer + flips per-route enforcement instead of
        // returning 503.
        let pair_handler = Arc::new(SupervisorPairHandler::new(
            config.state_dir.clone(),
            bridge.bearer_token.clone(),
        ));
        let bridge = bridge.with_pair_handler(pair_handler);
        // #438 piece 4 — re-arm bearer enforcement on startup. If a
        // previous pair persisted bearer.txt next to cert.pem / key.pem,
        // load it back into the BridgeState so /state /config /earnings
        // /audit/* /updates/check enforce immediately instead of falling
        // back to pre-pair pass-through. I/O failures don't fail the
        // boot — they just leave enforcement off until the next pair.
        match iogrid_transport::identity::IdentityBundle::load_bearer(&config.state_dir) {
            Ok(Some(token)) => {
                bridge.set_bearer_token(Some(token));
                tracing::info!(state_dir = %config.state_dir.display(), "bearer re-armed from disk");
            }
            Ok(None) => {
                tracing::info!(
                    state_dir = %config.state_dir.display(),
                    "no persisted bearer — UI bridge runs pre-pair pass-through",
                );
            }
            Err(e) => {
                tracing::warn!(error = %e, state_dir = %config.state_dir.display(), "failed to load persisted bearer; UI bridge runs pre-pair pass-through");
            }
        }
        // Windows: wire the Squirrel `Update.exe` driver so the future
        // tray UI's "Check for updates" verb (parallel to the macOS
        // statusbar from PR #402) can drive the daily update path
        // on-demand. iogrid#399.
        #[cfg(windows)]
        let bridge = bridge.with_update_handler(Arc::new(WindowsUpdateHandler));
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
            runners,
            dispatch_cancel: None,
            build_poller_cancel: None,
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
    /// requested (SIGINT / SIGTERM on Unix, Ctrl+C on Windows, or the
    /// macOS status-bar UI's `Quit` action via the [`ipc_mac`] UDS
    /// listener — see issue #388).
    pub async fn run(mut self) -> anyhow::Result<()> {
        tracing::info!(
            coordinator = %self.config.coordinator_url,
            ui_listen = %self.config.ui_listen,
            socks_listen = %self.config.socks_listen,
            "iogridd supervisor starting",
        );
        self.state = SupervisorState::Connected;
        let mut tasks: JoinSet<anyhow::Result<()>> = JoinSet::new();

        // Status-bar IPC shutdown signal (macOS only). Built here even
        // when the listener doesn't spawn so the macOS-only branch and
        // the wait_for_shutdown call below share one Arc<Notify> — and
        // so non-macOS builds don't need to feature-gate the call site.
        let ipc_shutdown = Arc::new(tokio::sync::Notify::new());

        // Spawn the menu-bar IPC listener on macOS. The listener fires
        // `ipc_shutdown.notify_waiters()` when the user picks Quit from
        // the status-bar menu, which `wait_for_shutdown` awaits alongside
        // SIGINT/SIGTERM. On every other platform this block compiles to
        // nothing — there's no status-bar UI to host.
        #[cfg(target_os = "macos")]
        {
            let sock = ipc_mac::socket_path(&self.config.state_dir);
            let handle = ipc_mac::spawn_listener(sock, ipc_shutdown.clone());
            tasks.spawn(async move {
                handle.await.map_err(anyhow::Error::from)??;
                Ok(())
            });
        }

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

        // Heartbeat pump.
        //
        // #311: when the daemon has a paired identity + reachable
        // coordinator URL, route heartbeats over a real bidi gRPC stream
        // (`SchedulingService.StreamHeartbeats`) so the coordinator's
        // providers-svc can UPDATE `providers.last_seen_at` on every
        // tick. Without this, the row stayed frozen at `registered_at`
        // and `/admin/providers` showed paired daemons as "offline"
        // forever (the founder DoD bug under #309).
        //
        // First-boot / unpaired daemons fall back to the in-memory
        // [`MemSink`] — there is no identity bundle to mTLS with yet,
        // so the heartbeats can't leave the box anyway.
        let provider_id = if self.config.provider_id.is_empty() {
            "unpaired".to_string()
        } else {
            self.config.provider_id.clone()
        };
        if let Some(hb_cfg) = live_transport_config(&self.config) {
            let live = iogrid_transport::spawn_live_heartbeats(hb_cfg);
            let h_hb = iogrid_transport::spawn_heartbeat_pump(
                provider_id.clone(),
                self.scheduler.clone(),
                live.sink.clone(),
                Duration::from_secs(self.config.heartbeat_secs),
            );
            tasks.spawn(async move {
                h_hb.await.map_err(anyhow::Error::from)?;
                Ok(())
            });

            // Consume server-side acks and apply them to the scheduler.
            // `operations_pause` is the operator override that lets
            // ops staff pause a misbehaving daemon without a redeploy.
            // `config_changed` is logged for now — the SchedulingConfig
            // refresh wire is owned by a separate follow-up; the daemon
            // currently rereads on next boot.
            let scheduler_for_ack = self.scheduler.clone();
            let iogrid_transport::LiveHeartbeatHandle {
                sink: _,
                ack_rx,
                cancel_tx,
                task,
            } = live;
            let mut ack_rx = ack_rx;
            tasks.spawn(async move {
                while let Some(ack) = ack_rx.recv().await {
                    scheduler_for_ack.set_operations_pause(ack.operations_pause);
                    if ack.config_changed {
                        tracing::info!("coordinator signalled config_changed in heartbeat ack");
                    }
                }
                Ok(())
            });

            // Bridge task runs until process exit. Keep the cancel sender
            // alive in the task so the watch channel stays open without
            // tying up another JoinSet entry; wiring `cancel_tx` to the
            // supervisor shutdown path is a follow-up alongside the
            // dispatch-bridge shutdown wire.
            tasks.spawn(async move {
                let _keep = cancel_tx;
                task.await.map_err(anyhow::Error::from)?;
                Ok(())
            });
            tracing::info!(
                provider_id = %provider_id,
                "live heartbeat bridge spawned (SchedulingService.StreamHeartbeats)"
            );
        } else {
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
            tracing::info!("heartbeat pump using MemSink (no paired identity yet)");
        }

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

        // Workload dispatch router — wires either:
        //   * the real bidi gRPC stream (`iogrid_transport::spawn_live_dispatch`)
        //     when the daemon has a paired identity bundle on disk + a non-
        //     placeholder coordinator URL, OR
        //   * an in-process loopback (`iogrid_transport::dispatch_loopback`)
        //     for unit tests + first-boot/unpaired state where the daemon has
        //     nothing to talk to yet.
        //
        // The router itself doesn't care which side it's wired to — the
        // DispatchChannel shape is identical. The loopback `_coord_side` is
        // dropped intentionally so test code that needs to drive the other
        // end calls `dispatch_loopback()` directly.
        let mut daemon_side = match live_transport_config(&self.config) {
            Some(mut connect_cfg) => {
                // #253: pre-resolve the coordinator's IP HERE — on the
                // supervisor task, BEFORE any reconnect loop spawns. The
                // resulting `Arc<RwLock<SocketAddr>>` is stashed on
                // `ConnectConfig` so every per-attempt `Channel::connect`
                // reads the cached IP instead of running `lookup_host`
                // inside the per-attempt future tonic / tower can drop.
                //
                // If pre-resolve fails (no network at boot, captive
                // portal, DNS daemon not yet up), we *still* spawn the
                // live bridge — `Channel::connect` falls back to the
                // legacy in-loop resolver path (PR #251) and the
                // reconnect loop's backoff covers the transient.
                // The supervisor isn't a viable place to block boot on
                // DNS: the operator may have brought up iogridd before
                // their VPN / wifi.
                match iogrid_transport::pre_resolve_addr(&connect_cfg.coordinator_url).await {
                    Ok(arc) => {
                        connect_cfg.resolved_addr = Some(arc.clone());
                        // Hourly refresh — picks up coordinator LB IP
                        // rotations even when the daemon stays healthy.
                        // The cancel watch is wired into the JoinSet's
                        // shutdown path via the Drop on the sender.
                        let (refresh_cancel_tx, refresh_cancel_rx) =
                            tokio::sync::watch::channel(false);
                        let refresh_handle = iogrid_transport::spawn_addr_refresh(
                            connect_cfg.coordinator_url.clone(),
                            arc,
                            Duration::from_secs(3600),
                            refresh_cancel_rx,
                        );
                        // Keep the sender alive until shutdown by
                        // moving it into the joinset task; when the
                        // joinset is shutdown(), the task drops the
                        // sender and the refresh loop exits.
                        tasks.spawn(async move {
                            let _keep = refresh_cancel_tx;
                            refresh_handle.await.map_err(anyhow::Error::from)?;
                            Ok(())
                        });
                    }
                    Err(e) => {
                        tracing::warn!(
                            coordinator = %connect_cfg.coordinator_url,
                            error = %e,
                            "supervisor pre-resolve failed; falling back to per-attempt resolver (PR #251)"
                        );
                    }
                }
                // #253: single-permit semaphore. Phase 0 only the
                // dispatch loop dials, so it's a no-op today; once
                // heartbeat + ruleset become real gRPC streams the
                // permit prevents three parallel connect attempts
                // racing through the same blocking-getaddrinfo pool.
                connect_cfg.connect_semaphore = Some(Arc::new(tokio::sync::Semaphore::new(1)));

                let hello = iogrid_transport::DispatchHello {
                    provider_id: self.config.provider_id.clone(),
                    // Phase 0: every paired daemon advertises BANDWIDTH so
                    // the proxy-gateway SOCKS5 path can route customer
                    // traffic. IOS_BUILD is added here when the host is a
                    // macOS 15 (Sequoia) or newer machine — the Tart-based
                    // iOS-build runner needs the Xcode-26 / iOS-18 SDK that
                    // only ships on Sequoia+. workloads-svc's scheduler hard-
                    // filters `ios_build` to providers that both advertise the
                    // type AND report Platform=macos, so non-Macs that somehow
                    // advertised it would still be rejected. (DOCKER/GPU get
                    // added by their respective runner wires.)
                    supported_types: eligible_workload_types(),
                    max_concurrent: 4,
                };
                let handle = iogrid_transport::spawn_live_dispatch(connect_cfg, hello);
                tracing::info!(
                    coordinator = %self.config.coordinator_url,
                    "live dispatch bridge spawned"
                );
                // #705: poll-based iOS-build dispatch. The dispatch stream's
                // server→client Assignment push is dropped by the edge for a
                // REMOTE daemon; the poller fetches assignments over a plain
                // GET (which traverses the edge, like the VPN binder) and
                // runs them through the same iOS-build runner. Only spawned
                // on a host that can actually build iOS (advertises the type)
                // and that has a real provider id.
                if eligible_workload_types().iter().any(|t| t == "IOS_BUILD")
                    && !self.config.provider_id.trim().is_empty()
                {
                    let (tx, rx) = tokio::sync::watch::channel(false);
                    // The JoinHandle is intentionally dropped: the poll task
                    // runs detached for the daemon's lifetime and is stopped
                    // via the watch sender below, not by joining.
                    let _poller_task = iogrid_workload_ios::build_poller::spawn_build_poller(
                        iogrid_workload_ios::build_poller::BuildPollerConfig {
                            provider_id: self.config.provider_id.clone(),
                            coordinator_base_url: self.config.coordinator_url.clone(),
                        },
                        reqwest::Client::new(),
                        self.runners.ios.clone(),
                        rx,
                    );
                    self.build_poller_cancel = Some(tx);
                    tracing::info!(
                        provider_id = %self.config.provider_id,
                        "iOS-build poller spawned (#705 poll-based dispatch)"
                    );
                }
                // Keep `cancel_tx` alive on the supervisor stack for the
                // lifetime of the daemon. Dropping the watch sender closes
                // the channel; the bridge's `run_with_reconnect` then sees
                // `cancel.changed().await` return Err on every poll and
                // races the connect-future to cancellation, producing the
                // sub-millisecond spin-loop diagnosed via #482. Mirrors
                // the heartbeat side at L508 which already pins its sender.
                let iogrid_transport::LiveDispatchHandle {
                    daemon_side,
                    cancel_tx,
                    task: _,
                } = handle;
                self.dispatch_cancel = Some(cancel_tx);
                daemon_side
            }
            None => {
                let (daemon_side, _coord_side) = iogrid_transport::dispatch_loopback();
                tracing::info!(
                    "dispatch loopback active (no paired identity / placeholder coordinator URL)"
                );
                daemon_side
            }
        };
        let router = Arc::new(WorkloadRouter::new(
            self.runners.clone(),
            daemon_side.tx.clone(),
            self.scheduler.clone(),
        ));
        let router_for_dispatch = router.clone();
        // TunnelManager: routes TunnelOpen/Data/Close from coordinator
        // to per-attempt TCP pumps; needed for BANDWIDTH workload bytes
        // to flow end-to-end. Without it, the workloads-svc forwarder
        // sends TunnelOpen down the bidi stream and the daemon silently
        // drops it — the proxy-gateway side then waits forever for
        // upstream bytes. See iogrid/iogrid#482.
        let tunnel_manager = Arc::new(tunnel::TunnelManager::new(
            daemon_side.tx.clone(),
            4, // matches DaemonHello.max_concurrent advertised above
            self.filter.clone(),
        ));
        let tunnel_for_dispatch = tunnel_manager.clone();
        // Registry clone for TunnelOpen → workload_id attribution (#490).
        let registry_for_tunnel = router.registry();
        tasks.spawn(async move {
            while let Some(frame) = daemon_side.rx.recv().await {
                match frame {
                    DispatchFrame::TunnelOpen {
                        attempt_id,
                        target_host_port,
                    } => {
                        // Look up the workload_id for this attempt so the pump
                        // can emit bytes_in/bytes_out in its Update on close.
                        // Falls back to empty string if the assignment is gone
                        // (race with Cancel/Drain — billing-svc matches on
                        // attempt_id anyway).
                        let workload_id = registry_for_tunnel
                            .workload_id_for_attempt(&attempt_id)
                            .unwrap_or_default();
                        tunnel_for_dispatch
                            .open(workload_id, attempt_id, target_host_port)
                            .await;
                    }
                    DispatchFrame::TunnelData {
                        attempt_id,
                        payload,
                    } => {
                        tunnel_for_dispatch.data(&attempt_id, payload).await;
                    }
                    DispatchFrame::TunnelClose { attempt_id, error } => {
                        tunnel_for_dispatch.close(&attempt_id, error).await;
                    }
                    other => {
                        router_for_dispatch.handle(other).await;
                    }
                }
            }
            Ok(())
        });

        // Windows-only: daily Squirrel `Update.exe` tick.
        //
        // Phase 2 of EPIC #348 (Windows track #399). The legacy manifest
        // poller below still runs, but on Windows we prefer the
        // Squirrel-native side-by-side update path so providers get
        // the same UAC-free, atomic-restart behaviour the macOS
        // track (Sparkle) ships in PR #402. Update.exe terminates the
        // parent process on a successful swap, so the supervisor's
        // current JoinSet entries are drained naturally and SCM
        // restarts the new `app-X.Y.Z\iogridd.exe`.
        //
        // Disabled by default. Provider opts in via the same
        // `updater.disabled = false` flag used by the manifest poller —
        // we don't want this path to fire without the same explicit
        // opt-in the macOS Sparkle track requires.
        #[cfg(windows)]
        if !self.config.updater.disabled {
            // 24h cadence with a small head-start so a freshly-paired
            // daemon doesn't sit at the old version for a full day.
            let initial_delay = Duration::from_secs(5 * 60);
            let tick = Duration::from_secs(24 * 60 * 60);
            tasks.spawn(async move {
                tokio::time::sleep(initial_delay).await;
                let mut ticker = tokio::time::interval(tick);
                // First tick fires immediately after the sleep —
                // skip it so we don't double-trigger right after the
                // 5min warm-up.
                ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
                ticker.tick().await;
                loop {
                    ticker.tick().await;
                    // `apply_update` is synchronous + cheap (it just
                    // shells out). We don't bother with
                    // `spawn_blocking` because there's no I/O wait —
                    // `Command::spawn` returns as soon as the child is
                    // forked.
                    match update_windows::apply_update() {
                        update_windows::UpdateStatus::Spawned {
                            update_exe,
                            feed_url,
                        } => {
                            tracing::info!(
                                update_exe = %update_exe.display(),
                                feed_url,
                                "Update.exe spawned (daily tick)"
                            );
                        }
                        update_windows::UpdateStatus::UpdateExeMissing { probed } => {
                            tracing::warn!(
                                probed = ?probed,
                                "Update.exe not found at any probed location; \
                                 falling back to legacy manifest poller"
                            );
                        }
                        update_windows::UpdateStatus::SpawnFailed { update_exe, error } => {
                            tracing::warn!(
                                update_exe = %update_exe.display(),
                                error,
                                "Update.exe spawn failed; will retry on next tick"
                            );
                        }
                    }
                }
            });
            tracing::info!("Windows Update.exe daily tick spawned (Phase 2 #348)");
        }

        // Auto-update polling worker. Only spawned when the operator
        // opted in via config.toml. The default (disabled=true) makes
        // this a no-op — the worker task exits immediately. See #59.
        if !self.config.updater.disabled {
            let ctx = updater::PollCtx {
                config: self.config.updater.clone(),
                current_version: env!("CARGO_PKG_VERSION").to_string(),
                target: target_triple().to_string(),
                layout: default_install_layout(),
                fetcher: std::sync::Arc::new(updater::HttpFetcher::default()),
            };
            let _handle = updater::spawn_update_poll(ctx);
            // We deliberately drop _handle — the supervisor doesn't yet
            // surface UpdateState to the UI bridge in this PR. The
            // follow-up "expose state to /api/v1/account/updates" PR
            // will store the handle on `Supervisor` and forward it.
            tracing::info!(
                channel = %self.config.updater.channel.as_str(),
                manifest_url = %self.config.updater.manifest_url,
                "auto-update polling enabled"
            );
        }

        // Scheduler-pause watcher — if scheduler flips to Paused, revoke
        // every in-flight assignment (the supervisor's contract per
        // docs/TECH.md § Workload acceptance).
        let scheduler_for_watch = self.scheduler.clone();
        let router_for_watch = router.clone();
        tasks.spawn(async move {
            let mut last_active = matches!(
                scheduler_for_watch.refresh(),
                iogrid_scheduler::State::Active
            );
            let mut ticker = tokio::time::interval(Duration::from_secs(1));
            loop {
                ticker.tick().await;
                let now_active = matches!(
                    scheduler_for_watch.refresh(),
                    iogrid_scheduler::State::Active
                );
                if last_active && !now_active {
                    tracing::info!("scheduler flipped to paused — revoking active workloads");
                    router_for_watch.revoke_all("scheduler_paused").await;
                }
                last_active = now_active;
            }
        });

        // Block on Ctrl+C / SIGTERM / status-bar Quit. We don't kill
        // VPN-542 (#542): spin up the VPN modules — boringtun WG
        // server + register + health + ICE + peer-binder — if the
        // operator passed --vpn-svc (or set [vpn] vpn_svc_url in
        // config.toml). Disabled by default so pure-SOCKS5 deployments
        // are unaffected.
        let vpn_handles = vpn_wiring::spawn_vpn_modules(&self.config).await;

        // in-flight workloads — the JoinSet drains on drop and tasks
        // see the cancellation token.
        wait_for_shutdown(ipc_shutdown.clone()).await;
        tracing::info!("iogridd shutdown requested");
        // Best-effort: cancel everything still in-flight.
        router.revoke_all("daemon_shutdown").await;
        // Signal the VPN modules to exit; the BoringTun pump and all
        // 3 reporter loops watch this `watch::Sender<bool>`.
        if let Some(handles) = vpn_handles {
            let _ = handles.shutdown_tx.send(true);
            // `Tunnel::stop` is the trait method — bring it into scope
            // for the call so the BoringTun shutdown sequence runs.
            use iogrid_routing::Tunnel;
            if let Err(e) = handles.boringtun.stop().await {
                tracing::warn!(error = %e, "boringtun stop returned err during shutdown");
            }
            // We don't await `handles.task_handles` — each task has a
            // bounded shutdown path (offline POST budget = 3 s on the
            // health loop, instant on the others); the JoinSet drain
            // below catches any straggler the tokio runtime tracks.
        }
        tasks.shutdown().await;
        Ok(())
    }
}

/// Decide whether the supervisor should attempt a real coordinator dispatch
/// stream this boot. Returns `Some(ConnectConfig)` when *every* prerequisite
/// is satisfied:
///
///  * `coordinator_url` is an `https://` URL (rules out the unpaired-default
///    placeholder check by the caller — see also [`DaemonConfig::default`]).
///  * `provider_id` is non-empty (i.e. pairing completed).
///  * `cert.pem` AND `key.pem` exist under `state_dir`.
///
/// All three guards must hold; otherwise the supervisor falls back to the
/// in-process loopback so first-boot/unpaired daemons (and unit tests that
/// don't write an identity bundle) stay self-contained.
///
/// Environment override: setting `IOGRID_FORCE_LOOPBACK=1` keeps the
/// loopback path regardless of identity / URL — useful for the
/// integration tests that drive the dispatch oneself via
/// [`iogrid_transport::dispatch_loopback`].
pub fn live_transport_config(cfg: &DaemonConfig) -> Option<TransportConfig> {
    if std::env::var_os("IOGRID_FORCE_LOOPBACK").is_some() {
        return None;
    }
    if cfg.provider_id.trim().is_empty() {
        return None;
    }
    if !cfg.coordinator_url.starts_with("https://") {
        return None;
    }
    let cert = cfg.state_dir.join("cert.pem");
    let key = cfg.state_dir.join("key.pem");
    if !cert.exists() || !key.exists() {
        return None;
    }
    Some(TransportConfig {
        coordinator_url: cfg.coordinator_url.clone(),
        cert_pem: cert,
        key_pem: key,
        ca_pem: None,
        max_backoff: Duration::from_secs(60),
        initial_backoff: Duration::from_secs(1),
        // `Supervisor::run` populates both of these after this builder
        // returns — see the live-dispatch branch above. They stay None
        // here so tests and other callers that take this struct on the
        // first-boot path see the same shape as PR #251.
        resolved_addr: None,
        connect_semaphore: None,
    })
}

/// Park until any of the recognised shutdown triggers fires:
///   * SIGTERM / SIGINT (Unix, including macOS)
///   * Ctrl+C (Windows)
///   * `ipc_shutdown.notify_waiters()` — fired by the macOS status-bar
///     UI's `Quit` action via the `ipc_mac` UDS listener. On non-macOS
///     targets nothing ever fires this Notify, so it stays a benign
///     no-op branch in the `select!`.
async fn wait_for_shutdown(ipc_shutdown: Arc<tokio::sync::Notify>) {
    let ipc_notified = ipc_shutdown.notified();
    tokio::pin!(ipc_notified);

    #[cfg(unix)]
    {
        use tokio::signal::unix::{signal, SignalKind};
        let mut term = signal(SignalKind::terminate()).expect("install SIGTERM handler");
        let mut intr = signal(SignalKind::interrupt()).expect("install SIGINT handler");
        tokio::select! {
            _ = term.recv() => {}
            _ = intr.recv() => {}
            _ = ipc_notified => {}
        }
    }
    #[cfg(not(unix))]
    {
        tokio::select! {
            _ = tokio::signal::ctrl_c() => {}
            _ = ipc_notified => {}
        }
    }
}

/// Best-effort guess of the rustc target triple of the running binary.
/// Compiled in at build time. Used by the auto-updater to pick the
/// right artifact entry from the manifest.
pub fn target_triple() -> &'static str {
    // The full triple isn't exposed via env! but the relevant pieces
    // are: TARGET_OS, TARGET_ARCH, TARGET_ENV. We assemble the same
    // form rustc uses for its `--target` flag.
    #[cfg(all(target_os = "linux", target_arch = "x86_64"))]
    {
        "x86_64-unknown-linux-gnu"
    }
    #[cfg(all(target_os = "linux", target_arch = "aarch64"))]
    {
        "aarch64-unknown-linux-gnu"
    }
    #[cfg(all(target_os = "macos", target_arch = "x86_64"))]
    {
        "x86_64-apple-darwin"
    }
    #[cfg(all(target_os = "macos", target_arch = "aarch64"))]
    {
        "aarch64-apple-darwin"
    }
    #[cfg(all(target_os = "windows", target_arch = "x86_64"))]
    {
        "x86_64-pc-windows-msvc"
    }
    #[cfg(all(target_os = "windows", target_arch = "aarch64"))]
    {
        "aarch64-pc-windows-msvc"
    }
    #[cfg(not(any(
        all(target_os = "linux", target_arch = "x86_64"),
        all(target_os = "linux", target_arch = "aarch64"),
        all(target_os = "macos", target_arch = "x86_64"),
        all(target_os = "macos", target_arch = "aarch64"),
        all(target_os = "windows", target_arch = "x86_64"),
        all(target_os = "windows", target_arch = "aarch64"),
    )))]
    {
        "unsupported"
    }
}

/// Resolve the directory the running daemon binary lives in. Falls back
/// to a sensible OS-specific default if `current_exe()` is unavailable
/// (e.g. test binaries running under cargo-nextest).
pub fn default_install_layout() -> updater::InstallLayout {
    let exe = std::env::current_exe().ok();
    let (dir, name) = match exe {
        Some(p) => {
            let name = p
                .file_name()
                .map(|s| s.to_string_lossy().into_owned())
                .unwrap_or_else(|| "iogridd".into());
            let dir = p
                .parent()
                .map(PathBuf::from)
                .unwrap_or_else(default_fallback_install_dir);
            (dir, name)
        }
        None => (default_fallback_install_dir(), "iogridd".to_string()),
    };
    updater::InstallLayout::new(dir, name)
}

fn default_fallback_install_dir() -> PathBuf {
    #[cfg(target_os = "windows")]
    {
        PathBuf::from(r"C:\Program Files\iogrid")
    }
    #[cfg(not(target_os = "windows"))]
    {
        PathBuf::from("/usr/local/iogrid")
    }
}

/// Windows-only `UpdateHandler` impl that delegates straight to the
/// Squirrel `Update.exe` driver in [`update_windows`]. Wired into
/// [`BridgeState`] from [`Supervisor::with_runners`] so the future
/// Windows tray UI's "Check for updates" verb (parallel to the macOS
/// statusbar from PR #402) can trigger an on-demand update without
/// waiting for the 24h daily tick. iogrid#399.
#[cfg(windows)]
pub struct WindowsUpdateHandler;

#[cfg(windows)]
#[async_trait::async_trait]
impl iogrid_ui_bridge::UpdateHandler for WindowsUpdateHandler {
    async fn check_for_updates(&self) -> UpdateCheckOutcome {
        // `apply_update` shells out to `Command::spawn` and returns
        // immediately; no I/O wait that warrants spawn_blocking.
        match update_windows::apply_update() {
            update_windows::UpdateStatus::Spawned {
                update_exe,
                feed_url,
            } => UpdateCheckOutcome {
                status: "spawned".into(),
                message: format!(
                    "Update.exe spawned at {} against {}",
                    update_exe.display(),
                    feed_url,
                ),
            },
            update_windows::UpdateStatus::UpdateExeMissing { probed } => UpdateCheckOutcome {
                status: "missing".into(),
                message: format!(
                    "Update.exe not found at any of: {}",
                    probed
                        .iter()
                        .map(|p| p.display().to_string())
                        .collect::<Vec<_>>()
                        .join(", "),
                ),
            },
            update_windows::UpdateStatus::SpawnFailed { update_exe, error } => UpdateCheckOutcome {
                status: "spawn_failed".into(),
                message: format!("spawn {} failed: {}", update_exe.display(), error),
            },
        }
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
    fn eligible_types_always_include_bandwidth() {
        let t = eligible_workload_types();
        assert!(t.contains(&"BANDWIDTH".to_string()));
        // IOS_BUILD is added only on macOS 15+; on the Linux CI runner it
        // must be absent. On a Sequoia+ Mac the platform gate adds it.
        #[cfg(not(target_os = "macos"))]
        assert!(
            !t.contains(&"IOS_BUILD".to_string()),
            "non-macOS hosts must not advertise IOS_BUILD"
        );
        #[cfg(target_os = "macos")]
        assert_eq!(
            t.contains(&"IOS_BUILD".to_string()),
            iogrid_platform_mac::supports_ios_build()
        );
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
    fn live_transport_off_when_unpaired() {
        let dir = tempfile::tempdir().unwrap();
        let cfg = DaemonConfig {
            provider_id: String::new(),
            coordinator_url: "https://coordinator.iogrid.org:443".into(),
            state_dir: dir.path().to_path_buf(),
            ..DaemonConfig::default()
        };
        assert!(live_transport_config(&cfg).is_none());
    }

    #[test]
    fn live_transport_off_without_identity_bundle() {
        let dir = tempfile::tempdir().unwrap();
        let cfg = DaemonConfig {
            provider_id: "00000000-0000-0000-0000-000000000001".into(),
            coordinator_url: "https://coordinator.iogrid.org:443".into(),
            state_dir: dir.path().to_path_buf(),
            ..DaemonConfig::default()
        };
        assert!(live_transport_config(&cfg).is_none());
    }

    #[test]
    fn live_transport_on_when_paired_and_bundle_present() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(
            dir.path().join("cert.pem"),
            b"-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n",
        )
        .unwrap();
        std::fs::write(
            dir.path().join("key.pem"),
            b"-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n",
        )
        .unwrap();
        let cfg = DaemonConfig {
            provider_id: "00000000-0000-0000-0000-000000000001".into(),
            coordinator_url: "https://coordinator.iogrid.org:443".into(),
            state_dir: dir.path().to_path_buf(),
            ..DaemonConfig::default()
        };
        // The env override forces loopback irrespective of pairing.
        // SAFETY: tests run single-threaded under cargo-nextest per workspace
        // policy; this is the only test that pokes that env var.
        std::env::remove_var("IOGRID_FORCE_LOOPBACK");
        let live = live_transport_config(&cfg).expect("should choose live");
        assert_eq!(live.coordinator_url, cfg.coordinator_url);
        std::env::set_var("IOGRID_FORCE_LOOPBACK", "1");
        assert!(live_transport_config(&cfg).is_none());
        std::env::remove_var("IOGRID_FORCE_LOOPBACK");
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

    // ---- iogrid#268: IOGRID_SCHEDULER_PROFILE env override ----

    #[test]
    fn apply_scheduler_profile_headless_flips_fields_and_reports_change() {
        let mut cfg = DaemonConfig::default();
        // Sanity: laptop defaults are baked in.
        assert!(cfg.idle_only);
        assert_eq!(cfg.cpu_cap_pct, 30);

        let changed = cfg.apply_scheduler_profile("headless");
        assert!(changed, "headless must flip at least one field");
        assert!(!cfg.idle_only);
        assert_eq!(cfg.cpu_cap_pct, 80);
        assert_eq!(cfg.memory_cap_pct, 80);
        assert_eq!(cfg.idle_threshold_secs, 0);
    }

    #[test]
    fn apply_scheduler_profile_headless_twice_is_idempotent() {
        let mut cfg = DaemonConfig::default();
        let first = cfg.apply_scheduler_profile("headless");
        let second = cfg.apply_scheduler_profile("headless");
        assert!(first);
        assert!(!second, "second apply must be a no-op (no field changed)");
    }

    #[test]
    fn apply_scheduler_profile_unknown_is_no_op() {
        let mut cfg = DaemonConfig::default();
        let before_cpu = cfg.cpu_cap_pct;
        let before_idle_only = cfg.idle_only;
        let changed = cfg.apply_scheduler_profile("totally-bogus");
        assert!(!changed);
        assert_eq!(cfg.cpu_cap_pct, before_cpu);
        assert_eq!(cfg.idle_only, before_idle_only);
    }

    #[test]
    fn apply_scheduler_profile_preserves_non_scheduler_fields() {
        let mut cfg = DaemonConfig {
            provider_id: "00000000-0000-0000-0000-000000000042".into(),
            coordinator_url: "https://custom.example.com:443".into(),
            ..DaemonConfig::default()
        };
        cfg.apply_scheduler_profile("headless");
        assert_eq!(cfg.provider_id, "00000000-0000-0000-0000-000000000042");
        assert_eq!(cfg.coordinator_url, "https://custom.example.com:443");
    }

    #[test]
    fn load_or_init_headless_env_persists_and_subsequent_boots_keep_it() {
        // Combined headless + laptop flows in a single test so the env-var
        // manipulation isn't racing other tests in the same process. Mirrors
        // the precedent set by `live_transport_on_when_paired_and_bundle_present`.
        // SAFETY: this is the only test in the module that touches
        // IOGRID_SCHEDULER_PROFILE; the env mutation is bounded by set/remove
        // pairs.

        // --- Laptop (no env var) path: defaults stay laptop. ---
        std::env::remove_var("IOGRID_SCHEDULER_PROFILE");
        let dir_laptop = tempfile::tempdir().unwrap();
        let p_l = dir_laptop.path();
        let cfg_l = DaemonConfig::load_or_init(p_l).unwrap();
        assert!(cfg_l.idle_only);
        assert_eq!(cfg_l.cpu_cap_pct, 30);
        assert_eq!(cfg_l.memory_cap_pct, 25);
        assert_eq!(cfg_l.idle_threshold_secs, 300);

        // --- Headless env path: flips fields + persists. ---
        let dir_hl = tempfile::tempdir().unwrap();
        let p_h = dir_hl.path();
        std::env::set_var("IOGRID_SCHEDULER_PROFILE", "headless");
        let cfg_h1 = DaemonConfig::load_or_init(p_h).unwrap();
        std::env::remove_var("IOGRID_SCHEDULER_PROFILE");

        assert!(!cfg_h1.idle_only);
        assert_eq!(cfg_h1.cpu_cap_pct, 80);
        assert_eq!(cfg_h1.memory_cap_pct, 80);
        assert_eq!(cfg_h1.idle_threshold_secs, 0);

        // Subsequent boot without env var still reads headless values
        // because they were persisted to disk on the first boot.
        let cfg_h2 = DaemonConfig::load_or_init(p_h).unwrap();
        assert!(!cfg_h2.idle_only);
        assert_eq!(cfg_h2.cpu_cap_pct, 80);
        assert_eq!(cfg_h2.memory_cap_pct, 80);
        assert_eq!(cfg_h2.idle_threshold_secs, 0);
    }
}
