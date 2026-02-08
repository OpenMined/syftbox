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
use futures_util::FutureExt;
use md5::compute as md5_compute;
use quinn::crypto::rustls::{QuicClientConfig, QuicServerConfig};
use quinn::{ClientConfig, Endpoint, EndpointConfig, ServerConfig, TokioRuntime};
use rcgen::generate_simple_self_signed;
use rustls::client::danger::{HandshakeSignatureValid, ServerCertVerified, ServerCertVerifier};
use rustls::pki_types::{CertificateDer, PrivatePkcs8KeyDer, ServerName, UnixTime};
use serde_json::json;
use serde_json::Value as JsonValue;
use std::collections::{BTreeMap, HashMap};
use std::fmt::Write as _;
use std::net::{IpAddr, Ipv4Addr, SocketAddr, ToSocketAddrs};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex as StdMutex, Once};
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use tokio::io::{AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpListener;
use tokio::sync::{Mutex as TokioMutex, Notify, RwLock};
use tokio::time::timeout;
use tokio_tungstenite::tungstenite::Message;
use uuid::Uuid;

const HOTLINK_ACCEPT_TIMEOUT: Duration = Duration::from_secs(5);
const HOTLINK_ACCEPT_DELAY: Duration = Duration::from_millis(200);
const HOTLINK_CONNECT_TIMEOUT: Duration = Duration::from_secs(5);
const HOTLINK_IPC_WRITE_TIMEOUT: Duration = Duration::from_secs(30);
const HOTLINK_IPC_RETRY_DELAY: Duration = Duration::from_millis(100);
const HOTLINK_TCP_BIND_TIMEOUT: Duration = Duration::from_secs(30);
const HOTLINK_TCP_BIND_RETRY_DELAY: Duration = Duration::from_millis(250);
const HOTLINK_TCP_SUFFIX: &str = "stream.tcp.request";
const HOTLINK_QUIC_DIAL_TIMEOUT: Duration = Duration::from_millis(1500);
const HOTLINK_QUIC_ACCEPT_TIMEOUT: Duration = Duration::from_millis(2500);
const HOTLINK_QUIC_ALPN: &[u8] = b"syftbox-hotlink";
const HOTLINK_STUN_SERVER_ENV: &str = "SYFTBOX_HOTLINK_STUN_SERVER";
const HOTLINK_STUN_TIMEOUT: Duration = Duration::from_millis(1200);
const STUN_BINDING_REQUEST: u16 = 0x0001;
const STUN_BINDING_SUCCESS: u16 = 0x0101;
const STUN_MAGIC_COOKIE: u32 = 0x2112_A442;
const STUN_MAPPED_ADDRESS: u16 = 0x0001;
const STUN_XOR_MAPPED_ADDRESS: u16 = 0x0020;
static RUSTLS_PROVIDER_INIT: Once = Once::new();
// Keep below common WebRTC data-channel max message limits.
const HOTLINK_TCP_PROXY_CHUNK_SIZE: usize = 14 * 1024;
const HOTLINK_WEBRTC_READY_TIMEOUT: Duration = Duration::from_secs(10);
const HOTLINK_TELEMETRY_FLUSH_MS: u64 = 1000;

struct TcpMarkerInfo {
    port: u16,
    from_email: String,
    to_email: String,
    from_pid: usize,
    to_pid: usize,
}

fn debug_writer_keys(keys: impl Iterator<Item = String>) -> String {
    let mut all: Vec<String> = keys.collect();
    all.sort();
    all.join(",")
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
    socket_only: bool,
    quic_enabled: bool,
    quic_only: bool,
    datasites_root: PathBuf,
    ws: crate::client::WsHandle,
    sessions: Arc<RwLock<HashMap<String, HotlinkSession>>>,
    outbound: Arc<RwLock<HashMap<String, HotlinkOutbound>>>,
    outbound_by_path: Arc<RwLock<HashMap<String, String>>>,
    ipc_writers: Arc<TokioMutex<HashMap<PathBuf, Arc<TokioMutex<HotlinkIpcWriter>>>>>,
    local_readers: Arc<StdMutex<HashMap<PathBuf, ()>>>,
    tcp_writers: Arc<TokioMutex<HashMap<String, TcpWriterEntry>>>,
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
    dir_abs: PathBuf,
    ipc_path: PathBuf,
    accept_path: PathBuf,
    quic: Option<HotlinkQuicSession>,
}

#[allow(dead_code)]
struct HotlinkOutbound {
    id: String,
    path_key: String,
    accepted: bool,
    seq: u64,
    notify: Arc<Notify>,
    rejected: Option<String>,
    ws_fallback_logged: bool,
    quic: Option<HotlinkQuicOutbound>,
}

#[derive(Clone)]
struct HotlinkQuicSession {
    endpoint: Endpoint,
    state: Arc<TokioMutex<HotlinkQuicState>>,
    ready: Arc<Notify>,
    ready_flag: Arc<AtomicBool>,
}

#[derive(Clone)]
struct HotlinkQuicOutbound {
    endpoint: Endpoint,
    state: Arc<TokioMutex<HotlinkQuicState>>,
    ready: Arc<Notify>,
    ready_flag: Arc<AtomicBool>,
}

struct HotlinkQuicState {
    send: Option<quinn::SendStream>,
    err: Option<String>,
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
    tx_quic_packets: u64,
    tx_ws_packets: u64,
    tx_send_ms_total: u64,
    tx_send_ms_max: u64,
    rx_packets: u64,
    rx_bytes: u64,
    rx_write_ms_total: u64,
    rx_write_ms_max: u64,
    quic_offers: u64,
    quic_answers_ok: u64,
    quic_answers_err: u64,
    ws_fallbacks: u64,
}

struct HotlinkTelemetryState {
    started_ms: u64,
    last_flush_ms: u64,
    counters: HotlinkTelemetryCounters,
}

impl HotlinkManager {
    pub fn new(
        datasites_root: PathBuf,
        ws: crate::client::WsHandle,
        shutdown: Arc<Notify>,
    ) -> Self {
        // Hotlink defaults to ON. Set SYFTBOX_HOTLINK=0 to disable.
        let enabled = std::env::var("SYFTBOX_HOTLINK")
            .ok()
            .as_deref()
            .map(|v| v != "0")
            .unwrap_or(true);
        let socket_only = std::env::var("SYFTBOX_HOTLINK_SOCKET_ONLY")
            .ok()
            .as_deref()
            .map(|v| v != "0")
            .unwrap_or(true);
        let quic_enabled = std::env::var("SYFTBOX_HOTLINK_QUIC")
            .ok()
            .as_deref()
            .map(|v| v != "0")
            .unwrap_or(true);
        let quic_only = std::env::var("SYFTBOX_HOTLINK_QUIC_ONLY").ok().as_deref() == Some("1");
        let tcp_proxy = tcp_proxy_enabled();
        if enabled {
            crate::logging::info(format!(
                "hotlink config: socket_only={} quic_enabled={} quic_only={} tcp_proxy={}",
                socket_only, quic_enabled, quic_only, tcp_proxy
            ));
        }
        // Note: QUIC works for both socket-only IPC and TCP proxy modes.
        // This warning helps catch cases where TCP proxy is expected but not enabled.
        if quic_only && !tcp_proxy && !socket_only && enabled {
            crate::logging::info(
                "WARN: SYFTBOX_HOTLINK_QUIC_ONLY=1 but SYFTBOX_HOTLINK_TCP_PROXY is not enabled. \
                 If using MPC/syqure, TCP proxy must be enabled. Set SYFTBOX_HOTLINK_TCP_PROXY=1.",
            );
        }
        Self {
            enabled,
            socket_only,
            quic_enabled,
            quic_only,
            datasites_root,
            ws,
            sessions: Arc::new(RwLock::new(HashMap::new())),
            outbound: Arc::new(RwLock::new(HashMap::new())),
            outbound_by_path: Arc::new(RwLock::new(HashMap::new())),
            ipc_writers: Arc::new(TokioMutex::new(HashMap::new())),
            local_readers: Arc::new(StdMutex::new(HashMap::new())),
            tcp_writers: Arc::new(TokioMutex::new(HashMap::new())),
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

    #[allow(dead_code)]
    pub fn socket_only(&self) -> bool {
        self.socket_only
    }

    fn telemetry_mode(&self) -> &'static str {
        if !self.enabled {
            "disabled"
        } else if self.quic_only {
            "hotlink_quic_only"
        } else if self.quic_enabled {
            "hotlink_quic_pref"
        } else {
            "hotlink_ws_only"
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

    fn record_tx(&self, bytes: usize, send_ms: u64, via_quic: bool) {
        {
            let mut telemetry = self.telemetry.lock().unwrap();
            let c = &mut telemetry.counters;
            c.tx_packets += 1;
            c.tx_bytes += bytes as u64;
            if via_quic {
                c.tx_quic_packets += 1;
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

    fn record_quic_offer(&self) {
        {
            let mut telemetry = self.telemetry.lock().unwrap();
            telemetry.counters.quic_offers += 1;
        }
        self.flush_telemetry(false);
    }

    fn record_quic_answer(&self, ok: bool) {
        {
            let mut telemetry = self.telemetry.lock().unwrap();
            if ok {
                telemetry.counters.quic_answers_ok += 1;
            } else {
                telemetry.counters.quic_answers_err += 1;
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
                let wrtc = s.webrtc.as_ref().map(|w| Self::webrtc_state_str(w)).unwrap_or("none");
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
                let wrtc = o.webrtc.as_ref().map(|w| Self::webrtc_state_str(w)).unwrap_or("none");
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
        let out_accepted = outbound.as_ref().map(|o| o.values().filter(|v| v.accepted).count()).unwrap_or(0);
        let out_pending = out_count - out_accepted;

        // Count WebRTC connected sessions (both directions)
        let mut wrtc_connected = 0u64;
        if let Ok(ref sess) = sessions {
            for s in sess.values() {
                if s.webrtc.as_ref().map(|w| w.ready_flag.load(Ordering::Relaxed)).unwrap_or(false) {
                    wrtc_connected += 1;
                }
            }
        }
        if let Ok(ref out) = outbound {
            for o in out.values() {
                if o.webrtc.as_ref().map(|w| w.ready_flag.load(Ordering::Relaxed)).unwrap_or(false) {
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
                "started_ms": telemetry.started_ms,
                "updated_ms": now_ms,
                "tx_packets": c.tx_packets,
                "tx_bytes": c.tx_bytes,
                "tx_quic_packets": c.tx_quic_packets,
                "tx_ws_packets": c.tx_ws_packets,
                "tx_avg_send_ms": tx_avg,
                "tx_max_send_ms": c.tx_send_ms_max,
                "rx_packets": c.rx_packets,
                "rx_bytes": c.rx_bytes,
                "rx_avg_write_ms": rx_avg,
                "rx_max_write_ms": c.rx_write_ms_max,
                "quic_offers": c.quic_offers,
                "quic_answers_ok": c.quic_answers_ok,
                "quic_answers_err": c.quic_answers_err,
                "ws_fallbacks": c.ws_fallbacks,
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
        if !self.enabled || !self.socket_only {
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
        if !self.enabled || !tcp_proxy_enabled() {
            return;
        }
        let bind_ip = tcp_proxy_bind_ip(&_owner_email);
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
                    proxies.insert(channel_key, TcpProxyInfo {
                        port: info.port,
                        from_email: info.from_email.clone(),
                        to_email: info.to_email.clone(),
                    });
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
        // Use canonical channel key for outbound frames. This matches ACL ownership for
        // _mpc channel paths and avoids permission-denied opens on peer-owned paths.
        let outbound_key = channel_key.clone();
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
                "hotlink tcp proxy: marker={} rel={} from={}({}) to={}({}) port={} bind={} canonical_key={} local_key={}",
                marker_path.display(),
                rel_marker.display(),
                info.from_email,
                info.from_pid,
                info.to_email,
                info.to_pid,
                port,
                addr,
                channel_key,
                local_key.clone().unwrap_or_else(|| "<none>".to_string())
            ));
        }
        let bind_deadline = tokio::time::Instant::now() + HOTLINK_TCP_BIND_TIMEOUT;
        let mut next_bind_log = tokio::time::Instant::now();
        let listener = loop {
            if self.shutdown.notified().now_or_never().is_some() {
                self.clear_tcp_proxy_state(&channel_key, local_key.as_deref())
                    .await;
                return;
            }
            match TcpListener::bind(&addr).await {
                Ok(listener) => break listener,
                Err(err) => {
                    if tokio::time::Instant::now() >= bind_deadline {
                        crate::logging::error(format!(
                            "hotlink tcp proxy: bind timeout {} after {:?}: {err:?}",
                            addr, HOTLINK_TCP_BIND_TIMEOUT
                        ));
                        self.clear_tcp_proxy_state(&channel_key, local_key.as_deref())
                            .await;
                        return;
                    }
                    if debug || tokio::time::Instant::now() >= next_bind_log {
                        crate::logging::info(format!(
                            "hotlink tcp proxy: bind retry {}: {err:?}",
                            addr
                        ));
                        next_bind_log = tokio::time::Instant::now() + Duration::from_secs(2);
                    }
                    tokio::time::sleep(HOTLINK_TCP_BIND_RETRY_DELAY).await;
                }
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
            // Do not clobber an existing active mapping for this channel.
            // Desktop watchdog/health probes can briefly connect and close;
            // replacing the writer here can blackhole in-flight remote frames.
            let writer_active = Arc::new(AtomicBool::new(true));
            {
                let mut writers = self.tcp_writers.lock().await;
                let existing_active = writers
                    .get(&channel_key)
                    .map(|entry| entry.active.load(Ordering::Relaxed))
                    .unwrap_or(false);
                if !existing_active {
                    writers.insert(
                        channel_key.clone(),
                        TcpWriterEntry {
                            writer: writer_arc.clone(),
                            active: writer_active.clone(),
                        },
                    );
                }
                if let Some(local_key) = &local_key {
                    let local_existing_active = writers
                        .get(local_key)
                        .map(|entry| entry.active.load(Ordering::Relaxed))
                        .unwrap_or(false);
                    if !local_existing_active {
                        writers.insert(
                            local_key.clone(),
                            TcpWriterEntry {
                                writer: writer_arc.clone(),
                                active: writer_active.clone(),
                            },
                        );
                    }
                }
                if let Some(owner_from_key) = &owner_from_key {
                    let owner_from_existing_active = writers
                        .get(owner_from_key)
                        .map(|entry| entry.active.load(Ordering::Relaxed))
                        .unwrap_or(false);
                    if !owner_from_existing_active {
                        writers.insert(
                            owner_from_key.clone(),
                            TcpWriterEntry {
                                writer: writer_arc.clone(),
                                active: writer_active.clone(),
                            },
                        );
                    }
                }
                if let Some(owner_to_key) = &owner_to_key {
                    let owner_to_existing_active = writers
                        .get(owner_to_key)
                        .map(|entry| entry.active.load(Ordering::Relaxed))
                        .unwrap_or(false);
                    if !owner_to_existing_active {
                        writers.insert(
                            owner_to_key.clone(),
                            TcpWriterEntry {
                                writer: writer_arc.clone(),
                                active: writer_active.clone(),
                            },
                        );
                    }
                }
                if debug {
                    crate::logging::info(format!(
                        "hotlink tcp proxy: writer mapped keys channel={} local={:?} owner_from={:?} owner_to={:?} active={}",
                        channel_key,
                        local_key,
                        owner_from_key,
                        owner_to_key,
                        !existing_active
                    ));
                }
            }

            let manager = self.clone();
            let channel = channel_key.clone();
            let local_channel = local_key.clone();
            let owner_from_channel = owner_from_key.clone();
            let owner_to_channel = owner_to_key.clone();
            let outbound_channel = outbound_key.clone();
            let writer_for_cleanup = writer_arc.clone();
            let writer_active_for_cleanup = writer_active.clone();
            tokio::spawn(async move {
                let mut buf = vec![0u8; HOTLINK_TCP_PROXY_CHUNK_SIZE];
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
                    log_hotlink_tcp_dump("local->remote", &channel, None, &buf[..n]);
                    if let Err(err) = manager
                        .send_best_effort_ordered(
                            outbound_channel.clone(),
                            "".to_string(),
                            buf[..n].to_vec(),
                        )
                        .await
                    {
                        crate::logging::error(format!(
                            "hotlink tcp proxy: send failed (continuing): {err:?}"
                        ));
                    }
                }
                if hotlink_debug_enabled() {
                    let keys = {
                        let writers = manager.tcp_writers.lock().await;
                        debug_writer_keys(writers.keys().cloned())
                    };
                    crate::logging::info(format!(
                        "hotlink tcp proxy: closed channel={} remaining_keys=[{}]",
                        channel, keys
                    ));
                }
                let mut writers = manager.tcp_writers.lock().await;
                // Only clear entries that still point at this exact writer.
                // Multiple accepts can exist transiently; removing by key alone
                // can tear down the active mapping and recreate the desktop hang.
                if writers
                    .get(&channel)
                    .map(|w| Arc::ptr_eq(w, &writer_for_cleanup))
                    .unwrap_or(false)
                {
                    writers.remove(&channel);
                }
                if let Some(local_key) = &local_channel {
                    if writers
                        .get(local_key)
                        .map(|w| Arc::ptr_eq(w, &writer_for_cleanup))
                        .unwrap_or(false)
                    {
                        writers.remove(local_key);
                    }
                }
                writer_active_for_cleanup.store(false, Ordering::Relaxed);
                let mut writers = manager.tcp_writers.lock().await;
                if matches!(
                    writers.get(&channel),
                    Some(entry) if Arc::ptr_eq(&entry.writer, &writer_for_cleanup)
                ) {
                    writers.remove(&channel);
                }
                if let Some(local_key) = local_channel {
                    if matches!(
                        writers.get(&local_key),
                        Some(entry) if Arc::ptr_eq(&entry.writer, &writer_for_cleanup)
                    ) {
                        writers.remove(&local_key);
                    }
                }
                if let Some(owner_from_key) = owner_from_channel {
                    if matches!(
                        writers.get(&owner_from_key),
                        Some(entry) if Arc::ptr_eq(&entry.writer, &writer_for_cleanup)
                    ) {
                        writers.remove(&owner_from_key);
                    }
                }
                if let Some(owner_to_key) = owner_to_channel {
                    if matches!(
                        writers.get(&owner_to_key),
                        Some(entry) if Arc::ptr_eq(&entry.writer, &writer_for_cleanup)
                    ) {
                        writers.remove(&owner_to_key);
                    }
                }
            });
        }
        self.clear_tcp_proxy_state(&channel_key, local_key.as_deref())
            .await;
    }

    async fn clear_tcp_proxy_state(&self, channel_key: &str, local_key: Option<&str>) {
        {
            let mut proxies = self.tcp_proxies.lock().unwrap();
            proxies.remove(channel_key);
        }
        {
            let mut writers = self.tcp_writers.lock().await;
            writers.remove(channel_key);
            if let Some(local_key) = local_key {
                writers.remove(local_key);
            }
        }
        {
            let mut reorder = self.tcp_reorder.lock().await;
            reorder.remove(channel_key);
            if let Some(local_key) = local_key {
                reorder.remove(local_key);
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
            dir_abs,
            ipc_path: ipc_path.clone(),
            accept_path: accept_path.clone(),
            quic: None,
        };
        self.sessions
            .write()
            .await
            .insert(session_id.clone(), session);

        if is_tcp_proxy_path(&path) {
            crate::logging::info(format!(
                "hotlink sending accept for tcp proxy: session={} path={}",
                &session_id[..8], path
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
        if self.quic_only {
            crate::logging::info("hotlink ws data ignored (quic-only)");
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

        self.handle_frame(&ipc_path, frame).await;
    }

    async fn handle_frame(&self, ipc_path: &Path, frame: HotlinkFrame) {
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
                    let mut reorder = self.tcp_reorder.lock().await;
                    let buf = reorder
                        .entry(frame.path.clone())
                        .or_insert_with(|| TcpReorderBuf {
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
            if hotlink_debug_enabled() {
                let keys = {
                    let writers = self.tcp_writers.lock().await;
                    debug_writer_keys(writers.keys().cloned())
                };
                crate::logging::error(format!(
                    "hotlink tcp write skipped detail: session={} frame_path={} known_writer_keys=[{}]",
                    session.id, frame.path, keys
                ));
            }
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
            let keys = {
                let writers = self.tcp_writers.lock().await;
                debug_writer_keys(writers.keys().cloned())
            };
            crate::logging::info(format!(
                "hotlink tcp writer not ready, waiting for path={} known_writer_keys=[{}]",
                rel_path, keys
            ));
        }
        for _ in 0..20 {
            tokio::time::sleep(tokio::time::Duration::from_millis(250)).await;
            if let Some(w) = self.get_tcp_writer(rel_path).await {
                if hotlink_debug_enabled() {
                    let keys = {
                        let writers = self.tcp_writers.lock().await;
                        debug_writer_keys(writers.keys().cloned())
                    };
                    crate::logging::info(format!(
                        "hotlink tcp writer ready after wait path={} known_writer_keys=[{}]",
                        rel_path, keys
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
        if !self.enabled || !self.quic_enabled {
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
                    "hotlink quic error: session={} error={}",
                    signal.session_id, signal.error
                ));
            }
            _ => {}
        }
    }

    async fn maybe_start_quic_offer(&self, session_id: &str) {
        if !self.quic_enabled {
            return;
        }
        let mut sessions = self.sessions.write().await;
        let session = match sessions.get_mut(session_id) {
            Some(s) => s,
            None => return,
        };
        if session.quic.is_some() {
            return;
        }
        crate::logging::info(format!(
            "hotlink quic offer starting: session={}",
            session_id
        ));
        let (endpoint, stun_addr) = match quic_server_endpoint() {
            Ok(values) => values,
            Err(err) => {
                crate::logging::error(format!("hotlink quic server endpoint failed: {err:?}"));
                return;
            }
        };
        let addr = endpoint.local_addr().ok();
        let quic = HotlinkQuicSession {
            endpoint: endpoint.clone(),
            state: Arc::new(TokioMutex::new(HotlinkQuicState {
                send: None,
                err: None,
            })),
            ready: Arc::new(Notify::new()),
            ready_flag: Arc::new(AtomicBool::new(false)),
        };
        session.quic = Some(quic.clone());
        drop(sessions);

        let addrs = quic_offer_addrs(addr, stun_addr);
        if addrs.is_empty() {
            crate::logging::error("hotlink quic offer missing local addr");
            return;
        }
        if let Err(err) = self
            .send_signal(session_id, "quic_offer", &addrs, "", "")
            .await
        {
            crate::logging::error(format!(
                "hotlink quic offer send failed: session={} error={err:?}",
                session_id
            ));
        } else {
            self.record_quic_offer();
            crate::logging::info(format!(
                "hotlink quic offer sent: session={} addrs={:?}",
                session_id, addrs
            ));
        }

        let manager = self.clone();
        let session_id = session_id.to_string();
        tokio::spawn(async move {
            manager.accept_quic(&session_id, quic).await;
        });
    }

    async fn accept_quic(&self, session_id: &str, quic: HotlinkQuicSession) {
        crate::logging::info(format!(
            "hotlink quic accept waiting: session={}",
            session_id
        ));
        let incoming: Option<quinn::Incoming> =
            timeout(HOTLINK_QUIC_ACCEPT_TIMEOUT, quic.endpoint.accept())
                .await
                .unwrap_or_default();
        let Some(incoming) = incoming else {
            quic_set_err_state(
                &quic.state,
                &quic.ready,
                &quic.ready_flag,
                "quic accept timeout".to_string(),
            )
            .await;
            crate::logging::info(format!(
                "hotlink quic accept timeout, ws fallback: session={}",
                session_id
            ));
            return;
        };

        let conn = match incoming.await {
            Ok(conn) => conn,
            Err(err) => {
                quic_set_err_state(
                    &quic.state,
                    &quic.ready,
                    &quic.ready_flag,
                    format!("quic accept failed: {err:?}"),
                )
                .await;
                return;
            }
        };

        let (mut send, mut recv) = match conn.accept_bi().await {
            Ok(streams) => streams,
            Err(err) => {
                quic_set_err_state(
                    &quic.state,
                    &quic.ready,
                    &quic.ready_flag,
                    format!("quic accept stream failed: {err:?}"),
                )
                .await;
                return;
            }
        };

        if let Err(err) = read_quic_handshake(&mut recv, session_id).await {
            quic_set_err_state(
                &quic.state,
                &quic.ready,
                &quic.ready_flag,
                format!("quic handshake failed: {err:?}"),
            )
            .await;
            let _ = send.finish();
            return;
        }

        {
            let mut state = quic.state.lock().await;
            state.send = Some(send);
            state.err = None;
        }
        quic_ready_flag(&quic.ready, &quic.ready_flag);

        let manager = self.clone();
        let session_id = session_id.to_string();
        tokio::spawn(async move {
            manager.quic_read_loop(&session_id, &mut recv).await;
        });
    }

    async fn handle_quic_offer(&self, signal: crate::wsproto::HotlinkSignal) {
        let session_id = signal.session_id.clone();
        crate::logging::info(format!(
            "hotlink quic offer received: session={} addrs={:?}",
            session_id, signal.addrs
        ));
        let quic = {
            let out = self.outbound.read().await;
            out.get(&session_id).and_then(|o| o.quic.clone())
        };
        let Some(quic) = quic else {
            return;
        };

        if signal.addrs.is_empty() {
            quic_set_err_state(
                &quic.state,
                &quic.ready,
                &quic.ready_flag,
                "quic offer missing addrs".to_string(),
            )
            .await;
            if let Err(err) = self
                .send_signal(&session_id, "quic_answer", &[], "", "offer missing addrs")
                .await
            {
                crate::logging::error(format!(
                    "hotlink quic answer send failed: session={} err={err:?}",
                    session_id
                ));
                self.record_quic_answer(false);
            }
            return;
        }

        let mut last_err: Option<String> = None;
        for addr in signal.addrs.iter() {
            let addr = match addr.parse::<SocketAddr>() {
                Ok(a) => a,
                Err(_) => continue,
            };
            let connect = quic
                .endpoint
                .connect_with(quic_client_config(), addr, "syftbox-hotlink");
            let conn = match connect {
                Ok(c) => c,
                Err(err) => {
                    last_err = Some(format!("{err:?}"));
                    continue;
                }
            };
            let conn = match timeout(HOTLINK_QUIC_DIAL_TIMEOUT, conn).await {
                Ok(Ok(c)) => c,
                Ok(Err(err)) => {
                    last_err = Some(format!("{err:?}"));
                    continue;
                }
                Err(_) => {
                    last_err = Some("quic dial timeout".to_string());
                    continue;
                }
            };
            let (mut send, mut recv) = match conn.open_bi().await {
                Ok(streams) => streams,
                Err(err) => {
                    last_err = Some(format!("{err:?}"));
                    continue;
                }
            };
            if let Err(err) = write_quic_handshake(&mut send, &session_id).await {
                last_err = Some(format!("{err:?}"));
                continue;
            }

            {
                let mut state = quic.state.lock().await;
                state.send = Some(send);
                state.err = None;
            }
            quic_ready_flag(&quic.ready, &quic.ready_flag);
            if let Err(err) = self
                .send_signal(&session_id, "quic_answer", &[addr.to_string()], "ok", "")
                .await
            {
                crate::logging::error(format!(
                    "hotlink quic answer send failed: session={} err={err:?}",
                    session_id
                ));
            } else {
                self.record_quic_answer(true);
                crate::logging::info(format!(
                    "hotlink quic answer sent: session={} addr={}",
                    session_id, addr
                ));
            }
            let manager = self.clone();
            tokio::spawn(async move {
                manager.quic_read_loop(&session_id, &mut recv).await;
            });
            return;
        }

        let err = last_err.unwrap_or_else(|| "quic dial failed".to_string());
        quic_set_err_state(&quic.state, &quic.ready, &quic.ready_flag, err.clone()).await;
        if let Err(err) = self
            .send_signal(&session_id, "quic_answer", &[], "", &err)
            .await
        {
            crate::logging::error(format!(
                "hotlink quic answer send failed: session={} err={err:?}",
                session_id
            ));
        } else {
            self.record_quic_answer(false);
            crate::logging::info(format!(
                "hotlink quic answer sent: session={} error={}",
                session_id, err
            ));
        }
        crate::logging::info(format!(
            "hotlink quic dial failed, ws fallback: session={} error={}",
            session_id, err
        ));
    }

    async fn handle_quic_answer(&self, signal: crate::wsproto::HotlinkSignal) {
        crate::logging::info(format!(
            "hotlink quic answer received: session={} addrs={:?} error={}",
            signal.session_id, signal.addrs, signal.error
        ));
        if !signal.error.is_empty() {
            self.record_quic_answer(false);
            crate::logging::info(format!(
                "hotlink quic answer error, ws fallback: session={} error={}",
                signal.session_id, signal.error
            ));
            if self.quic_only {
                let _ = self.send_close(&signal.session_id, "quic-only").await;
            }
            return;
        }
        crate::logging::info(format!(
            "hotlink quic answer ok: session={} addr={}",
            signal.session_id,
            signal.addrs.join(",")
        ));
        self.record_quic_answer(true);
    }

    async fn try_send_quic(
        &self,
        session_id: &str,
        rel_path: &str,
        etag: &str,
        seq: u64,
        payload: &[u8],
        wait: bool,
    ) -> Result<Option<()>> {
        let quic = {
            let out = self.outbound.read().await;
            out.get(session_id).and_then(|o| o.quic.clone())
        };
        let Some(quic) = quic else {
            return Ok(None);
        };
        if !quic.ready_flag.load(Ordering::SeqCst) {
            if wait {
                if timeout(HOTLINK_QUIC_ACCEPT_TIMEOUT, quic.ready.notified())
                    .await
                    .is_err()
                {
                    return Err(anyhow::anyhow!("quic wait timeout"));
                }
            } else {
                return Ok(None);
            }
        }
        let mut state = quic.state.lock().await;
        if let Some(err) = state.err.clone() {
            return Err(anyhow::anyhow!(err));
        }
        let Some(send) = state.send.as_mut() else {
            return Ok(None);
        };
        let frame = HotlinkFrame {
            path: rel_path.to_string(),
            etag: etag.to_string(),
            seq,
            payload: payload.to_vec(),
        };
        let bytes = crate::hotlink::encode_hotlink_frame(&frame);
        send.write_all(&bytes).await?;
        Ok(Some(()))
    }

    async fn mark_ws_fallback(&self, session_id: &str, rel_path: &str) {
        let mut out = self.outbound.write().await;
        if let Some(entry) = out.get_mut(session_id) {
            if !entry.ws_fallback_logged {
                entry.ws_fallback_logged = true;
                self.record_ws_fallback();
                crate::logging::info(format!(
                    "hotlink quic not ready, using ws fallback: session={} path={}",
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
            self.handle_frame(&ipc_path, frame).await;
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
            self.handle_frame(Path::new(""), frame).await;
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
        let path_key = parent_path(&rel_path);
        let existing_session = {
            let guard = self.outbound_by_path.read().await;
            guard.get(&path_key).cloned()
        };
        let inbound_session = if existing_session.is_none() {
            let sessions = self.sessions.read().await;
            sessions.iter().find_map(|(sid, sess)| {
                if parent_path(&sess.path) == path_key {
                    Some((sid.clone(), sess.webrtc.clone()))
                } else {
                    None
                }
            })
        } else {
            None
        };

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
                &id[..8], rel_path
            ));
            let outbound = HotlinkOutbound {
                id: id.clone(),
                path_key: path_key.clone(),
                accepted: false,
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
            if let Err(err) = self.send_open(&id, rel_path.clone()).await {
                crate::logging::error(format!("hotlink send open failed: {err:?}"));
                self.remove_outbound(&id).await;
                return Err(err);
            }
            (id, true)
        };

        // Only block-wait for accept on newly created sessions.
        // For existing sessions, check non-blocking so we don't stall the TCP read loop.
        if is_new {
            if self.wait_for_accept(&session_id).await {
                crate::logging::info(format!(
                    "hotlink session accepted: session={}",
                    &session_id[..8]
                ));
            } else {
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

        let p2p_only = self.is_p2p_only();
        {
            let send_started = tokio::time::Instant::now();
            // In p2p_only mode, wait for WebRTC data channel to be ready.
            // In normal mode, try non-blocking and fall back to WS.
            match self
                .try_send_webrtc(&session_id, &rel_path, &etag, seq, &payload, p2p_only)
                .await
            {
                Ok(Some(())) => {
                    self.record_tx(
                        payload_len,
                        send_started.elapsed().as_millis() as u64,
                        true,
                    );
                    return Ok(());
                }
                Ok(None) => {
                    if !p2p_only {
                        self.mark_ws_fallback(&session_id, &rel_path).await;
                    }
                }
                Err(e) => {
                    if hotlink_debug_enabled() {
                        crate::logging::info(format!(
                            "hotlink webrtc send err: {e:?}"
                        ));
                    }
                    if !p2p_only {
                        self.mark_ws_fallback(&session_id, &rel_path).await;
                    }
                }
            }
        }

        if p2p_only {
            crate::logging::info(format!(
                "hotlink p2p_only: dropping packet seq={} (webrtc not ready after wait)",
                seq
            ));
            return Ok(());
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
        let deadline =
            tokio::time::Instant::now() + HOTLINK_WEBRTC_READY_TIMEOUT;
        loop {
            {
                let out = self.outbound.read().await;
                if let Some(entry) = out.get(session_id) {
                    if let Some(ref w) = entry.webrtc {
                        if w.ready_flag.load(Ordering::SeqCst) {
                            crate::logging::info(format!(
                                "hotlink webrtc ready for outbound: session={}",
                                &session_id[..8.min(session_id.len())]
                            ));
                            return;
                        }
                    }
                } else {
                    return;
                }
            }
            if tokio::time::Instant::now() >= deadline {
                crate::logging::info(format!(
                    "hotlink webrtc ready timeout ({}s): session={}",
                    HOTLINK_WEBRTC_READY_TIMEOUT.as_secs(),
                    &session_id[..8.min(session_id.len())]
                ));
                return;
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

fn hotlink_tcp_dump_enabled() -> bool {
    env_flag_truthy("SYFTBOX_HOTLINK_TCP_DUMP")
}

fn hotlink_tcp_dump_full_enabled() -> bool {
    env_flag_truthy("SYFTBOX_HOTLINK_TCP_DUMP_FULL")
}

fn hotlink_tcp_dump_preview_bytes() -> usize {
    std::env::var("SYFTBOX_HOTLINK_TCP_DUMP_PREVIEW")
        .ok()
        .and_then(|v| v.trim().parse::<usize>().ok())
        .map(|n| n.clamp(1, 4096))
        .unwrap_or(64)
}

fn env_flag_truthy(name: &str) -> bool {
    match std::env::var(name) {
        Ok(v) => matches!(
            v.trim().to_ascii_lowercase().as_str(),
            "1" | "true" | "yes" | "on"
        ),
        Err(_) => false,
    }
}

fn hex_encode(data: &[u8]) -> String {
    let mut out = String::with_capacity(data.len() * 2);
    for b in data {
        let _ = write!(&mut out, "{:02x}", b);
    }
    out
}

fn log_hotlink_tcp_dump(direction: &str, channel: &str, seq: Option<u64>, payload: &[u8]) {
    if !hotlink_tcp_dump_enabled() {
        return;
    }
    let preview_len = if hotlink_tcp_dump_full_enabled() {
        payload.len()
    } else {
        payload.len().min(hotlink_tcp_dump_preview_bytes())
    };
    let preview = &payload[..preview_len];
    let truncated = preview_len < payload.len();
    let seq_label = seq
        .map(|v| v.to_string())
        .unwrap_or_else(|| "-".to_string());
    crate::logging::info(format!(
        "hotlink tcp dump: dir={} channel={} seq={} bytes={} sample_bytes={} truncated={} hex={}",
        direction,
        channel,
        seq_label,
        payload.len(),
        preview.len(),
        truncated,
        hex_encode(preview)
    ));
}

fn tcp_proxy_enabled() -> bool {
    // TCP proxy defaults to ON. Set SYFTBOX_HOTLINK_TCP_PROXY=0 to disable.
    std::env::var("SYFTBOX_HOTLINK_TCP_PROXY")
        .ok()
        .as_deref()
        .map(|v| v != "0")
        .unwrap_or(true)
}

fn tcp_proxy_bind_ip(owner_email: &str) -> String {
    if let Ok(addr) = std::env::var("SYFTBOX_HOTLINK_TCP_PROXY_ADDR") {
        let trimmed = addr.trim();
        if !trimmed.is_empty() {
            if let Some((ip, _)) = trimmed.split_once(':') {
                return ip.to_string();
            }
            return trimmed.to_string();
        }
    }
    let _ = owner_email;
    "127.0.0.1".to_string()
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
pub async fn connect_ipc(marker_path: &Path, timeout: Duration) -> Result<HotlinkStream> {
    dial_hotlink_ipc(marker_path, timeout).await
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
                ..Default::default()
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

fn quic_client_endpoint() -> Result<Endpoint> {
    let addr = SocketAddr::new(IpAddr::V4(Ipv4Addr::UNSPECIFIED), 0);
    Ok(Endpoint::client(addr)?)
}

fn quic_server_config() -> Result<ServerConfig> {
    ensure_rustls_provider();
    let cert = generate_simple_self_signed(vec!["localhost".to_string()])?;
    let key = PrivatePkcs8KeyDer::from(cert.key_pair.serialize_der());
    let cert_chain = vec![cert.cert.into()];
    let mut server_crypto = rustls::ServerConfig::builder()
        .with_no_client_auth()
        .with_single_cert(cert_chain, key.into())?;
    server_crypto.alpn_protocols = vec![HOTLINK_QUIC_ALPN.to_vec()];
    let server_config =
        ServerConfig::with_crypto(Arc::new(QuicServerConfig::try_from(server_crypto)?));
    Ok(server_config)
}

fn quic_client_config() -> ClientConfig {
    ensure_rustls_provider();
    let mut client_crypto = rustls::ClientConfig::builder()
        .dangerous()
        .with_custom_certificate_verifier(Arc::new(AcceptAnyCert))
        .with_no_client_auth();
    client_crypto.alpn_protocols = vec![HOTLINK_QUIC_ALPN.to_vec()];
    ClientConfig::new(Arc::new(
        QuicClientConfig::try_from(client_crypto).expect("quic client config"),
    ))
}

fn quic_offer_addrs(addr: Option<SocketAddr>, stun_addr: Option<SocketAddr>) -> Vec<String> {
    let mut out = Vec::new();
    if let Some(addr) = addr {
        let port = addr.port();
        append_unique_addr(&mut out, format!("127.0.0.1:{port}"));
        let bind_ip = tcp_proxy_bind_ip("");
        if bind_ip != "127.0.0.1" {
            append_unique_addr(&mut out, format!("{bind_ip}:{port}"));
        }
    }
    if let Some(stun_addr) = stun_addr {
        append_unique_addr(&mut out, stun_addr.to_string());
    }
    out
}

fn append_unique_addr(out: &mut Vec<String>, addr: String) {
    if addr.trim().is_empty() {
        return;
    }
    if out
        .iter()
        .any(|existing| existing.eq_ignore_ascii_case(&addr))
    {
        return;
    }
    out.push(addr);
}

fn discover_stun_addr(socket: &std::net::UdpSocket) -> Result<Option<SocketAddr>> {
    let mut server = std::env::var(HOTLINK_STUN_SERVER_ENV)
        .unwrap_or_else(|_| "stun.l.google.com:19302".to_string());
    server = server.trim().to_string();
    if server.is_empty() {
        server = "stun.l.google.com:19302".to_string();
    }
    if server == "0"
        || server.eq_ignore_ascii_case("off")
        || server.eq_ignore_ascii_case("disabled")
    {
        return Ok(None);
    }

    let server_addr = server
        .to_socket_addrs()?
        .find(|addr| matches!(addr, SocketAddr::V4(_) | SocketAddr::V6(_)))
        .ok_or_else(|| anyhow::anyhow!("unable to resolve stun server {}", server))?;

    let mut txid = [0u8; 12];
    let uuid_bytes = *Uuid::new_v4().as_bytes();
    txid.copy_from_slice(&uuid_bytes[..12]);

    let mut req = [0u8; 20];
    req[0..2].copy_from_slice(&STUN_BINDING_REQUEST.to_be_bytes());
    req[2..4].copy_from_slice(&0u16.to_be_bytes());
    req[4..8].copy_from_slice(&STUN_MAGIC_COOKIE.to_be_bytes());
    req[8..20].copy_from_slice(&txid);

    socket.set_write_timeout(Some(HOTLINK_STUN_TIMEOUT))?;
    let send_res = socket.send_to(&req, server_addr);
    socket.set_write_timeout(None)?;
    send_res?;

    let mut resp = [0u8; 1024];
    socket.set_read_timeout(Some(HOTLINK_STUN_TIMEOUT))?;
    let read_res = socket.recv_from(&mut resp);
    socket.set_read_timeout(None)?;
    let (n, _) = read_res?;

    let mapped = parse_stun_mapped_addr(&resp[..n], txid)?;
    Ok(Some(mapped))
}

fn parse_stun_mapped_addr(msg: &[u8], txid: [u8; 12]) -> Result<SocketAddr> {
    if msg.len() < 20 {
        anyhow::bail!("stun response too short");
    }
    if u16::from_be_bytes([msg[0], msg[1]]) != STUN_BINDING_SUCCESS {
        anyhow::bail!("unexpected stun response type");
    }
    if u32::from_be_bytes([msg[4], msg[5], msg[6], msg[7]]) != STUN_MAGIC_COOKIE {
        anyhow::bail!("invalid stun magic cookie");
    }
    if msg[8..20] != txid {
        anyhow::bail!("stun transaction mismatch");
    }

    let msg_len = u16::from_be_bytes([msg[2], msg[3]]) as usize;
    let limit = (20 + msg_len).min(msg.len());
    let mut offset = 20usize;
    while offset + 4 <= limit {
        let attr_type = u16::from_be_bytes([msg[offset], msg[offset + 1]]);
        let attr_len = u16::from_be_bytes([msg[offset + 2], msg[offset + 3]]) as usize;
        offset += 4;
        if offset + attr_len > limit {
            break;
        }
        let value = &msg[offset..offset + attr_len];
        match attr_type {
            STUN_XOR_MAPPED_ADDRESS => {
                if let Ok(addr) = parse_stun_address_value(value, &txid, true) {
                    return Ok(addr);
                }
            }
            STUN_MAPPED_ADDRESS => {
                if let Ok(addr) = parse_stun_address_value(value, &txid, false) {
                    return Ok(addr);
                }
            }
            _ => {}
        }
        offset += attr_len;
        let rem = offset % 4;
        if rem != 0 {
            offset += 4 - rem;
        }
    }
    anyhow::bail!("no mapped address attributes in stun response");
}

fn parse_stun_address_value(value: &[u8], txid: &[u8; 12], xor: bool) -> Result<SocketAddr> {
    if value.len() < 8 {
        anyhow::bail!("stun address attribute too short");
    }
    let family = value[1];
    let mut port = u16::from_be_bytes([value[2], value[3]]);
    if xor {
        port ^= (STUN_MAGIC_COOKIE >> 16) as u16;
    }
    match family {
        0x01 => {
            if value.len() < 8 {
                anyhow::bail!("stun ipv4 attribute too short");
            }
            let mut octets = [value[4], value[5], value[6], value[7]];
            if xor {
                let cookie = STUN_MAGIC_COOKIE.to_be_bytes();
                for i in 0..4 {
                    octets[i] ^= cookie[i];
                }
            }
            Ok(SocketAddr::new(IpAddr::V4(Ipv4Addr::from(octets)), port))
        }
        0x02 => {
            if value.len() < 20 {
                anyhow::bail!("stun ipv6 attribute too short");
            }
            let mut octets = [0u8; 16];
            octets.copy_from_slice(&value[4..20]);
            if xor {
                let cookie = STUN_MAGIC_COOKIE.to_be_bytes();
                for i in 0..4 {
                    octets[i] ^= cookie[i];
                }
                for i in 0..12 {
                    octets[4 + i] ^= txid[i];
                }
            }
            Ok(SocketAddr::new(IpAddr::from(octets), port))
        }
        _ => anyhow::bail!("unsupported stun address family {}", family),
    }
}

fn ensure_rustls_provider() {
    RUSTLS_PROVIDER_INIT.call_once(|| {
        let provider = rustls::crypto::aws_lc_rs::default_provider();
        if let Err(err) = provider.install_default() {
            crate::logging::error(format!(
                "hotlink quic rustls provider install failed: {err:?}"
            ));
        }
    });
}

async fn quic_set_err_state(
    state: &Arc<TokioMutex<HotlinkQuicState>>,
    ready: &Arc<Notify>,
    flag: &Arc<AtomicBool>,
    err: String,
) {
    {
        let mut guard = state.lock().await;
        guard.err = Some(err);
    }
    quic_ready_flag(ready, flag);
}

fn quic_ready_flag(ready: &Arc<Notify>, flag: &Arc<AtomicBool>) {
    if !flag.swap(true, Ordering::SeqCst) {
        ready.notify_waiters();
    }
}

async fn write_quic_handshake(send: &mut quinn::SendStream, session_id: &str) -> Result<()> {
    if session_id.len() > u16::MAX as usize {
        anyhow::bail!("session id too long");
    }
    send.write_all(b"HLQ1").await?;
    send.write_all(&(session_id.len() as u16).to_be_bytes())
        .await?;
    send.write_all(session_id.as_bytes()).await?;
    Ok(())
}

async fn read_quic_handshake(recv: &mut quinn::RecvStream, session_id: &str) -> Result<()> {
    let mut magic = [0u8; 4];
    recv.read_exact(&mut magic).await?;
    if &magic != b"HLQ1" {
        anyhow::bail!("invalid quic handshake magic");
    }
    let mut len_buf = [0u8; 2];
    recv.read_exact(&mut len_buf).await?;
    let len = u16::from_be_bytes(len_buf) as usize;
    if len == 0 || len > 1024 {
        anyhow::bail!("invalid quic handshake length");
    }
    let mut buf = vec![0u8; len];
    recv.read_exact(&mut buf).await?;
    if buf != session_id.as_bytes() {
        anyhow::bail!("quic handshake session mismatch");
    }
    Ok(())
}

#[derive(Debug)]
struct AcceptAnyCert;

impl ServerCertVerifier for AcceptAnyCert {
    fn verify_server_cert(
        &self,
        _end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &ServerName<'_>,
        _ocsp_response: &[u8],
        _now: UnixTime,
    ) -> Result<ServerCertVerified, rustls::Error> {
        Ok(ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        _message: &[u8],
        _cert: &CertificateDer<'_>,
        _dss: &rustls::DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        Ok(HandshakeSignatureValid::assertion())
    }

    fn verify_tls13_signature(
        &self,
        _message: &[u8],
        _cert: &CertificateDer<'_>,
        _dss: &rustls::DigitallySignedStruct,
    ) -> Result<HandshakeSignatureValid, rustls::Error> {
        Ok(HandshakeSignatureValid::assertion())
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        vec![
            rustls::SignatureScheme::ED25519,
            rustls::SignatureScheme::ECDSA_NISTP256_SHA256,
            rustls::SignatureScheme::RSA_PKCS1_SHA256,
        ]
    }
}

fn now_millis() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as u64
}
