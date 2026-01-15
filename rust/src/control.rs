use std::{collections::HashMap, net::SocketAddr, sync::Arc, sync::Mutex};

use axum::{
    extract::{Path, Query, State},
    http::{HeaderMap, StatusCode},
    response::sse::{Event, KeepAlive, Sse},
    response::IntoResponse,
    routing::{get, post},
    Json, Router,
};
use chrono::{DateTime, Utc};
use futures_util::stream::unfold;
use serde::{Deserialize, Serialize};
use tokio::sync::{broadcast, Notify};
use uuid::Uuid;

use crate::telemetry::HttpStats;

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

impl ControlPlane {
    /// Start the control plane HTTP server.
    ///
    /// This function will:
    /// 1. Try to bind to the configured address
    /// 2. If that fails (e.g., port in use), try binding to port 0 (OS assigns random available port)
    /// 3. Return the actual bound address so callers know where the server is listening
    ///
    /// All operations are logged for debugging.
    pub async fn start_async(
        addr: &str,
        token: Option<String>,
        http_stats: Arc<HttpStats>,
        shutdown: Option<Arc<Notify>>,
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

        // Try to bind to the requested address
        let (listener, bound_addr) = match tokio::net::TcpListener::bind(requested_addr).await {
            Ok(listener) => {
                let bound = listener.local_addr()?;
                crate::logging::info_kv(
                    "control plane bound to requested port",
                    &[("addr", &bound.to_string())],
                );
                (listener, bound)
            }
            Err(e) => {
                crate::logging::info_kv(
                    "control plane requested port unavailable, trying fallback",
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
                        (listener, bound)
                    }
                    Err(fallback_err) => {
                        crate::logging::error(format!(
                            "control plane FAILED to bind - both requested port {} and fallback failed: original={}, fallback={}",
                            requested_addr, e, fallback_err
                        ));
                        return Err(anyhow::anyhow!(
                            "Failed to bind control plane: requested {} failed ({}), fallback to port 0 also failed ({})",
                            requested_addr, e, fallback_err
                        ));
                    }
                }
            }
        };

        let state = Arc::new(ControlState {
            token,
            uploads: Mutex::new(HashMap::new()),
            sync_status: Mutex::new(HashMap::new()),
            sync_events: broadcast::channel(1024).0,
            sync_now: Notify::new(),
            http_stats,
        });

        // Create authenticated routes (require Bearer token)
        let authenticated_routes = Router::new()
            .route("/v1/sync/status", get(sync_status))
            .route("/v1/sync/status/file", get(sync_status_file))
            .route("/v1/sync/events", get(sync_events))
            .route("/v1/sync/now", post(sync_now))
            .route("/v1/uploads/", get(list_uploads))
            .route("/v1/uploads/:id", get(get_upload).delete(delete_upload))
            .route("/v1/uploads/:id/pause", post(pause_upload))
            .route("/v1/uploads/:id/resume", post(resume_upload))
            .route("/v1/uploads/:id/restart", post(restart_upload))
            .with_state(state.clone())
            .layer(axum::middleware::from_fn_with_state(
                state.clone(),
                auth_middleware,
            ));

        // Health/status endpoint is public (no auth required)
        // This allows health checks and probes without needing the token
        let app = Router::new()
            .route("/v1/status", get(status))
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
    ) -> anyhow::Result<ControlPlaneStartResult> {
        // We need a runtime to run the async code. Since we're already in a tokio context
        // (called from daemon), we can use block_in_place to avoid nested runtime issues.
        tokio::task::block_in_place(|| {
            tokio::runtime::Handle::current()
                .block_on(Self::start_async(addr, token, http_stats, shutdown))
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

async fn list_uploads(State(state): State<Arc<ControlState>>) -> impl IntoResponse {
    let uploads = state.uploads.lock().unwrap();
    let mut list: Vec<UploadEntry> = uploads.values().cloned().collect();
    list.sort_by(|a, b| a.started_at.cmp(&b.started_at));
    Json(UploadListResponse { uploads: list })
}

async fn get_upload(
    State(state): State<Arc<ControlState>>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    let uploads = state.uploads.lock().unwrap();
    if let Some(u) = uploads.get(&id) {
        return (StatusCode::OK, Json(u.clone())).into_response();
    }
    StatusCode::NOT_FOUND.into_response()
}

async fn delete_upload(
    State(state): State<Arc<ControlState>>,
    Path(id): Path<String>,
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
    Path(id): Path<String>,
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
    Path(id): Path<String>,
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
    Path(id): Path<String>,
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

    #[tokio::test]
    async fn uploads_are_listed_and_sync_status_completed() {
        let stats = Arc::new(HttpStats::default());
        let (tx, _) = broadcast::channel(16);
        let state = Arc::new(ControlState {
            token: "secret".into(),
            uploads: Mutex::new(HashMap::new()),
            sync_status: Mutex::new(HashMap::new()),
            sync_events: tx,
            sync_now: Notify::new(),
            http_stats: stats,
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
    }
}
