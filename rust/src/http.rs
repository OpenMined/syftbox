use std::sync::Arc;
use std::time::Duration;

use anyhow::{Context, Result};
use chrono::{DateTime, Utc};
use reqwest::{Client as HttpClient, ClientBuilder, RequestBuilder, Response, StatusCode};
use serde::Deserialize;
use serde::Serialize;
use tokio::sync::Mutex;

use crate::auth::{refresh_auth_tokens, validate_token, AuthTokenResponse};
use crate::telemetry::HttpStats;

#[derive(Clone)]
pub struct ApiClient {
    base: String,
    http: HttpClient,
    user: String,
    stats: std::sync::Arc<HttpStats>,
    auth: Arc<AuthState>,
}

struct AuthState {
    email: String,
    access_token: Mutex<Option<String>>,
    refresh_token: Mutex<Option<String>>,
    config_path: Option<std::path::PathBuf>,
}

impl AuthState {
    async fn ensure_access_token_with<F, Fut>(&self, refresh: F) -> Result<()>
    where
        F: Fn(String) -> Fut,
        Fut: std::future::Future<Output = Result<AuthTokenResponse>>,
    {
        let needs_refresh = {
            let access = self.access_token.lock().await;
            match access.as_deref() {
                None => true,
                Some(t) => validate_token(t, "access", &self.email).is_err(),
            }
        };
        if !needs_refresh {
            return Ok(());
        }

        let refresh_token = { self.refresh_token.lock().await.clone() };
        let Some(refresh_token) = refresh_token else {
            return Ok(());
        };

        let tokens = refresh(refresh_token.clone()).await?;
        validate_token(&tokens.refresh_token, "refresh", &self.email).context("refresh token")?;
        validate_token(&tokens.access_token, "access", &self.email).context("access token")?;

        {
            let mut access = self.access_token.lock().await;
            *access = Some(tokens.access_token);
        }

        if tokens.refresh_token != refresh_token {
            {
                let mut rt = self.refresh_token.lock().await;
                *rt = Some(tokens.refresh_token.clone());
            }
            if let Some(path) = &self.config_path {
                let _ = crate::config::save_refresh_token_file_only(path, &tokens.refresh_token);
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use base64::engine::general_purpose::URL_SAFE_NO_PAD;
    use base64::Engine;
    use tokio::net::TcpListener;

    fn fake_jwt(payload: &serde_json::Value) -> String {
        let header = serde_json::json!({"alg":"none","typ":"JWT"});
        format!(
            "{}.{}.sig",
            URL_SAFE_NO_PAD.encode(serde_json::to_vec(&header).unwrap()),
            URL_SAFE_NO_PAD.encode(serde_json::to_vec(payload).unwrap())
        )
    }

    #[tokio::test]
    async fn ensure_access_token_refreshes_and_persists_rotated_refresh_token() {
        let tmp = std::env::temp_dir().join("syftbox-rs-token-refresh-test");
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();
        let cfg_path = tmp.join("config.json");
        std::fs::write(
            &cfg_path,
            format!(
                r#"{{
                  "email":"alice@example.com",
                  "data_dir":"{}",
                  "server_url":"https://syftbox.net",
                  "refresh_token":"old"
                }}"#,
                tmp.join("data").display()
            ),
        )
        .unwrap();

        let email = "alice@example.com".to_string();
        let expired_access =
            fake_jwt(&serde_json::json!({"type":"access","sub":email,"exp":1_i64}));
        let old_refresh =
            fake_jwt(&serde_json::json!({"type":"refresh","sub":email,"exp":9999999999_i64}));
        let new_refresh =
            fake_jwt(&serde_json::json!({"type":"refresh","sub":email,"exp":9999999999_i64}));
        let new_access =
            fake_jwt(&serde_json::json!({"type":"access","sub":email,"exp":9999999999_i64}));

        let auth = Arc::new(AuthState {
            email: "alice@example.com".to_string(),
            access_token: Mutex::new(Some(expired_access)),
            refresh_token: Mutex::new(Some(old_refresh.clone())),
            config_path: Some(cfg_path.clone()),
        });

        auth.ensure_access_token_with(|_rt| async {
            Ok(AuthTokenResponse {
                access_token: new_access.clone(),
                refresh_token: new_refresh.clone(),
            })
        })
        .await
        .unwrap();

        let access = auth.access_token.lock().await.clone().unwrap();
        assert_eq!(access, new_access);
        let refresh = auth.refresh_token.lock().await.clone().unwrap();
        assert_eq!(refresh, new_refresh);

        let raw = std::fs::read_to_string(&cfg_path).unwrap();
        assert!(raw.contains("\"refresh_token\""));
        assert!(!raw.contains("access_token"));
    }

    #[tokio::test]
    async fn upload_blob_retries_once_on_401_and_persists_rotated_refresh_token() {
        let tmp = std::env::temp_dir().join("syftbox-rs-upload-401-retry");
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();

        let cfg_path = tmp.join("config.json");
        std::fs::write(
            &cfg_path,
            format!(
                r#"{{
                  "email":"alice@example.com",
                  "data_dir":"{}",
                  "server_url":"http://127.0.0.1:0",
                  "refresh_token":"old"
                }}"#,
                tmp.join("data").display()
            ),
        )
        .unwrap();

        let email = "alice@example.com";
        let access1 = fake_jwt(&serde_json::json!({
            "type":"access","sub":email,"exp":9999999999_i64,"nonce":1
        }));
        let refresh1 = fake_jwt(&serde_json::json!({
            "type":"refresh","sub":email,"exp":9999999999_i64,"nonce":1
        }));
        let access2 = fake_jwt(&serde_json::json!({
            "type":"access","sub":email,"exp":9999999999_i64,"nonce":2
        }));
        let refresh2 = fake_jwt(&serde_json::json!({
            "type":"refresh","sub":email,"exp":9999999999_i64,"nonce":2
        }));

        let upload_file = tmp.join("payload.bin");
        std::fs::write(&upload_file, b"hello").unwrap();

        let listener = TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let base = format!("http://{}", addr);

        let app = axum::Router::new()
            .route(
                "/api/v1/blob/upload",
                axum::routing::put({
                    let access2 = access2.clone();
                    move |headers: axum::http::HeaderMap, body: axum::body::Body| async move {
                        // Drain the request body so the client doesn't see broken pipes when we
                        // return early (this endpoint is used with streaming multipart bodies).
                        let _ = axum::body::to_bytes(body, usize::MAX).await;
                        let got = headers
                            .get(axum::http::header::AUTHORIZATION)
                            .and_then(|v| v.to_str().ok())
                            .unwrap_or("");
                        if got == format!("Bearer {}", access2) {
                            axum::http::StatusCode::OK
                        } else {
                            axum::http::StatusCode::UNAUTHORIZED
                        }
                    }
                }),
            )
            .route(
                "/auth/refresh",
                axum::routing::post({
                    let access2 = access2.clone();
                    let refresh2 = refresh2.clone();
                    move || async move {
                        axum::Json(serde_json::json!({
                            "accessToken": access2,
                            "refreshToken": refresh2
                        }))
                    }
                }),
            );

        tokio::spawn(async move {
            let _ = axum::serve(listener, app).await;
        });

        let stats = std::sync::Arc::new(HttpStats::default());
        let client = ApiClient::new(
            &base,
            email,
            Some(&access1),
            Some(&refresh1),
            Some(&cfg_path),
            stats,
        )
        .unwrap();

        client
            .upload_blob("alice@example.com/public/test.bin", &upload_file)
            .await
            .unwrap();

        let raw = std::fs::read_to_string(&cfg_path).unwrap();
        assert!(raw.contains(&refresh2));
        assert!(!raw.contains("access_token"));
    }
}

impl ApiClient {
    pub fn new(
        base: &str,
        user: &str,
        auth_token: Option<&str>,
        refresh_token: Option<&str>,
        config_path: Option<&std::path::Path>,
        stats: std::sync::Arc<HttpStats>,
    ) -> Result<Self> {
        let builder = ClientBuilder::new()
            .timeout(Duration::from_secs(10 * 60))
            .connect_timeout(Duration::from_secs(5))
            .user_agent("syftbox-rs/0.1")
            .no_proxy();

        let http = builder.build()?;
        Ok(ApiClient {
            base: base.trim_end_matches('/').to_string(),
            http,
            user: user.to_string(),
            stats,
            auth: Arc::new(AuthState {
                email: user.to_string(),
                access_token: Mutex::new(auth_token.map(|s| s.to_string())),
                refresh_token: Mutex::new(refresh_token.map(|s| s.to_string())),
                config_path: config_path.map(|p| p.to_path_buf()),
            }),
        })
    }

    pub async fn healthz(&self) -> Result<()> {
        let url = format!("{}/healthz", self.base);
        let resp = self
            .send_authed("healthz", || self.with_user(self.http.get(url.clone())))
            .await?;
        map_status(resp, "healthz").await?;
        Ok(())
    }

    pub async fn get_blob_presigned(&self, body: &PresignedParams) -> Result<PresignedResponse> {
        let url = format!("{}/api/v1/blob/download", self.base);
        let body = serde_json::to_vec(body)?;
        let resp = self
            .send_authed("blob download", || {
                self.with_user(self.http.post(url.clone()))
                    .header(reqwest::header::CONTENT_TYPE, "application/json")
                    .body(body.clone())
            })
            .await?;
        map_error(resp, "blob download").await
    }

    pub async fn upload_blob(&self, key: &str, path: &std::path::Path) -> Result<()> {
        let url = format!("{}/api/v1/blob/upload", self.base);
        self.ensure_access_token().await?;

        let mut resp = self.send_upload_blob_once(&url, key, path).await?;
        if resp.status() == StatusCode::UNAUTHORIZED && self.has_refresh_token().await {
            self.clear_access_token().await;
            self.ensure_access_token().await?;
            resp = self.send_upload_blob_once(&url, key, path).await?;
        }
        if let Ok(meta) = std::fs::metadata(path) {
            self.stats.on_send(meta.len() as i64);
        }
        map_status(resp, "blob upload").await
    }

    async fn send_upload_blob_once(
        &self,
        url: &str,
        key: &str,
        path: &std::path::Path,
    ) -> Result<Response> {
        let form = reqwest::multipart::Form::new().file("file", path).await?;
        let mut req = self.with_user(self.http.put(url).query(&[("key", key)]));
        if let Some(token) = self.current_access_token().await {
            req = req.bearer_auth(token);
        }
        let resp = req.multipart(form).send().await?;
        Ok(resp)
    }

    pub async fn upload_multipart_urls(
        &self,
        body: &MultipartUploadRequest,
    ) -> Result<MultipartUploadResponse> {
        let url = format!("{}/api/v1/blob/upload/multipart", self.base);
        let body = serde_json::to_vec(body)?;
        let resp = self
            .send_authed("blob multipart upload", || {
                self.with_user(self.http.post(url.clone()))
                    .header(reqwest::header::CONTENT_TYPE, "application/json")
                    .body(body.clone())
            })
            .await?;
        map_error(resp, "blob multipart upload").await
    }

    pub async fn upload_multipart_complete(
        &self,
        body: &CompleteMultipartUploadRequest,
    ) -> Result<UploadResponse> {
        let url = format!("{}/api/v1/blob/upload/complete", self.base);
        let body = serde_json::to_vec(body)?;
        let resp = self
            .send_authed("blob multipart upload complete", || {
                self.with_user(self.http.post(url.clone()))
                    .header(reqwest::header::CONTENT_TYPE, "application/json")
                    .body(body.clone())
            })
            .await?;
        map_error(resp, "blob multipart upload complete").await
    }

    pub async fn upload_multipart_abort(&self, body: &AbortMultipartUploadRequest) -> Result<()> {
        let url = format!("{}/api/v1/blob/upload/abort", self.base);
        let body = serde_json::to_vec(body)?;
        let resp = self
            .send_authed("blob multipart upload abort", || {
                self.with_user(self.http.post(url.clone()))
                    .header(reqwest::header::CONTENT_TYPE, "application/json")
                    .body(body.clone())
            })
            .await?;
        map_status(resp, "blob multipart upload abort").await
    }

    pub async fn delete_blobs(&self, keys: &[String]) -> Result<()> {
        if keys.is_empty() {
            return Ok(());
        }
        let url = format!("{}/api/v1/blob/delete", self.base);
        let body = DeleteParams { keys };
        let body = serde_json::to_vec(&body)?;
        let resp = self
            .send_authed("blob delete", || {
                self.with_user(self.http.post(url.clone()))
                    .header(reqwest::header::CONTENT_TYPE, "application/json")
                    .body(body.clone())
            })
            .await?;
        map_status(resp, "blob delete").await
    }

    pub fn http(&self) -> &HttpClient {
        &self.http
    }

    pub fn stats(&self) -> std::sync::Arc<HttpStats> {
        self.stats.clone()
    }

    pub async fn datasite_view(&self) -> Result<DatasiteViewResponse> {
        let url = format!("{}/api/v1/datasite/view", self.base);
        let resp = self
            .send_authed("datasite view", || {
                self.with_user(self.http.get(url.clone()))
            })
            .await?;
        map_error(resp, "datasite view").await
    }

    fn with_user(&self, req: RequestBuilder) -> RequestBuilder {
        req.query(&[("user", &self.user)])
    }

    pub(crate) async fn current_access_token(&self) -> Option<String> {
        self.auth.access_token.lock().await.clone()
    }

    pub(crate) async fn has_refresh_token(&self) -> bool {
        self.auth.refresh_token.lock().await.is_some()
    }

    pub(crate) async fn clear_access_token(&self) {
        *self.auth.access_token.lock().await = None;
    }

    pub(crate) async fn ensure_access_token(&self) -> Result<()> {
        self.auth
            .ensure_access_token_with(|refresh| async move {
                refresh_auth_tokens(&self.http, &self.base, &refresh).await
            })
            .await
    }

    async fn send_authed<F>(&self, _op: &str, build: F) -> Result<Response>
    where
        F: Fn() -> RequestBuilder,
    {
        self.ensure_access_token().await?;
        let resp = self.send_once(build()).await?;
        if resp.status() != StatusCode::UNAUTHORIZED {
            return Ok(resp);
        }
        // Retry once after forcing a refresh (if possible).
        let has_refresh = self.auth.refresh_token.lock().await.is_some();
        if !has_refresh {
            return Ok(resp);
        }
        // Force refresh by clearing access token.
        *self.auth.access_token.lock().await = None;
        self.ensure_access_token().await?;
        self.send_once(build()).await
    }

    async fn send_once(&self, mut req: RequestBuilder) -> Result<Response> {
        if let Some(token) = self.current_access_token().await {
            req = req.bearer_auth(token);
        }
        let resp = req.send().await?;
        Ok(resp)
    }
}

async fn map_error<T: for<'de> Deserialize<'de>>(resp: Response, op: &str) -> Result<T> {
    let status = resp.status();
    if status.is_success() {
        let val = resp.json::<T>().await?;
        return Ok(val);
    }

    let text = resp.text().await.unwrap_or_default();
    match status {
        StatusCode::UNAUTHORIZED => anyhow::bail!("{op} unauthorized: {text}"),
        StatusCode::FORBIDDEN => anyhow::bail!("{op} forbidden: {text}"),
        StatusCode::NOT_FOUND => anyhow::bail!("{op} not found: {text}"),
        _ => anyhow::bail!("{op} failed: {status} {text}"),
    }
}

async fn map_status(resp: Response, op: &str) -> Result<()> {
    let status = resp.status();
    if status.is_success() {
        return Ok(());
    }
    let text = resp.text().await.unwrap_or_default();
    match status {
        StatusCode::UNAUTHORIZED => anyhow::bail!("{op} unauthorized: {text}"),
        StatusCode::FORBIDDEN => anyhow::bail!("{op} forbidden: {text}"),
        StatusCode::NOT_FOUND => anyhow::bail!("{op} not found: {text}"),
        _ => anyhow::bail!("{op} failed: {status} {text}"),
    }
}

#[derive(Debug, Deserialize)]
pub struct DatasiteViewResponse {
    pub files: Vec<BlobInfo>,
}

#[derive(Debug, Deserialize)]
pub struct BlobInfo {
    pub key: String,
    pub etag: String,
    pub size: i64,
    #[serde(rename = "lastModified")]
    pub last_modified: DateTime<Utc>,
}

#[derive(Debug, Deserialize)]
pub struct PresignedResponse {
    pub urls: Vec<BlobUrl>,
}

#[derive(Debug, Deserialize)]
pub struct BlobUrl {
    pub key: String,
    pub url: String,
}

#[derive(Debug, serde::Serialize)]
pub struct PresignedParams {
    pub keys: Vec<String>,
}

#[derive(Debug, Serialize)]
struct DeleteParams<'a> {
    keys: &'a [String],
}

#[derive(Debug, Serialize)]
pub struct MultipartUploadRequest {
    pub key: String,
    pub size: i64,
    #[serde(rename = "partSize")]
    pub part_size: i64,
    #[serde(rename = "uploadId", skip_serializing_if = "Option::is_none")]
    pub upload_id: Option<String>,
    #[serde(rename = "partNumbers", skip_serializing_if = "Vec::is_empty", default)]
    pub part_numbers: Vec<i64>,
}

#[derive(Debug, Deserialize)]
pub struct MultipartUploadResponse {
    #[serde(rename = "uploadId")]
    pub upload_id: String,
    #[serde(rename = "partSize")]
    pub part_size: i64,
    pub urls: std::collections::HashMap<i64, String>,
    #[serde(rename = "partCount")]
    pub part_count: i64,
}

#[derive(Debug, Serialize)]
pub struct CompletedPart {
    #[serde(rename = "partNumber")]
    pub part_number: i64,
    #[serde(rename = "etag")]
    pub etag: String,
}

#[derive(Debug, Serialize)]
pub struct CompleteMultipartUploadRequest {
    pub key: String,
    #[serde(rename = "uploadId")]
    pub upload_id: String,
    pub parts: Vec<CompletedPart>,
}

#[derive(Debug, Serialize)]
pub struct AbortMultipartUploadRequest {
    pub key: String,
    #[serde(rename = "uploadId")]
    pub upload_id: String,
}

#[derive(Debug, Deserialize)]
pub struct UploadResponse {}
