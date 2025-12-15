use std::{
    collections::HashSet,
    fs,
    path::{Path, PathBuf},
    sync::atomic::{AtomicBool, Ordering},
    time::Duration,
};

use anyhow::{Context, Result};
use base64::Engine;
use futures_util::{SinkExt, StreamExt};
use md5::compute as md5_compute;
use notify::{EventKind, RecommendedWatcher, RecursiveMode, Watcher};
use serde::Deserialize;
use serde_json::Value;
use tokio::{sync::broadcast::Receiver, time::sleep};
use tokio_tungstenite::connect_async;
use tokio_tungstenite::tungstenite::protocol::Message;
use url::Url;
use uuid::Uuid;
use walkdir::WalkDir;

use crate::config::Config;
use crate::control::ControlPlane;
use crate::filters::SyncFilters;
use crate::http::ApiClient;
use crate::sync::{
    download_keys, ensure_parent_dirs, sync_once_with_control, write_file_resolving_conflicts,
};

static ACL_READY: AtomicBool = AtomicBool::new(false);

pub struct Client {
    cfg: Config,
    api: ApiClient,
    filters: std::sync::Arc<SyncFilters>,
    control: Option<ControlPlane>,
    #[allow(dead_code)]
    events_rx: Option<Receiver<Message>>,
}

impl Client {
    pub fn new(
        cfg: Config,
        api: ApiClient,
        filters: std::sync::Arc<SyncFilters>,
        events_rx: Option<Receiver<Message>>,
        control: Option<ControlPlane>,
    ) -> Self {
        Self {
            cfg,
            api,
            filters,
            control,
            events_rx,
        }
    }

    pub async fn start(&mut self) -> Result<()> {
        self.wait_for_healthz().await?;

        let api = self.api.clone();
        let data_dir = self.cfg.data_dir.clone();
        let email = self.cfg.email.clone();
        let server_url = self.cfg.server_url.clone();
        let control = self.control.clone();
        let filters = self.filters.clone();
        let sync_kick = std::sync::Arc::new(tokio::sync::Notify::new());

        ensure_default_acls(&data_dir, &email)?;
        // Ensure any existing ACLs are present on the server before normal sync begins.
        upload_existing_acls(&api, &data_dir.join("datasites"), &email).await?;
        ACL_READY.store(true, Ordering::SeqCst);

        tokio::select! {
            res = run_ws_listener(api.clone(), server_url, data_dir.clone(), email.clone(), filters.clone(), sync_kick.clone()) => {
                if let Err(err) = res {
                    eprintln!("ws listener crashed: {err:?}");
                }
            }
            res = run_sync_loop(api, data_dir, email, control, filters, sync_kick) => {
                if let Err(err) = res {
                    eprintln!("sync loop crashed: {err:?}");
                }
            }
        }

        Ok(())
    }
}

impl Client {
    async fn wait_for_healthz(&self) -> Result<()> {
        let mut attempts = 0;
        loop {
            match self.api.healthz().await {
                Ok(_) => return Ok(()),
                Err(err) => {
                    attempts += 1;
                    if attempts >= 60 {
                        return Err(err).context("healthz");
                    }
                    sleep(Duration::from_millis(500)).await;
                }
            }
        }
    }
}

async fn run_sync_loop(
    api: ApiClient,
    data_dir: PathBuf,
    _email: String,
    control: Option<ControlPlane>,
    filters: std::sync::Arc<SyncFilters>,
    sync_kick: std::sync::Arc<tokio::sync::Notify>,
) -> Result<()> {
    let mut journal = crate::sync::SyncJournal::load(&data_dir)?;
    loop {
        if let Err(err) =
            sync_once_with_control(&api, &data_dir, control.clone(), &filters, &mut journal).await
        {
            eprintln!("sync error: {err:?}");
        }

        if let Some(cp) = &control {
            tokio::select! {
                _ = sleep(Duration::from_secs(5)) => {}
                _ = cp.wait_sync_now() => {}
                _ = sync_kick.notified() => {}
            }
        } else {
            tokio::select! {
                _ = sleep(Duration::from_secs(5)) => {}
                _ = sync_kick.notified() => {}
            }
        }
    }
}

#[derive(Deserialize)]
struct WsMessage {
    #[serde(rename = "typ")]
    typ: u16,
    #[serde(rename = "dat")]
    dat: Value,
}

#[derive(Deserialize)]
struct FileWrite {
    #[serde(rename = "pth")]
    path: String,
    #[serde(rename = "con", default, deserialize_with = "deserialize_base64_opt")]
    content: Option<Vec<u8>>,
}

#[derive(Deserialize)]
struct HttpMsg {
    #[serde(rename = "id")]
    id: String,
    #[serde(rename = "syft_url")]
    syft_url: String,
    #[serde(rename = "body", default, deserialize_with = "deserialize_base64_opt")]
    body: Option<Vec<u8>>,
}

async fn run_ws_listener(
    api: ApiClient,
    server_url: String,
    data_dir: PathBuf,
    email: String,
    filters: std::sync::Arc<SyncFilters>,
    sync_kick: std::sync::Arc<tokio::sync::Notify>,
) -> Result<()> {
    let datasites_root = data_dir.join("datasites");
    if !datasites_root.exists() {
        fs::create_dir_all(&datasites_root)?;
    }
    let ws_url = Url::parse(
        &server_url
            .replace("http://", "ws://")
            .replace("https://", "wss://"),
    )?;
    let mut ws_url = ws_url;
    ws_url.set_path("/api/v1/events");
    ws_url.query_pairs_mut().append_pair("user", &email);

    let (ws_stream, _) = connect_async(ws_url.as_str()).await?;
    let (mut write, mut read) = ws_stream.split();

    let (tx, mut rx) = tokio::sync::mpsc::channel::<String>(256);
    let watch_root = datasites_root.clone();
    let watcher_api = api.clone();
    let watcher_filters = filters.clone();
    let watcher_kick = sync_kick.clone();
    tokio::spawn(async move {
        if let Err(err) =
            watch_priority_files(&watch_root, watcher_api, watcher_filters, watcher_kick, tx).await
        {
            eprintln!("watcher error: {err:?}");
        }
    });

    // writer
    let write_task = tokio::spawn(async move {
        while let Some(msg) = rx.recv().await {
            if let Err(err) = write.send(Message::Text(msg)).await {
                eprintln!("ws send error: {err}");
                break;
            }
        }
    });

    // reader
    while let Some(msg) = read.next().await {
        match msg {
            Ok(Message::Text(txt)) => {
                handle_ws_message(&api, &datasites_root, &txt).await;
            }
            Ok(Message::Binary(bin)) => {
                if let Ok(txt) = String::from_utf8(bin) {
                    handle_ws_message(&api, &datasites_root, &txt).await;
                }
            }
            _ => {}
        }
    }

    write_task.abort();

    Ok(())
}

async fn handle_ws_message(api: &ApiClient, datasites_root: &Path, raw: &str) {
    let parsed = match serde_json::from_str::<WsMessage>(raw) {
        Ok(msg) => msg,
        Err(_) => return,
    };

    match parsed.typ {
        // MsgFileWrite
        2 | 7 => {
            if let Ok(file) = serde_json::from_value::<FileWrite>(parsed.dat.clone()) {
                if let Some(content) = file.content {
                    if let Err(err) = write_bytes(datasites_root, &file.path, &content) {
                        eprintln!("ws write error: {err:?}");
                    }
                } else {
                    let _ = download_keys(api, datasites_root, vec![file.path]).await;
                }
            }
        }
        // MsgHttp
        6 => {
            if let Ok(http_msg) = serde_json::from_value::<HttpMsg>(parsed.dat.clone()) {
                if let Some(rel) = syft_url_to_rel_path(&http_msg.syft_url) {
                    let file_key = format!("{rel}/{}.request", http_msg.id);
                    if let Some(body) = http_msg.body {
                        if let Err(err) = write_bytes(datasites_root, &file_key, &body) {
                            eprintln!("ws http write error: {err:?}");
                        }
                    } else {
                        let _ = download_keys(api, datasites_root, vec![file_key]).await;
                    }
                }
            }
        }
        _ => {}
    }
}

fn syft_url_to_rel_path(raw: &str) -> Option<String> {
    let url = Url::parse(raw).ok()?;
    let host = url.host_str()?;
    let user = url.username();
    if user.is_empty() {
        return None;
    }
    let datasite = format!("{user}@{host}");
    let mut parts: Vec<String> = url
        .path()
        .trim_start_matches('/')
        .split('/')
        .filter(|p| !p.is_empty())
        .map(|s| s.to_string())
        .collect();
    if parts.len() < 3 {
        return None;
    }
    Some(
        std::iter::once(datasite)
            .chain(parts.drain(..))
            .collect::<Vec<_>>()
            .join("/"),
    )
}

fn write_bytes(datasites_root: &Path, rel_path: &str, bytes: &[u8]) -> Result<()> {
    let target = datasites_root.join(rel_path);
    ensure_parent_dirs(&target)?;
    write_file_resolving_conflicts(&target, bytes)
}

async fn upload_existing_acls(
    api: &ApiClient,
    datasites_root: &Path,
    my_email: &str,
) -> Result<()> {
    if !datasites_root.exists() {
        return Ok(());
    }
    for entry in WalkDir::new(datasites_root)
        .into_iter()
        .filter_map(|e| e.ok())
        .filter(|e| e.file_type().is_file())
    {
        let path = entry.path();
        if let Some(name) = path.file_name().and_then(|n| n.to_str()) {
            if name == "syft.pub.yaml" {
                let rel = path.strip_prefix(datasites_root)?;
                let key = rel.to_string_lossy().to_string();
                if key.starts_with(my_email) {
                    // Best-effort upload; ignore errors so startup continues.
                    let _ = api.upload_blob(&key, path).await;
                }
            }
        }
    }
    Ok(())
}

fn ensure_default_acls(data_dir: &Path, email: &str) -> Result<()> {
    let root_dir = data_dir.join("datasites").join(email);
    let public_dir = root_dir.join("public");
    fs::create_dir_all(&public_dir)?;

    let root_acl = root_dir.join("syft.pub.yaml");
    if !root_acl.exists() {
        let content = "terminal: false\nrules:\n  - pattern: '**'\n    access:\n      admin: []\n      write: []\n      read: []\n";
        fs::write(&root_acl, content)?;
    }

    let public_acl = public_dir.join("syft.pub.yaml");
    if !public_acl.exists() {
        let content = "terminal: false\nrules:\n  - pattern: '**'\n    access:\n      admin: []\n      write: []\n      read: ['*']\n";
        fs::write(&public_acl, content)?;
    }

    Ok(())
}

async fn watch_priority_files(
    root: &Path,
    api: ApiClient,
    filters: std::sync::Arc<SyncFilters>,
    sync_kick: std::sync::Arc<tokio::sync::Notify>,
    tx: tokio::sync::mpsc::Sender<String>,
) -> Result<()> {
    let (event_tx, mut event_rx) = tokio::sync::mpsc::channel(64);
    let mut watcher = RecommendedWatcher::new(
        move |res| {
            let _ = event_tx.blocking_send(res);
        },
        notify::Config::default(),
    )?;
    watcher.watch(root, RecursiveMode::Recursive)?;

    let debounce = Duration::from_millis(50);
    let mut pending: HashSet<PathBuf> = HashSet::new();

    while let Some(res) = event_rx.recv().await {
        ingest_event_paths(&mut pending, res);

        // Debounce/burst buffering: coalesce events for a short window.
        let timer = tokio::time::sleep(debounce);
        tokio::pin!(timer);
        loop {
            tokio::select! {
                _ = &mut timer => break,
                next = event_rx.recv() => {
                    match next {
                        None => break,
                        Some(res) => ingest_event_paths(&mut pending, res),
                    }
                }
            }
        }

        let paths: Vec<PathBuf> = pending.drain().collect();
        for path in paths {
            match send_priority_if_small(root, &api, &filters, &path, &tx).await {
                Ok(did_trigger) => {
                    if did_trigger {
                        sync_kick.notify_one();
                    }
                }
                Err(err) => eprintln!("priority send error: {err:?}"),
            }
        }
    }

    Ok(())
}

fn ingest_event_paths(pending: &mut HashSet<PathBuf>, res: notify::Result<notify::Event>) {
    let event = match res {
        Ok(ev) => ev,
        Err(err) => {
            eprintln!("notify error: {err:?}");
            return;
        }
    };

    match event.kind {
        EventKind::Modify(_) | EventKind::Create(_) => {
            for path in event.paths {
                pending.insert(path);
            }
        }
        _ => {}
    }
}

async fn send_priority_if_small(
    root: &Path,
    api: &ApiClient,
    filters: &SyncFilters,
    path: &Path,
    tx: &tokio::sync::mpsc::Sender<String>,
) -> Result<bool> {
    let meta = tokio::fs::metadata(path).await?;
    if !meta.is_file() {
        return Ok(false);
    }

    let rel = path
        .strip_prefix(root)
        .with_context(|| format!("strip prefix {}", path.display()))?;
    let rel_str = rel.to_string_lossy().to_string();

    if filters.ignore.should_ignore_rel(rel, false) {
        return Ok(false);
    }
    if SyncFilters::is_marked_rel_path(&rel_str) {
        return Ok(false);
    }

    let is_priority = filters.priority.should_prioritize_rel(rel, false);
    if !is_priority {
        return Ok(false);
    }

    let size = meta.len();
    if size > 4 * 1024 * 1024 {
        // Still kick the sync loop so large priority files can be handled via normal sync.
        return Ok(true);
    }

    if rel_str.ends_with("syft.pub.yaml") {
        // Ensure ACLs land on the server immediately for permission checks.
        api.upload_blob(&rel_str, path).await?;
        ACL_READY.store(true, Ordering::SeqCst);
    } else if !ACL_READY.load(Ordering::SeqCst) {
        // Skip priority send until ACLs are established to avoid permission rejections.
        return Ok(false);
    }

    let bytes = tokio::fs::read(path).await?;
    let etag = format!("{:x}", md5_compute(&bytes));
    let payload = serde_json::json!({
        "id": Uuid::new_v4().to_string(),
        "typ": 2,
        "dat": {
            "pth": rel_str,
            "etg": etag,
            "len": size as i64,
            "con": base64::engine::general_purpose::STANDARD.encode(bytes),
        }
    });
    let text = serde_json::to_string(&payload)?;
    tx.send(text).await.ok();
    Ok(true)
}

fn deserialize_base64_opt<'de, D>(deserializer: D) -> std::result::Result<Option<Vec<u8>>, D::Error>
where
    D: serde::Deserializer<'de>,
{
    let opt = Option::<serde_json::Value>::deserialize(deserializer)?;
    match opt {
        None => Ok(None),
        Some(serde_json::Value::String(s)) => {
            let bytes = base64::engine::general_purpose::STANDARD
                .decode(s.as_bytes())
                .map_err(serde::de::Error::custom)?;
            Ok(Some(bytes))
        }
        Some(serde_json::Value::Array(arr)) => {
            let mut out = Vec::with_capacity(arr.len());
            for v in arr {
                let n = v
                    .as_u64()
                    .ok_or_else(|| serde::de::Error::custom("expected byte"))?;
                out.push(n as u8);
            }
            Ok(Some(out))
        }
        _ => Err(serde::de::Error::custom(
            "expected base64 string or array for bytes",
        )),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::env;

    #[test]
    fn syft_url_to_rel_path_parses() {
        let url = "syft://alice@example.com/app_data/perftest/rpc/latency/test-1KB.request";
        let rel = syft_url_to_rel_path(url).expect("parsed");
        assert_eq!(
            rel,
            "alice@example.com/app_data/perftest/rpc/latency/test-1KB.request"
        );
    }

    #[test]
    fn ensure_default_acls_written() {
        let tmp = env::temp_dir().join("syftbox-rs-acl-test");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        ensure_default_acls(&tmp, "alice@example.com").unwrap();
        let root_acl = tmp
            .join("datasites")
            .join("alice@example.com")
            .join("syft.pub.yaml");
        let public_acl = tmp
            .join("datasites")
            .join("alice@example.com")
            .join("public")
            .join("syft.pub.yaml");
        assert!(root_acl.exists());
        assert!(public_acl.exists());
    }
}
