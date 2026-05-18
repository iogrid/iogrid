//! iogridd binary entry point.
//!
//! Loads config, initialises tracing, hands control to [`iogrid_core::Supervisor`].

use anyhow::Result;
use iogrid_core::{init_tracing, DaemonConfig, Supervisor};

#[tokio::main]
async fn main() -> Result<()> {
    init_tracing();
    tracing::info!(version = env!("CARGO_PKG_VERSION"), "starting iogridd",);
    let config = DaemonConfig::default();
    let supervisor = Supervisor::new(config);
    supervisor.run().await?;
    Ok(())
}
