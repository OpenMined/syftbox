mod apps;
mod auth;
mod client;
mod config;
mod control;
mod events;
mod filters;
mod http;
mod logging;
mod login;
mod sync;
mod telemetry;
mod uploader;
mod workspace;
mod wsproto;

use std::path::PathBuf;

use anyhow::Result;
use clap::{Parser, Subcommand};
use config::{Config, ConfigOverrides};
use control::ControlPlane;
use filters::SyncFilters;
use http::ApiClient;
use telemetry::HttpStats;

#[derive(Parser, Debug)]
#[command(name = "syftbox", version)]
struct Cli {
    /// Path to config file
    #[arg(short = 'c', long = "config")]
    config: Option<PathBuf>,

    /// Email override (takes precedence over env/config)
    #[arg(long = "email")]
    email: Option<String>,

    /// Data directory override (takes precedence over env/config)
    #[arg(long = "datadir")]
    datadir: Option<PathBuf>,

    /// Server URL override (takes precedence over env/config)
    #[arg(long = "server")]
    server: Option<String>,

    /// Control plane URL override (takes precedence over env/config)
    #[arg(long = "client-url")]
    client_url: Option<String>,

    /// Control plane token override (takes precedence over env/config)
    #[arg(long = "client-token")]
    client_token: Option<String>,

    #[command(subcommand)]
    command: Option<Commands>,
}

#[derive(Subcommand, Debug)]
enum Commands {
    /// Login to the syftbox datasite (partial parity)
    #[command(alias = "init")]
    Login {
        /// Disable output (matches Go `syftbox login --quiet`)
        #[arg(short = 'q', long = "quiet", default_value_t = false)]
        quiet: bool,
    },

    /// Run the client daemon
    Daemon {
        /// Address to bind the local http server
        #[arg(short = 'a', long = "http-addr", default_value = "localhost:7938")]
        http_addr: String,

        /// Access token for the local http server
        #[arg(short = 't', long = "http-token", default_value = "")]
        http_token: String,

        /// Enable Swagger for the local http server (accepted for parity, currently unused)
        #[arg(
            short = 's',
            long = "http-swagger",
            default_value_t = true,
            default_missing_value = "true",
            num_args = 0..=1,
            value_parser = clap::value_parser!(bool)
        )]
        http_swagger: bool,
    },

    /// Print the resolved config file path
    ConfigPath,

    /// Print version information
    Version,

    /// Continuously poll local control plane /v1/status
    WatchStatus {
        /// Poll interval (e.g. 1s, 250ms)
        #[arg(long = "interval", default_value = "1s")]
        interval: String,

        /// Print raw json without pretty formatting
        #[arg(long = "raw", default_value_t = false)]
        raw: bool,
    },

    /// Manage SyftBox apps
    App {
        #[command(subcommand)]
        command: AppCommands,
    },
}

#[derive(Subcommand, Debug)]
enum AppCommands {
    /// List installed apps
    List,

    /// Install an app (local paths supported; URL installs are not yet implemented)
    Install {
        /// URL or local path
        uri: String,

        /// Branch to install from
        #[arg(long = "branch", default_value = "main")]
        branch: String,

        /// Tag to install from
        #[arg(long = "tag", default_value = "")]
        tag: String,

        /// Commit hash to install from
        #[arg(long = "commit", default_value = "")]
        commit: String,

        /// Force install
        #[arg(short = 'f', long = "force", default_value_t = false)]
        force: bool,

        /// Use git to install
        #[arg(short = 'g', long = "use-git", default_value_t = true)]
        use_git: bool,
    },

    /// Uninstall an app by ID or URI
    Uninstall { uri: String },
}

#[tokio::main]
async fn main() -> Result<()> {
    let Cli {
        config,
        email,
        datadir,
        server,
        client_url,
        client_token,
        command,
    } = Cli::parse();
    let resolved_config = Config::resolve_config_path(config.as_deref());

    match command {
        Some(Commands::Login { quiet }) => {
            let requested_server = server
                .clone()
                .unwrap_or_else(|| Config::default_server_url().to_string());
            login::run_login(login::LoginArgs {
                config_path: resolved_config,
                server_url: requested_server,
                data_dir: datadir.unwrap_or_else(Config::default_data_dir),
                email,
                quiet,
            })
            .await
        }
        Some(Commands::App { command }) => {
            run_app(
                command,
                email,
                datadir,
                server,
                client_url,
                client_token,
                resolved_config,
            )
            .await
        }
        Some(Commands::ConfigPath) => {
            println!("{}", resolved_config.display());
            Ok(())
        }
        Some(Commands::Version) => {
            println!("{}", detailed_version());
            Ok(())
        }
        Some(Commands::WatchStatus { interval, raw }) => {
            run_watch_status(interval, raw, client_url, client_token, resolved_config).await
        }
        Some(Commands::Daemon {
            http_addr,
            http_token,
            http_swagger: _,
        }) => {
            let overrides = ConfigOverrides {
                email,
                data_dir: datadir,
                server_url: server,
                client_url,
                client_token,
            };
            let cfg = Config::load_with_overrides(&resolved_config, overrides)?;
            run_daemon(cfg, http_addr, http_token).await?;
            Ok(())
        }
        None => {
            // Match Go behavior: `syftbox` with no subcommand runs the daemon.
            let overrides = ConfigOverrides {
                email,
                data_dir: datadir,
                server_url: server,
                client_url,
                client_token,
            };
            let cfg = Config::load_with_overrides(&resolved_config, overrides)?;
            let http_addr = cfg
                .client_url
                .as_deref()
                .and_then(client_url_to_addr)
                .unwrap_or_else(|| "127.0.0.1:7938".to_string());
            run_daemon(cfg, http_addr, String::new()).await?;
            Ok(())
        }
    }
}

async fn run_daemon(cfg: Config, http_addr: String, http_token: String) -> Result<()> {
    let mut cfg = cfg;
    let (http_addr, http_token) = prepare_control_plane(&mut cfg, &http_addr, &http_token)?;

    let log_path = crate::logging::init_default_log_file()?;
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
        http_stats.clone(),
    )?;

    let control = ControlPlane::start(&http_addr, Some(http_token), http_stats)?;

    // TODO: wire websocket events; keep None until implemented.
    let datasites_root = cfg.data_dir.join("datasites");
    let filters = std::sync::Arc::new(SyncFilters::load(&datasites_root)?);

    let mut client = client::Client::new(cfg, api, filters, None, Some(control));
    client.start().await?;
    Ok(())
}

fn prepare_control_plane(
    cfg: &mut Config,
    http_addr: &str,
    http_token_flag: &str,
) -> Result<(String, String)> {
    let http_addr = http_addr.trim();
    if http_addr.is_empty() {
        anyhow::bail!("http_addr is empty");
    }

    let token = if !http_token_flag.trim().is_empty() {
        http_token_flag.trim().to_string()
    } else {
        cfg.client_token.clone().unwrap_or_default()
    };
    let token = if token.trim().is_empty() {
        uuid::Uuid::new_v4().as_simple().to_string()
    } else {
        token
    };

    cfg.client_url = Some(format!("http://{http_addr}"));
    cfg.client_token = Some(token.clone());

    Ok((http_addr.to_string(), token))
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

fn detailed_version() -> String {
    let version = env!("CARGO_PKG_VERSION");
    let revision = option_env!("SYFTBOX_REVISION").unwrap_or("HEAD");
    let build_date = option_env!("SYFTBOX_BUILD_DATE").unwrap_or("");
    format!(
        "{} ({}; rust; {}/{}; {})",
        version,
        revision,
        std::env::consts::OS,
        std::env::consts::ARCH,
        build_date
    )
}

#[cfg(test)]
mod control_plane_tests {
    use super::*;

    #[test]
    fn prepare_control_plane_generates_token_and_persists_url() {
        let tmp = std::env::temp_dir().join("syftbox-rs-prepare-control-plane");
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();

        let cfg_path = tmp.join("config.json");
        std::fs::write(
            &cfg_path,
            format!(
                r#"{{
                  "email":"alice@example.com",
                  "data_dir":"{}",
                  "server_url":"https://syftbox.net"
                }}"#,
                tmp.join("data").display()
            ),
        )
        .unwrap();

        let mut cfg = Config::load_with_overrides(&cfg_path, ConfigOverrides::default()).unwrap();
        cfg.client_token = None;
        cfg.client_url = None;

        let (addr, token) = prepare_control_plane(&mut cfg, "127.0.0.1:7938", "").unwrap();
        assert_eq!(addr, "127.0.0.1:7938");
        assert!(!token.is_empty());
        assert_eq!(cfg.client_url.as_deref(), Some("http://127.0.0.1:7938"));
        assert_eq!(cfg.client_token.as_deref(), Some(token.as_str()));

        let (_, explicit) = prepare_control_plane(&mut cfg, "127.0.0.1:7938", "explicit").unwrap();
        assert_eq!(explicit, "explicit");
        assert_eq!(cfg.client_token.as_deref(), Some("explicit"));
    }

    #[test]
    fn root_cli_allows_no_subcommand() {
        let cli = Cli::try_parse_from(["syftbox"]).unwrap();
        assert!(cli.command.is_none());
    }
}

async fn run_watch_status(
    interval: String,
    raw: bool,
    client_url: Option<String>,
    client_token: Option<String>,
    config_path: PathBuf,
) -> Result<()> {
    let overrides = ConfigOverrides {
        email: None,
        data_dir: None,
        server_url: None,
        client_url,
        client_token,
    };

    let (client_url, client_token) = Config::load_control_plane_settings(&config_path, &overrides)?;
    let client_url = client_url.unwrap_or_default();
    let client_token = client_token.unwrap_or_default();
    if client_url.trim().is_empty() || client_token.trim().is_empty() {
        anyhow::bail!("client control plane not configured; set --client-url/--client-token or SYFTBOX_CLIENT_URL/SYFTBOX_CLIENT_TOKEN");
    }

    let poll_every = parse_duration(&interval)?;
    let status_url = format!("{}/v1/status", client_url.trim_end_matches('/'));
    let http = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(5))
        .build()?;

    let mut ticker = tokio::time::interval(poll_every);
    loop {
        tokio::select! {
            _ = tokio::signal::ctrl_c() => return Ok(()),
            _ = ticker.tick() => {
                let resp = http
                    .get(&status_url)
                    .header("Authorization", format!("Bearer {client_token}"))
                    .send()
                    .await;
                let resp = match resp {
                    Ok(r) => r,
                    Err(e) => {
                        eprintln!("{} ERROR {}", chrono::Utc::now().to_rfc3339(), e);
                        continue;
                    }
                };
                let body = match resp.bytes().await {
                    Ok(b) => b,
                    Err(e) => {
                        eprintln!("{} ERROR {}", chrono::Utc::now().to_rfc3339(), e);
                        continue;
                    }
                };
                if raw {
                    println!("{}", String::from_utf8_lossy(&body));
                    continue;
                }
                let parsed: serde_json::Value = match serde_json::from_slice(&body) {
                    Ok(v) => v,
                    Err(_) => {
                        println!("{}", String::from_utf8_lossy(&body));
                        continue;
                    }
                };
                println!("{}", serde_json::to_string_pretty(&parsed).unwrap_or_else(|_| String::from_utf8_lossy(&body).to_string()));
            }
        }
    }
}

async fn run_app(
    command: AppCommands,
    email: Option<String>,
    datadir: Option<PathBuf>,
    server: Option<String>,
    client_url: Option<String>,
    client_token: Option<String>,
    config_path: PathBuf,
) -> Result<()> {
    let overrides = ConfigOverrides {
        email,
        data_dir: datadir,
        server_url: server,
        client_url,
        client_token,
    };
    let cfg = Config::load_with_overrides(&config_path, overrides)?;

    match command {
        AppCommands::List => {
            let apps = apps::list_apps(&cfg)?;
            let out = apps::format_app_list(&apps::apps_dir(&cfg), &apps);
            print!("{out}");
        }
        AppCommands::Install {
            uri,
            branch,
            tag,
            commit,
            force,
            use_git,
            ..
        } => {
            let p = PathBuf::from(&uri);
            if p.is_dir() {
                let app = apps::install_from_path(&p, &cfg, force)?;
                print!("{}", apps::format_install_result(&app));
            } else if uri.starts_with("http://") || uri.starts_with("https://") {
                // For now: implement archive installs (matches Go when `--use-git=false` or git unavailable).
                let _ = use_git;
                let app = apps::install_from_url(&uri, &cfg, &branch, &tag, &commit, force).await?;
                print!("{}", apps::format_install_result(&app));
            } else {
                anyhow::bail!("invalid url or path {:?}", p);
            }
        }
        AppCommands::Uninstall { uri } => {
            let id = apps::uninstall_app(&cfg, &uri)?;
            print!("{}", apps::format_uninstall_result(&id));
        }
    }
    Ok(())
}

fn parse_duration(raw: &str) -> Result<std::time::Duration> {
    let s = raw.trim();
    if s.is_empty() {
        anyhow::bail!("invalid duration: empty");
    }
    let (num, unit) = if let Some(v) = s.strip_suffix("ms") {
        (v, "ms")
    } else if let Some(v) = s.strip_suffix('s') {
        (v, "s")
    } else {
        // default seconds if no unit
        (s, "s")
    };
    let value: u64 = num
        .parse()
        .map_err(|_| anyhow::anyhow!("invalid duration: {raw}"))?;
    Ok(match unit {
        "ms" => std::time::Duration::from_millis(value),
        "s" => std::time::Duration::from_secs(value),
        _ => std::time::Duration::from_secs(value),
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn daemon_cli_parses_flags() {
        let cli = Cli::try_parse_from([
            "syftbox",
            "-c",
            "config.json",
            "daemon",
            "-a",
            "127.0.0.1:7938",
            "-t",
            "token123",
            "--http-swagger=false",
        ])
        .unwrap();

        match cli.command {
            Some(Commands::Daemon {
                http_addr,
                http_token,
                http_swagger,
            }) => {
                assert_eq!(http_addr, "127.0.0.1:7938");
                assert_eq!(http_token, "token123");
                assert!(!http_swagger);
            }
            _ => panic!("expected daemon command"),
        }
    }

    #[test]
    fn watch_status_cli_parses_flags() {
        let cli = Cli::try_parse_from(["syftbox", "watch-status", "--interval", "250ms", "--raw"])
            .unwrap();
        match cli.command {
            Some(Commands::WatchStatus { interval, raw }) => {
                assert_eq!(interval, "250ms");
                assert!(raw);
            }
            _ => panic!("expected watch-status"),
        }
    }

    #[test]
    fn login_cli_supports_init_alias() {
        let cli = Cli::try_parse_from(["syftbox", "init", "--quiet"]).unwrap();
        match cli.command {
            Some(Commands::Login { quiet }) => assert!(quiet),
            _ => panic!("expected login via init alias"),
        }
    }

    #[test]
    fn parse_duration_accepts_ms_and_s() {
        assert_eq!(
            parse_duration("250ms").unwrap(),
            std::time::Duration::from_millis(250)
        );
        assert_eq!(
            parse_duration("2s").unwrap(),
            std::time::Duration::from_secs(2)
        );
        assert_eq!(
            parse_duration("2").unwrap(),
            std::time::Duration::from_secs(2)
        );
        assert!(parse_duration("").is_err());
    }
}
