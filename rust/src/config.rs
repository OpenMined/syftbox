use std::ffi::OsStr;
use std::path::{Component, Path, PathBuf};

use anyhow::{Context, Result};
use regex::Regex;
use serde::{Deserialize, Serialize};
use url::Url;

#[derive(Debug, Default, Deserialize, Clone)]
struct PartialConfig {
    #[serde(default)]
    data_dir: Option<PathBuf>,
    #[serde(default)]
    email: Option<String>,
    #[serde(default)]
    server_url: Option<String>,
    #[serde(default)]
    client_url: Option<String>,
    #[serde(default)]
    client_token: Option<String>,
    #[serde(default)]
    refresh_token: Option<String>,
    #[serde(default)]
    access_token: Option<String>,
}

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

#[derive(Debug, Default, Clone)]
pub struct ConfigOverrides {
    pub data_dir: Option<PathBuf>,
    pub email: Option<String>,
    pub server_url: Option<String>,
    pub client_url: Option<String>,
    pub client_token: Option<String>,
}

pub fn default_log_file_path() -> PathBuf {
    home_dir().join(".syftbox").join("logs").join("syftbox.log")
}

impl Config {
    pub fn default_data_dir() -> PathBuf {
        home_dir().join("SyftBox")
    }

    pub fn default_server_url() -> &'static str {
        "https://syftbox.net"
    }

    pub fn default_client_url() -> &'static str {
        "http://localhost:7938"
    }

    pub fn default_config_path() -> PathBuf {
        home_dir().join(".syftbox").join("config.json")
    }

    pub fn resolve_config_path(flag_path: Option<&Path>) -> PathBuf {
        if let Some(p) = flag_path {
            return absolutize_path(p);
        }

        if let Ok(env_path) = std::env::var("SYFTBOX_CONFIG_PATH") {
            let env_path = env_path.trim();
            if !env_path.is_empty() {
                return absolutize_path(Path::new(env_path));
            }
        }

        let candidates = [
            Self::default_config_path(),
            home_dir()
                .join(".config")
                .join("syftbox")
                .join("config.json"),
        ];
        for p in candidates {
            if p.exists() {
                return absolutize_path(&p);
            }
        }

        absolutize_path(&Self::default_config_path())
    }

    pub fn load_file_only(path: &Path) -> Result<Self> {
        let file_cfg = if path.exists() {
            let data = std::fs::read_to_string(path)
                .with_context(|| format!("read config {}", path.display()))?;
            serde_json::from_str::<PartialConfig>(&data).context("parse config json")?
        } else {
            PartialConfig::default()
        };

        let data_dir = file_cfg.data_dir.unwrap_or_else(Self::default_data_dir);
        let email = file_cfg.email.unwrap_or_default();
        let server_url = file_cfg
            .server_url
            .unwrap_or_else(|| Self::default_server_url().to_string());
        let client_url = file_cfg
            .client_url
            .or_else(|| Some(Self::default_client_url().to_string()));
        let client_token = file_cfg.client_token;
        let refresh_token = file_cfg.refresh_token;
        let access_token = file_cfg.access_token;

        let mut cfg = Config {
            data_dir,
            email,
            server_url,
            client_url,
            client_token,
            refresh_token,
            access_token,
            config_path: Some(absolutize_path(path)),
        };
        cfg.normalize()?;
        cfg.validate()?;
        Ok(cfg)
    }

    pub fn load_control_plane_settings(
        path: &Path,
        overrides: &ConfigOverrides,
    ) -> Result<(Option<String>, Option<String>)> {
        let file_cfg = if path.exists() {
            let data = std::fs::read_to_string(path)
                .with_context(|| format!("read config {}", path.display()))?;
            serde_json::from_str::<PartialConfig>(&data).context("parse config json")?
        } else {
            PartialConfig::default()
        };

        let env_cfg = read_env_config();

        let client_url = overrides
            .client_url
            .clone()
            .or(env_cfg.client_url)
            .or(file_cfg.client_url);
        let client_token = overrides
            .client_token
            .clone()
            .or(env_cfg.client_token)
            .or(file_cfg.client_token);
        Ok((client_url, client_token))
    }

    pub fn load_with_overrides(path: &Path, overrides: ConfigOverrides) -> Result<Self> {
        let file_cfg = if path.exists() {
            let data = std::fs::read_to_string(path)
                .with_context(|| format!("read config {}", path.display()))?;
            serde_json::from_str::<PartialConfig>(&data).context("parse config json")?
        } else {
            PartialConfig::default()
        };

        let env_cfg = read_env_config();

        let data_dir = overrides
            .data_dir
            .or(env_cfg.data_dir)
            .or(file_cfg.data_dir)
            .unwrap_or_else(Self::default_data_dir);
        let email = overrides
            .email
            .or(env_cfg.email)
            .or(file_cfg.email)
            .unwrap_or_default();
        let server_url = overrides
            .server_url
            .or(env_cfg.server_url)
            .or(file_cfg.server_url)
            .unwrap_or_else(|| Self::default_server_url().to_string());
        let client_url = overrides
            .client_url
            .or(env_cfg.client_url)
            .or(file_cfg.client_url)
            .or_else(|| Some(Self::default_client_url().to_string()));
        let client_token = overrides
            .client_token
            .or(env_cfg.client_token)
            .or(file_cfg.client_token);
        let refresh_token = env_cfg.refresh_token.or(file_cfg.refresh_token);
        let access_token = env_cfg.access_token.or(file_cfg.access_token);

        let mut cfg = Config {
            data_dir,
            email,
            server_url,
            client_url,
            client_token,
            refresh_token,
            access_token,
            config_path: Some(path.to_path_buf()),
        };
        cfg.normalize()?;
        cfg.validate()?;
        Ok(cfg)
    }

    pub fn new_for_save(
        path: &Path,
        data_dir: &Path,
        email: &str,
        server_url: &str,
        client_url: Option<String>,
        client_token: Option<String>,
        refresh_token: Option<String>,
    ) -> Result<Self> {
        let mut cfg = Config {
            data_dir: data_dir.to_path_buf(),
            email: email.to_string(),
            server_url: server_url.to_string(),
            client_url,
            client_token,
            refresh_token,
            access_token: None,
            config_path: Some(path.to_path_buf()),
        };
        cfg.normalize()?;
        cfg.validate()?;
        Ok(cfg)
    }

    pub fn save(&self) -> Result<()> {
        let Some(path) = &self.config_path else {
            anyhow::bail!("config_path missing");
        };
        save_config_file(path, self)
    }

    fn normalize(&mut self) -> Result<()> {
        self.email = self.email.to_lowercase();
        self.data_dir = absolutize_path(&self.data_dir);
        if let Some(p) = self.config_path.take() {
            self.config_path = Some(absolutize_path(&p));
        }
        Ok(())
    }

    fn validate(&self) -> Result<()> {
        validate_email(&self.email)?;
        validate_url(&self.server_url).context("server_url")?;
        if self.server_url.contains("openmined.org") {
            anyhow::bail!("legacy server detected. run `syftbox login` to re-authenticate");
        }
        if let Some(url) = &self.client_url {
            validate_url(url).context("client_url")?;
        }
        Ok(())
    }
}

#[derive(Debug, Serialize)]
struct PersistedConfig<'a> {
    data_dir: &'a PathBuf,
    email: &'a str,
    server_url: &'a str,
    #[serde(skip_serializing_if = "is_none_or_empty")]
    client_url: &'a Option<String>,
    #[serde(skip_serializing_if = "is_none_or_empty")]
    client_token: &'a Option<String>,
    #[serde(skip_serializing_if = "is_none_or_empty")]
    refresh_token: &'a Option<String>,
}

fn is_none_or_empty(v: &Option<String>) -> bool {
    v.as_deref().unwrap_or("").trim().is_empty()
}

fn save_config_file(path: &Path, cfg: &Config) -> Result<()> {
    if let Some(parent) = path.parent() {
        std::fs::create_dir_all(parent).with_context(|| format!("create {}", parent.display()))?;
    }
    let persisted = PersistedConfig {
        data_dir: &cfg.data_dir,
        email: &cfg.email,
        server_url: &cfg.server_url,
        client_url: &cfg.client_url,
        client_token: &cfg.client_token,
        refresh_token: &cfg.refresh_token,
    };
    let data = serde_json::to_vec(&persisted).context("serialize config")?;
    std::fs::write(path, data).with_context(|| format!("write {}", path.display()))?;
    Ok(())
}

#[allow(dead_code)]
pub fn save_refresh_token_file_only(path: &Path, refresh_token: &str) -> Result<()> {
    let mut cfg = Config::load_file_only(path)?;
    cfg.refresh_token = Some(refresh_token.to_string());
    cfg.save()
}

pub(crate) fn validate_url(raw: &str) -> Result<()> {
    let url = Url::parse(raw)?;
    if url.scheme() != "http" && url.scheme() != "https" {
        anyhow::bail!("url must be http or https");
    }
    Ok(())
}

pub(crate) fn validate_email(email: &str) -> Result<()> {
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

fn home_dir() -> PathBuf {
    std::env::var_os("HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from("."))
}

fn absolutize_path(path: &Path) -> PathBuf {
    let expanded = expand_tilde(path);
    let abs = if expanded.is_absolute() {
        expanded
    } else {
        std::env::current_dir()
            .unwrap_or_else(|_| PathBuf::from("."))
            .join(expanded)
    };
    let cleaned = clean_lexical(&abs);
    // On macOS, /tmp is a symlink to /private/tmp. Canonicalize to resolve symlinks
    // so all path comparisons use consistent forms. Fall back to cleaned path if
    // canonicalization fails (e.g., path doesn't exist yet).
    std::fs::canonicalize(&cleaned).unwrap_or(cleaned)
}

fn expand_tilde(path: &Path) -> PathBuf {
    let mut components = path.components();
    match components.next() {
        Some(Component::Normal(c)) if c == OsStr::new("~") => {
            let mut out = home_dir();
            for c in components {
                out.push(c.as_os_str());
            }
            out
        }
        _ => path.to_path_buf(),
    }
}

fn clean_lexical(path: &Path) -> PathBuf {
    // Similar to Go's filepath.Clean + Abs, but without requiring the path to exist.
    let mut out = PathBuf::new();
    for c in path.components() {
        match c {
            Component::Prefix(prefix) => out.push(prefix.as_os_str()),
            Component::RootDir => out.push(Path::new(&std::path::MAIN_SEPARATOR.to_string())),
            Component::CurDir => {}
            Component::ParentDir => {
                if !pop_normal_component(&mut out) && !out.as_os_str().is_empty() {
                    out.push("..");
                }
            }
            Component::Normal(p) => out.push(p),
        }
    }
    if out.as_os_str().is_empty() {
        PathBuf::from(".")
    } else {
        out
    }
}

fn pop_normal_component(path: &mut PathBuf) -> bool {
    let mut comps = path.components().collect::<Vec<_>>();
    match comps.pop() {
        Some(Component::Normal(_)) => {
            *path = rebuild_components(&comps);
            true
        }
        Some(Component::Prefix(_)) | Some(Component::RootDir) | None => false,
        Some(Component::CurDir) => {
            *path = rebuild_components(&comps);
            false
        }
        Some(Component::ParentDir) => {
            *path = rebuild_components(&comps);
            false
        }
    }
}

fn rebuild_components(components: &[Component<'_>]) -> PathBuf {
    let mut out = PathBuf::new();
    for c in components {
        match c {
            Component::Prefix(prefix) => out.push(prefix.as_os_str()),
            Component::RootDir => out.push(Path::new(&std::path::MAIN_SEPARATOR.to_string())),
            Component::CurDir => {}
            Component::ParentDir => out.push(".."),
            Component::Normal(p) => out.push(p),
        }
    }
    out
}

fn read_env_config() -> PartialConfig {
    let mut out = PartialConfig::default();
    if let Ok(v) = std::env::var("SYFTBOX_EMAIL") {
        let v = v.trim();
        if !v.is_empty() {
            out.email = Some(v.to_string());
        }
    }
    if let Ok(v) = std::env::var("SYFTBOX_DATA_DIR") {
        let v = v.trim();
        if !v.is_empty() {
            out.data_dir = Some(PathBuf::from(v));
        }
    }
    if let Ok(v) = std::env::var("SYFTBOX_SERVER_URL") {
        let v = v.trim();
        if !v.is_empty() {
            out.server_url = Some(v.to_string());
        }
    }
    if let Ok(v) = std::env::var("SYFTBOX_CLIENT_URL") {
        let v = v.trim();
        if !v.is_empty() {
            out.client_url = Some(v.to_string());
        }
    }
    if let Ok(v) = std::env::var("SYFTBOX_CLIENT_TOKEN") {
        let v = v.trim();
        if !v.is_empty() {
            out.client_token = Some(v.to_string());
        }
    }
    if let Ok(v) = std::env::var("SYFTBOX_REFRESH_TOKEN") {
        let v = v.trim();
        if !v.is_empty() {
            out.refresh_token = Some(v.to_string());
        }
    }
    if let Ok(v) = std::env::var("SYFTBOX_ACCESS_TOKEN") {
        let v = v.trim();
        if !v.is_empty() {
            out.access_token = Some(v.to_string());
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::collections::HashMap;
    use std::sync::Mutex;
    use std::{env, fs};

    static ENV_LOCK: once_cell::sync::Lazy<Mutex<()>> =
        once_cell::sync::Lazy::new(|| Mutex::new(()));

    struct EnvGuard {
        saved: HashMap<String, Option<String>>,
    }

    impl EnvGuard {
        fn new(keys: &[&str]) -> Self {
            let mut saved = HashMap::new();
            for k in keys {
                saved.insert((*k).to_string(), env::var(k).ok());
            }
            Self { saved }
        }
    }

    impl Drop for EnvGuard {
        fn drop(&mut self) {
            for (k, v) in self.saved.drain() {
                match v {
                    Some(val) => env::set_var(k, val),
                    None => env::remove_var(k),
                }
            }
        }
    }

    #[test]
    fn load_config_from_json_and_normalize() {
        let _lock = ENV_LOCK.lock().unwrap();
        let _guard = EnvGuard::new(&[
            "SYFTBOX_EMAIL",
            "SYFTBOX_DATA_DIR",
            "SYFTBOX_SERVER_URL",
            "SYFTBOX_CLIENT_URL",
            "SYFTBOX_CLIENT_TOKEN",
            "SYFTBOX_CONFIG_PATH",
        ]);
        env::remove_var("SYFTBOX_EMAIL");
        env::remove_var("SYFTBOX_DATA_DIR");
        env::remove_var("SYFTBOX_SERVER_URL");
        env::remove_var("SYFTBOX_CLIENT_URL");
        env::remove_var("SYFTBOX_CLIENT_TOKEN");
        env::remove_var("SYFTBOX_CONFIG_PATH");

        let tmp = env::temp_dir().join("syftbox-rs-config-test");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        let cfg_path = tmp.join("config.json");
        // Use forward slashes for cross-platform JSON compatibility
        let data_dir = tmp.join("data").display().to_string().replace('\\', "/");
        let json = format!(
            r#"{{
                "email": "Alice@Example.com",
                "data_dir": "{}",
                "server_url": "http://127.0.0.1:8080",
                "client_url": "http://127.0.0.1:7938"
            }}"#,
            data_dir
        );
        fs::write(&cfg_path, json).unwrap();

        let cfg = Config::load_with_overrides(&cfg_path, ConfigOverrides::default()).unwrap();
        assert_eq!(cfg.email, "alice@example.com");
        assert_eq!(cfg.server_url, "http://127.0.0.1:8080");
        assert_eq!(cfg.client_url.as_deref(), Some("http://127.0.0.1:7938"));
        assert_eq!(cfg.config_path.as_ref().unwrap(), &cfg_path);
        assert!(cfg.data_dir.is_absolute());
    }

    #[test]
    fn reject_invalid_url_scheme() {
        let _lock = ENV_LOCK.lock().unwrap();
        let _guard = EnvGuard::new(&[
            "SYFTBOX_EMAIL",
            "SYFTBOX_DATA_DIR",
            "SYFTBOX_SERVER_URL",
            "SYFTBOX_CLIENT_URL",
            "SYFTBOX_CLIENT_TOKEN",
        ]);
        env::remove_var("SYFTBOX_EMAIL");
        env::remove_var("SYFTBOX_DATA_DIR");
        env::remove_var("SYFTBOX_SERVER_URL");
        env::remove_var("SYFTBOX_CLIENT_URL");
        env::remove_var("SYFTBOX_CLIENT_TOKEN");

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
        let err = Config::load_with_overrides(&cfg_path, ConfigOverrides::default()).unwrap_err();
        assert!(err.to_string().contains("server_url"));
    }

    #[test]
    fn reject_invalid_email() {
        let _lock = ENV_LOCK.lock().unwrap();
        let _guard = EnvGuard::new(&[
            "SYFTBOX_EMAIL",
            "SYFTBOX_DATA_DIR",
            "SYFTBOX_SERVER_URL",
            "SYFTBOX_CLIENT_URL",
            "SYFTBOX_CLIENT_TOKEN",
        ]);
        env::remove_var("SYFTBOX_EMAIL");
        env::remove_var("SYFTBOX_DATA_DIR");
        env::remove_var("SYFTBOX_SERVER_URL");
        env::remove_var("SYFTBOX_CLIENT_URL");
        env::remove_var("SYFTBOX_CLIENT_TOKEN");

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
        let err = Config::load_with_overrides(&cfg_path, ConfigOverrides::default()).unwrap_err();
        assert!(err.to_string().contains("invalid email"));
    }

    #[test]
    fn resolve_config_path_flag_beats_env() {
        let _lock = ENV_LOCK.lock().unwrap();
        let _guard = EnvGuard::new(&["HOME", "SYFTBOX_CONFIG_PATH"]);

        let tmp = env::temp_dir().join("syftbox-rs-config-path-flag");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        env::set_var("HOME", &tmp);

        // Use cross-platform temp paths
        let env_path = tmp.join("env").join("config.json");
        let flag_path = tmp.join("flag").join("config.json");
        env::set_var("SYFTBOX_CONFIG_PATH", &env_path);

        let resolved = Config::resolve_config_path(Some(&flag_path));
        assert_eq!(resolved, flag_path);
    }

    #[test]
    fn resolve_config_path_uses_env_when_no_flag() {
        let _lock = ENV_LOCK.lock().unwrap();
        let _guard = EnvGuard::new(&["HOME", "SYFTBOX_CONFIG_PATH"]);

        let tmp = env::temp_dir().join("syftbox-rs-config-path-env");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        env::set_var("HOME", &tmp);

        // Use cross-platform temp path
        let env_path = tmp.join("env").join("config.json");
        env::set_var("SYFTBOX_CONFIG_PATH", &env_path);

        let resolved = Config::resolve_config_path(None);
        assert_eq!(resolved, env_path);
    }

    #[test]
    fn resolve_config_path_finds_existing_candidate() {
        let _lock = ENV_LOCK.lock().unwrap();
        let _guard = EnvGuard::new(&["HOME", "SYFTBOX_CONFIG_PATH"]);

        let tmp = env::temp_dir().join("syftbox-rs-config-path-existing");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        env::set_var("HOME", &tmp);
        env::remove_var("SYFTBOX_CONFIG_PATH");

        let candidate = tmp.join(".config").join("syftbox").join("config.json");
        fs::create_dir_all(candidate.parent().unwrap()).unwrap();
        fs::write(&candidate, "{}").unwrap();

        let resolved = Config::resolve_config_path(None);
        assert_eq!(resolved, candidate);
    }

    #[test]
    fn load_with_overrides_flag_beats_env_beats_file() {
        let _lock = ENV_LOCK.lock().unwrap();
        let _guard = EnvGuard::new(&[
            "SYFTBOX_EMAIL",
            "SYFTBOX_DATA_DIR",
            "SYFTBOX_SERVER_URL",
            "SYFTBOX_CLIENT_URL",
            "SYFTBOX_CLIENT_TOKEN",
        ]);

        let tmp = env::temp_dir().join("syftbox-rs-config-precedence");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();

        // Use cross-platform temp paths
        let file_data_dir = tmp.join("file-data");
        let env_data_dir = tmp.join("env-data");
        let flag_data_dir = tmp.join("flag-data");

        let cfg_path = tmp.join("config.json");
        // Use forward slashes for cross-platform JSON compatibility
        let file_data_dir_str = file_data_dir.display().to_string().replace('\\', "/");
        fs::write(
            &cfg_path,
            format!(
                r#"{{
              "email": "file@example.com",
              "data_dir": "{}",
              "server_url": "https://file.syftbox.net",
              "client_url": "http://file.local:1234",
              "client_token": "file-token"
            }}"#,
                file_data_dir_str
            ),
        )
        .unwrap();

        env::set_var("SYFTBOX_EMAIL", "env@example.com");
        env::set_var("SYFTBOX_DATA_DIR", env_data_dir.to_string_lossy().as_ref());
        env::set_var("SYFTBOX_SERVER_URL", "https://env.syftbox.net");
        env::set_var("SYFTBOX_CLIENT_URL", "http://env.local:5555");
        env::set_var("SYFTBOX_CLIENT_TOKEN", "env-token");

        let cfg = Config::load_with_overrides(&cfg_path, ConfigOverrides::default()).unwrap();
        assert_eq!(cfg.email, "env@example.com");
        assert_eq!(cfg.data_dir, env_data_dir);
        assert_eq!(cfg.server_url, "https://env.syftbox.net");
        assert_eq!(cfg.client_url.as_deref(), Some("http://env.local:5555"));
        assert_eq!(cfg.client_token.as_deref(), Some("env-token"));

        let overrides = ConfigOverrides {
            email: Some("flag@example.com".to_string()),
            data_dir: Some(flag_data_dir.clone()),
            server_url: Some("https://flag.syftbox.net".to_string()),
            client_url: Some("http://flag.local:9999".to_string()),
            client_token: Some("flag-token".to_string()),
        };
        let cfg = Config::load_with_overrides(&cfg_path, overrides).unwrap();
        assert_eq!(cfg.email, "flag@example.com");
        assert_eq!(cfg.data_dir, flag_data_dir);
        assert_eq!(cfg.server_url, "https://flag.syftbox.net");
        assert_eq!(cfg.client_url.as_deref(), Some("http://flag.local:9999"));
        assert_eq!(cfg.client_token.as_deref(), Some("flag-token"));
    }

    #[test]
    fn save_refresh_token_overwrites_file_and_omits_access_token() {
        let _lock = ENV_LOCK.lock().unwrap();
        let _guard = EnvGuard::new(&[
            "SYFTBOX_EMAIL",
            "SYFTBOX_DATA_DIR",
            "SYFTBOX_SERVER_URL",
            "SYFTBOX_CLIENT_URL",
            "SYFTBOX_CLIENT_TOKEN",
        ]);
        env::remove_var("SYFTBOX_EMAIL");
        env::remove_var("SYFTBOX_DATA_DIR");
        env::remove_var("SYFTBOX_SERVER_URL");
        env::remove_var("SYFTBOX_CLIENT_URL");
        env::remove_var("SYFTBOX_CLIENT_TOKEN");

        let tmp = env::temp_dir().join("syftbox-rs-save-refresh-token");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        let cfg_path = tmp.join("config.json");
        fs::write(
            &cfg_path,
            r#"{
              "email":"alice@example.com",
              "data_dir":"/tmp/syftbox",
              "server_url":"https://syftbox.net",
              "client_url":"http://localhost:7938",
              "refresh_token":"old",
              "access_token":"SHOULD_NOT_PERSIST"
            }"#,
        )
        .unwrap();

        save_refresh_token_file_only(&cfg_path, "new").unwrap();

        let raw = fs::read_to_string(&cfg_path).unwrap();
        assert!(raw.contains("\"refresh_token\":\"new\""));
        assert!(!raw.contains("access_token"));
    }

    #[test]
    fn default_log_file_path_matches_go_convention() {
        let _lock = ENV_LOCK.lock().unwrap();
        let _guard = EnvGuard::new(&["HOME"]);

        let tmp = env::temp_dir().join("syftbox-rs-log-path-home");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        env::set_var("HOME", &tmp);

        let p = default_log_file_path();
        assert!(p.ends_with(".syftbox/logs/syftbox.log"));
        assert!(p.to_string_lossy().contains(tmp.to_string_lossy().as_ref()));
    }
}
