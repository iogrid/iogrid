//! Anti-abuse — local pre-flight filters.
//!
//! Mirrors the SAME filter set that runs server-side (`antiabuse-svc`) so the
//! provider can audit, from the local UI bridge, exactly which rules are
//! active on their daemon. Filter rules are pulled from the coordinator on
//! startup and on every rule-set update event.
//!
//! The implementation is intentionally allocation-light: the active ruleset
//! lives behind an `Arc<RwLock<Inner>>` and `check()` only takes the read
//! lock, so SOCKS5 / proxy traffic on the hot path does not contend with the
//! 5-minute refresh task.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::collections::{HashMap, HashSet};
use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use chrono::{DateTime, Utc};
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Anti-abuse errors.
#[derive(Debug, Error)]
pub enum AntiAbuseError {
    /// Filter ruleset failed to parse.
    #[error("invalid ruleset: {0}")]
    InvalidRuleset(String),
    /// Failed to refresh from coordinator.
    #[error("ruleset refresh failed: {0}")]
    RefreshFailed(String),
}

/// One filter check verdict.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum Verdict {
    /// Traffic is allowed through.
    Allow,
    /// Traffic is allowed but flagged for review.
    Review {
        /// Reason category.
        category: String,
        /// Human-readable detail.
        detail: String,
    },
    /// Traffic is blocked; the daemon will reject the workload.
    Block {
        /// Reason category (`csam`, `phish`, `port`, `destination`, `rate-limit`, …).
        category: String,
        /// Human-readable detail (logged + surfaced in the audit feed).
        detail: String,
    },
}

/// What we're being asked to vet.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FilterRequest {
    /// Destination URL (including scheme + host + path) OR host:port for SOCKS.
    pub destination_url: String,
    /// Customer id submitting the workload.
    pub customer_id: String,
    /// Optional explicit destination port (for SOCKS5 CONNECT).
    pub port: Option<u16>,
    /// Optional content hash (for CSAM / PhotoDNA-class checks).
    pub content_hash: Option<String>,
}

/// Filter trait — the same surface server-side `antiabuse-svc` exposes.
#[async_trait]
pub trait Filter: Send + Sync {
    /// Check a request.
    async fn check(&self, req: &FilterRequest) -> Result<Verdict, AntiAbuseError>;

    /// Snapshot of the active ruleset (for the local audit UI).
    fn ruleset_snapshot(&self) -> RulesetSnapshot;
}

/// Active ruleset for display in the local UI bridge.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct RulesetSnapshot {
    /// Phish/scam domain blocklist size.
    pub phish_domains: usize,
    /// CSAM hash blocklist size (NCMEC PhotoDNA).
    pub csam_hashes: usize,
    /// Disallowed outbound destination domain patterns (glob suffixes — e.g. `*.chase.com`).
    pub blocked_destinations: Vec<String>,
    /// Disallowed outbound TCP ports.
    pub blocked_ports: Vec<u16>,
    /// Per-customer requests per minute cap (0 = unlimited).
    pub per_customer_rpm: u32,
    /// SHA-256 hex of the ruleset content — compared with coordinator for drift.
    pub ruleset_hash: String,
    /// Last refresh from coordinator.
    pub last_refreshed_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Default)]
struct Inner {
    snapshot: RulesetSnapshot,
    phish_set: HashSet<String>,
    csam_set: HashSet<String>,
    blocked_ports_set: HashSet<u16>,
    blocked_destination_suffixes: Vec<String>,
    rate_buckets: HashMap<String, (DateTime<Utc>, u32)>, // (window_start, count)
}

/// In-memory filter — used for tests + first-boot before coordinator sync,
/// and as the actual production type (rules ARE in-memory by design).
#[derive(Debug, Default, Clone)]
pub struct InMemoryFilter {
    inner: Arc<RwLock<Inner>>,
}

impl InMemoryFilter {
    /// Build an empty in-memory filter.
    pub fn new() -> Self {
        Self::default()
    }

    /// Replace the active ruleset (atomically).
    pub fn install(
        &self,
        snapshot: RulesetSnapshot,
        phish: HashSet<String>,
        csam: HashSet<String>,
    ) {
        let mut g = self.inner.write();
        g.blocked_ports_set = snapshot.blocked_ports.iter().copied().collect();
        g.blocked_destination_suffixes = snapshot
            .blocked_destinations
            .iter()
            .map(|p| {
                p.trim_start_matches('*')
                    .trim_start_matches('.')
                    .to_lowercase()
            })
            .collect();
        g.snapshot = snapshot;
        g.phish_set = phish;
        g.csam_set = csam;
    }

    /// Per-customer rate-limit gate. Sliding 60-second window.
    fn check_rate_limit(&self, customer_id: &str, rpm_cap: u32, now: DateTime<Utc>) -> bool {
        if rpm_cap == 0 {
            return true;
        }
        let mut g = self.inner.write();
        let entry = g
            .rate_buckets
            .entry(customer_id.to_string())
            .or_insert((now, 0));
        if (now - entry.0).num_seconds() >= 60 {
            *entry = (now, 1);
            return true;
        }
        if entry.1 >= rpm_cap {
            return false;
        }
        entry.1 += 1;
        true
    }
}

#[async_trait]
impl Filter for InMemoryFilter {
    async fn check(&self, req: &FilterRequest) -> Result<Verdict, AntiAbuseError> {
        // 1. CSAM hash check.
        if let Some(hash) = req.content_hash.as_deref() {
            if self.inner.read().csam_set.contains(hash) {
                return Ok(Verdict::Block {
                    category: "csam".into(),
                    detail: "content hash matched NCMEC blocklist".into(),
                });
            }
        }
        // 2. Port check (for SOCKS5).
        if let Some(p) = req.port {
            if self.inner.read().blocked_ports_set.contains(&p) {
                return Ok(Verdict::Block {
                    category: "port".into(),
                    detail: format!("destination port {p} is blocked"),
                });
            }
        }
        // 3. Host-based checks.
        let host = host_of(&req.destination_url).map(|h| h.to_lowercase());
        if let Some(host) = host.as_deref() {
            let g = self.inner.read();
            if g.phish_set.contains(host) {
                return Ok(Verdict::Block {
                    category: "phish".into(),
                    detail: format!("destination host {host} on phish blocklist"),
                });
            }
            for suffix in g.blocked_destination_suffixes.iter() {
                if host == suffix || host.ends_with(&format!(".{suffix}")) {
                    return Ok(Verdict::Block {
                        category: "destination".into(),
                        detail: format!(
                            "destination host {host} matches blocklist pattern *.{suffix}"
                        ),
                    });
                }
            }
        }
        // 4. Per-customer rate-limit.
        let rpm_cap = self.inner.read().snapshot.per_customer_rpm;
        if !self.check_rate_limit(&req.customer_id, rpm_cap, Utc::now()) {
            return Ok(Verdict::Block {
                category: "rate-limit".into(),
                detail: format!("customer {} exceeded {} req/min", req.customer_id, rpm_cap),
            });
        }
        Ok(Verdict::Allow)
    }

    fn ruleset_snapshot(&self) -> RulesetSnapshot {
        self.inner.read().snapshot.clone()
    }
}

/// Trait describing the "where do rule updates come from" backplane. The
/// coordinator implementation lives in the transport crate; tests use the
/// `StaticRulesetSource` below.
#[async_trait]
pub trait RulesetSource: Send + Sync {
    /// Fetch the current desired ruleset.
    async fn fetch(&self) -> Result<RulesetBundle, AntiAbuseError>;
}

/// Bundle returned by `RulesetSource::fetch`.
#[derive(Debug, Clone, Default)]
pub struct RulesetBundle {
    /// The display snapshot (hash + counts + ports + destinations).
    pub snapshot: RulesetSnapshot,
    /// Phish domains the daemon should block.
    pub phish: HashSet<String>,
    /// CSAM hashes the daemon should block.
    pub csam: HashSet<String>,
}

/// Static ruleset source — returns the same bundle on every call. Used by tests.
#[derive(Debug, Clone)]
pub struct StaticRulesetSource(pub RulesetBundle);

#[async_trait]
impl RulesetSource for StaticRulesetSource {
    async fn fetch(&self) -> Result<RulesetBundle, AntiAbuseError> {
        Ok(self.0.clone())
    }
}

/// Background refresher — pulls from `RulesetSource` every `interval` and
/// installs the result into the target filter. Returns the spawned task handle.
pub fn spawn_refresher<S>(
    filter: InMemoryFilter,
    source: Arc<S>,
    interval: Duration,
) -> tokio::task::JoinHandle<()>
where
    S: RulesetSource + 'static,
{
    tokio::spawn(async move {
        let mut ticker = tokio::time::interval(interval);
        // First tick fires immediately — good, we want a cold-start refresh.
        loop {
            ticker.tick().await;
            match source.fetch().await {
                Ok(bundle) => {
                    let mut snap = bundle.snapshot.clone();
                    snap.last_refreshed_at = Some(Utc::now());
                    filter.install(snap, bundle.phish, bundle.csam);
                    tracing::debug!("anti-abuse ruleset refreshed");
                }
                Err(err) => {
                    tracing::warn!(%err, "anti-abuse ruleset refresh failed");
                }
            }
        }
    })
}

fn host_of(url: &str) -> Option<&str> {
    let rest = url.split("://").nth(1).unwrap_or(url);
    let after_userinfo = rest.rsplit('@').next().unwrap_or(rest);
    let host = after_userinfo.split('/').next().unwrap_or(after_userinfo);
    // Strip :port if present.
    let host = host.split(':').next().unwrap_or(host);
    if host.is_empty() {
        None
    } else {
        Some(host)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn req(url: &str) -> FilterRequest {
        FilterRequest {
            destination_url: url.into(),
            customer_id: "c1".into(),
            port: None,
            content_hash: None,
        }
    }

    #[tokio::test]
    async fn empty_filter_allows_everything() {
        let f = InMemoryFilter::new();
        assert_eq!(
            f.check(&req("https://example.com/foo")).await.unwrap(),
            Verdict::Allow
        );
    }

    #[tokio::test]
    async fn phish_block_fires() {
        let f = InMemoryFilter::new();
        let mut phish = HashSet::new();
        phish.insert("evil.example".to_string());
        f.install(
            RulesetSnapshot {
                phish_domains: 1,
                ..Default::default()
            },
            phish,
            HashSet::new(),
        );
        let v = f.check(&req("https://evil.example/x")).await.unwrap();
        assert!(matches!(v, Verdict::Block { ref category, .. } if category == "phish"));
    }

    #[tokio::test]
    async fn blocked_destination_pattern_matches_subdomain() {
        let f = InMemoryFilter::new();
        f.install(
            RulesetSnapshot {
                blocked_destinations: vec!["*.chase.com".into()],
                ..Default::default()
            },
            HashSet::new(),
            HashSet::new(),
        );
        let v = f.check(&req("https://login.chase.com/")).await.unwrap();
        assert!(matches!(v, Verdict::Block { category, .. } if category == "destination"));
    }

    #[tokio::test]
    async fn blocked_port_for_socks5() {
        let f = InMemoryFilter::new();
        f.install(
            RulesetSnapshot {
                blocked_ports: vec![25],
                ..Default::default()
            },
            HashSet::new(),
            HashSet::new(),
        );
        let mut r = req("smtp.example.com:25");
        r.port = Some(25);
        let v = f.check(&r).await.unwrap();
        assert!(matches!(v, Verdict::Block { category, .. } if category == "port"));
    }

    #[tokio::test]
    async fn csam_hash_blocks() {
        let f = InMemoryFilter::new();
        let mut csam = HashSet::new();
        csam.insert("deadbeef".to_string());
        f.install(
            RulesetSnapshot {
                csam_hashes: 1,
                ..Default::default()
            },
            HashSet::new(),
            csam,
        );
        let mut r = req("https://x.example/img");
        r.content_hash = Some("deadbeef".into());
        let v = f.check(&r).await.unwrap();
        assert!(matches!(v, Verdict::Block { category, .. } if category == "csam"));
    }

    #[tokio::test]
    async fn rate_limit_blocks_after_cap() {
        let f = InMemoryFilter::new();
        f.install(
            RulesetSnapshot {
                per_customer_rpm: 2,
                ..Default::default()
            },
            HashSet::new(),
            HashSet::new(),
        );
        let r = req("https://example.com");
        for _ in 0..2 {
            assert_eq!(f.check(&r).await.unwrap(), Verdict::Allow);
        }
        let v = f.check(&r).await.unwrap();
        assert!(matches!(v, Verdict::Block { category, .. } if category == "rate-limit"));
    }

    #[tokio::test]
    async fn refresher_installs_bundle_on_first_tick() {
        let f = InMemoryFilter::new();
        let bundle = RulesetBundle {
            snapshot: RulesetSnapshot {
                phish_domains: 1,
                ruleset_hash: "abc".into(),
                ..Default::default()
            },
            phish: HashSet::from_iter(["bad.example".into()]),
            csam: HashSet::new(),
        };
        let h = spawn_refresher(
            f.clone(),
            Arc::new(StaticRulesetSource(bundle)),
            Duration::from_millis(20),
        );
        tokio::time::sleep(Duration::from_millis(40)).await;
        h.abort();
        let snap = f.ruleset_snapshot();
        assert_eq!(snap.ruleset_hash, "abc");
        assert!(snap.last_refreshed_at.is_some());
    }

    #[test]
    fn host_of_strips_scheme_path_and_port() {
        assert_eq!(
            host_of("https://example.com:443/x?y=1"),
            Some("example.com")
        );
        assert_eq!(host_of("example.com:8080"), Some("example.com"));
        assert_eq!(host_of("https://u:p@example.com"), Some("example.com"));
    }
}
