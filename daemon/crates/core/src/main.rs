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
    let url = format!("{}/api/v1/providers/pair", cfg.coordinator_url);
    let client = iogrid_transport::identity::PairingClient { pair_endpoint: url };
    let req = iogrid_transport::identity::PairingRequest {
        pairing_token: token.to_string(),
        csr_pem: String::new(),
    };
    match client.pair(req, state_dir).await {
        Ok(resp) => {
            cfg.provider_id = resp.provider_id.clone();
            cfg.save()?;
            // Persist the cert (the key is supplied by the user-side
            // pairing tool — follow-up PR generates it locally via rcgen).
            let bundle = iogrid_transport::identity::IdentityBundle {
                cert_pem: resp.cert_pem.into_bytes(),
                key_pem: b"# key.pem placeholder - generate locally via rcgen in follow-up PR\n"
                    .to_vec(),
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
