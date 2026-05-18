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

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use chrono::{DateTime, Datelike, NaiveTime, Utc, Weekday};
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
    /// CPU usage percent (instantaneous).
    pub cpu_used_pct: u8,
    /// Memory usage percent (instantaneous).
    pub memory_used_pct: u8,
    /// Seconds since the user was last active (mouse / kbd / etc.).
    pub idle_secs: u64,
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

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    fn sensors(b: u64, c: u8, m: u8, i: u64) -> SensorSnapshot {
        SensorSnapshot {
            bandwidth_used_gb: b,
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
        // 2026-01-01 was a Thursday — not Monday — outside window.
        let now = Utc.with_ymd_and_hms(2026, 1, 1, 22, 30, 0).unwrap();
        let st = decide(&cfg, sensors(0, 0, 0, 0), now);
        assert_eq!(st, State::Paused(PauseReason::OutsideCalendarWindow));
    }
}
