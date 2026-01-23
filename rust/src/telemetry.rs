use std::collections::VecDeque;
use std::sync::atomic::{AtomicI64, AtomicU64, Ordering};
use std::sync::Mutex;

const MAX_LATENCY_SAMPLES: usize = 60;

#[derive(Default)]
pub struct HttpStats {
    bytes_sent: AtomicI64,
    bytes_recv: AtomicI64,
    last_error: Mutex<Option<String>>,
}

pub struct LatencyStats {
    samples: Mutex<VecDeque<u64>>,
    last_ping_ms: AtomicU64,
    server_url: String,
}

impl LatencyStats {
    pub fn new(server_url: String) -> Self {
        Self {
            samples: Mutex::new(VecDeque::with_capacity(MAX_LATENCY_SAMPLES)),
            last_ping_ms: AtomicU64::new(0),
            server_url,
        }
    }

    pub fn record(&self, latency_ms: u64) {
        let mut samples = self.samples.lock().unwrap();
        if samples.len() >= MAX_LATENCY_SAMPLES {
            samples.pop_front();
        }
        samples.push_back(latency_ms);
        self.last_ping_ms.store(
            std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .unwrap_or_default()
                .as_millis() as u64,
            Ordering::Relaxed,
        );
    }

    pub fn snapshot(&self) -> LatencySnapshot {
        let samples = self.samples.lock().unwrap();
        let samples_vec: Vec<u64> = samples.iter().copied().collect();
        let (avg, min, max) = if samples_vec.is_empty() {
            (0, 0, 0)
        } else {
            let sum: u64 = samples_vec.iter().sum();
            let avg = sum / samples_vec.len() as u64;
            let min = *samples_vec.iter().min().unwrap_or(&0);
            let max = *samples_vec.iter().max().unwrap_or(&0);
            (avg, min, max)
        };
        LatencySnapshot {
            server_url: self.server_url.clone(),
            samples: samples_vec,
            avg_ms: avg,
            min_ms: min,
            max_ms: max,
            last_ping_ms: self.last_ping_ms.load(Ordering::Relaxed),
        }
    }
}

#[derive(Clone, serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct LatencySnapshot {
    pub server_url: String,
    pub samples: Vec<u64>,
    pub avg_ms: u64,
    pub min_ms: u64,
    pub max_ms: u64,
    pub last_ping_ms: u64,
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn latency_stats_records_and_calculates_correctly() {
        let stats = LatencyStats::new("https://test.example.com".to_string());

        // Initially empty
        let snap = stats.snapshot();
        assert_eq!(snap.samples.len(), 0);
        assert_eq!(snap.avg_ms, 0);
        assert_eq!(snap.server_url, "https://test.example.com");

        // Record some samples
        stats.record(10);
        stats.record(20);
        stats.record(30);

        let snap = stats.snapshot();
        assert_eq!(snap.samples.len(), 3);
        assert_eq!(snap.samples, vec![10, 20, 30]);
        assert_eq!(snap.avg_ms, 20); // (10 + 20 + 30) / 3 = 20
        assert_eq!(snap.min_ms, 10);
        assert_eq!(snap.max_ms, 30);
        assert!(snap.last_ping_ms > 0);
    }

    #[test]
    fn latency_stats_respects_max_samples() {
        let stats = LatencyStats::new("https://test.example.com".to_string());

        // Record more than MAX_LATENCY_SAMPLES
        for i in 0..70 {
            stats.record(i as u64);
        }

        let snap = stats.snapshot();
        assert_eq!(snap.samples.len(), MAX_LATENCY_SAMPLES);
        // Should have samples 10-69 (first 10 dropped)
        assert_eq!(snap.samples[0], 10);
        assert_eq!(snap.samples[59], 69);
    }
}
