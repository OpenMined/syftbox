use std::io::{self, Write};
use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use serde::Deserialize;

use crate::auth::{request_email_code, validate_token, verify_email_code};
use crate::config::{validate_email, validate_url, Config};

#[derive(Debug, Default, Deserialize)]
struct FileConfig {
    #[serde(default)]
    email: String,
    #[serde(default)]
    server_url: String,
    #[serde(default)]
    client_url: String,
    #[serde(default)]
    refresh_token: String,
}

pub struct LoginArgs {
    pub config_path: PathBuf,
    pub server_url: String,
    pub data_dir: PathBuf,
    pub email: Option<String>,
    pub quiet: bool,
}

pub async fn run_login(args: LoginArgs) -> Result<()> {
    if already_logged_in(&args.config_path, &args.server_url).is_ok() {
        if args.quiet {
            return Ok(());
        }
        let cfg = Config::load_file_only(&args.config_path)?;
        print!("{}", format_already_logged_in(&args.config_path, &cfg));
        return Ok(());
    }

    let http = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()
        .context("build http client")?;

    let email = match args.email {
        Some(e) => e,
        None => prompt_line("Email: ")?,
    };
    validate_email(&email).context("email")?;
    validate_url(&args.server_url).context("server_url")?;

    if !args.quiet {
        eprintln!("Requesting one-time code...");
    }
    request_email_code(&http, &args.server_url, &email).await?;

    let code = prompt_line("Code: ")?;
    let tokens = verify_email_code(&http, &args.server_url, &email, &code).await?;

    validate_token(&tokens.refresh_token, "refresh", &email).context("refresh token")?;
    validate_token(&tokens.access_token, "access", &email).context("access token")?;

    let cfg = Config::new_for_save(
        &args.config_path,
        &args.data_dir,
        &email,
        &args.server_url,
        Some(Config::default_client_url().to_string()),
        None,
        Some(tokens.refresh_token),
    )?;
    cfg.save()?;

    if !args.quiet {
        println!("SyftBox datasite initialized");
        print!("{}", format_already_logged_in(&args.config_path, &cfg));
    }

    Ok(())
}

fn already_logged_in(config_path: &Path, requested_server: &str) -> Result<()> {
    if !config_path.exists() {
        anyhow::bail!("no config");
    }
    let data = std::fs::read_to_string(config_path)
        .with_context(|| format!("read {}", config_path.display()))?;
    let file: FileConfig = serde_json::from_str(&data).context("parse config")?;

    validate_email(&file.email).context("email")?;
    validate_url(&file.server_url).context("server_url")?;
    if !file.client_url.trim().is_empty() {
        validate_url(&file.client_url).context("client_url")?;
    }
    if file.refresh_token.trim().is_empty() {
        anyhow::bail!("no refresh token");
    }
    if file.server_url != requested_server {
        anyhow::bail!("server changed");
    }
    validate_token(&file.refresh_token, "refresh", &file.email)?;
    Ok(())
}

pub fn format_already_logged_in(config_path: &Path, cfg: &Config) -> String {
    let mut s = String::new();
    s.push_str("**Already logged in**\n");
    s.push('\n');
    s.push_str("SYFTBOX DATASITE CONFIG\n");
    s.push_str(&format!("Email\t{}\n", cfg.email));
    s.push_str(&format!("Data\t{}\n", cfg.data_dir.display()));
    s.push_str(&format!("Config\t{}\n", config_path.display()));
    s.push_str(&format!("Server\t{}\n", cfg.server_url));
    s.push_str(&format!(
        "Client\t{}\n",
        cfg.client_url.clone().unwrap_or_default()
    ));
    s.push('\n');
    s
}

fn prompt_line(prompt: &str) -> Result<String> {
    let mut out = io::stderr();
    let _ = out.write_all(prompt.as_bytes());
    let _ = out.flush();
    let mut buf = String::new();
    io::stdin().read_line(&mut buf).context("read stdin")?;
    Ok(buf.trim().to_string())
}

#[cfg(test)]
mod tests {
    use super::*;
    use base64::engine::general_purpose::URL_SAFE_NO_PAD;
    use base64::Engine;

    fn fake_jwt(payload: &serde_json::Value) -> String {
        let header = serde_json::json!({"alg":"none","typ":"JWT"});
        format!(
            "{}.{}.sig",
            URL_SAFE_NO_PAD.encode(serde_json::to_vec(&header).unwrap()),
            URL_SAFE_NO_PAD.encode(serde_json::to_vec(payload).unwrap())
        )
    }

    #[test]
    fn already_logged_in_accepts_valid_refresh_token() {
        let tmp = std::env::temp_dir().join("syftbox-rs-login-test-ok");
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();
        let cfg_path = tmp.join("config.json");

        let email = "alice@example.com";
        let server_url = "http://127.0.0.1:8080";
        let refresh = fake_jwt(&serde_json::json!({
            "type": "refresh",
            "sub": email,
            "exp": 9999999999_i64
        }));

        let json = format!(
            r#"{{
                "email": "{email}",
                "data_dir": "{}",
                "server_url": "{server_url}",
                "client_url": "http://127.0.0.1:7938",
                "refresh_token": "{refresh}"
            }}"#,
            tmp.join("data").display()
        );
        std::fs::write(&cfg_path, json).unwrap();

        already_logged_in(&cfg_path, server_url).unwrap();

        let cfg = Config::load_file_only(&cfg_path).unwrap();
        let rendered = format_already_logged_in(&cfg_path, &cfg);
        assert!(rendered.contains("Already logged in"));
        assert!(rendered.contains(email));
        assert!(rendered.contains(server_url));
        assert!(rendered.contains("http://127.0.0.1:7938"));
    }

    #[test]
    fn already_logged_in_rejects_server_change() {
        let tmp = std::env::temp_dir().join("syftbox-rs-login-test-server-change");
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();
        let cfg_path = tmp.join("config.json");

        let email = "alice@example.com";
        let refresh = fake_jwt(&serde_json::json!({
            "type": "refresh",
            "sub": email,
            "exp": 9999999999_i64
        }));
        let json = format!(
            r#"{{
                "email": "{email}",
                "data_dir": "{}",
                "server_url": "http://127.0.0.1:1111",
                "client_url": "http://127.0.0.1:7938",
                "refresh_token": "{refresh}"
            }}"#,
            tmp.join("data").display()
        );
        std::fs::write(&cfg_path, json).unwrap();

        let err = already_logged_in(&cfg_path, "http://127.0.0.1:2222")
            .unwrap_err()
            .to_string();
        assert!(err.contains("server changed"));
    }

    #[test]
    fn already_logged_in_rejects_expired_token() {
        let tmp = std::env::temp_dir().join("syftbox-rs-login-test-expired");
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();
        let cfg_path = tmp.join("config.json");

        let email = "alice@example.com";
        let server_url = "http://127.0.0.1:8080";
        let refresh = fake_jwt(&serde_json::json!({
            "type": "refresh",
            "sub": email,
            "exp": 1_i64
        }));
        let json = format!(
            r#"{{
                "email": "{email}",
                "data_dir": "{}",
                "server_url": "{server_url}",
                "client_url": "http://127.0.0.1:7938",
                "refresh_token": "{refresh}"
            }}"#,
            tmp.join("data").display()
        );
        std::fs::write(&cfg_path, json).unwrap();

        let err = already_logged_in(&cfg_path, server_url)
            .unwrap_err()
            .to_string();
        assert!(err.contains("expired"));
    }
}
