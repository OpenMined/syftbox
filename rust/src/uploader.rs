use std::collections::{BTreeMap, HashMap};
use std::fs;
use std::io::{Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::time::Duration;

use anyhow::{Context, Result};
use reqwest::header::ETAG;
use serde::{Deserialize, Serialize};

use crate::control::ControlPlane;
use crate::http::{
    AbortMultipartUploadRequest, ApiClient, CompleteMultipartUploadRequest, CompletedPart,
    MultipartUploadRequest,
};

const DEFAULT_MULTIPART_PART_SIZE: i64 = 64 * 1024 * 1024; // 64MB
const MIN_MULTIPART_PART_SIZE: i64 = 5 * 1024 * 1024; // S3 minimum
const MAX_MULTIPART_PARTS: i64 = 10000;
const MULTIPART_THRESHOLD: i64 = 32 * 1024 * 1024; // match Go threshold

#[derive(Debug, Serialize, Deserialize)]
struct UploadSession {
    #[serde(rename = "uploadId")]
    upload_id: String,
    key: String,
    #[serde(rename = "filePath")]
    file_path: String,
    fingerprint: String,
    size: i64,
    #[serde(rename = "partSize")]
    part_size: i64,
    #[serde(rename = "partCount")]
    part_count: i64,
    completed: BTreeMap<i64, String>,
}

pub async fn upload_blob_smart(
    api: &ApiClient,
    cp: Option<&ControlPlane>,
    data_dir: &Path,
    key: &str,
    path: &Path,
) -> Result<()> {
    let meta = fs::metadata(path).with_context(|| format!("stat {}", path.display()))?;
    let size = meta.len() as i64;

    if size <= MULTIPART_THRESHOLD {
        let upload_id = cp.map(|c| {
            c.upsert_upload(
                key.to_string(),
                Some(path.to_string_lossy().to_string()),
                size,
                None,
                None,
            )
        });
        if let Err(err) = api.upload_blob(key, path).await {
            if let (Some(c), Some(id)) = (cp, upload_id.as_deref()) {
                c.set_upload_error(id, err.to_string());
            }
            return Err(err);
        }
        if let (Some(c), Some(id)) = (cp, upload_id.as_deref()) {
            c.set_upload_completed(id, size);
        }
        return Ok(());
    }

    ResumableUploader::new(api, cp, data_dir, key, path, size)?
        .upload()
        .await
}

struct ResumableUploader<'a> {
    api: &'a ApiClient,
    cp: Option<&'a ControlPlane>,
    key: String,
    file_path: PathBuf,
    size: i64,
    fingerprint: String,
    resume_dir: PathBuf,
    session: UploadSession,
    upload_entry_id: Option<String>,
    part_client: reqwest::Client,
    part_upload_timeout: Option<Duration>,
    part_sleep: Option<Duration>,
}

impl<'a> ResumableUploader<'a> {
    fn new(
        api: &'a ApiClient,
        cp: Option<&'a ControlPlane>,
        data_dir: &Path,
        key: &str,
        file_path: &Path,
        size: i64,
    ) -> Result<Self> {
        let resume_dir = data_dir.join(".data").join("upload-sessions");
        fs::create_dir_all(&resume_dir).ok();

        let fingerprint = default_fingerprint(file_path, size);

        let (part_size, part_count) = select_part_size(size, parse_part_size_env());
        let session = UploadSession {
            upload_id: String::new(),
            key: key.to_string(),
            file_path: file_path.to_string_lossy().to_string(),
            fingerprint: fingerprint.clone(),
            size,
            part_size,
            part_count,
            completed: BTreeMap::new(),
        };

        let part_client = reqwest::Client::builder()
            .timeout(Duration::from_secs(30 * 60))
            .connect_timeout(Duration::from_secs(10))
            .build()?;

        Ok(Self {
            api,
            cp,
            key: key.to_string(),
            file_path: file_path.to_path_buf(),
            size,
            fingerprint,
            resume_dir,
            session,
            upload_entry_id: None,
            part_client,
            part_upload_timeout: parse_part_upload_timeout_env(),
            part_sleep: parse_part_sleep_env(),
        })
    }

    async fn upload(mut self) -> Result<()> {
        self.load_or_init_session()?;
        self.ensure_upload_entry();

        // Upload parts until complete.
        loop {
            let remaining = self.remaining_parts();
            if remaining.is_empty() {
                break;
            }
            self.set_state("uploading", None);

            let resp = self
                .api
                .upload_multipart_urls(&MultipartUploadRequest {
                    key: self.key.clone(),
                    size: self.size,
                    part_size: self.session.part_size,
                    upload_id: if self.session.upload_id.is_empty() {
                        None
                    } else {
                        Some(self.session.upload_id.clone())
                    },
                    part_numbers: remaining.clone(),
                })
                .await?;

            if self.session.upload_id.is_empty() {
                self.session.upload_id = resp.upload_id.clone();
                self.session.part_size = resp.part_size;
                self.session.part_count = resp.part_count;
                self.save_session()?;
                self.ensure_upload_entry();
            }

            if self.upload_parts(resp.urls).await? {
                // Restart requested; start a fresh multipart session.
                continue;
            }
        }

        // Complete multipart upload.
        let parts = self
            .session
            .completed
            .iter()
            .map(|(n, etag)| CompletedPart {
                part_number: *n,
                etag: etag.clone(),
            })
            .collect::<Vec<_>>();

        let result = self
            .api
            .upload_multipart_complete(&CompleteMultipartUploadRequest {
                key: self.key.clone(),
                upload_id: self.session.upload_id.clone(),
                parts,
            })
            .await;

        match result {
            Ok(_resp) => {
                self.cleanup_session();
                if let (Some(cp), Some(id)) = (self.cp, self.upload_entry_id.as_deref()) {
                    cp.set_upload_completed(id, self.size);
                }
                Ok(())
            }
            Err(err) => {
                self.set_state("error", Some(err.to_string()));
                Err(err)
            }
        }
    }

    async fn upload_parts(&mut self, urls: HashMap<i64, String>) -> Result<bool> {
        let mut parts = urls.keys().copied().collect::<Vec<_>>();
        parts.sort_unstable();

        let mut file = fs::File::open(&self.file_path)
            .with_context(|| format!("open {}", self.file_path.display()))?;

        for part in parts {
            if self.wait_if_paused_or_restarted().await? {
                return Ok(true);
            }

            let url = urls
                .get(&part)
                .cloned()
                .ok_or_else(|| anyhow::anyhow!("missing url for part {part}"))?;

            let offset = (part - 1) * self.session.part_size;
            let chunk_size = self.part_size_for(part);
            if chunk_size <= 0 {
                continue;
            }
            file.seek(SeekFrom::Start(offset as u64))?;
            let mut buf = vec![0u8; chunk_size as usize];
            file.read_exact(&mut buf)?;

            let mut req = self
                .part_client
                .put(url)
                .header(reqwest::header::CONTENT_TYPE, "application/octet-stream")
                .body(buf);

            if let Some(d) = self.part_upload_timeout {
                req = req.timeout(d);
            }

            let resp = req.send().await;
            let resp = match resp {
                Ok(r) => r,
                Err(err) => {
                    self.api.stats().set_last_error(err.to_string());
                    self.set_state("error", Some(err.to_string()));
                    return Err(err.into());
                }
            };

            let status = resp.status();
            if !status.is_success() {
                let text = resp.text().await.unwrap_or_default();
                let err = anyhow::anyhow!("upload part {part} failed: {status} {text}");
                self.api.stats().set_last_error(err.to_string());
                self.set_state("error", Some(err.to_string()));
                return Err(err);
            }

            let etag = resp
                .headers()
                .get(ETAG)
                .and_then(|v| v.to_str().ok())
                .map(|s| s.trim_matches('"').to_string())
                .filter(|s| !s.is_empty())
                .unwrap_or_else(|| format!("{part}-{chunk_size}"));

            self.session.completed.insert(part, etag);
            self.save_session()?;

            self.api.stats().on_send(chunk_size);
            let uploaded = self.completed_bytes();
            self.update_progress(uploaded);

            if let Some(sleep) = self.part_sleep {
                tokio::time::sleep(sleep).await;
            }
        }

        Ok(false)
    }

    fn ensure_upload_entry(&mut self) {
        let Some(cp) = self.cp else {
            return;
        };
        let id = cp.upsert_upload(
            self.key.clone(),
            Some(self.file_path.to_string_lossy().to_string()),
            self.size,
            Some(self.session.part_size),
            Some(self.session.part_count),
        );
        self.upload_entry_id = Some(id);
        self.update_progress(self.completed_bytes());
    }

    fn set_state(&self, state: &str, error: Option<String>) {
        if let (Some(cp), Some(id)) = (self.cp, self.upload_entry_id.as_deref()) {
            cp.set_upload_state(id, state.to_string(), error);
        }
    }

    fn update_progress(&self, uploaded: i64) {
        if let (Some(cp), Some(id)) = (self.cp, self.upload_entry_id.as_deref()) {
            let completed_parts = self.session.completed.keys().copied().collect::<Vec<_>>();
            cp.update_upload_progress(id, uploaded, completed_parts);
        }
    }

    async fn wait_if_paused_or_restarted(&mut self) -> Result<bool> {
        let Some(cp) = self.cp else {
            return Ok(false);
        };
        let Some(id) = self.upload_entry_id.as_deref() else {
            return Ok(false);
        };

        loop {
            let state = cp.get_upload_state(id);
            match state.as_str() {
                "paused" => {
                    tokio::time::sleep(Duration::from_millis(100)).await;
                    continue;
                }
                "restarted" => {
                    self.restart_session()?;
                    return Ok(true);
                }
                _ => return Ok(false),
            }
        }
    }

    fn restart_session(&mut self) -> Result<()> {
        // Mirror Go's restart semantics: clear local session state so a fresh
        // multipart upload is started from scratch.
        self.cleanup_session();
        self.session.upload_id.clear();
        self.session.completed.clear();
        self.save_session()?;

        if let (Some(cp), Some(id)) = (self.cp, self.upload_entry_id.as_deref()) {
            cp.set_upload_state(id, "uploading".to_string(), None);
            cp.update_upload_progress(id, 0, Vec::new());
        }

        Ok(())
    }

    fn remaining_parts(&self) -> Vec<i64> {
        let mut out = Vec::new();
        for p in 1..=self.session.part_count {
            if !self.session.completed.contains_key(&p) {
                out.push(p);
            }
        }
        out
    }

    fn completed_bytes(&self) -> i64 {
        let mut total = 0;
        for p in self.session.completed.keys().copied() {
            total += self.part_size_for(p);
        }
        total
    }

    fn part_size_for(&self, part: i64) -> i64 {
        let offset = (part - 1) * self.session.part_size;
        if offset >= self.size {
            return 0;
        }
        let remaining = self.size - offset;
        if remaining < self.session.part_size {
            remaining
        } else {
            self.session.part_size
        }
    }

    fn session_path(&self) -> PathBuf {
        use sha1::{Digest, Sha1};
        let mut hasher = Sha1::new();
        hasher.update(format!("{}|{}", self.key, self.file_path.display()));
        let digest = format!("{:x}", hasher.finalize());
        self.resume_dir.join(format!("{digest}.json"))
    }

    fn load_or_init_session(&mut self) -> Result<()> {
        let p = self.session_path();
        let Ok(data) = fs::read(&p) else {
            self.save_session()?;
            return Ok(());
        };

        let s: UploadSession = serde_json::from_slice(&data).context("decode upload session")?;
        if s.key != self.key
            || s.file_path != self.file_path.to_string_lossy().as_ref()
            || s.fingerprint != self.fingerprint
            || s.size != self.size
        {
            let _ = fs::remove_file(&p);
            self.save_session()?;
            return Ok(());
        }

        self.session = s;
        Ok(())
    }

    fn save_session(&self) -> Result<()> {
        let p = self.session_path();
        let data = serde_json::to_vec(&self.session).context("encode upload session")?;
        fs::write(&p, data).with_context(|| format!("write {}", p.display()))?;
        Ok(())
    }

    fn cleanup_session(&self) {
        let _ = fs::remove_file(self.session_path());
    }

    #[allow(dead_code)]
    async fn abort(&self) -> Result<()> {
        if self.session.upload_id.is_empty() {
            return Ok(());
        }
        self.api
            .upload_multipart_abort(&AbortMultipartUploadRequest {
                key: self.key.clone(),
                upload_id: self.session.upload_id.clone(),
            })
            .await
    }
}

fn default_fingerprint(path: &Path, size: i64) -> String {
    let mtime = fs::metadata(path)
        .and_then(|m| m.modified())
        .ok()
        .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
        .map(|d| d.as_nanos() as i64)
        .unwrap_or(0);
    format!("{size}:{mtime}")
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

fn parse_part_size_env() -> Option<i64> {
    let v = std::env::var("SBDEV_PART_SIZE").ok()?;
    parse_bytes(&v)
}

fn parse_part_sleep_env() -> Option<Duration> {
    let v = std::env::var("SYFTBOX_UPLOAD_PART_SLEEP_MS").ok()?;
    let ms: u64 = v.trim().parse().ok()?;
    if ms == 0 {
        None
    } else {
        Some(Duration::from_millis(ms))
    }
}

fn parse_part_upload_timeout_env() -> Option<Duration> {
    if let Ok(v) = std::env::var("SBDEV_PART_UPLOAD_TIMEOUT") {
        let s = v.trim();
        if s.is_empty() {
            return None;
        }
        if let Some(d) = parse_duration(s) {
            return Some(d);
        }
    }
    if let Ok(v) = std::env::var("SYFTBOX_PART_UPLOAD_TIMEOUT_MS") {
        let ms: u64 = v.trim().parse().ok()?;
        if ms > 0 {
            return Some(Duration::from_millis(ms));
        }
    }
    None
}

fn parse_duration(s: &str) -> Option<Duration> {
    // Minimal duration parser: <int><unit> where unit is ms|s|m|h.
    let s = s.trim().to_lowercase();
    let (num, unit) = s
        .chars()
        .position(|c| !c.is_ascii_digit())
        .map(|i| s.split_at(i))?;
    let n: u64 = num.parse().ok()?;
    match unit {
        "ms" => Some(Duration::from_millis(n)),
        "s" => Some(Duration::from_secs(n)),
        "m" => Some(Duration::from_secs(n * 60)),
        "h" => Some(Duration::from_secs(n * 3600)),
        _ => None,
    }
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
