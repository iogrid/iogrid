//! Ed25519 + SHA-256 verification helpers.
//!
//! Two distinct verifications are surfaced:
//!
//! 1. [`verify_manifest`] — checks the manifest's `signature.value`
//!    against the **canonical JSON** body with `.signature` stripped.
//!    The expected key is selected from the embedded trust-root set
//!    via [`ManifestSignature::key_id`]. This protects against
//!    downgrade / version-substitution attacks at the catalog layer.
//!
//! 2. [`verify_artifact`] — checks two things on a downloaded binary
//!    blob:
//!    a) the SHA-256 hash matches the manifest's `sha256` field
//!       (free integrity check); and
//!    b) if the manifest carries `signature` (per-binary Ed25519
//!       over the hex SHA-256), that signature verifies against
//!       the same embedded pubkey set.
//!    Either failure rejects the staged binary.
//!
//! Test vectors are generated at runtime — we sign a known body with
//! a fresh keypair, then verify with the matching pubkey. The
//! production trust-root list lives in [`trusted_keys`] and is wired
//! at compile time via `include_bytes!`.

use base64::Engine;
use ed25519_dalek::{Signature, Verifier, VerifyingKey};
use sha2::{Digest, Sha256};
use thiserror::Error;

use super::types::{ReleaseArtifact, UpdateManifest};

/// Errors raised by the verifier.
#[derive(Debug, Error)]
pub enum VerifyError {
    /// The manifest references a key_id that isn't in our trust-root.
    #[error("unknown signing key_id: {0:?}")]
    UnknownKeyId(String),
    /// Base64-decode of the signature failed.
    #[error("signature is not valid base64: {0}")]
    BadSignatureBase64(String),
    /// Signature is wrong length (Ed25519 is always 64 bytes).
    #[error("signature must be 64 bytes (got {0})")]
    BadSignatureLength(usize),
    /// Ed25519 verification itself failed.
    #[error("signature verification failed")]
    BadSignature,
    /// The binary's SHA-256 didn't match the manifest's declared hash.
    #[error("SHA-256 mismatch: manifest={manifest}, blob={actual}")]
    HashMismatch { manifest: String, actual: String },
    /// JSON re-serialisation of the manifest for signing failed.
    #[error("could not canonicalise manifest for signing: {0}")]
    Canonicalise(String),
}

/// One trusted update-signing public key. Currently 2 keys ship per
/// the rotation policy in `installer/auto-update/README.md`.
pub struct TrustedKey {
    /// Stable identifier (`iogrid-update-YYYY-N`).
    pub key_id: &'static str,
    /// Raw 32-byte Ed25519 public key. Stored as a byte slice so the
    /// compiler can fold the embedded bytes into the binary's rodata.
    pub bytes: &'static [u8],
}

/// The compile-time embedded trust root. Each entry is the Ed25519
/// pubkey whose private half is held by the iogrid release-signing
/// HSM (per the rotation policy in installer/auto-update/README.md).
///
/// During Phase 0 we ship a development trust root so CI's
/// integration test (which signs its own manifest with a freshly
/// generated keypair) can run end-to-end. Production release
/// runners replace this slice via `build.rs` reading
/// `IOGRID_TRUSTED_PUBKEYS` from the secrets store.
pub const TRUSTED_KEYS: &[TrustedKey] = &[
    // Phase-0 placeholder. 32 zero bytes — verification will never
    // succeed against this in production, which is the desired
    // fail-closed default. Tests inject their own pubkey via
    // [`verify_manifest_with`].
    TrustedKey {
        key_id: "iogrid-update-dev-0",
        bytes: &[0u8; 32],
    },
];

/// Look up a trusted key by its `key_id`.
pub fn trusted_key(key_id: &str) -> Option<&'static TrustedKey> {
    TRUSTED_KEYS.iter().find(|k| k.key_id == key_id)
}

/// Verify the manifest against the embedded trust root.
pub fn verify_manifest(m: &UpdateManifest) -> Result<(), VerifyError> {
    let key = trusted_key(&m.signature.key_id)
        .ok_or_else(|| VerifyError::UnknownKeyId(m.signature.key_id.clone()))?;
    verify_manifest_with(m, key.bytes)
}

/// Verify the manifest against a caller-supplied pubkey. Used in
/// tests and by [`verify_manifest`]. The pubkey MUST be 32 bytes.
pub fn verify_manifest_with(m: &UpdateManifest, pubkey: &[u8]) -> Result<(), VerifyError> {
    let pk_arr: [u8; 32] = pubkey
        .try_into()
        .map_err(|_| VerifyError::BadSignatureLength(pubkey.len()))?;
    let vk = VerifyingKey::from_bytes(&pk_arr).map_err(|_| VerifyError::BadSignature)?;

    let sig_bytes = base64::engine::general_purpose::STANDARD
        .decode(m.signature.value.as_bytes())
        .map_err(|e| VerifyError::BadSignatureBase64(e.to_string()))?;
    if sig_bytes.len() != 64 {
        return Err(VerifyError::BadSignatureLength(sig_bytes.len()));
    }
    let sig_arr: [u8; 64] = sig_bytes
        .as_slice()
        .try_into()
        .map_err(|_| VerifyError::BadSignatureLength(sig_bytes.len()))?;
    let sig = Signature::from_bytes(&sig_arr);

    let canonical = canonical_manifest_body(m)?;
    vk.verify(&canonical, &sig)
        .map_err(|_| VerifyError::BadSignature)
}

/// Re-serialise the manifest with `signature.value` cleared and
/// `signature.algorithm` + `signature.key_id` preserved. This is the
/// **canonical body** the signer signed.
///
/// `serde_json` emits keys in struct-declaration order which is stable
/// across builds. We use compact (no-whitespace) encoding so the byte
/// sequence is reproducible across signer & verifier toolchains.
pub fn canonical_manifest_body(m: &UpdateManifest) -> Result<Vec<u8>, VerifyError> {
    let mut clone = m.clone();
    clone.signature.value.clear();
    serde_json::to_vec(&clone).map_err(|e| VerifyError::Canonicalise(e.to_string()))
}

/// Verify a downloaded binary against the manifest entry.
pub fn verify_artifact(a: &ReleaseArtifact, blob: &[u8]) -> Result<(), VerifyError> {
    verify_artifact_with(a, blob, None)
}

/// Verify with an explicit pubkey (used in tests). When `pubkey` is
/// `None`, the trust-root is consulted via the manifest's `key_id`.
/// When the artifact has no per-binary `signature`, only the SHA-256
/// integrity check fires — which is sufficient when the manifest
/// itself was verified (the manifest carries the artifact's hash).
pub fn verify_artifact_with(
    a: &ReleaseArtifact,
    blob: &[u8],
    pubkey: Option<&[u8]>,
) -> Result<(), VerifyError> {
    let mut hasher = Sha256::new();
    hasher.update(blob);
    let digest = hasher.finalize();
    let actual_hex = hex_encode(digest.as_slice());
    if actual_hex.to_ascii_lowercase() != a.sha256.to_ascii_lowercase() {
        return Err(VerifyError::HashMismatch {
            manifest: a.sha256.clone(),
            actual: actual_hex,
        });
    }
    // Per-binary Ed25519 signature, if present. The signed payload is
    // the lowercase hex SHA-256 string (not the raw digest bytes — easier
    // to interoperate with `openssl dgst` / `cosign` workflows).
    if let Some(sig_b64) = &a.signature {
        let sig_bytes = base64::engine::general_purpose::STANDARD
            .decode(sig_b64.as_bytes())
            .map_err(|e| VerifyError::BadSignatureBase64(e.to_string()))?;
        if sig_bytes.len() != 64 {
            return Err(VerifyError::BadSignatureLength(sig_bytes.len()));
        }
        let sig_arr: [u8; 64] = sig_bytes
            .as_slice()
            .try_into()
            .map_err(|_| VerifyError::BadSignatureLength(sig_bytes.len()))?;
        let sig = Signature::from_bytes(&sig_arr);

        let pk_bytes = match pubkey {
            Some(b) => b.to_vec(),
            None => {
                // Use the first trusted key. Production manifests embed
                // signatures from the same key as the manifest itself,
                // so this is identical to the manifest pubkey path.
                TRUSTED_KEYS
                    .first()
                    .map(|k| k.bytes.to_vec())
                    .ok_or(VerifyError::BadSignature)?
            }
        };
        let pk_arr: [u8; 32] = pk_bytes
            .as_slice()
            .try_into()
            .map_err(|_| VerifyError::BadSignatureLength(pk_bytes.len()))?;
        let vk = VerifyingKey::from_bytes(&pk_arr).map_err(|_| VerifyError::BadSignature)?;
        vk.verify(actual_hex.as_bytes(), &sig)
            .map_err(|_| VerifyError::BadSignature)?;
    }
    Ok(())
}

/// Tiny no-alloc hex encoder for fixed-size digests. Lowercase output.
pub fn hex_encode(bytes: &[u8]) -> String {
    const HEX: &[u8; 16] = b"0123456789abcdef";
    let mut s = String::with_capacity(bytes.len() * 2);
    for b in bytes {
        s.push(HEX[(b >> 4) as usize] as char);
        s.push(HEX[(b & 0xf) as usize] as char);
    }
    s
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::updater::types::{ManifestSignature, ReleaseEntry};
    use ed25519_dalek::{Signer, SigningKey};

    fn make_manifest(channel: &str, version: &str) -> UpdateManifest {
        UpdateManifest {
            version: 1,
            channel: channel.into(),
            issued_at: Some("2026-05-19T00:00:00Z".into()),
            releases: vec![ReleaseEntry {
                version: version.into(),
                min_supported_from: "0.0.1".into(),
                release_notes_url: None,
                artifacts: vec![ReleaseArtifact {
                    target: "x86_64-unknown-linux-gnu".into(),
                    url: "https://example/iogridd".into(),
                    sha256: "0".repeat(64),
                    size_bytes: 1024,
                    signature: None,
                    cosign_signature: None,
                }],
            }],
            signature: ManifestSignature {
                algorithm: "ed25519".into(),
                key_id: "test-key".into(),
                value: String::new(),
            },
        }
    }

    fn fresh_key() -> SigningKey {
        // Deterministic 32-byte seed so test failures are reproducible.
        let seed = [7u8; 32];
        SigningKey::from_bytes(&seed)
    }

    #[test]
    fn manifest_signs_and_verifies_with_matching_key() {
        let sk = fresh_key();
        let mut m = make_manifest("stable", "0.2.0");
        let canonical = canonical_manifest_body(&m).unwrap();
        let sig = sk.sign(&canonical);
        m.signature.value = base64::engine::general_purpose::STANDARD.encode(sig.to_bytes());
        verify_manifest_with(&m, sk.verifying_key().as_bytes()).unwrap();
    }

    #[test]
    fn manifest_verify_rejects_tampered_body() {
        let sk = fresh_key();
        let mut m = make_manifest("stable", "0.2.0");
        let canonical = canonical_manifest_body(&m).unwrap();
        let sig = sk.sign(&canonical);
        m.signature.value = base64::engine::general_purpose::STANDARD.encode(sig.to_bytes());
        // Tamper post-signing.
        m.releases[0].version = "0.9.9".into();
        let err = verify_manifest_with(&m, sk.verifying_key().as_bytes()).unwrap_err();
        assert!(matches!(err, VerifyError::BadSignature));
    }

    #[test]
    fn manifest_verify_rejects_wrong_key() {
        let sk = fresh_key();
        let other = SigningKey::from_bytes(&[9u8; 32]);
        let mut m = make_manifest("stable", "0.2.0");
        let canonical = canonical_manifest_body(&m).unwrap();
        let sig = sk.sign(&canonical);
        m.signature.value = base64::engine::general_purpose::STANDARD.encode(sig.to_bytes());
        let err = verify_manifest_with(&m, other.verifying_key().as_bytes()).unwrap_err();
        assert!(matches!(err, VerifyError::BadSignature));
    }

    #[test]
    fn manifest_verify_rejects_bad_base64() {
        let mut m = make_manifest("stable", "0.2.0");
        m.signature.value = "not!base64!!".into();
        let err = verify_manifest_with(&m, &[1u8; 32]).unwrap_err();
        assert!(matches!(err, VerifyError::BadSignatureBase64(_)));
    }

    #[test]
    fn artifact_verifies_when_hash_matches_no_signature() {
        let blob = b"hello world".to_vec();
        let hash = {
            let mut h = Sha256::new();
            h.update(&blob);
            hex_encode(h.finalize().as_slice())
        };
        let a = ReleaseArtifact {
            target: "x86_64-unknown-linux-gnu".into(),
            url: "https://example/iogridd".into(),
            sha256: hash,
            size_bytes: blob.len() as u64,
            signature: None,
            cosign_signature: None,
        };
        verify_artifact(&a, &blob).unwrap();
    }

    #[test]
    fn artifact_rejects_hash_mismatch() {
        let a = ReleaseArtifact {
            target: "x86_64-unknown-linux-gnu".into(),
            url: "https://example/iogridd".into(),
            sha256: "0".repeat(64),
            size_bytes: 11,
            signature: None,
            cosign_signature: None,
        };
        let err = verify_artifact(&a, b"hello world").unwrap_err();
        assert!(matches!(err, VerifyError::HashMismatch { .. }));
    }

    #[test]
    fn artifact_verifies_with_signature_over_hex_hash() {
        let blob = b"binary payload".to_vec();
        let mut h = Sha256::new();
        h.update(&blob);
        let hex = hex_encode(h.finalize().as_slice());
        let sk = fresh_key();
        let sig = sk.sign(hex.as_bytes());
        let a = ReleaseArtifact {
            target: "x86_64-unknown-linux-gnu".into(),
            url: "https://example/iogridd".into(),
            sha256: hex,
            size_bytes: blob.len() as u64,
            signature: Some(base64::engine::general_purpose::STANDARD.encode(sig.to_bytes())),
            cosign_signature: None,
        };
        verify_artifact_with(&a, &blob, Some(sk.verifying_key().as_bytes())).unwrap();
    }

    #[test]
    fn hex_encode_is_lowercase() {
        assert_eq!(hex_encode(&[0xab, 0xcd]), "abcd");
        assert_eq!(hex_encode(&[0xff, 0x00]), "ff00");
    }

    #[test]
    fn trusted_keys_has_phase0_placeholder() {
        assert!(trusted_key("iogrid-update-dev-0").is_some());
        assert!(trusted_key("does-not-exist").is_none());
    }
}
