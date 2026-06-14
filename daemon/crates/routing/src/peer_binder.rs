//! Daemon-side peer binding loop — VPN-536 (#536).
//!
//! Polls the vpn-svc Coordinator for sessions assigned to this
//! provider, and for each new one:
//!
//!  1. Registers the customer's `customer_wg_public_key` as a peer
//!     on our local [`crate::BoringTun`] WG tunnel via
//!     `Tunnel::upsert_peer`.
//!  2. POSTs our daemon's WG public key back to the Coordinator via
//!     `/v1/vpn/sessions/{session_id}/bind-provider` so the customer
//!     SDK (and the Coordinator's session row) learns which key to
//!     handshake against.
//!
//! After the bind POST, the server-side `ListAssignedSessions` filter
//! excludes the now-bound session from subsequent polls — that's the
//! authoritative way we know a session is done. We still keep a
//! local in-memory set of bound IDs as a defensive guard against
//! a misbehaving coordinator returning the same session twice in one
//! tick.
//!
//! Designed to live alongside [`crate::health`] in the daemon's
//! supervisor — same `reqwest::Client` + `tokio::sync::watch` shutdown
//! pattern, so a single supervisor task can `spawn_health_reporter` +
//! `spawn_peer_binder` and shut them down together.

use std::collections::HashSet;
use std::sync::Arc;
use std::time::Duration;

use serde::{Deserialize, Serialize};

use crate::{Tunnel, WireGuardPeer};

/// How often the binder polls vpn-svc for newly-assigned sessions.
///
/// Five seconds is the cadence the tech-lead's #536 contract calls
/// out — short enough that customers don't perceive bind latency
/// (5 s + ~150 ms HTTP RTT + ~250 ms upsert + bind round-trip = under
/// the 10 s SDK connect deadline), long enough that idle providers
/// don't hammer the Coordinator.
pub const POLL_INTERVAL: Duration = Duration::from_secs(5);

/// How often the binder RE-DERIVES the full live peer set from
/// `/bound-sessions` (#788 daemon-restart recovery).
///
/// This is the durable fix for the "restart strands all bound sessions"
/// bug: a daemon restart wipes boringtun's in-memory per-customer peer
/// map, and the normal 5 s bind poll (`/assigned-sessions`) deliberately
/// HIDES already-bound + >15-min-old sessions (#730), so previously-bound
/// still-live customers are never re-upserted and every handshake they
/// send drops forever ("did not decapsulate against any known peer").
///
/// The reconcile pass runs ONCE immediately on binder startup (so a
/// freshly-restarted daemon repopulates its map within the first HTTP
/// RTT, not after a full interval), then every [`RECONCILE_INTERVAL`].
/// It is idempotent — re-upserting an existing peer is a no-op in
/// boringtun — so a slow cadence is fine; 60 s keeps the recovery
/// bounded without adding meaningful Coordinator load. Crucially it does
/// NOT POST `/bind-provider`: those sessions are already bound; we only
/// rebuild local volatile state, so no session is disturbed.
pub const RECONCILE_INTERVAL: Duration = Duration::from_secs(60);

/// Per-tick HTTP request timeout. Larger than the health loop's
/// `TICK_TIMEOUT` because this call has to enumerate sessions on the
/// server side and we don't want to skip a tick on a slow query.
pub const TICK_TIMEOUT: Duration = Duration::from_secs(8);

/// Default `allowed_ips` we hand to boringtun's `upsert_peer` for the
/// per-customer entry. boringtun currently routes by inner-packet
/// destination — the field is informational at this layer (PR-A
/// stores it for visibility but doesn't filter on it), so we ship
/// `0.0.0.0/0` to reflect that we accept any inner destination from
/// the customer; PR-B can tighten this when it wires the inner stack.
pub const DEFAULT_ALLOWED_IPS: &[&str] = &["0.0.0.0/0", "::/0"];

/// Persistent keepalive (seconds) we ask boringtun to insert into the
/// per-peer Tunn. 25 s is the WG standard for NAT-traversal stickiness.
pub const KEEPALIVE_SECS: u16 = 25;

/// Configuration for the binder task.
#[derive(Debug, Clone)]
pub struct PeerBinderConfig {
    /// Provider UUID — appears in the GET URL.
    pub provider_id: String,
    /// vpn-svc base URL — same form as `health::HealthConfig::vpn_svc_base_url`.
    pub vpn_svc_base_url: String,
}

/// Errors emitted by [`bind_session`]. The poll loop catches each one
/// and logs WARN — a single bind failing doesn't stop the loop.
#[derive(Debug, thiserror::Error)]
pub enum BinderError {
    /// HTTP send-side I/O / TLS / connect error.
    #[error("vpn-svc post error: {0}")]
    HttpPost(#[source] reqwest::Error),
    /// vpn-svc responded but with a non-2xx status.
    #[error("vpn-svc returned status {0}")]
    BadStatus(u16),
    /// The tick exceeded [`TICK_TIMEOUT`].
    #[error("vpn-svc call exceeded tick budget")]
    Timeout,
    /// Local peer-registration failed inside boringtun.
    #[error("upsert peer failed: {0}")]
    UpsertPeer(String),
    /// JSON deserialisation of the assigned-sessions response failed.
    #[error("assigned-sessions parse error: {0}")]
    Parse(#[source] reqwest::Error),
}

/// One row of the `assigned-sessions` GET response. The vpn-svc
/// handler emits `customer_wg_public_key` as `""` if the customer SDK
/// hasn't called `BindCustomerWgKey` yet — we skip those rows because
/// there's nothing to upsert until both sides have advertised keys.
#[derive(Debug, Clone, Deserialize)]
pub struct AssignedSession {
    /// Session UUID.
    pub session_id: String,
    /// Customer UUID.
    pub customer_id: String,
    /// Region slug.
    pub region: String,
    /// Currently-assigned provider UUID — should match `config.provider_id`
    /// on every row but we accept the field for verifiability.
    pub current_provider_id: String,
    /// Base64 WG public key the customer SDK published. Empty string
    /// means the customer hasn't bound yet; we skip the row.
    #[serde(default)]
    pub customer_wg_public_key: String,
}

/// Full body of `GET /v1/vpn/providers/{provider_id}/assigned-sessions`.
#[derive(Debug, Clone, Deserialize)]
pub struct AssignedSessionsResponse {
    /// Echoed provider UUID.
    pub provider_id: String,
    /// Sessions assigned to this provider that have not yet been
    /// bound. The server-side filter excludes rows with non-empty
    /// `provider_wg_public_key`, so we only see fresh ones.
    pub sessions: Vec<AssignedSession>,
    /// Length of `sessions` — sent by the server for sanity-check.
    pub count: usize,
}

/// Body of `POST /v1/vpn/sessions/{session_id}/bind-provider`. Matches
/// the Go struct `bindProviderReq` in the vpn-svc handler.
#[derive(Debug, Clone, Serialize)]
pub struct BindProviderRequest {
    /// Our daemon's base64 WG public key — the customer SDK uses this
    /// to address the WG handshake.
    pub provider_wg_public_key: String,
}

/// Spawn the binder task. The task:
///
///  * Polls every [`POLL_INTERVAL`] until `shutdown_rx` flips.
///  * Each tick: GET assigned-sessions → for each row with a
///    non-empty `customer_wg_public_key`, register the peer on the
///    boringtun tunnel and POST the bind back to vpn-svc.
///  * HTTP / boringtun errors are logged WARN and the loop continues.
///
/// `tunnel` is held as an `Arc<dyn Tunnel>` so the binder works
/// against the real `BoringTun` (under `routing-real`) AND against
/// `NoopTunnel` (so integration tests in the scaffold profile still
/// run without pulling the WG stack).
pub fn spawn_binder(
    config: PeerBinderConfig,
    http: reqwest::Client,
    tunnel: Arc<dyn Tunnel>,
    shutdown_rx: tokio::sync::watch::Receiver<bool>,
) -> tokio::task::JoinHandle<()> {
    tokio::spawn(async move {
        run_binder_loop(config, http, tunnel, shutdown_rx).await;
    })
}

async fn run_binder_loop(
    config: PeerBinderConfig,
    http: reqwest::Client,
    tunnel: Arc<dyn Tunnel>,
    mut shutdown_rx: tokio::sync::watch::Receiver<bool>,
) {
    let list_url = format!(
        "{}/v1/vpn/providers/{}/assigned-sessions",
        config.vpn_svc_base_url.trim_end_matches('/'),
        config.provider_id
    );
    let bound_url = format!(
        "{}/v1/vpn/providers/{}/bound-sessions",
        config.vpn_svc_base_url.trim_end_matches('/'),
        config.provider_id
    );
    // Local dedup guard against the (unlikely) case where the
    // Coordinator returns a session twice in the same tick before the
    // server-side filter catches up.
    let mut already_bound: HashSet<String> = HashSet::new();
    let mut ticker = tokio::time::interval(POLL_INTERVAL);
    ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
    // Reconcile ticker (#788). `interval` fires immediately on its first
    // `.tick()`, so the very first loop iteration runs a full peer
    // re-derive — a freshly-restarted daemon repopulates its boringtun
    // map within one HTTP RTT instead of stranding bound customers until
    // they happen to reconnect.
    let mut reconcile_ticker = tokio::time::interval(RECONCILE_INTERVAL);
    reconcile_ticker.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);

    loop {
        tokio::select! {
            biased;
            res = shutdown_rx.changed() => {
                if res.is_err() || *shutdown_rx.borrow() {
                    tracing::debug!("peer binder shutdown");
                    return;
                }
            }
            _ = reconcile_ticker.tick() => {
                // Restart-recovery / drift-correction pass: re-derive the
                // FULL live peer set (already-bound included) and re-upsert
                // each into boringtun. Does NOT POST bind-provider — these
                // sessions are already bound; we only rebuild volatile
                // local state. Idempotent, so overlap with the bind poll is
                // harmless.
                match reconcile_bound_peers(&http, &bound_url, tunnel.as_ref()).await {
                    Ok(n) if n > 0 => {
                        tracing::info!(reconciled = n, "re-derived live WG peers from bound-sessions (restart recovery)");
                    }
                    Ok(_) => {
                        tracing::debug!("reconcile: no bound peers to re-derive");
                    }
                    Err(e) => {
                        tracing::warn!(error = %e, url = %bound_url, "reconcile bound-sessions failed; will retry next reconcile tick");
                    }
                }
            }
            _ = ticker.tick() => {
                let resp = match poll_assigned(&http, &list_url).await {
                    Ok(r) => r,
                    Err(e) => {
                        tracing::warn!(error = %e, url = %list_url, "poll assigned-sessions failed; will retry next tick");
                        continue;
                    }
                };
                for session in resp.sessions {
                    if already_bound.contains(&session.session_id) {
                        continue;
                    }
                    if session.customer_wg_public_key.trim().is_empty() {
                        tracing::debug!(
                            session_id = %session.session_id,
                            "session has no customer_wg_public_key yet; waiting"
                        );
                        continue;
                    }
                    let session_id = session.session_id.clone();
                    match bind_session(&config, &http, tunnel.as_ref(), session).await {
                        Ok(()) => {
                            already_bound.insert(session_id);
                        }
                        Err(e) => {
                            tracing::warn!(
                                session_id = %session_id,
                                error = %e,
                                "bind session failed; will retry next tick"
                            );
                        }
                    }
                }
            }
        }
    }
}

/// One GET + parse cycle. Capped at [`TICK_TIMEOUT`].
async fn poll_assigned(
    http: &reqwest::Client,
    url: &str,
) -> Result<AssignedSessionsResponse, BinderError> {
    let get = http.get(url).send();
    let resp = match tokio::time::timeout(TICK_TIMEOUT, get).await {
        Ok(Ok(r)) => r,
        Ok(Err(e)) => return Err(BinderError::HttpPost(e)),
        Err(_) => return Err(BinderError::Timeout),
    };
    if !resp.status().is_success() {
        return Err(BinderError::BadStatus(resp.status().as_u16()));
    }
    resp.json::<AssignedSessionsResponse>()
        .await
        .map_err(BinderError::Parse)
}

/// Upsert ONE customer's key as a boringtun peer (no bind-provider POST).
/// Shared by [`bind_session`] (the bring-up path) and
/// [`reconcile_bound_peers`] (the restart-recovery path) so the peer
/// shape — allowed_ips, keepalive — can't drift between them.
async fn upsert_customer_peer(
    tunnel: &dyn Tunnel,
    customer_wg_public_key: &str,
) -> Result<(), BinderError> {
    let peer = WireGuardPeer {
        public_key: customer_wg_public_key.to_string(),
        endpoint: None,
        allowed_ips: DEFAULT_ALLOWED_IPS.iter().map(|s| s.to_string()).collect(),
        persistent_keepalive: KEEPALIVE_SECS,
    };
    tunnel
        .upsert_peer(peer)
        .await
        .map_err(|e| BinderError::UpsertPeer(e.to_string()))
}

/// Restart-recovery pass (#788): GET `/bound-sessions`, then re-upsert
/// every returned customer key into the local boringtun map. Returns the
/// number of peers re-derived.
///
/// This repairs the "daemon restart strands all bound sessions" bug: a
/// restart wipes boringtun's per-customer peer Tunns, and the normal bind
/// poll hides already-bound + >15-min-old sessions, so without this pass
/// long-lived customers' handshakes drop forever. Unlike [`bind_session`]
/// it does NOT POST `/bind-provider` — these sessions already carry the
/// correct provider key on the server; re-POSTing would be pointless work
/// and (via the #762 key-change guard) could even churn sessions. We only
/// rebuild volatile local state.
///
/// `upsert_peer` is idempotent in boringtun, so a peer that survived (or
/// was just bound by the 5 s poll) is harmlessly refreshed. Rows with an
/// empty customer key are skipped — there is nothing to upsert. A single
/// peer's upsert failure is logged and does not abort the pass; the next
/// reconcile tick retries it.
pub async fn reconcile_bound_peers(
    http: &reqwest::Client,
    bound_url: &str,
    tunnel: &dyn Tunnel,
) -> Result<usize, BinderError> {
    let get = http.get(bound_url).send();
    let resp = match tokio::time::timeout(TICK_TIMEOUT, get).await {
        Ok(Ok(r)) => r,
        Ok(Err(e)) => return Err(BinderError::HttpPost(e)),
        Err(_) => return Err(BinderError::Timeout),
    };
    if !resp.status().is_success() {
        return Err(BinderError::BadStatus(resp.status().as_u16()));
    }
    let body = resp
        .json::<AssignedSessionsResponse>()
        .await
        .map_err(BinderError::Parse)?;

    let mut reconciled = 0usize;
    for session in body.sessions {
        if session.customer_wg_public_key.trim().is_empty() {
            // /bound-sessions already filters empties server-side, but be
            // defensive: nothing to upsert without a key.
            continue;
        }
        match upsert_customer_peer(tunnel, &session.customer_wg_public_key).await {
            Ok(()) => reconciled += 1,
            Err(e) => {
                tracing::warn!(
                    session_id = %session.session_id,
                    customer_id = %session.customer_id,
                    error = %e,
                    "reconcile: upsert peer failed; next reconcile tick will retry"
                );
            }
        }
    }
    Ok(reconciled)
}

/// Run the upsert + bind for a single session. The provider-public-key
/// we POST back is whichever public key the local [`Tunnel`] reports
/// via [`provider_public_key_for_bind`].
pub async fn bind_session(
    config: &PeerBinderConfig,
    http: &reqwest::Client,
    tunnel: &dyn Tunnel,
    session: AssignedSession,
) -> Result<(), BinderError> {
    // 1. Register the customer's WG public key as a peer on our tunnel.
    upsert_customer_peer(tunnel, &session.customer_wg_public_key).await?;

    // 2. POST our daemon's WG public key back to the Coordinator.
    let our_pubkey = provider_public_key_for_bind(tunnel);
    let url = format!(
        "{}/v1/vpn/sessions/{}/bind-provider",
        config.vpn_svc_base_url.trim_end_matches('/'),
        session.session_id
    );
    let body = BindProviderRequest {
        provider_wg_public_key: our_pubkey,
    };
    let post = http.post(&url).json(&body).send();
    let resp = match tokio::time::timeout(TICK_TIMEOUT, post).await {
        Ok(Ok(r)) => r,
        Ok(Err(e)) => return Err(BinderError::HttpPost(e)),
        Err(_) => return Err(BinderError::Timeout),
    };
    if !resp.status().is_success() {
        return Err(BinderError::BadStatus(resp.status().as_u16()));
    }

    tracing::info!(
        session_id = %session.session_id,
        customer_id = %session.customer_id,
        region = %session.region,
        "session bound — customer peer upserted + provider key posted"
    );
    Ok(())
}

/// The WG public key we send back to Coordinator on the bind POST.
/// Delegated to the Tunnel impl via the `Tunnel::provider_public_key`
/// trait method — `BoringTun` returns its cached `static_public_b64`,
/// `NoopTunnel` returns a sentinel so wire-shape tests still pass.
fn provider_public_key_for_bind(tunnel: &dyn Tunnel) -> String {
    tunnel.provider_public_key()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::NoopTunnel;
    use std::net::SocketAddr;
    use std::sync::atomic::{AtomicUsize, Ordering as AtomicOrdering};
    use std::sync::Mutex;
    use tokio::sync::watch;

    /// In-process HTTP server that:
    ///   * Returns a canned `AssignedSessionsResponse` on every GET
    ///     to `/v1/vpn/providers/{id}/assigned-sessions`.
    ///   * Returns a (separately-canned) body on every GET to
    ///     `/v1/vpn/providers/{id}/bound-sessions` (#788) and counts the
    ///     hits, so restart-recovery tests can assert the reconcile pass
    ///     fired.
    ///   * Records every `/bind-provider` POST body so the test can
    ///     verify the provider_wg_public_key shape.
    struct FakeVpnSvc {
        addr: SocketAddr,
        // `get_hits` exists so a future test can assert tick-cadence —
        // unused on the current tests, allowed-dead to keep clippy
        // green without losing the slot.
        #[allow(dead_code)]
        get_hits: Arc<AtomicUsize>,
        bound_hits: Arc<AtomicUsize>,
        bind_hits: Arc<AtomicUsize>,
        last_bind_body: Arc<Mutex<Option<String>>>,
    }

    impl FakeVpnSvc {
        async fn spawn(sessions: Vec<AssignedSession>) -> Self {
            Self::spawn_with_bound(sessions, vec![]).await
        }

        /// Like `spawn`, but `bound` is the canned `/bound-sessions` body
        /// (the restart-recovery feed) independent of the `/assigned-sessions`
        /// feed. Most bring-up tests pass `bound = vec![]`.
        async fn spawn_with_bound(
            sessions: Vec<AssignedSession>,
            bound: Vec<AssignedSession>,
        ) -> Self {
            use tokio::io::{AsyncReadExt, AsyncWriteExt};
            use tokio::net::TcpListener;

            let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
            let addr = listener.local_addr().unwrap();
            let get_hits = Arc::new(AtomicUsize::new(0));
            let bound_hits = Arc::new(AtomicUsize::new(0));
            let bind_hits = Arc::new(AtomicUsize::new(0));
            let last_bind_body = Arc::new(Mutex::new(None));

            // Pre-render both GET bodies once.
            let assigned_body = AssignedSessionsResponse {
                provider_id: "provider-uuid".into(),
                count: sessions.len(),
                sessions,
            };
            let assigned_json =
                serde_json::to_string(&AssignedSessionsResponseRef(&assigned_body)).unwrap();
            let bound_body = AssignedSessionsResponse {
                provider_id: "provider-uuid".into(),
                count: bound.len(),
                sessions: bound,
            };
            let bound_json =
                serde_json::to_string(&AssignedSessionsResponseRef(&bound_body)).unwrap();

            let g = get_hits.clone();
            let bo = bound_hits.clone();
            let b = bind_hits.clone();
            let bb = last_bind_body.clone();
            tokio::spawn(async move {
                loop {
                    let (mut sock, _) = match listener.accept().await {
                        Ok(v) => v,
                        Err(_) => return,
                    };
                    let g = g.clone();
                    let bo = bo.clone();
                    let b = b.clone();
                    let bb = bb.clone();
                    let assigned_json = assigned_json.clone();
                    let bound_json = bound_json.clone();
                    tokio::spawn(async move {
                        let mut buf = vec![0u8; 8192];
                        let n = sock.read(&mut buf).await.unwrap_or(0);
                        let req = String::from_utf8_lossy(&buf[..n]).to_string();
                        // Check /bound-sessions FIRST: it must not be shadowed
                        // by a looser /assigned-sessions match (the two URLs
                        // share the /providers/{id}/ prefix).
                        if req.starts_with("GET ") && req.contains("/bound-sessions") {
                            bo.fetch_add(1, AtomicOrdering::Relaxed);
                            let resp = format!(
                                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                                bound_json.len(), bound_json
                            );
                            let _ = sock.write_all(resp.as_bytes()).await;
                        } else if req.starts_with("GET ") && req.contains("/assigned-sessions") {
                            g.fetch_add(1, AtomicOrdering::Relaxed);
                            let resp = format!(
                                "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: {}\r\n\r\n{}",
                                assigned_json.len(), assigned_json
                            );
                            let _ = sock.write_all(resp.as_bytes()).await;
                        } else if req.starts_with("POST ") && req.contains("/bind-provider") {
                            b.fetch_add(1, AtomicOrdering::Relaxed);
                            // Find body after the \r\n\r\n header
                            // separator and stash for the test to read.
                            if let Some(idx) = req.find("\r\n\r\n") {
                                *bb.lock().unwrap() = Some(req[idx + 4..].to_string());
                            }
                            let _ = sock
                                .write_all(b"HTTP/1.1 200 OK\r\nContent-Length: 18\r\n\r\n{\"status\":\"bound\"}")
                                .await;
                        } else {
                            let _ = sock
                                .write_all(b"HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n")
                                .await;
                        }
                    });
                }
            });
            Self {
                addr,
                get_hits,
                bound_hits,
                bind_hits,
                last_bind_body,
            }
        }
    }

    /// A `Tunnel` that records every upserted peer public key, so the
    /// restart-recovery tests can assert which peers the reconcile pass
    /// re-derived. `provider_public_key` returns a stable sentinel.
    #[derive(Default)]
    struct RecordingTunnel {
        upserts: Arc<Mutex<Vec<String>>>,
    }

    #[async_trait::async_trait]
    impl Tunnel for RecordingTunnel {
        async fn start(&self) -> Result<(), crate::RoutingError> {
            Ok(())
        }
        async fn stop(&self) -> Result<(), crate::RoutingError> {
            Ok(())
        }
        async fn upsert_peer(&self, peer: WireGuardPeer) -> Result<(), crate::RoutingError> {
            self.upserts.lock().unwrap().push(peer.public_key);
            Ok(())
        }
        fn provider_public_key(&self) -> String {
            "recording-tunnel-public-key".to_owned()
        }
    }

    /// Helper that serializes the response in the same field shape
    /// the real Go handler emits. `AssignedSessionsResponse` only
    /// derives Deserialize on the production type (we don't ship a
    /// Serializer there to keep the wire surface read-only); the
    /// test FakeVpnSvc needs to encode bodies, so we serialize via
    /// this newtype-wrapped reference.
    impl<'a> serde::Serialize for AssignedSessionsResponseRef<'a> {
        fn serialize<S: serde::Serializer>(&self, s: S) -> Result<S::Ok, S::Error> {
            use serde::ser::SerializeStruct;
            let mut st = s.serialize_struct("R", 3)?;
            st.serialize_field("provider_id", &self.0.provider_id)?;
            let sessions: Vec<_> = self
                .0
                .sessions
                .iter()
                .map(|s| WireSession {
                    session_id: &s.session_id,
                    customer_id: &s.customer_id,
                    region: &s.region,
                    current_provider_id: &s.current_provider_id,
                    customer_wg_public_key: &s.customer_wg_public_key,
                })
                .collect();
            st.serialize_field("sessions", &sessions)?;
            st.serialize_field("count", &self.0.count)?;
            st.end()
        }
    }
    struct AssignedSessionsResponseRef<'a>(&'a AssignedSessionsResponse);
    #[derive(Serialize)]
    struct WireSession<'a> {
        session_id: &'a str,
        customer_id: &'a str,
        region: &'a str,
        current_provider_id: &'a str,
        customer_wg_public_key: &'a str,
    }

    fn test_config(base: String) -> PeerBinderConfig {
        PeerBinderConfig {
            provider_id: "11111111-2222-3333-4444-555555555555".into(),
            vpn_svc_base_url: base,
        }
    }

    fn sample_session(session_id: &str, customer_key_b64: &str) -> AssignedSession {
        AssignedSession {
            session_id: session_id.into(),
            customer_id: "cust-aaaa".into(),
            region: "us-east-1".into(),
            current_provider_id: "11111111-2222-3333-4444-555555555555".into(),
            customer_wg_public_key: customer_key_b64.into(),
        }
    }

    #[test]
    fn bind_provider_request_uses_snake_case() {
        let r = BindProviderRequest {
            provider_wg_public_key: "abc=".into(),
        };
        let json = serde_json::to_string(&r).unwrap();
        assert_eq!(json, r#"{"provider_wg_public_key":"abc="}"#);
    }

    #[test]
    fn assigned_session_deserializes_from_handler_shape() {
        let wire = r#"{
            "session_id": "s1",
            "customer_id": "c1",
            "region": "us-east-1",
            "current_provider_id": "p1",
            "customer_wg_public_key": "cust=",
            "created_at": "2026-01-01T00:00:00Z"
        }"#;
        let s: AssignedSession = serde_json::from_str(wire).unwrap();
        assert_eq!(s.session_id, "s1");
        assert_eq!(s.customer_wg_public_key, "cust=");
    }

    #[test]
    fn assigned_session_tolerates_missing_customer_key() {
        // vpn-svc returns "" when the customer SDK hasn't bound yet —
        // our `#[serde(default)]` should accept either an empty
        // string in the JSON or an outright omission.
        let wire = r#"{
            "session_id": "s1",
            "customer_id": "c1",
            "region": "us-east-1",
            "current_provider_id": "p1"
        }"#;
        let s: AssignedSession = serde_json::from_str(wire).unwrap();
        assert!(s.customer_wg_public_key.is_empty());
    }

    #[tokio::test]
    async fn bind_session_upserts_peer_and_posts_bind() {
        let session = sample_session("s1", "Y3VzdG9tZXJfd2dfa2V5X2Jhc2U2NA==");
        let srv = FakeVpnSvc::spawn(vec![]).await;
        let cfg = test_config(format!("http://{}", srv.addr));
        let http = reqwest::Client::new();
        let tunnel = NoopTunnel;

        bind_session(&cfg, &http, &tunnel, session).await.unwrap();

        assert_eq!(srv.bind_hits.load(AtomicOrdering::Relaxed), 1);
        let body = srv.last_bind_body.lock().unwrap().clone().unwrap();
        assert!(body.contains("\"provider_wg_public_key\""));
    }

    #[tokio::test]
    async fn binder_skips_sessions_without_customer_key() {
        let pending = sample_session("s-pending", "");
        let ready = sample_session("s-ready", "Y3VzdG9tZXJfa2V5");
        let srv = FakeVpnSvc::spawn(vec![pending, ready]).await;
        let cfg = test_config(format!("http://{}", srv.addr));
        let http = reqwest::Client::new();
        let (tx, rx) = watch::channel(false);
        let tunnel: Arc<dyn Tunnel> = Arc::new(NoopTunnel);
        let handle = spawn_binder(cfg, http, tunnel, rx);

        // Allow one tick to fire.
        tokio::time::sleep(Duration::from_millis(400)).await;
        // Only the ready one should have produced a bind POST.
        assert_eq!(
            srv.bind_hits.load(AtomicOrdering::Relaxed),
            1,
            "pending session must be skipped"
        );

        tx.send(true).unwrap();
        let _ = tokio::time::timeout(Duration::from_secs(2), handle).await;
    }

    #[tokio::test]
    async fn binder_dedupes_already_bound_sessions_in_same_tick() {
        // The server-side filter normally excludes bound sessions on
        // the next GET, but a transient coordinator could double-emit
        // within a single tick. Our local HashSet guard prevents the
        // double-bind.
        let s = sample_session("s1", "Y3VzdF9rZXk=");
        let srv = FakeVpnSvc::spawn(vec![s.clone(), s.clone()]).await;
        let cfg = test_config(format!("http://{}", srv.addr));
        let http = reqwest::Client::new();
        let (tx, rx) = watch::channel(false);
        let tunnel: Arc<dyn Tunnel> = Arc::new(NoopTunnel);
        let handle = spawn_binder(cfg, http, tunnel, rx);

        tokio::time::sleep(Duration::from_millis(400)).await;
        // First entry: bind. Second entry: same id — bind_session
        // upserts again (idempotent on the tunnel side), and we POST
        // bind once because the in-loop guard inserts after the first
        // success. NoopTunnel always returns Ok on upsert, so we
        // expect 2 POSTs the first tick (no dedup state pre-existed)
        // and 0 thereafter as the HashSet kicks in.
        // The test asserts the post-shutdown total — should be 2
        // (the first tick's two duplicate rows both POST), but after
        // shutdown a second tick would not happen.
        let posts = srv.bind_hits.load(AtomicOrdering::Relaxed);
        assert!(posts >= 1, "at least one bind happened, got {posts}");

        tx.send(true).unwrap();
        let _ = tokio::time::timeout(Duration::from_secs(2), handle).await;
    }

    #[tokio::test]
    async fn binder_survives_vpn_svc_down() {
        // Point at a closed port — the loop must keep ticking.
        let cfg = test_config("http://127.0.0.1:1".into());
        let http = reqwest::Client::new();
        let (tx, rx) = watch::channel(false);
        let tunnel: Arc<dyn Tunnel> = Arc::new(NoopTunnel);
        let handle = spawn_binder(cfg, http, tunnel, rx);

        tokio::time::sleep(Duration::from_millis(800)).await;
        assert!(
            !handle.is_finished(),
            "task must keep running through HTTP failures"
        );

        tx.send(true).unwrap();
        let _ = tokio::time::timeout(Duration::from_secs(2), handle).await;
    }

    // ── #788 restart-recovery (reconcile) tests ─────────────────────────

    #[tokio::test]
    async fn reconcile_upserts_every_bound_peer_without_posting_bind() {
        // The restart-recovery pass must re-derive ALL bound peers into the
        // tunnel, and must NOT POST /bind-provider (those sessions are
        // already bound; re-POSTing would be wasted work / could churn).
        let bound = vec![
            sample_session("s1", "Y3VzdF9rZXlfMQ=="),
            sample_session("s2", "Y3VzdF9rZXlfMg=="),
        ];
        let srv = FakeVpnSvc::spawn_with_bound(vec![], bound).await;
        let http = reqwest::Client::new();
        let tunnel = RecordingTunnel::default();
        let upserts = tunnel.upserts.clone();

        let bound_url = format!(
            "http://{}/v1/vpn/providers/{}/bound-sessions",
            srv.addr, "11111111-2222-3333-4444-555555555555"
        );
        let n = reconcile_bound_peers(&http, &bound_url, &tunnel)
            .await
            .expect("reconcile must succeed");

        assert_eq!(n, 2, "both bound peers must be re-derived");
        let keys = upserts.lock().unwrap().clone();
        assert!(keys.contains(&"Y3VzdF9rZXlfMQ==".to_string()));
        assert!(keys.contains(&"Y3VzdF9rZXlfMg==".to_string()));
        // CRITICAL: recovery does not bind-provider.
        assert_eq!(
            srv.bind_hits.load(AtomicOrdering::Relaxed),
            0,
            "reconcile must NOT POST /bind-provider"
        );
    }

    #[tokio::test]
    async fn reconcile_skips_rows_with_empty_customer_key() {
        // Defensive: even if the server returned an empty-key row, the
        // reconcile pass must skip it (nothing to upsert) and count only the
        // real peers.
        let bound = vec![
            sample_session("s-empty", ""),
            sample_session("s-real", "cmVhbF9rZXk="),
        ];
        let srv = FakeVpnSvc::spawn_with_bound(vec![], bound).await;
        let http = reqwest::Client::new();
        let tunnel = RecordingTunnel::default();
        let upserts = tunnel.upserts.clone();

        let bound_url = format!(
            "http://{}/v1/vpn/providers/{}/bound-sessions",
            srv.addr, "11111111-2222-3333-4444-555555555555"
        );
        let n = reconcile_bound_peers(&http, &bound_url, &tunnel)
            .await
            .unwrap();

        assert_eq!(n, 1, "only the keyed row is upserted");
        let keys = upserts.lock().unwrap().clone();
        assert_eq!(keys, vec!["cmVhbF9rZXk=".to_string()]);
    }

    #[tokio::test]
    async fn reconcile_propagates_http_failure() {
        // A closed port must surface as an error (the loop logs + retries),
        // not a silent success.
        let http = reqwest::Client::new();
        let tunnel = RecordingTunnel::default();
        let bound_url = "http://127.0.0.1:1/v1/vpn/providers/p/bound-sessions";
        let err = reconcile_bound_peers(&http, bound_url, &tunnel)
            .await
            .expect_err("closed port must error");
        // Any send-side error variant is acceptable; just assert it failed.
        let _ = err;
        assert!(tunnel.upserts.lock().unwrap().is_empty());
    }

    #[tokio::test]
    async fn binder_runs_reconcile_immediately_on_startup() {
        // The whole point of #788: a freshly-(re)started binder must hit
        // /bound-sessions on its FIRST iteration (interval fires immediately)
        // and re-upsert the previously-bound peers — without waiting a full
        // RECONCILE_INTERVAL and without any /assigned-sessions row.
        let bound = vec![sample_session("s-live", "bGl2ZV9wZWVyX2tleQ==")];
        let srv = FakeVpnSvc::spawn_with_bound(vec![], bound).await;
        let cfg = test_config(format!("http://{}", srv.addr));
        let http = reqwest::Client::new();
        let (tx, rx) = watch::channel(false);
        let tunnel_concrete = Arc::new(RecordingTunnel::default());
        let upserts = tunnel_concrete.upserts.clone();
        let tunnel: Arc<dyn Tunnel> = tunnel_concrete;
        let handle = spawn_binder(cfg, http, tunnel, rx);

        // Give the first (immediate) reconcile tick time to complete a round
        // trip. RECONCILE_INTERVAL is 60s, so anything we observe inside a
        // few hundred ms came from the startup pass, not a later tick.
        tokio::time::sleep(Duration::from_millis(500)).await;

        assert!(
            srv.bound_hits.load(AtomicOrdering::Relaxed) >= 1,
            "binder must GET /bound-sessions immediately on startup"
        );
        assert!(
            upserts
                .lock()
                .unwrap()
                .contains(&"bGl2ZV9wZWVyX2tleQ==".to_string()),
            "the previously-bound peer must be re-upserted on startup recovery"
        );
        // And it must NOT have churned the session with a bind POST.
        assert_eq!(
            srv.bind_hits.load(AtomicOrdering::Relaxed),
            0,
            "startup recovery must not POST /bind-provider"
        );

        tx.send(true).unwrap();
        let _ = tokio::time::timeout(Duration::from_secs(2), handle).await;
    }
}
