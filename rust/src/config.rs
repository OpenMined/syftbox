use std::path::{Path, PathBuf};

use anyhow::{Context, Result};
use regex::Regex;
use serde::Deserialize;
use url::Url;

#[derive(Debug, Deserialize, Clone)]
#[allow(dead_code)]
pub struct Config {
    pub data_dir: PathBuf,
    pub email: String,
    pub server_url: String,
    #[serde(default)]
    pub client_url: Option<String>,
    #[serde(default)]
    pub client_token: Option<String>,
    #[serde(default)]
    pub refresh_token: Option<String>,
    #[serde(default)]
    pub access_token: Option<String>,
    #[serde(default)]
    pub config_path: Option<PathBuf>,
}

impl Config {
    pub fn load(path: &Path) -> Result<Self> {
        let data = std::fs::read_to_string(path)
            .with_context(|| format!("read config {}", path.display()))?;
        let mut cfg: Config = serde_json::from_str(&data).context("parse config json")?;
        cfg.config_path = Some(path.to_path_buf());
        cfg.normalize()?;
        cfg.validate()?;
        Ok(cfg)
    }

    fn normalize(&mut self) -> Result<()> {
        self.email = self.email.to_lowercase();
        if self.data_dir.is_relative() {
            if let Ok(abs) = std::fs::canonicalize(&self.data_dir) {
                self.data_dir = abs;
            }
        }
        Ok(())
    }

    fn validate(&self) -> Result<()> {
        validate_email(&self.email)?;
        validate_url(&self.server_url).context("server_url")?;
        if let Some(url) = &self.client_url {
            validate_url(url).context("client_url")?;
        }
        Ok(())
    }
}

fn validate_url(raw: &str) -> Result<()> {
    let url = Url::parse(raw)?;
    if url.scheme() != "http" && url.scheme() != "https" {
        anyhow::bail!("url must be http or https");
    }
    Ok(())
}

fn validate_email(email: &str) -> Result<()> {
    // Simple RFC5322-ish check; matches Go client behaviour closely enough.
    static PATTERN: once_cell::sync::Lazy<Regex> = once_cell::sync::Lazy::new(|| {
        Regex::new(r"(?i)^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$").unwrap()
    });
    if PATTERN.is_match(email) {
        Ok(())
    } else {
        anyhow::bail!("invalid email: {email}")
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::{env, fs};

    #[test]
    fn load_config_from_json_and_normalize() {
        let tmp = env::temp_dir().join("syftbox-rs-config-test");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        let cfg_path = tmp.join("config.json");
        let data_dir = tmp.join("data");
        let json = format!(
            r#"{{
                "email": "Alice@Example.com",
                "data_dir": "{}",
                "server_url": "http://127.0.0.1:8080",
                "client_url": "http://127.0.0.1:7938"
            }}"#,
            data_dir.display()
        );
        fs::write(&cfg_path, json).unwrap();

        let cfg = Config::load(&cfg_path).unwrap();
        assert_eq!(cfg.email, "alice@example.com");
        assert_eq!(cfg.server_url, "http://127.0.0.1:8080");
        assert_eq!(cfg.client_url.as_deref(), Some("http://127.0.0.1:7938"));
        assert_eq!(cfg.config_path.as_ref().unwrap(), &cfg_path);
        assert!(cfg.data_dir.is_absolute());
    }

    #[test]
    fn reject_invalid_url_scheme() {
        let tmp = env::temp_dir().join("syftbox-rs-config-test-bad-url");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        let cfg_path = tmp.join("config.json");
        let json = r#"{
            "email": "alice@example.com",
            "data_dir": "/tmp/data",
            "server_url": "ftp://bad.example.com"
        }"#;
        fs::write(&cfg_path, json).unwrap();
        let err = Config::load(&cfg_path).unwrap_err();
        assert!(err.to_string().contains("server_url"));
    }

    #[test]
    fn reject_invalid_email() {
        let tmp = env::temp_dir().join("syftbox-rs-config-test-bad-email");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        let cfg_path = tmp.join("config.json");
        let json = r#"{
            "email": "not-an-email",
            "data_dir": "/tmp/data",
            "server_url": "http://localhost:8080"
        }"#;
        fs::write(&cfg_path, json).unwrap();
        let err = Config::load(&cfg_path).unwrap_err();
        assert!(err.to_string().contains("invalid email"));
    }
}
