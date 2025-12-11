use std::{collections::HashMap, fs, path::Path};

use anyhow::{Context, Result};
use walkdir::WalkDir;

use crate::control::ControlPlane;
use crate::http::{ApiClient, BlobInfo, PresignedParams};

#[derive(Debug, Clone)]
struct LocalFile {
    key: String,
    path: std::path::PathBuf,
    etag: String,
}

pub async fn sync_once_with_control(
    api: &ApiClient,
    data_dir: &Path,
    my_email: &str,
    control: Option<ControlPlane>,
) -> Result<()> {
    let datasites_root = data_dir.join("datasites");
    let local = scan_local(&datasites_root)?;
    let remote = scan_remote(api).await?;

    // Upload local changes for my datasite only.
    for file in local.values() {
        if !file.key.starts_with(my_email) {
            continue;
        }
        match remote.get(&file.key) {
            Some(r) if r.etag == file.etag => {}
            _ => {
                api.upload_blob(&file.key, &file.path).await?;
                if let Some(cp) = &control {
                    let size = std::fs::metadata(&file.path)
                        .map(|m| m.len() as i64)
                        .unwrap_or(0);
                    cp.record_upload(file.key.clone(), size);
                }
            }
        }
    }

    // Download missing/updated remote files (skip my own to avoid churn).
    let mut download_list = Vec::new();
    for (key, meta) in &remote {
        if key.starts_with(my_email) {
            continue;
        }
        match local.get(key) {
            Some(l) if l.etag == meta.etag => {}
            _ => download_list.push(key.clone()),
        }
    }

    download_keys(api, &datasites_root, download_list).await?;

    Ok(())
}

pub async fn download_keys(
    api: &ApiClient,
    datasites_root: &Path,
    keys: Vec<String>,
) -> Result<()> {
    if keys.is_empty() {
        return Ok(());
    }
    let presigned = api
        .get_blob_presigned(&PresignedParams { keys: keys.clone() })
        .await?;
    for blob in presigned.urls {
        let target = datasites_root.join(&blob.key);
        if let Some(parent) = target.parent() {
            fs::create_dir_all(parent)?;
        }
        let bytes = api.http().get(&blob.url).send().await?.bytes().await?;
        fs::write(&target, &bytes)?;
    }
    Ok(())
}

fn scan_local(datasites_root: &Path) -> Result<HashMap<String, LocalFile>> {
    let mut out = HashMap::new();
    if !datasites_root.exists() {
        return Ok(out);
    }
    for entry in WalkDir::new(datasites_root)
        .into_iter()
        .filter_map(|e| e.ok())
    {
        if entry.file_type().is_dir() {
            continue;
        }
        let path = entry.path();
        let rel = path
            .strip_prefix(datasites_root)
            .with_context(|| format!("strip prefix {}", path.display()))?;
        let key = rel.to_string_lossy().to_string();
        let _meta = entry.metadata()?;
        let etag = compute_md5(path)?;
        out.insert(
            key.clone(),
            LocalFile {
                key,
                path: path.to_path_buf(),
                etag,
            },
        );
    }
    Ok(out)
}

async fn scan_remote(api: &ApiClient) -> Result<HashMap<String, BlobInfo>> {
    let mut out = HashMap::new();
    let view = api.datasite_view().await?;
    for file in view.files {
        out.insert(file.key.clone(), file);
    }
    Ok(out)
}

fn compute_md5(path: &Path) -> Result<String> {
    let data = fs::read(path)?;
    let digest = md5::compute(&data);
    Ok(format!("{:x}", digest))
}
