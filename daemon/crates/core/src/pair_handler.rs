//! Supervisor [`PairHandler`] — completes #438 piece 3.
//!
//! Bridges the loopback `POST /pair` HTTP route on the UI-bridge to the two
//! seams shipped earlier in the same EPIC:
//!
//! * [`BridgeState::generate_bearer`] — mints a 256-bit opaque bearer.
//! * [`iogrid_transport::IdentityBundle::save_bearer`] — persists it
//!   alongside the mTLS bundle so the daemon can re-arm enforcement after
//!   a restart without re-pairing the user (#451).
//! * `BridgeState::set_bearer_token(Some(_))` — flips the per-route
//!   middleware (#450) into enforcement mode in-process.
//!
//! What is *deliberately* out of scope for this slice: the real coordinator
//! pair RPC (`POST coordinator/api/v1/providers/pair`) + mTLS bundle
//! exchange. That sits on top of this handler in a follow-up — the seams
//! are now in place for that ship to be a straight insertion. See
//! [`iogrid_transport::PairingClient`].

use std::path::PathBuf;
use std::sync::Arc;

use async_trait::async_trait;
use parking_lot::Mutex;

use iogrid_transport::IdentityBundle;
use iogrid_ui_bridge::{BridgeState, PairHandler, PairRequest, PairResponse};

/// Default [`PairHandler`] wired by the supervisor at startup.
pub struct SupervisorPairHandler {
    /// `~/.iogrid` (or `state_dir`) — where the bearer file lives next to
    /// `cert.pem` / `key.pem`. Created on first save.
    identity_dir: PathBuf,
    /// Shared bearer slot — same `Arc` the [`BridgeState`] reads on every
    /// protected route, so flipping it here flips enforcement instantly
    /// without the supervisor having to rebuild state.
    bearer_slot: Arc<Mutex<Option<String>>>,
}

impl SupervisorPairHandler {
    /// Build a handler that persists into `identity_dir` and flips the
    /// supplied shared bearer slot.
    pub fn new(identity_dir: PathBuf, bearer_slot: Arc<Mutex<Option<String>>>) -> Self {
        Self {
            identity_dir,
            bearer_slot,
        }
    }

    /// Convenience: wire against the live `BridgeState` — clones the
    /// bearer `Arc` so the handler tracks the same slot.
    pub fn from_bridge(identity_dir: PathBuf, bridge: &BridgeState) -> Self {
        Self::new(identity_dir, bridge.bearer_token.clone())
    }
}

#[async_trait]
impl PairHandler for SupervisorPairHandler {
    async fn pair(&self, _req: PairRequest) -> Result<PairResponse, String> {
        let bearer = BridgeState::generate_bearer();
        IdentityBundle::save_bearer(&self.identity_dir, &bearer)
            .map_err(|e| format!("persist bearer: {e}"))?;
        *self.bearer_slot.lock() = Some(bearer.clone());
        Ok(PairResponse {
            provider_id: String::new(),
            status: "paired".into(),
            message: "bearer minted + persisted".into(),
            bearer_token: bearer,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn pair_mints_bearer_and_persists_and_flips_slot() {
        let tmp = tempfile::tempdir().unwrap();
        let slot: Arc<Mutex<Option<String>>> = Arc::new(Mutex::new(None));
        let handler = SupervisorPairHandler::new(tmp.path().to_path_buf(), slot.clone());

        let resp = handler
            .pair(PairRequest {
                pairing_token: "ABC123".into(),
                coordinator_url: None,
            })
            .await
            .expect("pair");

        assert_eq!(resp.status, "paired");
        assert_eq!(resp.bearer_token.len(), 64);
        assert!(resp.bearer_token.chars().all(|c| c.is_ascii_hexdigit()));
        assert_eq!(slot.lock().as_deref(), Some(resp.bearer_token.as_str()));

        let on_disk = IdentityBundle::load_bearer(tmp.path()).unwrap();
        assert_eq!(on_disk.as_deref(), Some(resp.bearer_token.as_str()));
    }

    #[tokio::test]
    async fn from_bridge_shares_slot_with_state() {
        let tmp = tempfile::tempdir().unwrap();
        let bridge = BridgeState::default();
        let handler = SupervisorPairHandler::from_bridge(tmp.path().to_path_buf(), &bridge);

        let resp = handler
            .pair(PairRequest {
                pairing_token: "QRS789".into(),
                coordinator_url: None,
            })
            .await
            .expect("pair");

        assert_eq!(
            bridge.bearer_snapshot().as_deref(),
            Some(resp.bearer_token.as_str()),
            "flipping the handler's slot must also flip the live bridge state",
        );
    }
}
