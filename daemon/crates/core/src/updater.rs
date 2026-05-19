//! Auto-update — Sparkle-style polling updater.
//!
//! Phase 0: spec + stubs. The real verifier (Ed25519 manifest +
//! cosign blob) ships behind the `auto-update` cargo feature in #59.
//! Default daemons do NOT auto-update yet.
//!
//! See `installer/auto-update/README.md` for the full spec.

use std::time::Duration;

use serde::{Deserialize, Serialize};

/// How often we poll the manifest. 24h ± 10% jitter is applied at the
/// caller (the supervisor task), not here, so this constant stays a
/// pure scalar.
pub const POLL_INTERVAL: Duration = Duration::from_secs(24 * 60 * 60);

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
    /// Bleeding edge. Internal only.
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
}

/// Configuration knobs for the updater. Loaded from the daemon's
/// `config.toml`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UpdateConfig {
    /// Where the manifest is fetched from.
    pub manifest_url: String,
    /// Channel to follow.
    pub channel: Channel,
    /// Disable auto-update entirely.
    pub disabled: bool,
}

impl Default for UpdateConfig {
    fn default() -> Self {
        Self {
            manifest_url: "https://updates.iogrid.org/manifest.json".to_string(),
            channel: Channel::Stable,
            // Phase 0: disabled by default. Flipped to `false` in the
            // PR that lands the actual verifier + atomic-replace.
            disabled: true,
        }
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
    fn default_config_is_safe() {
        // Phase 0: auto-update is off by default. Re-enable carefully.
        let c = UpdateConfig::default();
        assert!(c.disabled);
        assert_eq!(c.channel, Channel::Stable);
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
    fn poll_interval_is_one_day() {
        assert_eq!(POLL_INTERVAL.as_secs(), 86_400);
    }
}
