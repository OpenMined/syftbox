package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

const hotlinkBenchEnv = "SYFTBOX_HOTLINK_BENCH"

func TestHotlinkBaselineFileWatcherLatency(t *testing.T) {
	if os.Getenv(hotlinkBenchEnv) != "1" {
		t.Skipf("set %s=1 to run hotlink baseline latency test", hotlinkBenchEnv)
	}

	dir := t.TempDir()
	fw := NewFileWatcher(dir)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := fw.Start(ctx); err != nil {
		t.Fatalf("start watcher: %v", err)
	}
	defer fw.Stop()

	// Warmup: ensure watcher is online before measuring.
	warmupPath := filepath.Join(dir, "warmup.request")
	if err := os.WriteFile(warmupPath, []byte("warmup"), 0o644); err != nil {
		t.Fatalf("warmup write: %v", err)
	}
	if !waitForWatcherPath(t, fw, warmupPath, 2*time.Second) {
		t.Fatalf("warmup event not observed")
	}

	const iterations = 20
	durations := make([]time.Duration, 0, iterations)
	for i := 0; i < iterations; i++ {
		path := filepath.Join(dir, fmt.Sprintf("msg-%03d.request", i))
		start := time.Now()
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		if !waitForWatcherPath(t, fw, path, 2*time.Second) {
			t.Fatalf("event not observed for %s", path)
		}
		durations = append(durations, time.Since(start))
	}

	reportLatencyStats(t, durations)
}

func waitForWatcherPath(t *testing.T, fw *FileWatcher, want string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		select {
		case <-deadline.C:
			return false
		case ev, ok := <-fw.Events():
			if !ok {
				return false
			}
			if ev.Path() == want {
				return true
			}
		}
	}
}

func reportLatencyStats(t *testing.T, durations []time.Duration) {
	t.Helper()
	if len(durations) == 0 {
		t.Fatalf("no durations collected")
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	var sum time.Duration
	for _, d := range durations {
		sum += d
	}
	avg := sum / time.Duration(len(durations))
	p95 := durations[(len(durations)-1)*95/100]
	min := durations[0]
	max := durations[len(durations)-1]

	t.Logf("hotlink baseline (file watcher) n=%d min=%s avg=%s p95=%s max=%s", len(durations), min, avg, p95, max)
}
