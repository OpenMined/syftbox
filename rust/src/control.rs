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

#[derive(Clone)]
pub struct ControlPlane {
    state: Arc<ControlState>,
}

struct ControlState {
    token: String,
    uploads: Mutex<HashMap<String, UploadEntry>>,
    sync_now: Notify,
}

#[derive(Clone, Serialize, Deserialize)]
struct UploadEntry {
    id: String,
    key: String,
    state: String,
    size: i64,
    uploaded_bytes: i64,
    part_size: Option<i64>,
    part_count: Option<i64>,
    completed_parts: Vec<i64>,
    progress: f64,
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
    pub fn start(addr: &str) -> anyhow::Result<Self> {
        let token = Uuid::new_v4().as_simple().to_string();
        println!("control plane start token={}", token);
        use std::io::Write;
        let _ = std::io::stdout().flush();

        let state = Arc::new(ControlState {
            token,
            uploads: Mutex::new(HashMap::new()),
            sync_now: Notify::new(),
        });

        let app = Router::new()
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

    pub fn record_upload(&self, key: String, size: i64) {
        let mut uploads = self.state.uploads.lock().unwrap();
        let now = Utc::now();
        let entry = UploadEntry {
            id: Uuid::new_v4().to_string(),
            key,
            state: "completed".to_string(),
            size,
            uploaded_bytes: size,
            part_size: None,
            part_count: None,
            completed_parts: Vec::new(),
            progress: 100.0,
            started_at: now,
            updated_at: now,
        };
        uploads.insert(entry.id.clone(), entry);
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
            state: "completed".to_string(),
            conflict_state: None,
            progress: u.progress,
            error: None,
            updated_at: u.updated_at,
        });
        summary.completed += 1;
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
        u.state = "resumed".to_string();
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
        let state = Arc::new(ControlState {
            token: "secret".into(),
            uploads: Mutex::new(HashMap::new()),
            sync_now: Notify::new(),
        });
        let cp = ControlPlane {
            state: state.clone(),
        };
        cp.record_upload("alice@example.com/public/demo.bin".into(), 1024);
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
