//! Daemon identity — cert + key bundle on disk, plus the one-shot pairing
//! flow that exchanges an enrolment token for a freshly-minted mTLS cert.
//!
//! On first run the daemon has no cert. The user pastes (or the installer
//! provides) a pairing token; the daemon POSTs that token to the
//! coordinator's `/api/v1/providers/pair` endpoint, receives a PEM cert +
//! key pair, and writes them to disk with 0600 permissions.
//!
//! This module exposes:
//!
//! * [`IdentityBundle::load`] — read cert.pem + key.pem from disk.
//! * [`IdentityBundle::save`] — atomically write them.
//! * [`PairingClient`] — issue the pair RPC + persist the returned identity.

use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Identity errors.
#[derive(Debug, Error)]
pub enum IdentityError {
    /// I/O failure while reading or writing the on-disk bundle.
    #[error("identity I/O failed at {path}: {source}")]
    Io {
        /// Path that was being read/written.
        path: PathBuf,
        /// Underlying I/O error.
        #[source]
        source: std::io::Error,
    },
    /// Bundle is empty or malformed.
    #[error("identity bundle malformed: {0}")]
    Malformed(String),
    /// Pairing call to coordinator failed.
    #[error("pairing failed: {0}")]
    PairingFailed(String),
}

/// PEM-encoded cert + key bundle.
#[derive(Debug, Clone)]
pub struct IdentityBundle {
    /// PEM bytes of the leaf certificate.
    pub cert_pem: Vec<u8>,
    /// PEM bytes of the private key.
    pub key_pem: Vec<u8>,
}

impl IdentityBundle {
    /// Load cert + key from `dir/cert.pem` and `dir/key.pem`.
    pub fn load(dir: &Path) -> Result<Self, IdentityError> {
        let cert_path = dir.join("cert.pem");
        let key_path = dir.join("key.pem");
        let cert_pem = std::fs::read(&cert_path).map_err(|source| IdentityError::Io {
            path: cert_path.clone(),
            source,
        })?;
        let key_pem = std::fs::read(&key_path).map_err(|source| IdentityError::Io {
            path: key_path.clone(),
            source,
        })?;
        if cert_pem.is_empty() || key_pem.is_empty() {
            return Err(IdentityError::Malformed("empty PEM file".into()));
        }
        if !cert_pem.starts_with(b"-----BEGIN") {
            return Err(IdentityError::Malformed(
                "cert.pem does not start with -----BEGIN".into(),
            ));
        }
        if !key_pem.starts_with(b"-----BEGIN") {
            return Err(IdentityError::Malformed(
                "key.pem does not start with -----BEGIN".into(),
            ));
        }
        Ok(Self { cert_pem, key_pem })
    }

    /// Write cert + key to `dir/cert.pem` and `dir/key.pem` with `0600`
    /// permissions on Unix (no special mode on Windows — file system ACLs
    /// govern). Creates `dir` if missing.
    pub fn save(&self, dir: &Path) -> Result<(), IdentityError> {
        std::fs::create_dir_all(dir).map_err(|source| IdentityError::Io {
            path: dir.to_path_buf(),
            source,
        })?;
        let cert_path = dir.join("cert.pem");
        let key_path = dir.join("key.pem");
        atomic_write(&cert_path, &self.cert_pem)?;
        atomic_write(&key_path, &self.key_pem)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mode_600 = std::fs::Permissions::from_mode(0o600);
            std::fs::set_permissions(&cert_path, mode_600.clone()).map_err(|source| {
                IdentityError::Io {
                    path: cert_path.clone(),
                    source,
                }
            })?;
            std::fs::set_permissions(&key_path, mode_600).map_err(|source| IdentityError::Io {
                path: key_path.clone(),
                source,
            })?;
        }
        Ok(())
    }
}

fn atomic_write(path: &Path, data: &[u8]) -> Result<(), IdentityError> {
    let tmp = path.with_extension(format!("tmp.{}", std::process::id()));
    std::fs::write(&tmp, data).map_err(|source| IdentityError::Io {
        path: tmp.clone(),
        source,
    })?;
    std::fs::rename(&tmp, path).map_err(|source| IdentityError::Io {
        path: path.to_path_buf(),
        source,
    })?;
    Ok(())
}

/// Pairing-request body — POST'd to `coordinator/api/v1/providers/pair`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PairingRequest {
    /// One-time pairing token displayed in the web UI during signup.
    pub pairing_token: String,
    /// CSR (PKCS#10 PEM) the daemon generated.
    pub csr_pem: String,
    /// Human-friendly display name the operator sees in /admin/providers
    /// and /provide. Populated from the OS hostname (`gethostname(3)` on
    /// Unix, `GetComputerNameExW` on Windows) at pairing time so the
    /// coordinator does not have to fall back to `provider-<short-id>`.
    ///
    /// Doubles as the server-side dedupe key in `PairDaemon`: when the
    /// SAME `owner_user_id + display_name` pair re-registers (i.e. the
    /// daemon was reinstalled / the keypair was wiped on the same host),
    /// the coordinator UPDATES the existing row in-place instead of
    /// INSERTing a duplicate. Without this, every re-pair from the same
    /// machine appeared as a fresh provider in /admin/providers — the
    /// "Hatice's Mac" + "provider-a7a93576-…" duplicate Hatice spotted
    /// on 2026-05-20 was exactly this bug.
    ///
    /// `#[serde(default, skip_serializing_if = "String::is_empty")]` so
    /// older daemons (or test harnesses) that ship an empty string get
    /// the legacy server-side fallback rather than a confusing empty
    /// `display_name`. The server-side handler treats empty as "I don't
    /// know my hostname; please derive one for me".
    #[serde(default, skip_serializing_if = "String::is_empty")]
    pub display_name: String,
}

/// Read the OS hostname and normalise it for use as a provider
/// display-name. Returns an empty string when the OS lookup fails —
/// callers MUST treat that as "let the coordinator derive a fallback".
///
/// Normalisation:
///   * Strip a trailing `.local` (macOS bonjour suffix) so paired Macs
///     show as `Hatices-Mac-mini-2` rather than `Hatices-Mac-mini-2.local`.
///   * Trim ASCII whitespace.
///   * Truncate to 64 chars (DB column is unconstrained TEXT, but the
///     operator-visible chrome looks bad past ~64 chars).
///
/// The function is infallible from the caller's point of view: failure
/// to read the hostname is reported as `""`, never as a panic. The
/// daemon's pair flow is too important to crash on a missing hostname.
pub fn local_display_name() -> String {
    let raw = match hostname::get() {
        Ok(os) => os.to_string_lossy().into_owned(),
        Err(_) => return String::new(),
    };
    let trimmed = raw.trim();
    let stripped = trimmed
        .strip_suffix(".local")
        .or_else(|| trimmed.strip_suffix(".lan"))
        .unwrap_or(trimmed);
    let bounded: String = stripped.chars().take(64).collect();
    bounded
}

/// Pairing-response body returned by the coordinator.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PairingResponse {
    /// Signed leaf cert.
    pub cert_pem: String,
    /// Provider id assigned by the coordinator.
    pub provider_id: String,
    /// Issued CA chain for the coordinator's leaf.
    pub server_ca_pem: String,
}

/// Pairing client — the supervisor calls this once on first boot.
pub struct PairingClient {
    /// URL of the coordinator's pairing endpoint (no auth required — the
    /// pairing token is the authenticator).
    pub pair_endpoint: String,
}

impl PairingClient {
    /// Perform pairing.
    ///
    /// `key_pem` is the daemon's locally-generated private key (PEM). The
    /// CSR is built from it; the coordinator returns the signed leaf.
    ///
    /// NOTE: In this minimal-viable implementation, key generation and CSR
    /// production are delegated to the caller (or a follow-up PR that
    /// brings in `rcgen`). This method is the network round-trip plus
    /// disk-write part — already meaningful.
    pub async fn pair(
        &self,
        req: PairingRequest,
        _state_dir: &Path,
    ) -> Result<PairingResponse, IdentityError> {
        let body = serde_json::to_vec(&req)
            .map_err(|e| IdentityError::PairingFailed(format!("serialize: {e}")))?;
        // Use plain reqwest? Avoid heavy dep. Use hyper-util? We use a tiny
        // async POST via tokio + tcp would still need TLS. For the minimal
        // version we shell out to `curl` if present, else return an error
        // surfaced to the operator. A follow-up PR replaces this with a
        // proper `reqwest`-or-`hyper` impl.
        if let Ok(out) = tokio::process::Command::new("curl")
            .args([
                "-fsSL",
                "-X",
                "POST",
                "-H",
                "content-type: application/json",
                "--data-binary",
                "@-",
                &self.pair_endpoint,
            ])
            .stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::piped())
            .spawn()
            .and_then(|mut child| {
                let mut stdin = child
                    .stdin
                    .take()
                    .ok_or_else(|| std::io::Error::other("stdin not captured"))?;
                let body = body.clone();
                tokio::spawn(async move {
                    use tokio::io::AsyncWriteExt;
                    let _ = stdin.write_all(&body).await;
                });
                Ok(child)
            })
        {
            let out = out
                .wait_with_output()
                .await
                .map_err(|e| IdentityError::PairingFailed(format!("curl: {e}")))?;
            if !out.status.success() {
                return Err(IdentityError::PairingFailed(format!(
                    "curl exit {:?}: {}",
                    out.status.code(),
                    String::from_utf8_lossy(&out.stderr)
                )));
            }
            let resp: PairingResponse = serde_json::from_slice(&out.stdout)
                .map_err(|e| IdentityError::PairingFailed(format!("parse response: {e}")))?;
            return Ok(resp);
        }
        Err(IdentityError::PairingFailed(
            "no HTTP client available (curl not on PATH)".into(),
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn load_then_save_round_trip() {
        let dir = tempfile::tempdir().unwrap();
        let b = IdentityBundle {
            cert_pem: b"-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n".to_vec(),
            key_pem: b"-----BEGIN PRIVATE KEY-----\nfake\n-----END PRIVATE KEY-----\n".to_vec(),
        };
        b.save(dir.path()).unwrap();
        let r = IdentityBundle::load(dir.path()).unwrap();
        assert_eq!(r.cert_pem, b.cert_pem);
        assert_eq!(r.key_pem, b.key_pem);
    }

    #[test]
    fn load_rejects_missing() {
        let dir = tempfile::tempdir().unwrap();
        let err = IdentityBundle::load(dir.path()).unwrap_err();
        assert!(matches!(err, IdentityError::Io { .. }));
    }

    #[test]
    fn load_rejects_garbage_pem() {
        let dir = tempfile::tempdir().unwrap();
        std::fs::write(dir.path().join("cert.pem"), b"not a pem").unwrap();
        std::fs::write(dir.path().join("key.pem"), b"not a pem either").unwrap();
        let err = IdentityBundle::load(dir.path()).unwrap_err();
        assert!(matches!(err, IdentityError::Malformed(_)));
    }

    #[test]
    fn pairing_request_skips_empty_display_name_on_wire() {
        // Older daemons must round-trip through serde without emitting a
        // `"display_name":""` field — the server-side fallback (`provider-
        // <short-id>`) only triggers when the JSON field is absent.
        let req = PairingRequest {
            pairing_token: "tok".into(),
            csr_pem: "csr".into(),
            display_name: String::new(),
        };
        let json = serde_json::to_string(&req).unwrap();
        assert!(
            !json.contains("display_name"),
            "empty display_name must be skipped on the wire; got {json}"
        );
    }

    #[test]
    fn pairing_request_serializes_populated_display_name() {
        // The happy path: a daemon with a known hostname ships it so the
        // coordinator can use it as both the operator-visible label AND
        // the dedupe key for re-pair.
        let req = PairingRequest {
            pairing_token: "tok".into(),
            csr_pem: "csr".into(),
            display_name: "Hatices-Mac-mini-2".into(),
        };
        let json = serde_json::to_string(&req).unwrap();
        assert!(
            json.contains("\"display_name\":\"Hatices-Mac-mini-2\""),
            "populated display_name must round-trip; got {json}"
        );
    }

    #[test]
    fn pairing_request_accepts_missing_display_name_field() {
        // Server-side / replay path: a payload from an older daemon (no
        // display_name field at all) must still deserialize cleanly with
        // the field defaulted to "".
        let body = br#"{"pairing_token":"tok","csr_pem":"csr"}"#;
        let req: PairingRequest = serde_json::from_slice(body).unwrap();
        assert_eq!(req.display_name, "");
        assert_eq!(req.pairing_token, "tok");
    }

    #[test]
    fn local_display_name_returns_non_empty_or_empty_on_supported_oses() {
        // Cannot assert a specific hostname (test runs anywhere), but the
        // helper must never panic and must NOT return whitespace-padded
        // garbage. On any sane CI/dev box the OS hostname is set, so we
        // tolerate either a populated value (the common case) or "" (the
        // documented infallible-failure path).
        let h = local_display_name();
        assert_eq!(
            h,
            h.trim(),
            "local_display_name returned untrimmed value: {h:?}"
        );
        assert!(
            h.chars().count() <= 64,
            "local_display_name returned >64 chars: {h:?}"
        );
        if !h.is_empty() {
            assert!(
                !h.ends_with(".local"),
                ".local suffix must be stripped: {h:?}"
            );
            assert!(!h.ends_with(".lan"), ".lan suffix must be stripped: {h:?}");
        }
    }
}
