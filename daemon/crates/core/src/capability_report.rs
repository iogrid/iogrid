//! Startup capability report to providers-svc (#746).
//!
//! ## Why this exists
//!
//! The daemon's only providers-svc write is the one-shot **pairing**
//! handshake (`POST /api/v1/providers/pair`), which carries the CSR +
//! display name but *no capabilities* — the providers row is created with
//! `supported_types={}`, `ios_build_enabled=false`, `platform=NULL`. The
//! capabilities were never populated afterwards: the dispatch-stream
//! `DaemonHello` advertises them to **workloads-svc** (so dispatch works),
//! and `StreamHeartbeats` only bumps `last_seen_at`. So a Mac that gains
//! iOS-build capability after first pairing (e.g. Xcode installed later)
//! stays `ios_build_enabled=false` in the admin / provider dashboard
//! forever — exactly the #746 stale-record bug.
//!
//! ## What it does
//!
//! On every supervisor startup (after pairing, when the daemon has a real
//! provider id), POST the *current* capability snapshot to a providers-svc
//! REST shim that upserts it onto the `providers` row. This keeps the
//! dashboard record live and version-bearing without adding a capability
//! field to the heartbeat hot path. Best-effort: a failure logs a warning
//! and is retried on the next daemon restart — the authoritative dispatch
//! path (DaemonHello → workloads-svc) is unaffected either way.

use std::time::Duration;

use serde::Serialize;

/// How long a single capability POST may take before we give up and let
/// the next startup retry. Generous (the edge may be cold) but bounded so
/// a hung coordinator never stalls daemon boot.
const POST_TIMEOUT: Duration = Duration::from_secs(10);

/// Inputs for [`report_capabilities`]. Cloned from `DaemonConfig` +
/// the live capability probes at call time.
#[derive(Debug, Clone)]
pub struct CapabilityReport {
    /// `coordinator_url` base (e.g. `https://coordinator.iogrid.org:443`).
    /// The providers-svc capability shim is reached through the same edge
    /// that serves `/api/v1/providers/pair`.
    pub coordinator_base_url: String,
    /// Provider id assigned at pairing. Empty → caller must skip the POST.
    pub provider_id: String,
    /// Workload type slugs the daemon can run right now (e.g.
    /// `["BANDWIDTH", "IOS_BUILD"]`). Mirrors `DaemonHello.eligible_types`.
    pub supported_types: Vec<String>,
    /// Host macOS major version (0 if not macOS / undetectable).
    pub host_macos_version: u32,
}

/// Wire body for `POST /api/v1/providers/{id}/capabilities`. Flat JSON so
/// the daemon doesn't depend on the providers-svc proto. The providers-svc
/// shim (`CapabilityReportREST`) translates it into the canonical
/// `UpdateCapabilityInventory` Connect RPC in-process.
#[derive(Debug, Serialize)]
struct CapabilityReportBody {
    /// Workload type slugs (lower-cased server-side as needed).
    supported_types: Vec<String>,
    /// Derived from `supported_types` so the daemon stays the single
    /// source of truth; sent explicitly to keep the server shim trivial.
    gpu_enabled: bool,
    ios_build_enabled: bool,
    /// Host macOS major version (0 = unknown / not macOS).
    host_macos_version: u32,
}

/// Errors surfaced by [`report_capabilities`]. The supervisor logs these
/// and moves on — capability reporting is best-effort.
#[derive(Debug, thiserror::Error)]
pub enum CapabilityReportError {
    /// Skipped because no provider id is available yet (unpaired daemon).
    #[error("no provider id; daemon not paired")]
    Unpaired,
    /// HTTP send-side I/O / TLS / connect error.
    #[error("providers-svc capability POST error: {0}")]
    HttpPost(#[source] reqwest::Error),
    /// providers-svc responded but with a non-2xx status.
    #[error("providers-svc returned status {0}")]
    BadStatus(u16),
    /// We exceeded [`POST_TIMEOUT`] waiting for the response.
    #[error("capability POST timed out")]
    Timeout,
}

/// POST the current capability snapshot to providers-svc. Idempotent: the
/// server upserts onto the existing row keyed by provider id. Returns
/// `Ok(())` only on a 2xx.
pub async fn report_capabilities(
    report: &CapabilityReport,
    http: &reqwest::Client,
) -> Result<(), CapabilityReportError> {
    if report.provider_id.trim().is_empty() {
        return Err(CapabilityReportError::Unpaired);
    }
    let url = format!(
        "{}/api/v1/providers/{}/capabilities",
        report.coordinator_base_url.trim_end_matches('/'),
        report.provider_id
    );
    // Derive the per-capability booleans from the slug list so there's one
    // source of truth (the daemon's eligible-types gate) — matches how
    // workloads-svc's snapshotFromHello infers them on the dispatch path.
    let upper: Vec<String> = report
        .supported_types
        .iter()
        .map(|s| s.to_ascii_uppercase())
        .collect();
    let body = CapabilityReportBody {
        supported_types: report.supported_types.clone(),
        gpu_enabled: upper.iter().any(|t| t == "GPU"),
        ios_build_enabled: upper.iter().any(|t| t == "IOS_BUILD"),
        host_macos_version: report.host_macos_version,
    };

    let post = http.post(&url).json(&body).send();
    let resp = match tokio::time::timeout(POST_TIMEOUT, post).await {
        Ok(Ok(r)) => r,
        Ok(Err(e)) => return Err(CapabilityReportError::HttpPost(e)),
        Err(_) => return Err(CapabilityReportError::Timeout),
    };
    if !resp.status().is_success() {
        return Err(CapabilityReportError::BadStatus(resp.status().as_u16()));
    }
    tracing::info!(
        provider_id = %report.provider_id,
        ios_build_enabled = body.ios_build_enabled,
        host_macos_version = body.host_macos_version,
        "providers-svc capability record refreshed (#746)"
    );
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn unpaired_is_skipped_without_network() {
        let report = CapabilityReport {
            coordinator_base_url: "https://example.invalid".into(),
            provider_id: "   ".into(),
            supported_types: vec!["BANDWIDTH".into()],
            host_macos_version: 0,
        };
        let http = reqwest::Client::new();
        let err = report_capabilities(&report, &http).await.unwrap_err();
        assert!(matches!(err, CapabilityReportError::Unpaired));
    }

    #[test]
    fn body_derives_flags_from_slugs() {
        // IOS_BUILD present → ios_build_enabled; no GPU slug → false.
        let report = CapabilityReport {
            coordinator_base_url: "https://x".into(),
            provider_id: "p".into(),
            supported_types: vec!["BANDWIDTH".into(), "ios_build".into()],
            host_macos_version: 14,
        };
        let upper: Vec<String> = report
            .supported_types
            .iter()
            .map(|s| s.to_ascii_uppercase())
            .collect();
        let body = CapabilityReportBody {
            supported_types: report.supported_types.clone(),
            gpu_enabled: upper.iter().any(|t| t == "GPU"),
            ios_build_enabled: upper.iter().any(|t| t == "IOS_BUILD"),
            host_macos_version: report.host_macos_version,
        };
        assert!(body.ios_build_enabled);
        assert!(!body.gpu_enabled);
        assert_eq!(body.host_macos_version, 14);
        let json = serde_json::to_string(&body).unwrap();
        assert!(json.contains("\"ios_build_enabled\":true"));
        assert!(json.contains("\"host_macos_version\":14"));
    }
}
