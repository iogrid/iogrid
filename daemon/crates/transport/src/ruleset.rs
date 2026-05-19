//! Coordinator-backed `RulesetSource` for the anti-abuse crate.
//!
//! The real network call uses the gRPC `AbuseFilterService.ListFilters` RPC.
//! For the minimal-viable transport in this PR we model it as a sink that
//! the coordinator pushes deltas to via the dispatch bidi stream (the
//! coordinator drives the cadence). A follow-up PR wires this to the
//! tonic-generated `AbuseFilterServiceClient`.

use std::collections::HashSet;
use std::sync::Arc;

use async_trait::async_trait;
use iogrid_anti_abuse::{AntiAbuseError, RulesetBundle, RulesetSnapshot, RulesetSource};
use parking_lot::RwLock;

/// `RulesetSource` impl backed by the coordinator dispatch stream. The
/// supervisor wires `set_bundle()` to the dispatch-frame handler so the
/// anti-abuse refresher just picks up the latest pushed bundle each tick.
#[derive(Debug, Clone, Default)]
pub struct CoordinatorRulesetSource {
    inner: Arc<RwLock<RulesetBundle>>,
}

impl CoordinatorRulesetSource {
    /// Empty source — every fetch returns the empty ruleset until
    /// `set_bundle()` is called.
    pub fn new() -> Self {
        Self::default()
    }

    /// Replace the cached bundle. Called by the dispatch handler each time
    /// the coordinator pushes a fresh `ListFilters` payload.
    pub fn set_bundle(&self, bundle: RulesetBundle) {
        *self.inner.write() = bundle;
    }

    /// Convenience: install just a snapshot (no hashes) — used by tests.
    pub fn set_snapshot(&self, snapshot: RulesetSnapshot) {
        self.inner.write().snapshot = snapshot;
    }
}

#[async_trait]
impl RulesetSource for CoordinatorRulesetSource {
    async fn fetch(&self) -> Result<RulesetBundle, AntiAbuseError> {
        Ok(self.inner.read().clone())
    }
}

/// Helper that maps the coordinator-pushed wire-shape into the
/// `RulesetBundle` the anti-abuse crate expects. Wire shape is the
/// `ListFiltersResponse` proto plus the two big hash-set blobs streamed
/// out-of-band.
#[derive(Debug, Clone, Default)]
pub struct ListFiltersWire {
    /// Active phish/scam domains.
    pub phish_domains: Vec<String>,
    /// Active CSAM hashes.
    pub csam_hashes: Vec<String>,
    /// Blocked outbound TCP ports.
    pub blocked_ports: Vec<u16>,
    /// Blocked outbound destination glob patterns.
    pub blocked_destinations: Vec<String>,
    /// Per-customer requests-per-minute cap (0 = unlimited).
    pub per_customer_rpm: u32,
    /// Ruleset hash returned by the server (sha256 hex over the canonical
    /// JSON form of the rule set).
    pub ruleset_hash: String,
}

impl From<ListFiltersWire> for RulesetBundle {
    fn from(w: ListFiltersWire) -> Self {
        let snapshot = RulesetSnapshot {
            phish_domains: w.phish_domains.len(),
            csam_hashes: w.csam_hashes.len(),
            blocked_destinations: w.blocked_destinations,
            blocked_ports: w.blocked_ports,
            per_customer_rpm: w.per_customer_rpm,
            ruleset_hash: w.ruleset_hash,
            last_refreshed_at: None,
        };
        RulesetBundle {
            snapshot,
            phish: w.phish_domains.into_iter().collect::<HashSet<_>>(),
            csam: w.csam_hashes.into_iter().collect::<HashSet<_>>(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn empty_source_returns_default() {
        let s = CoordinatorRulesetSource::new();
        let b = s.fetch().await.unwrap();
        assert_eq!(b.snapshot.phish_domains, 0);
        assert!(b.phish.is_empty());
    }

    #[tokio::test]
    async fn set_bundle_then_fetch_returns_it() {
        let s = CoordinatorRulesetSource::new();
        let wire = ListFiltersWire {
            phish_domains: vec!["bad.example".into()],
            csam_hashes: vec![],
            blocked_ports: vec![25, 137],
            blocked_destinations: vec!["*.chase.com".into()],
            per_customer_rpm: 60,
            ruleset_hash: "abc123".into(),
        };
        s.set_bundle(wire.into());
        let b = s.fetch().await.unwrap();
        assert_eq!(b.snapshot.ruleset_hash, "abc123");
        assert_eq!(b.snapshot.blocked_ports, vec![25, 137]);
        assert!(b.phish.contains("bad.example"));
    }
}
