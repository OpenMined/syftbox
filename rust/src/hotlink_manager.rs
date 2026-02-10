use crate::hotlink::{
    dial_hotlink_ipc, ensure_hotlink_ipc_marker, hotlink_ipc_marker_name, listen_hotlink_ipc,
    HotlinkFrame, HotlinkListener, HotlinkStream, HOTLINK_ACCEPT_NAME,
};
use crate::wsproto::{
    Encoding, MsgpackHotlinkAccept, MsgpackHotlinkClose, MsgpackHotlinkData, MsgpackHotlinkOpen,
    MsgpackHotlinkReject, MsgpackHotlinkSignal,
};
use anyhow::{Context, Result};
use base64::Engine;
use bytes::Bytes;
use futures_util::FutureExt;
use md5::compute as md5_compute;
use serde_json::json;
use serde_json::Value as JsonValue;
use std::collections::{BTreeMap, HashMap};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex as StdMutex};
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;
use tokio::sync::{Mutex as TokioMutex, Notify, RwLock};
use tokio::time::timeout;
use tokio_tungstenite::tungstenite::Message;
use uuid::Uuid;
use webrtc::api::interceptor_registry::register_default_interceptors;
use webrtc::api::media_engine::MediaEngine;
use webrtc::api::setting_engine::SettingEngine;
use webrtc::api::APIBuilder;
use webrtc::data_channel::data_channel_message::DataChannelMessage;
use webrtc::data_channel::RTCDataChannel;
use webrtc::ice_transport::ice_candidate::RTCIceCandidateInit;
use webrtc::ice_transport::ice_server::RTCIceServer;
use webrtc::interceptor::registry::Registry;
use webrtc::peer_connection::configuration::RTCConfiguration;
use webrtc::peer_connection::sdp::session_description::RTCSessionDescription;
use webrtc::peer_connection::RTCPeerConnection;

// Keep close to the legacy Go hotlink handshake window so stalled sessions
// fail quickly instead of adding multi-second pauses per new stream.
const HOTLINK_ACCEPT_TIMEOUT: Duration = Duration::from_millis(1500);
const HOTLINK_ACCEPT_DELAY: Duration = Duration::from_millis(200);
const HOTLINK_CONNECT_TIMEOUT: Duration = Duration::from_secs(5);
const HOTLINK_IPC_WRITE_TIMEOUT: Duration = Duration::from_secs(30);
const HOTLINK_IPC_RETRY_DELAY: Duration = Duration::from_millis(100);
const HOTLINK_TCP_SUFFIX: &str = "stream.tcp.request";
// Default near 60KiB to stay under common SCTP/WebRTC message limits while
// still reducing per-send overhead vs the old 14KiB default.
const HOTLINK_TCP_PROXY_CHUNK_SIZE_DEFAULT: usize = 60 * 1024;
const HOTLINK_TCP_PROXY_CHUNK_SIZE_MIN: usize = 4 * 1024;
const HOTLINK_TCP_PROXY_CHUNK_SIZE_MAX: usize = 1024 * 1024;
const HOTLINK_WEBRTC_READY_TIMEOUT: Duration = Duration::from_secs(10);
const HOTLINK_WEBRTC_FALLBACK_GRACE_DEFAULT_MS: u64 = 1500;
const HOTLINK_WEBRTC_BUFFERED_HIGH_DEFAULT: usize = 1024 * 1024;
const HOTLINK_WEBRTC_BUFFERED_HIGH_MAX: usize = 16 * 1024 * 1024;
const HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS_DEFAULT: u64 = 1500;
const HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS_MAX: u64 = 10_000;
const HOTLINK_WEBRTC_BACKPRESSURE_POLL_MS: u64 = 5;
const HOTLINK_WEBRTC_ERR_OUTBOUND_TOO_LARGE: &str =
    "outbound packet larger than maximum message size";
const HOTLINK_TELEMETRY_FLUSH_MS: u64 = 1000;
const HOTLINK_BENCH_STRICT_ENV: &str = "SYFTBOX_HOTLINK_BENCH_STRICT";

struct TcpMarkerInfo {
    port: u16,
    from_email: String,
    to_email: String,
    from_pid: usize,
    to_pid: usize,
}

#[derive(Clone)]
struct TcpProxyInfo {
    port: u16,
    from_email: String,
    to_email: String,
}

#[derive(Clone)]
struct TcpWriterEntry {
    writer: Arc<TokioMutex<tokio::net::tcp::OwnedWriteHalf>>,
    active: Arc<AtomicBool>,
}

#[derive(Clone)]
pub struct HotlinkManager {
    enabled: bool,
    datasites_root: PathBuf,
    ws: crate::client::WsHandle,
    sessions: Arc<RwLock<HashMap<String, HotlinkSession>>>,
    outbound: Arc<RwLock<HashMap<String, HotlinkOutbound>>>,
    outbound_by_path: Arc<RwLock<HashMap<String, String>>>,
    ipc_writers: Arc<TokioMutex<HashMap<PathBuf, Arc<TokioMutex<HotlinkIpcWriter>>>>>,
    local_readers: Arc<StdMutex<HashMap<PathBuf, ()>>>,
    tcp_writers: Arc<TokioMutex<HashMap<String, TcpWriterEntry>>>,
    tcp_writers_standby: Arc<TokioMutex<HashMap<String, TcpWriterEntry>>>,
    tcp_proxies: Arc<StdMutex<HashMap<String, TcpProxyInfo>>>,
    tcp_reorder: Arc<TokioMutex<HashMap<String, TcpReorderBuf>>>,
    tcp_bind_ip: Arc<StdMutex<Option<String>>>,
    local_email: Arc<StdMutex<Option<String>>>,
    telemetry: Arc<StdMutex<HotlinkTelemetryState>>,
    shutdown: Arc<Notify>,
}

#[allow(dead_code)]
struct HotlinkSession {
    id: String,
    path: String,
    remote_user: Option<String>,
    dir_abs: PathBuf,
    ipc_path: PathBuf,
    accept_path: PathBuf,
    webrtc: Option<WebRTCSession>,
}

#[allow(dead_code)]
struct HotlinkOutbound {
    id: String,
    path_key: String,
    accepted: bool,
    adopted_from_inbound: bool,
    seq: u64,
    notify: Arc<Notify>,
    rejected: Option<String>,
    ws_fallback_logged: bool,
    webrtc: Option<WebRTCSession>,
}

#[derive(Clone)]
struct WebRTCSession {
    peer_connection: Arc<RTCPeerConnection>,
    data_channel: Arc<TokioMutex<Option<Arc<RTCDataChannel>>>>,
    ready: Arc<Notify>,
    ready_flag: Arc<AtomicBool>,
    err: Arc<TokioMutex<Option<String>>>,
    pending_candidates: Arc<TokioMutex<Vec<RTCIceCandidateInit>>>,
    remote_desc_set: Arc<AtomicBool>,
}

struct HotlinkIpcWriter {
    listener: Option<HotlinkListener>,
    conn: Option<HotlinkStream>,
}

struct TcpReorderBuf {
    next_seq: u64,
    pending: BTreeMap<u64, Vec<u8>>,
}

#[derive(Default, Clone)]
struct HotlinkTelemetryCounters {
    tx_packets: u64,
    tx_bytes: u64,
    tx_p2p_packets: u64,
    tx_ws_packets: u64,
    tx_send_ms_total: u64,
    tx_send_ms_max: u64,
    rx_packets: u64,
    rx_bytes: u64,
    rx_write_ms_total: u64,
    rx_write_ms_max: u64,
    p2p_offers: u64,
    p2p_answers_ok: u64,
    p2p_answers_err: u64,
    ws_fallbacks: u64,
    strict_violations: u64,
}

struct HotlinkTelemetryState {
    started_ms: u64,
    last_flush_ms: u64,
    counters: HotlinkTelemetryCounters,
}

impl HotlinkManager {
    async fn promote_standby_tcp_writer(&self, key: &str) -> bool {
        let replacement = {
            let mut standby = self.tcp_writers_standby.lock().await;
            standby.remove(key)
        };
        if let Some(entry) = replacement {
            let mut writers = self.tcp_writers.lock().await;
            let active_ok = writers
                .get(key)
                .map(|e| e.active.load(Ordering::Relaxed))
                .unwrap_or(false);
            if !active_ok {
                writers.insert(key.to_string(), entry);
                if hotlink_debug_enabled() {
                    crate::logging::info(format!(
                        "hotlink tcp writer promoted standby key={}",
                        key
                    ));
                }
                return true;
            }
            drop(writers);
            let mut standby = self.tcp_writers_standby.lock().await;
            if !standby.contains_key(key) {
                standby.insert(key.to_string(), entry);
            }
        }
        false
    }

    async fn upsert_tcp_writer_key(
        &self,
        key: &str,
        writer: Arc<TokioMutex<tokio::net::tcp::OwnedWriteHalf>>,
        active: Arc<AtomicBool>,
    ) -> bool {
        let entry = TcpWriterEntry { writer, active };
        let mut writers = self.tcp_writers.lock().await;
        let current_active = writers
            .get(key)
            .map(|e| e.active.load(Ordering::Relaxed))
            .unwrap_or(false);
        if !current_active {
            writers.insert(key.to_string(), entry);
            drop(writers);
            let mut standby = self.tcp_writers_standby.lock().await;
            standby.remove(key);
            true
        } else {
            drop(writers);
            let mut standby = self.tcp_writers_standby.lock().await;
            standby.insert(key.to_string(), entry);
            false
        }
    }

    async fn clear_tcp_writer_key_if_current(
        &self,
        key: &str,
        current_writer: &Arc<TokioMutex<tokio::net::tcp::OwnedWriteHalf>>,
    ) {
        {
            let mut writers = self.tcp_writers.lock().await;
            if matches!(
                writers.get(key),
                Some(entry) if Arc::ptr_eq(&entry.writer, current_writer)
            ) {
                writers.remove(key);
            }
        }
        let _ = self.promote_standby_tcp_writer(key).await;
    }

    pub fn new(
        datasites_root: PathBuf,
        ws: crate::client::WsHandle,
        shutdown: Arc<Notify>,
    ) -> Self {
        // SYFTBOX_HOTLINK=1 enables hotlink mode (WebRTC P2P + WS fallback + TCP proxy).
        // Everything else is on by default when hotlink is enabled.
        let enabled = std::env::var("SYFTBOX_HOTLINK").ok().as_deref() == Some("1");
        if enabled {
            crate::logging::info("hotlink enabled: webrtc p2p + ws fallback + tcp proxy");
        }
        Self {
            enabled,
            datasites_root,
            ws,
            sessions: Arc::new(RwLock::new(HashMap::new())),
            outbound: Arc::new(RwLock::new(HashMap::new())),
            outbound_by_path: Arc::new(RwLock::new(HashMap::new())),
            ipc_writers: Arc::new(TokioMutex::new(HashMap::new())),
            local_readers: Arc::new(StdMutex::new(HashMap::new())),
            tcp_writers: Arc::new(TokioMutex::new(HashMap::new())),
            tcp_writers_standby: Arc::new(TokioMutex::new(HashMap::new())),
            tcp_proxies: Arc::new(StdMutex::new(HashMap::new())),
            tcp_reorder: Arc::new(TokioMutex::new(HashMap::new())),
            tcp_bind_ip: Arc::new(StdMutex::new(None)),
            local_email: Arc::new(StdMutex::new(None)),
            telemetry: Arc::new(StdMutex::new(HotlinkTelemetryState {
                started_ms: now_millis(),
                last_flush_ms: 0,
                counters: HotlinkTelemetryCounters::default(),
            })),
            shutdown,
        }
    }

    pub fn enabled(&self) -> bool {
        self.enabled
    }

    fn telemetry_mode(&self) -> &'static str {
        if !self.enabled {
            "disabled"
        } else if hotlink_bench_strict_enabled() {
            "hotlink_p2p_strict"
        } else {
            "hotlink_p2p"
        }
    }

    fn telemetry_path(&self) -> Option<PathBuf> {
        let owner = self.local_email.lock().unwrap().clone()?;
        Some(
            self.datasites_root
                .join(owner)
                .join(".syftbox")
                .join("hotlink_telemetry.json"),
        )
    }

    fn record_tx(&self, bytes: usize, send_ms: u64, via_p2p: bool) {
        {
            let mut telemetry = self.telemetry.lock().unwrap();
            let c = &mut telemetry.counters;
            c.tx_packets += 1;
            c.tx_bytes += bytes as u64;
            if via_p2p {
                c.tx_p2p_packets += 1;
            } else {
                c.tx_ws_packets += 1;
            }
            c.tx_send_ms_total += send_ms;
            c.tx_send_ms_max = c.tx_send_ms_max.max(send_ms);
        }
        self.flush_telemetry(false);
    }

    fn record_rx(&self, bytes: usize, write_ms: u64) {
        {
            let mut telemetry = self.telemetry.lock().unwrap();
            let c = &mut telemetry.counters;
            c.rx_packets += 1;
            c.rx_bytes += bytes as u64;
            c.rx_write_ms_total += write_ms;
            c.rx_write_ms_max = c.rx_write_ms_max.max(write_ms);
        }
        self.flush_telemetry(false);
    }

    fn record_ws_fallback(&self) {
        {
            let mut telemetry = self.telemetry.lock().unwrap();
            telemetry.counters.ws_fallbacks += 1;
        }
        self.flush_telemetry(false);
    }

    fn record_strict_violation(&self) {
        {
            let mut telemetry = self.telemetry.lock().unwrap();
            telemetry.counters.strict_violations += 1;
        }
        self.flush_telemetry(false);
    }

    fn record_p2p_offer(&self) {
        {
            let mut telemetry = self.telemetry.lock().unwrap();
            telemetry.counters.p2p_offers += 1;
        }
        self.flush_telemetry(false);
    }

    fn record_p2p_answer(&self, ok: bool) {
        {
            let mut telemetry = self.telemetry.lock().unwrap();
            if ok {
                telemetry.counters.p2p_answers_ok += 1;
            } else {
                telemetry.counters.p2p_answers_err += 1;
            }
        }
        self.flush_telemetry(false);
    }

    fn webrtc_state_str(w: &WebRTCSession) -> &'static str {
        if w.ready_flag.load(Ordering::Relaxed) {
            "connected"
        } else if w.err.try_lock().map(|e| e.is_some()).unwrap_or(false) {
            "error"
        } else {
            "connecting"
        }
    }

    fn peer_from_path(path: &str) -> &str {
        path.split('/').next().unwrap_or("?")
    }

    fn channel_from_path(path: &str) -> String {
        let parts: Vec<&str> = path.split('/').collect();
        for (i, p) in parts.iter().enumerate() {
            if *p == "_mpc" {
                if let Some(ch) = parts.get(i + 1) {
                    return ch.to_string();
                }
            }
        }
        parts.last().unwrap_or(&"?").to_string()
    }

    fn telemetry_snapshot_sync(&self) -> JsonValue {
        let sessions = self.sessions.try_read();
        let outbound = self.outbound.try_read();
        let tcp_proxies = self.tcp_proxies.lock().unwrap();
        let tcp_writers = self.tcp_writers.try_lock();

        let mut inbound_list = Vec::new();
        if let Ok(ref sess) = sessions {
            for (id, s) in sess.iter() {
                let short_id = &id[..8.min(id.len())];
                let peer = Self::peer_from_path(&s.path);
                let channel = Self::channel_from_path(&s.path);
                let wrtc = s
                    .webrtc
                    .as_ref()
                    .map(|w| Self::webrtc_state_str(w))
                    .unwrap_or("none");
                inbound_list.push(json!({
                    "sid": short_id,
                    "peer": peer,
                    "channel": channel,
                    "webrtc": wrtc,
                }));
            }
        }

        let mut outbound_list = Vec::new();
        if let Ok(ref out) = outbound {
            for (id, o) in out.iter() {
                let short_id = &id[..8.min(id.len())];
                let peer = Self::peer_from_path(&o.path_key);
                let channel = Self::channel_from_path(&o.path_key);
                let status = if o.rejected.is_some() {
                    "rejected"
                } else if o.accepted {
                    "accepted"
                } else {
                    "pending"
                };
                let wrtc = o
                    .webrtc
                    .as_ref()
                    .map(|w| Self::webrtc_state_str(w))
                    .unwrap_or("none");
                outbound_list.push(json!({
                    "sid": short_id,
                    "peer": peer,
                    "channel": channel,
                    "status": status,
                    "seq": o.seq,
                    "webrtc": wrtc,
                }));
            }
        }

        let mut proxy_list = Vec::new();
        for (key, info) in tcp_proxies.iter() {
            let has_writer = tcp_writers
                .as_ref()
                .ok()
                .and_then(|w| w.get(key))
                .map(|entry| entry.active.load(Ordering::Relaxed))
                .unwrap_or(false);
            proxy_list.push(json!({
                "port": info.port,
                "from": info.from_email,
                "to": info.to_email,
                "connected": has_writer,
            }));
        }

        let in_count = sessions.as_ref().map(|s| s.len()).unwrap_or(0);
        let out_count = outbound.as_ref().map(|o| o.len()).unwrap_or(0);
        let out_accepted = outbound
            .as_ref()
            .map(|o| o.values().filter(|v| v.accepted).count())
            .unwrap_or(0);
        let out_pending = out_count - out_accepted;

        // Count WebRTC connected sessions (both directions)
        let mut wrtc_connected = 0u64;
        if let Ok(ref sess) = sessions {
            for s in sess.values() {
                if s.webrtc
                    .as_ref()
                    .map(|w| w.ready_flag.load(Ordering::Relaxed))
                    .unwrap_or(false)
                {
                    wrtc_connected += 1;
                }
            }
        }
        if let Ok(ref out) = outbound {
            for o in out.values() {
                if o.webrtc
                    .as_ref()
                    .map(|w| w.ready_flag.load(Ordering::Relaxed))
                    .unwrap_or(false)
                {
                    wrtc_connected += 1;
                }
            }
        }

        json!({
            "inbound": in_count,
            "outbound_accepted": out_accepted,
            "outbound_pending": out_pending,
            "webrtc_connected": wrtc_connected,
            "tcp_proxies": proxy_list.len(),
            "sessions_in": inbound_list,
            "sessions_out": outbound_list,
            "proxies": proxy_list,
        })
    }

    fn flush_telemetry(&self, force: bool) {
        let now_ms = now_millis();
        let (payload, path, log_line) = {
            let mut telemetry = self.telemetry.lock().unwrap();
            if !force
                && telemetry.last_flush_ms != 0
                && now_ms.saturating_sub(telemetry.last_flush_ms) < HOTLINK_TELEMETRY_FLUSH_MS
            {
                return;
            }
            telemetry.last_flush_ms = now_ms;

            let c = telemetry.counters.clone();
            let tx_avg = if c.tx_packets > 0 {
                c.tx_send_ms_total as f64 / c.tx_packets as f64
            } else {
                0.0
            };
            let rx_avg = if c.rx_packets > 0 {
                c.rx_write_ms_total as f64 / c.rx_packets as f64
            } else {
                0.0
            };

            let snapshot = self.telemetry_snapshot_sync();

            fn fmt_bytes(b: u64) -> String {
                if b >= 1_048_576 {
                    format!("{:.1}MB", b as f64 / 1_048_576.0)
                } else if b >= 1024 {
                    format!("{:.1}KB", b as f64 / 1024.0)
                } else {
                    format!("{}B", b)
                }
            }

            let wrtc_up = snapshot["webrtc_connected"].as_u64().unwrap_or(0);
            let sess_in = snapshot["inbound"].as_u64().unwrap_or(0);
            let sess_out_ok = snapshot["outbound_accepted"].as_u64().unwrap_or(0);
            let sess_out_wait = snapshot["outbound_pending"].as_u64().unwrap_or(0);

            let p2p_label = if wrtc_up > 0 {
                format!("p2p:UP({})", wrtc_up)
            } else if c.p2p_offers > 0 {
                "p2p:negotiating".to_string()
            } else {
                "p2p:off".to_string()
            };

            let log = format!(
                "hotlink | ↑tx {} {}(p2p:{}/ws:{}) | ↓rx {} {} | {} | sess in:{} out:{}/{} wait",
                c.tx_packets,
                fmt_bytes(c.tx_bytes),
                c.tx_p2p_packets,
                c.tx_ws_packets,
                c.rx_packets,
                fmt_bytes(c.rx_bytes),
                p2p_label,
                sess_in,
                sess_out_ok,
                sess_out_wait,
            );

            let json = json!({
                "mode": self.telemetry_mode(),
                "bench_strict": hotlink_bench_strict_enabled(),
                "started_ms": telemetry.started_ms,
                "updated_ms": now_ms,
                "tx_packets": c.tx_packets,
                "tx_bytes": c.tx_bytes,
                "tx_p2p_packets": c.tx_p2p_packets,
                "tx_ws_packets": c.tx_ws_packets,
                "tx_avg_send_ms": tx_avg,
                "tx_max_send_ms": c.tx_send_ms_max,
                "rx_packets": c.rx_packets,
                "rx_bytes": c.rx_bytes,
                "rx_avg_write_ms": rx_avg,
                "rx_max_write_ms": c.rx_write_ms_max,
                "p2p_offers": c.p2p_offers,
                "p2p_answers_ok": c.p2p_answers_ok,
                "p2p_answers_err": c.p2p_answers_err,
                "ws_fallbacks": c.ws_fallbacks,
                "strict_violations": c.strict_violations,
                "webrtc_connected": wrtc_up,
                "live": snapshot,
            });
            (json.to_string(), self.telemetry_path(), log)
        };

        crate::logging::info(log_line);

        let Some(path) = path else {
            return;
        };
        if let Some(parent) = path.parent() {
            let _ = std::fs::create_dir_all(parent);
        }
        let _ = std::fs::write(path, payload);
    }

    pub fn start_local_discovery(&self, owner_email: String) {
        if !self.enabled {
            return;
        }
        {
            let mut guard = self.local_email.lock().unwrap();
            if guard.is_none() {
                *guard = Some(owner_email.clone());
            }
        }
        self.flush_telemetry(true);
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

    pub fn start_tcp_proxy_discovery(&self, _owner_email: String) {
        if !self.enabled {
            return;
        }
        let bind_ip = tcp_proxy_bind_ip();
        {
            let mut guard = self.tcp_bind_ip.lock().unwrap();
            if guard.is_none() {
                *guard = Some(bind_ip);
            }
        }
        {
            let mut guard = self.local_email.lock().unwrap();
            if guard.is_none() {
                *guard = Some(_owner_email.clone());
            }
        }
        self.flush_telemetry(true);
        let debug = std::env::var("SYFTBOX_HOTLINK_DEBUG").ok().as_deref() == Some("1");

        // Periodic telemetry flush so we always see state even when no tx/rx events fire.
        let tele_mgr = self.clone();
        tokio::spawn(async move {
            loop {
                if tele_mgr.shutdown.notified().now_or_never().is_some() {
                    break;
                }
                tokio::time::sleep(Duration::from_secs(3)).await;
                tele_mgr.flush_telemetry(true);
            }
        });

        let manager = self.clone();
        tokio::spawn(async move {
            let root = manager.datasites_root.clone();
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
                    if entry.file_name().to_string_lossy() != "stream.tcp" {
                        continue;
                    }
                    let marker_path = entry.path().to_path_buf();
                    let rel_marker = match marker_path.strip_prefix(&manager.datasites_root) {
                        Ok(rel) => rel.to_path_buf(),
                        Err(_) => continue,
                    };
                    let local_email = manager.local_email.lock().unwrap().clone();
                    let info = match read_tcp_marker_info(
                        &marker_path,
                        &rel_marker,
                        local_email.as_deref(),
                    )
                    .await
                    {
                        Ok(v) => v,
                        Err(_) => continue,
                    };
                    let channel_key = match canonical_tcp_key(&rel_marker, &info) {
                        Some(key) => key,
                        None => continue,
                    };
                    let mut proxies = manager.tcp_proxies.lock().unwrap();
                    if proxies.contains_key(&channel_key) {
                        continue;
                    }
                    if debug {
                        crate::logging::info(format!(
                            "hotlink tcp proxy: starting for {}",
                            marker_path.display()
                        ));
                    }
                    proxies.insert(
                        channel_key,
                        TcpProxyInfo {
                            port: info.port,
                            from_email: info.from_email.clone(),
                            to_email: info.to_email.clone(),
                        },
                    );
                    let manager_clone = manager.clone();
                    tokio::spawn(async move {
                        manager_clone.run_tcp_proxy(marker_path).await;
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

    async fn run_tcp_proxy(&self, marker_path: PathBuf) {
        let rel_marker = match marker_path.strip_prefix(&self.datasites_root) {
            Ok(rel) => rel.to_path_buf(),
            Err(_) => return,
        };
        let local_email = self.local_email.lock().unwrap().clone();
        let debug = hotlink_debug_enabled();
        let info =
            match read_tcp_marker_info(&marker_path, &rel_marker, local_email.as_deref()).await {
                Ok(v) => v,
                Err(err) => {
                    crate::logging::error(format!(
                        "hotlink tcp proxy: failed to read port {}: {err:?}",
                        marker_path.display()
                    ));
                    return;
                }
            };
        let channel_key = match canonical_tcp_key(&rel_marker, &info) {
            Some(key) => key,
            None => return,
        };
        let local_key = local_tcp_key(&rel_marker);
        let owner_from_key = owner_tcp_key(&rel_marker, &info.from_email);
        let owner_to_key = owner_tcp_key(&rel_marker, &info.to_email);
        // Use directional outbound path owned by local user so that the
        // server write-ACL check always passes.  Each party sends on its own
        // namespace (`local_email/_mpc/local_pid_to_peer_pid/...`).
        let peer_inbound_key = peer_inbound_tcp_key(&rel_marker, &info, local_email.as_deref());
        let outbound_key = local_outbound_tcp_key(&rel_marker, &info, local_email.as_deref())
            .unwrap_or_else(|| channel_key.clone());
        let port = info.port;

        let bind_ip = self
            .tcp_bind_ip
            .lock()
            .unwrap()
            .clone()
            .unwrap_or_else(|| "127.0.0.1".to_string());
        let addr = format!("{bind_ip}:{port}");
        if debug {
            crate::logging::info(format!(
                "hotlink tcp proxy: marker={} from={}({}) to={}({}) port={} bind={}",
                marker_path.display(),
                info.from_email,
                info.from_pid,
                info.to_email,
                info.to_pid,
                port,
                addr
            ));
        }
        let listener = match TcpListener::bind(&addr).await {
            Ok(l) => l,
            Err(err) => {
                crate::logging::error(format!("hotlink tcp proxy: bind failed {}: {err:?}", addr));
                return;
            }
        };

        loop {
            let accept = tokio::select! {
                _ = self.shutdown.notified() => break,
                res = listener.accept() => res,
            };
            let (stream, _) = match accept {
                Ok(v) => v,
                Err(_) => continue,
            };
            if debug {
                crate::logging::info(format!(
                    "hotlink tcp proxy: accepted on {} for {} (outbound={})",
                    addr, channel_key, outbound_key
                ));
            }
            let (mut reader, writer) = stream.into_split();

            let writer_arc = Arc::new(TokioMutex::new(writer));
            let writer_active = Arc::new(AtomicBool::new(true));
            let channel_mapped = self
                .upsert_tcp_writer_key(&channel_key, writer_arc.clone(), writer_active.clone())
                .await;
            let local_mapped = if let Some(local_key) = &local_key {
                self.upsert_tcp_writer_key(local_key, writer_arc.clone(), writer_active.clone())
                    .await
            } else {
                false
            };
            let owner_from_mapped = if let Some(owner_from_key) = &owner_from_key {
                self.upsert_tcp_writer_key(
                    owner_from_key,
                    writer_arc.clone(),
                    writer_active.clone(),
                )
                .await
            } else {
                false
            };
            let owner_to_mapped = if let Some(owner_to_key) = &owner_to_key {
                self.upsert_tcp_writer_key(owner_to_key, writer_arc.clone(), writer_active.clone())
                    .await
            } else {
                false
            };
            // Map writer to the path the PEER would use for their outbound
            // traffic.  When the peer sends on their own directional path
            // (e.g. `peer@.../peer_pid_to_local_pid/...`), incoming hotlink
            // data carries that path and we need a matching writer entry.
            let peer_mapped = if let Some(peer_key) = &peer_inbound_key {
                self.upsert_tcp_writer_key(peer_key, writer_arc.clone(), writer_active.clone())
                    .await
            } else {
                false
            };
            if debug {
                crate::logging::info(format!(
                    "hotlink tcp proxy: writer mapped keys channel={} local={:?} owner_from={:?} owner_to={:?} peer_inbound={:?} active(ch/loc/from/to/peer)={}/{}/{}/{}/{}",
                    channel_key,
                    local_key,
                    owner_from_key,
                    owner_to_key,
                    peer_inbound_key,
                    channel_mapped,
                    local_mapped,
                    owner_from_mapped,
                    owner_to_mapped,
                    peer_mapped
                ));
            }

            let manager = self.clone();
            let channel = channel_key.clone();
            let local_channel = local_key.clone();
            let owner_from_channel = owner_from_key.clone();
            let owner_to_channel = owner_to_key.clone();
            let peer_inbound_channel = peer_inbound_key.clone();
            let outbound_channel = outbound_key.clone();
            let writer_for_cleanup = writer_arc.clone();
            let writer_active_for_cleanup = writer_active.clone();
            tokio::spawn(async move {
                let mut buf = vec![0u8; hotlink_tcp_proxy_chunk_size()];
                let mut send_chunk_size = hotlink_tcp_proxy_chunk_size();
                loop {
                    let n = match reader.read(&mut buf).await {
                        Ok(0) => break,
                        Ok(n) => n,
                        Err(_) => break,
                    };
                    if hotlink_debug_enabled() {
                        crate::logging::info(format!(
                            "hotlink tcp proxy: recv bytes={} channel={} outbound={}",
                            n, channel, outbound_channel
                        ));
                    }
                    let mut off = 0usize;
                    while off < n {
                        let end = (off + send_chunk_size).min(n);
                        let chunk = buf[off..end].to_vec();
                        match manager
                            .send_best_effort_ordered(
                                outbound_channel.clone(),
                                "".to_string(),
                                chunk,
                            )
                            .await
                        {
                            Ok(()) => {
                                off = end;
                            }
                            Err(err) => {
                                if hotlink_is_packet_too_large_err(&err)
                                    && send_chunk_size > HOTLINK_TCP_PROXY_CHUNK_SIZE_MIN
                                {
                                    let next =
                                        (send_chunk_size / 2).max(HOTLINK_TCP_PROXY_CHUNK_SIZE_MIN);
                                    if next < send_chunk_size {
                                        send_chunk_size = next;
                                        crate::logging::info(format!(
                                            "hotlink tcp proxy: reducing send chunk size to {} after oversized packet error",
                                            send_chunk_size
                                        ));
                                        continue;
                                    }
                                }
                                crate::logging::error(format!(
                                    "hotlink tcp proxy: send failed (continuing): {err:?}"
                                ));
                                break;
                            }
                        }
                    }
                }
                if hotlink_debug_enabled() {
                    crate::logging::info(format!("hotlink tcp proxy: closed channel={}", channel));
                }
                writer_active_for_cleanup.store(false, Ordering::Relaxed);
                manager
                    .clear_tcp_writer_key_if_current(&channel, &writer_for_cleanup)
                    .await;
                if let Some(local_key) = local_channel {
                    manager
                        .clear_tcp_writer_key_if_current(&local_key, &writer_for_cleanup)
                        .await;
                }
                if let Some(owner_from_key) = owner_from_channel {
                    manager
                        .clear_tcp_writer_key_if_current(&owner_from_key, &writer_for_cleanup)
                        .await;
                }
                if let Some(owner_to_key) = owner_to_channel {
                    manager
                        .clear_tcp_writer_key_if_current(&owner_to_key, &writer_for_cleanup)
                        .await;
                }
                if let Some(peer_key) = peer_inbound_channel {
                    manager
                        .clear_tcp_writer_key_if_current(&peer_key, &writer_for_cleanup)
                        .await;
                }
            });
        }
    }

    pub async fn handle_open(&self, session_id: String, path: String, from: Option<String>) {
        if !self.enabled {
            return;
        }
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink open received: session={} path={} from={}",
                session_id,
                path,
                from.as_deref().unwrap_or("")
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
        if !is_tcp_proxy_path(&path) {
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
        }

        let accept_path = dir_abs.join(HOTLINK_ACCEPT_NAME);
        let session = HotlinkSession {
            id: session_id.clone(),
            path: path.clone(),
            remote_user: from.clone(),
            dir_abs,
            ipc_path: ipc_path.clone(),
            accept_path: accept_path.clone(),
            webrtc: None,
        };
        self.sessions
            .write()
            .await
            .insert(session_id.clone(), session);

        if is_tcp_proxy_path(&path) {
            crate::logging::info(format!(
                "hotlink sending accept for tcp proxy: session={} path={}",
                &session_id[..8],
                path
            ));
            if let Err(err) = self.send_accept(&session_id).await {
                crate::logging::error(format!(
                    "hotlink send accept failed: session={} err={err:?}",
                    &session_id[..8]
                ));
            }
            let mgr = self.clone();
            let sid = session_id.clone();
            tokio::spawn(async move {
                mgr.maybe_start_webrtc_offer(&sid).await;
            });
            return;
        }

        // Eagerly create the IPC listener so the test/SDK can connect immediately.
        if !is_tcp_proxy_path(&path) {
            if let Err(err) = self.ensure_ipc_listener(&ipc_path).await {
                crate::logging::error(format!("hotlink ensure listener failed: {err:?}"));
            }
        }

        if tokio::fs::metadata(&accept_path).await.is_ok() {
            if hotlink_debug_enabled() {
                crate::logging::info(format!(
                    "hotlink accept marker present: {}",
                    accept_path.display()
                ));
            }
            let _ = self.send_accept(&session_id).await;
            let mgr2 = self.clone();
            let sid2 = session_id.clone();
            tokio::spawn(async move {
                mgr2.maybe_start_webrtc_offer(&sid2).await;
            });
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
                    let mgr3 = manager.clone();
                    let sid3 = session_id.clone();
                    tokio::spawn(async move {
                        mgr3.maybe_start_webrtc_offer(&sid3).await;
                    });
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
        crate::logging::info(format!(
            "hotlink accept received: session={}",
            &session_id[..8.min(session_id.len())]
        ));
        let mut out = self.outbound.write().await;
        if let Some(entry) = out.get_mut(&session_id) {
            entry.accepted = true;
            entry.notify.notify_one();
        } else {
            crate::logging::info(format!(
                "hotlink accept for unknown outbound: session={}",
                &session_id[..8.min(session_id.len())]
            ));
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
            entry.notify.notify_one();
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
        // Clone session data and drop the read lock immediately.
        // Holding sessions.read() across handle_frame (which may block 30s in
        // get_tcp_writer_with_retry) prevents handle_open from acquiring
        // sessions.write(), deadlocking new session creation.
        let (session_path, ipc_path) = {
            let sessions = self.sessions.read().await;
            match sessions.get(&session_id) {
                Some(s) => (s.path.clone(), s.ipc_path.clone()),
                None => return,
            }
        };

        let frame = HotlinkFrame {
            path: if path.trim().is_empty() {
                session_path
            } else {
                path
            },
            etag,
            seq,
            payload,
        };

        self.handle_frame(&ipc_path, frame, Some(&session_id)).await;
    }

    async fn handle_frame(&self, ipc_path: &Path, frame: HotlinkFrame, session_id: Option<&str>) {
        let payload_len = frame.payload.len();
        let write_started = tokio::time::Instant::now();
        if is_tcp_proxy_path(&frame.path) {
            let writer = self.get_tcp_writer_with_retry(&frame.path).await;
            if let Some(writer) = writer {
                // Extract ready-to-write frames under the reorder lock, then release
                // the lock BEFORE doing TCP writes. This prevents a global deadlock
                // where a slow TCP consumer (e.g. aggregator trusted dealer) holds the
                // reorder lock across write_all, blocking all other paths.
                let ready_frames = {
                    let reorder_key = match session_id {
                        Some(sid) => format!("{}#{}", frame.path, sid),
                        None => frame.path.clone(),
                    };
                    let mut reorder = self.tcp_reorder.lock().await;
                    let buf = reorder.entry(reorder_key).or_insert_with(|| TcpReorderBuf {
                        next_seq: 1,
                        pending: BTreeMap::new(),
                    });
                    buf.pending.insert(frame.seq, frame.payload);
                    let mut ready = Vec::new();
                    while let Some(data) = buf.pending.remove(&buf.next_seq) {
                        ready.push(data);
                        buf.next_seq += 1;
                    }
                    ready
                };
                // reorder lock released — write to TCP without blocking other paths
                if !ready_frames.is_empty() {
                    let mut guard = writer.lock().await;
                    for data in ready_frames {
                        if let Err(err) = guard.write_all(&data).await {
                            crate::logging::error(format!("hotlink tcp write failed: {err:?}"));
                            break;
                        }
                        self.record_rx(data.len(), write_started.elapsed().as_millis() as u64);
                    }
                }
                return;
            }
            crate::logging::error(format!(
                "hotlink tcp write skipped: no writer for path={} after retries",
                frame.path
            ));
        }

        if let Err(err) = self.write_ipc(ipc_path, frame).await {
            crate::logging::error(format!("hotlink ipc write failed: {err:?}"));
        } else {
            self.record_rx(payload_len, write_started.elapsed().as_millis() as u64);
        }
    }

    async fn get_tcp_writer(
        &self,
        rel_path: &str,
    ) -> Option<Arc<TokioMutex<tokio::net::tcp::OwnedWriteHalf>>> {
        let writers = self.tcp_writers.lock().await;
        writers.get(rel_path).and_then(|entry| {
            if entry.active.load(Ordering::Relaxed) {
                Some(entry.writer.clone())
            } else {
                None
            }
        })
    }

    async fn get_tcp_writer_with_retry(
        &self,
        rel_path: &str,
    ) -> Option<Arc<TokioMutex<tokio::net::tcp::OwnedWriteHalf>>> {
        if let Some(w) = self.get_tcp_writer(rel_path).await {
            return Some(w);
        }
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink tcp writer not ready, waiting for path={}",
                rel_path
            ));
        }
        for _ in 0..20 {
            tokio::time::sleep(tokio::time::Duration::from_millis(250)).await;
            if let Some(w) = self.get_tcp_writer(rel_path).await {
                if hotlink_debug_enabled() {
                    crate::logging::info(format!(
                        "hotlink tcp writer ready after wait path={}",
                        rel_path
                    ));
                }
                return Some(w);
            }
        }
        None
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

    pub async fn handle_signal(&self, signal: crate::wsproto::HotlinkSignal) {
        if !self.enabled {
            return;
        }
        match signal.kind.as_str() {
            "sdp_offer" => {
                let mgr = self.clone();
                tokio::spawn(async move {
                    mgr.handle_sdp_offer(signal).await;
                });
            }
            "sdp_answer" => {
                let mgr = self.clone();
                tokio::spawn(async move {
                    mgr.handle_sdp_answer(signal).await;
                });
            }
            "ice_candidate" => {
                let mgr = self.clone();
                tokio::spawn(async move {
                    mgr.handle_ice_candidate(signal).await;
                });
            }
            "webrtc_error" => {
                crate::logging::error(format!(
                    "hotlink webrtc error: session={} error={}",
                    signal.session_id, signal.error
                ));
            }
            // backwards compat: ignore old quic signals
            "quic_offer" | "quic_answer" | "quic_error" => {}
            _ => {}
        }
    }

    async fn maybe_start_webrtc_offer(&self, session_id: &str) {
        let mut sessions = self.sessions.write().await;
        let session = match sessions.get_mut(session_id) {
            Some(s) => s,
            None => return,
        };
        if session.webrtc.is_some() {
            return;
        }
        crate::logging::info(format!(
            "hotlink webrtc offer starting: session={}",
            session_id
        ));

        let webrtc_session = match create_webrtc_session().await {
            Ok(s) => s,
            Err(err) => {
                crate::logging::error(format!("hotlink webrtc session create failed: {err:?}"));
                return;
            }
        };

        let dc = webrtc_session
            .peer_connection
            .create_data_channel("hotlink", None)
            .await;
        let dc = match dc {
            Ok(dc) => dc,
            Err(err) => {
                crate::logging::error(format!(
                    "hotlink webrtc data channel create failed: {err:?}"
                ));
                return;
            }
        };

        {
            let mut dc_guard = webrtc_session.data_channel.lock().await;
            *dc_guard = Some(dc.clone());
        }

        // Set up data channel callbacks
        let manager_for_open = self.clone();
        let session_id_for_open = session_id.to_string();
        let ready_for_open = webrtc_session.ready.clone();
        let ready_flag_for_open = webrtc_session.ready_flag.clone();
        dc.on_open(Box::new(move || {
            Box::pin(async move {
                crate::logging::info(format!(
                    "hotlink webrtc data channel open (offerer): session={}",
                    session_id_for_open
                ));
                webrtc_set_ready(&ready_for_open, &ready_flag_for_open);
                manager_for_open.record_p2p_answer(true);
            })
        }));

        let manager_for_msg = self.clone();
        let session_id_for_msg = session_id.to_string();
        dc.on_message(Box::new(move |msg: DataChannelMessage| {
            let mgr = manager_for_msg.clone();
            let sid = session_id_for_msg.clone();
            Box::pin(async move {
                mgr.handle_webrtc_message(&sid, &msg.data).await;
            })
        }));

        // Set up ICE candidate trickle
        let manager_for_ice = self.clone();
        let session_id_for_ice = session_id.to_string();
        webrtc_session
            .peer_connection
            .on_ice_candidate(Box::new(move |candidate| {
                let mgr = manager_for_ice.clone();
                let sid = session_id_for_ice.clone();
                Box::pin(async move {
                    if let Some(candidate) = candidate {
                        if let Ok(init) = candidate.to_json() {
                            let json = serde_json::to_string(&init).unwrap_or_default();
                            if let Err(err) =
                                mgr.send_signal(&sid, "ice_candidate", &[], &json, "").await
                            {
                                crate::logging::error(format!(
                                    "hotlink ice candidate send failed: {err:?}"
                                ));
                            }
                        }
                    }
                })
            }));

        session.webrtc = Some(webrtc_session.clone());
        drop(sessions);

        // Create and send SDP offer
        let offer = match webrtc_session.peer_connection.create_offer(None).await {
            Ok(o) => o,
            Err(err) => {
                crate::logging::error(format!("hotlink webrtc create offer failed: {err:?}"));
                return;
            }
        };
        if let Err(err) = webrtc_session
            .peer_connection
            .set_local_description(offer.clone())
            .await
        {
            crate::logging::error(format!("hotlink webrtc set local desc failed: {err:?}"));
            return;
        }

        let sdp_json = json!({"type": "offer", "sdp": offer.sdp}).to_string();
        if let Err(err) = self
            .send_signal(session_id, "sdp_offer", &[], &sdp_json, "")
            .await
        {
            crate::logging::error(format!(
                "hotlink webrtc offer send failed: session={} error={err:?}",
                session_id
            ));
        } else {
            self.record_p2p_offer();
            crate::logging::info(format!("hotlink webrtc offer sent: session={}", session_id));
        }
    }

    async fn handle_sdp_offer(&self, signal: crate::wsproto::HotlinkSignal) {
        let session_id = signal.session_id.clone();
        crate::logging::info(format!(
            "hotlink webrtc sdp_offer received: session={}",
            session_id
        ));

        let webrtc_session = {
            let out = self.outbound.read().await;
            out.get(&session_id).and_then(|o| o.webrtc.clone())
        };
        let webrtc_session = match webrtc_session {
            Some(s) => s,
            None => {
                crate::logging::info(format!(
                    "hotlink webrtc sdp_offer: no outbound session, creating answerer: session={}",
                    session_id
                ));
                let s = match create_webrtc_session().await {
                    Ok(s) => s,
                    Err(err) => {
                        crate::logging::error(format!(
                            "hotlink webrtc session create failed: {err:?}"
                        ));
                        return;
                    }
                };

                // Answerer receives data channel via on_data_channel
                let manager_for_dc = self.clone();
                let session_id_for_dc = session_id.clone();
                let dc_holder = s.data_channel.clone();
                let ready_for_dc = s.ready.clone();
                let ready_flag_for_dc = s.ready_flag.clone();
                s.peer_connection
                    .on_data_channel(Box::new(move |dc: Arc<RTCDataChannel>| {
                        let mgr = manager_for_dc.clone();
                        let sid = session_id_for_dc.clone();
                        let holder = dc_holder.clone();
                        let ready = ready_for_dc.clone();
                        let flag = ready_flag_for_dc.clone();
                        Box::pin(async move {
                            crate::logging::info(format!(
                            "hotlink webrtc data channel received (answerer): session={} label={}",
                            sid,
                            dc.label()
                        ));
                            {
                                let mut guard = holder.lock().await;
                                *guard = Some(dc.clone());
                            }

                            let mgr_open = mgr.clone();
                            let sid_open = sid.clone();
                            dc.on_open(Box::new(move || {
                                Box::pin(async move {
                                    crate::logging::info(format!(
                                        "hotlink webrtc data channel open (answerer): session={}",
                                        sid_open
                                    ));
                                    webrtc_set_ready(&ready, &flag);
                                    mgr_open.record_p2p_answer(true);
                                })
                            }));

                            dc.on_message(Box::new(move |msg: DataChannelMessage| {
                                let m = mgr.clone();
                                let s = sid.clone();
                                Box::pin(async move {
                                    m.handle_webrtc_message(&s, &msg.data).await;
                                })
                            }));
                        })
                    }));

                // Set up ICE candidate trickle
                let manager_for_ice = self.clone();
                let session_id_for_ice = session_id.clone();
                s.peer_connection
                    .on_ice_candidate(Box::new(move |candidate| {
                        let mgr = manager_for_ice.clone();
                        let sid = session_id_for_ice.clone();
                        Box::pin(async move {
                            if let Some(candidate) = candidate {
                                if let Ok(init) = candidate.to_json() {
                                    let json = serde_json::to_string(&init).unwrap_or_default();
                                    if let Err(err) =
                                        mgr.send_signal(&sid, "ice_candidate", &[], &json, "").await
                                    {
                                        crate::logging::error(format!(
                                            "hotlink ice candidate send failed: {err:?}"
                                        ));
                                    }
                                }
                            }
                        })
                    }));

                // Store in outbound
                {
                    let mut out = self.outbound.write().await;
                    if let Some(entry) = out.get_mut(&session_id) {
                        entry.webrtc = Some(s.clone());
                    }
                }

                // Also store in sessions for the receiver side
                {
                    let mut sessions = self.sessions.write().await;
                    if let Some(sess) = sessions.get_mut(&session_id) {
                        sess.webrtc = Some(s.clone());
                    }
                }

                s
            }
        };

        // Parse the SDP offer from token
        let sdp_value: serde_json::Value = match serde_json::from_str(&signal.token) {
            Ok(v) => v,
            Err(err) => {
                crate::logging::error(format!("hotlink webrtc sdp_offer parse failed: {err:?}"));
                return;
            }
        };
        let sdp_str = sdp_value
            .get("sdp")
            .and_then(|v| v.as_str())
            .unwrap_or_default();

        let offer = match RTCSessionDescription::offer(sdp_str.to_string()) {
            Ok(o) => o,
            Err(err) => {
                crate::logging::error(format!("hotlink webrtc offer construct failed: {err:?}"));
                return;
            }
        };

        if let Err(err) = webrtc_session
            .peer_connection
            .set_remote_description(offer)
            .await
        {
            crate::logging::error(format!("hotlink webrtc set remote desc failed: {err:?}"));
            return;
        }
        webrtc_session.remote_desc_set.store(true, Ordering::SeqCst);

        // Flush any buffered ICE candidates
        {
            let mut pending = webrtc_session.pending_candidates.lock().await;
            for candidate in pending.drain(..) {
                if let Err(err) = webrtc_session
                    .peer_connection
                    .add_ice_candidate(candidate)
                    .await
                {
                    crate::logging::error(format!(
                        "hotlink webrtc add buffered ice candidate failed: {err:?}"
                    ));
                }
            }
        }

        // Create and send SDP answer
        let answer = match webrtc_session.peer_connection.create_answer(None).await {
            Ok(a) => a,
            Err(err) => {
                crate::logging::error(format!("hotlink webrtc create answer failed: {err:?}"));
                return;
            }
        };
        if let Err(err) = webrtc_session
            .peer_connection
            .set_local_description(answer.clone())
            .await
        {
            crate::logging::error(format!(
                "hotlink webrtc set local desc (answer) failed: {err:?}"
            ));
            return;
        }

        let sdp_json = json!({"type": "answer", "sdp": answer.sdp}).to_string();
        if let Err(err) = self
            .send_signal(&session_id, "sdp_answer", &[], &sdp_json, "")
            .await
        {
            crate::logging::error(format!(
                "hotlink webrtc answer send failed: session={} err={err:?}",
                session_id
            ));
        } else {
            self.record_p2p_answer(true);
            crate::logging::info(format!(
                "hotlink webrtc answer sent: session={}",
                session_id
            ));
        }
    }

    async fn handle_sdp_answer(&self, signal: crate::wsproto::HotlinkSignal) {
        let session_id = signal.session_id.clone();
        crate::logging::info(format!(
            "hotlink webrtc sdp_answer received: session={}",
            session_id
        ));

        let webrtc_session = {
            let sessions = self.sessions.read().await;
            sessions.get(&session_id).and_then(|s| s.webrtc.clone())
        };
        let Some(webrtc_session) = webrtc_session else {
            crate::logging::error(format!(
                "hotlink webrtc sdp_answer: no session found: session={}",
                session_id
            ));
            return;
        };

        let sdp_value: serde_json::Value = match serde_json::from_str(&signal.token) {
            Ok(v) => v,
            Err(err) => {
                crate::logging::error(format!("hotlink webrtc sdp_answer parse failed: {err:?}"));
                return;
            }
        };
        let sdp_str = sdp_value
            .get("sdp")
            .and_then(|v| v.as_str())
            .unwrap_or_default();

        let answer = match RTCSessionDescription::answer(sdp_str.to_string()) {
            Ok(a) => a,
            Err(err) => {
                crate::logging::error(format!("hotlink webrtc answer construct failed: {err:?}"));
                return;
            }
        };

        if let Err(err) = webrtc_session
            .peer_connection
            .set_remote_description(answer)
            .await
        {
            crate::logging::error(format!(
                "hotlink webrtc set remote desc (answer) failed: {err:?}"
            ));
            return;
        }
        webrtc_session.remote_desc_set.store(true, Ordering::SeqCst);

        // Flush any buffered ICE candidates
        {
            let mut pending = webrtc_session.pending_candidates.lock().await;
            for candidate in pending.drain(..) {
                if let Err(err) = webrtc_session
                    .peer_connection
                    .add_ice_candidate(candidate)
                    .await
                {
                    crate::logging::error(format!(
                        "hotlink webrtc add buffered ice candidate failed: {err:?}"
                    ));
                }
            }
        }

        crate::logging::info(format!(
            "hotlink webrtc answer applied: session={}",
            session_id
        ));
    }

    async fn handle_ice_candidate(&self, signal: crate::wsproto::HotlinkSignal) {
        let session_id = signal.session_id.clone();
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink webrtc ice_candidate received: session={}",
                session_id
            ));
        }

        // Try sessions first (receiver side), then outbound (sender side)
        let webrtc_session = {
            let sessions = self.sessions.read().await;
            sessions.get(&session_id).and_then(|s| s.webrtc.clone())
        };
        let webrtc_session = match webrtc_session {
            Some(s) => s,
            None => {
                let out = self.outbound.read().await;
                match out.get(&session_id).and_then(|o| o.webrtc.clone()) {
                    Some(s) => s,
                    None => return,
                }
            }
        };

        let candidate: RTCIceCandidateInit = match serde_json::from_str(&signal.token) {
            Ok(c) => c,
            Err(err) => {
                crate::logging::error(format!(
                    "hotlink webrtc ice_candidate parse failed: {err:?}"
                ));
                return;
            }
        };

        if !webrtc_session.remote_desc_set.load(Ordering::SeqCst) {
            // Buffer until remote description is set
            let mut pending = webrtc_session.pending_candidates.lock().await;
            pending.push(candidate);
            return;
        }

        if let Err(err) = webrtc_session
            .peer_connection
            .add_ice_candidate(candidate)
            .await
        {
            crate::logging::error(format!("hotlink webrtc add ice candidate failed: {err:?}"));
        }
    }

    async fn try_send_webrtc(
        &self,
        session_id: &str,
        rel_path: &str,
        etag: &str,
        seq: u64,
        payload: &[u8],
        wait: bool,
    ) -> Result<Option<()>> {
        let webrtc = {
            let out = self.outbound.read().await;
            out.get(session_id).and_then(|o| o.webrtc.clone())
        };
        let Some(webrtc) = webrtc else {
            return Ok(None);
        };
        if !webrtc.ready_flag.load(Ordering::SeqCst) {
            if wait {
                if timeout(HOTLINK_WEBRTC_READY_TIMEOUT, webrtc.ready.notified())
                    .await
                    .is_err()
                {
                    return Err(anyhow::anyhow!("webrtc wait timeout"));
                }
            } else {
                return Ok(None);
            }
        }
        {
            let err_guard = webrtc.err.lock().await;
            if let Some(err) = err_guard.as_ref() {
                return Err(anyhow::anyhow!(err.clone()));
            }
        }
        let dc = {
            let dc_guard = webrtc.data_channel.lock().await;
            let Some(dc) = dc_guard.as_ref() else {
                return Ok(None);
            };
            dc.clone()
        };
        self.wait_for_webrtc_send_capacity(&dc).await?;
        let frame = HotlinkFrame {
            path: rel_path.to_string(),
            etag: etag.to_string(),
            seq,
            payload: payload.to_vec(),
        };
        let bytes = crate::hotlink::encode_hotlink_frame(&frame);
        dc.send(&Bytes::from(bytes)).await?;
        Ok(Some(()))
    }

    async fn wait_for_webrtc_send_capacity(&self, dc: &Arc<RTCDataChannel>) -> Result<()> {
        let high_water = hotlink_webrtc_buffered_high();
        let wait_budget = hotlink_webrtc_backpressure_wait();
        let deadline = tokio::time::Instant::now() + wait_budget;
        loop {
            let buffered = dc.buffered_amount().await;
            if buffered <= high_water {
                return Ok(());
            }
            if tokio::time::Instant::now() >= deadline {
                anyhow::bail!(
                    "webrtc send buffer still high: buffered={} high={} wait_ms={}",
                    buffered,
                    high_water,
                    wait_budget.as_millis()
                );
            }
            tokio::time::sleep(Duration::from_millis(HOTLINK_WEBRTC_BACKPRESSURE_POLL_MS)).await;
        }
    }

    async fn mark_ws_fallback(&self, session_id: &str, rel_path: &str) {
        let mut out = self.outbound.write().await;
        if let Some(entry) = out.get_mut(session_id) {
            if !entry.ws_fallback_logged {
                entry.ws_fallback_logged = true;
                self.record_ws_fallback();
                crate::logging::info(format!(
                    "hotlink p2p not ready, using ws fallback: session={} path={}",
                    session_id, rel_path
                ));
            }
        }
    }

    async fn handle_webrtc_message(&self, session_id: &str, data: &[u8]) {
        let frame = match crate::hotlink::parse_hotlink_frame_from_bytes(data) {
            Ok(f) => f,
            Err(err) => {
                crate::logging::error(format!("hotlink webrtc frame parse failed: {err:?}"));
                return;
            }
        };
        let ipc_path = {
            let sessions = self.sessions.read().await;
            sessions.get(session_id).map(|s| s.ipc_path.clone())
        };
        if let Some(ipc_path) = ipc_path {
            self.handle_frame(&ipc_path, frame, Some(session_id)).await;
            return;
        }

        // For tcp-proxy traffic, routing is path-based via tcp_writers and does
        // not require session ipc metadata. Keep processing even if the session
        // id is not in self.sessions (e.g. answerer side created from outbound).
        if is_tcp_proxy_path(&frame.path) {
            if hotlink_debug_enabled() {
                crate::logging::info(format!(
                    "hotlink webrtc message for unknown session, routing by tcp path: session={} path={}",
                    session_id, frame.path
                ));
            }
            self.handle_frame(Path::new(""), frame, Some(session_id))
                .await;
            return;
        }

        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink webrtc message dropped: unknown session={} path={}",
                session_id, frame.path
            ));
        }
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

    async fn send_best_effort_ordered(
        &self,
        rel_path: String,
        etag: String,
        payload: Vec<u8>,
    ) -> Result<()> {
        if hotlink_debug_enabled() {
            crate::logging::info(format!(
                "hotlink best effort (ordered): path={} bytes={} enabled={} eligible={}",
                rel_path,
                payload.len(),
                self.enabled,
                is_hotlink_eligible(&rel_path)
            ));
        }
        if !self.enabled {
            return Ok(());
        }
        if !is_hotlink_eligible(&rel_path) || payload.is_empty() {
            if hotlink_debug_enabled() {
                crate::logging::info(format!(
                    "hotlink best effort (ordered) skipped: path={} bytes={}",
                    rel_path,
                    payload.len()
                ));
            }
            return Ok(());
        }
        self.send_hotlink(rel_path, etag, payload).await
    }

    fn is_p2p_only(&self) -> bool {
        let p2p_only = std::env::var("SYFTBOX_HOTLINK_P2P_ONLY").ok();
        if matches!(p2p_only.as_deref(), Some("1")) {
            return true;
        }
        matches!(
            std::env::var("SYFTBOX_HOTLINK_QUIC_ONLY").ok().as_deref(),
            Some("1")
        )
    }

    async fn send_hotlink(&self, rel_path: String, etag: String, payload: Vec<u8>) -> Result<()> {
        let payload_len = payload.len();
        let p2p_only = self.is_p2p_only();
        let bench_strict = hotlink_bench_strict_enabled();
        let path_key = parent_path(&rel_path);
        let owner_is_local = self
            .local_email
            .lock()
            .unwrap()
            .as_deref()
            .map(|local| path_key.split('/').next() == Some(local))
            .unwrap_or(false);

        let mut existing_session = {
            let guard = self.outbound_by_path.read().await;
            guard.get(&path_key).cloned()
        };
        // In WS-fallback mode, outbound session ownership must stay local
        // (server forwards hotlink data only from session.FromUser).
        // Any previously adopted inbound session is invalid for WS sending.
        if !p2p_only {
            if let Some(id) = existing_session.clone() {
                let adopted = {
                    let out = self.outbound.read().await;
                    out.get(&id)
                        .map(|e| e.adopted_from_inbound)
                        .unwrap_or(false)
                };
                if adopted {
                    self.remove_outbound(&id).await;
                    existing_session = None;
                }
            }
        }
        // Self-routed TCP sessions can occur when owner(path) == local.
        // Those sessions are locally initiated (present in `outbound`) and
        // also reflected in `sessions` with the same sid/path. Ignore them.
        if is_tcp_proxy_path(&rel_path) && owner_is_local {
            if let Some(id) = existing_session.clone() {
                let local_email = self.local_email.lock().unwrap().clone();
                let self_routed = {
                    let out = self.outbound.read().await;
                    let sess = self.sessions.read().await;
                    let local_outbound = out
                        .get(&id)
                        .map(|e| e.path_key == path_key)
                        .unwrap_or(false);
                    let inbound_same_path = sess.get(&id).map(|s| {
                        let same_path = parent_path(&s.path) == path_key;
                        let self_peer = match (s.remote_user.as_deref(), local_email.as_deref()) {
                            (Some(remote), Some(local)) => remote == local,
                            _ => false,
                        };
                        same_path && self_peer
                    });
                    local_outbound && inbound_same_path.unwrap_or(false)
                };
                if self_routed {
                    if hotlink_debug_enabled() {
                        crate::logging::info(format!(
                            "hotlink tcp self-route guard: discarding existing self session={} path={}",
                            &id[..8.min(id.len())],
                            rel_path
                        ));
                    }
                    existing_session = None;
                }
            }
        }
        let mut inbound_session = if p2p_only && existing_session.is_none() {
            let local_email = self.local_email.lock().unwrap().clone();
            let sessions = self.sessions.read().await;
            let outbound = self.outbound.read().await;
            sessions.iter().find_map(|(sid, sess)| {
                if parent_path(&sess.path) != path_key {
                    return None;
                }
                if is_tcp_proxy_path(&rel_path)
                    && owner_is_local
                    && matches!(
                        (sess.remote_user.as_deref(), local_email.as_deref()),
                        (Some(remote), Some(local)) if remote == local
                    )
                {
                    return None;
                }
                // Ignore locally-initiated sessions when owner(path) == local:
                // those are self-routes, not peer inbound channels.
                if is_tcp_proxy_path(&rel_path) && owner_is_local {
                    if let Some(entry) = outbound.get(sid) {
                        if entry.path_key == path_key && !entry.adopted_from_inbound {
                            return None;
                        }
                    }
                }
                Some((sid.clone(), sess.webrtc.clone()))
            })
        } else {
            None
        };

        // For TCP proxy paths owned by local, opening a new outbound can self-route.
        // Give the real peer a short window to open first, then reuse that inbound session.
        if existing_session.is_none()
            && inbound_session.is_none()
            && is_tcp_proxy_path(&rel_path)
            && owner_is_local
            && p2p_only
        {
            let mut waited = Duration::from_millis(0);
            while waited < HOTLINK_ACCEPT_TIMEOUT {
                tokio::time::sleep(HOTLINK_IPC_RETRY_DELAY).await;
                waited += HOTLINK_IPC_RETRY_DELAY;
                let local_email = self.local_email.lock().unwrap().clone();
                let sessions = self.sessions.read().await;
                let outbound = self.outbound.read().await;
                inbound_session = sessions.iter().find_map(|(sid, sess)| {
                    if parent_path(&sess.path) != path_key {
                        return None;
                    }
                    if matches!(
                        (sess.remote_user.as_deref(), local_email.as_deref()),
                        (Some(remote), Some(local)) if remote == local
                    ) {
                        return None;
                    }
                    if let Some(entry) = outbound.get(sid) {
                        if entry.path_key == path_key && !entry.adopted_from_inbound {
                            return None;
                        }
                    }
                    Some((sid.clone(), sess.webrtc.clone()))
                });
                if inbound_session.is_some() {
                    if hotlink_debug_enabled() {
                        crate::logging::info(format!(
                            "hotlink tcp self-route guard: reused inbound after {}ms path={}",
                            waited.as_millis(),
                            rel_path
                        ));
                    }
                    break;
                }
            }
        }

        let (session_id, is_new) = if let Some(id) = existing_session {
            (id, false)
        } else if let Some((id, webrtc)) = inbound_session {
            if hotlink_debug_enabled() {
                crate::logging::info(format!(
                    "hotlink reusing inbound session: session={} path={}",
                    &id[..8.min(id.len())],
                    rel_path
                ));
            }
            let outbound = HotlinkOutbound {
                id: id.clone(),
                path_key: path_key.clone(),
                accepted: true,
                adopted_from_inbound: true,
                seq: 0,
                notify: Arc::new(Notify::new()),
                rejected: None,
                ws_fallback_logged: false,
                webrtc,
            };
            self.outbound.write().await.insert(id.clone(), outbound);
            self.outbound_by_path
                .write()
                .await
                .insert(path_key.clone(), id.clone());
            (id, false)
        } else {
            let id = Uuid::new_v4().to_string();
            crate::logging::info(format!(
                "hotlink session new: session={} path={}",
                &id[..8],
                rel_path
            ));
            let outbound = HotlinkOutbound {
                id: id.clone(),
                path_key: path_key.clone(),
                accepted: false,
                adopted_from_inbound: false,
                seq: 0,
                notify: Arc::new(Notify::new()),
                rejected: None,
                ws_fallback_logged: false,
                webrtc: None,
            };
            self.outbound.write().await.insert(id.clone(), outbound);
            self.outbound_by_path
                .write()
                .await
                .insert(path_key.clone(), id.clone());
            let preferred_to = {
                if is_tcp_proxy_path(&rel_path) {
                    self.preferred_remote_for_tcp_path(&rel_path)
                } else {
                    let local_email = self.local_email.lock().unwrap().clone();
                    let sessions = self.sessions.read().await;
                    sessions.values().find_map(|s| {
                        if parent_path(&s.path) != path_key {
                            return None;
                        }
                        let remote = s.remote_user.as_ref()?.to_string();
                        if local_email.as_deref() == Some(remote.as_str()) {
                            return None;
                        }
                        Some(remote)
                    })
                }
            };
            if let Err(err) = self.send_open(&id, rel_path.clone(), preferred_to).await {
                crate::logging::error(format!("hotlink send open failed: {err:?}"));
                self.remove_outbound(&id).await;
                return Err(err);
            }
            (id, true)
        };

        if hotlink_debug_enabled() && is_tcp_proxy_path(&rel_path) {
            crate::logging::info(format!(
                "hotlink send path selected: path={} session={} is_new={} p2p_only={}",
                rel_path,
                &session_id[..8.min(session_id.len())],
                is_new,
                p2p_only
            ));
        }

        // Only block-wait for accept on newly created sessions.
        // For existing sessions, check non-blocking so we don't stall the TCP read loop.
        if is_new {
            if self.wait_for_accept(&session_id).await {
                crate::logging::info(format!(
                    "hotlink session accepted: session={}",
                    &session_id[..8]
                ));
            } else {
                if bench_strict {
                    let err = format!(
                        "hotlink strict: accept timeout session={} path={}",
                        &session_id[..8.min(session_id.len())],
                        rel_path
                    );
                    self.record_strict_violation();
                    crate::logging::error(err.clone());
                    self.remove_outbound(&session_id).await;
                    return Err(anyhow::anyhow!(err));
                }
                crate::logging::info(format!(
                    "hotlink accept timeout ({}s), sending anyway: session={} path={}",
                    HOTLINK_ACCEPT_TIMEOUT.as_secs(),
                    &session_id[..8],
                    rel_path
                ));
            }
            // In p2p_only mode, also wait for the WebRTC session to be established
            // before sending the first packet. The SDP offer/answer exchange happens
            // concurrently with accept — typically ready within ~500ms.
            if self.is_p2p_only() {
                self.wait_for_outbound_webrtc(&session_id).await;
            }
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

        if hotlink_debug_enabled() && is_tcp_proxy_path(&rel_path) && (seq <= 3 || seq % 500 == 0) {
            crate::logging::info(format!(
                "hotlink send data: path={} session={} seq={} bytes={}",
                rel_path,
                &session_id[..8.min(session_id.len())],
                seq,
                payload_len
            ));
        }

        // In fallback mode, allow a short grace period for SDP/ICE completion
        // before dropping to websocket, which reduces avoidable ws fallbacks.
        if !p2p_only && seq <= 3 {
            let _ = self
                .wait_for_outbound_webrtc_for(&session_id, hotlink_ws_fallback_grace(), false)
                .await;
        }

        {
            let send_started = tokio::time::Instant::now();
            // In p2p_only mode, wait for WebRTC data channel to be ready.
            // In normal mode, try non-blocking and fall back to WS.
            match self
                .try_send_webrtc(&session_id, &rel_path, &etag, seq, &payload, p2p_only)
                .await
            {
                Ok(Some(())) => {
                    self.record_tx(payload_len, send_started.elapsed().as_millis() as u64, true);
                    return Ok(());
                }
                Ok(None) => {
                    if bench_strict {
                        let err = format!(
                            "hotlink strict: websocket fallback attempted (not ready) session={} path={} seq={}",
                            &session_id[..8.min(session_id.len())],
                            rel_path,
                            seq
                        );
                        self.record_strict_violation();
                        crate::logging::error(err.clone());
                        return Err(anyhow::anyhow!(err));
                    }
                    if !p2p_only {
                        self.mark_ws_fallback(&session_id, &rel_path).await;
                    }
                }
                Err(e) => {
                    if bench_strict {
                        let err = format!(
                            "hotlink strict: websocket fallback attempted after webrtc error session={} path={} seq={} err={:?}",
                            &session_id[..8.min(session_id.len())],
                            rel_path,
                            seq,
                            e
                        );
                        self.record_strict_violation();
                        crate::logging::error(err.clone());
                        return Err(anyhow::anyhow!(err));
                    }
                    if hotlink_debug_enabled() {
                        crate::logging::info(format!("hotlink webrtc send err: {e:?}"));
                    }
                    if !p2p_only {
                        self.mark_ws_fallback(&session_id, &rel_path).await;
                    }
                }
            }
        }

        if p2p_only {
            let err = format!(
                "hotlink p2p_only send failed: webrtc not ready after wait session={} path={} seq={}",
                &session_id[..8.min(session_id.len())],
                rel_path,
                seq
            );
            crate::logging::error(err.clone());
            return Err(anyhow::anyhow!(err));
        }

        let send_started = tokio::time::Instant::now();
        if let Err(err) = self
            .send_data(&session_id, seq, rel_path, etag, payload)
            .await
        {
            crate::logging::error(format!("hotlink send data failed: {err:?}"));
            return Err(err);
        }
        self.record_tx(
            payload_len,
            send_started.elapsed().as_millis() as u64,
            false,
        );
        Ok(())
    }

    async fn wait_for_outbound_webrtc(&self, session_id: &str) {
        let _ = self
            .wait_for_outbound_webrtc_for(session_id, HOTLINK_WEBRTC_READY_TIMEOUT, true)
            .await;
    }

    async fn wait_for_outbound_webrtc_for(
        &self,
        session_id: &str,
        timeout_dur: Duration,
        log_timeout: bool,
    ) -> bool {
        let deadline = tokio::time::Instant::now() + timeout_dur;
        loop {
            {
                let out = self.outbound.read().await;
                if let Some(entry) = out.get(session_id) {
                    if let Some(ref w) = entry.webrtc {
                        if w.ready_flag.load(Ordering::SeqCst) {
                            if log_timeout {
                                crate::logging::info(format!(
                                    "hotlink webrtc ready for outbound: session={}",
                                    &session_id[..8.min(session_id.len())]
                                ));
                            }
                            return true;
                        }
                    }
                } else {
                    return false;
                }
            }
            if tokio::time::Instant::now() >= deadline {
                if log_timeout {
                    crate::logging::info(format!(
                        "hotlink webrtc ready timeout ({}s): session={}",
                        timeout_dur.as_secs(),
                        &session_id[..8.min(session_id.len())]
                    ));
                }
                return false;
            }
            tokio::time::sleep(Duration::from_millis(100)).await;
        }
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

    fn preferred_remote_for_tcp_path(&self, rel_path: &str) -> Option<String> {
        let local_email = self.local_email.lock().unwrap().clone()?;
        let proxies = self.tcp_proxies.lock().unwrap();
        let info = proxies.get(rel_path)?;
        if info.from_email == local_email {
            Some(info.to_email.clone())
        } else if info.to_email == local_email {
            Some(info.from_email.clone())
        } else {
            None
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
            // Retry accept with longer overall timeout - syqure may not have opened
            // its TCP socket yet when QUIC data arrives.
            let deadline = tokio::time::Instant::now() + HOTLINK_IPC_WRITE_TIMEOUT;
            loop {
                match listener.accept(HOTLINK_CONNECT_TIMEOUT).await {
                    Ok(conn) => {
                        writer.conn = Some(conn);
                        break;
                    }
                    Err(_) => {
                        if tokio::time::Instant::now() >= deadline {
                            anyhow::bail!(
                                "ipc accept timeout after {:?}",
                                HOTLINK_IPC_WRITE_TIMEOUT
                            );
                        }
                        if hotlink_debug_enabled() {
                            crate::logging::info(format!(
                                "hotlink ipc accept retry: {}",
                                marker_path.display()
                            ));
                        }
                        tokio::time::sleep(HOTLINK_IPC_RETRY_DELAY).await;
                    }
                }
            }
        }
        if let Some(conn) = writer.conn.as_mut() {
            if conn.write_frame(&frame).await.is_err() {
                writer.conn = None;
                anyhow::bail!("ipc write failed");
            }
        }
        Ok(())
    }

    async fn send_open(&self, session_id: &str, path: String, to: Option<String>) -> Result<()> {
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
                    "dat": {"sid": session_id, "pth": path, "to": to}
                });
                self.ws
                    .send_ws(Message::Text(serde_json::to_string(&payload)?))
                    .await?;
            }
            Encoding::MsgPack => {
                let open = MsgpackHotlinkOpen {
                    session_id: session_id.to_string(),
                    path,
                    from: None,
                    to,
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

    async fn send_signal(
        &self,
        session_id: &str,
        kind: &str,
        addrs: &[String],
        token: &str,
        error: &str,
    ) -> Result<()> {
        let id = Uuid::new_v4().to_string();
        match self.ws.encoding() {
            Encoding::Json => {
                let payload = serde_json::json!({
                    "id": id,
                    "typ": 14,
                    "dat": {
                        "sid": session_id,
                        "knd": kind,
                        "adr": addrs,
                        "tok": token,
                        "err": error
                    }
                });
                self.ws
                    .send_ws(Message::Text(serde_json::to_string(&payload)?))
                    .await?;
            }
            Encoding::MsgPack => {
                let signal = MsgpackHotlinkSignal {
                    session_id: session_id.to_string(),
                    kind: kind.to_string(),
                    addrs: addrs.to_vec(),
                    token: token.to_string(),
                    error: error.to_string(),
                };
                let bin = crate::wsproto::encode_msgpack(&id, 14, &signal)?;
                self.ws.send_ws(Message::Binary(bin)).await?;
            }
        }
        Ok(())
    }

    #[allow(dead_code)]
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

fn tcp_proxy_bind_ip() -> String {
    std::env::var("SYFTBOX_HOTLINK_TCP_PROXY_ADDR").unwrap_or_else(|_| "127.0.0.1".to_string())
}

fn hotlink_tcp_proxy_chunk_size() -> usize {
    std::env::var("SYFTBOX_HOTLINK_TCP_PROXY_CHUNK_SIZE")
        .ok()
        .and_then(|v| v.trim().parse::<usize>().ok())
        .map(|v| {
            v.clamp(
                HOTLINK_TCP_PROXY_CHUNK_SIZE_MIN,
                HOTLINK_TCP_PROXY_CHUNK_SIZE_MAX,
            )
        })
        .unwrap_or(HOTLINK_TCP_PROXY_CHUNK_SIZE_DEFAULT)
}

fn hotlink_webrtc_buffered_high() -> usize {
    std::env::var("SYFTBOX_HOTLINK_WEBRTC_BUFFERED_HIGH")
        .ok()
        .and_then(|v| v.trim().parse::<usize>().ok())
        .map(|v| v.min(HOTLINK_WEBRTC_BUFFERED_HIGH_MAX))
        .unwrap_or(HOTLINK_WEBRTC_BUFFERED_HIGH_DEFAULT)
}

fn hotlink_webrtc_backpressure_wait() -> Duration {
    let ms = std::env::var("SYFTBOX_HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS")
        .ok()
        .and_then(|v| v.trim().parse::<u64>().ok())
        .unwrap_or(HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS_DEFAULT)
        .min(HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS_MAX);
    Duration::from_millis(ms)
}

fn hotlink_is_packet_too_large_err(err: &anyhow::Error) -> bool {
    err.to_string()
        .to_ascii_lowercase()
        .contains(HOTLINK_WEBRTC_ERR_OUTBOUND_TOO_LARGE)
}

fn hotlink_ws_fallback_grace() -> Duration {
    let ms = std::env::var("SYFTBOX_HOTLINK_WS_FALLBACK_GRACE_MS")
        .ok()
        .and_then(|v| v.trim().parse::<u64>().ok())
        .unwrap_or(HOTLINK_WEBRTC_FALLBACK_GRACE_DEFAULT_MS)
        .min(10_000);
    Duration::from_millis(ms)
}

fn hotlink_bench_strict_enabled() -> bool {
    matches!(
        std::env::var(HOTLINK_BENCH_STRICT_ENV)
            .ok()
            .map(|v| v.trim().to_ascii_lowercase())
            .as_deref(),
        Some("1" | "true" | "yes" | "on")
    )
}

fn is_tcp_proxy_path(path: &str) -> bool {
    path.ends_with(HOTLINK_TCP_SUFFIX)
}

fn canonical_tcp_key(rel_marker: &Path, info: &TcpMarkerInfo) -> Option<String> {
    let (min_pid, max_pid) = if info.from_pid <= info.to_pid {
        (info.from_pid, info.to_pid)
    } else {
        (info.to_pid, info.from_pid)
    };
    let min_email = std::cmp::min(&info.from_email, &info.to_email);
    let mut comps: Vec<String> = rel_marker
        .components()
        .map(|c| c.as_os_str().to_string_lossy().to_string())
        .collect();
    if comps.len() < 2 {
        return None;
    }
    let last = comps.last()?.as_str();
    if last != "stream.tcp" {
        return None;
    }
    comps[0] = min_email.to_string();
    let channel_idx = comps.len().checked_sub(2)?;
    comps[channel_idx] = format!("{}_to_{}", min_pid, max_pid);
    let last_idx = comps.len().checked_sub(1)?;
    comps[last_idx] = HOTLINK_TCP_SUFFIX.to_string();
    Some(comps.join("/"))
}

fn local_tcp_key(rel_marker: &Path) -> Option<String> {
    let mut comps: Vec<String> = rel_marker
        .components()
        .map(|c| c.as_os_str().to_string_lossy().to_string())
        .collect();
    if comps.len() < 2 {
        return None;
    }
    let last = comps.last()?.as_str();
    if last != "stream.tcp" {
        return None;
    }
    let last_idx = comps.len().checked_sub(1)?;
    comps[last_idx] = HOTLINK_TCP_SUFFIX.to_string();
    Some(comps.join("/"))
}

fn owner_tcp_key(rel_marker: &Path, owner_email: &str) -> Option<String> {
    let (from_pid, to_pid) = parse_channel_pids(rel_marker)?;
    let (min_pid, max_pid) = if from_pid <= to_pid {
        (from_pid, to_pid)
    } else {
        (to_pid, from_pid)
    };
    let mut comps: Vec<String> = rel_marker
        .components()
        .map(|c| c.as_os_str().to_string_lossy().to_string())
        .collect();
    if comps.len() < 2 {
        return None;
    }
    let last = comps.last()?.as_str();
    if last != "stream.tcp" {
        return None;
    }
    comps[0] = owner_email.to_string();
    let channel_idx = comps.len().checked_sub(2)?;
    comps[channel_idx] = format!("{}_to_{}", min_pid, max_pid);
    let last_idx = comps.len().checked_sub(1)?;
    comps[last_idx] = HOTLINK_TCP_SUFFIX.to_string();
    Some(comps.join("/"))
}

/// Compute the hotlink path the PEER would use for their outbound traffic
/// on the reverse direction of this channel.
///
/// In the 3-party case each direction has its own directory: party 1→2 uses
/// `1_to_2/` while party 2→1 uses `2_to_1/`.  The peer sends on
/// `{peer_email}/.../{to_pid}_to_{from_pid}/stream.tcp.request`, so we
/// swap both the email prefix AND the PIDs in the channel directory.
///
/// For the 2-party case (both parties share `0_to_1`), `owner_to_key`
/// already covers the same-dir match, so the reversed key here is harmless
/// (it simply won't match any incoming data).
fn peer_inbound_tcp_key(
    rel_marker: &Path,
    info: &TcpMarkerInfo,
    local_email: Option<&str>,
) -> Option<String> {
    let local_email = local_email?;
    let peer_email = if info.from_email == local_email {
        &info.to_email
    } else if info.to_email == local_email {
        &info.from_email
    } else {
        return None;
    };
    let mut comps: Vec<String> = rel_marker
        .components()
        .map(|c| c.as_os_str().to_string_lossy().to_string())
        .collect();
    if comps.len() < 2 {
        return None;
    }
    let last = comps.last()?.as_str();
    if last != "stream.tcp" {
        return None;
    }
    comps[0] = peer_email.to_string();
    // Reverse the channel directory: 1_to_2 → 2_to_1
    let channel_idx = comps.len().checked_sub(2)?;
    comps[channel_idx] = format!("{}_to_{}", info.to_pid, info.from_pid);
    let last_idx = comps.len().checked_sub(1)?;
    comps[last_idx] = HOTLINK_TCP_SUFFIX.to_string();
    Some(comps.join("/"))
}

/// Compute the local directional outbound path for this channel.
/// Since the marker lives under the local user's datasite, `local_tcp_key`
/// already produces the correct path (`{local_email}/.../{channel_dir}/stream.tcp.request`).
/// This wrapper exists only for symmetry with `peer_inbound_tcp_key`.
fn local_outbound_tcp_key(
    rel_marker: &Path,
    _info: &TcpMarkerInfo,
    local_email: Option<&str>,
) -> Option<String> {
    let _local_email = local_email?;
    local_tcp_key(rel_marker)
}

fn parse_channel_pids(rel_marker: &Path) -> Option<(usize, usize)> {
    let parent = rel_marker.parent()?;
    let channel_dir = parent.file_name()?.to_string_lossy();
    let mut parts = channel_dir.split("_to_");
    let from = parts.next()?.parse::<usize>().ok()?;
    let to = parts.next()?.parse::<usize>().ok()?;
    Some((from, to))
}

async fn read_tcp_marker_info(
    marker_path: &Path,
    rel_marker: &Path,
    local_email: Option<&str>,
) -> Result<TcpMarkerInfo> {
    let data = tokio::fs::read(marker_path).await?;
    let txt = String::from_utf8_lossy(&data).trim().to_string();
    if txt.is_empty() {
        anyhow::bail!("stream.tcp marker is empty");
    }
    let (from_pid, to_pid) = parse_channel_pids(rel_marker)
        .ok_or_else(|| anyhow::anyhow!("unable to parse channel pids"))?;
    let mut from_email = String::new();
    let mut to_email = String::new();
    let mut port: Option<u16> = None;
    let mut ports_map: Option<HashMap<String, u64>> = None;

    if let Ok(json) = serde_json::from_str::<JsonValue>(&txt) {
        if let Some(v) = json.get("from").and_then(|v| v.as_str()) {
            from_email = v.to_string();
        }
        if let Some(v) = json.get("to").and_then(|v| v.as_str()) {
            to_email = v.to_string();
        }
        if let Some(ports) = json.get("ports").and_then(|v| v.as_object()) {
            let mut map = HashMap::new();
            for (k, v) in ports {
                if let Some(port) = v.as_u64() {
                    map.insert(k.to_string(), port);
                }
            }
            ports_map = Some(map);
        }
        port = json
            .get("port")
            .and_then(|v| v.as_u64())
            .and_then(|v| u16::try_from(v).ok());
    } else if let Ok(p) = txt.parse::<u16>() {
        port = Some(p);
    } else if let Some(raw_port) = txt.rsplit(':').next() {
        if let Ok(p) = raw_port.parse::<u16>() {
            port = Some(p);
        }
    }

    if let (Some(ports), Some(email)) = (ports_map, local_email) {
        if let Some(v) = ports.get(email).copied() {
            port = u16::try_from(v).ok();
        } else {
            anyhow::bail!("stream.tcp marker missing port for local email");
        }
    }
    let port = port.ok_or_else(|| anyhow::anyhow!("missing port in stream.tcp"))?;
    if from_email.is_empty() || to_email.is_empty() {
        anyhow::bail!("stream.tcp marker missing from/to emails");
    }

    Ok(TcpMarkerInfo {
        port,
        from_email,
        to_email,
        from_pid,
        to_pid,
    })
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
pub async fn connect_ipc(marker_path: &Path, timeout_dur: Duration) -> Result<HotlinkStream> {
    dial_hotlink_ipc(marker_path, timeout_dur).await
}

fn ice_servers() -> Vec<RTCIceServer> {
    let servers_str = std::env::var("SYFTBOX_HOTLINK_ICE_SERVERS")
        .or_else(|_| std::env::var("SYFTBOX_HOTLINK_STUN_SERVER"))
        .unwrap_or_else(|_| "stun:stun.l.google.com:19302".to_string());
    let username = std::env::var("SYFTBOX_HOTLINK_TURN_USER").unwrap_or_default();
    let credential = std::env::var("SYFTBOX_HOTLINK_TURN_PASS").unwrap_or_default();
    servers_str
        .split(',')
        .filter(|s| !s.trim().is_empty())
        .map(|url| {
            let url = url.trim().to_string();
            let is_turn = url.starts_with("turn:") || url.starts_with("turns:");
            RTCIceServer {
                urls: vec![url],
                username: if is_turn {
                    username.clone()
                } else {
                    String::new()
                },
                credential: if is_turn {
                    credential.clone()
                } else {
                    String::new()
                },
            }
        })
        .collect()
}

async fn create_webrtc_session() -> Result<WebRTCSession> {
    let mut m = MediaEngine::default();
    m.register_default_codecs()?;
    let mut registry = Registry::new();
    registry = register_default_interceptors(registry, &mut m)?;
    let se = SettingEngine::default();
    let api = APIBuilder::new()
        .with_media_engine(m)
        .with_interceptor_registry(registry)
        .with_setting_engine(se)
        .build();

    let config = RTCConfiguration {
        ice_servers: ice_servers(),
        ..Default::default()
    };

    let peer_connection = Arc::new(api.new_peer_connection(config).await?);

    Ok(WebRTCSession {
        peer_connection,
        data_channel: Arc::new(TokioMutex::new(None)),
        ready: Arc::new(Notify::new()),
        ready_flag: Arc::new(AtomicBool::new(false)),
        err: Arc::new(TokioMutex::new(None)),
        pending_candidates: Arc::new(TokioMutex::new(Vec::new())),
        remote_desc_set: Arc::new(AtomicBool::new(false)),
    })
}

fn webrtc_set_ready(ready: &Arc<Notify>, flag: &Arc<AtomicBool>) {
    if !flag.swap(true, Ordering::SeqCst) {
        ready.notify_one();
    }
}

fn now_millis() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::{Mutex, OnceLock};

    fn env_lock() -> std::sync::MutexGuard<'static, ()> {
        static LOCK: OnceLock<Mutex<()>> = OnceLock::new();
        LOCK.get_or_init(|| Mutex::new(())).lock().unwrap()
    }

    #[test]
    fn tcp_proxy_chunk_size_defaults_and_clamps() {
        let _guard = env_lock();
        std::env::remove_var("SYFTBOX_HOTLINK_TCP_PROXY_CHUNK_SIZE");
        assert_eq!(
            hotlink_tcp_proxy_chunk_size(),
            HOTLINK_TCP_PROXY_CHUNK_SIZE_DEFAULT
        );

        std::env::set_var("SYFTBOX_HOTLINK_TCP_PROXY_CHUNK_SIZE", "1");
        assert_eq!(
            hotlink_tcp_proxy_chunk_size(),
            HOTLINK_TCP_PROXY_CHUNK_SIZE_MIN
        );

        std::env::set_var(
            "SYFTBOX_HOTLINK_TCP_PROXY_CHUNK_SIZE",
            (HOTLINK_TCP_PROXY_CHUNK_SIZE_MAX * 2).to_string(),
        );
        assert_eq!(
            hotlink_tcp_proxy_chunk_size(),
            HOTLINK_TCP_PROXY_CHUNK_SIZE_MAX
        );

        std::env::set_var("SYFTBOX_HOTLINK_TCP_PROXY_CHUNK_SIZE", "262144");
        assert_eq!(hotlink_tcp_proxy_chunk_size(), 262_144);
    }

    #[test]
    fn webrtc_buffered_high_defaults_and_clamps() {
        let _guard = env_lock();
        std::env::remove_var("SYFTBOX_HOTLINK_WEBRTC_BUFFERED_HIGH");
        assert_eq!(
            hotlink_webrtc_buffered_high(),
            HOTLINK_WEBRTC_BUFFERED_HIGH_DEFAULT
        );

        std::env::set_var(
            "SYFTBOX_HOTLINK_WEBRTC_BUFFERED_HIGH",
            (HOTLINK_WEBRTC_BUFFERED_HIGH_MAX * 2).to_string(),
        );
        assert_eq!(
            hotlink_webrtc_buffered_high(),
            HOTLINK_WEBRTC_BUFFERED_HIGH_MAX
        );

        std::env::set_var("SYFTBOX_HOTLINK_WEBRTC_BUFFERED_HIGH", "2097152");
        assert_eq!(hotlink_webrtc_buffered_high(), 2 * 1024 * 1024);
    }

    #[test]
    fn webrtc_backpressure_wait_defaults_and_clamps() {
        let _guard = env_lock();
        std::env::remove_var("SYFTBOX_HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS");
        assert_eq!(
            hotlink_webrtc_backpressure_wait(),
            Duration::from_millis(HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS_DEFAULT)
        );

        std::env::set_var("SYFTBOX_HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS", "999999");
        assert_eq!(
            hotlink_webrtc_backpressure_wait(),
            Duration::from_millis(HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS_MAX)
        );

        std::env::set_var("SYFTBOX_HOTLINK_WEBRTC_BACKPRESSURE_WAIT_MS", "250");
        assert_eq!(
            hotlink_webrtc_backpressure_wait(),
            Duration::from_millis(250)
        );
    }

    #[test]
    fn packet_too_large_error_detection() {
        let err = anyhow::anyhow!("outbound packet larger than maximum message size");
        assert!(hotlink_is_packet_too_large_err(&err));

        let wrapped = anyhow::anyhow!(
            "hotlink strict: websocket fallback attempted after webrtc error: outbound packet larger than maximum message size"
        );
        assert!(hotlink_is_packet_too_large_err(&wrapped));

        let other = anyhow::anyhow!("webrtc wait timeout");
        assert!(!hotlink_is_packet_too_large_err(&other));
    }
}
