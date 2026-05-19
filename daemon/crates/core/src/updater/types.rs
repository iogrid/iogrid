//! Data types shared across the updater submodules.
//!
//! Keep this file dependency-light — no I/O, no async, no crypto. The
//! manifest / signature verification / worker / atomic-replace logic
//! lives in sibling modules.

use std::time::Duration;

use serde::{Deserialize, Serialize};

/// How often we poll the manifest. 6 hours per the activation spec
/// (PR #139's docs read "24h ± 10% jitter"; the spec was tightened to
/// 6h after dogfooding so providers pick up security fixes within a
/// business day). Caller applies ±10% jitter so a CDN flap doesn't
/// thunder-herd.
pub const POLL_INTERVAL: Duration = Duration::from_secs(6 * 60 * 60);

/// Wall-clock health-check window after exec'ing the staged binary.
/// If the new binary panics / exits within this window, the wrapper
/// shim rolls back to `iogridd.old`.
pub const HEALTH_CHECK_WINDOW: Duration = Duration::from_secs(30);

/// Default channel. Production daemons ship on `stable`; the founder's
/// dogfood machine + CI runners flip to `beta` via config.
pub const DEFAULT_CHANNEL: &str = "stable";

/// Update channels.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum Channel {
    /// Production releases. Default.
    Stable,
    /// Pre-release candidates. Beta-opt-in users.
    Beta,
    /// Bleeding edge. Internal only. Also accepted as `canary` for
    /// human-readability; both serialise as `edge` on the wire.
    Edge,
}

impl Channel {
    /// The string form used in the manifest's `channel` field.
    pub fn as_str(self) -> &'static str {
        match self {
            Channel::Stable => "stable",
            Channel::Beta => "beta",
            Channel::Edge => "edge",
        }
    }

    /// Parse from the wire / UI string. Accepts the canonical name plus
    /// the `canary` alias for `edge`.
    pub fn parse(s: &str) -> Option<Self> {
        match s.trim().to_ascii_lowercase().as_str() {
            "stable" => Some(Channel::Stable),
            "beta" => Some(Channel::Beta),
            "edge" | "canary" => Some(Channel::Edge),
            _ => None,
        }
    }
}

/// Configuration knobs for the updater. Loaded from the daemon's
/// `config.toml`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UpdateConfig {
    /// Where the manifest is fetched from.
    pub manifest_url: String,
    /// Channel to follow.
    pub channel: Channel,
    /// Disable auto-update entirely. Provider opts in via config or web UI.
    #[serde(default = "default_disabled")]
    pub disabled: bool,
    /// Override poll cadence (seconds). Useful in CI integration tests
    /// where 6h is impractical. Falls back to [`POLL_INTERVAL`] when None.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub poll_interval_secs: Option<u64>,
}

fn default_disabled() -> bool {
    true
}

impl Default for UpdateConfig {
    fn default() -> Self {
        Self {
            manifest_url: "https://updates.iogrid.org/manifest.json".to_string(),
            channel: Channel::Stable,
            // Off by default. Provider opts in via config or web UI.
            disabled: true,
            poll_interval_secs: None,
        }
    }
}

impl UpdateConfig {
    /// Effective poll interval (config override or [`POLL_INTERVAL`]).
    pub fn effective_poll_interval(&self) -> Duration {
        self.poll_interval_secs
            .map(Duration::from_secs)
            .unwrap_or(POLL_INTERVAL)
    }
}

/// One artifact entry inside a release.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReleaseArtifact {
    /// rustc target triple this artifact is for.
    pub target: String,
    /// CDN URL of the binary blob.
    pub url: String,
    /// Hex-encoded SHA-256 of the blob.
    pub sha256: String,
    /// Total bytes (used as a free integrity check + progress UI).
    pub size_bytes: u64,
    /// Per-binary Ed25519 signature over the hex SHA-256 (base64).
    /// Verified with the same embedded pubkey set as the manifest;
    /// preferred over the optional cosign blob.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub signature: Option<String>,
    /// Optional cosign blob signature, base64-encoded.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub cosign_signature: Option<String>,
}

/// One release entry inside the manifest.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ReleaseEntry {
    /// SemVer string (e.g. "0.1.0", "0.1.0-rc.1").
    pub version: String,
    /// Lowest installed version that may directly upgrade to this.
    pub min_supported_from: String,
    /// Release notes URL.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub release_notes_url: Option<String>,
    /// One entry per supported target triple.
    pub artifacts: Vec<ReleaseArtifact>,
}

/// The full signed manifest. Daemons verify `.signature` against an
/// embedded Ed25519 pubkey before parsing the rest.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UpdateManifest {
    /// Schema version. Bumped on incompatible changes (current: 1).
    pub version: u32,
    /// Channel this manifest is for. Daemons MUST verify this matches
    /// their configured channel — a stable-channel daemon refuses an
    /// edge manifest even if it parses.
    pub channel: String,
    /// ISO-8601 timestamp when the manifest was issued.
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub issued_at: Option<String>,
    /// Available releases, newest-first by convention.
    pub releases: Vec<ReleaseEntry>,
    /// Ed25519 signature over the manifest with `.signature` stripped.
    pub signature: ManifestSignature,
}

/// The trailing signature stanza.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ManifestSignature {
    /// Algorithm; currently "ed25519".
    pub algorithm: String,
    /// Identifier of the key that signed (rotation hint).
    pub key_id: String,
    /// Base64-encoded signature bytes.
    pub value: String,
}

/// Outcome of a single update poll. Useful in both the worker loop and
/// the manual `iogridd update --check` CLI path.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(tag = "status", rename_all = "snake_case")]
pub enum UpdateOutcome {
    /// Manifest says current is latest.
    UpToDate { current: String },
    /// Daemon refused to apply (disabled by config, no compatible
    /// artifact, etc.). `reason` is human-readable.
    Skipped { reason: String },
    /// A pending update has been staged at `<install>/iogridd.new`.
    /// The wrapper / supervisor will replace + restart on next stop.
    Staged {
        from: String,
        to: String,
        path: String,
    },
    /// Manifest fetch / verify failed. `error` is the underlying string.
    Failed { error: String },
}

/// One row in the update-history ledger surfaced to the web UI.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UpdateHistoryEntry {
    /// ISO-8601 timestamp of the event.
    pub at: String,
    /// Channel that was checked.
    pub channel: String,
    /// Daemon version at the time of the check.
    pub from_version: String,
    /// Outcome of the check.
    pub outcome: UpdateOutcome,
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn channel_strings_are_stable() {
        assert_eq!(Channel::Stable.as_str(), "stable");
        assert_eq!(Channel::Beta.as_str(), "beta");
        assert_eq!(Channel::Edge.as_str(), "edge");
    }

    #[test]
    fn channel_parse_accepts_canary_alias() {
        assert_eq!(Channel::parse("canary"), Some(Channel::Edge));
        assert_eq!(Channel::parse("EDGE"), Some(Channel::Edge));
        assert_eq!(Channel::parse("Stable"), Some(Channel::Stable));
        assert_eq!(Channel::parse(""), None);
        assert_eq!(Channel::parse("nightly"), None);
    }

    #[test]
    fn default_config_is_safe() {
        // Auto-update remains off by default — provider opts in via the
        // web UI or config.toml.
        let c = UpdateConfig::default();
        assert!(c.disabled);
        assert_eq!(c.channel, Channel::Stable);
        assert_eq!(c.effective_poll_interval(), POLL_INTERVAL);
    }

    #[test]
    fn effective_poll_interval_honours_override() {
        let c = UpdateConfig {
            poll_interval_secs: Some(60),
            ..UpdateConfig::default()
        };
        assert_eq!(c.effective_poll_interval(), Duration::from_secs(60));
    }

    #[test]
    fn manifest_roundtrips_through_json() {
        let m = UpdateManifest {
            version: 1,
            channel: "stable".into(),
            issued_at: Some("2026-05-19T00:00:00Z".into()),
            releases: vec![ReleaseEntry {
                version: "0.1.0".into(),
                min_supported_from: "0.0.1".into(),
                release_notes_url: None,
                artifacts: vec![ReleaseArtifact {
                    target: "x86_64-apple-darwin".into(),
                    url: "https://releases.iogrid.org/0.1.0/iogridd-darwin-amd64".into(),
                    sha256: "0".repeat(64),
                    size_bytes: 5_242_880,
                    signature: None,
                    cosign_signature: None,
                }],
            }],
            signature: ManifestSignature {
                algorithm: "ed25519".into(),
                key_id: "iogrid-update-2026-1".into(),
                value: "REPLACE_ME_BASE64".into(),
            },
        };
        let s = serde_json::to_string(&m).unwrap();
        let back: UpdateManifest = serde_json::from_str(&s).unwrap();
        assert_eq!(back.releases[0].version, "0.1.0");
    }

    #[test]
    fn poll_interval_is_six_hours() {
        assert_eq!(POLL_INTERVAL.as_secs(), 6 * 60 * 60);
    }

    #[test]
    fn health_check_window_is_thirty_seconds() {
        assert_eq!(HEALTH_CHECK_WINDOW.as_secs(), 30);
    }

    #[test]
    fn update_outcome_roundtrips() {
        let o = UpdateOutcome::Staged {
            from: "0.1.0".into(),
            to: "0.1.1".into(),
            path: "/usr/local/iogrid/iogridd.new".into(),
        };
        let j = serde_json::to_string(&o).unwrap();
        assert!(j.contains("\"status\":\"staged\""));
        let back: UpdateOutcome = serde_json::from_str(&j).unwrap();
        assert_eq!(back, o);
    }
}
