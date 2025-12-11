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
