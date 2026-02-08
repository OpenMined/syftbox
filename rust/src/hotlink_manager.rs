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
    tcp_writers: Arc<TokioMutex<HashMap<String, Arc<TokioMutex<tokio::net::tcp::OwnedWriteHalf>>>>>,
    tcp_proxies: Arc<StdMutex<HashMap<String, ()>>>,
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
        let enabled = std::env::var("SYFTBOX_HOTLINK").ok().as_deref() == Some("1");
        let socket_only = std::env::var("SYFTBOX_HOTLINK_SOCKET_ONLY").ok().as_deref() == Some("1");
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

    fn flush_telemetry(&self, force: bool) {
        let now_ms = now_millis();
        let (payload, path) = {
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
            });
            (json.to_string(), self.telemetry_path())
        };

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
                    proxies.insert(channel_key, ());
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
                        next_bind_log =
                            tokio::time::Instant::now() + Duration::from_secs(2);
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
                    "hotlink tcp proxy: accepted on {} for {}",
                    addr, channel_key
                ));
            }
            let (mut reader, writer) = stream.into_split();

            let writer_arc = Arc::new(TokioMutex::new(writer));
            let mapped_as_active = {
                let mut writers = self.tcp_writers.lock().await;
                // Do not clobber an existing active mapping for this channel.
                // Desktop watchdog/health probes can briefly connect and close;
                // replacing the writer here can blackhole in-flight remote frames.
                if writers.contains_key(&channel_key) {
                    false
                } else {
                    writers.insert(channel_key.clone(), writer_arc.clone());
                    if let Some(local_key) = &local_key {
                        writers
                            .entry(local_key.clone())
                            .or_insert_with(|| writer_arc.clone());
                    }
                    true
                }
            };
            if debug {
                let keys = {
                    let writers = self.tcp_writers.lock().await;
                    debug_writer_keys(writers.keys().cloned())
                };
                crate::logging::info(format!(
                    "hotlink tcp proxy: writer mapped canonical={} local={} active={} keys=[{}]",
                    channel_key,
                    local_key.clone().unwrap_or_else(|| "<none>".to_string()),
                    mapped_as_active,
                    keys
                ));
            }

            let manager = self.clone();
            let channel = channel_key.clone();
            let local_channel = local_key.clone();
            let writer_for_cleanup = writer_arc.clone();
            tokio::spawn(async move {
                let mut buf = vec![0u8; 64 * 1024];
                loop {
                    let n = match reader.read(&mut buf).await {
                        Ok(0) => break,
                        Ok(n) => n,
                        Err(_) => break,
                    };
                    if hotlink_debug_enabled() {
                        crate::logging::info(format!(
                            "hotlink tcp proxy: recv bytes={} channel={}",
                            n, channel
                        ));
                    }
                    log_hotlink_tcp_dump("local->remote", &channel, None, &buf[..n]);
                    if let Err(err) = manager
                        .send_best_effort_ordered(
                            channel.clone(),
                            "".to_string(),
                            buf[..n].to_vec(),
                        )
                        .await
                    {
                        crate::logging::error(format!("hotlink tcp proxy: send failed: {err:?}"));
                        break;
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
            let _ = self.send_accept(&session_id).await;
            self.maybe_start_quic_offer(&session_id).await;
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
            self.maybe_start_quic_offer(&session_id).await;
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
                    manager.maybe_start_quic_offer(&session_id).await;
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
            entry.notify.notify_one();
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

        self.handle_frame(session, frame).await;
    }

    async fn handle_frame(&self, session: &HotlinkSession, frame: HotlinkFrame) {
        let payload_len = frame.payload.len();
        let write_started = tokio::time::Instant::now();
        if is_tcp_proxy_path(&frame.path) {
            let writer = self.get_tcp_writer_with_retry(&frame.path).await;
            if let Some(writer) = writer {
                let mut reorder = self.tcp_reorder.lock().await;
                let buf = reorder
                    .entry(frame.path.clone())
                    .or_insert_with(|| TcpReorderBuf {
                        next_seq: 1,
                        pending: BTreeMap::new(),
                    });
                buf.pending.insert(frame.seq, frame.payload);
                let mut guard = writer.lock().await;
                while let Some(data) = buf.pending.remove(&buf.next_seq) {
                    let seq = buf.next_seq;
                    log_hotlink_tcp_dump("remote->local", &frame.path, Some(seq), &data);
                    if let Err(err) = guard.write_all(&data).await {
                        crate::logging::error(format!("hotlink tcp write failed: {err:?}"));
                        break;
                    }
                    self.record_rx(data.len(), write_started.elapsed().as_millis() as u64);
                    buf.next_seq += 1;
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

        if let Err(err) = self.write_ipc(&session.ipc_path, frame).await {
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
        writers.get(rel_path).cloned()
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
        for _ in 0..60 {
            tokio::time::sleep(tokio::time::Duration::from_millis(500)).await;
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
            "quic_offer" => {
                self.handle_quic_offer(signal).await;
            }
            "quic_answer" => {
                self.handle_quic_answer(signal).await;
            }
            "quic_error" => {
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

    async fn quic_read_loop(&self, session_id: &str, recv: &mut quinn::RecvStream) {
        loop {
            let frame = match crate::hotlink::read_hotlink_frame(recv).await {
                Ok(f) => f,
                Err(_) => break,
            };
            let sessions = self.sessions.read().await;
            let session = match sessions.get(session_id) {
                Some(s) => s,
                None => break,
            };
            self.handle_frame(session, frame).await;
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
            let quic = if self.quic_enabled {
                let endpoint = quic_client_endpoint().context("quic client endpoint")?;
                Some(HotlinkQuicOutbound {
                    endpoint,
                    state: Arc::new(TokioMutex::new(HotlinkQuicState {
                        send: None,
                        err: None,
                    })),
                    ready: Arc::new(Notify::new()),
                    ready_flag: Arc::new(AtomicBool::new(false)),
                })
            } else {
                None
            };
            let outbound = HotlinkOutbound {
                id: id.clone(),
                path_key: path_key.clone(),
                accepted: false,
                seq: 0,
                notify: Arc::new(Notify::new()),
                rejected: None,
                ws_fallback_logged: false,
                quic,
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

        if self.quic_enabled {
            let wait = self.quic_only;
            let send_started = tokio::time::Instant::now();
            match self
                .try_send_quic(&session_id, &rel_path, &etag, seq, &payload, wait)
                .await
            {
                Ok(Some(())) => {
                    self.record_tx(
                        payload.len(),
                        send_started.elapsed().as_millis() as u64,
                        true,
                    );
                    return Ok(());
                }
                Ok(None) => {
                    if self.quic_only {
                        return Err(anyhow::anyhow!("hotlink quic unavailable"));
                    }
                    self.mark_ws_fallback(&session_id, &rel_path).await;
                }
                Err(err) => {
                    if self.quic_only {
                        return Err(err);
                    }
                    self.mark_ws_fallback(&session_id, &rel_path).await;
                }
            }
        }

        let send_started = tokio::time::Instant::now();
        let payload_len = payload.len();
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
    std::env::var("SYFTBOX_HOTLINK_TCP_PROXY").ok().as_deref() == Some("1")
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

fn quic_server_endpoint() -> Result<(Endpoint, Option<SocketAddr>)> {
    let server_config = quic_server_config()?;
    let addr = SocketAddr::new(IpAddr::V4(Ipv4Addr::UNSPECIFIED), 0);
    let socket = std::net::UdpSocket::bind(addr)?;
    let stun_addr = match discover_stun_addr(&socket) {
        Ok(addr) => addr,
        Err(err) => {
            crate::logging::info(format!("hotlink quic stun discovery failed: {err:?}"));
            None
        }
    };
    let endpoint = Endpoint::new(
        EndpointConfig::default(),
        Some(server_config),
        socket,
        Arc::new(TokioRuntime),
    )?;
    Ok((endpoint, stun_addr))
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
