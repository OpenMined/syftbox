use crate::hotlink::{
    dial_hotlink_ipc, ensure_hotlink_ipc_marker, hotlink_ipc_marker_name, listen_hotlink_ipc,
    HotlinkFrame, HotlinkListener, HotlinkStream, HOTLINK_ACCEPT_NAME,
};
use crate::wsproto::{
    Encoding, MsgpackHotlinkAccept, MsgpackHotlinkClose, MsgpackHotlinkData, MsgpackHotlinkOpen,
    MsgpackHotlinkReject,
};
use anyhow::{Context, Result};
use base64::Engine;
use futures_util::FutureExt;
use md5::compute as md5_compute;
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex as StdMutex};
use std::time::Duration;
use tokio::sync::{Mutex as TokioMutex, Notify, RwLock};
use tokio::time::timeout;
use tokio_tungstenite::tungstenite::Message;
use uuid::Uuid;

const HOTLINK_ACCEPT_TIMEOUT: Duration = Duration::from_millis(1500);
const HOTLINK_ACCEPT_DELAY: Duration = Duration::from_millis(200);
const HOTLINK_CONNECT_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Clone)]
pub struct HotlinkManager {
    enabled: bool,
    socket_only: bool,
    datasites_root: PathBuf,
    ws: crate::client::WsHandle,
    sessions: Arc<RwLock<HashMap<String, HotlinkSession>>>,
    outbound: Arc<RwLock<HashMap<String, HotlinkOutbound>>>,
    outbound_by_path: Arc<RwLock<HashMap<String, String>>>,
    ipc_writers: Arc<TokioMutex<HashMap<PathBuf, Arc<TokioMutex<HotlinkIpcWriter>>>>>,
    local_readers: Arc<StdMutex<HashMap<PathBuf, ()>>>,
    shutdown: Arc<Notify>,
}

#[allow(dead_code)]
struct HotlinkSession {
    id: String,
    path: String,
    dir_abs: PathBuf,
    ipc_path: PathBuf,
    accept_path: PathBuf,
}

#[allow(dead_code)]
struct HotlinkOutbound {
    id: String,
    path_key: String,
    accepted: bool,
    seq: u64,
    notify: Arc<Notify>,
    rejected: Option<String>,
}

struct HotlinkIpcWriter {
    listener: Option<HotlinkListener>,
    conn: Option<HotlinkStream>,
}

impl HotlinkManager {
    pub fn new(
        datasites_root: PathBuf,
        ws: crate::client::WsHandle,
        shutdown: Arc<Notify>,
    ) -> Self {
        let enabled = std::env::var("SYFTBOX_HOTLINK").ok().as_deref() == Some("1");
        let socket_only = std::env::var("SYFTBOX_HOTLINK_SOCKET_ONLY").ok().as_deref() == Some("1");
        Self {
            enabled,
            socket_only,
            datasites_root,
            ws,
            sessions: Arc::new(RwLock::new(HashMap::new())),
            outbound: Arc::new(RwLock::new(HashMap::new())),
            outbound_by_path: Arc::new(RwLock::new(HashMap::new())),
            ipc_writers: Arc::new(TokioMutex::new(HashMap::new())),
            local_readers: Arc::new(StdMutex::new(HashMap::new())),
            shutdown,
        }
    }

    pub fn enabled(&self) -> bool {
        self.enabled
    }

    #[allow(dead_code)]
    pub fn socket_only(&self) -> bool {
        self.socket_only
    }

    pub fn start_local_discovery(&self, owner_email: String) {
        if !self.enabled || !self.socket_only {
            return;
        }
        let debug = std::env::var("SYFTBOX_HOTLINK_DEBUG").ok().as_deref() == Some("1");
        let manager = self.clone();
        tokio::spawn(async move {
            let root = manager.datasites_root.join(&owner_email).join("app_data");
            let marker = hotlink_ipc_marker_name();
            loop {
                if manager.shutdown.notified().now_or_never().is_some() {
                    break;
                }
                for entry in walkdir::WalkDir::new(&root)
                    .into_iter()
                    .filter_map(Result::ok)
                {
                    if !entry.file_type().is_file() {
                        continue;
                    }
                    if entry.file_name().to_string_lossy() != marker {
                        continue;
                    }
                    let marker_path = entry.path().to_path_buf();
                    let mut readers = manager.local_readers.lock().unwrap();
                    if readers.contains_key(&marker_path) {
                        continue;
                    }
                    if debug {
                        crate::logging::info(format!(
                            "hotlink discovery: starting reader for {}",
                            marker_path.display()
                        ));
                    }
                    readers.insert(marker_path.clone(), ());
                    let manager_clone = manager.clone();
                    tokio::spawn(async move {
                        manager_clone.run_local_reader(marker_path).await;
                    });
                }
                tokio::time::sleep(Duration::from_millis(250)).await;
            }
        });
    }

    async fn run_local_reader(&self, marker_path: PathBuf) {
        let debug = std::env::var("SYFTBOX_HOTLINK_DEBUG").ok().as_deref() == Some("1");
        let _ = ensure_hotlink_ipc_marker(&marker_path).await;

        // Create listener ONCE and reuse it for all connections.
        let listener = match listen_hotlink_ipc(&marker_path).await {
            Ok(l) => l,
            Err(err) => {
                crate::logging::error(format!("hotlink ipc listen failed: {err:?}"));
                return;
            }
        };

        loop {
            if self.shutdown.notified().now_or_never().is_some() {
                break;
            }
            let mut conn = match listener.accept(HOTLINK_CONNECT_TIMEOUT).await {
                Ok(c) => c,
                Err(_) => {
                    // Accept timed out; just try again without recreating the listener.
                    continue;
                }
            };
            if debug {
                crate::logging::info(format!("hotlink ipc accepted: {}", marker_path.display()));
            }
            loop {
                let frame = match conn.read_frame().await {
                    Ok(f) => f,
                    Err(err) => {
                        crate::logging::error(format!("hotlink ipc read failed: {err:?}"));
                        break;
                    }
                };
                if frame.payload.is_empty() || frame.path.trim().is_empty() {
                    continue;
                }
                if debug {
                    crate::logging::info(format!(
                        "hotlink ipc frame: path={} bytes={}",
                        frame.path,
                        frame.payload.len()
                    ));
                }
                let etag = if frame.etag.trim().is_empty() {
                    format!("{:x}", md5_compute(&frame.payload))
                } else {
                    frame.etag.clone()
                };
                if debug {
                    crate::logging::info(format!(
                        "hotlink send: path={} etag={}",
                        frame.path, etag
                    ));
                }
                self.send_best_effort(frame.path.clone(), etag, frame.payload.clone())
                    .await;
            }
        }
    }

    pub async fn handle_open(&self, session_id: String, path: String) {
        if !self.enabled {
            return;
        }
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink open received: session={} path={}",
                session_id, path
            ));
        }

        let mut dir_rel = PathBuf::from(&path);
        if is_hotlink_eligible(&path) {
            if let Some(parent) = Path::new(&path).parent() {
                dir_rel = parent.to_path_buf();
            }
        }
        let dir_abs = self.datasites_root.join(&dir_rel);
        if let Err(err) = tokio::fs::create_dir_all(&dir_abs).await {
            crate::logging::error(format!("hotlink open ensure dir failed: {err:?}"));
            let _ = self.send_reject(&session_id, "ipc unavailable").await;
            return;
        }

        let ipc_path = dir_abs.join(hotlink_ipc_marker_name());
        if let Err(err) = ensure_hotlink_ipc_marker(&ipc_path).await {
            crate::logging::error(format!("hotlink open ipc setup failed: {err:?}"));
            let _ = self.send_reject(&session_id, "ipc unavailable").await;
            return;
        }
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink open ipc marker ready: {}",
                ipc_path.display()
            ));
        }

        let accept_path = dir_abs.join(HOTLINK_ACCEPT_NAME);
        let session = HotlinkSession {
            id: session_id.clone(),
            path: path.clone(),
            dir_abs,
            ipc_path: ipc_path.clone(),
            accept_path: accept_path.clone(),
        };
        self.sessions
            .write()
            .await
            .insert(session_id.clone(), session);

        // Eagerly create the IPC listener so the test/SDK can connect immediately.
        if let Err(err) = self.ensure_ipc_listener(&ipc_path).await {
            crate::logging::error(format!("hotlink ensure listener failed: {err:?}"));
        }

        if tokio::fs::metadata(&accept_path).await.is_ok() {
            if hotlink_debug_enabled() {
                crate::logging::info(format!(
                    "hotlink accept marker present: {}",
                    accept_path.display()
                ));
            }
            let _ = self.send_accept(&session_id).await;
            return;
        }

        let manager = self.clone();
        tokio::spawn(async move {
            let mut waited = Duration::from_millis(0);
            while waited < HOTLINK_ACCEPT_TIMEOUT {
                if tokio::fs::metadata(&accept_path).await.is_ok() {
                    if hotlink_debug_enabled() {
                        crate::logging::info(format!(
                            "hotlink accept marker found after wait: {}",
                            accept_path.display()
                        ));
                    }
                    let _ = manager.send_accept(&session_id).await;
                    return;
                }
                tokio::time::sleep(HOTLINK_ACCEPT_DELAY).await;
                waited += HOTLINK_ACCEPT_DELAY;
            }
        });
    }

    pub async fn handle_accept(&self, session_id: String) {
        if !self.enabled {
            return;
        }
        if hotlink_debug_enabled() {
            crate::logging::info(format!("hotlink accept received: session={}", session_id));
        }
        let mut out = self.outbound.write().await;
        if let Some(entry) = out.get_mut(&session_id) {
            entry.accepted = true;
            entry.notify.notify_waiters();
        }
    }

    pub async fn handle_reject(&self, session_id: String, reason: String) {
        if !self.enabled {
            return;
        }
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink reject received: session={} reason={}",
                session_id, reason
            ));
        }
        let mut out = self.outbound.write().await;
        if let Some(entry) = out.get_mut(&session_id) {
            entry.rejected = Some(reason);
            entry.notify.notify_waiters();
        }
    }

    pub async fn handle_data(
        &self,
        session_id: String,
        path: String,
        etag: String,
        seq: u64,
        payload: Vec<u8>,
    ) {
        if !self.enabled {
            return;
        }
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink data received: session={} seq={} bytes={}",
                session_id,
                seq,
                payload.len()
            ));
        }
        let sessions = self.sessions.read().await;
        let session = match sessions.get(&session_id) {
            Some(s) => s,
            None => return,
        };

        let frame = HotlinkFrame {
            path: if path.trim().is_empty() {
                session.path.clone()
            } else {
                path
            },
            etag,
            seq,
            payload,
        };

        if let Err(err) = self.write_ipc(&session.ipc_path, frame).await {
            crate::logging::error(format!("hotlink ipc write failed: {err:?}"));
        }
    }

    pub async fn handle_close(&self, session_id: String) {
        if !self.enabled {
            return;
        }
        if hotlink_debug_enabled() {
            crate::logging::info(format!("hotlink close received: session={}", session_id));
        }
        self.sessions.write().await.remove(&session_id);
    }

    pub async fn send_best_effort(&self, rel_path: String, etag: String, payload: Vec<u8>) {
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink best effort: path={} bytes={} enabled={} eligible={}",
                rel_path,
                payload.len(),
                self.enabled,
                is_hotlink_eligible(&rel_path)
            ));
        }
        if !self.enabled {
            return;
        }
        if !is_hotlink_eligible(&rel_path) || payload.is_empty() {
            if hotlink_debug_enabled() {
                crate::logging::info(format!(
                    "hotlink best effort skipped: path={} bytes={}",
                    rel_path,
                    payload.len()
                ));
            }
            return;
        }
        if hotlink_debug_enabled() {
            crate::logging::info("hotlink best effort spawn send");
        }
        let manager = self.clone();
        tokio::spawn(async move {
            if hotlink_debug_enabled() {
                crate::logging::info("hotlink send task started");
            }
            if let Err(err) = manager.send_hotlink(rel_path, etag, payload).await {
                crate::logging::error(format!("hotlink send failed: {err:?}"));
            }
        });
    }

    async fn send_hotlink(&self, rel_path: String, etag: String, payload: Vec<u8>) -> Result<()> {
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink send start: path={} bytes={}",
                rel_path,
                payload.len()
            ));
        }
        let path_key = parent_path(&rel_path);
        // Check for existing session - must clone and drop the read guard before acquiring write lock
        // to avoid deadlock (the guard would otherwise be held across the if/else branches).
        let existing_session = {
            let guard = self.outbound_by_path.read().await;
            guard.get(&path_key).cloned()
        };
        let session_id = if let Some(id) = existing_session {
            id
        } else {
            let id = Uuid::new_v4().to_string();
            let outbound = HotlinkOutbound {
                id: id.clone(),
                path_key: path_key.clone(),
                accepted: false,
                seq: 0,
                notify: Arc::new(Notify::new()),
                rejected: None,
            };
            self.outbound.write().await.insert(id.clone(), outbound);
            self.outbound_by_path
                .write()
                .await
                .insert(path_key.clone(), id.clone());
            if let Err(err) = self.send_open(&id, rel_path.clone()).await {
                crate::logging::error(format!("hotlink send open failed: {err:?}"));
                self.remove_outbound(&id).await;
                return Err(err);
            }
            id
        };

        if !self.wait_for_accept(&session_id).await {
            if hotlink_debug_enabled() {
                crate::logging::info(format!(
                    "hotlink accept timeout: session={} path={}",
                    session_id, rel_path
                ));
            }
            let _ = self.send_close(&session_id, "fallback").await;
            self.remove_outbound(&session_id).await;
            return Ok(());
        }

        let seq = {
            let mut out = self.outbound.write().await;
            if let Some(entry) = out.get_mut(&session_id) {
                entry.seq += 1;
                entry.seq
            } else {
                1
            }
        };

        if let Err(err) = self
            .send_data(&session_id, seq, rel_path, etag, payload)
            .await
        {
            crate::logging::error(format!("hotlink send data failed: {err:?}"));
            return Err(err);
        }
        Ok(())
    }

    async fn wait_for_accept(&self, session_id: &str) -> bool {
        let notify = {
            let out = self.outbound.read().await;
            match out.get(session_id) {
                Some(entry) if entry.accepted => return true,
                Some(entry) => entry.notify.clone(),
                None => return false,
            }
        };

        if timeout(HOTLINK_ACCEPT_TIMEOUT, notify.notified())
            .await
            .is_err()
        {
            return false;
        }

        let out = self.outbound.read().await;
        if let Some(entry) = out.get(session_id) {
            return entry.accepted && entry.rejected.is_none();
        }
        false
    }

    async fn remove_outbound(&self, session_id: &str) {
        let mut out = self.outbound.write().await;
        if let Some(entry) = out.remove(session_id) {
            self.outbound_by_path.write().await.remove(&entry.path_key);
        }
    }

    async fn ensure_ipc_listener(&self, marker_path: &Path) -> Result<()> {
        let writer = {
            let mut writers = self.ipc_writers.lock().await;
            writers
                .entry(marker_path.to_path_buf())
                .or_insert_with(|| {
                    Arc::new(TokioMutex::new(HotlinkIpcWriter {
                        listener: None,
                        conn: None,
                    }))
                })
                .clone()
        };

        let mut writer = writer.lock().await;
        if writer.listener.is_none() {
            writer.listener = Some(listen_hotlink_ipc(marker_path).await?);
        }
        Ok(())
    }

    async fn write_ipc(&self, marker_path: &Path, frame: HotlinkFrame) -> Result<()> {
        let writer = {
            let mut writers = self.ipc_writers.lock().await;
            writers
                .entry(marker_path.to_path_buf())
                .or_insert_with(|| {
                    Arc::new(TokioMutex::new(HotlinkIpcWriter {
                        listener: None,
                        conn: None,
                    }))
                })
                .clone()
        };

        let mut writer = writer.lock().await;
        if writer.listener.is_none() {
            writer.listener = Some(listen_hotlink_ipc(marker_path).await?);
        }
        if writer.conn.is_none() {
            let listener = writer.listener.as_ref().context("ipc listener")?;
            writer.conn = Some(listener.accept(HOTLINK_CONNECT_TIMEOUT).await?);
        }
        if let Some(conn) = writer.conn.as_mut() {
            if conn.write_frame(&frame).await.is_err() {
                writer.conn = None;
                anyhow::bail!("ipc write failed");
            }
        }
        Ok(())
    }

    async fn send_open(&self, session_id: &str, path: String) -> Result<()> {
        let id = Uuid::new_v4().to_string();
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink send open: session={} path={} encoding={}",
                session_id,
                path,
                self.ws.encoding().as_str()
            ));
        }
        match self.ws.encoding() {
            Encoding::Json => {
                let payload = serde_json::json!({
                    "id": id,
                    "typ": 9,
                    "dat": {"sid": session_id, "pth": path}
                });
                self.ws
                    .send_ws(Message::Text(serde_json::to_string(&payload)?))
                    .await?;
            }
            Encoding::MsgPack => {
                let open = MsgpackHotlinkOpen {
                    session_id: session_id.to_string(),
                    path,
                };
                let bin = crate::wsproto::encode_msgpack(&id, 9, &open)?;
                self.ws.send_ws(Message::Binary(bin)).await?;
            }
        }
        Ok(())
    }

    async fn send_accept(&self, session_id: &str) -> Result<()> {
        let id = Uuid::new_v4().to_string();
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink send accept: session={} encoding={}",
                session_id,
                self.ws.encoding().as_str()
            ));
        }
        match self.ws.encoding() {
            Encoding::Json => {
                let payload = serde_json::json!({
                    "id": id,
                    "typ": 10,
                    "dat": {"sid": session_id}
                });
                self.ws
                    .send_ws(Message::Text(serde_json::to_string(&payload)?))
                    .await?;
            }
            Encoding::MsgPack => {
                let accept = MsgpackHotlinkAccept {
                    session_id: session_id.to_string(),
                };
                let bin = crate::wsproto::encode_msgpack(&id, 10, &accept)?;
                self.ws.send_ws(Message::Binary(bin)).await?;
            }
        }
        Ok(())
    }

    async fn send_reject(&self, session_id: &str, reason: &str) -> Result<()> {
        let id = Uuid::new_v4().to_string();
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink send reject: session={} reason={} encoding={}",
                session_id,
                reason,
                self.ws.encoding().as_str()
            ));
        }
        match self.ws.encoding() {
            Encoding::Json => {
                let payload = serde_json::json!({
                    "id": id,
                    "typ": 11,
                    "dat": {"sid": session_id, "rsn": reason}
                });
                self.ws
                    .send_ws(Message::Text(serde_json::to_string(&payload)?))
                    .await?;
            }
            Encoding::MsgPack => {
                let reject = MsgpackHotlinkReject {
                    session_id: session_id.to_string(),
                    reason: reason.to_string(),
                };
                let bin = crate::wsproto::encode_msgpack(&id, 11, &reject)?;
                self.ws.send_ws(Message::Binary(bin)).await?;
            }
        }
        Ok(())
    }

    async fn send_data(
        &self,
        session_id: &str,
        seq: u64,
        path: String,
        etag: String,
        payload: Vec<u8>,
    ) -> Result<()> {
        let id = Uuid::new_v4().to_string();
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink send data: session={} seq={} bytes={} encoding={}",
                session_id,
                seq,
                payload.len(),
                self.ws.encoding().as_str()
            ));
        }
        match self.ws.encoding() {
            Encoding::Json => {
                let payload = serde_json::json!({
                    "id": id,
                    "typ": 12,
                    "dat": {
                        "sid": session_id,
                        "seq": seq,
                        "pth": path,
                        "etg": etag,
                        "pay": base64::engine::general_purpose::STANDARD.encode(payload)
                    }
                });
                self.ws
                    .send_ws(Message::Text(serde_json::to_string(&payload)?))
                    .await?;
            }
            Encoding::MsgPack => {
                let data = MsgpackHotlinkData {
                    session_id: session_id.to_string(),
                    seq,
                    path,
                    etag,
                    payload: Some(payload.into()),
                };
                let bin = crate::wsproto::encode_msgpack(&id, 12, &data)?;
                self.ws.send_ws(Message::Binary(bin)).await?;
            }
        }
        Ok(())
    }

    async fn send_close(&self, session_id: &str, reason: &str) -> Result<()> {
        let id = Uuid::new_v4().to_string();
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink send close: session={} reason={} encoding={}",
                session_id,
                reason,
                self.ws.encoding().as_str()
            ));
        }
        match self.ws.encoding() {
            Encoding::Json => {
                let payload = serde_json::json!({
                    "id": id,
                    "typ": 13,
                    "dat": {"sid": session_id, "rsn": reason}
                });
                self.ws
                    .send_ws(Message::Text(serde_json::to_string(&payload)?))
                    .await?;
            }
            Encoding::MsgPack => {
                let close = MsgpackHotlinkClose {
                    session_id: session_id.to_string(),
                    reason: reason.to_string(),
                };
                let bin = crate::wsproto::encode_msgpack(&id, 13, &close)?;
                self.ws.send_ws(Message::Binary(bin)).await?;
            }
        }
        Ok(())
    }
}

fn hotlink_debug_enabled() -> bool {
    std::env::var("SYFTBOX_HOTLINK_DEBUG").ok().as_deref() == Some("1")
}

fn is_hotlink_eligible(path: &str) -> bool {
    path.ends_with(".request") || path.ends_with(".response")
}

fn parent_path(path: &str) -> String {
    let p = Path::new(path);
    p.parent()
        .map(|p| p.to_string_lossy().to_string())
        .unwrap_or_else(|| path.to_string())
}

// Expose IPC dial helper for SDKs.
#[allow(dead_code)]
pub async fn connect_ipc(marker_path: &Path, timeout: Duration) -> Result<HotlinkStream> {
    dial_hotlink_ipc(marker_path, timeout).await
}
