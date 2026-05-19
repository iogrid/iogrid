//! Auto-update — Sparkle-style polling updater.
//!
//! Activated for #59 on top of the manifest schema + signing scaffolding
//! shipped in #139. The high-level flow lives in
//! `installer/auto-update/README.md`; this module's responsibilities:
//!
//!  * [`types`]     — wire types + config knobs (no I/O).
//!  * [`manifest`]  — JSON parse + schema-level validation.
//!  * [`verify`]    — Ed25519 manifest signature + SHA-256 / per-binary
//!                    Ed25519 signature verification.
//!  * [`binary`]    — atomic-replace + rollback on the install dir.
//!  * [`worker`]    — the polling loop + fetcher trait + UpdateHandle
//!                    that the supervisor parks alongside its other
//!                    sub-tasks.
//!
//! The updater is **disabled by default**. Providers opt in via
//! `config.updater.disabled = false` (config.toml) or via the web UI
//! at `/account/updates`. When disabled the worker exits immediately
//! after the first iteration of [`worker::spawn_update_poll`].

pub mod binary;
pub mod manifest;
pub mod types;
pub mod verify;
pub mod worker;

// Re-exports so callers can write `use iogrid_core::updater::UpdateConfig`
// without depending on the internal module layout.
pub use binary::{rollback as binary_rollback, BinaryError, InstallLayout};
pub use manifest::{parse as parse_manifest, pick_artifact, pick_upgrade, ManifestError};
pub use types::{
    Channel, ManifestSignature, ReleaseArtifact, ReleaseEntry, UpdateConfig, UpdateHistoryEntry,
    UpdateManifest, UpdateOutcome, DEFAULT_CHANNEL, HEALTH_CHECK_WINDOW, POLL_INTERVAL,
};
pub use verify::{verify_artifact, verify_manifest, VerifyError, TRUSTED_KEYS};
pub use worker::{
    apply_pending, rollback as apply_rollback, run_one_poll, spawn_update_poll, Fetcher,
    HttpFetcher, PollCtx, UpdateHandle, UpdateState, WorkerError, HISTORY_CAP,
};
