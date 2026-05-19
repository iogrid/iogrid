//! Manifest parsing + lightweight schema validation.
//!
//! We don't run a generic JSON-Schema validator here (that would pull
//! in jsonschema + a regex crate). Instead we mirror the constraints
//! from `installer/auto-update/manifest.schema.json` in idiomatic
//! Rust — version range, semver pattern, sha256 hex pattern, target
//! triple allow-list. The schema file is the canonical contract; this
//! module's tests must keep parity with it.

use thiserror::Error;

use super::types::{ReleaseArtifact, ReleaseEntry, UpdateManifest};

/// Errors raised by [`validate`].
#[derive(Debug, Error)]
pub enum ManifestError {
    /// `version` field is unsupported.
    #[error("unsupported manifest version: {0} (this daemon supports v1)")]
    UnsupportedVersion(u32),
    /// `channel` field is outside the allow-list.
    #[error("unknown channel: {0:?}")]
    UnknownChannel(String),
    /// A release's `version` doesn't match the semver-ish pattern.
    #[error("release version {0:?} is not valid semver")]
    BadSemver(String),
    /// A release's `min_supported_from` is not valid semver.
    #[error("min_supported_from {0:?} is not valid semver")]
    BadMinSupported(String),
    /// An artifact's `target` is not in the supported allow-list.
    #[error("unsupported target triple: {0:?}")]
    UnsupportedTarget(String),
    /// An artifact's `sha256` is not 64 hex chars.
    #[error("sha256 must be 64 hex chars (got {0} chars)")]
    BadSha256Length(usize),
    /// An artifact's `sha256` contains non-hex chars.
    #[error("sha256 contains non-hex characters")]
    BadSha256Charset,
    /// An artifact's `size_bytes` is zero.
    #[error("artifact size_bytes must be > 0")]
    ZeroSize,
    /// Signature algorithm is not "ed25519".
    #[error("signature algorithm must be ed25519, got {0:?}")]
    BadAlgorithm(String),
    /// A release has no artifacts at all.
    #[error("release {0:?} has no artifacts")]
    NoArtifacts(String),
    /// JSON parse error.
    #[error("manifest JSON parse: {0}")]
    Parse(#[from] serde_json::Error),
}

/// Allow-list of rustc target triples. Mirrors
/// `installer/auto-update/manifest.schema.json`.
pub const SUPPORTED_TARGETS: &[&str] = &[
    "x86_64-unknown-linux-gnu",
    "aarch64-unknown-linux-gnu",
    "x86_64-apple-darwin",
    "aarch64-apple-darwin",
    "x86_64-pc-windows-msvc",
    "aarch64-pc-windows-msvc",
];

/// Allow-list of channel names. Mirrors the schema enum.
pub const SUPPORTED_CHANNELS: &[&str] = &["stable", "beta", "edge"];

/// Parse a manifest body from JSON and validate against the schema's
/// hard constraints. Returns the typed manifest on success.
pub fn parse(body: &[u8]) -> Result<UpdateManifest, ManifestError> {
    let m: UpdateManifest = serde_json::from_slice(body)?;
    validate(&m)?;
    Ok(m)
}

/// Validate an already-parsed manifest without re-deserialising.
pub fn validate(m: &UpdateManifest) -> Result<(), ManifestError> {
    if m.version != 1 {
        return Err(ManifestError::UnsupportedVersion(m.version));
    }
    if !SUPPORTED_CHANNELS.contains(&m.channel.as_str()) {
        return Err(ManifestError::UnknownChannel(m.channel.clone()));
    }
    if m.signature.algorithm != "ed25519" {
        return Err(ManifestError::BadAlgorithm(m.signature.algorithm.clone()));
    }
    for r in &m.releases {
        validate_release(r)?;
    }
    Ok(())
}

fn validate_release(r: &ReleaseEntry) -> Result<(), ManifestError> {
    if !is_semver_ish(&r.version) {
        return Err(ManifestError::BadSemver(r.version.clone()));
    }
    if !is_semver_ish(&r.min_supported_from) {
        return Err(ManifestError::BadMinSupported(r.min_supported_from.clone()));
    }
    if r.artifacts.is_empty() {
        return Err(ManifestError::NoArtifacts(r.version.clone()));
    }
    for a in &r.artifacts {
        validate_artifact(a)?;
    }
    Ok(())
}

fn validate_artifact(a: &ReleaseArtifact) -> Result<(), ManifestError> {
    if !SUPPORTED_TARGETS.contains(&a.target.as_str()) {
        return Err(ManifestError::UnsupportedTarget(a.target.clone()));
    }
    if a.sha256.len() != 64 {
        return Err(ManifestError::BadSha256Length(a.sha256.len()));
    }
    if !a.sha256.bytes().all(|b| b.is_ascii_hexdigit()) {
        return Err(ManifestError::BadSha256Charset);
    }
    if a.size_bytes == 0 {
        return Err(ManifestError::ZeroSize);
    }
    Ok(())
}

/// Loose semver check — `MAJOR.MINOR.PATCH(-prerelease)?`. We don't pull
/// in the full `semver` crate here because the validator is the only
/// place we need this; comparison logic in [`super::version`] uses the
/// `semver` crate directly.
fn is_semver_ish(s: &str) -> bool {
    // Split on '-' once to separate prerelease.
    let (core, pre) = match s.split_once('-') {
        Some((a, b)) => (a, Some(b)),
        None => (s, None),
    };
    let parts: Vec<&str> = core.split('.').collect();
    if parts.len() != 3 {
        return false;
    }
    for p in &parts {
        if p.is_empty() {
            return false;
        }
        if !p.bytes().all(|b| b.is_ascii_digit()) {
            return false;
        }
    }
    if let Some(pre) = pre {
        if pre.is_empty() {
            return false;
        }
        if !pre
            .bytes()
            .all(|b| b.is_ascii_alphanumeric() || b == b'.' || b == b'-')
        {
            return false;
        }
    }
    true
}

/// Pick the highest-semver release in `m` whose `min_supported_from`
/// is ≤ `current` and whose `version` is > `current`. Returns None if
/// the daemon is already at the latest version, or if no release in
/// the manifest is a valid upgrade target.
pub fn pick_upgrade<'a>(
    m: &'a UpdateManifest,
    current: &str,
) -> Result<Option<&'a ReleaseEntry>, semver::Error> {
    let cur = semver::Version::parse(current)?;
    let mut best: Option<&ReleaseEntry> = None;
    let mut best_v: Option<semver::Version> = None;
    for r in &m.releases {
        let v = match semver::Version::parse(&r.version) {
            Ok(v) => v,
            Err(_) => continue,
        };
        let min = match semver::Version::parse(&r.min_supported_from) {
            Ok(v) => v,
            Err(_) => continue,
        };
        if v <= cur {
            continue;
        }
        if cur < min {
            continue;
        }
        match &best_v {
            None => {
                best = Some(r);
                best_v = Some(v);
            }
            Some(bv) if v > *bv => {
                best = Some(r);
                best_v = Some(v);
            }
            _ => {}
        }
    }
    Ok(best)
}

/// Pick the artifact entry for a given rustc target triple. Returns
/// None when no artifact matches — the daemon then skips this release.
pub fn pick_artifact<'a>(
    release: &'a ReleaseEntry,
    target: &str,
) -> Option<&'a ReleaseArtifact> {
    release.artifacts.iter().find(|a| a.target == target)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::updater::types::ManifestSignature;

    fn good_manifest() -> UpdateManifest {
        UpdateManifest {
            version: 1,
            channel: "stable".into(),
            issued_at: Some("2026-05-19T00:00:00Z".into()),
            releases: vec![ReleaseEntry {
                version: "0.2.0".into(),
                min_supported_from: "0.1.0".into(),
                release_notes_url: None,
                artifacts: vec![ReleaseArtifact {
                    target: "x86_64-unknown-linux-gnu".into(),
                    url: "https://releases.iogrid.org/0.2.0/iogridd-linux-amd64".into(),
                    sha256: "a".repeat(64),
                    size_bytes: 4_194_304,
                    signature: None,
                    cosign_signature: None,
                }],
            }],
            signature: ManifestSignature {
                algorithm: "ed25519".into(),
                key_id: "iogrid-update-2026-1".into(),
                value: "AA==".into(),
            },
        }
    }

    #[test]
    fn validates_a_well_formed_manifest() {
        validate(&good_manifest()).unwrap();
    }

    #[test]
    fn rejects_unsupported_schema_version() {
        let mut m = good_manifest();
        m.version = 2;
        let err = validate(&m).unwrap_err();
        assert!(matches!(err, ManifestError::UnsupportedVersion(2)));
    }

    #[test]
    fn rejects_unknown_channel() {
        let mut m = good_manifest();
        m.channel = "nightly".into();
        let err = validate(&m).unwrap_err();
        assert!(matches!(err, ManifestError::UnknownChannel(_)));
    }

    #[test]
    fn rejects_bad_semver() {
        let mut m = good_manifest();
        m.releases[0].version = "not.a.version".into();
        let err = validate(&m).unwrap_err();
        assert!(matches!(err, ManifestError::BadSemver(_)));
    }

    #[test]
    fn rejects_unsupported_target() {
        let mut m = good_manifest();
        m.releases[0].artifacts[0].target = "mips-unknown-linux-gnu".into();
        let err = validate(&m).unwrap_err();
        assert!(matches!(err, ManifestError::UnsupportedTarget(_)));
    }

    #[test]
    fn rejects_short_sha256() {
        let mut m = good_manifest();
        m.releases[0].artifacts[0].sha256 = "abc".into();
        let err = validate(&m).unwrap_err();
        assert!(matches!(err, ManifestError::BadSha256Length(3)));
    }

    #[test]
    fn rejects_non_hex_sha256() {
        let mut m = good_manifest();
        m.releases[0].artifacts[0].sha256 = "z".repeat(64);
        let err = validate(&m).unwrap_err();
        assert!(matches!(err, ManifestError::BadSha256Charset));
    }

    #[test]
    fn rejects_zero_size() {
        let mut m = good_manifest();
        m.releases[0].artifacts[0].size_bytes = 0;
        let err = validate(&m).unwrap_err();
        assert!(matches!(err, ManifestError::ZeroSize));
    }

    #[test]
    fn rejects_bad_algorithm() {
        let mut m = good_manifest();
        m.signature.algorithm = "rsa".into();
        let err = validate(&m).unwrap_err();
        assert!(matches!(err, ManifestError::BadAlgorithm(_)));
    }

    #[test]
    fn rejects_empty_releases_with_no_artifacts() {
        let mut m = good_manifest();
        m.releases[0].artifacts.clear();
        let err = validate(&m).unwrap_err();
        assert!(matches!(err, ManifestError::NoArtifacts(_)));
    }

    #[test]
    fn parse_validates_example_manifest_shape() {
        // Mirror installer/auto-update/manifest.example.json shape.
        let raw = serde_json::to_vec(&good_manifest()).unwrap();
        parse(&raw).unwrap();
    }

    #[test]
    fn pick_upgrade_returns_highest_compatible() {
        let m = UpdateManifest {
            releases: vec![
                ReleaseEntry {
                    version: "0.2.0".into(),
                    min_supported_from: "0.1.0".into(),
                    release_notes_url: None,
                    artifacts: good_manifest().releases[0].artifacts.clone(),
                },
                ReleaseEntry {
                    version: "0.3.0".into(),
                    min_supported_from: "0.1.0".into(),
                    release_notes_url: None,
                    artifacts: good_manifest().releases[0].artifacts.clone(),
                },
                ReleaseEntry {
                    version: "0.1.5".into(),
                    min_supported_from: "0.0.1".into(),
                    release_notes_url: None,
                    artifacts: good_manifest().releases[0].artifacts.clone(),
                },
            ],
            ..good_manifest()
        };
        let pick = pick_upgrade(&m, "0.1.0").unwrap().unwrap();
        assert_eq!(pick.version, "0.3.0");
    }

    #[test]
    fn pick_upgrade_skips_when_current_is_latest() {
        let pick = pick_upgrade(&good_manifest(), "9.9.9").unwrap();
        assert!(pick.is_none());
    }

    #[test]
    fn pick_upgrade_skips_when_under_min_supported() {
        // good_manifest has min_supported_from=0.1.0; we're on 0.0.1.
        let pick = pick_upgrade(&good_manifest(), "0.0.1").unwrap();
        assert!(pick.is_none());
    }

    #[test]
    fn pick_artifact_matches_target() {
        let r = good_manifest().releases[0].clone();
        assert!(pick_artifact(&r, "x86_64-unknown-linux-gnu").is_some());
        assert!(pick_artifact(&r, "aarch64-apple-darwin").is_none());
    }

    #[test]
    fn semver_ish_accepts_prerelease() {
        assert!(is_semver_ish("0.1.0"));
        assert!(is_semver_ish("0.1.0-rc.1"));
        assert!(is_semver_ish("1.2.3-beta-7"));
        assert!(!is_semver_ish("0.1"));
        assert!(!is_semver_ish("0.1.0-"));
        assert!(!is_semver_ish("v0.1.0"));
    }
}
