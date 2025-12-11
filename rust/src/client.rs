use std::path::{Path, PathBuf};
use std::time::Duration;

use anyhow::{Context, Result};
use tokio::sync::broadcast::Receiver;
use tokio::time::sleep;
use tokio_tungstenite::tungstenite::protocol::Message;

use crate::config::Config;
use crate::http::{ApiClient, PresignedParams};

pub struct Client {
    cfg: Config,
    api: ApiClient,
    #[allow(dead_code)]
    events_rx: Option<Receiver<Message>>,
}

impl Client {
    pub fn new(cfg: Config, api: ApiClient, events_rx: Option<Receiver<Message>>) -> Self {
        Self {
            cfg,
            api,
            events_rx,
        }
    }

    pub async fn start(&mut self) -> Result<()> {
        // Basic health check to ensure server reachability.
        self.api.healthz().await.context("healthz")?;

        // TODO: integrate watch + upload/download logic; for now keep local mirror.
        self.local_sync_loop().await
    }

    async fn local_sync_loop(&mut self) -> Result<()> {
        let root = root_from_datasite(&self.cfg.data_dir);
        loop {
            crate::sync::sync_once(&root)?;
            sleep(Duration::from_millis(200)).await;
        }
    }

    #[allow(dead_code)]
    async fn fetch_remote(&self, key: &str) -> Result<()> {
        let params = PresignedParams {
            keys: vec![key.to_string()],
        };
        let _resp = self
            .api
            .get_blob_presigned(&params)
            .await
            .context("presigned download")?;
        Ok(())
    }
}

fn root_from_datasite(data_dir: &Path) -> PathBuf {
    data_dir
        .parent()
        .map(|p| p.to_path_buf())
        .unwrap_or_else(|| data_dir.to_path_buf())
}
