mod client;
mod config;
mod control;
mod events;
mod filters;
mod http;
mod sync;

use std::path::PathBuf;

use anyhow::Result;
use clap::{Parser, Subcommand};
use config::Config;
use control::ControlPlane;
use filters::SyncFilters;
use http::ApiClient;

#[derive(Parser, Debug)]
#[command(name = "syftbox", version)]
struct Cli {
    /// Path to config file
    #[arg(short = 'c', long = "config")]
    config: PathBuf,

    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand, Debug)]
enum Commands {
    /// Run the client daemon
    Daemon {
        /// Local HTTP address (for compatibility, currently unused)
        #[arg(long = "http-addr", default_value = "127.0.0.1:7938")]
        _http_addr: String,
    },
}

#[tokio::main]
async fn main() -> Result<()> {
    let cli = Cli::parse();
    let cfg = Config::load(&cli.config)?;

    match cli.command {
        Commands::Daemon { _http_addr: _ } => {
            run_daemon(cfg).await?;
        }
    }

    Ok(())
}

async fn run_daemon(cfg: Config) -> Result<()> {
    let api = ApiClient::new(&cfg.server_url, &cfg.email, cfg.access_token.as_deref())?;

    let client_addr = cfg
        .client_url
        .as_deref()
        .unwrap_or("http://127.0.0.1:0")
        .replace("http://", "")
        .replace("https://", "");
    let control = ControlPlane::start(&client_addr)?;

    // TODO: wire websocket events; keep None until implemented.
    let datasites_root = cfg.data_dir.join("datasites");
    let filters = std::sync::Arc::new(SyncFilters::load(&datasites_root)?);

    let mut client = client::Client::new(cfg, api, filters, None, Some(control));
    client.start().await?;
    Ok(())
}
