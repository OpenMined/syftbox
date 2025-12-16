use std::path::{Path, PathBuf};
use std::process::Command;

use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use url::Url;
use uuid::Uuid;

use crate::config::Config;

const APP_INFO_FILE: &str = ".syftboxapp.json";

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct AppInfo {
    pub id: String,
    pub name: String,
    pub path: String,
    pub source: String,
    #[serde(default)]
    #[serde(rename = "sourceURI")]
    pub source_uri: String,
    #[serde(default)]
    pub branch: String,
    #[serde(default)]
    pub tag: String,
    #[serde(default)]
    pub commit: String,
    #[serde(default)]
    pub installed_on: Option<DateTime<Utc>>,
}

pub fn apps_dir(cfg: &Config) -> PathBuf {
    cfg.data_dir.join("apps")
}

pub fn internal_data_dir(cfg: &Config) -> PathBuf {
    cfg.data_dir.join(".data")
}

pub fn is_valid_app(path: &Path) -> bool {
    path.join("run.sh").is_file()
}

pub fn app_id_from_path(path: &Path) -> String {
    let base = path
        .file_name()
        .and_then(|s| s.to_str())
        .unwrap_or_default();
    // Match Go: replace '.' and runs of whitespace/dots with '-'; do not lowercase.
    let base = base.replace('.', "-");
    let base = regex::Regex::new(r"[\s.]+")
        .unwrap()
        .replace_all(&base, "-")
        .to_string();
    format!("local.{base}")
}

pub fn app_name_from_path(path: &Path) -> String {
    path.file_name()
        .and_then(|s| s.to_str())
        .unwrap_or_default()
        .to_lowercase()
}

pub fn install_from_path(app_src: &Path, cfg: &Config, force: bool) -> Result<AppInfo> {
    let app_src = app_src
        .canonicalize()
        .with_context(|| format!("resolve path {}", app_src.display()))?;

    if !is_valid_app(&app_src) {
        anyhow::bail!("not a valid syftbox app");
    }

    let apps_dir = apps_dir(cfg);
    let data_dir = internal_data_dir(cfg);
    std::fs::create_dir_all(&apps_dir)
        .with_context(|| format!("create apps dir {}", apps_dir.display()))?;
    std::fs::create_dir_all(&data_dir)
        .with_context(|| format!("create data dir {}", data_dir.display()))?;

    let id = app_id_from_path(&app_src);
    let name = app_name_from_path(&app_src);
    let install_dir = apps_dir.join(&id);

    if install_dir.exists() {
        if force {
            remove_app_path(&install_dir)
                .with_context(|| format!("remove existing {}", install_dir.display()))?;
        } else {
            anyhow::bail!("app already exists at {:?}", install_dir);
        }
    }

    create_symlink_dir(&app_src, &install_dir).with_context(|| {
        format!(
            "create symlink {} -> {}",
            install_dir.display(),
            app_src.display()
        )
    })?;

    let info = AppInfo {
        id: id.clone(),
        name,
        path: install_dir.display().to_string(),
        source: "local".to_string(),
        source_uri: app_src.display().to_string(),
        branch: "".to_string(),
        tag: "".to_string(),
        commit: "".to_string(),
        installed_on: Some(Utc::now()),
    };

    save_app_info(&install_dir, &info)?;
    Ok(info)
}

pub async fn install_from_url(
    uri: &str,
    cfg: &Config,
    branch: &str,
    tag: &str,
    commit: &str,
    force: bool,
) -> Result<AppInfo> {
    let parsed = Url::parse(uri).context("invalid url")?;
    let id = app_id_from_url(&parsed);
    let name = app_name_from_url(&parsed);
    let apps_dir = apps_dir(cfg);
    let data_dir = internal_data_dir(cfg);
    std::fs::create_dir_all(&apps_dir)
        .with_context(|| format!("create apps dir {}", apps_dir.display()))?;
    std::fs::create_dir_all(&data_dir)
        .with_context(|| format!("create data dir {}", data_dir.display()))?;

    let install_dir = apps_dir.join(&id);
    if install_dir.exists() {
        if force {
            remove_app_path(&install_dir)?;
        } else {
            anyhow::bail!("app already exists at {:?}", install_dir);
        }
    }

    let archive_url = get_archive_url(&parsed, branch, tag, commit)?;
    let zip_path = download_zip(&archive_url).await?;
    struct Tmp(PathBuf);
    impl Drop for Tmp {
        fn drop(&mut self) {
            let _ = std::fs::remove_file(&self.0);
        }
    }
    let _cleanup = Tmp(zip_path.clone());

    extract_zip_strip_root(&zip_path, &install_dir)?;
    if !is_valid_app(&install_dir) {
        let _ = std::fs::remove_dir_all(&install_dir);
        anyhow::bail!("not a valid syftbox app");
    }

    let info = AppInfo {
        id: id.clone(),
        name,
        path: install_dir.display().to_string(),
        source: "git".to_string(),
        source_uri: uri.to_string(),
        branch: branch.to_string(),
        tag: tag.to_string(),
        commit: commit.to_string(),
        installed_on: Some(Utc::now()),
    };
    save_app_info(&install_dir, &info)?;
    Ok(info)
}

pub fn list_apps(cfg: &Config) -> Result<Vec<AppInfo>> {
    let apps_dir = apps_dir(cfg);
    let mut out = Vec::new();
    let entries = match std::fs::read_dir(&apps_dir) {
        Ok(e) => e,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(out),
        Err(e) => return Err(e).with_context(|| format!("read apps dir {}", apps_dir.display())),
    };

    for entry in entries {
        let entry = match entry {
            Ok(e) => e,
            Err(_) => continue,
        };
        let ft = match entry.file_type() {
            Ok(ft) => ft,
            Err(_) => continue,
        };
        if !(ft.is_dir() || ft.is_symlink()) {
            continue;
        }
        let p = entry.path();
        let info = match load_app_info_from_path(&p) {
            Ok(i) => i,
            Err(_) => continue,
        };
        out.push(info);
    }

    out.sort_by(|a, b| a.id.cmp(&b.id));
    Ok(out)
}

pub fn uninstall_app(cfg: &Config, uri: &str) -> Result<String> {
    let apps = list_apps(cfg)?;
    let mut target: Option<(String, PathBuf)> = None;
    for app in apps {
        if app.path == uri || app.id == uri || app.name == uri || app.source_uri == uri {
            target = Some((app.id, PathBuf::from(app.path)));
            break;
        }
    }

    let Some((id, path)) = target else {
        anyhow::bail!("app not found");
    };
    remove_app_path(&path)?;
    Ok(id)
}

pub fn format_app_list(apps_dir: &Path, apps: &[AppInfo]) -> String {
    if apps.is_empty() {
        return format!("No apps installed at '{}'\n", apps_dir.display());
    }
    let mut s = String::new();
    for (idx, app) in apps.iter().enumerate() {
        if idx > 0 {
            s.push('\n');
        }
        let src = if app.source != "local" {
            if !app.branch.is_empty() {
                &app.branch
            } else if !app.tag.is_empty() {
                &app.tag
            } else {
                &app.commit
            }
        } else {
            &app.source
        };
        s.push_str(&format!("ID      {}\n", app.id));
        s.push_str(&format!("Path    {}\n", app.path));
        s.push_str(&format!("Source  {} ({})\n", app.source_uri, src));
    }
    s
}

pub fn format_install_result(app: &AppInfo) -> String {
    format!("Installed '{}' at '{}'\n", app.name, app.path)
}

pub fn format_uninstall_result(app_id: &str) -> String {
    format!("Uninstalled '{}'\n", app_id)
}

fn app_id_from_url(url: &Url) -> String {
    let host = url.host_str().unwrap_or_default();
    let mut parts: Vec<&str> = host.split('.').filter(|p| !p.is_empty()).collect();
    parts.reverse();

    let path_parts: Vec<String> = url
        .path()
        .trim_matches('/')
        .split('/')
        .filter(|p| !p.is_empty())
        .map(|p| p.replace('.', "-").to_lowercase())
        .collect();

    let mut all: Vec<String> = parts.into_iter().map(|p| p.to_string()).collect();
    all.extend(path_parts);
    all.join(".")
}

fn app_name_from_url(url: &Url) -> String {
    url.path()
        .trim_matches('/')
        .split('/')
        .filter(|p| !p.is_empty())
        .next_back()
        .unwrap_or_default()
        .to_lowercase()
}

fn get_archive_url(url: &Url, branch: &str, tag: &str, commit: &str) -> Result<String> {
    let mut base = url.clone();
    base.set_query(None);
    base.set_fragment(None);
    let path = base
        .path()
        .trim_end_matches('/')
        .trim_end_matches(".git")
        .to_string();
    base.set_path(&path);
    let repo = base.to_string().trim_end_matches('/').to_string();
    let host = url.host_str().unwrap_or_default();
    match host {
        "github.com" => {
            if !branch.is_empty() {
                Ok(format!("{repo}/archive/refs/heads/{branch}.zip"))
            } else if !tag.is_empty() {
                Ok(format!("{repo}/archive/refs/tags/{tag}.zip"))
            } else if !commit.is_empty() {
                Ok(format!("{repo}/archive/{commit}.zip"))
            } else {
                anyhow::bail!("no branch, tag or commit provided");
            }
        }
        "gitlab.com" => {
            if !branch.is_empty() {
                Ok(format!("{repo}/-/archive/{branch}/archive.zip"))
            } else if !tag.is_empty() {
                Ok(format!("{repo}/-/archive/{tag}/archive.zip"))
            } else if !commit.is_empty() {
                Ok(format!("{repo}/-/archive/{commit}/archive.zip"))
            } else {
                anyhow::bail!("no branch, tag or commit provided");
            }
        }
        _ => anyhow::bail!("unsupported host: {host:?}"),
    }
}

async fn download_zip(url: &str) -> Result<PathBuf> {
    let tmp = std::env::temp_dir().join(format!("syftbox-app-{}.zip", Uuid::new_v4()));
    let http = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(30))
        .build()?;
    let resp = http.get(url).send().await.context("http get")?;
    let status = resp.status();
    let bytes = resp.bytes().await.context("read body")?;
    if !status.is_success() {
        anyhow::bail!("download failed: http {status}");
    }
    std::fs::write(&tmp, &bytes).with_context(|| format!("write {}", tmp.display()))?;
    Ok(tmp)
}

fn extract_zip_strip_root(zip_path: &Path, dst: &Path) -> Result<()> {
    let tmp = std::env::temp_dir().join(format!("syftbox-unzip-{}", Uuid::new_v4()));
    std::fs::create_dir_all(&tmp).with_context(|| format!("create {}", tmp.display()))?;

    let status = Command::new("unzip")
        .arg("-q")
        .arg(zip_path)
        .arg("-d")
        .arg(&tmp)
        .status();
    match status {
        Ok(s) if s.success() => {}
        Ok(s) => anyhow::bail!("unzip failed: {s}"),
        Err(e) => return Err(e).context("spawn unzip"),
    }

    let mut root_dir: Option<PathBuf> = None;
    for entry in std::fs::read_dir(&tmp).with_context(|| format!("read {}", tmp.display()))? {
        let entry = entry?;
        let p = entry.path();
        if p.is_dir() {
            root_dir = Some(p);
            break;
        }
    }
    let Some(root_dir) = root_dir else {
        anyhow::bail!("zip did not contain a root directory");
    };

    std::fs::create_dir_all(dst).with_context(|| format!("create {}", dst.display()))?;
    for entry in
        std::fs::read_dir(&root_dir).with_context(|| format!("read {}", root_dir.display()))?
    {
        let entry = entry?;
        let from = entry.path();
        let to = dst.join(entry.file_name());
        std::fs::rename(&from, &to)
            .or_else(|_| copy_recursively(&from, &to))
            .with_context(|| format!("move {} -> {}", from.display(), to.display()))?;
    }
    let _ = std::fs::remove_dir_all(&tmp);
    Ok(())
}

fn copy_recursively(from: &Path, to: &Path) -> std::io::Result<()> {
    if from.is_dir() {
        std::fs::create_dir_all(to)?;
        for entry in std::fs::read_dir(from)? {
            let entry = entry?;
            let src = entry.path();
            let dst = to.join(entry.file_name());
            copy_recursively(&src, &dst)?;
        }
        Ok(())
    } else {
        if let Some(parent) = to.parent() {
            std::fs::create_dir_all(parent)?;
        }
        std::fs::copy(from, to)?;
        Ok(())
    }
}

fn save_app_info(app_dir: &Path, info: &AppInfo) -> Result<()> {
    let path = app_dir.join(APP_INFO_FILE);
    let data = serde_json::to_vec(info).context("serialize app metadata")?;
    std::fs::write(&path, data).with_context(|| format!("write {}", path.display()))?;
    Ok(())
}

fn load_app_info_from_path(app_path: &Path) -> Result<AppInfo> {
    if !is_valid_app(app_path) {
        anyhow::bail!("not a valid syftbox app");
    }
    let meta = app_path.join(APP_INFO_FILE);
    if !meta.exists() {
        let id = app_id_from_path(app_path);
        let name = app_name_from_path(app_path);
        return Ok(AppInfo {
            id,
            name,
            path: app_path.display().to_string(),
            source: "local".to_string(),
            source_uri: app_path.display().to_string(),
            branch: "".to_string(),
            tag: "".to_string(),
            commit: "".to_string(),
            installed_on: None,
        });
    }
    let data =
        std::fs::read_to_string(&meta).with_context(|| format!("read {}", meta.display()))?;
    let v = serde_json::from_str::<AppInfo>(&data).context("parse app metadata")?;
    Ok(v)
}

fn remove_app_path(path: &Path) -> Result<()> {
    let meta =
        std::fs::symlink_metadata(path).with_context(|| format!("stat {}", path.display()))?;
    if meta.file_type().is_symlink() || meta.is_file() {
        std::fs::remove_file(path).with_context(|| format!("remove {}", path.display()))?;
        return Ok(());
    }
    std::fs::remove_dir_all(path).with_context(|| format!("remove {}", path.display()))?;
    Ok(())
}

#[cfg(unix)]
fn create_symlink_dir(src: &Path, dst: &Path) -> std::io::Result<()> {
    std::os::unix::fs::symlink(src, dst)
}

#[cfg(windows)]
fn create_symlink_dir(src: &Path, dst: &Path) -> std::io::Result<()> {
    std::os::windows::fs::symlink_dir(src, dst)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::config::ConfigOverrides;
    use std::process::Command;

    #[test]
    fn local_install_list_uninstall_formats_like_go() {
        let tmp = std::env::temp_dir().join("syftbox-rs-apps-test");
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();

        let cfg_path = tmp.join("config.json");
        std::fs::write(
            &cfg_path,
            format!(
                r#"{{
                  "email":"alice@example.com",
                  "data_dir":"{}",
                  "server_url":"{}"
                }}"#,
                tmp.display(),
                Config::default_server_url()
            ),
        )
        .unwrap();
        let cfg = Config::load_with_overrides(&cfg_path, ConfigOverrides::default()).unwrap();

        let app_src = tmp.join("demo-app");
        std::fs::create_dir_all(&app_src).unwrap();
        std::fs::write(app_src.join("run.sh"), "#!/bin/sh\necho ok\n").unwrap();

        let app = install_from_path(&app_src, &cfg, false).unwrap();
        assert_eq!(app.id, "local.demo-app");
        assert!(
            app.path.ends_with("/apps/local.demo-app")
                || app.path.ends_with("\\apps\\local.demo-app")
        );

        let apps = list_apps(&cfg).unwrap();
        let listing = format_app_list(&apps_dir(&cfg), &apps);
        assert!(listing.contains("ID      local.demo-app\n"));
        assert!(listing.contains("Source  "));
        assert!(listing.contains("(local)\n"));

        let id = uninstall_app(&cfg, "local.demo-app").unwrap();
        assert_eq!(id, "local.demo-app");
        let apps = list_apps(&cfg).unwrap();
        let listing = format_app_list(&apps_dir(&cfg), &apps);
        assert!(listing.contains("No apps installed at"));
    }

    #[test]
    fn extract_zip_strips_root_dir() {
        // Requires `zip` and `unzip` binaries. Skip if missing.
        if Command::new("zip").arg("-v").output().is_err() {
            return;
        }
        if Command::new("unzip").arg("-v").output().is_err() {
            return;
        }

        let tmp = std::env::temp_dir().join("syftbox-rs-app-zip-test");
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();

        let root = tmp.join("demo-app-main");
        std::fs::create_dir_all(&root).unwrap();
        std::fs::write(root.join("run.sh"), "#!/bin/sh\necho ok\n").unwrap();

        let zip_path = tmp.join("app.zip");
        let status = Command::new("zip")
            .current_dir(&tmp)
            .arg("-qr")
            .arg(&zip_path)
            .arg("demo-app-main")
            .status()
            .unwrap();
        assert!(status.success());

        let dst = tmp.join("out");
        let _ = std::fs::remove_dir_all(&dst);
        extract_zip_strip_root(&zip_path, &dst).unwrap();
        assert!(dst.join("run.sh").is_file());
    }

    #[test]
    fn app_id_from_url_matches_go() {
        let parsed = Url::parse("https://github.com/OpenMined/pingpong").unwrap();
        assert_eq!(app_id_from_url(&parsed), "com.github.openmined.pingpong");

        let parsed = Url::parse("https://github.com/madhavajay/youtube-wrapped").unwrap();
        assert_eq!(
            app_id_from_url(&parsed),
            "com.github.madhavajay.youtube-wrapped"
        );

        let parsed = Url::parse("https://gitlab.com/cznic/sqlite").unwrap();
        assert_eq!(app_id_from_url(&parsed), "com.gitlab.cznic.sqlite");
    }

    #[test]
    fn archive_url_matches_go() {
        let url = Url::parse("https://github.com/OpenMined/demo-app").unwrap();
        assert_eq!(
            get_archive_url(&url, "main", "", "").unwrap(),
            "https://github.com/OpenMined/demo-app/archive/refs/heads/main.zip"
        );
        let url = Url::parse("https://gitlab.com/cznic/sqlite").unwrap();
        assert_eq!(
            get_archive_url(&url, "main", "", "").unwrap(),
            "https://gitlab.com/cznic/sqlite/-/archive/main/archive.zip"
        );
    }
}
