use notify::{Config, Event, RecommendedWatcher, RecursiveMode, Watcher};
use std::path::{Path, PathBuf};
use std::sync::mpsc::{channel, Receiver};
use std::time::{Duration, Instant};
use uuid::Uuid;

const HOTLINK_BENCH_ENV: &str = "SYFTBOX_HOTLINK_BENCH";

#[test]
fn hotlink_baseline_notify_latency() -> Result<(), Box<dyn std::error::Error>> {
    if std::env::var(HOTLINK_BENCH_ENV).ok().as_deref() != Some("1") {
        eprintln!("set {HOTLINK_BENCH_ENV}=1 to run hotlink baseline latency test");
        return Ok(());
    }

    let root = std::env::temp_dir().join(format!("syftbox-hotlink-baseline-{}", Uuid::new_v4()));
    std::fs::create_dir_all(&root)?;

    let (tx, rx) = channel();
    let mut watcher = RecommendedWatcher::new(tx, Config::default())?;
    watcher.watch(&root, RecursiveMode::Recursive)?;

    // Warmup: ensure watcher is online before measuring.
    let warmup = root.join("warmup.request");
    std::fs::write(&warmup, b"warmup")?;
    if !wait_for_path(&rx, &warmup, Duration::from_secs(2)) {
        return Err("warmup event not observed".into());
    }

    let iterations = 20;
    let mut durations = Vec::with_capacity(iterations);
    for i in 0..iterations {
        let path = root.join(format!("msg-{i:03}.request"));
        let start = Instant::now();
        std::fs::write(&path, b"x")?;
        if !wait_for_path(&rx, &path, Duration::from_secs(2)) {
            return Err(format!("event not observed for {}", path.display()).into());
        }
        durations.push(start.elapsed());
    }

    report_latency_stats(&durations);
    Ok(())
}

fn wait_for_path(rx: &Receiver<notify::Result<Event>>, want: &Path, timeout: Duration) -> bool {
    let deadline = Instant::now() + timeout;
    loop {
        let now = Instant::now();
        if now >= deadline {
            return false;
        }
        let remaining = deadline - now;
        match rx.recv_timeout(remaining) {
            Ok(Ok(ev)) => {
                if event_has_path(&ev, want) {
                    return true;
                }
            }
            Ok(Err(_)) => continue,
            Err(_) => return false,
        }
    }
}

fn event_has_path(ev: &Event, want: &Path) -> bool {
    ev.paths.iter().any(|p| same_path(p, want))
}

fn same_path(a: &PathBuf, b: &Path) -> bool {
    a == b
}

fn report_latency_stats(durations: &[Duration]) {
    if durations.is_empty() {
        eprintln!("no durations collected");
        return;
    }
    let mut sorted = durations.to_vec();
    sorted.sort();
    let sum: Duration = sorted.iter().copied().sum();
    let avg = sum / sorted.len() as u32;
    let p95 = sorted[(sorted.len() - 1) * 95 / 100];
    let min = sorted[0];
    let max = sorted[sorted.len() - 1];
    eprintln!(
        "hotlink baseline (notify watcher) n={} min={:?} avg={:?} p95={:?} max={:?}",
        sorted.len(),
        min,
        avg,
        p95,
        max
    );
}
