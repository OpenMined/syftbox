use std::time::Duration;

use anyhow::Result;
use chrono::{DateTime, Utc};
use reqwest::{Client as HttpClient, ClientBuilder, RequestBuilder, Response, StatusCode};
use serde::Deserialize;
use serde::Serialize;

use crate::telemetry::HttpStats;

#[derive(Clone)]
pub struct ApiClient {
    base: String,
    http: HttpClient,
    user: String,
    stats: std::sync::Arc<HttpStats>,
}

impl ApiClient {
    pub fn new(
        base: &str,
        user: &str,
        auth_token: Option<&str>,
        stats: std::sync::Arc<HttpStats>,
    ) -> Result<Self> {
        let mut builder = ClientBuilder::new()
            .timeout(Duration::from_secs(10 * 60))
            .connect_timeout(Duration::from_secs(5))
            .user_agent("syftbox-rs/0.1")
            .no_proxy();

        if let Some(token) = auth_token {
            builder = builder.default_headers({
                let mut h = reqwest::header::HeaderMap::new();
                let value = format!("Bearer {token}");
                h.insert(
                    reqwest::header::AUTHORIZATION,
                    reqwest::header::HeaderValue::from_str(&value)?,
                );
                h
            });
        }

        let http = builder.build()?;
        Ok(ApiClient {
            base: base.trim_end_matches('/').to_string(),
            http,
            user: user.to_string(),
            stats,
        })
    }

    pub async fn healthz(&self) -> Result<()> {
        let url = format!("{}/healthz", self.base);
        let resp = self.with_user(self.http.get(url)).send().await?;
        map_status(resp, "healthz").await?;
        Ok(())
    }

    pub async fn get_blob_presigned(&self, body: &PresignedParams) -> Result<PresignedResponse> {
        let url = format!("{}/api/v1/blob/download", self.base);
        let resp = self
            .with_user(self.http.post(url))
            .json(body)
            .send()
            .await?;
        map_error(resp, "blob download").await
    }

    pub async fn upload_blob(&self, key: &str, path: &std::path::Path) -> Result<()> {
        let url = format!("{}/api/v1/blob/upload", self.base);
        let form = reqwest::multipart::Form::new().file("file", path).await?;
        let resp = self
            .with_user(self.http.put(url).query(&[("key", key)]))
            .multipart(form)
            .send()
            .await?;
        if let Ok(meta) = std::fs::metadata(path) {
            self.stats.on_send(meta.len() as i64);
        }
        map_status(resp, "blob upload").await
    }

    pub async fn upload_multipart_urls(
        &self,
        body: &MultipartUploadRequest,
    ) -> Result<MultipartUploadResponse> {
        let url = format!("{}/api/v1/blob/upload/multipart", self.base);
        let resp = self
            .with_user(self.http.post(url))
            .json(body)
            .send()
            .await?;
        map_error(resp, "blob multipart upload").await
    }

    pub async fn upload_multipart_complete(
        &self,
        body: &CompleteMultipartUploadRequest,
    ) -> Result<UploadResponse> {
        let url = format!("{}/api/v1/blob/upload/complete", self.base);
        let resp = self
            .with_user(self.http.post(url))
            .json(body)
            .send()
            .await?;
        map_error(resp, "blob multipart upload complete").await
    }

    pub async fn upload_multipart_abort(&self, body: &AbortMultipartUploadRequest) -> Result<()> {
        let url = format!("{}/api/v1/blob/upload/abort", self.base);
        let resp = self
            .with_user(self.http.post(url))
            .json(body)
            .send()
            .await?;
        map_status(resp, "blob multipart upload abort").await
    }

    pub async fn delete_blobs(&self, keys: &[String]) -> Result<()> {
        if keys.is_empty() {
            return Ok(());
        }
        let url = format!("{}/api/v1/blob/delete", self.base);
        let body = DeleteParams { keys };
        let resp = self
            .with_user(self.http.post(url))
            .json(&body)
            .send()
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
        let resp = self.with_user(self.http.get(url)).send().await?;
        map_error(resp, "datasite view").await
    }

    fn with_user(&self, req: RequestBuilder) -> RequestBuilder {
        req.query(&[("user", &self.user)])
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
    pub key: String,
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
pub struct UploadResponse {
    pub key: String,
    pub etag: String,
    pub size: i64,
    #[serde(rename = "lastModified")]
    pub last_modified: String,
    #[serde(default)]
    pub version: String,
}
