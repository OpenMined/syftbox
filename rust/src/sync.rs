use std::{
    collections::{HashMap, HashSet},
    fs,
    io::Read,
    path::{Path, PathBuf},
    sync::atomic::{AtomicBool, Ordering},
};

use anyhow::{Context, Result};
use futures_util::StreamExt;
use reqwest::StatusCode;
use rusqlite::params;
use serde::{Deserialize, Serialize};
use tokio::io::AsyncWriteExt;
use walkdir::WalkDir;

use crate::control::ControlPlane;
use crate::filters::SyncFilters;
use crate::http::{ApiClient, BlobInfo, HttpStatusError, PresignedParams};
use crate::uploader::upload_blob_smart;

static OWNER_MISMATCH_LOGGED: AtomicBool = AtomicBool::new(false);

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
    #[serde(default)]
    pub local_etag: String,
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
    local_etag TEXT NOT NULL DEFAULT '',
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
    dirty: HashSet<String>,
    deleted: HashSet<String>,
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
        ensure_local_etag_column(&conn).context("migrate sync journal")?;

        let mut state = JournalState::default();
        let mut stmt = conn.prepare(
            "SELECT path, size, etag, local_etag, version, last_modified FROM sync_journal",
        )?;
        let mut rows = stmt.query([])?;
        while let Some(row) = rows.next()? {
            let path: String = row.get(0)?;
            let size: i64 = row.get(1)?;
            let etag: String = row.get(2)?;
            let local_etag: String = row.get(3)?;
            let version: String = row.get(4)?;
            let last_modified: String = row.get(5)?;

            let lm_epoch = parse_rfc3339_epoch(&last_modified).unwrap_or(0);
            state.files.insert(
                path,
                FileMetadata {
                    etag,
                    local_etag,
                    size,
                    last_modified: lm_epoch,
                    version,
                    completed_at: 0,
                },
            );
        }

        Ok(SyncJournal {
            db_path,
            state,
            dirty: HashSet::new(),
            deleted: HashSet::new(),
        })
    }

    fn refresh_from_disk(&mut self) -> Result<()> {
        if let Some(parent) = self.db_path.parent() {
            fs::create_dir_all(parent)?;
        }

        let conn = rusqlite::Connection::open(&self.db_path)
            .with_context(|| format!("open journal {}", self.db_path.display()))?;
        conn.execute_batch(SYNC_JOURNAL_SCHEMA)
            .context("init sync journal schema")?;
        ensure_local_etag_column(&conn).context("migrate sync journal")?;

        let mut next = HashMap::new();
        let mut stmt = conn.prepare(
            "SELECT path, size, etag, local_etag, version, last_modified FROM sync_journal",
        )?;
        let mut rows = stmt.query([])?;
        while let Some(row) = rows.next()? {
            let path: String = row.get(0)?;
            let size: i64 = row.get(1)?;
            let etag: String = row.get(2)?;
            let local_etag: String = row.get(3)?;
            let version: String = row.get(4)?;
            let last_modified: String = row.get(5)?;

            let lm_epoch = parse_rfc3339_epoch(&last_modified).unwrap_or(0);
            let completed_at = self
                .state
                .files
                .get(&path)
                .map(|m| m.completed_at)
                .unwrap_or(0);
            next.insert(
                path,
                FileMetadata {
                    etag,
                    local_etag,
                    size,
                    last_modified: lm_epoch,
                    version,
                    completed_at,
                },
            );
        }

        self.state.files = next;
        self.dirty.clear();
        self.deleted.clear();
        Ok(())
    }

    fn save(&mut self) -> Result<()> {
        if let Some(parent) = self.db_path.parent() {
            fs::create_dir_all(parent)?;
        }

        let mut conn = rusqlite::Connection::open(&self.db_path)
            .with_context(|| format!("open journal {}", self.db_path.display()))?;
        conn.execute_batch(SYNC_JOURNAL_SCHEMA)
            .context("init sync journal schema")?;
        ensure_local_etag_column(&conn).context("migrate sync journal")?;

        let tx = conn.transaction().context("begin sync journal tx")?;
        {
            let mut delete_stmt = tx.prepare("DELETE FROM sync_journal WHERE path = ?1")?;
            for key in &self.deleted {
                delete_stmt.execute(params![key])?;
            }
        }

        {
            let mut upsert_stmt = tx.prepare(
                "INSERT OR REPLACE INTO sync_journal (path, size, etag, local_etag, version, last_modified) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
            )?;
            for key in &self.dirty {
                if let Some(meta) = self.state.files.get(key) {
                    let last_modified = epoch_to_rfc3339(meta.last_modified);
                    upsert_stmt.execute(params![
                        key,
                        meta.size,
                        meta.etag,
                        meta.local_etag,
                        meta.version,
                        last_modified
                    ])?;
                }
            }
        }

        tx.commit().context("commit sync journal tx")?;
        self.dirty.clear();
        self.deleted.clear();
        Ok(())
    }

    fn get(&self, key: &str) -> Option<&FileMetadata> {
        self.state.files.get(key)
    }

    fn set(&mut self, key: String, meta: FileMetadata) {
        if std::env::var("SYFTBOX_DEBUG_JOURNAL").is_ok() && key.contains("jg-file-") {
            crate::logging::info(format!(
                "debug journal set key={} etag={} size={}",
                key, meta.etag, meta.size
            ));
        }
        self.state.files.insert(key.clone(), meta);
        self.deleted.remove(&key);
        self.dirty.insert(key);
    }

    fn delete(&mut self, key: &str) {
        self.state.files.remove(key);
        self.dirty.remove(key);
        self.deleted.insert(key.to_string());
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
                            local_etag: l.etag.clone(),
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

pub(crate) fn journal_upsert_direct(
    data_dir: &Path,
    key: &str,
    etag: &str,
    local_etag: &str,
    size: i64,
    last_modified_epoch: i64,
) -> Result<()> {
    if std::env::var("SYFTBOX_DEBUG_JOURNAL").is_ok() && key.contains("jg-file-") {
        crate::logging::info(format!(
            "debug journal upsert key={key} etag={} size={size}",
            etag
        ));
    }
    let db_path = data_dir.join(".data").join("sync.db");
    if let Some(parent) = db_path.parent() {
        fs::create_dir_all(parent)?;
    }

    let conn = rusqlite::Connection::open(&db_path)
        .with_context(|| format!("open journal {}", db_path.display()))?;
    conn.execute_batch(SYNC_JOURNAL_SCHEMA)
        .context("init sync journal schema")?;
    ensure_local_etag_column(&conn).context("migrate sync journal")?;

    let last_modified = epoch_to_rfc3339(last_modified_epoch);
    conn.execute(
        "INSERT OR REPLACE INTO sync_journal (path, size, etag, local_etag, version, last_modified) VALUES (?1, ?2, ?3, ?4, ?5, ?6)",
        params![key, size, etag, local_etag, "", last_modified],
    )
    .context("upsert sync journal row")?;

    Ok(())
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

fn ensure_local_etag_column(conn: &rusqlite::Connection) -> Result<()> {
    let mut stmt = conn.prepare("PRAGMA table_info(sync_journal)")?;
    let mut rows = stmt.query([])?;
    while let Some(row) = rows.next()? {
        let name: String = row.get(1)?;
        if name == "local_etag" {
            return Ok(());
        }
    }
    conn.execute(
        "ALTER TABLE sync_journal ADD COLUMN local_etag TEXT NOT NULL DEFAULT ''",
        [],
    )?;
    Ok(())
}

pub async fn sync_once_with_control(
    api: &ApiClient,
    data_dir: &Path,
    owner_email: &str,
    control: Option<ControlPlane>,
    filters: &SyncFilters,
    local_scanner: &mut LocalScanner,
    journal: &mut SyncJournal,
) -> Result<()> {
    journal
        .refresh_from_disk()
        .context("refresh sync journal")?;

    let token_subject = api
        .current_access_token()
        .await
        .and_then(|t| crate::auth::token_subject(&t));
    let owner_mismatch = token_subject
        .as_deref()
        .is_some_and(|sub| sub != owner_email);
    if owner_mismatch && !OWNER_MISMATCH_LOGGED.swap(true, Ordering::SeqCst) {
        crate::logging::error(format!(
            "sync identity mismatch: config email={} token subject={}",
            owner_email,
            token_subject.as_deref().unwrap_or("")
        ));
    }

    let datasites_root = data_dir.join("datasites");
    let local = local_scanner.scan(&datasites_root, filters)?;
    let remote = scan_remote(api, filters).await?;

    journal.rebuild_if_empty(&local, &remote);

    let heal_journal_gaps = journal_gap_healing_enabled();

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

        let is_owner = is_owner_sync_key(owner_email, &key);

        // Special case: both local and remote exist but journal is missing.
        //
        // When journal entries are missing, the default baseline comparison can treat both sides as
        // modified, which leads to spurious conflict markers for mirrored (non-owner) paths.
        //
        // - If content appears identical, treat as unchanged (and optionally heal the journal).
        // - If content differs and this is a mirrored path, prefer a download (server-wins) and
        //   avoid creating conflict markers.
        if local_exists && remote_exists && !journal_exists {
            let l = local_meta.unwrap();
            let r = remote_meta.unwrap();
            let differs = content_differs_for_key(
                is_owner,
                &l.etag,
                l.size,
                &r.etag,
                r.size,
                l.last_modified,
                r.last_modified.timestamp(),
            );
            if !differs {
                if heal_journal_gaps {
                    journal.set(
                        key.clone(),
                        FileMetadata {
                            etag: r.etag.clone(),
                            local_etag: l.etag.clone(),
                            size: r.size,
                            last_modified: r.last_modified.timestamp(),
                            version: String::new(),
                            completed_at: chrono::Utc::now().timestamp(),
                        },
                    );
                }
                continue;
            }

            if !is_owner {
                download_keys_list.push(key);
                continue;
            }
        }

        // Recent-complete grace window to avoid spurious conflicts on rapid overwrites.
        if let Some(jm) = journal_meta {
            let now = chrono::Utc::now().timestamp();
            if jm.completed_at > 0 && now - jm.completed_at < 5 {
                let remote_changed =
                    remote_exists && has_modified_remote(is_owner, Some(jm), remote_meta.unwrap());
                if !remote_changed {
                    continue;
                }
            }
        }

        let local_created = local_exists && !journal_exists && !remote_exists;
        let remote_created = !local_exists && !journal_exists && remote_exists;
        let local_deleted = !local_exists && journal_exists && remote_exists;
        let remote_deleted = local_exists && journal_exists && !remote_exists;

        let local_modified =
            local_exists && has_modified_local(is_owner, local_meta.unwrap(), journal_meta);
        let remote_modified =
            remote_exists && has_modified_remote(is_owner, journal_meta, remote_meta.unwrap());

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
                            local_etag: l.etag.clone(),
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
            if let Some(cp) = control.as_ref() {
                cp.set_sync_conflicted(&l.key);
            }
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
            if rejected_marker_exists(&l.path) {
                // Mirror Go behavior: once a rejected marker exists for this base path, do not
                // keep retrying uploads until resolved; drop journal so the remote winner can be
                // pulled if present.
                if let Some(cp) = control.as_ref() {
                    cp.set_sync_rejected(&l.key);
                }
                journal.delete(&key);
                continue;
            }

            if let Err(err) =
                upload_blob_smart(api, control.as_ref(), data_dir, &l.key, &l.path).await
            {
                let forbidden = err
                    .downcast_ref::<HttpStatusError>()
                    .is_some_and(|e| e.status == StatusCode::FORBIDDEN);
                if forbidden {
                    let _ = mark_rejected(&l.path);
                    if let Some(cp) = control.as_ref() {
                        cp.set_sync_rejected(&l.key);
                    }
                    journal.delete(&key);
                }

                crate::logging::error(format!("sync upload error for {}: {err:?}", l.key));
                continue;
            }
            journal.set(
                l.key.clone(),
                FileMetadata {
                    etag: l.etag.clone(),
                    local_etag: l.etag.clone(),
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
        download_keys_list.sort();
        download_keys_list.dedup();

        if let Some(cp) = control.as_ref() {
            for key in &download_keys_list {
                cp.set_sync_syncing(key, 0.0);
            }
        }

        let staged = stage_downloads(api, &datasites_root, &download_keys_list).await?;
        commit_staged_downloads(staged).await?;

        for key in &download_keys_list {
            if let Some(r) = remote.get(key) {
                let local_path = datasites_root.join(key);
                let local_etag = match fs::metadata(&local_path) {
                    Ok(meta) => match compute_local_etag(&local_path, meta.len() as i64) {
                        Ok(etag) => etag,
                        Err(err) => {
                            crate::logging::error(format!(
                                "sync download hash error for {}: {err:?}",
                                key
                            ));
                            String::new()
                        }
                    },
                    Err(_) => String::new(),
                };
                journal.set(
                    key.clone(),
                    FileMetadata {
                        etag: r.etag.clone(),
                        local_etag,
                        size: r.size,
                        last_modified: r.last_modified.timestamp(),
                        version: String::new(),
                        completed_at: chrono::Utc::now().timestamp(),
                    },
                );
            }
        }

        journal.save()?;
        if let Some(cp) = control.as_ref() {
            for key in &download_keys_list {
                cp.set_sync_completed(key);
            }
        }
    }

    // Remote deletes
    if !remote_deletes.is_empty() {
        if let Err(err) = api.delete_blobs(&remote_deletes).await {
            crate::logging::error(format!("sync remote delete error: {err:?}"));
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

fn journal_gap_healing_enabled() -> bool {
    match std::env::var("SYFTBOX_SYNC_HEAL_JOURNAL_GAPS") {
        Ok(raw) => {
            let v = raw.trim().to_lowercase();
            v != "0" && v != "false"
        }
        Err(_) => true,
    }
}

fn is_owner_sync_key(owner_email: &str, key: &str) -> bool {
    key.strip_prefix(owner_email)
        .is_some_and(|rest| rest.starts_with('/'))
}

fn content_differs_for_key(
    is_owner: bool,
    etag_a: &str,
    size_a: i64,
    etag_b: &str,
    size_b: i64,
    lm_a: i64,
    lm_b: i64,
) -> bool {
    if size_a != size_b {
        return true;
    }

    let a = normalize_etag(etag_a);
    let b = normalize_etag(etag_b);
    if !a.is_empty() && !b.is_empty() {
        if a == b {
            return false;
        }
        if !is_owner && is_mixed_multipart_etag_pair(&a, &b) {
            // For mirrored paths, tolerate mixed multipart-vs-plain ETags when sizes match to
            // avoid reupload/download loops.
            return false;
        }
        return true;
    }

    // Fallback: if ETags aren't usable, compare last-modified timestamps.
    lm_a != lm_b
}

fn normalize_etag(raw: &str) -> String {
    raw.trim().trim_matches('"').to_ascii_lowercase()
}

fn is_mixed_multipart_etag_pair(a: &str, b: &str) -> bool {
    (is_plain_md5_etag(a) && is_multipart_etag(b)) || (is_multipart_etag(a) && is_plain_md5_etag(b))
}

fn is_plain_md5_etag(etag: &str) -> bool {
    etag.len() == 32 && etag.chars().all(|c| c.is_ascii_hexdigit())
}

fn is_multipart_etag(etag: &str) -> bool {
    let Some((left, right)) = etag.split_once('-') else {
        return false;
    };
    is_plain_md5_etag(left) && !right.is_empty() && right.chars().all(|c| c.is_ascii_digit())
}

struct CompareMeta<'a> {
    etag: &'a str,
    local_etag: &'a str,
    size: i64,
    last_modified: i64,
}

fn has_modified_local(is_owner: bool, local: &LocalFile, journal: Option<&FileMetadata>) -> bool {
    has_modified(
        journal.map(|j| CompareMeta {
            etag: j.etag.as_str(),
            local_etag: j.local_etag.as_str(),
            size: j.size,
            last_modified: j.last_modified,
        }),
        Some(CompareMeta {
            etag: local.etag.as_str(),
            local_etag: local.etag.as_str(),
            size: local.size,
            last_modified: local.last_modified,
        }),
        is_owner,
    )
}

fn has_modified_remote(is_owner: bool, journal: Option<&FileMetadata>, remote: &BlobInfo) -> bool {
    has_modified(
        journal.map(|j| CompareMeta {
            etag: j.etag.as_str(),
            local_etag: j.local_etag.as_str(),
            size: j.size,
            last_modified: j.last_modified,
        }),
        Some(CompareMeta {
            etag: remote.etag.as_str(),
            local_etag: "",
            size: remote.size,
            last_modified: remote.last_modified.timestamp(),
        }),
        is_owner,
    )
}

fn has_modified(a: Option<CompareMeta<'_>>, b: Option<CompareMeta<'_>>, is_owner: bool) -> bool {
    match (a, b) {
        (None, None) => false,
        (None, Some(_)) | (Some(_), None) => true,
        (Some(a), Some(b)) => {
            if !a.local_etag.is_empty() && !b.local_etag.is_empty() {
                return normalize_etag(a.local_etag) != normalize_etag(b.local_etag);
            }

            if !a.etag.is_empty() && !b.etag.is_empty() {
                let ea = normalize_etag(a.etag);
                let eb = normalize_etag(b.etag);
                if ea == eb {
                    return false;
                }
                if !is_owner && is_mixed_multipart_etag_pair(&ea, &eb) && a.size == b.size {
                    // Mirror Go: tolerate mixed multipart-vs-plain ETags for non-owner paths.
                    return false;
                }
                return true;
            }

            if a.size != b.size {
                return true;
            }

            a.last_modified != b.last_modified
        }
    }
}

struct StagedDownload {
    tmp: PathBuf,
    target: PathBuf,
}

pub async fn download_keys(
    api: &ApiClient,
    datasites_root: &Path,
    keys: Vec<String>,
) -> Result<()> {
    if keys.is_empty() {
        return Ok(());
    }
    let staged = stage_downloads(api, datasites_root, &keys).await?;
    commit_staged_downloads(staged).await
}

async fn stage_downloads(
    api: &ApiClient,
    datasites_root: &Path,
    keys: &[String],
) -> Result<Vec<StagedDownload>> {
    if keys.is_empty() {
        return Ok(Vec::new());
    }
    let presigned = api
        .get_blob_presigned(&PresignedParams {
            keys: keys.to_vec(),
        })
        .await?;

    let mut out = Vec::with_capacity(presigned.urls.len());
    for blob in presigned.urls {
        let target = datasites_root.join(&blob.key);
        ensure_parent_dirs(&target)?;
        let tmp = download_to_tmp(api, &blob.url, &target).await?;
        out.push(StagedDownload { tmp, target });
    }
    Ok(out)
}

async fn commit_staged_downloads(staged: Vec<StagedDownload>) -> Result<()> {
    for item in staged {
        if item.target.exists() {
            let meta = fs::metadata(&item.target)?;
            if meta.is_dir() {
                fs::remove_dir_all(&item.target)?;
            } else {
                let _ = fs::remove_file(&item.target);
            }
        }
        tokio::fs::rename(&item.tmp, &item.target)
            .await
            .with_context(|| {
                format!("rename {} -> {}", item.tmp.display(), item.target.display())
            })?;
    }
    Ok(())
}

async fn download_to_tmp(api: &ApiClient, url: &str, target: &Path) -> Result<PathBuf> {
    let resp = api.http().get(url).send().await?;
    let status = resp.status();
    if !status.is_success() {
        let text = resp.text().await.unwrap_or_default();
        anyhow::bail!("download failed: {status} {text}");
    }

    let Some(parent) = target.parent() else {
        anyhow::bail!("target has no parent: {}", target.display());
    };
    let fname = target
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or("download");
    let tmp = parent.join(format!(".{}.tmp-{}", fname, uuid::Uuid::new_v4()));

    if tmp.exists() {
        let _ = fs::remove_file(&tmp);
    }

    let mut f = tokio::fs::File::create(&tmp)
        .await
        .with_context(|| format!("create {}", tmp.display()))?;
    let mut stream = resp.bytes_stream();
    while let Some(chunk) = stream.next().await {
        let bytes = chunk?;
        api.stats().on_recv(bytes.len() as i64);
        f.write_all(&bytes).await?;
    }
    f.flush().await?;
    drop(f);

    Ok(tmp)
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
    key.contains(".conflict")
        || key.contains(".rejected")
        || key.contains("syftrejected")
        || key.contains("syftconflict")
}

fn is_synced_key(key: &str) -> bool {
    // Full datasites sync: keep everything that is under a datasite root directory.
    //
    // In the on-disk datasites layout, the first path segment is the email identity
    // (e.g. `client1@sandbox.local/...`). Restricting to that shape avoids syncing
    // any non-datasites server-side objects that may share the same bucket.
    let key = key.trim_start_matches('/');
    let Some((root, _rest)) = key.split_once('/') else {
        return false;
    };
    root.contains('@')
}

fn should_ignore_key(filters: &SyncFilters, key: &str) -> bool {
    filters.ignore.should_ignore_rel(Path::new(key), false) || SyncFilters::is_marked_rel_path(key)
}

#[derive(Clone, Debug)]
struct LocalScanCacheEntry {
    size: i64,
    mtime_nanos: u128,
    etag: String,
}

#[derive(Default)]
pub(crate) struct LocalScanner {
    last_state: HashMap<String, LocalScanCacheEntry>,
}

impl LocalScanner {
    fn scan(
        &mut self,
        datasites_root: &Path,
        filters: &SyncFilters,
    ) -> Result<HashMap<String, LocalFile>> {
        let mut out = HashMap::new();
        let mut next_state: HashMap<String, LocalScanCacheEntry> = HashMap::new();

        if !datasites_root.exists() {
            self.last_state.clear();
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
            let size = meta.len() as i64;
            let (mtime_nanos, last_modified_secs) = match meta.modified() {
                Ok(st) => {
                    let d = st.duration_since(std::time::UNIX_EPOCH).unwrap_or_default();
                    (d.as_nanos(), d.as_secs() as i64)
                }
                Err(_) => (0, 0),
            };

            let etag = match self.last_state.get(&key) {
                Some(prev) if prev.size == size && prev.mtime_nanos == mtime_nanos => {
                    prev.etag.clone()
                }
                _ => compute_local_etag(path, size)?,
            };

            next_state.insert(
                key.clone(),
                LocalScanCacheEntry {
                    size,
                    mtime_nanos,
                    etag: etag.clone(),
                },
            );

            out.insert(
                key.clone(),
                LocalFile {
                    key,
                    path: path.to_path_buf(),
                    etag,
                    size,
                    last_modified: last_modified_secs,
                },
            );
        }

        self.last_state = next_state;
        Ok(out)
    }
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

const DEFAULT_MULTIPART_PART_SIZE: i64 = 64 * 1024 * 1024; // match uploader
const MIN_MULTIPART_PART_SIZE: i64 = 5 * 1024 * 1024; // S3 minimum
const MAX_MULTIPART_PARTS: i64 = 10000;
const MULTIPART_THRESHOLD: i64 = 32 * 1024 * 1024; // match uploader / Go threshold

pub(crate) fn compute_local_etag(path: &Path, size: i64) -> Result<String> {
    if size > MULTIPART_THRESHOLD {
        let (part_size, part_count) = select_part_size(size, parse_part_size_env());
        return compute_multipart_etag(path, size, part_size, part_count);
    }
    compute_md5_hex_streaming(path)
}

fn compute_md5_hex_streaming(path: &Path) -> Result<String> {
    let mut file = fs::File::open(path)?;
    let mut ctx = md5::Context::new();
    let mut buf = vec![0u8; 1024 * 1024];
    loop {
        let n = file.read(&mut buf)?;
        if n == 0 {
            break;
        }
        ctx.consume(&buf[..n]);
    }
    Ok(format!("{:x}", ctx.compute()))
}

fn compute_multipart_etag(
    path: &Path,
    size: i64,
    part_size: i64,
    part_count: i64,
) -> Result<String> {
    let mut file = fs::File::open(path)?;
    let mut buf = vec![0u8; 1024 * 1024];
    let mut remaining = size;
    let mut part_digests = Vec::with_capacity(part_count.max(0) as usize);

    for _ in 0..part_count {
        let mut ctx = md5::Context::new();
        let mut to_read = remaining.min(part_size);
        while to_read > 0 {
            let cap = std::cmp::min(buf.len() as i64, to_read) as usize;
            let n = file.read(&mut buf[..cap])?;
            if n == 0 {
                break;
            }
            ctx.consume(&buf[..n]);
            to_read -= n as i64;
            remaining -= n as i64;
        }
        part_digests.push(ctx.compute());
    }

    let mut concat = Vec::with_capacity(part_digests.len() * 16);
    for d in &part_digests {
        concat.extend_from_slice(&d.0);
    }
    let final_digest = md5::compute(&concat);
    Ok(format!("{:x}-{part_count}", final_digest))
}

fn parse_part_size_env() -> Option<i64> {
    let v = std::env::var("SBDEV_PART_SIZE").ok()?;
    parse_bytes(&v)
}

fn select_part_size(size: i64, override_part_size: Option<i64>) -> (i64, i64) {
    let mut part_size = override_part_size.unwrap_or(DEFAULT_MULTIPART_PART_SIZE);
    if part_size < MIN_MULTIPART_PART_SIZE {
        part_size = MIN_MULTIPART_PART_SIZE;
    }
    let mut part_count = divide_and_ceil(size, part_size);
    while part_count > MAX_MULTIPART_PARTS {
        part_size *= 2;
        part_count = divide_and_ceil(size, part_size);
    }
    (part_size, part_count)
}

fn divide_and_ceil(n: i64, d: i64) -> i64 {
    if d <= 0 {
        return 0;
    }
    let mut q = n / d;
    if n % d != 0 {
        q += 1;
    }
    q
}

fn parse_bytes(s: &str) -> Option<i64> {
    let raw = s.trim();
    if raw.is_empty() {
        return None;
    }
    let upper = raw.to_uppercase();
    let (num, mult) = if let Some(n) = upper.strip_suffix("GB") {
        (n, 1024_i64 * 1024 * 1024)
    } else if let Some(n) = upper.strip_suffix("MB") {
        (n, 1024_i64 * 1024)
    } else if let Some(n) = upper.strip_suffix("KB") {
        (n, 1024_i64)
    } else if let Some(n) = upper.strip_suffix('B') {
        (n, 1_i64)
    } else {
        (upper.as_str(), 1_i64)
    };
    let n: i64 = num.trim().parse().ok()?;
    if n <= 0 {
        return None;
    }
    Some(n.saturating_mul(mult))
}

fn mark_conflict(path: &Path) -> Result<()> {
    if !path.exists() {
        return Ok(());
    }
    if is_marked_path(path, ".conflict") {
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

fn mark_rejected(path: &Path) -> Result<()> {
    if !path.exists() {
        return Ok(());
    }
    if is_marked_path(path, ".rejected") {
        return Ok(());
    }
    if find_existing_marker(path, ".rejected").is_some() {
        // Mirror Go behavior: avoid unbounded dedupe/rotation loops.
        // If any rejected marker already exists for this base path, keep the existing one and
        // delete the new offending file without rotating.
        let _ = fs::remove_file(path);
        return Ok(());
    }

    let marked = as_marked_path(path, ".rejected");
    fs::rename(path, marked)?;
    Ok(())
}

fn rejected_marker_exists(path: &Path) -> bool {
    find_existing_marker(path, ".rejected").is_some()
}

fn is_marked_path(path: &Path, marker: &str) -> bool {
    path.file_name()
        .and_then(|n| n.to_str())
        .is_some_and(|name| name.contains(marker))
}

fn as_marked_path(path: &Path, marker: &str) -> PathBuf {
    let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("");
    let base = if ext.is_empty() {
        path.to_path_buf()
    } else {
        PathBuf::from(path.to_string_lossy().trim_end_matches(&format!(".{ext}")))
    };
    if ext.is_empty() {
        PathBuf::from(format!("{}{}", base.to_string_lossy(), marker))
    } else {
        PathBuf::from(format!("{}{}.{ext}", base.to_string_lossy(), marker))
    }
}

fn find_existing_marker(path: &Path, marker: &str) -> Option<PathBuf> {
    let dir = path.parent()?;
    let file_name = path.file_name()?.to_str()?;
    let ext = path.extension().and_then(|e| e.to_str()).unwrap_or("");
    let base = if ext.is_empty() {
        file_name.to_string()
    } else {
        file_name
            .strip_suffix(&format!(".{ext}"))
            .unwrap_or(file_name)
            .to_string()
    };

    let unrotated = if ext.is_empty() {
        format!("{base}{marker}")
    } else {
        format!("{base}{marker}.{ext}")
    };
    let unrotated_path = dir.join(&unrotated);
    if unrotated_path.exists() {
        return Some(unrotated_path);
    }

    let prefix = format!("{base}{marker}.");
    let suffix = if ext.is_empty() {
        String::new()
    } else {
        format!(".{ext}")
    };

    let mut matches: Vec<PathBuf> = Vec::new();
    let entries = std::fs::read_dir(dir).ok()?;
    for entry in entries.flatten() {
        let name = entry.file_name();
        let name = name.to_str().unwrap_or("");
        if !name.starts_with(&prefix) || !name.ends_with(&suffix) {
            continue;
        }
        let ts = &name[prefix.len()..name.len().saturating_sub(suffix.len())];
        if ts.len() == 14 && ts.chars().all(|c| c.is_ascii_digit()) {
            matches.push(entry.path());
        }
    }
    matches.sort_by(|a, b| a.to_string_lossy().cmp(&b.to_string_lossy()));
    matches.into_iter().next()
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
        let mut scanner = LocalScanner::default();
        let state = scanner.scan(&root, &filters).unwrap();
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
        let mut scanner = LocalScanner::default();
        let state = scanner.scan(&root, &filters).unwrap();
        let key = "alice@example.com/public/a.txt".to_string();
        assert!(state.contains_key(&key));
        let meta = state.get(&key).unwrap();
        assert_eq!(meta.key, key);
        assert!(!meta.etag.is_empty());

        let computed = compute_md5_hex_streaming(&f1).unwrap();
        assert_eq!(computed, meta.etag);
    }

    #[test]
    fn content_differs_ignores_last_modified_when_etag_matches() {
        let etag = "0123456789abcdef0123456789abcdef";
        assert!(!content_differs_for_key(
            false, etag, 10, etag, 10, 111, 222
        ));
    }

    #[test]
    fn content_differs_tolerates_mixed_multipart_for_mirror_paths() {
        let plain = "0123456789abcdef0123456789abcdef";
        let multipart = "0123456789abcdef0123456789abcdef-2";
        assert!(!content_differs_for_key(
            false, plain, 10, multipart, 10, 0, 0
        ));
    }

    #[test]
    fn content_differs_flags_different_etags() {
        assert!(content_differs_for_key(
            false,
            "0123456789abcdef0123456789abcdef",
            10,
            "fedcba9876543210fedcba9876543210",
            10,
            0,
            0
        ));
    }

    #[test]
    fn mark_conflict_does_not_double_mark() {
        let root = make_temp_dir();
        let dir = root.join("alice@example.com/public");
        fs::create_dir_all(&dir).unwrap();
        let orig = dir.join("file.txt");
        fs::write(&orig, b"v1").unwrap();

        mark_conflict(&orig).unwrap();
        let marked = dir.join("file.conflict.txt");
        assert!(marked.exists());

        // Marking an already-marked file should be a no-op (avoid `.conflict.conflict.*` loops).
        mark_conflict(&marked).unwrap();
        assert!(marked.exists());
        assert!(!dir.join("file.conflict.conflict.txt").exists());
    }

    #[test]
    fn mark_conflict_rotates_existing_marker() {
        let root = make_temp_dir();
        let dir = root.join("alice@example.com/public");
        fs::create_dir_all(&dir).unwrap();
        let orig = dir.join("file.txt");
        fs::write(&orig, b"v1").unwrap();
        mark_conflict(&orig).unwrap();

        // Create another file at the original path and mark again to force rotation.
        fs::write(&orig, b"v2").unwrap();
        mark_conflict(&orig).unwrap();

        let marked = dir.join("file.conflict.txt");
        assert!(marked.exists());

        // Expect a rotated prior marker with a timestamp.
        let mut found_rotated = false;
        for entry in fs::read_dir(&dir).unwrap().flatten() {
            let name = entry.file_name().to_string_lossy().to_string();
            if name.starts_with("file.conflict.") && name.ends_with(".txt") {
                let ts = name
                    .trim_start_matches("file.conflict.")
                    .trim_end_matches(".txt");
                if ts.len() == 14 && ts.chars().all(|c| c.is_ascii_digit()) {
                    found_rotated = true;
                    break;
                }
            }
        }
        assert!(found_rotated);
    }

    #[test]
    fn mark_rejected_dedupes_without_rotation() {
        let root = make_temp_dir();
        let dir = root.join("alice@example.com/public");
        fs::create_dir_all(&dir).unwrap();
        let orig = dir.join("file.txt");
        fs::write(&orig, b"v1").unwrap();

        mark_rejected(&orig).unwrap();
        let marked = dir.join("file.rejected.txt");
        assert!(marked.exists());

        // Create another file at the original path; marking should delete it and keep the marker.
        fs::write(&orig, b"v2").unwrap();
        mark_rejected(&orig).unwrap();
        assert!(!orig.exists());
        assert!(marked.exists());

        // No rotation should have occurred for rejected markers.
        let mut rejected_count = 0;
        for entry in fs::read_dir(&dir).unwrap().flatten() {
            let name = entry.file_name().to_string_lossy().to_string();
            if name.contains(".rejected") {
                rejected_count += 1;
                assert!(!name.starts_with("file.rejected.") || name == "file.rejected.txt");
            }
        }
        assert_eq!(rejected_count, 1);
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
