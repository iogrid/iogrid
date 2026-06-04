//! Provider health probe + graceful shutdown reporter — VPN-7 (#511).
//!
//! Companion to [`crate::ice`]. The ICE module tells vpn-svc *how* to
//! reach a provider; this one tells vpn-svc *whether* the provider is
//! reachable at all.
//!
//! Two responsibilities:
//!
//! 1. **Periodic health POST** (every 15 s) — keeps the provider's
//!    `Status` row at `healthy` and bumps `last_seen_at`. Falls
//!    silently into degraded/offline at the coordinator if the daemon
//!    stops checking in (failover store methods consult
//!    `last_seen_at` as the staleness signal — see VPN-4).
//!
//! 2. **Graceful shutdown notify** — on SIGTERM the daemon emits one
//!    final POST flipping status to `offline` with a 3 s budget, so
//!    the customer SDK's failover detector (VPN-11) can re-route
//!    active sessions before the next 15 s tick would otherwise
//!    expire. Best-effort: if vpn-svc is unreachable we still exit.
//!
//! The wire JSON is hand-rolled (no proto type for it yet) — matches
//! what the new vpn-svc handlers `POST /v1/vpn/providers/{id}/health`
//! and `POST /v1/vpn/providers/{id}/offline` accept.

use std::net::SocketAddr;
use std::time::Duration;

use serde::Serialize;

/// How often the reporter re-publishes a healthy heartbeat.
///
/// The failover store consults `last_seen_at` plus a 30 s stale
/// threshold (set by VPN-4); 15 s here gives one missed-tick of slack
/// before a stuck-but-not-crashed daemon is failed over.
pub const HEALTH_INTERVAL: Duration = Duration::from_secs(15);

/// Wall-clock budget for the final offline POST during shutdown. The
/// daemon must exit promptly even when vpn-svc is the thing that's
/// down; this caps the wait.
pub const SHUTDOWN_BUDGET: Duration = Duration::from_secs(3);

/// Per-tick HTTP request timeout. Short enough that a hung coordinator
/// doesn't stall the reporter into Skipping ticks indefinitely.
pub const TICK_TIMEOUT: Duration = Duration::from_secs(5);

/// Health status the daemon publishes for itself. The string form is
/// the wire encoding — vpn-svc's `UpdateProviderHealth` accepts the
/// same three strings, matching the provider `Status` column in the
/// failover store.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize)]
#[serde(rename_all = "lowercase")]
pub enum HealthStatus {
    /// Provider is fully serving — accept new sessions, keep existing
    /// ones routed here.
    Healthy,
    /// Provider is up but should be deprioritised — failover store
    /// keeps it as a fallback only.
    Degraded,
    /// Provider is going away. Customer SDKs should fail over.
    Offline,
}

impl HealthStatus {
    /// String form as sent on the wire + stored in the failover table.
    pub fn as_wire(self) -> &'static str {
        match self {
            HealthStatus::Healthy => "healthy",
            HealthStatus::Degraded => "degraded",
            HealthStatus::Offline => "offline",
        }
    }
}

/// Configuration for the health reporter task.
#[derive(Debug, Clone)]
pub struct HealthConfig {
    /// Provider UUID — appears in the URL.
    pub provider_id: String,
    /// Region slug (e.g. `us-east-1`). vpn-svc's failover store keys
    /// on this when picking alternate providers; it's the body of the
    /// `/register` POST that this module fires at supervisor startup
    /// before the first `/health` heartbeat.
    pub region: String,
    /// vpn-svc base URL (e.g. `https://api.iogrid.org`). The reporter
    /// appends `/v1/vpn/providers/{provider_id}/register`,
    /// `.../health`, and `.../offline`.
    pub vpn_svc_base_url: String,
    /// The VPN listener address — included in the heartbeat body for
    /// debugging (vpn-svc currently ignores it but logs are easier
    /// when the report carries the endpoint).
    pub vpn_listen_addr: SocketAddr,
    /// The daemon's static WireGuard public key (base64). Sent in the
    /// `/register` body so vpn-svc can hand it to customers as the WG
    /// peer key (#570 / #696 mobile bring-up). Without it the mobile
    /// session handler returns an empty `peer_public_key` and the
    /// customer's tunnel can't complete a handshake.
    pub wg_public_key: String,
}

/// Wire body for `POST /v1/vpn/providers/{id}/health`.
#[derive(Debug, Clone, Serialize)]
pub struct HealthReport {
    /// Provider UUID, echoed so vpn-svc can sanity-check the route id.
    pub provider_id: String,
    /// Current status. Always `healthy` from the periodic ticker;
    /// `offline` is sent through [`notify_offline`] separately so the
    /// receiver can short-circuit failover.
    pub status: HealthStatus,
    /// Wall-clock at the moment of report.
    pub at_unix_ms: i64,
    /// VPN UDP listener — debug-only, vpn-svc may use it for staleness
    /// vs candidate-row reconciliation.
    pub vpn_listen_addr: String,
}

/// Wire body for `POST /v1/vpn/providers/{id}/register`. The handler
/// upserts a `{status: healthy, last_seen_at: now}` row keyed on the
/// route parameter, using `region` to bucket the provider for the
/// failover store. Idempotent — re-registering preserves SessionCount.
#[derive(Debug, Clone, Serialize)]
pub struct RegisterReport {
    /// Region slug the daemon advertises.
    pub region: String,
    /// The daemon's static WireGuard public key (base64). vpn-svc surfaces
    /// this to customers as the provider's WG peer key (#570); the mobile
    /// session handler returns an empty `peer_public_key` without it, so
    /// the customer's tunnel can't handshake (#696).
    pub wg_public_key: String,
}

/// Wire body for `POST /v1/vpn/providers/{id}/offline`.
#[derive(Debug, Clone, Serialize)]
pub struct OfflineReport {
    /// Provider UUID, echoed for route sanity.
    pub provider_id: String,
    /// Wall-clock at the moment of shutdown.
    pub at_unix_ms: i64,
    /// Optional short reason — "sigterm", "sigint", "manual_stop".
    /// Helps post-mortem dashboards distinguish operator action from
    /// crash-restart loops.
    pub reason: String,
}

/// Spawn the health reporter task. The task:
///
///  * Immediately fires one healthy POST so a freshly-paired daemon
///    flips its row to `healthy` without waiting [`HEALTH_INTERVAL`].
///  * Re-POSTs every [`HEALTH_INTERVAL`] thereafter.
///  * Watches `shutdown_rx` — when it flips to `true`, sends one final
///    offline POST with reason `sigterm` (capped at [`SHUTDOWN_BUDGET`])
///    and exits.
///
/// HTTP errors are logged WARN and the loop continues — coordinator
/// unreachable never crashes the daemon.
pub fn spawn_reporter(
    config: HealthConfig,
    http: reqwest::Client,
    shutdown_rx: tokio::sync::watch::Receiver<bool>,
) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        run_health_loop(config, http, shutdown_rx).await;
    })
}

async fn run_health_loop(
    config: HealthConfig,
    http: reqwest::Client,
    mut shutdown_rx: tokio::sync::watch::Receiver<bool>,
) {
    // Register before the heartbeat loop so vpn-svc's
    // `UpdateProviderHealth` UPDATE has a row to hit — otherwise the
    // first `/health` POST 404s on a freshly-paired daemon. The
    // handler is idempotent (preserves SessionCount per Store
    // contract) so a restart-after-pair stays clean. Best-effort:
    // failure here logs WARN and the loop continues — the operator
    // can restart the daemon once vpn-svc is reachable.
    if let Err(e) = register_provider(&config, &http).await {
        tracing::warn!(error = %e, "register provider failed; first /health POST may 404 until next daemon restart");
    }

    let health_url = format!(
        "{}/v1/vpn/providers/{}/health",
        config.vpn_svc_base_url.trim_end_matches('/'),
        config.provider_id
    );
    let mut ticker = tokio::time::interval(HEALTH_INTERVAL);
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    loop {
        tokio::select! {
            biased;
            // Shutdown takes priority — if both fire on the same poll,
            // we want to send offline rather than another healthy.
            res = shutdown_rx.changed() => {
                if res.is_err() {
                    // Sender dropped — supervisor is going away. Treat
                    // the same as shutdown.
                    tracing::debug!("health reporter shutdown sender dropped");
                }
                if *shutdown_rx.borrow() || res.is_err() {
                    let _ = notify_offline(&config, &http, "sigterm").await;
                    return;
                }
            }
            _ = ticker.tick() => {
                let report = HealthReport {
                    provider_id: config.provider_id.clone(),
                    status: HealthStatus::Healthy,
                    at_unix_ms: now_unix_ms(),
                    vpn_listen_addr: config.vpn_listen_addr.to_string(),
                };
                let post = http.post(&health_url).json(&report).send();
                match tokio::time::timeout(TICK_TIMEOUT, post).await {
                    Ok(Ok(resp)) if resp.status().is_success() => {
                        tracing::debug!(
                            status = %resp.status(),
                            "health heartbeat published"
                        );
                    }
                    Ok(Ok(resp)) => {
                        tracing::warn!(
                            status = %resp.status(),
                            url = %health_url,
                            "vpn-svc rejected health heartbeat"
                        );
                    }
                    Ok(Err(e)) => {
                        tracing::warn!(error = %e, "health heartbeat POST failed; will retry next tick");
                    }
                    Err(_) => {
                        tracing::warn!(
                            timeout_ms = TICK_TIMEOUT.as_millis(),
                            "health heartbeat POST timed out; will retry next tick"
                        );
                    }
                }
            }
        }
    }
}

/// Register (or idempotently re-register) the provider with vpn-svc's
/// failover store. The corresponding handler upserts a row keyed on
/// `config.provider_id` with `region`, `status=healthy`, and
/// `last_seen_at=now`; existing rows preserve `session_count`.
///
/// Called once at the top of [`run_health_loop`] before the periodic
/// `/health` heartbeat — without this seed the first health POST would
/// 404 because `UpdateProviderHealth` only UPDATEs existing rows.
/// Capped at [`TICK_TIMEOUT`] so a hung coordinator doesn't stall
/// daemon boot.
pub async fn register_provider(
    config: &HealthConfig,
    http: &reqwest::Client,
) -> Result<(), HealthError> {
    let url = format!(
        "{}/v1/vpn/providers/{}/register",
        config.vpn_svc_base_url.trim_end_matches('/'),
        config.provider_id
    );
    let body = RegisterReport {
        region: config.region.clone(),
        wg_public_key: config.wg_public_key.clone(),
    };
    let post = http.post(&url).json(&body).send();
    let resp = match tokio::time::timeout(TICK_TIMEOUT, post).await {
        Ok(Ok(r)) => r,
        Ok(Err(e)) => return Err(HealthError::HttpPost(e)),
        Err(_) => return Err(HealthError::Timeout),
    };
    if !resp.status().is_success() {
        return Err(HealthError::BadStatus(resp.status().as_u16()));
    }
    tracing::info!(
        provider_id = %config.provider_id,
        region = %config.region,
        "provider registered with vpn-svc"
    );
    Ok(())
}

/// Send one offline POST. Best-effort — caller should not block longer
/// than [`SHUTDOWN_BUDGET`] on this. Returns `Ok(())` only when
/// vpn-svc accepted the request with a 2xx; any other outcome is
/// `Err`. Callers in the daemon's shutdown path use the boolean
/// "tried and either succeeded or hit budget" semantic rather than
/// branching on the error — once we're shutting down we exit either
/// way.
pub async fn notify_offline(
    config: &HealthConfig,
    http: &reqwest::Client,
    reason: &str,
) -> Result<(), HealthError> {
    let url = format!(
        "{}/v1/vpn/providers/{}/offline",
        config.vpn_svc_base_url.trim_end_matches('/'),
        config.provider_id
    );
    let body = OfflineReport {
        provider_id: config.provider_id.clone(),
        at_unix_ms: now_unix_ms(),
        reason: reason.to_owned(),
    };
    let post = http.post(&url).json(&body).send();
    let resp = match tokio::time::timeout(SHUTDOWN_BUDGET, post).await {
        Ok(Ok(r)) => r,
        Ok(Err(e)) => {
            tracing::warn!(error = %e, url = %url, "graceful offline POST failed");
            return Err(HealthError::HttpPost(e));
        }
        Err(_) => {
            tracing::warn!(
                budget_ms = SHUTDOWN_BUDGET.as_millis(),
                "graceful offline POST exceeded shutdown budget"
            );
            return Err(HealthError::Timeout);
        }
    };
    if !resp.status().is_success() {
        tracing::warn!(status = %resp.status(), url = %url, "vpn-svc rejected offline POST");
        return Err(HealthError::BadStatus(resp.status().as_u16()));
    }
    tracing::info!(provider_id = %config.provider_id, reason, "provider marked offline at coordinator");
    Ok(())
}

/// Errors emitted by the health reporter's one-shot calls. The
/// periodic loop swallows these internally; only [`notify_offline`]
/// surfaces them so the shutdown path can log the cause if it cares.
#[derive(Debug, thiserror::Error)]
pub enum HealthError {
    /// HTTP send-side I/O / TLS / connect error.
    #[error("vpn-svc post error: {0}")]
    HttpPost(#[source] reqwest::Error),
    /// vpn-svc responded but with a non-2xx status.
    #[error("vpn-svc returned status {0}")]
    BadStatus(u16),
    /// We exceeded [`SHUTDOWN_BUDGET`] while waiting for the offline
    /// response.
    #[error("offline notification exceeded shutdown budget")]
    Timeout,
}

fn now_unix_ms() -> i64 {
    use std::time::{SystemTime, UNIX_EPOCH};
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_millis() as i64)
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::atomic::{AtomicUsize, Ordering};
    use std::sync::Arc;
    use tokio::sync::watch;

    fn test_config(base: String) -> HealthConfig {
        HealthConfig {
            provider_id: "11111111-2222-3333-4444-555555555555".into(),
            region: "us-east-1".into(),
            vpn_svc_base_url: base,
            vpn_listen_addr: "127.0.0.1:51820".parse().unwrap(),
            wg_public_key: "TESTWGKEY0000000000000000000000000000000000=".into(),
        }
    }

    #[test]
    fn status_serializes_to_lowercase_wire_form() {
        assert_eq!(HealthStatus::Healthy.as_wire(), "healthy");
        assert_eq!(HealthStatus::Degraded.as_wire(), "degraded");
        assert_eq!(HealthStatus::Offline.as_wire(), "offline");
        let json = serde_json::to_string(&HealthStatus::Healthy).unwrap();
        assert_eq!(json, "\"healthy\"");
    }

    #[test]
    fn health_report_uses_snake_case() {
        let r = HealthReport {
            provider_id: "p1".into(),
            status: HealthStatus::Healthy,
            at_unix_ms: 1_700_000_000_000,
            vpn_listen_addr: "1.2.3.4:51820".into(),
        };
        let json = serde_json::to_string(&r).unwrap();
        assert!(json.contains("\"provider_id\":\"p1\""));
        assert!(json.contains("\"status\":\"healthy\""));
        assert!(json.contains("\"at_unix_ms\":1700000000000"));
        assert!(json.contains("\"vpn_listen_addr\":\"1.2.3.4:51820\""));
    }

    #[test]
    fn offline_report_uses_snake_case() {
        let r = OfflineReport {
            provider_id: "p1".into(),
            at_unix_ms: 1_700_000_000_000,
            reason: "sigterm".into(),
        };
        let json = serde_json::to_string(&r).unwrap();
        assert!(json.contains("\"provider_id\":\"p1\""));
        assert!(json.contains("\"reason\":\"sigterm\""));
        assert!(json.contains("\"at_unix_ms\":1700000000000"));
    }

    // -- Integration-style tests against an in-process HTTP server --

    /// Tiny in-process HTTP server that counts hits and lets the test
    /// assert which URL paths were exercised in which order. Records
    /// the request order so a test can verify register fired before
    /// the first health POST.
    struct CountingServer {
        addr: SocketAddr,
        health_hits: Arc<AtomicUsize>,
        offline_hits: Arc<AtomicUsize>,
        register_hits: Arc<AtomicUsize>,
        first_endpoint: Arc<std::sync::Mutex<Option<&'static str>>>,
    }

    impl CountingServer {
        async fn spawn() -> Self {
            use tokio::io::{AsyncReadExt, AsyncWriteExt};
            use tokio::net::TcpListener;

            let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
            let addr = listener.local_addr().unwrap();
            let health_hits = Arc::new(AtomicUsize::new(0));
            let offline_hits = Arc::new(AtomicUsize::new(0));
            let register_hits = Arc::new(AtomicUsize::new(0));
            let first_endpoint: Arc<std::sync::Mutex<Option<&'static str>>> =
                Arc::new(std::sync::Mutex::new(None));
            let h = health_hits.clone();
            let o = offline_hits.clone();
            let reg = register_hits.clone();
            let first = first_endpoint.clone();
            tokio::spawn(async move {
                loop {
                    let (mut sock, _) = match listener.accept().await {
                        Ok(v) => v,
                        Err(_) => return,
                    };
                    let h = h.clone();
                    let o = o.clone();
                    let reg = reg.clone();
                    let first = first.clone();
                    tokio::spawn(async move {
                        let mut buf = vec![0u8; 4096];
                        let n = sock.read(&mut buf).await.unwrap_or(0);
                        let req = String::from_utf8_lossy(&buf[..n]).to_string();
                        if req.starts_with("POST ") {
                            // Order matters: /register and /offline
                            // must be checked before /health because
                            // the request line literally contains
                            // "/health" as a substring of the
                            // hostname's chi-route prefix is OK but
                            // grep order here keeps the buckets
                            // disjoint.
                            let endpoint = if req.contains("/register") {
                                reg.fetch_add(1, Ordering::Relaxed);
                                Some("register")
                            } else if req.contains("/offline") {
                                o.fetch_add(1, Ordering::Relaxed);
                                Some("offline")
                            } else if req.contains("/health") {
                                h.fetch_add(1, Ordering::Relaxed);
                                Some("health")
                            } else {
                                None
                            };
                            if let Some(ep) = endpoint {
                                let mut g = first.lock().unwrap();
                                if g.is_none() {
                                    *g = Some(ep);
                                }
                            }
                        }
                        // Drain any body the client wrote past the
                        // header — small bodies fit inside the same
                        // read.
                        let _ = sock
                            .write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
                            .await;
                    });
                }
            });
            Self {
                addr,
                health_hits,
                offline_hits,
                register_hits,
                first_endpoint,
            }
        }
    }

    #[tokio::test]
    async fn notify_offline_hits_the_offline_endpoint() {
        let srv = CountingServer::spawn().await;
        let cfg = test_config(format!("http://{}", srv.addr));
        let http = reqwest::Client::new();
        notify_offline(&cfg, &http, "sigterm").await.unwrap();
        assert_eq!(srv.offline_hits.load(Ordering::Relaxed), 1);
        assert_eq!(srv.health_hits.load(Ordering::Relaxed), 0);
    }

    #[tokio::test]
    async fn reporter_fires_one_immediate_healthy_then_shuts_down_with_offline() {
        let srv = CountingServer::spawn().await;
        let cfg = test_config(format!("http://{}", srv.addr));
        let http = reqwest::Client::new();
        let (tx, rx) = watch::channel(false);
        let handle = spawn_reporter(cfg, http, rx);

        // Give the immediate tick time to fire.
        tokio::time::sleep(Duration::from_millis(300)).await;
        assert!(
            srv.health_hits.load(Ordering::Relaxed) >= 1,
            "expected immediate healthy tick"
        );

        // Trigger shutdown — reporter should fire offline within
        // SHUTDOWN_BUDGET and exit.
        tx.send(true).unwrap();
        let exited = tokio::time::timeout(Duration::from_secs(4), handle).await;
        assert!(
            exited.is_ok(),
            "reporter task should exit promptly after shutdown"
        );
        assert_eq!(
            srv.offline_hits.load(Ordering::Relaxed),
            1,
            "exactly one offline POST on shutdown"
        );
    }

    #[tokio::test]
    async fn register_provider_posts_region_body() {
        let srv = CountingServer::spawn().await;
        let cfg = test_config(format!("http://{}", srv.addr));
        let http = reqwest::Client::new();
        register_provider(&cfg, &http).await.unwrap();
        assert_eq!(srv.register_hits.load(Ordering::Relaxed), 1);
        assert_eq!(srv.health_hits.load(Ordering::Relaxed), 0);
        assert_eq!(srv.offline_hits.load(Ordering::Relaxed), 0);
    }

    #[tokio::test]
    async fn reporter_calls_register_before_first_health_post() {
        // Lifecycle: the reporter must hit /register before /health on
        // startup so vpn-svc's UpdateProviderHealth UPDATE doesn't 404
        // on a freshly-paired daemon.
        let srv = CountingServer::spawn().await;
        let cfg = test_config(format!("http://{}", srv.addr));
        let http = reqwest::Client::new();
        let (tx, rx) = watch::channel(false);
        let handle = spawn_reporter(cfg, http, rx);

        // Let register + first heartbeat both fire.
        tokio::time::sleep(Duration::from_millis(400)).await;
        assert_eq!(
            srv.register_hits.load(Ordering::Relaxed),
            1,
            "register must fire exactly once on startup"
        );
        assert!(
            srv.health_hits.load(Ordering::Relaxed) >= 1,
            "first health POST should follow register"
        );
        // Endpoint-order assertion: register was first.
        assert_eq!(
            *srv.first_endpoint.lock().unwrap(),
            Some("register"),
            "register must precede health on the wire"
        );

        // Clean shutdown.
        tx.send(true).unwrap();
        let _ = tokio::time::timeout(Duration::from_secs(4), handle).await;
    }

    #[tokio::test]
    async fn reporter_survives_coordinator_down() {
        // Point at a black-hole port — the loop must continue ticking
        // without panicking.
        let cfg = test_config("http://127.0.0.1:1".into());
        let http = reqwest::Client::new();
        let (tx, rx) = watch::channel(false);
        let handle = spawn_reporter(cfg, http, rx);

        // Let the immediate tick fail + the loop park.
        tokio::time::sleep(Duration::from_millis(800)).await;
        assert!(
            !handle.is_finished(),
            "task must keep running through HTTP failures"
        );

        // Clean shutdown.
        tx.send(true).unwrap();
        let _ = tokio::time::timeout(Duration::from_secs(4), handle).await;
    }
}
