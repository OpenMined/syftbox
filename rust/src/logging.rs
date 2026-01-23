use std::fs::{File, OpenOptions};
use std::io::Write;
use std::path::{Path, PathBuf};
use std::sync::{Mutex, OnceLock};

use anyhow::{Context, Result};
use chrono::SecondsFormat;

static LOGGER: OnceLock<Logger> = OnceLock::new();

pub fn init_log_file(path: &Path) -> Result<()> {
    if LOGGER.get().is_some() {
        return Ok(());
    }
    let logger = Logger::new(path)?;
    let _ = LOGGER.set(logger);
    Ok(())
}

pub fn init_default_log_file() -> Result<PathBuf> {
    let path = crate::config::default_log_file_path();
    init_log_file(&path)?;
    Ok(path)
}

pub fn info(msg: impl AsRef<str>) {
    log_kv("INFO", msg.as_ref(), &[]);
}

pub fn error(msg: impl AsRef<str>) {
    log_kv("ERROR", msg.as_ref(), &[]);
}

pub fn info_kv(msg: &str, kv: &[(&str, &str)]) {
    log_kv("INFO", msg, kv);
}

fn log_kv(level: &str, msg: &str, kv: &[(&str, &str)]) {
    if let Some(logger) = LOGGER.get() {
        logger.write_kv(level, msg, kv);
    }
}

struct Logger {
    file: Mutex<File>,
    mirror_to_stdout: bool,
}

impl Logger {
    fn new(path: &Path) -> Result<Self> {
        Self::new_with_stdout(path, true)
    }

    fn new_with_stdout(path: &Path, mirror_to_stdout: bool) -> Result<Self> {
        if let Some(parent) = path.parent() {
            std::fs::create_dir_all(parent)
                .with_context(|| format!("create {}", parent.display()))?;
        }
        // Match Go daemon behavior: new log file per run (truncate).
        let file = OpenOptions::new()
            .create(true)
            .truncate(true)
            .write(true)
            .open(path)
            .with_context(|| format!("open {}", path.display()))?;
        Ok(Self {
            file: Mutex::new(file),
            mirror_to_stdout,
        })
    }

    fn write_kv(&self, level: &str, msg: &str, kv: &[(&str, &str)]) {
        let ts = chrono::Utc::now().to_rfc3339_opts(SecondsFormat::Millis, true);
        let mut pretty_line = format!("{ts} {level} {msg}");
        for (k, v) in kv {
            pretty_line.push(' ');
            pretty_line.push_str(k);
            pretty_line.push('=');
            pretty_line.push_str(v);
        }
        pretty_line.push('\n');

        let mut slog_line = format!("time={ts} level={level} msg=\"{}\"", escape_slog_value(msg));
        for (k, v) in kv {
            slog_line.push(' ');
            slog_line.push_str(k);
            slog_line.push('=');
            slog_line.push_str(v);
        }
        slog_line.push('\n');
        if let Ok(mut f) = self.file.lock() {
            let _ = f.write_all(slog_line.as_bytes());
            let _ = f.flush();
        }
        if self.mirror_to_stdout {
            let mut out = std::io::stdout();
            let _ = out.write_all(pretty_line.as_bytes());
            let _ = out.flush();
        }
    }
}

fn escape_slog_value(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            '\\' => out.push_str("\\\\"),
            '"' => out.push_str("\\\""),
            '\n' => out.push_str("\\n"),
            '\r' => out.push_str("\\r"),
            '\t' => out.push_str("\\t"),
            _ => out.push(c),
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn init_log_file_truncates_and_writes() {
        let tmp = std::env::temp_dir().join("syftbox-rs-log-test");
        let _ = std::fs::remove_dir_all(&tmp);
        std::fs::create_dir_all(&tmp).unwrap();
        let log_path = tmp.join("syftbox.log");
        std::fs::write(&log_path, "old\n").unwrap();

        let logger = Logger::new_with_stdout(&log_path, false).unwrap();
        logger.write_kv(
            "INFO",
            "control plane start",
            &[("addr", "127.0.0.1:7938"), ("token", "abc")],
        );

        let raw = std::fs::read_to_string(&log_path).unwrap();
        assert!(!raw.contains("old"));
        assert!(raw.contains("level=INFO"));
        assert!(raw.contains("msg=\"control plane start\""));
        assert!(raw.contains("addr=127.0.0.1:7938"));
        assert!(raw.contains("token=abc"));
    }
}
