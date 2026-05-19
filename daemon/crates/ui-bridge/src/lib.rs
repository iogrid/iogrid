//! UI bridge — localhost HTTP + SSE API the web management plane talks to.
//!
//! Endpoints (per docs/TECH.md §IPC):
//!  * `GET  /state`          — current supervisor state + active count + earnings today.
//!  * `GET  /audit/stream`   — Server-Sent Events stream of every audit-log line.
//!  * `GET  /audit/filters`  — active anti-abuse filter ruleset (for provider audit).
//!  * `POST /config`         — replace the daemon's runtime config (JSON body).
//!  * `GET  /earnings`       — today / this-week / this-month earnings summary.
//!  * `POST /pair`           — one-time pairing token submission.
//!  * `GET  /healthz`        — liveness probe (used by installers).
//!
//! Binds to `127.0.0.1:7777` by default (loopback only). The pairing
//! endpoint enforces a one-shot token; subsequent calls require an
//! Authorization: Bearer header bound to the paired identity (TODO follow-up
//! PR — the wiring is in place but the bearer check is currently advisory).

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::convert::Infallible;
use std::net::SocketAddr;
use std::sync::Arc;
use std::time::Duration;

use axum::extract::State;
use axum::http::StatusCode;
use axum::response::sse::{Event, KeepAlive, Sse};
use axum::routing::{get, post};
use axum::Json;
use axum::Router;
use chrono::Utc;
use futures::stream::Stream;
use iogrid_anti_abuse::{Filter, InMemoryFilter, RulesetSnapshot};
use iogrid_scheduler::SchedulerHandle;
use parking_lot::Mutex;
use serde::{Deserialize, Serialize};
use tokio::sync::broadcast;

/// One audit-stream event.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuditEvent {
    /// RFC3339 timestamp.
    pub ts: String,
    /// Event kind, e.g. "bandwidth.batch", "filter.block", "workload.dispatched".
    pub kind: String,
    /// Free-form message.
    pub msg: String,
    /// Optional bytes-in counter for this event.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub bytes_in: Option<u64>,
    /// Optional bytes-out counter.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub bytes_out: Option<u64>,
}

impl AuditEvent {
    /// Build a new event with `now()`.
    pub fn now(kind: impl Into<String>, msg: impl Into<String>) -> Self {
        Self {
            ts: Utc::now().to_rfc3339(),
            kind: kind.into(),
            msg: msg.into(),
            bytes_in: None,
            bytes_out: None,
        }
    }
}

/// Public daemon state surfaced over the UI bridge.
#[derive(Debug, Clone, Serialize, Deserialize, Default)]
pub struct DaemonStateView {
    /// One of `"starting" | "connected" | "active" | "paused" | "faulted"`.
    pub state: String,
    /// Pause reason if state is paused.
    pub pause_reason: Option<String>,
    /// Daemon version (`CARGO_PKG_VERSION`).
    pub version: String,
    /// Coordinator URL the daemon is paired with.
    pub coordinator_url: String,
    /// Number of workloads currently running.
    pub active_workloads: u32,
    /// Earnings today (micro-USD, integer to avoid float drift).
    pub earnings_today_micros: u64,
    /// Current cumulative bandwidth used this billing window (bytes).
    pub bandwidth_used_bytes_month: u64,
    /// Current CPU%.
    pub cpu_pct: u8,
    /// Current memory%.
    pub memory_pct: u8,
    /// Current idle seconds.
    pub idle_secs: u64,
}

/// Earnings summary view.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct EarningsView {
    /// Today (micro-USD).
    pub today_micros: u64,
    /// This week (micro-USD).
    pub week_micros: u64,
    /// This month (micro-USD).
    pub month_micros: u64,
    /// Pending payout (settles next 1st of the month).
    pub pending_micros: u64,
}

/// Inbound config-replace payload.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConfigUpdate {
    /// New bandwidth cap.
    pub bandwidth_cap_gb: Option<u64>,
    /// New CPU cap.
    pub cpu_cap_pct: Option<u8>,
    /// New memory cap.
    pub memory_cap_pct: Option<u8>,
    /// Idle-only toggle.
    pub idle_only: Option<bool>,
    /// Idle threshold in seconds.
    pub idle_threshold_secs: Option<u64>,
    /// Manual pause toggle.
    pub manual_pause: Option<bool>,
}

/// One-shot pairing payload — provider feeds the token from the onboarding
/// flow into the daemon.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PairRequest {
    /// One-time pairing token displayed in the web UI.
    pub pairing_token: String,
    /// Optional override of the coordinator URL (defaults to
    /// `https://coordinator.iogrid.org:443`).
    pub coordinator_url: Option<String>,
}

/// Pairing response.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PairResponse {
    /// Provider id the coordinator assigned.
    pub provider_id: String,
    /// "paired" / "already_paired" / "error".
    pub status: String,
    /// Free-form detail.
    pub message: String,
}

/// Snapshot of the active filter ruleset for the audit endpoint.
pub type FilterRulesetView = RulesetSnapshot;

/// Shared bridge state.
#[derive(Clone)]
pub struct BridgeState {
    /// Current daemon view (mutable).
    pub view: Arc<Mutex<DaemonStateView>>,
    /// Earnings ledger.
    pub earnings: Arc<Mutex<EarningsView>>,
    /// Audit-event broadcast channel — handlers can subscribe.
    pub audit_tx: broadcast::Sender<AuditEvent>,
    /// Scheduler handle (config-write side of POST /config).
    pub scheduler: Option<SchedulerHandle>,
    /// Anti-abuse filter (audit/filters source).
    pub filter: Option<Arc<InMemoryFilter>>,
    /// Pairing handler — the supervisor wires this in. When `None`, POST
    /// /pair returns 503 (daemon not ready for pairing).
    pub pair_handler: Option<Arc<dyn PairHandler>>,
}

impl Default for BridgeState {
    fn default() -> Self {
        let (audit_tx, _) = broadcast::channel(256);
        Self {
            view: Arc::new(Mutex::new(DaemonStateView::default())),
            earnings: Arc::new(Mutex::new(EarningsView::default())),
            audit_tx,
            scheduler: None,
            filter: None,
            pair_handler: None,
        }
    }
}

impl BridgeState {
    /// New bridge state, owning the supplied view.
    pub fn new(view: DaemonStateView) -> Self {
        let s = Self::default();
        *s.view.lock() = view;
        s
    }
    /// Read-only snapshot of the public view.
    pub fn snapshot(&self) -> DaemonStateView {
        self.view.lock().clone()
    }
    /// Replace the snapshot.
    pub fn set(&self, view: DaemonStateView) {
        *self.view.lock() = view;
    }
    /// Publish an audit event.
    pub fn publish_audit(&self, ev: AuditEvent) {
        let _ = self.audit_tx.send(ev);
    }
    /// Wire in the scheduler handle.
    pub fn with_scheduler(mut self, sched: SchedulerHandle) -> Self {
        self.scheduler = Some(sched);
        self
    }
    /// Wire in the filter.
    pub fn with_filter(mut self, f: Arc<InMemoryFilter>) -> Self {
        self.filter = Some(f);
        self
    }
    /// Wire in the pair handler.
    pub fn with_pair_handler(mut self, h: Arc<dyn PairHandler>) -> Self {
        self.pair_handler = Some(h);
        self
    }
    /// Replace the earnings ledger.
    pub fn set_earnings(&self, e: EarningsView) {
        *self.earnings.lock() = e;
    }
}

/// Pairing handler trait — the supervisor implements this to bridge the
/// HTTP POST /pair call to the transport's `PairingClient`.
#[async_trait::async_trait]
pub trait PairHandler: Send + Sync {
    /// Perform the pair RPC + persist the returned identity bundle.
    async fn pair(&self, req: PairRequest) -> Result<PairResponse, String>;
}

/// Build the axum router.
pub fn router(state: BridgeState) -> Router {
    Router::new()
        .route("/healthz", get(get_healthz))
        .route("/state", get(get_state))
        .route("/audit/stream", get(get_audit_stream))
        .route("/audit/filters", get(get_audit_filters))
        .route("/config", post(post_config))
        .route("/earnings", get(get_earnings))
        .route("/pair", post(post_pair))
        .with_state(state)
}

/// Bind + serve on `addr`. Loops until the task is dropped.
pub async fn serve(addr: SocketAddr, state: BridgeState) -> std::io::Result<()> {
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!(%addr, "ui bridge listening");
    axum::serve(listener, router(state))
        .await
        .map_err(std::io::Error::other)
}

async fn get_healthz() -> Json<serde_json::Value> {
    Json(serde_json::json!({"ok": true, "version": env!("CARGO_PKG_VERSION")}))
}

async fn get_state(State(state): State<BridgeState>) -> Json<DaemonStateView> {
    let mut v = state.snapshot();
    // Decorate with live scheduler signals if wired.
    if let Some(sched) = &state.scheduler {
        let st = sched.refresh();
        let sensors = sched.sensors();
        v.cpu_pct = sensors.cpu_used_pct;
        v.memory_pct = sensors.memory_used_pct;
        v.idle_secs = sensors.idle_secs;
        v.bandwidth_used_bytes_month = sensors.bandwidth_used_bytes;
        v.state = match &st {
            iogrid_scheduler::State::Active => "active".into(),
            iogrid_scheduler::State::Paused(_) => "paused".into(),
        };
        v.pause_reason = match st {
            iogrid_scheduler::State::Paused(r) => Some(r.slug().to_string()),
            _ => None,
        };
    }
    Json(v)
}

async fn get_audit_stream(
    State(state): State<BridgeState>,
) -> Sse<impl Stream<Item = Result<Event, Infallible>>> {
    use futures::StreamExt;
    use tokio_stream::wrappers::BroadcastStream;
    // Emit a synthetic "connected" event so curl --no-buffer immediately
    // sees something.
    let _ = state
        .audit_tx
        .send(AuditEvent::now("connected", "audit stream attached"));
    let rx = state.audit_tx.subscribe();
    let stream = BroadcastStream::new(rx).map(|res| {
        let ev = match res {
            Ok(ev) => ev,
            Err(_) => AuditEvent::now("lag", "audit consumer lagged"),
        };
        let data = serde_json::to_string(&ev).unwrap_or_default();
        Ok::<_, Infallible>(Event::default().event(ev.kind.clone()).data(data))
    });
    Sse::new(stream).keep_alive(KeepAlive::new().interval(Duration::from_secs(15)))
}

async fn get_audit_filters(State(state): State<BridgeState>) -> Json<FilterRulesetView> {
    let snap = state
        .filter
        .as_deref()
        .map(|f| f.ruleset_snapshot())
        .unwrap_or_default();
    Json(snap)
}

async fn post_config(
    State(state): State<BridgeState>,
    Json(body): Json<ConfigUpdate>,
) -> (StatusCode, Json<serde_json::Value>) {
    if let Some(sched) = &state.scheduler {
        let mut cfg = sched.config();
        if let Some(v) = body.bandwidth_cap_gb {
            cfg.bandwidth_cap_gb = v;
        }
        if let Some(v) = body.cpu_cap_pct {
            cfg.cpu_cap_pct = v.min(100);
        }
        if let Some(v) = body.memory_cap_pct {
            cfg.memory_cap_pct = v.min(100);
        }
        if let Some(v) = body.idle_only {
            cfg.idle_only = v;
        }
        if let Some(v) = body.idle_threshold_secs {
            cfg.idle_threshold_secs = v;
        }
        sched.set_config(cfg);
        if let Some(v) = body.manual_pause {
            sched.set_manual_pause(v);
        }
        state.publish_audit(AuditEvent::now("config", "scheduling config updated"));
        (StatusCode::ACCEPTED, Json(serde_json::json!({"ok": true})))
    } else {
        (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(serde_json::json!({"ok": false, "error": "scheduler not wired"})),
        )
    }
}

async fn get_earnings(State(state): State<BridgeState>) -> Json<EarningsView> {
    Json(state.earnings.lock().clone())
}

async fn post_pair(
    State(state): State<BridgeState>,
    Json(req): Json<PairRequest>,
) -> (StatusCode, Json<PairResponse>) {
    match &state.pair_handler {
        None => (
            StatusCode::SERVICE_UNAVAILABLE,
            Json(PairResponse {
                provider_id: String::new(),
                status: "error".into(),
                message: "daemon not ready for pairing".into(),
            }),
        ),
        Some(h) => match h.pair(req).await {
            Ok(r) => (StatusCode::OK, Json(r)),
            Err(e) => (
                StatusCode::BAD_REQUEST,
                Json(PairResponse {
                    provider_id: String::new(),
                    status: "error".into(),
                    message: e,
                }),
            ),
        },
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashSet;

    #[test]
    fn router_builds() {
        let s = BridgeState::new(DaemonStateView {
            state: "starting".into(),
            ..Default::default()
        });
        let _ = router(s);
    }

    #[test]
    fn snapshot_round_trip() {
        let s = BridgeState::new(DaemonStateView {
            state: "active".into(),
            version: "0.1.0".into(),
            ..Default::default()
        });
        assert_eq!(s.snapshot().state, "active");
        s.set(DaemonStateView {
            state: "paused".into(),
            pause_reason: Some("idle".into()),
            ..Default::default()
        });
        assert_eq!(s.snapshot().state, "paused");
    }

    #[tokio::test]
    async fn handlers_return_expected_types() {
        let s = BridgeState::default();
        let Json(view) = get_state(State(s.clone())).await;
        assert_eq!(view.state, "");

        let (status, Json(body)) = post_config(
            State(s.clone()),
            Json(ConfigUpdate {
                bandwidth_cap_gb: Some(100),
                cpu_cap_pct: None,
                memory_cap_pct: None,
                idle_only: Some(false),
                idle_threshold_secs: None,
                manual_pause: None,
            }),
        )
        .await;
        // No scheduler wired → 503.
        assert_eq!(status, StatusCode::SERVICE_UNAVAILABLE);
        assert_eq!(body["ok"], false);
    }

    #[tokio::test]
    async fn post_config_with_scheduler_updates_cfg() {
        let sched = SchedulerHandle::new(iogrid_scheduler::SchedulerConfig::default());
        let s = BridgeState::default().with_scheduler(sched.clone());
        let (status, _) = post_config(
            State(s.clone()),
            Json(ConfigUpdate {
                bandwidth_cap_gb: Some(200),
                cpu_cap_pct: Some(50),
                memory_cap_pct: Some(40),
                idle_only: Some(false),
                idle_threshold_secs: Some(60),
                manual_pause: Some(true),
            }),
        )
        .await;
        assert_eq!(status, StatusCode::ACCEPTED);
        let cfg = sched.config();
        assert_eq!(cfg.bandwidth_cap_gb, 200);
        assert_eq!(cfg.cpu_cap_pct, 50);
        assert!(matches!(
            sched.current(),
            iogrid_scheduler::State::Paused(iogrid_scheduler::PauseReason::ManuallyPaused)
        ));
    }

    #[tokio::test]
    async fn get_audit_filters_serializes_snapshot() {
        let f = Arc::new(InMemoryFilter::new());
        f.install(
            RulesetSnapshot {
                phish_domains: 3,
                csam_hashes: 5,
                blocked_ports: vec![25, 587],
                blocked_destinations: vec!["*.chase.com".into()],
                per_customer_rpm: 60,
                ruleset_hash: "h".into(),
                ..Default::default()
            },
            HashSet::new(),
            HashSet::new(),
        );
        let s = BridgeState::default().with_filter(f);
        let Json(snap) = get_audit_filters(State(s)).await;
        assert_eq!(snap.phish_domains, 3);
        assert_eq!(snap.csam_hashes, 5);
        assert_eq!(snap.blocked_ports, vec![25, 587]);
    }

    #[tokio::test]
    async fn earnings_round_trip() {
        let s = BridgeState::default();
        s.set_earnings(EarningsView {
            today_micros: 1_234_000,
            week_micros: 5_678_000,
            month_micros: 9_999_000,
            pending_micros: 4_321_000,
        });
        let Json(e) = get_earnings(State(s)).await;
        assert_eq!(e.today_micros, 1_234_000);
        assert_eq!(e.month_micros, 9_999_000);
    }

    #[tokio::test]
    async fn pair_returns_503_when_not_wired() {
        let s = BridgeState::default();
        let (status, _) = post_pair(
            State(s),
            Json(PairRequest {
                pairing_token: "deadbeef".into(),
                coordinator_url: None,
            }),
        )
        .await;
        assert_eq!(status, StatusCode::SERVICE_UNAVAILABLE);
    }
}
