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
    health, ice, peer_binder, BoringTun, BoringTunConfig, HealthConfig, IceConfig, InnerPacketSink,
    LoggingSink, Meter, PeerBinderConfig, Tunnel,
};
// TUN forward sink is Linux-only — the supervisor falls back to
// LoggingSink on macOS / Windows dev boxes so the daemon still boots.
// Feature is unconditionally on in core/Cargo.toml so the wiring
// compiles everywhere; the actual /dev/net/tun setup is gated inside
// the module by `#[cfg(target_os = "linux")]`. See #529 path c.
#[cfg(target_os = "linux")]
use iogrid_routing::TunForwardSink;

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

/// Path to the persisted standalone-mode provider UUID under the
/// daemon state dir. Created by [`load_or_generate_provider_id`] on
/// first boot of a `--vpn-svc`-only daemon.
pub fn provider_id_path(state_dir: &Path) -> std::path::PathBuf {
    state_dir.join("provider_id")
}

/// Load the persisted standalone provider UUID, or generate + persist
/// a fresh v4 if missing — VPN-544 (#544).
///
/// This is the standalone-mode shortcut around providers-svc pairing:
/// the daemon picks its own UUID, writes it to disk, registers with
/// vpn-svc directly. Subsequent boots reuse the same id so customers
/// keep their bind across daemon restarts.
///
/// File mode is 0644 (not 0600 — the provider id is intentionally
/// public; the customer SDK reads it indirectly when it queries
/// `/v1/vpn/providers/{id}/candidates`).
pub fn load_or_generate_provider_id(state_dir: &Path) -> anyhow::Result<String> {
    let path = provider_id_path(state_dir);
    if path.exists() {
        let body = std::fs::read_to_string(&path)?;
        let trimmed = body.trim();
        // Validate — the file is operator-editable so a typo
        // shouldn't lock the daemon into a broken state silently.
        uuid::Uuid::parse_str(trimmed)
            .map_err(|e| anyhow::anyhow!("invalid UUID at {}: {}", path.display(), e))?;
        return Ok(trimmed.to_owned());
    }
    let new_id = uuid::Uuid::new_v4().to_string();
    std::fs::create_dir_all(state_dir)?;
    std::fs::write(&path, &new_id)?;
    tracing::info!(
        path = %path.display(),
        provider_id = %new_id,
        "generated standalone-mode provider_id (VPN-only); reuse it via --provider-id on next boot or just rely on the persisted file"
    );
    Ok(new_id)
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

    // ---- Inner-packet sink ----
    // On Linux, prefer the TUN-forward sink: it opens /dev/net/tun,
    // configures the inner-tunnel IP, installs an iptables MASQUERADE
    // rule on the WAN interface, and ships decapsulated customer
    // packets to the kernel for real NAT'd egress (the data plane
    // that closes #529 path c and lets the customer `curl ifconfig.me`
    // see the provider's residential IP). If setup fails (no
    // CAP_NET_ADMIN, no iptables on PATH, etc.) or we're not on Linux,
    // fall back to LoggingSink so the daemon still boots — operators
    // see the warning + can fix the host config.
    let meter = Arc::new(Meter::default());
    // Hold the concrete TunForwardSink Arc separately from the
    // dyn-trait Arc passed to BoringTun, so we can call the
    // attach_encapsulator method after start(). Both point at the
    // same allocation — `tun_concrete.clone()` is just a refcount bump.
    #[cfg(target_os = "linux")]
    let mut tun_concrete: Option<Arc<TunForwardSink>> = None;
    let sink: Arc<dyn InnerPacketSink> = {
        #[cfg(target_os = "linux")]
        {
            let wan_iface = if vpn.wan_iface.trim().is_empty() {
                "eth0".to_string()
            } else {
                vpn.wan_iface.trim().to_string()
            };
            let ifname = if vpn.tun_ifname.trim().is_empty() {
                iogrid_routing::DEFAULT_TUN_IFNAME.to_string()
            } else {
                vpn.tun_ifname.trim().to_string()
            };
            match TunForwardSink::new(&ifname, iogrid_routing::PROVIDER_INNER_CIDR, &wan_iface) {
                Ok(tun) => {
                    tracing::info!(
                        ifname = %ifname,
                        wan_iface = %wan_iface,
                        inner = iogrid_routing::PROVIDER_INNER_CIDR,
                        "TunForwardSink ready — decap path will MASQUERADE through kernel"
                    );
                    tun_concrete = Some(tun.clone());
                    tun as Arc<dyn InnerPacketSink>
                }
                Err(e) => {
                    tracing::warn!(
                        error = %e,
                        "TunForwardSink setup failed; falling back to LoggingSink — \
                         end-to-end VPN egress will NOT work until host config is fixed"
                    );
                    Arc::new(LoggingSink) as Arc<dyn InnerPacketSink>
                }
            }
        }
        #[cfg(not(target_os = "linux"))]
        {
            tracing::info!("non-Linux build: VPN data plane uses LoggingSink (no kernel egress)");
            Arc::new(LoggingSink) as Arc<dyn InnerPacketSink>
        }
    };

    // ---- BoringTun ----
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

    // ---- Attach encapsulator to TunForwardSink ----
    // Two-phase setup: BoringTun owns the encapsulator and the sink
    // needs the encapsulator to ship reply packets through the tunnel.
    // After start() binds the UDP socket, hand the sink an Arc so its
    // background read loop can call encapsulate_for_peer.
    #[cfg(target_os = "linux")]
    if let Some(tun) = tun_concrete.as_ref() {
        match boringtun.outbound_encapsulator() {
            Some(enc) => {
                tun.attach_encapsulator(enc);
                tracing::info!("TunForwardSink read loop attached to BoringTun encapsulator");
            }
            None => {
                tracing::warn!("BoringTun encapsulator not yet ready; TUN read loop not started");
            }
        }
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
    let public_ip = if vpn.public_ip.trim().is_empty() {
        None
    } else {
        match vpn.public_ip.trim().parse::<std::net::IpAddr>() {
            Ok(ip) => {
                tracing::info!(public_ip = %ip, "publishing manual public-IP host candidate (#557)");
                Some(ip)
            }
            Err(e) => {
                tracing::warn!(
                    public_ip = %vpn.public_ip,
                    error = %e,
                    "ignoring malformed VPN_PUBLIC_IP / vpn.public_ip"
                );
                None
            }
        }
    };
    let ice_cfg = IceConfig {
        provider_id: config.provider_id.clone(),
        stun_server,
        vpn_svc_base_url: vpn.vpn_svc_url.clone(),
        vpn_listen_addr,
        public_ip,
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
    fn provider_id_persists_across_load_calls() {
        let dir = tempdir().unwrap();
        let p = dir.path();
        let id1 = load_or_generate_provider_id(p).unwrap();
        let id2 = load_or_generate_provider_id(p).unwrap();
        assert_eq!(id1, id2, "second call must reuse the persisted UUID");
        // And the file contains exactly that string.
        let on_disk = std::fs::read_to_string(provider_id_path(p)).unwrap();
        assert_eq!(on_disk.trim(), id1);
    }

    #[test]
    fn provider_id_generated_is_valid_uuid_v4() {
        let dir = tempdir().unwrap();
        let p = dir.path();
        let id = load_or_generate_provider_id(p).unwrap();
        let parsed = uuid::Uuid::parse_str(&id).expect("must be a valid UUID string");
        assert_eq!(
            parsed.get_version_num(),
            4,
            "generated id should be UUID v4"
        );
    }

    #[test]
    fn provider_id_rejects_invalid_persisted_value() {
        let dir = tempdir().unwrap();
        let p = dir.path();
        std::fs::create_dir_all(p).unwrap();
        std::fs::write(provider_id_path(p), "not-a-uuid\n").unwrap();
        let err = load_or_generate_provider_id(p).expect_err("garbage on disk must be rejected");
        assert!(err.to_string().contains("invalid UUID"));
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
