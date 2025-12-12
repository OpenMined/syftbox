use std::time::Duration;

use anyhow::Result;
use chrono::{DateTime, Utc};
use reqwest::{Client as HttpClient, ClientBuilder, RequestBuilder, Response, StatusCode};
use serde::Deserialize;
use serde::Serialize;

#[derive(Clone)]
pub struct ApiClient {
    base: String,
    http: HttpClient,
    user: String,
}

impl ApiClient {
    pub fn new(base: &str, user: &str, auth_token: Option<&str>) -> Result<Self> {
        let mut builder = ClientBuilder::new()
            .timeout(Duration::from_secs(15))
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
        map_status(resp, "blob upload").await
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
