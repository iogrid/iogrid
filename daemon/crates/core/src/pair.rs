//! Pairing-code lifecycle.
//!
//! On first run a freshly-installed daemon has no identity material with the
//! coordinator. The installer (curl-pipe-sh, .pkg postinstall, .msi custom
//! action, etc.) asks the daemon for a one-time *pairing code* — a 6-character
//! human-friendly token that the user types into (or has auto-pasted into) the
//! browser onboarding page at `app.iogrid.org/onboard/<code>`.
//!
//! The flow:
//!
//! ```text
//!   installer ──▶ iogridd pair --request
//!                       │
//!                       ▼
//!              ┌─────────────────────────┐
//!              │ generate 6-char code    │  (Crockford base32, no I/L/O/U)
//!              │ store with 10-min TTL   │
//!              │ write to ~/.iogrid/     │  (so installer can read it)
//!              │   pairing-code.txt      │
//!              │ print to stdout         │
//!              └─────────────────────────┘
//!                       │
//!                       ▼
//!              installer opens browser
//!                  /onboard/<code>
//!                       │
//!                       ▼
//!     browser ─▶ gateway-bff /api/v1/onboard/start { token: <code> }
//!                       │
//!                       ▼
//!         coordinator ─ NATS ─▶ daemon (polling, see below)
//!                       │
//!                       ▼
//!                 daemon receives signed mTLS cert
//!                 + scheduling config, persists,
//!                 starts producing
//!
//! Polling (Phase 0): the daemon polls
//! `POST /api/v1/onboard/poll { code }` every 5 s after generating the code,
//! up to the 10-minute TTL. On success the response contains the mTLS bundle
//! + scheduling defaults. On TTL expiry the code is wiped and the user has to
//! re-run `iogridd pair --request`.
//!
//! The code is **single-use** — once claimed by an authenticated browser
//! session it is invalidated server-side immediately and the daemon's next
//! poll resolves the bundle. Stolen codes (e.g. snooped from terminal output)
//! are useless after pairing.

use std::path::PathBuf;
use std::time::{Duration, SystemTime};

use rand::Rng;
use serde::{Deserialize, Serialize};

/// Time the pairing code is valid for. 10 minutes mirrors the magic-link TTL
/// — short enough to limit shoulder-surfing, long enough for installer flow.
pub const PAIRING_TTL: Duration = Duration::from_secs(10 * 60);

/// Length of the human-friendly pairing code.
pub const PAIRING_CODE_LEN: usize = 6;

/// Crockford base32 alphabet minus the visually-confusing characters
/// (I, L, O, U). 32 chars total → 6 chars = 32^6 ≈ 1.07e9 combinations.
/// At the 10-min TTL + per-IP rate-limit on the coordinator's pair
/// endpoint, that's well over the brute-force ceiling.
const CROCKFORD_ALPHA: &[u8] = b"0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/// A one-time pairing code with metadata.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct PairingCode {
    /// 6-character Crockford-base32 code, uppercase. Human reads it.
    pub code: String,
    /// When the code was minted.
    pub issued_at: SystemTime,
    /// When it expires (`issued_at + PAIRING_TTL`).
    pub expires_at: SystemTime,
}

impl PairingCode {
    /// Mint a fresh code.
    pub fn generate() -> Self {
        let mut rng = rand::thread_rng();
        let code: String = (0..PAIRING_CODE_LEN)
            .map(|_| {
                let idx = rng.gen_range(0..CROCKFORD_ALPHA.len());
                CROCKFORD_ALPHA[idx] as char
            })
            .collect();
        let now = SystemTime::now();
        Self {
            code,
            issued_at: now,
            expires_at: now + PAIRING_TTL,
        }
    }

    /// True if the code has not yet expired.
    pub fn is_valid(&self) -> bool {
        SystemTime::now() <= self.expires_at
    }

    /// Resolve the on-disk path for the dotfile fallback.
    ///
    /// We default to `$HOME/.iogrid/pairing-code.txt`. The installer
    /// (which may be running as root via sudo on Linux, or as the
    /// installing user on Mac) writes this so subsequent CLI calls
    /// can read the code without needing to talk to the daemon process.
    pub fn dotfile_path() -> PathBuf {
        if let Ok(home) = std::env::var("HOME") {
            return PathBuf::from(home).join(".iogrid").join("pairing-code.txt");
        }
        if let Ok(profile) = std::env::var("USERPROFILE") {
            return PathBuf::from(profile)
                .join(".iogrid")
                .join("pairing-code.txt");
        }
        PathBuf::from("/tmp/iogrid-pairing-code.txt")
    }

    /// Persist the code to the well-known dotfile path so the installer
    /// + a subsequent CLI invocation can recover it.
    ///
    /// Returns the path actually written.
    pub fn persist_to_dotfile(&self) -> std::io::Result<PathBuf> {
        let path = Self::dotfile_path();
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)?;
        }
        std::fs::write(&path, self.code.as_bytes())?;
        Ok(path)
    }

    /// Read the dotfile, if present. Returns None if the file doesn't
    /// exist OR if it's empty (whitespace).
    pub fn from_dotfile() -> Option<String> {
        let path = Self::dotfile_path();
        let raw = std::fs::read_to_string(&path).ok()?;
        let trimmed = raw.trim();
        if trimmed.is_empty() {
            return None;
        }
        Some(trimmed.to_string())
    }
}

/// Subcommand dispatched by the daemon CLI when invoked as
/// `iogridd pair --request`. Mints a code, writes it, prints it.
///
/// Exit code: 0 on success, non-zero on filesystem error.
pub fn cli_pair_request() -> anyhow::Result<()> {
    let pc = PairingCode::generate();
    let path = pc.persist_to_dotfile()?;
    // Print the code on stdout — installer captures this via
    // `iogridd pair --request`. The path goes to stderr so a curl-pipe
    // installer that interpolates the stdout into a URL won't get
    // confused.
    println!("{}", pc.code);
    eprintln!("[iogridd] pairing code written to {}", path.display());
    eprintln!("[iogridd] expires in {} seconds", PAIRING_TTL.as_secs());
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn generated_code_has_expected_length() {
        let pc = PairingCode::generate();
        assert_eq!(pc.code.len(), PAIRING_CODE_LEN);
    }

    #[test]
    fn generated_code_uses_alphabet_only() {
        let pc = PairingCode::generate();
        for b in pc.code.as_bytes() {
            assert!(
                CROCKFORD_ALPHA.contains(b),
                "code contained out-of-alphabet char: {}",
                *b as char
            );
        }
    }

    #[test]
    fn fresh_code_is_valid() {
        let pc = PairingCode::generate();
        assert!(pc.is_valid());
    }

    #[test]
    fn ttl_is_ten_minutes() {
        assert_eq!(PAIRING_TTL.as_secs(), 600);
    }

    #[test]
    fn generated_codes_are_unique_in_aggregate() {
        // 1000 codes shouldn't have more than 1 collision at the 6-char
        // 32-symbol space (32^6 ≈ 1.07e9, expected collisions over 1000
        // samples are vanishingly small).
        use std::collections::HashSet;
        let mut seen = HashSet::new();
        for _ in 0..1000 {
            let pc = PairingCode::generate();
            assert!(seen.insert(pc.code), "unexpected duplicate code");
        }
    }

    #[test]
    fn persist_and_read_roundtrip() {
        // Use a temp HOME so we don't clobber the developer's real
        // ~/.iogrid/pairing-code.txt.
        let tmp = tempfile::tempdir().unwrap();
        let prev_home = std::env::var("HOME").ok();
        let prev_profile = std::env::var("USERPROFILE").ok();
        // SAFETY: tests share env; we use a serialized lock when running
        // in `cargo test` by leveraging the fact that PairingCode resolves
        // HOME at call time. This test is single-threaded by `cargo test`'s
        // default per-test isolation when --test-threads=1, but to be safe
        // in CI we restore.
        std::env::set_var("HOME", tmp.path());
        std::env::remove_var("USERPROFILE");

        let pc = PairingCode::generate();
        let path = pc.persist_to_dotfile().unwrap();
        assert!(path.exists());

        let read = PairingCode::from_dotfile().unwrap();
        assert_eq!(read, pc.code);

        // Restore env.
        match prev_home {
            Some(v) => std::env::set_var("HOME", v),
            None => std::env::remove_var("HOME"),
        }
        if let Some(v) = prev_profile {
            std::env::set_var("USERPROFILE", v);
        }
    }
}
