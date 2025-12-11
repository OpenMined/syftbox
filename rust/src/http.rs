use std::time::Duration;

use anyhow::Result;
use reqwest::{Client as HttpClient, ClientBuilder, Response, StatusCode};
use serde::Deserialize;

#[derive(Clone)]
pub struct ApiClient {
    base: String,
    http: HttpClient,
}

impl ApiClient {
    pub fn new(base: &str, auth_token: Option<&str>) -> Result<Self> {
        let mut builder = ClientBuilder::new()
            .timeout(Duration::from_secs(15))
            .connect_timeout(Duration::from_secs(5))
            .user_agent("syftbox-rs/0.1");

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
        })
    }

    pub async fn healthz(&self) -> Result<()> {
        let url = format!("{}/healthz", self.base);
        let resp = self.http.get(url).send().await?;
        map_status(resp, "healthz").await?;
        Ok(())
    }

    pub async fn get_blob_presigned(&self, body: &PresignedParams) -> Result<PresignedResponse> {
        let url = format!("{}/api/v1/blob/download", self.base);
        let resp = self.http.post(url).json(body).send().await?;
        map_error(resp, "blob download").await
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
#[allow(dead_code)]
pub struct PresignedResponse {
    pub urls: Vec<String>,
}

#[derive(Debug, serde::Serialize)]
pub struct PresignedParams {
    pub keys: Vec<String>,
}
