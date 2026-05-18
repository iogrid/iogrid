//! Scheduler — combined caps + calendar + idle-detect state machine.
//!
//! Three independent signals AND-combine to decide whether workloads are
//! accepted (`State::Active`) or rejected (`State::Paused(reason)`):
//!
//! 1. Resource caps (bandwidth GB-this-window, CPU%, memory%).
//! 2. Calendar window (active hours).
//! 3. Idle detection (user inactive for at least `idle_threshold_secs`).
//!
//! Mirrors the pseudo-code in `docs/TECH.md § Scheduling state machine`.
//!
//! In addition to the pure `decide()` function, this crate exposes a
//! [`SchedulerHandle`] type whose tokio task polls the platform crate for
//! idle seconds and `sysinfo` for CPU/MEM every 5 seconds and atomically
//! republishes the current state. The supervisor and workload runners read
//! the published state to gate work acceptance.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::sync::Arc;
use std::time::Duration;

use chrono::{DateTime, Datelike, NaiveTime, Utc, Weekday};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};

/// Scheduler verdict.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum State {
    /// Workloads are accepted.
    Active,
    /// Workloads are rejected; see reason.
    Paused(PauseReason),
}

/// Why the scheduler is currently paused.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum PauseReason {
    /// Bandwidth GB-this-window cap reached.
    BandwidthCapReached,
    /// CPU% cap reached.
    CpuCapReached,
    /// Memory% cap reached.
    MemoryCapReached,
    /// User is active (idle-only mode is on).
    UserActive,
    /// Now is outside the configured calendar window.
    OutsideCalendarWindow,
    /// Operator pressed the "pause" button in the UI.
    ManuallyPaused,
    /// Coordinator pushed a directive (e.g. maintenance).
    OperationsPause,
}

impl PauseReason {
    /// Short stable slug used in proto + UI.
    pub fn slug(&self) -> &'static str {
        match self {
            PauseReason::BandwidthCapReached => "bandwidth_cap",
            PauseReason::CpuCapReached => "cpu_cap",
            PauseReason::MemoryCapReached => "memory_cap",
            PauseReason::UserActive => "user_active",
            PauseReason::OutsideCalendarWindow => "outside_calendar",
            PauseReason::ManuallyPaused => "manually_paused",
            PauseReason::OperationsPause => "operations_pause",
        }
    }
}

/// Configuration for the scheduler.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SchedulerConfig {
    /// GB allowed per billing window.
    pub bandwidth_cap_gb: u64,
    /// CPU percent cap (0–100).
    pub cpu_cap_pct: u8,
    /// Memory percent cap (0–100).
    pub memory_cap_pct: u8,
    /// Only run when idle for at least this long.
    pub idle_threshold_secs: u64,
    /// If true, idle detection gate is enforced.
    pub idle_only: bool,
    /// Active calendar windows. Empty = always active.
    pub calendar: Vec<CalendarWindow>,
}

impl Default for SchedulerConfig {
    fn default() -> Self {
        Self {
            bandwidth_cap_gb: 50,
            cpu_cap_pct: 30,
            memory_cap_pct: 25,
            idle_threshold_secs: 300,
            idle_only: true,
            calendar: Vec::new(),
        }
    }
}

/// One active window on the weekly calendar.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct CalendarWindow {
    /// Day of week this window applies to.
    pub weekday: Weekday,
    /// Window start (UTC).
    pub start_utc: NaiveTime,
    /// Window end (UTC).
    pub end_utc: NaiveTime,
}

impl CalendarWindow {
    /// Is `now` inside this window?
    pub fn contains(&self, now: &DateTime<Utc>) -> bool {
        if now.weekday() != self.weekday {
            return false;
        }
        let t = now.time();
        if self.start_utc <= self.end_utc {
            self.start_utc <= t && t < self.end_utc
        } else {
            // Wraps midnight.
            t >= self.start_utc || t < self.end_utc
        }
    }
}

/// Live sensor readings — fed in by the supervisor before each `decide()` call.
#[derive(Debug, Clone, Copy, Serialize, Deserialize)]
pub struct SensorSnapshot {
    /// GB used this billing window.
    pub bandwidth_used_gb: u64,
    /// Bytes used this billing window (high-precision counter).
    pub bandwidth_used_bytes: u64,
    /// CPU usage percent (instantaneous).
    pub cpu_used_pct: u8,
    /// Memory usage percent (instantaneous).
    pub memory_used_pct: u8,
    /// Seconds since the user was last active (mouse / kbd / etc.).
    pub idle_secs: u64,
}

impl Default for SensorSnapshot {
    fn default() -> Self {
        Self {
            bandwidth_used_gb: 0,
            bandwidth_used_bytes: 0,
            cpu_used_pct: 0,
            memory_used_pct: 0,
            idle_secs: u64::MAX,
        }
    }
}

/// Decide the current scheduler state given config + sensors + wall-clock.
pub fn decide(cfg: &SchedulerConfig, sensors: SensorSnapshot, now: DateTime<Utc>) -> State {
    if sensors.bandwidth_used_gb >= cfg.bandwidth_cap_gb {
        return State::Paused(PauseReason::BandwidthCapReached);
    }
    if sensors.cpu_used_pct >= cfg.cpu_cap_pct {
        return State::Paused(PauseReason::CpuCapReached);
    }
    if sensors.memory_used_pct >= cfg.memory_cap_pct {
        return State::Paused(PauseReason::MemoryCapReached);
    }
    if !cfg.calendar.is_empty() && !cfg.calendar.iter().any(|w| w.contains(&now)) {
        return State::Paused(PauseReason::OutsideCalendarWindow);
    }
    if cfg.idle_only && sensors.idle_secs < cfg.idle_threshold_secs {
        return State::Paused(PauseReason::UserActive);
    }
    State::Active
}

/// Provides the current "user has been idle this many seconds" reading.
///
/// Implemented by the platform crates so the scheduler is portable. The
/// no-op default returns `u64::MAX` (effectively "always idle") — useful
/// in tests and on headless servers.
pub trait IdleSource: Send + Sync + 'static {
    /// Current idle seconds.
    fn idle_seconds(&self) -> u64;
}

/// No-op idle source — used in tests and on headless servers.
#[derive(Debug, Default, Clone, Copy)]
pub struct AlwaysIdle;

impl IdleSource for AlwaysIdle {
    fn idle_seconds(&self) -> u64 {
        u64::MAX
    }
}

#[derive(Debug, Default)]
struct Shared {
    cfg: SchedulerConfig,
    sensors: SensorSnapshot,
    state: Option<State>,
    /// Total bandwidth used this billing window (bytes, monotonic — resets on
    /// the 1st of the month). The supervisor calls [`record_bytes`] from the
    /// routing tap.
    bandwidth_used_bytes: u64,
    /// UTC of the start of the current billing window.
    billing_window_start: Option<DateTime<Utc>>,
    /// Manual operator pause.
    manual_pause: bool,
    /// Coordinator-pushed pause.
    operations_pause: bool,
}

/// Live, shared scheduler handle. Cheap to clone (Arc inside).
#[derive(Debug, Clone, Default)]
pub struct SchedulerHandle {
    inner: Arc<RwLock<Shared>>,
}

impl SchedulerHandle {
    /// New handle with the given config.
    pub fn new(cfg: SchedulerConfig) -> Self {
        let h = Self::default();
        h.set_config(cfg);
        // Initialise the billing window to start of current UTC month.
        let now = Utc::now();
        let start = chrono::TimeZone::with_ymd_and_hms(&Utc, now.year(), now.month(), 1, 0, 0, 0)
            .single()
            .unwrap_or(now);
        h.inner.write().billing_window_start = Some(start);
        h
    }

    /// Replace the config atomically.
    pub fn set_config(&self, cfg: SchedulerConfig) {
        self.inner.write().cfg = cfg;
    }

    /// Read the config.
    pub fn config(&self) -> SchedulerConfig {
        self.inner.read().cfg.clone()
    }

    /// Pause / resume by operator.
    pub fn set_manual_pause(&self, paused: bool) {
        self.inner.write().manual_pause = paused;
    }

    /// Pause / resume from coordinator.
    pub fn set_operations_pause(&self, paused: bool) {
        self.inner.write().operations_pause = paused;
    }

    /// Record bytes that just flowed through the routing tap. Resets at the
    /// start of each calendar month.
    pub fn record_bytes(&self, bytes: u64) {
        let now = Utc::now();
        let mut g = self.inner.write();
        let needs_reset = match g.billing_window_start {
            Some(start) => start.month() != now.month() || start.year() != now.year(),
            None => true,
        };
        if needs_reset {
            g.bandwidth_used_bytes = 0;
            g.billing_window_start =
                chrono::TimeZone::with_ymd_and_hms(&Utc, now.year(), now.month(), 1, 0, 0, 0)
                    .single();
        }
        g.bandwidth_used_bytes = g.bandwidth_used_bytes.saturating_add(bytes);
        g.sensors.bandwidth_used_bytes = g.bandwidth_used_bytes;
        g.sensors.bandwidth_used_gb = g.bandwidth_used_bytes / 1_000_000_000;
    }

    /// Current sensors snapshot.
    pub fn sensors(&self) -> SensorSnapshot {
        self.inner.read().sensors
    }

    /// Replace the sensor reading wholesale (used by the polling task).
    pub fn set_sensors(&self, sensors: SensorSnapshot) {
        let mut g = self.inner.write();
        // Keep the bandwidth-bytes counter — only allow overwriting CPU/MEM/idle.
        let bytes = g.bandwidth_used_bytes;
        g.sensors = SensorSnapshot {
            bandwidth_used_bytes: bytes,
            bandwidth_used_gb: bytes / 1_000_000_000,
            ..sensors
        };
    }

    /// Decide the current state given the latest sensors + manual flags.
    pub fn current(&self) -> State {
        let g = self.inner.read();
        if g.operations_pause {
            return State::Paused(PauseReason::OperationsPause);
        }
        if g.manual_pause {
            return State::Paused(PauseReason::ManuallyPaused);
        }
        decide(&g.cfg, g.sensors, Utc::now())
    }

    /// Force-evaluate + cache the state (also returned).
    pub fn refresh(&self) -> State {
        let s = self.current();
        self.inner.write().state = Some(s.clone());
        s
    }
}

/// Spawn the 5-second polling task that updates sensors from sysinfo + idle source.
pub fn spawn_poller<I: IdleSource + 'static>(
    handle: SchedulerHandle,
    idle: I,
    interval: Duration,
) -> tokio::task::JoinHandle<()> {
    let idle = Arc::new(idle);
    tokio::spawn(async move {
        let mut sys = sysinfo::System::new();
        let mut ticker = tokio::time::interval(interval);
        loop {
            ticker.tick().await;
            sys.refresh_cpu_usage();
            sys.refresh_memory();
            let cpu = sys.global_cpu_usage().round().clamp(0.0, 100.0) as u8;
            let total = sys.total_memory().max(1);
            let used = sys.used_memory();
            let mem = ((used as f64 * 100.0 / total as f64).round() as i64).clamp(0, 100) as u8;
            let idle_secs = idle.idle_seconds();
            let prev = handle.sensors();
            handle.set_sensors(SensorSnapshot {
                bandwidth_used_gb: prev.bandwidth_used_gb,
                bandwidth_used_bytes: prev.bandwidth_used_bytes,
                cpu_used_pct: cpu,
                memory_used_pct: mem,
                idle_secs,
            });
            let _ = handle.refresh();
        }
    })
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn sensors(b: u64, c: u8, m: u8, i: u64) -> SensorSnapshot {
        SensorSnapshot {
            bandwidth_used_gb: b,
            bandwidth_used_bytes: b * 1_000_000_000,
            cpu_used_pct: c,
            memory_used_pct: m,
            idle_secs: i,
        }
    }

    #[test]
    fn bandwidth_cap_pauses() {
        let cfg = SchedulerConfig {
            bandwidth_cap_gb: 10,
            ..Default::default()
        };
        let now = Utc.with_ymd_and_hms(2026, 1, 1, 12, 0, 0).unwrap();
        let st = decide(&cfg, sensors(11, 0, 0, 99999), now);
        assert_eq!(st, State::Paused(PauseReason::BandwidthCapReached));
    }

    #[test]
    fn idle_only_pauses_when_user_active() {
        let cfg = SchedulerConfig {
            idle_only: true,
            idle_threshold_secs: 300,
            ..Default::default()
        };
        let now = Utc.with_ymd_and_hms(2026, 1, 1, 12, 0, 0).unwrap();
        let st = decide(&cfg, sensors(0, 0, 0, 10), now);
        assert_eq!(st, State::Paused(PauseReason::UserActive));
    }

    #[test]
    fn all_green_returns_active() {
        let cfg = SchedulerConfig::default();
        let now = Utc.with_ymd_and_hms(2026, 1, 1, 12, 0, 0).unwrap();
        let st = decide(&cfg, sensors(0, 0, 0, 99999), now);
        assert_eq!(st, State::Active);
    }

    #[test]
    fn outside_calendar_pauses() {
        let cfg = SchedulerConfig {
            idle_only: false,
            calendar: vec![CalendarWindow {
                weekday: Weekday::Mon,
                start_utc: NaiveTime::from_hms_opt(22, 0, 0).unwrap(),
                end_utc: NaiveTime::from_hms_opt(23, 0, 0).unwrap(),
            }],
            ..Default::default()
        };
        let now = Utc.with_ymd_and_hms(2026, 1, 1, 22, 30, 0).unwrap();
        let st = decide(&cfg, sensors(0, 0, 0, 0), now);
        assert_eq!(st, State::Paused(PauseReason::OutsideCalendarWindow));
    }

    #[test]
    fn handle_manual_pause_wins_over_active() {
        let h = SchedulerHandle::new(SchedulerConfig {
            idle_only: false,
            ..Default::default()
        });
        h.set_sensors(sensors(0, 0, 0, u64::MAX));
        assert_eq!(h.current(), State::Active);
        h.set_manual_pause(true);
        assert_eq!(h.current(), State::Paused(PauseReason::ManuallyPaused));
    }

    #[test]
    fn handle_operations_pause_overrides_manual() {
        let h = SchedulerHandle::new(SchedulerConfig {
            idle_only: false,
            ..Default::default()
        });
        h.set_manual_pause(true);
        h.set_operations_pause(true);
        assert_eq!(h.current(), State::Paused(PauseReason::OperationsPause));
    }

    #[test]
    fn record_bytes_accumulates_into_gb() {
        let h = SchedulerHandle::new(SchedulerConfig {
            bandwidth_cap_gb: 1,
            idle_only: false,
            ..Default::default()
        });
        h.record_bytes(500_000_000);
        h.record_bytes(600_000_000);
        let s = h.sensors();
        assert_eq!(s.bandwidth_used_gb, 1);
        assert!(s.bandwidth_used_bytes >= 1_100_000_000);
    }

    #[test]
    fn pause_reason_slugs_are_stable() {
        assert_eq!(PauseReason::BandwidthCapReached.slug(), "bandwidth_cap");
        assert_eq!(PauseReason::ManuallyPaused.slug(), "manually_paused");
    }
}
