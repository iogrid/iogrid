//! UI bridge — localhost HTTP + SSE API the web management plane talks to.
//!
//! Endpoints (per docs/TECH.md §IPC):
//!  * `GET /state` — current supervisor state snapshot (JSON).
//!  * `GET /audit` — Server-Sent Events stream of every audit-log line.
//!  * `POST /config` — replace the daemon's runtime config (JSON body).
//!
//! Binds to `127.0.0.1:7777` by default (loopback only). mTLS pairing code
//! is enforced by middleware (not yet in the scaffold).

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
use futures::stream::{self, Stream};
use serde::{Deserialize, Serialize};

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
}

/// Shared state the axum router carries.
#[derive(Clone, Default)]
pub struct BridgeState {
    inner: Arc<parking_lot_compat::Mutex<DaemonStateView>>,
}

impl BridgeState {
    /// New bridge state, owning the supplied view.
    pub fn new(view: DaemonStateView) -> Self {
        Self {
            inner: Arc::new(parking_lot_compat::Mutex::new(view)),
        }
    }
    /// Read-only snapshot.
    pub fn snapshot(&self) -> DaemonStateView {
        self.inner.lock().clone()
    }
    /// Replace the snapshot.
    pub fn set(&self, view: DaemonStateView) {
        *self.inner.lock() = view;
    }
}

/// Build the axum router. Hand the result to `axum::serve` to run.
pub fn router(state: BridgeState) -> Router {
    Router::new()
        .route("/state", get(get_state))
        .route("/audit", get(get_audit))
        .route("/config", post(post_config))
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

async fn get_state(State(state): State<BridgeState>) -> Json<DaemonStateView> {
    Json(state.snapshot())
}

async fn get_audit(
    State(_state): State<BridgeState>,
) -> Sse<impl Stream<Item = Result<Event, Infallible>>> {
    // Scaffold: emit one synthetic line then keep alive.
    let s = stream::once(async {
        Ok::<_, Infallible>(
            Event::default().data(
                serde_json::to_string(&serde_json::json!({
                    "ts": chrono::Utc::now().to_rfc3339(),
                    "kind": "scaffold",
                    "msg": "audit stream connected"
                }))
                .unwrap(),
            ),
        )
    });
    Sse::new(s).keep_alive(KeepAlive::new().interval(Duration::from_secs(15)))
}

async fn post_config(
    State(_state): State<BridgeState>,
    Json(_body): Json<ConfigUpdate>,
) -> (StatusCode, Json<serde_json::Value>) {
    // Real impl: validate, atomically swap config, persist to disk.
    (StatusCode::ACCEPTED, Json(serde_json::json!({"ok": true})))
}

// ---------------------------------------------------------------------------
// Tiny private mutex shim so we don't add a parking_lot dep just for `lock()`
// — `std::sync::Mutex` works fine inside async handlers because we never
// hold the guard across an await point.
// ---------------------------------------------------------------------------
mod parking_lot_compat {
    pub struct Mutex<T>(std::sync::Mutex<T>);

    impl<T> Mutex<T> {
        pub fn new(t: T) -> Self {
            Self(std::sync::Mutex::new(t))
        }
        pub fn lock(&self) -> std::sync::MutexGuard<'_, T> {
            self.0.lock().expect("ui bridge state mutex poisoned")
        }
    }

    impl<T: Default> Default for Mutex<T> {
        fn default() -> Self {
            Self::new(T::default())
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

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
        let s = BridgeState::new(DaemonStateView::default());
        let Json(view) = get_state(State(s.clone())).await;
        assert_eq!(view.state, "");

        let (status, Json(body)) = post_config(
            State(s),
            Json(ConfigUpdate {
                bandwidth_cap_gb: Some(100),
                cpu_cap_pct: None,
                memory_cap_pct: None,
                idle_only: Some(false),
            }),
        )
        .await;
        assert_eq!(status, StatusCode::ACCEPTED);
        assert_eq!(body["ok"], true);
    }
}
