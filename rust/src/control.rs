use std::{collections::HashMap, fs, net::SocketAddr, path::PathBuf, sync::Arc, sync::Mutex};

use axum::{
    extract::{Path as AxumPath, Query, State},
    http::{HeaderMap, StatusCode},
    response::sse::{Event, KeepAlive, Sse},
    response::IntoResponse,
    routing::{delete, get, post},
    Json, Router,
};
use chrono::{DateTime, Utc};
use futures_util::stream::unfold;
use serde::{Deserialize, Serialize};
use tokio::sync::{broadcast, Notify};
use uuid::Uuid;
use walkdir::WalkDir;

use crate::telemetry::{HttpStats, LatencyStats};
use crate::{http::ApiClient, subscriptions};

#[derive(Clone, Debug)]
pub struct ControlPlane {
    state: Arc<ControlState>,
    bound_addr: SocketAddr,
}

// Manual Debug impl needed because ControlState contains non-Debug types
impl std::fmt::Debug for ControlState {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ControlState")
            .field("token", &"[redacted]")
            .finish()
    }
}

/// Result of starting the control plane, including the actual bound address.
#[derive(Debug, Clone)]
pub struct ControlPlaneStartResult {
    pub control_plane: ControlPlane,
    pub bound_addr: SocketAddr,
}

struct ControlState {
    token: String,
    uploads: Mutex<HashMap<String, UploadEntry>>,
    sync_status: Mutex<HashMap<String, SyncFileStatus>>,
    sync_events: broadcast::Sender<SyncFileStatus>,
    sync_now: Notify,
    http_stats: Arc<HttpStats>,
    latency_stats: Arc<LatencyStats>,
    data_dir: PathBuf,
    owner_email: String,
    api: Option<ApiClient>,
}

#[derive(Clone, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
struct UploadEntry {
    id: String,
    key: String,
    state: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    local_path: Option<String>,
    size: i64,
    uploaded_bytes: i64,
    part_size: Option<i64>,
    part_count: Option<i64>,
    completed_parts: Vec<i64>,
    progress: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
    started_at: DateTime<Utc>,
    updated_at: DateTime<Utc>,
}

#[derive(Clone, Serialize, Deserialize)]
struct SyncFileStatus {
    path: String,
    state: String,
    #[serde(rename = "conflictState")]
    #[serde(default = "default_conflict_state")]
    conflict_state: String,
    #[serde(default)]
    progress: f64,
    #[serde(skip_serializing_if = "String::is_empty")]
    #[serde(default)]
    error: String,
    #[serde(rename = "errorCount", skip_serializing_if = "is_zero")]
    #[serde(default)]
    error_count: i64,
    #[serde(rename = "updatedAt")]
    updated_at: DateTime<Utc>,
}

fn default_conflict_state() -> String {
    "none".to_string()
}

fn is_zero(v: &i64) -> bool {
    *v == 0
}

fn parse_action(raw: &str) -> Option<subscriptions::Action> {
    match raw.trim().to_lowercase().as_str() {
        "allow" => Some(subscriptions::Action::Allow),
        "pause" => Some(subscriptions::Action::Pause),
        "block" | "deny" => Some(subscriptions::Action::Block),
        _ => None,
    }
}

#[derive(Serialize, Deserialize)]
struct SyncSummary {
    pending: usize,
    syncing: usize,
    completed: usize,
    error: usize,
}

#[derive(Serialize, Deserialize)]
struct SyncStatusResponse {
    files: Vec<SyncFileStatus>,
    summary: SyncSummary,
}

#[derive(Serialize, Deserialize)]
struct UploadListResponse {
    uploads: Vec<UploadEntry>,
}

#[derive(Serialize, Deserialize)]
struct SubscriptionsResponse {
    path: String,
    config: subscriptions::Subscriptions,
}

#[derive(Serialize, Deserialize)]
struct SubscriptionsUpdateRequest {
    config: subscriptions::Subscriptions,
}

#[derive(Serialize)]
struct DiscoveryFile {
    path: String,
    etag: String,
    size: i64,
    #[serde(rename = "lastModified")]
    last_modified: DateTime<Utc>,
    action: String,
}

#[derive(Serialize)]
struct DiscoveryResponse {
    files: Vec<DiscoveryFile>,
}

#[derive(Serialize)]
struct EffectiveFile {
    path: String,
    action: String,
    allowed: bool,
}

#[derive(Serialize)]
struct EffectiveResponse {
    files: Vec<EffectiveFile>,
}

#[derive(Serialize)]
struct SyncQueueResponse {
    files: Vec<SyncFileStatus>,
}

#[derive(Serialize)]
struct PublicationEntry {
    path: String,
    content: String,
}

#[derive(Serialize)]
struct MarkedFileInfo {
    path: String,
    #[serde(rename = "markerType")]
    marker_type: String,
    #[serde(rename = "originalPath")]
    original_path: String,
    size: u64,
    #[serde(rename = "modTime")]
    mod_time: DateTime<Utc>,
}

#[derive(Serialize)]
struct ConflictsSummary {
    #[serde(rename = "conflictCount")]
    conflict_count: usize,
    #[serde(rename = "rejectedCount")]
    rejected_count: usize,
}

#[derive(Serialize)]
struct ConflictsResponse {
    conflicts: Vec<MarkedFileInfo>,
    rejected: Vec<MarkedFileInfo>,
    summary: ConflictsSummary,
}

#[derive(Serialize)]
struct CleanupResponse {
    #[serde(rename = "cleanedCount")]
    cleaned_count: usize,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    errors: Vec<String>,
}

#[derive(Serialize)]
struct PublicationsResponse {
    files: Vec<PublicationEntry>,
}

#[derive(Deserialize)]
struct SubscriptionsRuleRequest {
    rule: subscriptions::Rule,
}

#[derive(Deserialize)]
struct SubscriptionsRuleDeleteQuery {
    datasite: Option<String>,
    path: String,
    action: Option<String>,
}

impl ControlPlane {
    /// Start the control plane HTTP server.
    ///
    /// This function will:
    /// 1. Try to bind to the configured address with retries (handles port TIME_WAIT after process kill)
    /// 2. If that fails after retries, try binding to port 0 (OS assigns random available port)
    /// 3. Return the actual bound address so callers know where the server is listening
    ///
    /// All operations are logged for debugging.
    pub async fn start_async(
        addr: &str,
        token: Option<String>,
        http_stats: Arc<HttpStats>,
        shutdown: Option<Arc<Notify>>,
        data_dir: PathBuf,
        owner_email: String,
        server_url: String,
        api: Option<ApiClient>,
    ) -> anyhow::Result<ControlPlaneStartResult> {
        let token = token.unwrap_or_else(|| Uuid::new_v4().as_simple().to_string());

        crate::logging::info_kv(
            "control plane starting",
            &[("requested_addr", addr), ("token", token.as_str())],
        );

        // Parse the requested address
        let requested_addr: SocketAddr = match addr.parse() {
            Ok(a) => a,
            Err(e) => {
                crate::logging::error(format!(
                    "control plane failed to parse address '{}': {} - address must be numeric IP (e.g., 127.0.0.1:7938), not hostname",
                    addr, e
                ));
                return Err(anyhow::anyhow!(
                    "Invalid address '{}': {} (use numeric IP, not hostname like 'localhost')",
                    addr,
                    e
                ));
            }
        };

        // Try to bind to the requested address with retries.
        // On Windows especially, ports can remain in TIME_WAIT for a short period after
        // a process is killed, so we retry a few times before falling back to port 0.
        const MAX_BIND_RETRIES: u32 = 5;
        const RETRY_DELAY_MS: u64 = 200;

        let mut last_error = None;
        for attempt in 1..=MAX_BIND_RETRIES {
            match tokio::net::TcpListener::bind(requested_addr).await {
                Ok(listener) => {
                    let bound = listener.local_addr()?;
                    crate::logging::info_kv(
                        "control plane bound to requested port",
                        &[
                            ("addr", &bound.to_string()),
                            ("attempt", &attempt.to_string()),
                        ],
                    );
                    return Self::finish_start(
                        listener,
                        bound,
                        token,
                        http_stats,
                        shutdown,
                        data_dir.clone(),
                        owner_email.clone(),
                        server_url.clone(),
                        api.clone(),
                    )
                    .await;
                }
                Err(e) => {
                    last_error = Some(e);
                    if attempt < MAX_BIND_RETRIES {
                        crate::logging::info_kv(
                            "control plane bind attempt failed, retrying",
                            &[
                                ("requested_addr", &requested_addr.to_string()),
                                ("attempt", &attempt.to_string()),
                                ("max_attempts", &MAX_BIND_RETRIES.to_string()),
                            ],
                        );
                        tokio::time::sleep(std::time::Duration::from_millis(RETRY_DELAY_MS)).await;
                    }
                }
            }
        }

        let e = last_error.unwrap();
        crate::logging::info_kv(
            "control plane requested port unavailable after retries, trying fallback",
            &[
                ("requested_addr", &requested_addr.to_string()),
                ("error", &e.to_string()),
            ],
        );

        // Try port 0 (OS assigns random available port)
        let fallback_addr: SocketAddr = format!("{}:0", requested_addr.ip()).parse()?;
        match tokio::net::TcpListener::bind(fallback_addr).await {
            Ok(listener) => {
                let bound = listener.local_addr()?;
                crate::logging::info_kv(
                    "control plane bound to fallback port",
                    &[
                        ("original_request", &requested_addr.to_string()),
                        ("actual_addr", &bound.to_string()),
                    ],
                );
                Self::finish_start(
                    listener,
                    bound,
                    token,
                    http_stats,
                    shutdown,
                    data_dir,
                    owner_email,
                    server_url,
                    api,
                )
                .await
            }
            Err(fallback_err) => {
                crate::logging::error(format!(
                    "control plane FAILED to bind - both requested port {} and fallback failed: original={}, fallback={}",
                    requested_addr, e, fallback_err
                ));
                Err(anyhow::anyhow!(
                    "Failed to bind control plane: requested {} failed ({}), fallback to port 0 also failed ({})",
                    requested_addr, e, fallback_err
                ))
            }
        }
    }

    /// Helper to complete the control plane startup once we have a bound listener.
    #[allow(clippy::too_many_arguments)]
    async fn finish_start(
        listener: tokio::net::TcpListener,
        bound_addr: SocketAddr,
        token: String,
        http_stats: Arc<HttpStats>,
        shutdown: Option<Arc<Notify>>,
        data_dir: PathBuf,
        owner_email: String,
        server_url: String,
        api: Option<ApiClient>,
    ) -> anyhow::Result<ControlPlaneStartResult> {
        let latency_stats = Arc::new(LatencyStats::new(server_url));
        let state = Arc::new(ControlState {
            token,
            uploads: Mutex::new(HashMap::new()),
            sync_status: Mutex::new(HashMap::new()),
            sync_events: broadcast::channel(1024).0,
            sync_now: Notify::new(),
            http_stats,
            latency_stats,
            data_dir,
            owner_email,
            api,
        });

        // Create authenticated routes (require Bearer token)
        let authenticated_routes = Router::new()
            .route("/v1/sync/status", get(sync_status))
            .route("/v1/sync/status/file", get(sync_status_file))
            .route("/v1/sync/queue", get(sync_queue))
            .route("/v1/sync/conflicts", get(sync_conflicts))
            .route("/v1/sync/now", post(sync_now))
            .route("/v1/sync/refresh", post(sync_refresh))
            .route("/v1/sync/cleanup", post(sync_cleanup))
            .route("/v1/uploads/", get(list_uploads))
            .route("/v1/uploads/:id", get(get_upload).delete(delete_upload))
            .route("/v1/uploads/:id/pause", post(pause_upload))
            .route("/v1/uploads/:id/resume", post(resume_upload))
            .route("/v1/uploads/:id/restart", post(restart_upload))
            .route(
                "/v1/subscriptions",
                get(subscriptions_get).put(subscriptions_put),
            )
            .route("/v1/subscriptions/effective", get(subscriptions_effective))
            .route("/v1/subscriptions/rules", post(subscriptions_rules_post))
            .route(
                "/v1/subscriptions/rules",
                delete(subscriptions_rules_delete),
            )
            .route("/v1/discovery/files", get(discovery_files))
            .route("/v1/publications", get(publications))
            .with_state(state.clone())
            .layer(axum::middleware::from_fn_with_state(
                state.clone(),
                auth_middleware,
            ));

        // Health/status endpoint is public (no auth required)
        // SSE events use query param auth (EventSource can't send headers)
        // Latency stats are public for easy polling from UI
        let app = Router::new()
            .route("/v1/status", get(status))
            .route("/v1/sync/events", get(sync_events_with_query_auth))
            .route("/v1/stats/latency", get(server_latency))
            .with_state(state.clone())
            .merge(authenticated_routes);

        // Spawn the server
        let shutdown_clone = shutdown.clone();
        tokio::spawn(async move {
            if let Some(shutdown) = shutdown_clone {
                let result = axum::serve(listener, app)
                    .with_graceful_shutdown(async move {
                        shutdown.notified().await;
                    })
                    .await;
                if let Err(e) = result {
                    crate::logging::error(format!("control plane server error: {}", e));
                }
            } else {
                let result = axum::serve(listener, app).await;
                if let Err(e) = result {
                    crate::logging::error(format!("control plane server error: {}", e));
                }
            }
            crate::logging::info("control plane server stopped");
        });

        crate::logging::info_kv(
            "control plane started successfully",
            &[("bound_addr", &bound_addr.to_string())],
        );

        Ok(ControlPlaneStartResult {
            control_plane: ControlPlane { state, bound_addr },
            bound_addr,
        })
    }

    /// Synchronous wrapper for start_async that blocks until binding completes.
    /// This ensures the port is actually bound before returning.
    pub fn start(
        addr: &str,
        token: Option<String>,
        http_stats: Arc<HttpStats>,
        shutdown: Option<Arc<Notify>>,
        data_dir: PathBuf,
        owner_email: String,
        server_url: String,
        api: Option<ApiClient>,
    ) -> anyhow::Result<ControlPlaneStartResult> {
        // We need a runtime to run the async code. Since we're already in a tokio context
        // (called from daemon), we can use block_in_place to avoid nested runtime issues.
        tokio::task::block_in_place(|| {
            tokio::runtime::Handle::current().block_on(Self::start_async(
                addr,
                token,
                http_stats,
                shutdown,
                data_dir,
                owner_email,
                server_url,
                api,
            ))
        })
    }

    /// Get the actual address the control plane is bound to.
    pub fn bound_addr(&self) -> SocketAddr {
        self.bound_addr
    }

    pub async fn wait_sync_now(&self) {
        self.state.sync_now.notified().await;
    }

    pub fn seed_completed(&self, keys: impl IntoIterator<Item = String>) {
        let mut sync = self.state.sync_status.lock().unwrap();
        let now = Utc::now();
        for key in keys {
            sync.entry(key.clone()).or_insert(SyncFileStatus {
                path: key,
                state: "completed".to_string(),
                conflict_state: "none".to_string(),
                progress: 100.0,
                error: String::new(),
                error_count: 0,
                updated_at: now,
            });
        }
    }

    pub fn set_sync_syncing(&self, key: &str, progress: f64) {
        self.upsert_sync_status(key, "syncing", progress, None, None, false);
    }

    pub fn set_sync_completed(&self, key: &str) {
        self.upsert_sync_status(key, "completed", 100.0, None, None, false);
    }

    pub fn set_sync_conflicted(&self, key: &str) {
        self.upsert_sync_status(key, "completed", 100.0, Some("conflicted"), None, false);
    }

    pub fn set_sync_rejected(&self, key: &str) {
        self.upsert_sync_status(key, "completed", 100.0, Some("rejected"), None, false);
    }

    pub fn set_sync_error(&self, key: &str, err: &str) {
        self.upsert_sync_status(key, "error", 0.0, None, Some(err), true);
    }

    fn upsert_sync_status(
        &self,
        key: &str,
        state: &str,
        progress: f64,
        conflict_state: Option<&str>,
        error: Option<&str>,
        inc_error_count: bool,
    ) {
        let mut sync = self.state.sync_status.lock().unwrap();
        let now = Utc::now();
        let entry = sync.entry(key.to_string()).or_insert(SyncFileStatus {
            path: key.to_string(),
            state: "pending".to_string(),
            conflict_state: "none".to_string(),
            progress: 0.0,
            error: String::new(),
            error_count: 0,
            updated_at: now,
        });
        entry.state = state.to_string();
        entry.progress = progress.clamp(0.0, 100.0);
        if let Some(cs) = conflict_state {
            entry.conflict_state = cs.to_string();
        }
        if let Some(e) = error {
            entry.error = e.to_string();
            if inc_error_count {
                entry.error_count += 1;
            }
        } else {
            entry.error.clear();
        }
        entry.updated_at = now;

        let _ = self.state.sync_events.send(entry.clone());
    }

    pub fn upsert_upload(
        &self,
        key: String,
        local_path: Option<String>,
        size: i64,
        part_size: Option<i64>,
        part_count: Option<i64>,
    ) -> String {
        let mut uploads = self.state.uploads.lock().unwrap();
        let now = Utc::now();

        // Prefer reusing an existing non-completed entry for this key.
        for (id, u) in uploads.iter_mut() {
            if u.key == key && u.state != "completed" {
                u.size = size;
                if local_path.is_some() {
                    u.local_path = local_path.clone();
                }
                u.part_size = part_size;
                u.part_count = part_count;
                u.updated_at = now;
                self.set_sync_syncing(&u.key, u.progress);
                return id.clone();
            }
        }

        let id = Uuid::new_v4().to_string();
        let key_clone = key.clone();
        uploads.insert(
            id.clone(),
            UploadEntry {
                id: id.clone(),
                key,
                state: "uploading".to_string(),
                local_path,
                size,
                uploaded_bytes: 0,
                part_size,
                part_count,
                completed_parts: Vec::new(),
                progress: 0.0,
                error: None,
                started_at: now,
                updated_at: now,
            },
        );
        self.set_sync_syncing(&key_clone, 0.0);
        id
    }

    pub fn update_upload_progress(&self, id: &str, uploaded_bytes: i64, completed_parts: Vec<i64>) {
        let mut uploads = self.state.uploads.lock().unwrap();
        if let Some(u) = uploads.get_mut(id) {
            u.uploaded_bytes = uploaded_bytes.max(0);
            u.completed_parts = completed_parts;
            if u.size > 0 {
                u.progress = (u.uploaded_bytes as f64) * 100.0 / (u.size as f64);
                if u.progress > 100.0 {
                    u.progress = 100.0;
                }
            }
            u.updated_at = Utc::now();
            self.set_sync_syncing(&u.key, u.progress);
        }
    }

    pub fn set_upload_state(&self, id: &str, state: String, error: Option<String>) {
        let mut uploads = self.state.uploads.lock().unwrap();
        if let Some(u) = uploads.get_mut(id) {
            u.state = state;
            u.error = error;
            u.updated_at = Utc::now();
            let sync_state = match u.state.as_str() {
                "uploading" => "syncing",
                "paused" => "pending",
                "completed" => "completed",
                "error" => "error",
                _ => "pending",
            };
            if let Some(err) = u.error.as_deref() {
                self.set_sync_error(&u.key, err);
            } else {
                self.upsert_sync_status(&u.key, sync_state, u.progress, None, None, false);
            }
        }
    }

    pub fn set_upload_error(&self, id: &str, err: String) {
        self.set_upload_state(id, "error".to_string(), Some(err));
    }

    pub fn set_upload_completed(&self, id: &str, uploaded_bytes: i64) {
        let mut uploads = self.state.uploads.lock().unwrap();
        if let Some(u) = uploads.get_mut(id) {
            u.state = "completed".to_string();
            u.error = None;
            u.uploaded_bytes = uploaded_bytes.max(0);
            u.progress = 100.0;
            u.updated_at = Utc::now();
        }
        let key = uploads.get(id).map(|u| u.key.clone()).unwrap_or_default();
        if !key.is_empty() {
            self.set_sync_completed(&key);
            uploads.remove(id);
        }
    }

    pub fn get_upload_state(&self, id: &str) -> String {
        let uploads = self.state.uploads.lock().unwrap();
        uploads.get(id).map(|u| u.state.clone()).unwrap_or_default()
    }

    pub fn record_latency(&self, latency_ms: u64) {
        self.state.latency_stats.record(latency_ms);
    }
}

async fn auth_middleware(
    State(state): State<Arc<ControlState>>,
    headers: HeaderMap,
    req: axum::http::Request<axum::body::Body>,
    next: axum::middleware::Next,
) -> impl IntoResponse {
    let expected = format!("Bearer {}", state.token);
    if let Some(value) = headers.get(axum::http::header::AUTHORIZATION) {
        if value.to_str().map(|v| v == expected).unwrap_or(false) {
            return next.run(req).await;
        }
    }
    (StatusCode::UNAUTHORIZED, "unauthorized").into_response()
}

#[derive(Serialize)]
struct StatusResponse {
    status: String,
    #[serde(rename = "ts")]
    timestamp: String,
    version: String,
    revision: String,
    #[serde(rename = "buildDate")]
    build_date: String,
    runtime: RuntimeInfo,
}

#[derive(Serialize)]
struct RuntimeInfo {
    http: HttpInfo,
}

#[derive(Serialize)]
struct HttpInfo {
    bytes_sent_total: i64,
    bytes_recv_total: i64,
    #[serde(skip_serializing_if = "String::is_empty")]
    last_error: String,
}

async fn status(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let snap = state.http_stats.snapshot();
    Json(StatusResponse {
        status: "ok".to_string(),
        timestamp: Utc::now().to_rfc3339(),
        version: env!("CARGO_PKG_VERSION").to_string(),
        revision: String::new(),
        build_date: String::new(),
        runtime: RuntimeInfo {
            http: HttpInfo {
                bytes_sent_total: snap.bytes_sent_total,
                bytes_recv_total: snap.bytes_recv_total,
                last_error: snap.last_error,
            },
        },
    })
}

async fn server_latency(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    Json(state.latency_stats.snapshot())
}

async fn sync_status(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let sync = state.sync_status.lock().unwrap();
    let mut files: Vec<SyncFileStatus> = sync.values().cloned().collect();
    files.sort_by(|a, b| a.path.cmp(&b.path));
    let mut summary = SyncSummary {
        pending: 0,
        syncing: 0,
        completed: 0,
        error: 0,
    };
    for f in &files {
        match f.state.as_str() {
            "pending" => summary.pending += 1,
            "syncing" => summary.syncing += 1,
            "completed" => summary.completed += 1,
            "error" => summary.error += 1,
            _ => summary.pending += 1,
        }
    }
    Json(SyncStatusResponse { files, summary })
}

async fn sync_events(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let rx = state.sync_events.subscribe();
    let stream = unfold(rx, |mut rx| async move {
        loop {
            match rx.recv().await {
                Ok(status) => {
                    let data = serde_json::to_string(&status).unwrap_or_else(|_| "{}".to_string());
                    let ev = Event::default().event("sync").data(data);
                    return Some((Ok::<_, std::convert::Infallible>(ev), rx));
                }
                Err(tokio::sync::broadcast::error::RecvError::Closed) => return None,
                Err(tokio::sync::broadcast::error::RecvError::Lagged(_)) => continue,
            }
        }
    });
    Sse::new(stream).keep_alive(KeepAlive::new().interval(std::time::Duration::from_secs(15)))
}

#[derive(Deserialize)]
struct SseTokenQuery {
    token: Option<String>,
}

async fn sync_events_with_query_auth(
    State(state): State<Arc<ControlState>>,
    Query(q): Query<SseTokenQuery>,
) -> axum::response::Response {
    // Validate token from query param (EventSource can't send Authorization headers)
    match q.token {
        Some(t) if t == state.token => sync_events(State(state)).await.into_response(),
        _ => (StatusCode::UNAUTHORIZED, "unauthorized").into_response(),
    }
}

#[derive(Deserialize)]
struct SyncStatusFileQuery {
    path: String,
}

async fn sync_status_file(
    State(state): State<Arc<ControlState>>,
    Query(q): Query<SyncStatusFileQuery>,
) -> impl IntoResponse {
    if q.path.trim().is_empty() {
        return StatusCode::BAD_REQUEST.into_response();
    }
    let sync = state.sync_status.lock().unwrap();
    if let Some(s) = sync.get(&q.path) {
        return (StatusCode::OK, Json(s.clone())).into_response();
    }
    StatusCode::NOT_FOUND.into_response()
}

async fn sync_now(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    state.sync_now.notify_one();
    (
        StatusCode::OK,
        Json(serde_json::json!({ "status": "sync triggered" })),
    )
}

async fn sync_refresh(
    State(state): State<Arc<ControlState>>,
    Query(q): Query<std::collections::HashMap<String, String>>,
) -> impl IntoResponse {
    state.sync_now.notify_one();
    let mut resp = serde_json::json!({ "status": "sync triggered" });
    if let Some(path) = q.get("path") {
        resp["path"] = serde_json::Value::String(path.clone());
    }
    (StatusCode::OK, Json(resp))
}

async fn sync_queue(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let sync = state.sync_status.lock().unwrap();
    let mut files: Vec<SyncFileStatus> = sync
        .values()
        .filter(|s| s.state == "pending" || s.state == "syncing")
        .cloned()
        .collect();
    files.sort_by(|a, b| a.path.cmp(&b.path));
    Json(SyncQueueResponse { files })
}

async fn sync_conflicts(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let datasites_dir = state.data_dir.join("datasites");
    let (conflicts, rejected) = list_marked_files(&datasites_dir);
    Json(ConflictsResponse {
        summary: ConflictsSummary {
            conflict_count: conflicts.len(),
            rejected_count: rejected.len(),
        },
        conflicts,
        rejected,
    })
}

async fn sync_cleanup(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let datasites_dir = state.data_dir.join("datasites");
    let (cleaned, errors) = cleanup_orphaned_temp_files(&datasites_dir);
    Json(CleanupResponse {
        cleaned_count: cleaned,
        errors,
    })
}

fn is_temp_file(name: &str) -> bool {
    // Match patterns: .*.tmp-* or *.syft.tmp.*
    name.contains(".tmp-") || (name.contains(".syft.tmp.") && !name.ends_with(".syft.tmp."))
}

fn cleanup_orphaned_temp_files(datasites_dir: &std::path::Path) -> (usize, Vec<String>) {
    let mut cleaned = 0;
    let mut errors = Vec::new();

    for entry in WalkDir::new(datasites_dir)
        .into_iter()
        .filter_map(|e| e.ok())
    {
        if !entry.file_type().is_file() {
            continue;
        }
        let name = entry.file_name().to_string_lossy();
        if is_temp_file(&name) {
            match fs::remove_file(entry.path()) {
                Ok(()) => {
                    crate::logging::info(format!(
                        "cleaned up orphaned temp file: {}",
                        entry.path().display()
                    ));
                    cleaned += 1;
                }
                Err(e) => {
                    errors.push(format!(
                        "failed to remove {}: {}",
                        entry.path().display(),
                        e
                    ));
                }
            }
        }
    }

    (cleaned, errors)
}

fn is_marked_path(path: &str) -> bool {
    path.contains(".conflict")
        || path.contains(".rejected")
        || path.contains("syftrejected")
        || path.contains("syftconflict")
}

fn get_unmarked_path(path: &str) -> String {
    use regex::Regex;
    // Remove .conflict and .rejected markers with optional timestamps
    let re = Regex::new(r"\.(conflict|rejected)(\.\d{14})?").unwrap();
    let result = re.replace_all(path, "");
    // Also handle legacy markers
    let legacy_re = Regex::new(r"\.(syftconflict|syftrejected)(\.\d{14})?").unwrap();
    legacy_re.replace_all(&result, "").to_string()
}

fn list_marked_files(
    datasites_dir: &std::path::Path,
) -> (Vec<MarkedFileInfo>, Vec<MarkedFileInfo>) {
    let mut conflicts = Vec::new();
    let mut rejected = Vec::new();

    let walker = WalkDir::new(datasites_dir);
    for entry in walker.into_iter().filter_map(|e| e.ok()) {
        if !entry.file_type().is_file() {
            continue;
        }

        let rel_path = entry
            .path()
            .strip_prefix(datasites_dir)
            .map(|p| p.to_string_lossy().replace('\\', "/"))
            .unwrap_or_default();

        if !is_marked_path(&rel_path) {
            continue;
        }

        let metadata = match entry.metadata() {
            Ok(m) => m,
            Err(_) => continue,
        };

        let mod_time = metadata
            .modified()
            .ok()
            .and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok())
            .map(|d| DateTime::<Utc>::from_timestamp(d.as_secs() as i64, 0).unwrap_or_default())
            .unwrap_or_default();

        let original_path = get_unmarked_path(&rel_path);

        let info = MarkedFileInfo {
            path: rel_path.clone(),
            original_path,
            size: metadata.len(),
            mod_time,
            marker_type: if rel_path.contains(".conflict") || rel_path.contains("syftconflict") {
                "conflict".to_string()
            } else {
                "rejected".to_string()
            },
        };

        if info.marker_type == "conflict" {
            conflicts.push(info);
        } else {
            rejected.push(info);
        }
    }

    (conflicts, rejected)
}

async fn subscriptions_get(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let path = subscriptions::config_path(&state.data_dir);
    let cfg = subscriptions::load(&path).unwrap_or_else(|err| {
        crate::logging::error(format!(
            "subscriptions load error path={} err={:?}",
            path.display(),
            err
        ));
        subscriptions::default_config()
    });
    Json(SubscriptionsResponse {
        path: path.display().to_string(),
        config: cfg,
    })
}

async fn subscriptions_put(
    State(state): State<Arc<ControlState>>,
    Json(req): Json<SubscriptionsUpdateRequest>,
) -> axum::response::Response {
    let path = subscriptions::config_path(&state.data_dir);
    if let Err(err) = subscriptions::save(&path, &req.config) {
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({ "error": err.to_string() })),
        )
            .into_response();
    }
    state.sync_now.notify_one();
    Json(SubscriptionsResponse {
        path: path.display().to_string(),
        config: req.config,
    })
    .into_response()
}

async fn subscriptions_effective(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let Some(api) = state.api.clone() else {
        return StatusCode::SERVICE_UNAVAILABLE.into_response();
    };

    let path = subscriptions::config_path(&state.data_dir);
    let cfg = subscriptions::load(&path).unwrap_or_else(|err| {
        crate::logging::error(format!(
            "subscriptions load error path={} err={:?}",
            path.display(),
            err
        ));
        subscriptions::default_config()
    });

    let view = match api.datasite_view().await {
        Ok(v) => v,
        Err(err) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": err.to_string() })),
            )
                .into_response();
        }
    };

    let mut files = Vec::new();
    for file in view.files {
        if file.key.ends_with("/syft.pub.yaml") || file.key == "syft.pub.yaml" {
            continue;
        }
        if subscriptions::is_sub_file(&file.key) {
            continue;
        }
        let action = subscriptions::action_for_path(&cfg, &state.owner_email, &file.key);
        files.push(EffectiveFile {
            path: file.key,
            action: format!("{:?}", action).to_lowercase(),
            allowed: action == subscriptions::Action::Allow,
        });
    }

    Json(EffectiveResponse { files }).into_response()
}

async fn subscriptions_rules_post(
    State(state): State<Arc<ControlState>>,
    Json(req): Json<SubscriptionsRuleRequest>,
) -> axum::response::Response {
    if req.rule.path.trim().is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": "rule.path is required" })),
        )
            .into_response();
    }

    let path = subscriptions::config_path(&state.data_dir);
    let mut cfg = subscriptions::load(&path).unwrap_or_else(|err| {
        crate::logging::error(format!(
            "subscriptions load error path={} err={:?}",
            path.display(),
            err
        ));
        subscriptions::default_config()
    });

    let mut updated = false;
    for rule in &mut cfg.rules {
        if rule.datasite == req.rule.datasite && rule.path == req.rule.path {
            rule.action = req.rule.action.clone();
            updated = true;
            break;
        }
    }
    if !updated {
        cfg.rules.push(req.rule);
    }

    if let Err(err) = subscriptions::save(&path, &cfg) {
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({ "error": err.to_string() })),
        )
            .into_response();
    }
    state.sync_now.notify_one();

    Json(SubscriptionsResponse {
        path: path.display().to_string(),
        config: cfg,
    })
    .into_response()
}

async fn subscriptions_rules_delete(
    State(state): State<Arc<ControlState>>,
    Query(q): Query<SubscriptionsRuleDeleteQuery>,
) -> axum::response::Response {
    if q.path.trim().is_empty() {
        return (
            StatusCode::BAD_REQUEST,
            Json(serde_json::json!({ "error": "path is required" })),
        )
            .into_response();
    }

    let action_filter = match q.action.as_deref() {
        None => None,
        Some(raw) => match parse_action(raw) {
            Some(action) => Some(action),
            None => {
                return (
                    StatusCode::BAD_REQUEST,
                    Json(serde_json::json!({ "error": "invalid action" })),
                )
                    .into_response()
            }
        },
    };

    let path = subscriptions::config_path(&state.data_dir);
    let mut cfg = subscriptions::load(&path).unwrap_or_else(|err| {
        crate::logging::error(format!(
            "subscriptions load error path={} err={:?}",
            path.display(),
            err
        ));
        subscriptions::default_config()
    });

    cfg.rules.retain(|rule| {
        if rule.path != q.path {
            return true;
        }
        if let Some(ref datasite) = q.datasite {
            if rule.datasite.as_deref() != Some(datasite.as_str()) {
                return true;
            }
        }
        if let Some(action) = action_filter.as_ref() {
            return &rule.action != action;
        }
        false
    });

    if let Err(err) = subscriptions::save(&path, &cfg) {
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(serde_json::json!({ "error": err.to_string() })),
        )
            .into_response();
    }
    state.sync_now.notify_one();

    Json(SubscriptionsResponse {
        path: path.display().to_string(),
        config: cfg,
    })
    .into_response()
}

async fn discovery_files(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let Some(api) = state.api.clone() else {
        return StatusCode::SERVICE_UNAVAILABLE.into_response();
    };

    let path = subscriptions::config_path(&state.data_dir);
    let cfg = subscriptions::load(&path).unwrap_or_else(|err| {
        crate::logging::error(format!(
            "subscriptions load error path={} err={:?}",
            path.display(),
            err
        ));
        subscriptions::default_config()
    });

    let view = match api.datasite_view().await {
        Ok(v) => v,
        Err(err) => {
            return (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": err.to_string() })),
            )
                .into_response();
        }
    };

    let mut files = Vec::new();
    for file in view.files {
        if file.key.ends_with("/syft.pub.yaml") || file.key == "syft.pub.yaml" {
            continue;
        }
        if subscriptions::is_sub_file(&file.key) {
            continue;
        }
        let action = subscriptions::action_for_path(&cfg, &state.owner_email, &file.key);
        if action == subscriptions::Action::Allow {
            continue;
        }
        files.push(DiscoveryFile {
            path: file.key,
            etag: file.etag,
            size: file.size,
            last_modified: file.last_modified,
            action: format!("{:?}", action).to_lowercase(),
        });
    }

    Json(DiscoveryResponse { files }).into_response()
}

async fn publications(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let root = state.data_dir.join("datasites").join(&state.owner_email);
    let base = state.data_dir.join("datasites");
    let mut files = Vec::new();

    if root.exists() {
        for entry in WalkDir::new(&root).into_iter().filter_map(|e| e.ok()) {
            if !entry.file_type().is_file() {
                continue;
            }
            let name = entry.file_name().to_string_lossy();
            if name != "syft.pub.yaml" {
                continue;
            }
            let path = entry.path();
            let rel = path
                .strip_prefix(&base)
                .unwrap_or(path)
                .to_string_lossy()
                .replace('\\', "/");
            let content = match std::fs::read_to_string(path) {
                Ok(c) => c,
                Err(_) => continue,
            };
            files.push(PublicationEntry { path: rel, content });
        }
    }

    Json(PublicationsResponse { files }).into_response()
}

async fn list_uploads(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let uploads = state.uploads.lock().unwrap();
    let mut list: Vec<UploadEntry> = uploads.values().cloned().collect();
    list.sort_by(|a, b| a.started_at.cmp(&b.started_at));
    Json(UploadListResponse { uploads: list })
}

async fn get_upload(
    State(state): State<Arc<ControlState>>,
    AxumPath(id): AxumPath<String>,
) -> impl IntoResponse {
    let uploads = state.uploads.lock().unwrap();
    if let Some(u) = uploads.get(&id) {
        return (StatusCode::OK, Json(u.clone())).into_response();
    }
    StatusCode::NOT_FOUND.into_response()
}

async fn delete_upload(
    State(state): State<Arc<ControlState>>,
    AxumPath(id): AxumPath<String>,
) -> impl IntoResponse {
    let mut uploads = state.uploads.lock().unwrap();
    if uploads.remove(&id).is_some() {
        return (
            StatusCode::OK,
            Json(serde_json::json!({ "status": "cancelled" })),
        )
            .into_response();
    }
    StatusCode::NOT_FOUND.into_response()
}

async fn pause_upload(
    State(state): State<Arc<ControlState>>,
    AxumPath(id): AxumPath<String>,
) -> impl IntoResponse {
    let mut uploads = state.uploads.lock().unwrap();
    if let Some(u) = uploads.get_mut(&id) {
        u.state = "paused".to_string();
        u.updated_at = Utc::now();
        let mut sync = state.sync_status.lock().unwrap();
        if let Some(s) = sync.get_mut(&u.key) {
            s.state = "pending".to_string();
            s.updated_at = Utc::now();
            let _ = state.sync_events.send(s.clone());
        }
        return (
            StatusCode::OK,
            Json(serde_json::json!({ "status": "paused" })),
        )
            .into_response();
    }
    StatusCode::NOT_FOUND.into_response()
}

async fn resume_upload(
    State(state): State<Arc<ControlState>>,
    AxumPath(id): AxumPath<String>,
) -> impl IntoResponse {
    let mut uploads = state.uploads.lock().unwrap();
    if let Some(u) = uploads.get_mut(&id) {
        u.state = "uploading".to_string();
        u.updated_at = Utc::now();
        let mut sync = state.sync_status.lock().unwrap();
        if let Some(s) = sync.get_mut(&u.key) {
            s.state = "syncing".to_string();
            s.updated_at = Utc::now();
            let _ = state.sync_events.send(s.clone());
        }
        return (
            StatusCode::OK,
            Json(serde_json::json!({ "status": "resumed" })),
        )
            .into_response();
    }
    StatusCode::NOT_FOUND.into_response()
}

async fn restart_upload(
    State(state): State<Arc<ControlState>>,
    AxumPath(id): AxumPath<String>,
) -> impl IntoResponse {
    let mut uploads = state.uploads.lock().unwrap();
    if let Some(u) = uploads.get_mut(&id) {
        u.state = "restarted".to_string();
        u.progress = 0.0;
        u.uploaded_bytes = 0;
        u.updated_at = Utc::now();
        let mut sync = state.sync_status.lock().unwrap();
        let status = SyncFileStatus {
            path: u.key.clone(),
            state: "pending".to_string(),
            conflict_state: "none".to_string(),
            progress: 0.0,
            error: String::new(),
            error_count: 0,
            updated_at: Utc::now(),
        };
        sync.insert(u.key.clone(), status.clone());
        let _ = state.sync_events.send(status);
        return (
            StatusCode::OK,
            Json(serde_json::json!({ "status": "restarted" })),
        )
            .into_response();
    }
    StatusCode::NOT_FOUND.into_response()
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::to_bytes;
    use std::time::SystemTime;

    fn make_temp_dir() -> PathBuf {
        let mut root = std::env::temp_dir();
        let nanos = SystemTime::now()
            .duration_since(SystemTime::UNIX_EPOCH)
            .unwrap()
            .as_nanos();
        root.push(format!("syftbox-control-test-{nanos}"));
        fs::create_dir_all(&root).unwrap();
        root
    }

    #[tokio::test]
    async fn uploads_are_listed_and_sync_status_completed() {
        let stats = Arc::new(HttpStats::default());
        let latency_stats = Arc::new(crate::telemetry::LatencyStats::new(
            "https://test.example.com".to_string(),
        ));
        let (tx, _) = broadcast::channel(16);
        let tmp_dir = make_temp_dir();
        let state = Arc::new(ControlState {
            token: "secret".into(),
            uploads: Mutex::new(HashMap::new()),
            sync_status: Mutex::new(HashMap::new()),
            sync_events: tx,
            sync_now: Notify::new(),
            http_stats: stats,
            latency_stats,
            data_dir: tmp_dir.clone(),
            owner_email: "test@example.com".into(),
            api: None,
        });
        let cp = ControlPlane {
            state: state.clone(),
            bound_addr: "127.0.0.1:7938".parse().unwrap(),
        };
        let id = cp.upsert_upload(
            "alice@example.com/public/demo.bin".into(),
            None,
            1024,
            None,
            None,
        );
        cp.set_upload_completed(&id, 1024);
        let list_resp = list_uploads(State(state.clone())).await;
        let list_bytes = to_bytes(list_resp.into_response().into_body(), usize::MAX)
            .await
            .unwrap();
        let list: UploadListResponse = serde_json::from_slice(&list_bytes).unwrap();
        assert_eq!(list.uploads.len(), 0);

        let status_resp = sync_status(State(state.clone())).await;
        let status_bytes = to_bytes(status_resp.into_response().into_body(), usize::MAX)
            .await
            .unwrap();
        let status: SyncStatusResponse = serde_json::from_slice(&status_bytes).unwrap();
        assert_eq!(status.summary.completed, 1);
        assert_eq!(status.files.len(), 1);
        assert_eq!(status.files[0].state, "completed");

        let _ = fs::remove_dir_all(&tmp_dir);
    }

    #[test]
    fn test_is_temp_file() {
        // Rust download temp files: .filename.tmp-uuid
        assert!(is_temp_file(".syft.pub.yaml.tmp-8cd89f7b-1234"));
        assert!(is_temp_file(".config.json.tmp-abcdef12"));
        assert!(is_temp_file("data.tmp-12345678"));

        // Go atomic write temp files: *.syft.tmp.*
        assert!(is_temp_file("file.syft.tmp.123456"));

        // Regular files should NOT match
        assert!(!is_temp_file("data.txt"));
        assert!(!is_temp_file("syft.pub.yaml"));
        assert!(!is_temp_file("file.rejected.txt"));
        assert!(!is_temp_file("file.conflict.txt"));
    }

    #[test]
    fn test_is_marked_path() {
        // Conflict files
        assert!(is_marked_path("file.conflict.txt"));
        assert!(is_marked_path("data.conflict.20250101120000.json"));

        // Rejected files
        assert!(is_marked_path("file.rejected.txt"));
        assert!(is_marked_path("data.rejected.20250101120000.json"));

        // Legacy markers
        assert!(is_marked_path("file.syftrejected.txt"));
        assert!(is_marked_path("file.syftconflict.txt"));

        // Regular files should NOT match
        assert!(!is_marked_path("data.txt"));
        assert!(!is_marked_path("syft.pub.yaml"));
    }

    #[test]
    fn test_get_unmarked_path() {
        assert_eq!(get_unmarked_path("file.conflict.txt"), "file.txt");
        assert_eq!(get_unmarked_path("file.rejected.txt"), "file.txt");
        assert_eq!(
            get_unmarked_path("file.conflict.20250101120000.txt"),
            "file.txt"
        );
        assert_eq!(
            get_unmarked_path("data.rejected.20250101120000.json"),
            "data.json"
        );
    }

    #[test]
    fn test_cleanup_orphaned_temp_files() {
        let root = make_temp_dir();
        let datasites = root.join("datasites");
        let alice_dir = datasites.join("alice@example.com/public");
        fs::create_dir_all(&alice_dir).unwrap();

        // Create temp files that should be cleaned up
        let temp1 = alice_dir.join(".syft.pub.yaml.tmp-8cd89f7b");
        let temp2 = alice_dir.join("data.tmp-12345678");
        let temp3 = alice_dir.join("config.syft.tmp.999999");
        fs::write(&temp1, b"temp1").unwrap();
        fs::write(&temp2, b"temp2").unwrap();
        fs::write(&temp3, b"temp3").unwrap();

        // Create regular files that should NOT be cleaned up
        let regular = alice_dir.join("data.txt");
        let acl = alice_dir.join("syft.pub.yaml");
        fs::write(&regular, b"regular").unwrap();
        fs::write(&acl, b"acl").unwrap();

        let (cleaned, errors) = cleanup_orphaned_temp_files(&datasites);

        assert!(errors.is_empty(), "cleanup should not have errors");
        assert_eq!(cleaned, 3, "should have cleaned up 3 temp files");

        // Verify temp files are gone
        assert!(!temp1.exists(), "temp file 1 should be removed");
        assert!(!temp2.exists(), "temp file 2 should be removed");
        assert!(!temp3.exists(), "temp file 3 should be removed");

        // Verify regular files still exist
        assert!(regular.exists(), "regular file should still exist");
        assert!(acl.exists(), "ACL file should still exist");

        let _ = fs::remove_dir_all(&root);
    }

    #[test]
    fn test_list_marked_files() {
        let root = make_temp_dir();
        let alice_dir = root.join("alice@example.com/public");
        let bob_dir = root.join("bob@example.com/shared");
        fs::create_dir_all(&alice_dir).unwrap();
        fs::create_dir_all(&bob_dir).unwrap();

        // Create conflict files
        fs::write(alice_dir.join("data.conflict.txt"), b"conflict1").unwrap();
        fs::write(bob_dir.join("config.conflict.json"), b"conflict2").unwrap();

        // Create rejected files
        fs::write(alice_dir.join("secret.rejected.txt"), b"rejected1").unwrap();
        fs::write(bob_dir.join("private.rejected.json"), b"rejected2").unwrap();

        // Create a legacy marker file
        fs::write(alice_dir.join("old.syftrejected.txt"), b"legacy").unwrap();

        // Create regular files that should NOT be listed
        fs::write(alice_dir.join("normal.txt"), b"regular").unwrap();

        let (conflicts, rejected) = list_marked_files(&root);

        assert_eq!(conflicts.len(), 2, "should find 2 conflict files");
        assert_eq!(
            rejected.len(),
            3,
            "should find 3 rejected files (including legacy)"
        );

        // Verify marker types
        for c in &conflicts {
            assert_eq!(c.marker_type, "conflict");
        }
        for r in &rejected {
            assert_eq!(r.marker_type, "rejected");
        }

        let _ = fs::remove_dir_all(&root);
    }
}
