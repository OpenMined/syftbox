use anyhow::{Context, Result};
use sha1::{Digest, Sha1};
use std::path::{Path, PathBuf};
use std::time::Duration;
use tokio::io::{AsyncReadExt, AsyncWriteExt};

const HOTLINK_FRAME_MAGIC: &[u8] = b"HLNK";
const HOTLINK_FRAME_VERSION: u8 = 1;
const HOTLINK_SOCKET_DIR: &str = "/tmp/syftbox-hotlink";

pub const HOTLINK_ACCEPT_NAME: &str = "stream.accept";

#[derive(Debug, Clone)]
pub struct HotlinkFrame {
    pub path: String,
    pub etag: String,
    pub seq: u64,
    pub payload: Vec<u8>,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum HotlinkIpcMode {
    Unix,
    Tcp,
    Pipe,
}

pub fn hotlink_ipc_mode() -> HotlinkIpcMode {
    let mode = std::env::var("SYFTBOX_HOTLINK_IPC")
        .unwrap_or_default()
        .trim()
        .to_lowercase();
    if mode == "tcp" {
        return HotlinkIpcMode::Tcp;
    }
    if cfg!(windows) {
        return HotlinkIpcMode::Pipe;
    }
    HotlinkIpcMode::Unix
}

pub fn hotlink_ipc_marker_name() -> &'static str {
    match hotlink_ipc_mode() {
        HotlinkIpcMode::Tcp => "stream.tcp",
        HotlinkIpcMode::Pipe => "stream.pipe",
        HotlinkIpcMode::Unix => "stream.sock",
    }
}

pub fn hotlink_socket_target(marker_path: &Path) -> PathBuf {
    let mut hasher = Sha1::new();
    hasher.update(marker_path.as_os_str().to_string_lossy().as_bytes());
    let sum = hasher.finalize();
    let name = format!("{:x}.sock", sum);
    PathBuf::from(HOTLINK_SOCKET_DIR).join(name)
}

pub fn hotlink_dir_path(
    datasites_root: &Path,
    owner_email: &str,
    app_name: &str,
    endpoint: &str,
) -> PathBuf {
    datasites_root
        .join(owner_email)
        .join("app_data")
        .join(app_name)
        .join("rpc")
        .join(endpoint)
}

pub fn hotlink_ipc_marker_path(
    datasites_root: &Path,
    owner_email: &str,
    app_name: &str,
    endpoint: &str,
) -> PathBuf {
    hotlink_dir_path(datasites_root, owner_email, app_name, endpoint)
        .join(hotlink_ipc_marker_name())
}

pub fn hotlink_accept_path(
    datasites_root: &Path,
    owner_email: &str,
    app_name: &str,
    endpoint: &str,
) -> PathBuf {
    hotlink_dir_path(datasites_root, owner_email, app_name, endpoint).join(HOTLINK_ACCEPT_NAME)
}

pub async fn create_accept_marker(path: &Path) -> Result<()> {
    if let Some(dir) = path.parent() {
        tokio::fs::create_dir_all(dir).await?;
    }
    tokio::fs::write(path, b"1").await?;
    Ok(())
}

pub async fn create_sender_marker(path: &Path) -> Result<()> {
    ensure_hotlink_ipc_marker(path).await
}

pub async fn ensure_hotlink_ipc_marker(marker_path: &Path) -> Result<()> {
    if let Some(dir) = marker_path.parent() {
        tokio::fs::create_dir_all(dir).await?;
    }
    match hotlink_ipc_mode() {
        HotlinkIpcMode::Tcp => {
            let addr = std::env::var("SYFTBOX_HOTLINK_TCP_ADDR")
                .unwrap_or_else(|_| "127.0.0.1:0".to_string());
            tokio::fs::write(marker_path, addr).await?;
        }
        HotlinkIpcMode::Pipe => {
            // Windows named pipe support is not implemented yet; keep marker empty.
            tokio::fs::write(marker_path, b"").await?;
        }
        HotlinkIpcMode::Unix => {
            tokio::fs::create_dir_all(HOTLINK_SOCKET_DIR).await?;
            let target = hotlink_socket_target(marker_path);
            tokio::fs::write(marker_path, target.to_string_lossy().as_bytes()).await?;
        }
    }
    Ok(())
}

pub enum HotlinkListener {
    #[cfg(unix)]
    Unix(tokio::net::UnixListener),
    Tcp(tokio::net::TcpListener),
}

pub enum HotlinkStream {
    #[cfg(unix)]
    Unix(tokio::net::UnixStream),
    Tcp(tokio::net::TcpStream),
}

impl HotlinkStream {
    pub async fn write_frame(&mut self, frame: &HotlinkFrame) -> Result<()> {
        let payload = encode_hotlink_frame(frame);
        match self {
            #[cfg(unix)]
            HotlinkStream::Unix(s) => {
                s.write_all(&payload).await?;
                s.flush().await?;
            }
            HotlinkStream::Tcp(s) => {
                s.write_all(&payload).await?;
                s.flush().await?;
            }
        }
        Ok(())
    }

    pub async fn read_frame(&mut self) -> Result<HotlinkFrame> {
        match self {
            #[cfg(unix)]
            HotlinkStream::Unix(s) => read_hotlink_frame(s).await,
            HotlinkStream::Tcp(s) => read_hotlink_frame(s).await,
        }
    }
}

pub async fn listen_hotlink_ipc(marker_path: &Path) -> Result<HotlinkListener> {
    ensure_hotlink_ipc_marker(marker_path).await?;
    match hotlink_ipc_mode() {
        HotlinkIpcMode::Tcp => {
            let addr = std::env::var("SYFTBOX_HOTLINK_TCP_ADDR")
                .unwrap_or_else(|_| "127.0.0.1:0".to_string());
            let listener = tokio::net::TcpListener::bind(addr).await?;
            if let Ok(addr) = listener.local_addr() {
                tokio::fs::write(marker_path, addr.to_string()).await?;
            }
            Ok(HotlinkListener::Tcp(listener))
        }
        HotlinkIpcMode::Pipe => {
            anyhow::bail!("named pipe hotlink IPC is not supported yet")
        }
        HotlinkIpcMode::Unix => {
            #[cfg(unix)]
            {
                let target = hotlink_socket_target(marker_path);
                let _ = tokio::fs::remove_file(&target).await;
                let listener = tokio::net::UnixListener::bind(&target)?;
                Ok(HotlinkListener::Unix(listener))
            }
            #[cfg(not(unix))]
            {
                anyhow::bail!("unix sockets not supported on this platform")
            }
        }
    }
}

impl HotlinkListener {
    pub async fn accept(&self, timeout: Duration) -> Result<HotlinkStream> {
        match self {
            #[cfg(unix)]
            HotlinkListener::Unix(listener) => {
                let (stream, _) = tokio::time::timeout(timeout, listener.accept()).await??;
                Ok(HotlinkStream::Unix(stream))
            }
            HotlinkListener::Tcp(listener) => {
                let (stream, _) = tokio::time::timeout(timeout, listener.accept()).await??;
                Ok(HotlinkStream::Tcp(stream))
            }
        }
    }
}

pub async fn dial_hotlink_ipc(marker_path: &Path, timeout: Duration) -> Result<HotlinkStream> {
    let deadline = tokio::time::Instant::now() + timeout;
    loop {
        if tokio::time::Instant::now() > deadline {
            anyhow::bail!("timeout waiting for ipc");
        }

        if let Ok(data) = tokio::fs::read(marker_path).await {
            let target = String::from_utf8_lossy(&data).trim().to_string();
            if target.is_empty() {
                tokio::time::sleep(Duration::from_millis(50)).await;
                continue;
            }
            if target.contains(':') && !target.starts_with('/') {
                if let Ok(stream) = tokio::net::TcpStream::connect(target.clone()).await {
                    return Ok(HotlinkStream::Tcp(stream));
                }
                tokio::time::sleep(Duration::from_millis(50)).await;
                continue;
            }
            #[cfg(unix)]
            {
                if let Ok(stream) = tokio::net::UnixStream::connect(&target).await {
                    return Ok(HotlinkStream::Unix(stream));
                }
            }
        }
        tokio::time::sleep(Duration::from_millis(50)).await;
    }
}

pub fn encode_hotlink_frame(frame: &HotlinkFrame) -> Vec<u8> {
    let path_bytes = frame.path.as_bytes();
    let etag_bytes = frame.etag.as_bytes();
    let header_len = 4 + 1 + 2 + 2 + 4 + 8;
    let total = header_len + path_bytes.len() + etag_bytes.len() + frame.payload.len();
    let mut out = Vec::with_capacity(total);
    out.extend_from_slice(HOTLINK_FRAME_MAGIC);
    out.push(HOTLINK_FRAME_VERSION);
    out.extend_from_slice(&(path_bytes.len() as u16).to_be_bytes());
    out.extend_from_slice(&(etag_bytes.len() as u16).to_be_bytes());
    out.extend_from_slice(&(frame.payload.len() as u32).to_be_bytes());
    out.extend_from_slice(&frame.seq.to_be_bytes());
    out.extend_from_slice(path_bytes);
    out.extend_from_slice(etag_bytes);
    out.extend_from_slice(&frame.payload);
    out
}

pub async fn read_hotlink_frame<R>(reader: &mut R) -> Result<HotlinkFrame>
where
    R: tokio::io::AsyncRead + Unpin,
{
    let mut window = Vec::with_capacity(HOTLINK_FRAME_MAGIC.len());
    loop {
        let b = reader.read_u8().await?;
        window.push(b);
        if window.len() > HOTLINK_FRAME_MAGIC.len() {
            window.remove(0);
        }
        if window.len() < HOTLINK_FRAME_MAGIC.len() {
            continue;
        }
        if window == HOTLINK_FRAME_MAGIC {
            break;
        }
    }

    let version = reader.read_u8().await?;
    if version != HOTLINK_FRAME_VERSION {
        anyhow::bail!("unsupported hotlink frame version: {version}");
    }
    let path_len = reader.read_u16().await? as usize;
    let etag_len = reader.read_u16().await? as usize;
    let payload_len = reader.read_u32().await? as usize;
    let seq = reader.read_u64().await?;

    let mut path = vec![0u8; path_len];
    reader.read_exact(&mut path).await?;
    let mut etag = vec![0u8; etag_len];
    reader.read_exact(&mut etag).await?;
    let mut payload = vec![0u8; payload_len];
    reader.read_exact(&mut payload).await?;

    Ok(HotlinkFrame {
        path: String::from_utf8(path).context("hotlink frame path utf8")?,
        etag: String::from_utf8(etag).context("hotlink frame etag utf8")?,
        seq,
        payload,
    })
}
