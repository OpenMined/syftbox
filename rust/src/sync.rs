use std::{
    collections::{HashMap, HashSet},
    fs,
    path::{Path, PathBuf},
    time::{SystemTime, UNIX_EPOCH},
};

use anyhow::{Context, Result};
use rusqlite::params;
use serde::{Deserialize, Serialize};
use walkdir::WalkDir;

use crate::control::ControlPlane;
use crate::filters::SyncFilters;
use crate::http::{ApiClient, BlobInfo, PresignedParams};
use crate::uploader::upload_blob_smart;

#[derive(Debug, Clone)]
struct LocalFile {
    key: String,
    path: std::path::PathBuf,
    etag: String,
    size: i64,
    last_modified: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FileMetadata {
    pub etag: String,
    pub size: i64,
    pub last_modified: i64,
    #[serde(default)]
    pub version: String,
    /// Epoch seconds when this key last completed a sync operation.
    #[serde(default)]
    pub completed_at: i64,
}

#[derive(Debug, Default, Serialize, Deserialize)]
struct JournalState {
    files: HashMap<String, FileMetadata>,
}

const SYNC_JOURNAL_SCHEMA: &str = r#"
CREATE TABLE IF NOT EXISTS sync_journal (
    path TEXT PRIMARY KEY,
    etag TEXT NOT NULL,
    version TEXT NOT NULL,
    size INTEGER NOT NULL,
    last_modified TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_journal_path ON sync_journal(path);
CREATE INDEX IF NOT EXISTS idx_journal_etag ON sync_journal(etag);
CREATE INDEX IF NOT EXISTS idx_journal_last_modified ON sync_journal(last_modified);
"#;

pub(crate) struct SyncJournal {
    db_path: PathBuf,
    state: JournalState,
}

impl SyncJournal {
    pub(crate) fn load(data_dir: &Path) -> Result<Self> {
        let db_path = data_dir.join(".data").join("sync.db");
        if let Some(parent) = db_path.parent() {
            fs::create_dir_all(parent)?;
        }

        let conn = rusqlite::Connection::open(&db_path)
            .with_context(|| format!("open journal {}", db_path.display()))?;
        conn.execute_batch(SYNC_JOURNAL_SCHEMA)
            .context("init sync journal schema")?;

        let mut state = JournalState::default();
        let mut stmt =
            conn.prepare("SELECT path, size, etag, version, last_modified FROM sync_journal")?;
        let mut rows = stmt.query([])?;
        while let Some(row) = rows.next()? {
            let path: String = row.get(0)?;
            let size: i64 = row.get(1)?;
            let etag: String = row.get(2)?;
            let version: String = row.get(3)?;
            let last_modified: String = row.get(4)?;

            let lm_epoch = parse_rfc3339_epoch(&last_modified).unwrap_or(0);
            state.files.insert(
                path,
                FileMetadata {
                    etag,
                    size,
                    last_modified: lm_epoch,
                    version,
                    completed_at: 0,
                },
            );
        }

        Ok(SyncJournal { db_path, state })
    }

    fn save(&self) -> Result<()> {
        if let Some(parent) = self.db_path.parent() {
            fs::create_dir_all(parent)?;
        }

        let mut conn = rusqlite::Connection::open(&self.db_path)
            .with_context(|| format!("open journal {}", self.db_path.display()))?;
        conn.execute_batch(SYNC_JOURNAL_SCHEMA)
            .context("init sync journal schema")?;

        let tx = conn.transaction().context("begin sync journal tx")?;
        tx.execute("DELETE FROM sync_journal", [])
            .context("truncate sync journal")?;

        {
            let mut stmt = tx.prepare(
                "INSERT OR REPLACE INTO sync_journal (path, size, etag, version, last_modified) VALUES (?1, ?2, ?3, ?4, ?5)",
            )?;

            for (path, meta) in &self.state.files {
                let last_modified = epoch_to_rfc3339(meta.last_modified);
                stmt.execute(params![
                    path,
                    meta.size,
                    meta.etag,
                    meta.version,
                    last_modified
                ])?;
            }
        }

        tx.commit().context("commit sync journal tx")?;
        Ok(())
    }

    fn get(&self, key: &str) -> Option<&FileMetadata> {
        self.state.files.get(key)
    }

    fn set(&mut self, key: String, meta: FileMetadata) {
        self.state.files.insert(key, meta);
    }

    fn delete(&mut self, key: &str) {
        self.state.files.remove(key);
    }

    fn count(&self) -> usize {
        self.state.files.len()
    }

    fn rebuild_if_empty(
        &mut self,
        local: &HashMap<String, LocalFile>,
        remote: &HashMap<String, BlobInfo>,
    ) {
        if self.count() > 0 {
            return;
        }
        for (key, l) in local {
            if let Some(r) = remote.get(key) {
                if l.etag == r.etag {
                    self.set(
                        key.clone(),
                        FileMetadata {
                            etag: l.etag.clone(),
                            size: l.size,
                            last_modified: l.last_modified,
                            version: String::new(),
                            completed_at: 0,
                        },
                    );
                }
            }
        }
    }
}

fn epoch_to_rfc3339(epoch_seconds: i64) -> String {
    chrono::DateTime::<chrono::Utc>::from_timestamp(epoch_seconds, 0)
        .unwrap_or_else(chrono::Utc::now)
        .to_rfc3339()
}

fn parse_rfc3339_epoch(raw: &str) -> Option<i64> {
    let parsed = chrono::DateTime::parse_from_rfc3339(raw).ok()?;
    Some(parsed.with_timezone(&chrono::Utc).timestamp())
}

pub async fn sync_once_with_control(
    api: &ApiClient,
    data_dir: &Path,
    control: Option<ControlPlane>,
    filters: &SyncFilters,
    journal: &mut SyncJournal,
) -> Result<()> {
    let datasites_root = data_dir.join("datasites");
    let local = scan_local(&datasites_root, filters)?;
    let remote = scan_remote(api, filters).await?;

    journal.rebuild_if_empty(&local, &remote);

    let mut all_keys: HashSet<String> = HashSet::new();
    all_keys.extend(local.keys().cloned());
    all_keys.extend(remote.keys().cloned());
    all_keys.extend(journal.state.files.keys().cloned());

    let mut upload_keys = Vec::new();
    let mut download_keys_list = Vec::new();
    let mut remote_deletes = Vec::new();
    let mut local_deletes = Vec::new();
    let mut conflicts = Vec::new();

    for key in all_keys {
        if !is_synced_key(&key) {
            continue;
        }
        if filters.ignore.should_ignore_rel(Path::new(&key), false) {
            continue;
        }
        if SyncFilters::is_marked_rel_path(&key) {
            continue;
        }
        let local_meta = local.get(&key);
        let remote_meta = remote.get(&key);
        let journal_meta = journal.get(&key);

        let local_exists = local_meta.is_some();
        let remote_exists = remote_meta.is_some();
        let journal_exists = journal_meta.is_some();

        // Recent-complete grace window to avoid spurious conflicts on rapid overwrites.
        if let Some(jm) = journal_meta {
            let now = chrono::Utc::now().timestamp();
            if jm.completed_at > 0 && now - jm.completed_at < 5 {
                let remote_changed =
                    remote_exists && has_modified_remote(Some(jm), remote_meta.unwrap());
                if !remote_changed {
                    continue;
                }
            }
        }

        let local_created = local_exists && !journal_exists && !remote_exists;
        let remote_created = !local_exists && !journal_exists && remote_exists;
        let local_deleted = !local_exists && journal_exists && remote_exists;
        let remote_deleted = local_exists && journal_exists && !remote_exists;

        let local_modified = local_exists && has_modified_local(local_meta.unwrap(), journal_meta);
        let remote_modified =
            remote_exists && has_modified_remote(journal_meta, remote_meta.unwrap());

        if !local_exists && !remote_exists && journal_exists {
            journal.delete(&key);
            continue;
        }

        if (local_modified && remote_modified) || (local_created && remote_created) {
            // Both diverged from the common ancestor (journal). Mirror Go's "consistent history
            // wins" behavior by treating the server as authoritative and preserving the local
            // divergent edits as a `.conflict` marker.
            if local_exists && remote_exists {
                let l = local_meta.unwrap();
                let r = remote_meta.unwrap();
                // If both sides converge to the same content and only the journal is behind,
                // just advance the journal and avoid creating a spurious conflict marker.
                if !l.etag.is_empty() && l.etag == r.etag {
                    journal.set(
                        key.clone(),
                        FileMetadata {
                            etag: r.etag.clone(),
                            size: r.size,
                            last_modified: r.last_modified.timestamp(),
                            version: String::new(),
                            completed_at: chrono::Utc::now().timestamp(),
                        },
                    );
                    continue;
                }
            }

            conflicts.push(key);
            continue;
        }

        if local_created || local_modified {
            upload_keys.push(key);
        } else if remote_created || remote_modified {
            download_keys_list.push(key);
        } else if local_deleted {
            remote_deletes.push(key);
        } else if remote_deleted {
            local_deletes.push(key);
        }
    }

    // Conflicts: mark local file as conflicted (rename), leave remote unchanged.
    for key in conflicts {
        if let Some(l) = local.get(&key) {
            let abs = datasites_root.join(&l.key);
            let _ = mark_conflict(&abs);
        }
        // Mirror Go behavior: remove journal entry so the remote winner
        // is pulled and avoid treating this as a delete.
        journal.delete(&key);
        // Pull the server's version immediately so the main path is restored.
        download_keys_list.push(key);
    }

    // Remote writes (uploads)
    for key in upload_keys {
        if let Some(l) = local.get(&key) {
            if let Err(err) = upload_blob_smart(api, control.as_ref(), data_dir, &l.key, &l.path).await {
                eprintln!("sync upload error for {}: {err:?}", l.key);
                continue;
            }
            journal.set(
                l.key.clone(),
                FileMetadata {
                    etag: l.etag.clone(),
                    size: l.size,
                    last_modified: l.last_modified,
                    version: String::new(),
                    completed_at: chrono::Utc::now().timestamp(),
                },
            );
        }
    }

    // Local writes (downloads)
    if !download_keys_list.is_empty() {
        download_keys(api, &datasites_root, download_keys_list.clone()).await?;
        for key in download_keys_list {
            if let Some(r) = remote.get(&key) {
                journal.set(
                    key.clone(),
                    FileMetadata {
                        etag: r.etag.clone(),
                        size: r.size,
                        last_modified: r.last_modified.timestamp(),
                        version: String::new(),
                        completed_at: chrono::Utc::now().timestamp(),
                    },
                );
            }
        }
    }

    // Remote deletes
    if !remote_deletes.is_empty() {
        if let Err(err) = api.delete_blobs(&remote_deletes).await {
            eprintln!("sync remote delete error: {err:?}");
        } else {
            for key in remote_deletes {
                journal.delete(&key);
            }
        }
    }

    // Local deletes
    for key in local_deletes {
        let abs = datasites_root.join(&key);
        if abs.exists() {
            let meta = fs::metadata(&abs)?;
            if meta.is_dir() {
                let _ = fs::remove_dir_all(&abs);
            } else {
                let _ = fs::remove_file(&abs);
            }
        }
        journal.delete(&key);
    }

    journal.save()?;

    Ok(())
}

fn has_modified_local(local: &LocalFile, journal: Option<&FileMetadata>) -> bool {
    has_modified(
        journal.map(|j| (j.etag.as_str(), j.size, j.last_modified)),
        Some((&local.etag, local.size, local.last_modified)),
    )
}

fn has_modified_remote(journal: Option<&FileMetadata>, remote: &BlobInfo) -> bool {
    has_modified(
        journal.map(|j| (j.etag.as_str(), j.size, j.last_modified)),
        Some((&remote.etag, remote.size, remote.last_modified.timestamp())),
    )
}

fn has_modified(a: Option<(&str, i64, i64)>, b: Option<(&str, i64, i64)>) -> bool {
    match (a, b) {
        (None, None) => false,
        (None, Some(_)) | (Some(_), None) => true,
        (Some((etag_a, size_a, lm_a)), Some((etag_b, size_b, lm_b))) => {
            if !etag_a.is_empty() && !etag_b.is_empty() && etag_a != etag_b {
                return true;
            }
            if size_a != size_b {
                return true;
            }
            lm_a != lm_b
        }
    }
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
        ensure_parent_dirs(&target)?;
        let bytes = api.http().get(&blob.url).send().await?.bytes().await?;
        api.stats().on_recv(bytes.len() as i64);
        write_file_resolving_conflicts(&target, &bytes)?;
    }
    Ok(())
}

/// Ensure parent directories exist for `target`. If a parent path exists as a file,
/// remove it so that remote directory structure can be created.
pub(crate) fn ensure_parent_dirs(target: &std::path::Path) -> Result<()> {
    let Some(parent) = target.parent() else {
        return Ok(());
    };

    match fs::create_dir_all(parent) {
        Ok(_) => Ok(()),
        Err(_err) => {
            // Find the nearest existing ancestor that is not a directory and remove it.
            let mut cur = parent.to_path_buf();
            loop {
                if cur.exists() {
                    let meta = fs::metadata(&cur)?;
                    if !meta.is_dir() {
                        fs::remove_file(&cur)?;
                    }
                    break;
                }
                if let Some(up) = cur.parent() {
                    cur = up.to_path_buf();
                } else {
                    break;
                }
            }
            fs::create_dir_all(parent)?;
            Ok(())
        }
    }
}

/// Write `bytes` to `target`, removing any conflicting directory first.
pub(crate) fn write_file_resolving_conflicts(target: &std::path::Path, bytes: &[u8]) -> Result<()> {
    match fs::write(target, bytes) {
        Ok(_) => Ok(()),
        Err(err) => {
            if target.exists() {
                let meta = fs::metadata(target)?;
                if meta.is_dir() {
                    fs::remove_dir_all(target)?;
                    fs::write(target, bytes)?;
                    return Ok(());
                }
            }
            Err(err).with_context(|| format!("write {}", target.display()))
        }
    }
}

fn is_marked_key(key: &str) -> bool {
    // Equivalent to Go IsMarkedPath checks on filenames.
    key.contains(".conflict") || key.contains(".rejected")
}

fn is_synced_key(key: &str) -> bool {
    // Devstack-focused scope (still not full Go parity): public datasite contents,
    // ACL files, and RPC request/response files.
    key.contains("/public/")
        || key.ends_with("syft.pub.yaml")
        || key.ends_with(".request")
        || key.ends_with(".response")
}

fn should_ignore_key(filters: &SyncFilters, key: &str) -> bool {
    filters.ignore.should_ignore_rel(Path::new(key), false) || SyncFilters::is_marked_rel_path(key)
}

fn scan_local(datasites_root: &Path, filters: &SyncFilters) -> Result<HashMap<String, LocalFile>> {
    let mut out = HashMap::new();
    if !datasites_root.exists() {
        return Ok(out);
    }
    for entry in WalkDir::new(datasites_root)
        .into_iter()
        .filter_entry(|e| e.file_name() != ".data")
        .filter_map(|e| e.ok())
    {
        let ftype = entry.file_type();
        if ftype.is_dir() || ftype.is_symlink() {
            continue;
        }
        let path = entry.path();
        let rel = path
            .strip_prefix(datasites_root)
            .with_context(|| format!("strip prefix {}", path.display()))?;
        if filters.ignore.should_ignore_rel(rel, false) {
            continue;
        }
        let key = rel.to_string_lossy().to_string();
        if !is_synced_key(&key) {
            continue;
        }
        if is_marked_key(&key) {
            continue;
        }
        let meta = entry.metadata()?;
        let etag = compute_md5(path)?;
        let size = meta.len() as i64;
        let last_modified = meta.modified().map(to_epoch_seconds).unwrap_or(0);
        out.insert(
            key.clone(),
            LocalFile {
                key,
                path: path.to_path_buf(),
                etag,
                size,
                last_modified,
            },
        );
    }
    Ok(out)
}

async fn scan_remote(api: &ApiClient, filters: &SyncFilters) -> Result<HashMap<String, BlobInfo>> {
    let mut out = HashMap::new();
    let view = api.datasite_view().await?;
    for file in view.files {
        if should_ignore_key(filters, &file.key) {
            continue;
        }
        if is_synced_key(&file.key) && !is_marked_key(&file.key) {
            out.insert(file.key.clone(), file);
        }
    }
    Ok(out)
}

fn compute_md5(path: &Path) -> Result<String> {
    let data = fs::read(path)?;
    let digest = md5::compute(&data);
    Ok(format!("{:x}", digest))
}

fn to_epoch_seconds(t: SystemTime) -> i64 {
    t.duration_since(UNIX_EPOCH)
        .map(|d| d.as_secs() as i64)
        .unwrap_or(0)
}

fn mark_conflict(path: &Path) -> Result<()> {
    if !path.exists() {
        return Ok(());
    }
    let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("");
    let base = if ext.is_empty() {
        path.to_path_buf()
    } else {
        PathBuf::from(path.to_string_lossy().trim_end_matches(&format!(".{ext}")))
    };
    let marked = if ext.is_empty() {
        PathBuf::from(format!("{}.conflict", base.to_string_lossy()))
    } else {
        PathBuf::from(format!("{}.conflict.{ext}", base.to_string_lossy()))
    };

    if marked.exists() {
        let ts = chrono::Utc::now().format("%Y%m%d%H%M%S");
        let rotated = if ext.is_empty() {
            PathBuf::from(format!("{}.conflict.{ts}", base.to_string_lossy()))
        } else {
            PathBuf::from(format!("{}.conflict.{ts}.{ext}", base.to_string_lossy()))
        };
        let _ = fs::rename(&marked, rotated);
    }

    fs::rename(path, marked)?;
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::filters::SyncFilters;
    use std::io::Write;
    use std::time::SystemTime;

    #[test]
    fn scan_local_empty_dir() {
        let root = make_temp_dir();
        let filters = SyncFilters::load(&root).unwrap();
        let state = scan_local(&root, &filters).unwrap();
        assert!(state.is_empty());
    }

    #[test]
    fn scan_local_collects_files_and_md5() {
        let root = make_temp_dir();
        let f1 = root.join("alice@example.com/public/a.txt");
        fs::create_dir_all(f1.parent().unwrap()).unwrap();
        let mut file = fs::File::create(&f1).unwrap();
        writeln!(file, "hello").unwrap();

        let filters = SyncFilters::load(&root).unwrap();
        let state = scan_local(&root, &filters).unwrap();
        let key = "alice@example.com/public/a.txt".to_string();
        assert!(state.contains_key(&key));
        let meta = state.get(&key).unwrap();
        assert_eq!(meta.key, key);
        assert!(!meta.etag.is_empty());

        let computed = compute_md5(&f1).unwrap();
        assert_eq!(computed, meta.etag);
    }

    fn make_temp_dir() -> PathBuf {
        let mut root = std::env::temp_dir();
        let nanos = SystemTime::now()
            .duration_since(SystemTime::UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        root.push(format!("syftbox-rs-sync-test-{nanos}"));
        fs::create_dir_all(&root).unwrap();
        root
    }
}
