use std::{fs, path::Path, time::SystemTime};

use anyhow::{Context, Result};
use walkdir::WalkDir;

// Temporary local-mirror sync to satisfy devstack warm-up while HTTP sync is ported.
pub fn sync_once(root: &Path) -> Result<()> {
    let clients = list_clients(root)?;
    for src in &clients {
        let src_datasite = root.join(src).join("datasites").join(src);
        if !src_datasite.exists() {
            continue;
        }
        for tgt in &clients {
            if tgt == src {
                continue;
            }
            let dest_base = root.join(tgt).join("datasites").join(src);
            copy_datasite(&src_datasite, &dest_base)?;
        }
    }
    Ok(())
}

fn list_clients(root: &Path) -> Result<Vec<String>> {
    let mut out = Vec::new();
    for entry in
        fs::read_dir(root).with_context(|| format!("list clients in {}", root.display()))?
    {
        let entry = entry?;
        if entry.file_type()?.is_dir() {
            if let Some(name) = entry.file_name().to_str() {
                out.push(name.to_owned());
            }
        }
    }
    Ok(out)
}

fn copy_datasite(src_base: &Path, dest_base: &Path) -> Result<()> {
    for entry in WalkDir::new(src_base).into_iter().filter_map(|e| e.ok()) {
        let path = entry.path();
        if entry.file_type().is_dir() {
            continue;
        }

        let rel = path.strip_prefix(src_base).unwrap_or(path);
        let dest = dest_base.join(rel);

        if let Some(parent) = dest.parent() {
            fs::create_dir_all(parent)?;
        }

        if let Ok((src_meta, dest_meta)) = file_meta_pair(path, &dest) {
            if !needs_copy(&src_meta, &dest_meta) {
                continue;
            }
        }

        fs::copy(path, &dest)?;
    }
    Ok(())
}

fn file_meta_pair(src: &Path, dest: &Path) -> Result<(fs::Metadata, fs::Metadata)> {
    let src_meta = fs::metadata(src)?;
    let dest_meta = fs::metadata(dest)?;
    Ok((src_meta, dest_meta))
}

fn needs_copy(src: &fs::Metadata, dest: &fs::Metadata) -> bool {
    if src.len() != dest.len() {
        return true;
    }
    let src_time = src.modified().unwrap_or(SystemTime::UNIX_EPOCH);
    let dest_time = dest.modified().unwrap_or(SystemTime::UNIX_EPOCH);
    src_time > dest_time
}
