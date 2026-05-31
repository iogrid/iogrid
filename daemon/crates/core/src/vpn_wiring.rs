//! Supervisor → iogrid-routing VPN modules wiring — VPN-542 (#542).
//!
//! Bridges the per-process [`DaemonConfig::vpn`] settings into the four
//! tokio tasks the routing crate exposes:
//!
//!   * `iogrid_routing::health::register_provider` — one-shot register
//!     POST so vpn-svc's failover store has a row for this provider.
//!   * `iogrid_routing::health::spawn_reporter` — 15 s heartbeat loop.
//!   * `iogrid_routing::ice::spawn_reporter` — 30 s ICE candidate
//!     enumeration + POST to `/v1/vpn/providers/{id}/candidates`.
//!   * `iogrid_routing::peer_binder::spawn_binder` — 5 s poll of
//!     `/v1/vpn/providers/{id}/assigned-sessions` + per-session
//!     upsert + bind POST.
//!
//! Plus the `BoringTun` UDP server itself — bound at startup, listens
//! for WG packets on the configured `--vpn-listen-addr` for as long as
//! the supervisor lives.
//!
//! ## WG keypair persistence
//!
//! The daemon's static X25519 private key lives at `<state_dir>/wg.key`
//! as 32 base64-encoded bytes. The file is created with mode 0600 on
//! first run; subsequent boots reuse the same identity so providers
//! keep their pubkey across restarts (otherwise customer SDKs would
//! need to re-fetch the bind every time the daemon restarts).

use std::path::Path;
use std::sync::Arc;

use base64::Engine;
use iogrid_routing::{
    health, ice, peer_binder, BoringTun, BoringTunConfig, HealthConfig, IceConfig, LoggingSink,
    Meter, PeerBinderConfig, Tunnel,
};

use crate::DaemonConfig;
#[cfg(test)]
use crate::VpnConfig;

/// Default region the daemon advertises when [`VpnConfig::region`] is
/// empty. Matches the bootstrap region the failover store buckets the
/// first paired Sovereign into.
const DEFAULT_REGION: &str = "us-east-1";

/// Path to the persisted WG private key under the daemon state dir.
pub fn wg_key_path(state_dir: &Path) -> std::path::PathBuf {
    state_dir.join("wg.key")
}

/// Load the persisted WG static private key, or generate + persist a
/// new one if missing. The key file is plain text — 32 bytes of
/// base64. The OpenSSH+ChaCha20-Poly1305 ritual is overkill at this
/// trust boundary (the daemon's `state_dir` already holds `key.pem`
/// in the clear), so we match the existing posture.
///
/// File mode is set to 0600 on Unix on creation; on Windows the
/// default ACL on the state dir is already user-only.
pub fn load_or_generate_wg_private_key(
    state_dir: &Path,
) -> anyhow::Result<boringtun::x25519::StaticSecret> {
    use boringtun::x25519::StaticSecret;
    let path = wg_key_path(state_dir);
    if path.exists() {
        let body = std::fs::read_to_string(&path)?;
        let bytes = base64::engine::general_purpose::STANDARD.decode(body.trim())?;
        if bytes.len() != 32 {
            anyhow::bail!(
                "WG private key at {} must be 32 bytes, got {}",
                path.display(),
                bytes.len()
            );
        }
        let mut arr = [0u8; 32];
        arr.copy_from_slice(&bytes);
        Ok(StaticSecret::from(arr))
    } else {
        let secret = BoringTun::generate_private_key();
        // Re-derive the bytes so we can persist them. boringtun's
        // StaticSecret doesn't expose `to_bytes()` in this version;
        // round-trip via PublicKey::from is one option but loses
        // the private bytes. Use the same OsRng-seeded path that
        // `BoringTun::generate_private_key` uses internally so the
        // bytes we persist match what's in the secret.
        let bytes = static_secret_bytes(&secret);
        std::fs::create_dir_all(state_dir)?;
        let encoded = base64::engine::general_purpose::STANDARD.encode(bytes);
        std::fs::write(&path, encoded)?;
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let perms = std::fs::Permissions::from_mode(0o600);
            std::fs::set_permissions(&path, perms)?;
        }
        tracing::info!(
            path = %path.display(),
            "generated new WG private key for daemon identity"
        );
        Ok(secret)
    }
}

/// Pull the 32 raw bytes out of a `StaticSecret`. The 2.0.1 API
/// exposes them via `to_bytes()`; older variants used `as_bytes()` —
/// boringtun's master pins 2.0.1 so this is stable.
fn static_secret_bytes(secret: &boringtun::x25519::StaticSecret) -> [u8; 32] {
    secret.to_bytes()
}

/// Handle the supervisor holds onto so the VPN modules survive until
/// the supervisor exits. Dropping it triggers shutdown via the watch
/// channel.
pub struct VpnHandles {
    /// Shared shutdown signal. Dropping the sender (or sending true)
    /// stops all 4 tasks + the BoringTun pump.
    pub shutdown_tx: tokio::sync::watch::Sender<bool>,
    /// The boringtun WG server. Kept alive so the UDP pump task stays
    /// alive (the pump task holds a clone of the socket Arc; dropping
    /// BoringTun is what triggers its end).
    pub boringtun: Arc<BoringTun>,
    /// Join handles for the 3 reporter tasks + the binder. We don't
    /// poll them — they're best-effort and own their own retry loops.
    pub task_handles: Vec<tokio::task::JoinHandle<()>>,
}

/// Spawn all VPN modules if the daemon is configured for VPN.
/// Returns `None` when [`VpnConfig::vpn_svc_url`] is empty (VPN
/// disabled — daemon runs pure-SOCKS5 path as before).
///
/// On error: logs the cause and returns `None` rather than refusing
/// to boot the daemon — the SOCKS5 / scheduler stack should keep
/// running even if vpn-svc is unreachable or the WG socket can't bind.
pub async fn spawn_vpn_modules(config: &DaemonConfig) -> Option<VpnHandles> {
    let vpn = &config.vpn;
    if vpn.vpn_svc_url.trim().is_empty() {
        tracing::debug!("VPN modules disabled (vpn_svc_url empty)");
        return None;
    }
    if config.provider_id.trim().is_empty() {
        tracing::warn!(
            "VPN modules requested but provider_id empty; pair the daemon first or skip --vpn-svc"
        );
        return None;
    }

    let vpn_listen_addr: std::net::SocketAddr = match vpn.vpn_listen_addr.parse() {
        Ok(a) => a,
        Err(e) => {
            tracing::error!(error = %e, addr = %vpn.vpn_listen_addr, "invalid --vpn-listen-addr; VPN disabled");
            return None;
        }
    };
    let stun_server: std::net::SocketAddr = match resolve_first(&vpn.stun_server).await {
        Some(a) => a,
        None => {
            tracing::error!(stun = %vpn.stun_server, "could not resolve --stun-server; VPN disabled");
            return None;
        }
    };
    let region = if vpn.region.trim().is_empty() {
        DEFAULT_REGION.to_string()
    } else {
        vpn.region.clone()
    };

    // ---- WG keypair ----
    let static_private = match load_or_generate_wg_private_key(&config.state_dir) {
        Ok(s) => s,
        Err(e) => {
            tracing::error!(error = %e, "WG key load/generate failed; VPN disabled");
            return None;
        }
    };

    // ---- BoringTun ----
    let meter = Arc::new(Meter::default());
    let sink = Arc::new(LoggingSink);
    let boringtun = Arc::new(BoringTun::new(
        BoringTunConfig {
            static_private,
            listen_addr: vpn_listen_addr,
        },
        meter.clone(),
        sink,
    ));
    if let Err(e) = boringtun.start().await {
        tracing::error!(error = %e, "boringtun start failed; VPN disabled");
        return None;
    }
    tracing::info!(
        listen = %vpn_listen_addr,
        our_pubkey = %boringtun.provider_public_key(),
        region = %region,
        "VPN data plane up; routing-real boringtun listening for WG packets"
    );

    // ---- HTTP client shared across all 4 tasks ----
    let http = reqwest::Client::builder()
        .user_agent(format!("iogridd/{}", env!("CARGO_PKG_VERSION")))
        .build()
        .unwrap_or_else(|_| reqwest::Client::new());

    // ---- Shared shutdown signal ----
    let (shutdown_tx, shutdown_rx) = tokio::sync::watch::channel(false);

    // ---- Health one-shot register + periodic loop ----
    let health_cfg = HealthConfig {
        provider_id: config.provider_id.clone(),
        region: region.clone(),
        vpn_svc_base_url: vpn.vpn_svc_url.clone(),
        vpn_listen_addr,
    };
    if let Err(e) = health::register_provider(&health_cfg, &http).await {
        tracing::warn!(error = %e, "VPN /register POST failed; health reporter will retry on next tick");
    }
    let health_handle = health::spawn_reporter(health_cfg, http.clone(), shutdown_rx.clone());

    // ---- ICE candidate reporter ----
    let ice_cfg = IceConfig {
        provider_id: config.provider_id.clone(),
        stun_server,
        vpn_svc_base_url: vpn.vpn_svc_url.clone(),
        vpn_listen_addr,
    };
    let ice_handle = ice::spawn_reporter(ice_cfg, http.clone());

    // ---- Peer binder (depends on the BoringTun handle above) ----
    let binder_cfg = PeerBinderConfig {
        provider_id: config.provider_id.clone(),
        vpn_svc_base_url: vpn.vpn_svc_url.clone(),
    };
    let tunnel: Arc<dyn Tunnel> = boringtun.clone();
    let binder_handle = peer_binder::spawn_binder(binder_cfg, http, tunnel, shutdown_rx);

    Some(VpnHandles {
        shutdown_tx,
        boringtun,
        task_handles: vec![health_handle, ice_handle, binder_handle],
    })
}

/// Tiny DNS-resolve helper — `<host>:<port>` → first `SocketAddr`.
/// Uses tokio's blocking-resolve fallback (the lookup is one-shot at
/// startup; we don't need the hickory hot-path here).
async fn resolve_first(host_port: &str) -> Option<std::net::SocketAddr> {
    use tokio::net::lookup_host;
    lookup_host(host_port)
        .await
        .ok()
        .and_then(|mut it| it.next())
}

#[cfg(test)]
#[allow(clippy::field_reassign_with_default)]
// `DaemonConfig` has ~14 fields; the spread-from-default + named-fields
// form clippy suggests is noisier than the per-field reassign the
// tests below use. Suppressed at module level.
mod tests {
    use super::*;
    use tempfile::tempdir;

    #[test]
    fn wg_key_persists_across_load_calls() {
        let dir = tempdir().unwrap();
        let p = dir.path();
        let k1 = load_or_generate_wg_private_key(p).unwrap();
        let k2 = load_or_generate_wg_private_key(p).unwrap();
        // Same bytes on the second load → file is being read, not
        // regenerated.
        assert_eq!(static_secret_bytes(&k1), static_secret_bytes(&k2));
    }

    #[test]
    fn wg_key_file_is_user_only_on_unix() {
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let dir = tempdir().unwrap();
            let p = dir.path();
            let _ = load_or_generate_wg_private_key(p).unwrap();
            let perms = std::fs::metadata(wg_key_path(p)).unwrap().permissions();
            assert_eq!(perms.mode() & 0o777, 0o600);
        }
    }

    #[test]
    fn wg_key_rejects_wrong_length() {
        let dir = tempdir().unwrap();
        let p = dir.path();
        std::fs::write(
            wg_key_path(p),
            base64::engine::general_purpose::STANDARD.encode([0u8; 16]),
        )
        .unwrap();
        // `.unwrap_err()` would require Ok-side (StaticSecret) : Debug
        // which boringtun deliberately suppresses to keep private
        // key bytes out of logs. Use `.err()` + `expect` instead.
        let err = load_or_generate_wg_private_key(p)
            .err()
            .expect("16-byte key file must be rejected");
        assert!(err.to_string().contains("32 bytes"));
    }

    #[tokio::test]
    async fn spawn_vpn_modules_returns_none_when_disabled() {
        let dir = tempdir().unwrap();
        let mut cfg = DaemonConfig::default();
        cfg.state_dir = dir.path().to_path_buf();
        cfg.provider_id = "provider-uuid".into();
        assert!(cfg.vpn.vpn_svc_url.is_empty());
        let handles = spawn_vpn_modules(&cfg).await;
        assert!(handles.is_none(), "no vpn_svc_url → no VPN spawn");
    }

    #[tokio::test]
    async fn spawn_vpn_modules_returns_none_when_unpaired() {
        let dir = tempdir().unwrap();
        let mut cfg = DaemonConfig::default();
        cfg.state_dir = dir.path().to_path_buf();
        cfg.provider_id = "".into(); // unpaired
        cfg.vpn = VpnConfig {
            vpn_svc_url: "https://api.iogrid.org".into(),
            vpn_listen_addr: "127.0.0.1:0".into(),
            stun_server: "stun.iogrid.org:3478".into(),
            region: "us-east-1".into(),
        };
        let handles = spawn_vpn_modules(&cfg).await;
        assert!(handles.is_none(), "empty provider_id → no VPN spawn");
    }
}
