//! iogridd binary entry point.
//!
//! Subcommands:
//!  * `iogridd` (no subcommand)   — run as the supervised long-lived daemon.
//!  * `iogridd pair <token>`      — exchange a pairing token for a fresh
//!    mTLS identity bundle, then exit.
//!  * `iogridd status`            — GET 127.0.0.1:7777/state and print.
//!  * `iogridd stop`              — send SIGTERM to the running daemon.
//!  * `iogridd uninstall`         — remove service unit + state dir.
//!  * `iogridd version`           — print version + commit + target triple.

use std::path::PathBuf;

use anyhow::{Context, Result};
use clap::{Parser, Subcommand};
use iogrid_core::{
    default_install_layout, init_tracing, target_triple, updater, DaemonConfig, Supervisor,
};
use std::sync::Arc;

#[derive(Parser)]
#[command(name = "iogridd", version, about = "iogrid provider daemon")]
struct Cli {
    /// State dir (defaults to ~/.iogrid).
    #[arg(long, env = "IOGRID_STATE_DIR")]
    state_dir: Option<PathBuf>,

    #[command(subcommand)]
    command: Option<Cmd>,
}

#[derive(Subcommand)]
enum Cmd {
    /// One-time pairing — exchange a token for a mTLS identity bundle.
    Pair {
        /// Pairing token displayed in the web UI.
        token: String,
        /// Optional coordinator URL override.
        #[arg(long)]
        coordinator_url: Option<String>,
    },
    /// Print the daemon's current state (GET /state on the local UI bridge).
    Status,
    /// Print an operator-pasteable diagnostic bundle: state-dir contents,
    /// config snapshot, pair status, bearer presence, recent log tail,
    /// heartbeat recency. The operator (or an SSH-via-tunnel session)
    /// pastes the output to the issue/comment so the platform side can
    /// diagnose why the daemon stopped reporting without round-tripping
    /// through the operator for each follow-up question. Refs #479.
    Diag {
        /// Pretty-print as JSON instead of the default human-readable
        /// format. JSON is easier to forward into a tracker / logging
        /// pipeline but harder to skim by eye.
        #[arg(long)]
        json: bool,
    },
    /// Stop the running daemon.
    Stop,
    /// Uninstall the daemon (service unit + state dir).
    Uninstall,
    /// Print version + target.
    Version,
    /// Auto-update subcommands.
    Update {
        /// Poll the manifest server once and print the outcome.
        #[arg(long, conflicts_with_all = ["apply", "rollback"])]
        check: bool,
        /// Apply a previously-staged update (rename `iogridd.new` over
        /// `iogridd`, copy old to `iogridd.old`).
        #[arg(long, conflicts_with_all = ["check", "rollback"])]
        apply: bool,
        /// Restore the previous binary (`iogridd.old` → `iogridd`).
        #[arg(long, conflicts_with_all = ["check", "apply"])]
        rollback: bool,
    },
}

#[tokio::main]
async fn main() -> Result<()> {
    init_tracing();
    // rustls 0.23 no longer auto-selects a CryptoProvider — it requires the
    // process to install one explicitly. Both `ring` and `aws-lc-rs` are
    // valid; we pin `ring` to match the dev-loop story (no system libcrypto
    // dependency) + because tokio-rustls in our deps already pulls it in.
    // Without this, every TLS handshake (live dispatch + auto-update fetch)
    // panics with "Could not automatically determine the process-level
    // CryptoProvider from Rustls crate features".
    if rustls::crypto::CryptoProvider::get_default().is_none() {
        rustls::crypto::ring::default_provider()
            .install_default()
            .expect("rustls: failed to install default CryptoProvider");
    }
    let cli = Cli::parse();
    let state_dir = cli.state_dir.unwrap_or_else(default_state_dir);

    match cli.command {
        None => run_daemon(&state_dir).await,
        Some(Cmd::Pair {
            token,
            coordinator_url,
        }) => run_pair(&state_dir, &token, coordinator_url).await,
        Some(Cmd::Status) => run_status().await,
        Some(Cmd::Diag { json }) => run_diag(&state_dir, json).await,
        Some(Cmd::Stop) => run_stop().await,
        Some(Cmd::Uninstall) => run_uninstall(&state_dir).await,
        Some(Cmd::Version) => {
            println!(
                "iogridd {} ({} on {})",
                env!("CARGO_PKG_VERSION"),
                option_env!("VERGEN_GIT_SHA").unwrap_or("unknown"),
                std::env::consts::OS,
            );
            Ok(())
        }
        Some(Cmd::Update {
            check,
            apply,
            rollback,
        }) => run_update(&state_dir, check, apply, rollback).await,
    }
}

fn default_state_dir() -> PathBuf {
    std::env::var_os("HOME")
        .or_else(|| std::env::var_os("USERPROFILE"))
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("/var/lib/iogrid"))
        .join(".iogrid")
}

async fn run_daemon(state_dir: &std::path::Path) -> Result<()> {
    tracing::info!(version = env!("CARGO_PKG_VERSION"), "starting iogridd");
    let config = DaemonConfig::load_or_init(state_dir).context("load daemon config")?;
    let supervisor = Supervisor::new(config);
    supervisor.run().await
}

/// CSR + keypair generated locally by the daemon at pairing time.
///
/// The PEM-encoded private key stays on disk (`~/.iogrid/key.pem`); only
/// the CSR PEM travels over the wire to providers-svc. Without this the
/// daemon used to ship a placeholder string as the private key — see
/// #235.
struct LocalPairingKey {
    csr_pem: String,
    key_pem: String,
}

/// Generate a fresh ECDSA-P256 keypair + matching PKCS#10 CSR.
///
/// The CSR's subject CommonName is intentionally a placeholder
/// ("daemon-pair-pending"): providers-svc rewrites the issued
/// certificate's CN to the real provider id (it does not echo the CSR's
/// subject back). Extracted so the round-trip test below can run without
/// touching the network.
fn mint_local_pairing_key() -> Result<LocalPairingKey> {
    let key_pair = rcgen::KeyPair::generate_for(&rcgen::PKCS_ECDSA_P256_SHA256)
        .context("rcgen: generate ECDSA P-256 keypair")?;
    let mut params = rcgen::CertificateParams::new(Vec::<String>::new())
        .context("rcgen: create CertificateParams")?;
    params.distinguished_name = rcgen::DistinguishedName::new();
    params
        .distinguished_name
        .push(rcgen::DnType::CommonName, "daemon-pair-pending");
    let csr = params
        .serialize_request(&key_pair)
        .context("rcgen: serialize CSR")?;
    Ok(LocalPairingKey {
        csr_pem: csr.pem().context("rcgen: encode CSR PEM")?,
        key_pem: key_pair.serialize_pem(),
    })
}

async fn run_pair(
    state_dir: &std::path::Path,
    token: &str,
    coordinator_url: Option<String>,
) -> Result<()> {
    let mut cfg = DaemonConfig::load_or_init(state_dir)?;
    if let Some(u) = coordinator_url {
        cfg.coordinator_url = u;
    }
    // Persist so the supervisor picks up the URL on its next launch.
    cfg.save()?;

    // Mint a local ECDSA-P256 keypair + CSR BEFORE the network call so
    // that a server-side failure leaves nothing half-written on disk.
    let local = mint_local_pairing_key().context("generate local keypair")?;

    let url = format!("{}/api/v1/providers/pair", cfg.coordinator_url);
    let client = iogrid_transport::identity::PairingClient { pair_endpoint: url };
    // Self-report the OS hostname as our preferred display_name. The
    // coordinator uses it as the operator-visible label AND as the
    // dedupe key on re-pair (owner_user_id + display_name): a fresh
    // pair on the same machine UPDATES the existing row instead of
    // INSERTing a duplicate, so /admin/providers stops accumulating
    // ghost rows after every daemon reinstall. Empty string -> server
    // falls back to `provider-<short-id>` (legacy behaviour preserved).
    let display_name = iogrid_transport::identity::local_display_name();
    let req = iogrid_transport::identity::PairingRequest {
        pairing_token: token.to_string(),
        csr_pem: local.csr_pem.clone(),
        display_name,
    };
    match client.pair(req, state_dir).await {
        Ok(resp) => {
            cfg.provider_id = resp.provider_id.clone();
            cfg.save()?;
            // Persist the signed cert + the LOCAL private key. The key
            // PEM never travels over the wire — only the CSR does.
            let bundle = iogrid_transport::identity::IdentityBundle {
                cert_pem: resp.cert_pem.into_bytes(),
                key_pem: local.key_pem.into_bytes(),
            };
            bundle.save(state_dir).context("persist identity bundle")?;
            println!("paired: provider_id={}", resp.provider_id);
            Ok(())
        }
        Err(e) => {
            // Pairing is allowed to fail (e.g. running offline tests). Don't
            // crash the CLI — surface the error and exit non-zero so scripts
            // can detect.
            eprintln!("pairing failed: {e}");
            std::process::exit(1);
        }
    }
}

async fn run_status() -> Result<()> {
    let url = "http://127.0.0.1:7777/state";
    let out = std::process::Command::new("curl")
        .args(["-fsS", url])
        .output();
    match out {
        Ok(o) if o.status.success() => {
            println!("{}", String::from_utf8_lossy(&o.stdout));
            Ok(())
        }
        Ok(o) => {
            eprintln!(
                "curl exit {}: {}",
                o.status.code().unwrap_or(-1),
                String::from_utf8_lossy(&o.stderr)
            );
            std::process::exit(1);
        }
        Err(e) => {
            eprintln!("could not invoke curl: {e}");
            eprintln!("(install curl or query {url} from another HTTP client)");
            std::process::exit(1);
        }
    }
}

/// Diagnostic bundle the `iogridd diag` subcommand emits. Kept stable
/// so JSON consumers (issue auto-paste, monitoring exporters) can rely
/// on the shape. Refs #479.
#[derive(serde::Serialize)]
struct Diag {
    iogridd_version: &'static str,
    target_os: &'static str,
    state_dir: String,
    state_dir_exists: bool,
    /// Contents of state_dir (just file names + sizes) so the operator
    /// can spot a missing key.pem / bearer.txt at a glance.
    state_dir_entries: Vec<DiagEntry>,
    /// Loaded DaemonConfig.coordinator_url + state_dir + provider_id +
    /// caps — small enough to dump inline. Secrets are NOT included
    /// (the keypair lives in key.pem; bearer in bearer.txt — each is
    /// reported as present/absent + size only).
    config_summary: Option<DiagConfig>,
    /// True if state_dir/cert.pem + key.pem both exist + non-empty.
    paired: bool,
    /// True if state_dir/bearer.txt exists + non-empty. (Different
    /// from `paired` — a fresh pair without /pair-handler invocation
    /// is paired-but-no-bearer.)
    bearer_present: bool,
    /// True if `127.0.0.1:7777/state` answered in <500ms.
    ui_bridge_reachable: bool,
    /// True if the daemon process is running on the local box. Best-
    /// effort `pgrep iogridd` minus our own pid.
    daemon_process_running: bool,
    /// Last few lines from the platform-conventional log file. Empty
    /// if the log file doesn't exist yet (fresh install).
    log_tail: Vec<String>,
    log_path_probed: String,
}

#[derive(serde::Serialize)]
struct DiagEntry {
    name: String,
    size_bytes: u64,
}

#[derive(serde::Serialize)]
struct DiagConfig {
    coordinator_url: String,
    provider_id: String,
    bandwidth_cap_gb: u64,
    cpu_cap_pct: u8,
    memory_cap_pct: u8,
    heartbeat_secs: u64,
}

async fn run_diag(state_dir: &std::path::Path, as_json: bool) -> Result<()> {
    let state_dir_exists = state_dir.exists();
    let state_dir_entries = if state_dir_exists {
        std::fs::read_dir(state_dir)
            .map(|rd| {
                rd.filter_map(|e| e.ok())
                    .filter_map(|e| {
                        let meta = e.metadata().ok()?;
                        Some(DiagEntry {
                            name: e.file_name().to_string_lossy().into_owned(),
                            size_bytes: meta.len(),
                        })
                    })
                    .collect()
            })
            .unwrap_or_default()
    } else {
        Vec::new()
    };
    // Loading config is best-effort — a corrupt config shouldn't crash
    // the diag command (that's exactly the kind of state we want to
    // surface).
    let config_summary = DaemonConfig::load_or_init(state_dir).ok().map(|c| DiagConfig {
        coordinator_url: c.coordinator_url,
        provider_id: c.provider_id,
        bandwidth_cap_gb: c.bandwidth_cap_gb,
        cpu_cap_pct: c.cpu_cap_pct,
        memory_cap_pct: c.memory_cap_pct,
        heartbeat_secs: c.heartbeat_secs,
    });
    let cert_path = state_dir.join("cert.pem");
    let key_path = state_dir.join("key.pem");
    let bearer_path = state_dir.join("bearer.txt");
    let paired = file_nonempty(&cert_path) && file_nonempty(&key_path);
    let bearer_present = file_nonempty(&bearer_path);
    // UI-bridge probe — 500ms timeout so a stuck supervisor doesn't
    // hang the diag command.
    let ui_bridge_reachable = std::process::Command::new("curl")
        .args([
            "-fsS",
            "--max-time",
            "1",
            "http://127.0.0.1:7777/state",
        ])
        .output()
        .map(|o| o.status.success())
        .unwrap_or(false);
    // pgrep iogridd minus our own pid → at least one other instance is
    // running. Best-effort; if pgrep is missing we report unknown.
    let daemon_process_running = std::process::Command::new("pgrep")
        .arg("iogridd")
        .output()
        .map(|o| {
            String::from_utf8_lossy(&o.stdout)
                .lines()
                .filter_map(|l| l.trim().parse::<u32>().ok())
                .any(|pid| pid != std::process::id())
        })
        .unwrap_or(false);
    // Log tail — platform-specific path. macOS LaunchAgent writes to
    // ~/Library/Logs/iogrid/iogridd.log per the install layout; Linux
    // systemd routes via journald (we'd shell out to journalctl but
    // that needs privilege; skipping); Windows is the Squirrel-managed
    // %LOCALAPPDATA%\iogrid\logs\.
    let log_path = default_log_path();
    let log_tail = std::fs::read_to_string(&log_path)
        .ok()
        .map(|s| s.lines().rev().take(50).map(|l| l.to_string()).collect::<Vec<_>>())
        .map(|mut v| {
            v.reverse();
            v
        })
        .unwrap_or_default();
    let bundle = Diag {
        iogridd_version: env!("CARGO_PKG_VERSION"),
        target_os: std::env::consts::OS,
        state_dir: state_dir.display().to_string(),
        state_dir_exists,
        state_dir_entries,
        config_summary,
        paired,
        bearer_present,
        ui_bridge_reachable,
        daemon_process_running,
        log_tail,
        log_path_probed: log_path.display().to_string(),
    };
    if as_json {
        println!("{}", serde_json::to_string_pretty(&bundle)?);
    } else {
        print_diag_human(&bundle);
    }
    Ok(())
}

fn file_nonempty(p: &std::path::Path) -> bool {
    std::fs::metadata(p).map(|m| m.len() > 0).unwrap_or(false)
}

fn default_log_path() -> PathBuf {
    if let Ok(home) = std::env::var("HOME") {
        let mac = std::path::Path::new(&home)
            .join("Library/Logs/iogrid/iogridd.log");
        if mac.exists() {
            return mac;
        }
        let linux = std::path::Path::new(&home).join(".iogrid/iogridd.log");
        if linux.exists() {
            return linux;
        }
    }
    if let Ok(la) = std::env::var("LOCALAPPDATA") {
        let win = std::path::Path::new(&la)
            .join("iogrid")
            .join("logs")
            .join("iogridd.log");
        return win;
    }
    PathBuf::from("/var/log/iogridd.log")
}

fn print_diag_human(b: &Diag) {
    println!("iogridd diag — {} ({})", b.iogridd_version, b.target_os);
    println!();
    println!(
        "state_dir:  {} (exists={})",
        b.state_dir, b.state_dir_exists
    );
    for e in &b.state_dir_entries {
        println!("  {:<24} {:>10} bytes", e.name, e.size_bytes);
    }
    println!();
    if let Some(c) = &b.config_summary {
        println!("coordinator_url:    {}", c.coordinator_url);
        println!(
            "provider_id:        {}",
            if c.provider_id.is_empty() {
                "(unpaired)"
            } else {
                &c.provider_id
            }
        );
        println!("bandwidth_cap_gb:   {}", c.bandwidth_cap_gb);
        println!("cpu/mem_cap_pct:    {}/{}", c.cpu_cap_pct, c.memory_cap_pct);
        println!("heartbeat_secs:     {}", c.heartbeat_secs);
    } else {
        println!("config:             could not load (corrupt / missing)");
    }
    println!();
    println!("paired (cert+key):  {}", b.paired);
    println!("bearer.txt present: {}", b.bearer_present);
    println!(
        "ui_bridge reachable (127.0.0.1:7777): {}",
        b.ui_bridge_reachable
    );
    println!(
        "daemon process running:               {}",
        b.daemon_process_running
    );
    println!();
    println!("log path probed:    {}", b.log_path_probed);
    if b.log_tail.is_empty() {
        println!("log tail:           (empty or file missing)");
    } else {
        println!("log tail (last {} lines):", b.log_tail.len());
        for line in &b.log_tail {
            println!("  {}", line);
        }
    }
}

async fn run_stop() -> Result<()> {
    #[cfg(unix)]
    {
        // Best-effort: send SIGTERM via pidfile if present, else via pgrep.
        if let Ok(out) = std::process::Command::new("pgrep").arg("iogridd").output() {
            for line in String::from_utf8_lossy(&out.stdout).lines() {
                if let Ok(pid) = line.trim().parse::<i32>() {
                    if pid == std::process::id() as i32 {
                        continue;
                    }
                    let _ = std::process::Command::new("kill")
                        .arg("-TERM")
                        .arg(pid.to_string())
                        .status();
                    println!("sent SIGTERM to pid {pid}");
                }
            }
        }
        Ok(())
    }
    #[cfg(not(unix))]
    {
        // sc stop iogridd on Windows; for other platforms a no-op message.
        let _ = std::process::Command::new("sc.exe")
            .args(["stop", "iogridd"])
            .status();
        Ok(())
    }
}

async fn run_update(
    state_dir: &std::path::Path,
    check: bool,
    apply: bool,
    rollback: bool,
) -> Result<()> {
    let layout = default_install_layout();
    // `check` is accepted explicitly so `iogridd update --check` reads
    // naturally; with no flag we fall through to the same branch.
    let _ = check;

    if rollback {
        let cur = updater::apply_rollback(&layout).context("rollback")?;
        println!("rolled back to {}", cur.display());
        return Ok(());
    }
    if apply {
        let cur = updater::apply_pending(&layout).context("apply pending update")?;
        println!("applied; binary now at {}", cur.display());
        println!(
            "service manager (launchd / systemd / sc.exe) will restart the daemon \
             on its next stop; or run `iogridd stop` to trigger immediately."
        );
        return Ok(());
    }

    // --check (default). Force the worker to run a single iteration
    // regardless of config.disabled — the operator just invoked us.
    let cfg = DaemonConfig::load_or_init(state_dir).context("load daemon config")?;
    let mut updater_cfg = cfg.updater.clone();
    updater_cfg.disabled = false;
    let ctx = updater::PollCtx {
        config: updater_cfg,
        current_version: env!("CARGO_PKG_VERSION").to_string(),
        target: target_triple().to_string(),
        layout,
        fetcher: Arc::new(updater::HttpFetcher::default()),
    };
    match updater::run_one_poll(&ctx).await {
        Ok(o) => {
            println!("{}", serde_json::to_string_pretty(&o)?);
            Ok(())
        }
        Err(e) => {
            eprintln!("update check failed: {e}");
            std::process::exit(1);
        }
    }
}

async fn run_uninstall(state_dir: &std::path::Path) -> Result<()> {
    tracing::warn!(?state_dir, "uninstalling iogridd");
    #[cfg(target_os = "linux")]
    {
        if let Some(p) = iogrid_platform_linux::systemd_unit_path() {
            let _ = std::fs::remove_file(&p);
        }
    }
    #[cfg(target_os = "macos")]
    {
        if let Some(p) = iogrid_platform_mac::launch_agent_path() {
            let _ = std::fs::remove_file(&p);
        }
    }
    #[cfg(target_os = "windows")]
    {
        let _ = iogrid_platform_windows::uninstall_service();
    }
    let _ = std::fs::remove_dir_all(state_dir);
    println!("uninstalled — state dir removed");
    Ok(())
}

#[cfg(test)]
mod pairing_tests {
    use super::*;
    use iogrid_transport::identity::IdentityBundle;

    /// CSR carries the daemon's public key + a self-signature; key.pem
    /// is a real PEM-encoded private key (not the old placeholder
    /// comment string). Round-trip through `IdentityBundle::save/load`
    /// must succeed so that downstream `Identity::from_pem(cert, key)`
    /// in tonic does not abort with "tls configuration failed".
    #[test]
    fn mint_local_pairing_key_emits_real_csr_and_key() {
        let local = mint_local_pairing_key().expect("mint local key");
        assert!(
            local
                .csr_pem
                .starts_with("-----BEGIN CERTIFICATE REQUEST-----"),
            "CSR PEM header missing: {:?}",
            &local.csr_pem[..local.csr_pem.len().min(80)],
        );
        assert!(
            local.csr_pem.contains("-----END CERTIFICATE REQUEST-----"),
            "CSR PEM trailer missing",
        );
        // rcgen 0.13 emits PKCS#8 or SEC1 EC PRIVATE KEY blocks depending
        // on the keypair flavour. Accept either as long as it's a real
        // PEM block — what we explicitly reject is the pre-#235
        // "# key.pem placeholder ..." comment string.
        assert!(
            local.key_pem.starts_with("-----BEGIN"),
            "key.pem must start with a PEM block, got {:?}",
            &local.key_pem[..local.key_pem.len().min(80)],
        );
        assert!(
            !local.key_pem.contains("placeholder"),
            "key.pem must NOT be the legacy placeholder string",
        );

        // The CSR is parseable as PKCS#10 by rustls-pemfile + a minimal
        // ASN.1 sanity check via openssl-free path: we only assert PEM
        // boundaries here; full RFC-2986 validation lives on the
        // providers-svc side (see rest_pair.go CSR parsing).

        // Atomic save + reload round-trip — the exact path the
        // post-pair daemon will exercise on every restart.
        let dir = tempfile::tempdir().expect("tempdir");
        let bundle = IdentityBundle {
            cert_pem: b"-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----\n".to_vec(),
            key_pem: local.key_pem.into_bytes(),
        };
        bundle.save(dir.path()).expect("save bundle");
        let reloaded = IdentityBundle::load(dir.path()).expect("load bundle");
        assert_eq!(reloaded.key_pem, bundle.key_pem);
    }
}
