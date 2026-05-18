//! iogridd binary entry point.
//!
//! CLI dispatcher.
//!
//! * `iogridd` (no args)             → start the daemon supervisor (the
//!   default mode used by LaunchAgent / systemd / Windows Service).
//! * `iogridd run`                   → explicit form of the same; used in
//!   the unit files for clarity.
//! * `iogridd pair --request`        → mint a one-time pairing code and
//!   print it on stdout (consumed by `install.sh` / `install.ps1` /
//!   pkgbuild postinstall to build the onboarding URL).
//! * `iogridd pair --read`           → print the most recently-minted
//!   code from `~/.iogrid/pairing-code.txt`. Exit 1 if absent.
//! * `iogridd uninstall`             → remove the service registration
//!   + binary + config (Phase 1).
//! * `iogridd --version`             → print version and exit.

use anyhow::{bail, Result};
use iogrid_core::pair;
use iogrid_core::{init_tracing, DaemonConfig, Supervisor};

#[tokio::main]
async fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let subcommand = args.first().map(String::as_str);

    match subcommand {
        None | Some("run") => {
            init_tracing();
            tracing::info!(version = env!("CARGO_PKG_VERSION"), "starting iogridd");
            let config = DaemonConfig::default();
            let supervisor = Supervisor::new(config);
            supervisor.run().await?;
        }

        Some("pair") => {
            // Sub-flag: --request (default) | --read
            let flag = args.get(1).map(String::as_str).unwrap_or("--request");
            match flag {
                "--request" => pair::cli_pair_request()?,
                "--read" => {
                    if let Some(code) = pair::PairingCode::from_dotfile() {
                        println!("{}", code);
                    } else {
                        bail!(
                            "no pairing code found at {}",
                            pair::PairingCode::dotfile_path().display()
                        );
                    }
                }
                other => bail!(
                    "unknown 'pair' flag: {}; expected --request | --read",
                    other
                ),
            }
        }

        Some("--version" | "-V" | "version") => {
            println!("iogridd {}", env!("CARGO_PKG_VERSION"));
        }

        Some("uninstall") => {
            // Phase 1 TODO; for now print a friendly message.
            eprintln!("iogridd: uninstall is not yet wired up.");
            eprintln!(
                "         remove the LaunchAgent / systemd unit / Windows Service \
                 manually for now."
            );
            std::process::exit(2);
        }

        Some(other) => {
            bail!(
                "unknown subcommand: {}\nusage: iogridd [run|pair --request|pair --read|uninstall|--version]",
                other
            );
        }
    }
    Ok(())
}
