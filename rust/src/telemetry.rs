use std::sync::atomic::{AtomicI64, Ordering};
use std::sync::Mutex;

#[derive(Default)]
pub struct HttpStats {
    bytes_sent: AtomicI64,
    bytes_recv: AtomicI64,
    last_error: Mutex<Option<String>>,
}

impl HttpStats {
    pub fn on_send(&self, n: i64) {
        if n <= 0 {
            return;
        }
        self.bytes_sent.fetch_add(n, Ordering::Relaxed);
    }

    pub fn on_recv(&self, n: i64) {
        if n <= 0 {
            return;
        }
        self.bytes_recv.fetch_add(n, Ordering::Relaxed);
    }

    pub fn set_last_error(&self, err: impl ToString) {
        let mut g = self.last_error.lock().unwrap();
        *g = Some(err.to_string());
    }

    pub fn snapshot(&self) -> HttpStatsSnapshot {
        let last_error = self.last_error.lock().unwrap().clone().unwrap_or_default();
        HttpStatsSnapshot {
            bytes_sent_total: self.bytes_sent.load(Ordering::Relaxed),
            bytes_recv_total: self.bytes_recv.load(Ordering::Relaxed),
            last_error,
        }
    }
}

#[derive(Clone)]
pub struct HttpStatsSnapshot {
    pub bytes_sent_total: i64,
    pub bytes_recv_total: i64,
    pub last_error: String,
}
