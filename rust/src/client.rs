use std::{
    collections::HashSet,
    env, fs, io,
    path::{Path, PathBuf},
    sync::atomic::{AtomicBool, Ordering},
    time::{Duration, Instant},
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
        handshake::client::Response as WsResponse,
        http::header::AUTHORIZATION,
        http::HeaderValue,
        http::StatusCode as HttpStatusCode,
        protocol::{Message, WebSocketConfig},
    },
};
use url::Url;
use uuid::Uuid;
use walkdir::WalkDir;

use crate::acl_staging::{ACLStagingManager, StagedACL};
use crate::config::Config;
use crate::control::ControlPlane;
use crate::filters::SyncFilters;
use crate::hotlink_manager::HotlinkManager;
use crate::http::ApiClient;
use crate::subscriptions;
use crate::sync::{
    compute_local_etag, download_keys, ensure_parent_dirs, sync_once_with_control,
    write_file_resolving_conflicts,
};
use crate::wsproto::{
    ACLManifest, Decoded, Encoding, FileWrite, HttpMsg, MsgpackFileWrite, WS_MAX_MESSAGE_BYTES,
};

static ACL_READY: AtomicBool = AtomicBool::new(false);

type PendingAcks = std::sync::Arc<
    tokio::sync::Mutex<std::collections::HashMap<String, oneshot::Sender<Result<()>>>>,
>;

fn maybe_apply_hotlink_ice_from_server(resp: &WsResponse) {
    if env::var("SYFTBOX_HOTLINK_ICE_SERVERS").is_err() {
        if let Some(ice) = resp
            .headers()
            .get("X-Syft-Hotlink-Ice-Servers")
            .and_then(|v| v.to_str().ok())
            .map(|v| v.trim())
            .filter(|v| !v.is_empty())
        {
            env::set_var("SYFTBOX_HOTLINK_ICE_SERVERS", ice);
            crate::logging::info(format!("hotlink ICE servers from server: {}", ice));
        }
    }

    if env::var("SYFTBOX_HOTLINK_TURN_USER").is_err() {
        if let Some(user) = resp
            .headers()
            .get("X-Syft-Hotlink-Turn-User")
            .and_then(|v| v.to_str().ok())
            .map(|v| v.trim())
            .filter(|v| !v.is_empty())
        {
            env::set_var("SYFTBOX_HOTLINK_TURN_USER", user);
            crate::logging::info(format!("hotlink TURN user from server: {}", user));
        }
    }

    if env::var("SYFTBOX_HOTLINK_TURN_PASS").is_err() {
        if let Some(pass) = resp
            .headers()
            .get("X-Syft-Hotlink-Turn-Pass")
            .and_then(|v| v.to_str().ok())
            .map(|v| v.trim())
            .filter(|v| !v.is_empty())
        {
            env::set_var("SYFTBOX_HOTLINK_TURN_PASS", pass);
            crate::logging::info("hotlink TURN pass from server: [set]");
        }
    }
}

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
        let sync_scheduler = std::sync::Arc::new(AdaptiveSyncScheduler::new());

        // Create ACL staging manager at this level so it's shared between WS listener and sync loop
        let datasites_root = data_dir.join("datasites");
        let datasites_root_for_staging = datasites_root.clone();
        let data_dir_for_staging = data_dir.clone();
        let acl_staging = std::sync::Arc::new(ACLStagingManager::new(move |datasite, acls| {
            on_acl_set_ready(
                &datasites_root_for_staging,
                &data_dir_for_staging,
                &datasite,
                acls,
            );
        }));

        crate::workspace::ensure_workspace_layout(&data_dir, &email)?;
        let _workspace_lock = maybe_lock_workspace(&data_dir)?;
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
            res = run_ws_listener(api.clone(), server_url, data_dir.clone(), email.clone(), filters.clone(), sync_kick.clone(), sync_scheduler.clone(), shutdown.clone(), acl_staging.clone()) => {
                if let Err(err) = res {
                    crate::logging::error(format!("ws listener crashed: {err:?}"));
                }
            }
            res = run_sync_loop(api.clone(), data_dir, email, control.clone(), filters, sync_kick, sync_scheduler, acl_staging) => {
                if let Err(err) = res {
                    crate::logging::error(format!("sync loop crashed: {err:?}"));
                }
            }
            _ = run_ping_loop(api, control) => {}
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
        let sync_scheduler = std::sync::Arc::new(AdaptiveSyncScheduler::new());

        // Create ACL staging manager at this level so it's shared between WS listener and sync loop
        let datasites_root = data_dir.join("datasites");
        let datasites_root_for_staging = datasites_root.clone();
        let data_dir_for_staging = data_dir.clone();
        let acl_staging = std::sync::Arc::new(ACLStagingManager::new(move |datasite, acls| {
            on_acl_set_ready(
                &datasites_root_for_staging,
                &data_dir_for_staging,
                &datasite,
                acls,
            );
        }));

        crate::workspace::ensure_workspace_layout(&data_dir, &email)?;
        let _workspace_lock = maybe_lock_workspace(&data_dir)?;
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
            res = run_ws_listener(api.clone(), server_url, data_dir.clone(), email.clone(), filters.clone(), sync_kick.clone(), sync_scheduler.clone(), shutdown.clone(), acl_staging.clone()) => {
                if let Err(err) = res {
                    crate::logging::error(format!("ws listener crashed: {err:?}"));
                }
            }
            res = run_sync_loop(api.clone(), data_dir, email, control.clone(), filters, sync_kick, sync_scheduler, acl_staging) => {
                if let Err(err) = res {
                    crate::logging::error(format!("sync loop crashed: {err:?}"));
                }
            }
            _ = run_ping_loop(api, control) => {}
        }

        Ok(())
    }
}

fn maybe_lock_workspace(data_dir: &Path) -> Result<Option<crate::workspace::WorkspaceLock>> {
    let skip_dir = env::var("SYFTBOX_SKIP_WORKSPACE_LOCK_DATA_DIR").ok();
    if skip_dir
        .as_deref()
        .map(|d| paths_match_for_skip_lock(d, data_dir))
        .unwrap_or(false)
    {
        crate::logging::info("Skipping workspace lock (embedded lock held)");
        return Ok(None);
    }
    Ok(Some(crate::workspace::WorkspaceLock::try_lock(data_dir)?))
}

fn paths_match_for_skip_lock(skip_dir: &str, data_dir: &Path) -> bool {
    let skip = normalize_path_for_compare(skip_dir);
    let data = normalize_path_for_compare(&data_dir.to_string_lossy());
    skip == data
}

fn normalize_path_for_compare(raw: &str) -> String {
    let mut out = raw.replace('\\', "/");
    if let Some(stripped) = out.strip_prefix("//?/") {
        out = stripped.to_string();
    }
    while out.ends_with('/') {
        out.pop();
    }
    if cfg!(windows) {
        out = out.to_ascii_lowercase();
    }
    out
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

#[allow(clippy::too_many_arguments)]
async fn run_sync_loop(
    api: ApiClient,
    data_dir: PathBuf,
    email: String,
    control: Option<ControlPlane>,
    filters: std::sync::Arc<SyncFilters>,
    sync_kick: std::sync::Arc<tokio::sync::Notify>,
    sync_scheduler: std::sync::Arc<AdaptiveSyncScheduler>,
    acl_staging: std::sync::Arc<ACLStagingManager>,
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
            &acl_staging,
        )
        .await
        {
            crate::logging::error(format!("sync error: {err:?}"));
        }

        let sleep_for = sync_scheduler.interval();
        if let Some(cp) = &control {
            tokio::select! {
                _ = sleep(sleep_for) => {}
                _ = cp.wait_sync_now() => {
                    sync_scheduler.record_activity();
                }
                _ = sync_kick.notified() => {
                    sync_scheduler.record_activity();
                }
            }
        } else {
            tokio::select! {
                _ = sleep(sleep_for) => {}
                _ = sync_kick.notified() => {
                    sync_scheduler.record_activity();
                }
            }
        }
    }
}

const PING_INTERVAL: Duration = Duration::from_secs(5);

async fn run_ping_loop(api: ApiClient, control: Option<ControlPlane>) {
    loop {
        if let Some(cp) = &control {
            let start = Instant::now();
            match api.healthz().await {
                Ok(()) => {
                    let latency_ms = start.elapsed().as_millis() as u64;
                    cp.record_latency(latency_ms);
                }
                Err(err) => {
                    crate::logging::info(format!("ping failed: {err}"));
                }
            }
        }
        sleep(PING_INTERVAL).await;
    }
}

const SYNC_INTERVAL_STARTUP: Duration = Duration::from_millis(100);
const SYNC_INTERVAL_BURST: Duration = Duration::from_millis(100);
const SYNC_INTERVAL_ACTIVE: Duration = Duration::from_millis(100);
const SYNC_INTERVAL_MODERATE: Duration = Duration::from_millis(500);
const SYNC_INTERVAL_IDLE: Duration = Duration::from_secs(1);
const SYNC_INTERVAL_IDLE2: Duration = Duration::from_secs(2);
const SYNC_INTERVAL_IDLE3: Duration = Duration::from_secs(5);
const SYNC_INTERVAL_DEEP_IDLE: Duration = Duration::from_secs(10);

const ACTIVITY_BURST_THRESHOLD: usize = 10;
const ACTIVITY_ACTIVE_THRESHOLD: usize = 3;
const ACTIVITY_MODERATE_THRESHOLD: usize = 1;
const ACTIVITY_WINDOW: Duration = Duration::from_secs(10);

const IDLE_TIMEOUT_1: Duration = Duration::from_secs(5);
const IDLE_TIMEOUT_2: Duration = Duration::from_secs(15);
const DEEP_IDLE_TIMEOUT: Duration = Duration::from_secs(60);

const STARTUP_DURATION: Duration = Duration::from_secs(3);

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
enum ActivityLevel {
    Startup,
    Burst,
    Active,
    Moderate,
    Idle,
    Idle2,
    Idle3,
    DeepIdle,
}

struct AdaptiveSyncState {
    created_at: Instant,
    last_activity: Instant,
    events: Vec<Instant>,
    level: ActivityLevel,
}

impl AdaptiveSyncState {
    fn new(now: Instant) -> Self {
        Self {
            created_at: now,
            last_activity: now,
            events: Vec::with_capacity(ACTIVITY_BURST_THRESHOLD * 2),
            level: ActivityLevel::Startup,
        }
    }

    fn update_activity_level(&mut self, now: Instant) {
        let event_count = self.events.len();
        let time_since_activity = now.duration_since(self.last_activity);
        let time_since_creation = now.duration_since(self.created_at);

        let new_level = if time_since_creation < STARTUP_DURATION {
            ActivityLevel::Startup
        } else {
            match event_count {
                n if n >= ACTIVITY_BURST_THRESHOLD => ActivityLevel::Burst,
                n if n >= ACTIVITY_ACTIVE_THRESHOLD => ActivityLevel::Active,
                n if n >= ACTIVITY_MODERATE_THRESHOLD => ActivityLevel::Moderate,
                _ => {
                    if time_since_activity < IDLE_TIMEOUT_1 {
                        ActivityLevel::Idle
                    } else if time_since_activity < IDLE_TIMEOUT_2 {
                        ActivityLevel::Idle2
                    } else if time_since_activity < DEEP_IDLE_TIMEOUT {
                        ActivityLevel::Idle3
                    } else {
                        ActivityLevel::DeepIdle
                    }
                }
            }
        };

        self.level = new_level;
    }

    fn interval(&self) -> Duration {
        match self.level {
            ActivityLevel::Startup => SYNC_INTERVAL_STARTUP,
            ActivityLevel::Burst => SYNC_INTERVAL_BURST,
            ActivityLevel::Active => SYNC_INTERVAL_ACTIVE,
            ActivityLevel::Moderate => SYNC_INTERVAL_MODERATE,
            ActivityLevel::Idle => SYNC_INTERVAL_IDLE,
            ActivityLevel::Idle2 => SYNC_INTERVAL_IDLE2,
            ActivityLevel::Idle3 => SYNC_INTERVAL_IDLE3,
            ActivityLevel::DeepIdle => SYNC_INTERVAL_DEEP_IDLE,
        }
    }
}

struct AdaptiveSyncScheduler {
    state: std::sync::Mutex<AdaptiveSyncState>,
}

impl AdaptiveSyncScheduler {
    fn new() -> Self {
        let now = Instant::now();
        Self {
            state: std::sync::Mutex::new(AdaptiveSyncState::new(now)),
        }
    }

    fn record_activity(&self) {
        let now = Instant::now();
        let mut state = self.state.lock().expect("sync scheduler lock");
        state.last_activity = now;
        state.events.push(now);
        let cutoff = now.checked_sub(ACTIVITY_WINDOW).unwrap_or(now);
        state.events.retain(|ts| *ts >= cutoff);
        state.update_activity_level(now);
    }

    fn interval(&self) -> Duration {
        let now = Instant::now();
        let mut state = self.state.lock().expect("sync scheduler lock");
        state.update_activity_level(now);
        state.interval()
    }
}

#[derive(Clone)]
pub(crate) struct WsHandle {
    encoding: Encoding,
    tx: mpsc::Sender<WsOutbound>,
    pending: PendingAcks,
}

struct WsOutbound {
    message: Message,
    ack_key: Option<String>,
}

impl WsHandle {
    pub(crate) fn encoding(&self) -> Encoding {
        self.encoding
    }
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

    pub(crate) async fn send_ws(&self, msg: Message) -> Result<()> {
        self.tx
            .send(WsOutbound {
                message: msg,
                ack_key: None,
            })
            .await
            .map_err(|err| anyhow::anyhow!(err))
            .context("ws send queue closed")?;
        Ok(())
    }
}

#[allow(clippy::too_many_arguments)]
async fn run_ws_listener(
    api: ApiClient,
    server_url: String,
    data_dir: PathBuf,
    email: String,
    filters: std::sync::Arc<SyncFilters>,
    sync_kick: std::sync::Arc<tokio::sync::Notify>,
    sync_scheduler: std::sync::Arc<AdaptiveSyncScheduler>,
    shutdown: std::sync::Arc<tokio::sync::Notify>,
    acl_staging: std::sync::Arc<ACLStagingManager>,
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
        maybe_apply_hotlink_ice_from_server(&resp);

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
        let hotlink_mgr =
            HotlinkManager::new(datasites_root.clone(), ws_handle.clone(), shutdown.clone());
        if hotlink_mgr.enabled() {
            hotlink_mgr.start_local_discovery(email.clone());
            hotlink_mgr.start_tcp_proxy_discovery(email.clone());
        }

        let watch_root = datasites_root.clone();
        let watcher_filters = filters.clone();
        let watcher_kick = sync_kick.clone();
        let watcher_ws = ws_handle.clone();
        let watcher_email = email.clone();
        let watcher_shutdown = shutdown.clone();
        let watcher_scheduler = sync_scheduler.clone();
        let watcher_task = tokio::spawn(async move {
            if let Err(err) = watch_priority_files(
                &watch_root,
                &watcher_email,
                watcher_filters,
                watcher_kick,
                watcher_scheduler,
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
                    handle_ws_message(
                        &api,
                        &datasites_root,
                        &data_dir,
                        &email,
                        &txt,
                        None,
                        &pending,
                        &acl_staging,
                        &hotlink_mgr,
                    )
                    .await;
                }
                Ok(Message::Binary(bin)) => {
                    handle_ws_message(
                        &api,
                        &datasites_root,
                        &data_dir,
                        &email,
                        "",
                        Some(&bin),
                        &pending,
                        &acl_staging,
                        &hotlink_mgr,
                    )
                    .await;
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

#[allow(clippy::too_many_arguments)]
async fn handle_ws_message(
    api: &ApiClient,
    datasites_root: &Path,
    data_dir: &Path,
    owner_email: &str,
    raw_text: &str,
    raw_bin: Option<&[u8]>,
    pending: &PendingAcks,
    acl_staging: &std::sync::Arc<ACLStagingManager>,
    hotlink_mgr: &HotlinkManager,
) {
    let decoded = match raw_bin {
        Some(bin) => crate::wsproto::decode_binary(bin),
        None => crate::wsproto::decode_text_json(raw_text),
    };
    let decoded = match decoded {
        Ok(d) => d,
        Err(e) => {
            crate::logging::error(format!("ws decode error: {e:?}"));
            return;
        }
    };

    crate::logging::info(format!("ws message decoded type={:?}", &decoded));

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
        Decoded::FileWrite(file) => {
            handle_ws_file_write(
                api,
                datasites_root,
                data_dir,
                owner_email,
                file,
                acl_staging,
            )
            .await
        }
        Decoded::HotlinkOpen(open) => {
            hotlink_mgr
                .handle_open(open.session_id, open.path, open.from)
                .await;
        }
        Decoded::HotlinkAccept(accept) => {
            hotlink_mgr.handle_accept(accept.session_id).await;
        }
        Decoded::HotlinkReject(reject) => {
            hotlink_mgr
                .handle_reject(reject.session_id, reject.reason)
                .await;
        }
        Decoded::HotlinkData(data) => {
            if let Some(payload) = data.payload {
                hotlink_mgr
                    .handle_data(data.session_id, data.path, data.etag, data.seq, payload)
                    .await;
            }
        }
        Decoded::HotlinkClose(close) => {
            hotlink_mgr.handle_close(close.session_id).await;
        }
        Decoded::HotlinkSignal(signal) => {
            hotlink_mgr.handle_signal(signal).await;
        }
        Decoded::Http(http_msg) => handle_ws_http(api, datasites_root, http_msg).await,
        Decoded::ACLManifest(manifest) => handle_ws_acl_manifest(manifest, acl_staging),
        Decoded::Other { id, typ } => drop((id, typ)),
    }
}

async fn handle_ws_file_write(
    api: &ApiClient,
    datasites_root: &Path,
    data_dir: &Path,
    owner_email: &str,
    file: FileWrite,
    acl_staging: &std::sync::Arc<ACLStagingManager>,
) {
    // Windows debug: log incoming file write
    crate::logging::info(format!(
        "ws_file_write path={} etag={} length={} has_content={}",
        file.path,
        file.etag,
        file.length,
        file.content.is_some()
    ));

    // Stage ACL for potential ordered application, but don't block the write
    // When all ACLs arrive, on_acl_set_ready will re-apply them in order
    let is_acl_file = file.path.ends_with("/syft.pub.yaml") || file.path == "syft.pub.yaml";
    if is_acl_file {
        // Extract datasite from path, handling potential leading slash
        let path_without_leading = file.path.trim_start_matches('/');
        let datasite = path_without_leading.split('/').next().unwrap_or("");
        crate::logging::info(format!(
            "ws_acl_file path={} extracted_datasite={}",
            file.path, datasite
        ));
        if !datasite.is_empty() {
            // Note ACL activity BEFORE checking for pending manifest (matches Go behavior).
            // This refreshes the grace window for the datasite, protecting ACL files from
            // deletion even when the user doesn't receive a new manifest (e.g., when another
            // user's ACL change triggers a broadcast that we receive).
            acl_staging.note_acl_activity(datasite);

            if acl_staging.has_pending_manifest(datasite) {
                // Get the ACL directory path (the parent of syft.pub.yaml)
                let acl_dir = if file.path.contains('/') {
                    let path = std::path::Path::new(&file.path);
                    path.parent()
                        .map(|p| p.to_string_lossy().to_string())
                        .unwrap_or_else(|| datasite.to_string())
                } else {
                    datasite.to_string()
                };

                // Stage the ACL but continue to write it immediately
                if let Some(ref content) = file.content {
                    acl_staging.stage_acl(datasite, &acl_dir, content.clone(), file.etag.clone());
                }
            }
        }
    }

    if !is_acl_file && subscriptions::is_sub_file(&file.path) {
        return;
    }

    if !is_acl_file {
        let subs_path = subscriptions::config_path(data_dir);
        let subs = subscriptions::load(&subs_path).unwrap_or_else(|err| {
            crate::logging::error(format!(
                "subscriptions load error path={} err={:?}",
                subs_path.display(),
                err
            ));
            subscriptions::default_config()
        });
        let action = subscriptions::action_for_path(&subs, owner_email, &file.path);
        if action != subscriptions::Action::Allow {
            crate::logging::info(format!(
                "ws_file_write skipped by subscription action={:?} path={}",
                action, file.path
            ));
            return;
        }
    }

    if let Some(content) = file.content {
        // Go treats empty content as a push notification (no embedded bytes) when length>0.
        // Avoid writing empty files; download the blob instead.
        if content.is_empty() && file.length > 0 {
            match download_keys(api, datasites_root, vec![file.path.clone()]).await {
                Ok(()) => {
                    journal_upsert_downloaded(datasites_root, &file.path, &file.etag, file.length);
                }
                Err(err) => {
                    crate::logging::error(format!("ws download error: {err:?}"));
                }
            }
            return;
        }

        let local_etag = format!("{:x}", md5_compute(&content));
        let etag = if file.etag.is_empty() {
            local_etag.clone()
        } else {
            file.etag.clone()
        };
        let local_path = datasites_root.join(&file.path);
        crate::logging::info(format!(
            "ws_file_write writing content path={} local_path={} content_len={}",
            file.path,
            local_path.display(),
            content.len()
        ));
        if let Err(err) = write_bytes(datasites_root, &file.path, &content) {
            crate::logging::error(format!("ws write error path={} err={:?}", file.path, err));
        } else {
            crate::logging::info(format!("ws_file_write success path={}", file.path));
            if ws_should_update_journal() {
                crate::logging::info(format!(
                    "ws_file_write journal_upsert path={} etag={} local_etag={}",
                    file.path, etag, local_etag
                ));
                let _ = crate::sync::journal_upsert_direct(
                    data_dir,
                    &file.path,
                    &etag,
                    &local_etag,
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
            // MD5 of empty content (matches server ETag semantics in devstack).
            let local_etag = format!("{:x}", md5_compute([]));
            let etag = if file.etag.is_empty() {
                local_etag.clone()
            } else {
                file.etag.clone()
            };
            let _ = crate::sync::journal_upsert_direct(
                data_dir,
                &file.path,
                &etag,
                &local_etag,
                0,
                chrono::Utc::now().timestamp(),
            );
        }
        return;
    }
    match download_keys(api, datasites_root, vec![file.path.clone()]).await {
        Ok(()) => {
            journal_upsert_downloaded(datasites_root, &file.path, &file.etag, file.length);
        }
        Err(err) => {
            crate::logging::error(format!("ws download error: {err:?}"));
        }
    }
}

fn handle_ws_acl_manifest(manifest: ACLManifest, acl_staging: &std::sync::Arc<ACLStagingManager>) {
    crate::logging::info(format!(
        "acl manifest received datasite={} for={} forHash={} aclCount={}",
        manifest.datasite,
        manifest.for_user,
        manifest.for_hash,
        manifest.acl_order.len()
    ));
    acl_staging.set_manifest(manifest);
}

fn on_acl_set_ready(datasites_root: &Path, data_dir: &Path, datasite: &str, acls: Vec<StagedACL>) {
    crate::logging::info(format!(
        "applying {} ACLs in order for datasite={}",
        acls.len(),
        datasite
    ));

    let tmp_dir = data_dir.join(".syft-tmp");
    if let Err(err) = fs::create_dir_all(&tmp_dir) {
        crate::logging::error(format!("failed to create tmp dir: {err:?}"));
        return;
    }

    for acl in acls {
        let acl_file_path = format!("{}/syft.pub.yaml", acl.path);
        let abs_path = datasites_root.join(&acl_file_path);

        if let Err(err) = ensure_parent_dirs(&abs_path) {
            crate::logging::error(format!(
                "failed to create parent dirs for {}: {err:?}",
                acl_file_path
            ));
            continue;
        }

        if let Err(err) = write_file_resolving_conflicts(&abs_path, &acl.content) {
            crate::logging::error(format!("failed to write ACL {}: {err:?}", acl_file_path));
            continue;
        }

        // Update the sync journal
        let size = acl.content.len() as i64;
        let local_etag = compute_local_etag(&abs_path, size).unwrap_or_default();
        let etag = if acl.etag.is_empty() {
            local_etag.clone()
        } else {
            acl.etag.clone()
        };

        let _ = crate::sync::journal_upsert_direct(
            data_dir,
            &acl_file_path,
            &etag,
            &local_etag,
            size,
            chrono::Utc::now().timestamp(),
        );

        crate::logging::info(format!("applied ACL {}", acl_file_path));
    }
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
            } else {
                let etag = format!("{:x}", md5_compute(&body));
                journal_upsert_downloaded(datasites_root, &file_key, &etag, body.len() as i64);
            }
        } else {
            match download_keys(api, datasites_root, vec![file_key.clone()]).await {
                Ok(()) => {
                    journal_upsert_downloaded(datasites_root, &file_key, "", 0);
                }
                Err(err) => {
                    crate::logging::error(format!("ws http download error: {err:?}"));
                }
            }
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

fn journal_upsert_downloaded(datasites_root: &Path, key: &str, etag_hint: &str, size_hint: i64) {
    if !ws_should_update_journal() {
        return;
    }
    let Some(data_dir) = datasites_root.parent() else {
        return;
    };
    let target = datasites_root.join(key);
    if !target.exists() {
        return;
    }

    let size = if size_hint > 0 {
        size_hint
    } else {
        fs::metadata(&target).map(|m| m.len() as i64).unwrap_or(0)
    };
    let local_etag = crate::sync::compute_local_etag(&target, size).unwrap_or_default();
    let etag = if !etag_hint.trim().is_empty() {
        etag_hint.to_string()
    } else {
        local_etag.clone()
    };

    if etag.is_empty() && local_etag.is_empty() {
        return;
    }

    let _ = crate::sync::journal_upsert_direct(
        data_dir,
        key,
        &etag,
        &local_etag,
        size,
        chrono::Utc::now().timestamp(),
    );
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
                let Some(rel) = strip_datasites_prefix(datasites_root, path) else {
                    continue;
                };
                // Normalize path separators for Windows compatibility (server expects forward slashes)
                let key = rel.to_string_lossy().replace('\\', "/");
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
        let Some(rel) = strip_datasites_prefix(datasites_root, path) else {
            continue;
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
    sync_scheduler: std::sync::Arc<AdaptiveSyncScheduler>,
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
                        sync_scheduler.record_activity();
                        sync_kick.notify_one();
                    }
                }
                Err(err) => {
                    crate::logging::error(format!("priority send error (startup scan): {err:?}"))
                }
            }
        }
    }

    let debounce = {
        let mut ms = 50u64;
        if let Ok(v) = std::env::var("SYFTBOX_PRIORITY_DEBOUNCE_MS") {
            if let Ok(parsed) = v.trim().parse::<u64>() {
                ms = parsed;
            }
        }
        Duration::from_millis(ms)
    };
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
                if !paths.is_empty() {
                    sync_scheduler.record_activity();
                }
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
                                sync_scheduler.record_activity();
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
                            sync_scheduler.record_activity();
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
    let meta = match tokio::fs::metadata(path).await {
        Ok(meta) => meta,
        Err(err) if err.kind() == io::ErrorKind::NotFound => return Ok(false),
        Err(err) => return Err(err.into()),
    };
    if !meta.is_file() {
        return Ok(false);
    }

    let rel = match strip_datasites_prefix(root, path) {
        Some(rel) => rel,
        None => return Ok(false),
    };
    let rel_str = normalize_ws_path(&rel);

    if filters.ignore.should_ignore_rel(&rel, false) {
        return Ok(false);
    }
    if !rel_str.starts_with(&format!("{my_email}/")) {
        // Never priority-upload files that belong to other datasites (prevents echo loops).
        return Ok(false);
    }
    if SyncFilters::is_marked_rel_path(&rel_str) {
        return Ok(false);
    }

    let is_priority = filters.priority.should_prioritize_rel(&rel, false);
    if !is_priority {
        // Non-priority changes should still wake the sync loop.
        return Ok(true);
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

    let bytes = match tokio::fs::read(path).await {
        Ok(bytes) => bytes,
        Err(err) if err.kind() == io::ErrorKind::NotFound => return Ok(false),
        Err(err) => return Err(err.into()),
    };
    let etag = format!("{:x}", md5_compute(&bytes));
    let ack_timeout = Duration::from_secs(5);
    crate::logging::info(format!(
        "priority send attempting path={} size={} is_acl={}",
        rel_str, size, is_acl
    ));
    match ws
        .send_filewrite_with_ack(rel_str.clone(), etag, size as i64, bytes, ack_timeout)
        .await
    {
        Ok(()) => {
            crate::logging::info(format!(
                "priority send SUCCESS path={} is_acl={}",
                rel_str, is_acl
            ));
            if is_acl {
                ACL_READY.store(true, Ordering::SeqCst);
            }
            Ok(true)
        }
        Err(err) => {
            crate::logging::error(format!("priority send ack error for {}: {err:?}", rel_str));
            // Fallback: let normal sync handle it ASAP.
            Ok(true)
        }
    }
}

fn strip_datasites_prefix(root: &Path, path: &Path) -> Option<PathBuf> {
    // Try original paths first (fast path)
    if let Ok(rel) = path.strip_prefix(root) {
        return Some(rel.to_path_buf());
    }
    // Fall back to canonicalized paths to handle macOS /tmp -> /private/tmp symlinks
    let root_resolved = resolve_path(root);
    let path_resolved = resolve_path(path);
    path_resolved
        .strip_prefix(&root_resolved)
        .ok()
        .map(|p| p.to_path_buf())
}

fn normalize_ws_path(path: &Path) -> String {
    path.to_string_lossy().replace('\\', "/")
}

fn resolve_path(path: &Path) -> PathBuf {
    std::fs::canonicalize(path).unwrap_or_else(|_| path.to_path_buf())
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
