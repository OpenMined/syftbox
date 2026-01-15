use std::path::PathBuf;
use std::sync::Arc;
use std::thread;

use anyhow::{Context, Result};

use crate::client::{Client, ClientStartOptions};
use crate::config::{Config, ConfigOverrides};
use crate::control::ControlPlane;
use crate::filters::SyncFilters;
use crate::http::ApiClient;
use crate::telemetry::HttpStats;

#[derive(Debug, Clone)]
pub struct DaemonOptions {
    pub http_addr: Option<String>,
    pub http_token: Option<String>,
    pub healthz_max_attempts: Option<usize>,
    pub log_path: Option<PathBuf>,
}

impl Default for DaemonOptions {
    fn default() -> Self {
        Self {
            http_addr: None,
            http_token: None,
            healthz_max_attempts: Some(60),
            log_path: None,
        }
    }
}

pub struct ThreadedDaemonHandle {
    shutdown: std::sync::mpsc::Sender<()>,
    join: Option<std::thread::JoinHandle<Result<()>>>,
}

impl ThreadedDaemonHandle {
    pub fn stop(mut self) -> Result<()> {
        let _ = self.shutdown.send(());
        if let Some(join) = self.join.take() {
            match join.join() {
                Ok(res) => res,
                Err(_) => anyhow::bail!("syftbox daemon thread panicked"),
            }
        } else {
            Ok(())
        }
    }
}

/// Run the SyftBox Rust daemon on the *current* tokio runtime until `shutdown` is notified.
pub async fn run_daemon_with_shutdown(
    cfg: Config,
    opts: DaemonOptions,
    shutdown: Arc<tokio::sync::Notify>,
) -> Result<()> {
    let mut cfg = cfg;

    let (http_addr, http_token) = prepare_control_plane(&mut cfg, opts.http_addr, opts.http_token)?;

    let log_path = opts.log_path.unwrap_or_else(|| daemon_log_path(&cfg));
    crate::logging::init_log_file(&log_path)?;
    crate::logging::info(format!(
        "daemon start version={} config={} log={}",
        env!("CARGO_PKG_VERSION"),
        cfg.config_path
            .as_ref()
            .map(|p| p.display().to_string())
            .unwrap_or_default(),
        log_path.display()
    ));

    // Match Go client behavior: persist chosen control-plane settings on startup (without persisting access token).
    cfg.save()?;

    let http_stats = std::sync::Arc::new(HttpStats::default());
    let api = ApiClient::new(
        &cfg.server_url,
        &cfg.email,
        cfg.access_token.as_deref(),
        cfg.refresh_token.as_deref(),
        cfg.config_path.as_deref(),
        http_stats.clone(),
    )?;

    let control_result = ControlPlane::start(
        &http_addr,
        Some(http_token),
        http_stats,
        Some(shutdown.clone()),
    )?;

    let control = control_result.control_plane;
    let actual_addr = control_result.bound_addr;

    // Update config with actual bound address (important if we fell back to a different port)
    let actual_client_url = format!("http://{}", actual_addr);
    let configured_url = cfg.client_url.clone().unwrap_or_default();
    if configured_url != actual_client_url {
        crate::logging::info_kv(
            "control plane bound to different port than configured",
            &[
                ("configured", &configured_url),
                ("actual", &actual_client_url),
            ],
        );
        cfg.client_url = Some(actual_client_url);
        // Save updated config so external clients know the actual port
        if let Err(e) = cfg.save() {
            crate::logging::error(format!(
                "failed to save updated config with actual control plane address: {}",
                e
            ));
        }
    }

    // TODO: wire websocket events; keep None until implemented.
    let datasites_root = cfg.data_dir.join("datasites");
    let filters = std::sync::Arc::new(SyncFilters::load(&datasites_root)?);

    let mut client = Client::new(cfg, api, filters, None, Some(control));
    client
        .start_with_shutdown(
            shutdown,
            ClientStartOptions {
                healthz_max_attempts: opts.healthz_max_attempts,
            },
        )
        .await?;
    Ok(())
}

/// Start a SyftBox Rust daemon in a dedicated background thread (with its own tokio runtime).
///
/// This is designed for embedding in other Rust applications that don't want to
/// own SyftBox's async lifecycle directly.
pub fn start_threaded(cfg: Config, opts: DaemonOptions) -> Result<ThreadedDaemonHandle> {
    let (shutdown_tx, shutdown_rx) = std::sync::mpsc::channel::<()>();
    let join = thread::Builder::new()
        .name("syftbox-rs-daemon".to_string())
        .spawn(move || {
            let rt = tokio::runtime::Builder::new_multi_thread()
                .enable_all()
                .worker_threads(2)
                .build()
                .context("build tokio runtime")?;

            rt.block_on(async move {
                let shutdown = Arc::new(tokio::sync::Notify::new());
                let shutdown_task = shutdown.clone();
                tokio::task::spawn_blocking(move || {
                    let _ = shutdown_rx.recv();
                    shutdown_task.notify_waiters();
                });

                run_daemon_with_shutdown(cfg, opts, shutdown).await
            })
        })
        .context("spawn syftbox daemon thread")?;

    Ok(ThreadedDaemonHandle {
        shutdown: shutdown_tx,
        join: Some(join),
    })
}

/// Convenience: load config with overrides (matching the CLI's precedence rules)
/// and then start a background daemon thread.
pub fn start_threaded_from_config_path(
    config_path: &std::path::Path,
    overrides: ConfigOverrides,
    opts: DaemonOptions,
) -> Result<ThreadedDaemonHandle> {
    let cfg = Config::load_with_overrides(config_path, overrides)?;
    start_threaded(cfg, opts)
}

fn daemon_log_path(cfg: &Config) -> PathBuf {
    if let Some(p) = cfg.config_path.as_ref().and_then(|p| p.parent()) {
        return p.join("logs").join("syftbox.log");
    }
    cfg.data_dir
        .join(".syftbox")
        .join("logs")
        .join("syftbox.log")
}

fn prepare_control_plane(
    cfg: &mut Config,
    http_addr: Option<String>,
    http_token_flag: Option<String>,
) -> Result<(String, String)> {
    let http_addr = http_addr
        .or_else(|| {
            cfg.client_url
                .as_deref()
                .and_then(client_url_to_addr)
                .or_else(|| Some("127.0.0.1:7938".to_string()))
        })
        .unwrap();
    let http_addr = http_addr.trim().to_string();
    if http_addr.is_empty() {
        anyhow::bail!("http_addr is empty");
    }

    let token = http_token_flag
        .filter(|t| !t.trim().is_empty())
        .or_else(|| cfg.client_token.clone())
        .unwrap_or_default();
    let token = if token.trim().is_empty() {
        uuid::Uuid::new_v4().as_simple().to_string()
    } else {
        token
    };

    cfg.client_url = Some(format!("http://{http_addr}"));
    cfg.client_token = Some(token.clone());

    Ok((http_addr, token))
}

fn client_url_to_addr(client_url: &str) -> Option<String> {
    let u = client_url.trim();
    if u.is_empty() {
        return None;
    }
    let parsed = url::Url::parse(u).ok()?;
    let host = parsed.host_str()?;
    let port = parsed.port().unwrap_or(7938);
    Some(format!("{host}:{port}"))
}
