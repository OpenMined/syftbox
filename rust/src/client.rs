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
use tokio::{
    sync::{broadcast::Receiver, mpsc, oneshot},
    time::{sleep, timeout},
};
use tokio_tungstenite::{
    connect_async_with_config,
    tungstenite::{
        client::IntoClientRequest,
        http::header::AUTHORIZATION,
        http::HeaderValue,
        http::StatusCode as HttpStatusCode,
        protocol::{Message, WebSocketConfig},
    },
};
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
use crate::wsproto::{
    Decoded, Encoding, FileWrite, HttpMsg, MsgpackFileWrite, WS_MAX_MESSAGE_BYTES,
};

static ACL_READY: AtomicBool = AtomicBool::new(false);

type PendingAcks = std::sync::Arc<
    tokio::sync::Mutex<std::collections::HashMap<String, oneshot::Sender<Result<()>>>>,
>;

pub struct Client {
    cfg: Config,
    api: ApiClient,
    filters: std::sync::Arc<SyncFilters>,
    control: Option<ControlPlane>,
    #[allow(dead_code)]
    events_rx: Option<Receiver<Message>>,
}

#[derive(Debug, Clone, Copy)]
pub struct ClientStartOptions {
    /// How many health checks to attempt before returning an error.
    /// If `None`, retry indefinitely until shutdown.
    pub healthz_max_attempts: Option<usize>,
}

impl Default for ClientStartOptions {
    fn default() -> Self {
        Self {
            healthz_max_attempts: Some(60),
        }
    }
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
        self.start_with_options(ClientStartOptions::default()).await
    }

    pub async fn start_with_options(&mut self, opts: ClientStartOptions) -> Result<()> {
        let shutdown = std::sync::Arc::new(tokio::sync::Notify::new());
        let shutdown_task = shutdown.clone();
        tokio::spawn(async move {
            let _ = tokio::signal::ctrl_c().await;
            shutdown_task.notify_waiters();
        });

        let healthy = self
            .wait_for_healthz(shutdown.clone(), opts.healthz_max_attempts)
            .await?;
        if !healthy {
            return Ok(());
        }

        let api = self.api.clone();
        let data_dir = self.cfg.data_dir.clone();
        let email = self.cfg.email.clone();
        let server_url = self.cfg.server_url.clone();
        let control = self.control.clone();
        let filters = self.filters.clone();
        let sync_kick = std::sync::Arc::new(tokio::sync::Notify::new());

        crate::workspace::ensure_workspace_layout(&data_dir, &email)?;
        let _workspace_lock = crate::workspace::WorkspaceLock::try_lock(&data_dir)?;
        // Ensure any existing ACLs are present on the server before normal sync begins.
        upload_existing_acls(&api, &data_dir.join("datasites"), &email).await?;
        ACL_READY.store(true, Ordering::SeqCst);

        // Seed control-plane sync status so clients can query /v1/sync/status immediately after
        // startup (Go parity). This is best-effort: failures should not prevent the daemon from
        // starting.
        if let Some(cp) = self.control.as_ref() {
            let keys = collect_synced_keys(&data_dir.join("datasites"), &filters);
            cp.seed_completed(keys);
        }

        tokio::select! {
            _ = shutdown.notified() => {
                crate::logging::info("shutdown requested");
            }
            res = run_ws_listener(api.clone(), server_url, data_dir.clone(), email.clone(), filters.clone(), sync_kick.clone(), shutdown.clone()) => {
                if let Err(err) = res {
                    crate::logging::error(format!("ws listener crashed: {err:?}"));
                }
            }
            res = run_sync_loop(api, data_dir, email, control, filters, sync_kick) => {
                if let Err(err) = res {
                    crate::logging::error(format!("sync loop crashed: {err:?}"));
                }
            }
        }

        Ok(())
    }

    pub async fn start_with_shutdown(
        &mut self,
        shutdown: std::sync::Arc<tokio::sync::Notify>,
        opts: ClientStartOptions,
    ) -> Result<()> {
        let healthy = self
            .wait_for_healthz(shutdown.clone(), opts.healthz_max_attempts)
            .await?;
        if !healthy {
            return Ok(());
        }

        let api = self.api.clone();
        let data_dir = self.cfg.data_dir.clone();
        let email = self.cfg.email.clone();
        let server_url = self.cfg.server_url.clone();
        let control = self.control.clone();
        let filters = self.filters.clone();
        let sync_kick = std::sync::Arc::new(tokio::sync::Notify::new());

        crate::workspace::ensure_workspace_layout(&data_dir, &email)?;
        let _workspace_lock = crate::workspace::WorkspaceLock::try_lock(&data_dir)?;
        upload_existing_acls(&api, &data_dir.join("datasites"), &email).await?;
        ACL_READY.store(true, Ordering::SeqCst);

        if let Some(cp) = self.control.as_ref() {
            let keys = collect_synced_keys(&data_dir.join("datasites"), &filters);
            cp.seed_completed(keys);
        }

        tokio::select! {
            _ = shutdown.notified() => {
                crate::logging::info("shutdown requested");
            }
            res = run_ws_listener(api.clone(), server_url, data_dir.clone(), email.clone(), filters.clone(), sync_kick.clone(), shutdown.clone()) => {
                if let Err(err) = res {
                    crate::logging::error(format!("ws listener crashed: {err:?}"));
                }
            }
            res = run_sync_loop(api, data_dir, email, control, filters, sync_kick) => {
                if let Err(err) = res {
                    crate::logging::error(format!("sync loop crashed: {err:?}"));
                }
            }
        }

        Ok(())
    }
}

impl Client {
    async fn wait_for_healthz(
        &self,
        shutdown: std::sync::Arc<tokio::sync::Notify>,
        max_attempts: Option<usize>,
    ) -> Result<bool> {
        let mut attempts = 0;
        loop {
            let res = tokio::select! {
                _ = shutdown.notified() => return Ok(false),
                res = self.api.healthz() => res,
            };
            match res {
                Ok(_) => return Ok(true),
                Err(err) => {
                    attempts += 1;
                    if let Some(max) = max_attempts {
                        if attempts >= max {
                            return Err(err).context("healthz");
                        }
                    }
                    tokio::select! {
                        _ = shutdown.notified() => return Ok(false),
                        _ = sleep(Duration::from_millis(500)) => {}
                    }
                }
            }
        }
    }
}

async fn run_sync_loop(
    api: ApiClient,
    data_dir: PathBuf,
    email: String,
    control: Option<ControlPlane>,
    filters: std::sync::Arc<SyncFilters>,
    sync_kick: std::sync::Arc<tokio::sync::Notify>,
) -> Result<()> {
    let mut journal = crate::sync::SyncJournal::load(&data_dir)?;
    let mut local_scanner = crate::sync::LocalScanner::default();
    loop {
        if let Err(err) = sync_once_with_control(
            &api,
            &data_dir,
            &email,
            control.clone(),
            &filters,
            &mut local_scanner,
            &mut journal,
        )
        .await
        {
            crate::logging::error(format!("sync error: {err:?}"));
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

#[derive(Clone)]
struct WsHandle {
    encoding: Encoding,
    tx: mpsc::Sender<WsOutbound>,
    pending: PendingAcks,
}

struct WsOutbound {
    message: Message,
    ack_key: Option<String>,
}

impl WsHandle {
    async fn send_filewrite_with_ack(
        &self,
        rel_path: String,
        etag: String,
        size: i64,
        content: Vec<u8>,
        ack_timeout: Duration,
    ) -> Result<()> {
        let id = Uuid::new_v4().to_string();
        let msg = match self.encoding {
            Encoding::Json => {
                let payload = serde_json::json!({
                    "id": id.clone(),
                    "typ": 2,
                    "dat": {
                        "pth": rel_path,
                        "etg": etag,
                        "len": size,
                        "con": base64::engine::general_purpose::STANDARD.encode(content),
                    }
                });
                Message::Text(serde_json::to_string(&payload)?)
            }
            Encoding::MsgPack => {
                let fw = MsgpackFileWrite {
                    path: rel_path,
                    etag,
                    length: size,
                    content: Some(content.into()),
                };
                let bin = crate::wsproto::encode_msgpack(&id, 2, &fw)?;
                Message::Binary(bin)
            }
        };

        let (ack_tx, ack_rx) = oneshot::channel::<Result<()>>();
        self.pending.lock().await.insert(id.clone(), ack_tx);
        if let Err(err) = self
            .tx
            .send(WsOutbound {
                message: msg,
                ack_key: Some(id.clone()),
            })
            .await
        {
            let _ = self.pending.lock().await.remove(&id);
            return Err(anyhow::anyhow!(err)).context("ws send queue closed");
        }

        match timeout(ack_timeout, ack_rx).await {
            Err(_) => {
                let _ = self.pending.lock().await.remove(&id);
                anyhow::bail!("ACK timeout after {ack_timeout:?}")
            }
            Ok(Err(_)) => {
                let _ = self.pending.lock().await.remove(&id);
                anyhow::bail!("ACK channel closed")
            }
            Ok(Ok(res)) => res,
        }
    }
}

async fn run_ws_listener(
    api: ApiClient,
    server_url: String,
    data_dir: PathBuf,
    email: String,
    filters: std::sync::Arc<SyncFilters>,
    sync_kick: std::sync::Arc<tokio::sync::Notify>,
    shutdown: std::sync::Arc<tokio::sync::Notify>,
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

    let supported_encodings = format!("{},{}", Encoding::MsgPack.as_str(), Encoding::Json.as_str());
    let config = WebSocketConfig {
        max_message_size: Some(WS_MAX_MESSAGE_BYTES),
        max_frame_size: Some(WS_MAX_MESSAGE_BYTES),
        ..Default::default()
    };

    let mut backoff = Duration::from_millis(250);
    loop {
        let _ = api.ensure_access_token().await;
        let token = api.current_access_token().await;

        let mut req = ws_url.as_str().into_client_request()?;
        req.headers_mut().insert(
            "X-Syft-WS-Encodings",
            HeaderValue::from_str(&supported_encodings)?,
        );
        if let Some(token) = token {
            let value = format!("Bearer {token}");
            req.headers_mut()
                .insert(AUTHORIZATION, HeaderValue::from_str(&value)?);
        }

        let connect = tokio::select! {
            _ = shutdown.notified() => return Ok(()),
            res = connect_async_with_config(req, Some(config), false) => res,
        };
        let (ws_stream, resp) = match connect {
            Ok(ok) => ok,
            Err(err) => {
                crate::logging::error(format!("ws connect error: {err:?}"));

                let auth_failed = matches!(
                    err,
                    tokio_tungstenite::tungstenite::Error::Http(ref resp)
                        if resp.status() == HttpStatusCode::UNAUTHORIZED
                            || resp.status() == HttpStatusCode::FORBIDDEN
                );
                if auth_failed && api.has_refresh_token().await {
                    api.clear_access_token().await;
                    let _ = api.ensure_access_token().await;
                }

                tokio::select! {
                    _ = shutdown.notified() => return Ok(()),
                    _ = tokio::time::sleep(backoff) => {}
                }
                backoff = std::cmp::min(backoff * 2, Duration::from_secs(5));
                continue;
            }
        };
        backoff = Duration::from_millis(250);

        let enc_header = resp
            .headers()
            .get("X-Syft-WS-Encoding")
            .and_then(|v| v.to_str().ok())
            .unwrap_or("");
        let encoding = crate::wsproto::preferred_encoding(enc_header);
        let (mut write, mut read) = ws_stream.split();

        let pending: PendingAcks =
            std::sync::Arc::new(tokio::sync::Mutex::new(std::collections::HashMap::<
                String,
                oneshot::Sender<Result<()>>,
            >::new()));

        let (tx, mut rx) = mpsc::channel::<WsOutbound>(256);
        let ws_handle = WsHandle {
            encoding,
            tx,
            pending: pending.clone(),
        };

        let watch_root = datasites_root.clone();
        let watcher_filters = filters.clone();
        let watcher_kick = sync_kick.clone();
        let watcher_ws = ws_handle.clone();
        let watcher_email = email.clone();
        let watcher_shutdown = shutdown.clone();
        let watcher_task = tokio::spawn(async move {
            if let Err(err) = watch_priority_files(
                &watch_root,
                &watcher_email,
                watcher_filters,
                watcher_kick,
                watcher_ws,
                watcher_shutdown,
            )
            .await
            {
                crate::logging::error(format!("watcher error: {err:?}"));
            }
        });

        let pending_writer = pending.clone();
        let write_shutdown = shutdown.clone();
        let write_task = tokio::spawn(async move {
            loop {
                let out = tokio::select! {
                    _ = write_shutdown.notified() => break,
                    out = rx.recv() => out,
                };
                let Some(out) = out else { break };
                if let Err(err) = write.send(out.message).await {
                    crate::logging::error(format!("ws send error: {err}"));
                    if let Some(key) = out.ack_key {
                        if let Some(sender) = pending_writer.lock().await.remove(&key) {
                            let _ = sender.send(Err(anyhow::anyhow!("ws send error: {err}")));
                        }
                    }
                    break;
                }
            }
        });

        let mut shutting_down = false;
        loop {
            let msg = tokio::select! {
                _ = shutdown.notified() => {
                    shutting_down = true;
                    break;
                },
                msg = read.next() => msg,
            };
            let Some(msg) = msg else { break };
            match msg {
                Ok(Message::Text(txt)) => {
                    handle_ws_message(&api, &datasites_root, &txt, None, &pending).await;
                }
                Ok(Message::Binary(bin)) => {
                    handle_ws_message(&api, &datasites_root, "", Some(&bin), &pending).await;
                }
                _ => {}
            }
        }

        write_task.abort();
        watcher_task.abort();

        let mut pending = pending.lock().await;
        for (_, sender) in pending.drain() {
            let _ = sender.send(Err(anyhow::anyhow!("ws disconnected")));
        }

        if shutting_down {
            return Ok(());
        }
        crate::logging::info("ws disconnected; reconnecting");
    }
}

async fn handle_ws_message(
    api: &ApiClient,
    datasites_root: &Path,
    raw_text: &str,
    raw_bin: Option<&[u8]>,
    pending: &PendingAcks,
) {
    let decoded = match raw_bin {
        Some(bin) => crate::wsproto::decode_binary(bin),
        None => crate::wsproto::decode_text_json(raw_text),
    };
    let decoded = match decoded {
        Ok(d) => d,
        Err(_) => return,
    };

    match decoded {
        Decoded::Ack(ack) => {
            if let Some(sender) = pending.lock().await.remove(&ack.original_id) {
                let _ = sender.send(Ok(()));
            }
        }
        Decoded::Nack(nack) => {
            if let Some(sender) = pending.lock().await.remove(&nack.original_id) {
                let _ = sender.send(Err(anyhow::anyhow!("NACK received: {}", nack.error)));
            }
        }
        Decoded::FileWrite(file) => handle_ws_file_write(api, datasites_root, file).await,
        Decoded::Http(http_msg) => handle_ws_http(api, datasites_root, http_msg).await,
        Decoded::Other { id, typ } => drop((id, typ)),
    }
}

async fn handle_ws_file_write(api: &ApiClient, datasites_root: &Path, file: FileWrite) {
    if let Some(content) = file.content {
        // Go treats empty content as a push notification (no embedded bytes) when length>0.
        // Avoid writing empty files; download the blob instead.
        if content.is_empty() && file.length > 0 {
            let _ = download_keys(api, datasites_root, vec![file.path]).await;
            return;
        }

        let etag = if file.etag.is_empty() {
            format!("{:x}", md5_compute(&content))
        } else {
            file.etag.clone()
        };
        if let Err(err) = write_bytes(datasites_root, &file.path, &content) {
            crate::logging::error(format!("ws write error: {err:?}"));
        } else if ws_should_update_journal() {
            if let Some(data_dir) = datasites_root.parent() {
                let _ = crate::sync::journal_upsert_direct(
                    data_dir,
                    &file.path,
                    &etag,
                    file.length,
                    chrono::Utc::now().timestamp(),
                );
            }
        }
        return;
    }
    if file.length == 0 {
        if let Err(err) = write_bytes(datasites_root, &file.path, &[]) {
            crate::logging::error(format!("ws write error: {err:?}"));
        } else if ws_should_update_journal() {
            if let Some(data_dir) = datasites_root.parent() {
                let etag = if file.etag.is_empty() {
                    // MD5 of empty content (matches server ETag semantics in devstack).
                    format!("{:x}", md5_compute([]))
                } else {
                    file.etag.clone()
                };
                let _ = crate::sync::journal_upsert_direct(
                    data_dir,
                    &file.path,
                    &etag,
                    0,
                    chrono::Utc::now().timestamp(),
                );
            }
        }
        return;
    }
    let _ = download_keys(api, datasites_root, vec![file.path]).await;
}

fn ws_should_update_journal() -> bool {
    match std::env::var("SYFTBOX_SYNC_HEAL_JOURNAL_GAPS") {
        Ok(raw) => {
            let v = raw.trim().to_lowercase();
            v != "0" && v != "false"
        }
        Err(_) => true,
    }
}

async fn handle_ws_http(api: &ApiClient, datasites_root: &Path, http_msg: HttpMsg) {
    if let Some(rel) = syft_url_to_rel_path(&http_msg.syft_url) {
        let file_key = format!("{rel}/{}.request", http_msg.id);
        if let Some(body) = http_msg.body {
            if let Err(err) = write_bytes(datasites_root, &file_key, &body) {
                crate::logging::error(format!("ws http write error: {err:?}"));
            }
        } else {
            let _ = download_keys(api, datasites_root, vec![file_key]).await;
        }
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

fn collect_synced_keys(datasites_root: &Path, filters: &SyncFilters) -> Vec<String> {
    if !datasites_root.exists() {
        return Vec::new();
    }

    let mut keys = Vec::new();
    for entry in WalkDir::new(datasites_root)
        .into_iter()
        .filter_map(|e| e.ok())
        .filter(|e| e.file_type().is_file())
    {
        let path = entry.path();
        let rel = match path.strip_prefix(datasites_root) {
            Ok(r) => r,
            Err(_) => continue,
        };
        let key = rel.to_string_lossy().replace('\\', "/");
        if key.is_empty() {
            continue;
        }
        if !is_synced_key(&key) {
            continue;
        }
        if SyncFilters::is_marked_rel_path(&key) {
            continue;
        }
        if filters.ignore.should_ignore_rel(Path::new(&key), false) {
            continue;
        }
        keys.push(key);
    }

    keys.sort();
    keys.dedup();
    keys
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

async fn watch_priority_files(
    root: &Path,
    my_email: &str,
    filters: std::sync::Arc<SyncFilters>,
    sync_kick: std::sync::Arc<tokio::sync::Notify>,
    ws: WsHandle,
    shutdown: std::sync::Arc<tokio::sync::Notify>,
) -> Result<()> {
    let (event_tx, mut event_rx) = tokio::sync::mpsc::channel(64);
    let mut watcher = RecommendedWatcher::new(
        move |res| {
            let _ = event_tx.blocking_send(res);
        },
        notify::Config::default(),
    )?;
    watcher.watch(root, RecursiveMode::Recursive)?;

    // Startup backstop: if the harness creates ACLs/requests immediately after spawning the
    // daemon, we can miss the fs event before the watcher is fully online. Scan the local
    // datasite once and eagerly publish any priority files (ACL first).
    let mut initial: Vec<PathBuf> = Vec::new();
    let my_root = root.join(my_email);
    if my_root.exists() {
        for entry in WalkDir::new(&my_root)
            .into_iter()
            .filter_map(|e| e.ok())
            .filter(|e| e.file_type().is_file())
        {
            initial.push(entry.path().to_path_buf());
        }
        initial.sort_by_key(|p| {
            let name = p.file_name().and_then(|n| n.to_str()).unwrap_or("");
            if name == "syft.pub.yaml" {
                0
            } else {
                1
            }
        });
        for path in initial {
            match send_priority_if_small(root, my_email, &filters, &path, &ws).await {
                Ok(did_trigger) => {
                    if did_trigger {
                        sync_kick.notify_one();
                    }
                }
                Err(err) => {
                    crate::logging::error(format!("priority send error (startup scan): {err:?}"))
                }
            }
        }
    }

    let debounce = Duration::from_millis(50);
    let mut pending: HashSet<PathBuf> = HashSet::new();

    let poll_interval = Duration::from_millis(250);
    let poll_until = std::time::Instant::now() + Duration::from_secs(10);
    let mut poll = tokio::time::interval(poll_interval);
    poll.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Delay);
    let mut seen_mtime_ns: std::collections::HashMap<PathBuf, i64> =
        std::collections::HashMap::new();

    loop {
        tokio::select! {
            _ = shutdown.notified() => break,
            maybe = event_rx.recv() => {
                let Some(res) = maybe else { break };
                ingest_event_paths(&mut pending, res);

                // Debounce/burst buffering: coalesce events for a short window.
                let timer = tokio::time::sleep(debounce);
                tokio::pin!(timer);
                loop {
                    tokio::select! {
                        _ = shutdown.notified() => return Ok(()),
                        _ = &mut timer => break,
                        next = event_rx.recv() => {
                            match next {
                                None => break,
                                Some(res) => ingest_event_paths(&mut pending, res),
                            }
                        }
                    }
                }

                let mut paths: Vec<PathBuf> = pending.drain().collect();
                paths.sort_by(|a, b| {
                    let a_name = a.file_name().and_then(|n| n.to_str()).unwrap_or("");
                    let b_name = b.file_name().and_then(|n| n.to_str()).unwrap_or("");
                    let a_is_acl = a_name == "syft.pub.yaml";
                    let b_is_acl = b_name == "syft.pub.yaml";
                    match (a_is_acl, b_is_acl) {
                        (true, false) => std::cmp::Ordering::Less,
                        (false, true) => std::cmp::Ordering::Greater,
                        _ => a.to_string_lossy().cmp(&b.to_string_lossy()),
                    }
                });
                for path in paths {
                    match send_priority_if_small(root, my_email, &filters, &path, &ws).await {
                        Ok(did_trigger) => {
                            if did_trigger {
                                sync_kick.notify_one();
                            }
                        }
                        Err(err) => crate::logging::error(format!("priority send error: {err:?}")),
                    }
                }
            }
            _ = poll.tick(), if std::time::Instant::now() < poll_until => {
                match poll_priority_changes(root, my_email, &filters, &ws, &mut seen_mtime_ns).await {
                    Ok(did_trigger) => {
                        if did_trigger {
                            sync_kick.notify_one();
                        }
                    }
                    Err(err) => crate::logging::error(format!("priority poll error: {err:?}")),
                }
            }
        }
    }

    Ok(())
}

async fn poll_priority_changes(
    root: &Path,
    my_email: &str,
    filters: &SyncFilters,
    ws: &WsHandle,
    seen_mtime_ns: &mut std::collections::HashMap<PathBuf, i64>,
) -> Result<bool> {
    let my_root = root.join(my_email);
    if !my_root.exists() {
        return Ok(false);
    }

    let mut did_trigger = false;
    let mut candidates: Vec<PathBuf> = WalkDir::new(&my_root)
        .into_iter()
        .filter_map(|e| e.ok())
        .filter(|e| e.file_type().is_file())
        .map(|e| e.path().to_path_buf())
        .collect();
    candidates.sort_by(|a, b| {
        let a_name = a.file_name().and_then(|n| n.to_str()).unwrap_or("");
        let b_name = b.file_name().and_then(|n| n.to_str()).unwrap_or("");
        let a_is_acl = a_name == "syft.pub.yaml";
        let b_is_acl = b_name == "syft.pub.yaml";
        match (a_is_acl, b_is_acl) {
            (true, false) => std::cmp::Ordering::Less,
            (false, true) => std::cmp::Ordering::Greater,
            _ => a.to_string_lossy().cmp(&b.to_string_lossy()),
        }
    });

    for path in candidates {
        let meta = match tokio::fs::metadata(&path).await {
            Ok(m) => m,
            Err(_) => continue,
        };
        let modified = match meta.modified() {
            Ok(m) => m,
            Err(_) => continue,
        };
        let ns = modified
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_nanos() as i64)
            .unwrap_or(0);

        let prev = seen_mtime_ns.get(&path).copied().unwrap_or(-1);
        if prev == ns {
            continue;
        }
        seen_mtime_ns.insert(path.clone(), ns);

        if send_priority_if_small(root, my_email, filters, &path, ws).await? {
            did_trigger = true;
        }
    }
    Ok(did_trigger)
}

fn ingest_event_paths(pending: &mut HashSet<PathBuf>, res: notify::Result<notify::Event>) {
    let event = match res {
        Ok(ev) => ev,
        Err(err) => {
            crate::logging::error(format!("notify error: {err:?}"));
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
    my_email: &str,
    filters: &SyncFilters,
    path: &Path,
    ws: &WsHandle,
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
    if !rel_str.starts_with(&format!("{my_email}/")) {
        // Never priority-upload files that belong to other datasites (prevents echo loops).
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

    let is_acl = rel_str.ends_with("syft.pub.yaml");
    if !is_acl && !ACL_READY.load(Ordering::SeqCst) {
        // Skip priority send until ACLs are established to avoid permission rejections.
        return Ok(false);
    }

    let bytes = tokio::fs::read(path).await?;
    let etag = format!("{:x}", md5_compute(&bytes));
    let ack_timeout = Duration::from_secs(5);
    match ws
        .send_filewrite_with_ack(rel_str.clone(), etag, size as i64, bytes, ack_timeout)
        .await
    {
        Ok(()) => {
            if is_acl {
                ACL_READY.store(true, Ordering::SeqCst);
            }
            Ok(true)
        }
        Err(err) => {
            crate::logging::error(format!("priority send ack error: {err:?}"));
            // Fallback: let normal sync handle it ASAP.
            Ok(true)
        }
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
    fn ensure_workspace_layout_written() {
        let tmp = env::temp_dir().join("syftbox-rs-acl-test");
        let _ = fs::remove_dir_all(&tmp);
        fs::create_dir_all(&tmp).unwrap();
        crate::workspace::ensure_workspace_layout(&tmp, "alice@example.com").unwrap();
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
        assert!(tmp.join("apps").exists());
        assert!(tmp.join(".data").exists());
    }
}
