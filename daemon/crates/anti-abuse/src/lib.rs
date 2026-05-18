//! Anti-abuse — local pre-flight filters.
//!
//! Mirrors the SAME filter set that runs server-side (`antiabuse-svc`) so the
//! provider can audit, from the local UI bridge, exactly which rules are
//! active on their daemon. Filter rules are pulled from the coordinator on
//! startup and on every rule-set update event.

#![forbid(unsafe_code)]
#![deny(missing_docs)]

use std::collections::HashSet;
use std::sync::Arc;

use async_trait::async_trait;
use parking_lot::RwLock;
use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Anti-abuse errors.
#[derive(Debug, Error)]
pub enum AntiAbuseError {
    /// Filter ruleset failed to parse.
    #[error("invalid ruleset: {0}")]
    InvalidRuleset(String),
}

/// One filter check verdict.
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub enum Verdict {
    /// Traffic is allowed through.
    Allow,
    /// Traffic is blocked; the daemon will reject the workload.
    Block {
        /// Reason category (`csam`, `phish`, `port`, `rate-limit`, …).
        category: String,
        /// Human-readable detail (logged + surfaced in the audit feed).
        detail: String,
    },
}

/// What we're being asked to vet.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FilterRequest {
    /// Destination URL (including scheme + host + path).
    pub destination_url: String,
    /// Customer id submitting the workload.
    pub customer_id: String,
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
    /// Disallowed outbound destination domain patterns.
    pub blocked_destinations: Vec<String>,
    /// Disallowed outbound TCP ports.
    pub blocked_ports: Vec<u16>,
}

/// In-memory filter — used for tests + first-boot before coordinator sync.
#[derive(Debug, Default, Clone)]
pub struct InMemoryFilter {
    inner: Arc<RwLock<RulesetSnapshot>>,
    phish_set: Arc<RwLock<HashSet<String>>>,
    csam_set: Arc<RwLock<HashSet<String>>>,
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
        *self.inner.write() = snapshot;
        *self.phish_set.write() = phish;
        *self.csam_set.write() = csam;
    }
}

#[async_trait]
impl Filter for InMemoryFilter {
    async fn check(&self, req: &FilterRequest) -> Result<Verdict, AntiAbuseError> {
        if let Some(hash) = req.content_hash.as_deref() {
            if self.csam_set.read().contains(hash) {
                return Ok(Verdict::Block {
                    category: "csam".into(),
                    detail: "content hash matched NCMEC blocklist".into(),
                });
            }
        }
        // Crude host match — production version uses suffix tree / Aho-Corasick.
        if let Some(host) = host_of(&req.destination_url) {
            if self.phish_set.read().contains(host) {
                return Ok(Verdict::Block {
                    category: "phish".into(),
                    detail: format!("destination host {host} on phish blocklist"),
                });
            }
        }
        Ok(Verdict::Allow)
    }

    fn ruleset_snapshot(&self) -> RulesetSnapshot {
        self.inner.read().clone()
    }
}

fn host_of(url: &str) -> Option<&str> {
    let rest = url.split("://").nth(1).unwrap_or(url);
    let host = rest.split('/').next().unwrap_or(rest);
    if host.is_empty() {
        None
    } else {
        Some(host)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn empty_filter_allows_everything() {
        let f = InMemoryFilter::new();
        let v = f
            .check(&FilterRequest {
                destination_url: "https://example.com/foo".into(),
                customer_id: "c1".into(),
                content_hash: None,
            })
            .await
            .unwrap();
        assert_eq!(v, Verdict::Allow);
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
        let v = f
            .check(&FilterRequest {
                destination_url: "https://evil.example/x".into(),
                customer_id: "c1".into(),
                content_hash: None,
            })
            .await
            .unwrap();
        assert!(matches!(v, Verdict::Block { ref category, .. } if category == "phish"));
    }
}
