use std::{collections::HashMap, net::SocketAddr, sync::Arc, sync::Mutex};

use axum::{
    extract::{Path, State},
    http::{HeaderMap, StatusCode},
    response::IntoResponse,
    routing::{get, post},
    Json, Router,
};
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use tokio::sync::Notify;
use uuid::Uuid;

use crate::telemetry::HttpStats;

#[derive(Clone)]
pub struct ControlPlane {
    state: Arc<ControlState>,
}

struct ControlState {
    token: String,
    uploads: Mutex<HashMap<String, UploadEntry>>,
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
    #[serde(with = "chrono::serde::ts_seconds")]
    started_at: DateTime<Utc>,
    #[serde(with = "chrono::serde::ts_seconds")]
    updated_at: DateTime<Utc>,
}

#[derive(Serialize, Deserialize)]
struct SyncFileStatus {
    path: String,
    state: String,
    #[serde(rename = "conflictState", skip_serializing_if = "Option::is_none")]
    conflict_state: Option<String>,
    progress: f64,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
    #[serde(with = "chrono::serde::ts_seconds")]
    #[serde(rename = "updatedAt")]
    updated_at: DateTime<Utc>,
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
    pub fn start(
        addr: &str,
        token: Option<String>,
        http_stats: Arc<HttpStats>,
    ) -> anyhow::Result<Self> {
        let token = token.unwrap_or_else(|| Uuid::new_v4().as_simple().to_string());
        crate::logging::info_kv(
            "control plane start",
            &[("addr", addr), ("token", token.as_str())],
        );

        let state = Arc::new(ControlState {
            token,
            uploads: Mutex::new(HashMap::new()),
            sync_now: Notify::new(),
            http_stats,
        });

        let app = Router::new()
            .route("/v1/status", get(status))
            .route("/v1/sync/status", get(sync_status))
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

        let addr: SocketAddr = addr.parse()?;
        tokio::spawn(async move {
            if let Ok(listener) = tokio::net::TcpListener::bind(addr).await {
                let _ = axum::serve(listener, app).await;
            }
        });

        Ok(ControlPlane { state })
    }

    pub async fn wait_sync_now(&self) {
        self.state.sync_now.notified().await;
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
                return id.clone();
            }
        }

        let id = Uuid::new_v4().to_string();
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
        }
    }

    pub fn set_upload_state(&self, id: &str, state: String, error: Option<String>) {
        let mut uploads = self.state.uploads.lock().unwrap();
        if let Some(u) = uploads.get_mut(id) {
            u.state = state;
            u.error = error;
            u.updated_at = Utc::now();
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
    let uploads = state.uploads.lock().unwrap();
    let mut files = Vec::new();
    let mut summary = SyncSummary {
        pending: 0,
        syncing: 0,
        completed: 0,
        error: 0,
    };
    for u in uploads.values() {
        files.push(SyncFileStatus {
            path: u.key.clone(),
            state: u.state.clone(),
            conflict_state: None,
            progress: u.progress,
            error: u.error.clone(),
            updated_at: u.updated_at,
        });
        match u.state.as_str() {
            "completed" => summary.completed += 1,
            "error" => summary.error += 1,
            "uploading" => summary.syncing += 1,
            "paused" => summary.pending += 1,
            _ => summary.pending += 1,
        }
    }
    Json(SyncStatusResponse { files, summary })
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
        StatusCode::OK
    } else {
        StatusCode::NOT_FOUND
    }
}

async fn pause_upload(
    State(state): State<Arc<ControlState>>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    let mut uploads = state.uploads.lock().unwrap();
    if let Some(u) = uploads.get_mut(&id) {
        u.state = "paused".to_string();
        u.updated_at = Utc::now();
        return StatusCode::OK;
    }
    StatusCode::NOT_FOUND
}

async fn resume_upload(
    State(state): State<Arc<ControlState>>,
    Path(id): Path<String>,
) -> impl IntoResponse {
    let mut uploads = state.uploads.lock().unwrap();
    if let Some(u) = uploads.get_mut(&id) {
        u.state = "uploading".to_string();
        u.updated_at = Utc::now();
        return StatusCode::OK;
    }
    StatusCode::NOT_FOUND
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
        return StatusCode::OK;
    }
    StatusCode::NOT_FOUND
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::to_bytes;

    #[tokio::test]
    async fn uploads_are_listed_and_sync_status_completed() {
        let stats = Arc::new(HttpStats::default());
        let state = Arc::new(ControlState {
            token: "secret".into(),
            uploads: Mutex::new(HashMap::new()),
            sync_now: Notify::new(),
            http_stats: stats,
        });
        let cp = ControlPlane {
            state: state.clone(),
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
        assert_eq!(list.uploads.len(), 1);
        assert_eq!(list.uploads[0].state, "completed");
        assert_eq!(list.uploads[0].uploaded_bytes, 1024);

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
