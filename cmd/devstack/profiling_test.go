//go:build integration
// +build integration

package main

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

type ResourceSnapshot struct {
	Timestamp   time.Time
	AllocMB     float64
	TotalAllocMB float64
	SysMB       float64
	NumGC       uint32
	Goroutines  int
}

type ResourceTracker struct {
	mu        sync.Mutex
	snapshots []ResourceSnapshot
	stopCh    chan struct{}
}

func NewResourceTracker() *ResourceTracker {
	return &ResourceTracker{
		snapshots: make([]ResourceSnapshot, 0),
		stopCh:    make(chan struct{}),
	}
}

func (rt *ResourceTracker) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				rt.Snapshot()
			case <-rt.stopCh:
				return
			}
		}
	}()
}

func (rt *ResourceTracker) Snapshot() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	snapshot := ResourceSnapshot{
		Timestamp:    time.Now(),
		AllocMB:      float64(m.Alloc) / 1024 / 1024,
		TotalAllocMB: float64(m.TotalAlloc) / 1024 / 1024,
		SysMB:        float64(m.Sys) / 1024 / 1024,
		NumGC:        m.NumGC,
		Goroutines:   runtime.NumGoroutine(),
	}

	rt.mu.Lock()
	rt.snapshots = append(rt.snapshots, snapshot)
	rt.mu.Unlock()
}

func (rt *ResourceTracker) Stop() {
	close(rt.stopCh)
	rt.Snapshot() // Final snapshot
}

func (rt *ResourceTracker) Report(t *testing.T) {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if len(rt.snapshots) == 0 {
		return
	}

	first := rt.snapshots[0]
	last := rt.snapshots[len(rt.snapshots)-1]

	var maxAlloc, maxSys float64
	var totalGC uint32
	for _, s := range rt.snapshots {
		if s.AllocMB > maxAlloc {
			maxAlloc = s.AllocMB
		}
		if s.SysMB > maxSys {
			maxSys = s.SysMB
		}
		if s.NumGC > totalGC {
			totalGC = s.NumGC
		}
	}

	t.Logf("\n📊 Resource Usage Summary:")
	t.Logf("  Memory - Current: %.2f MB | Peak: %.2f MB | System: %.2f MB",
		last.AllocMB, maxAlloc, maxSys)
	t.Logf("  Total Allocated: %.2f MB", last.TotalAllocMB)
	t.Logf("  Garbage Collections: %d", totalGC)
	t.Logf("  Goroutines - Start: %d | End: %d", first.Goroutines, last.Goroutines)
	t.Logf("  Duration: %v", last.Timestamp.Sub(first.Timestamp))
}

// TestProfilePerformance runs various performance scenarios with profiling enabled
// to generate flame graphs for identifying bottlenecks.
//
// Run with: PERF_PROFILE=1 just sbdev-test-profile
// Generate flame graphs: just sbdev-flamegraph
func TestProfilePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping profiling test in short mode")
	}

	h := NewDevstackHarness(t)

	// Start resource tracking (sample every 100ms)
	tracker := NewResourceTracker()
	tracker.Start(100 * time.Millisecond)
	defer func() {
		tracker.Stop()
		tracker.Report(t)
	}()

	// Start profiling
	if err := h.StartProfiling("performance_profile"); err != nil {
		t.Fatalf("start profiling: %v", err)
	}

	// Create default ACLs
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	// Setup RPC endpoint
	appName := "perftest"
	endpoint := "profile"
	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}

	// Wait for WebSocket connections
	time.Sleep(500 * time.Millisecond)

	t.Run("SmallFileBurst", func(t *testing.T) {
		// Simulate rapid small file transfers (tests WebSocket sync + ACK)
		numFiles := 50
		start := time.Now()

		for i := 0; i < numFiles; i++ {
			content := GenerateRandomFile(1024) // 1KB files
			md5Hash := CalculateMD5(content)
			filename := fmt.Sprintf("small-burst-%d.request", i)

			if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
				t.Fatalf("alice upload %d failed: %v", i, err)
			}

			timeout := 5 * time.Second
			if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hash, timeout); err != nil {
				t.Fatalf("bob didn't receive file %d: %v", i, err)
			}
		}

		elapsed := time.Since(start)
		avgPerFile := elapsed / time.Duration(numFiles)
		t.Logf("📊 Small file burst: %d files in %v (avg %v per file)", numFiles, elapsed, avgPerFile)
	})

	t.Run("MediumFileMix", func(t *testing.T) {
		// Mix of medium-sized files (tests MinIO overhead + sync polling)
		sizes := []int{
			10 * 1024,   // 10KB
			50 * 1024,   // 50KB
			100 * 1024,  // 100KB
			500 * 1024,  // 500KB
			1024 * 1024, // 1MB
		}

		start := time.Now()

		for i, size := range sizes {
			content := GenerateRandomFile(size)
			md5Hash := CalculateMD5(content)
			filename := fmt.Sprintf("medium-mix-%d.request", i)

			if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
				t.Fatalf("alice upload %d (%d bytes) failed: %v", i, size, err)
			}

			timeout := 10 * time.Second
			if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hash, timeout); err != nil {
				t.Fatalf("bob didn't receive file %d: %v", i, err)
			}
		}

		elapsed := time.Since(start)
		totalSize := 0
		for _, s := range sizes {
			totalSize += s
		}
		throughput := float64(totalSize) / elapsed.Seconds() / (1024 * 1024)
		t.Logf("📊 Medium file mix: %d files (%d MB) in %v (%.2f MB/s)", len(sizes), totalSize/(1024*1024), elapsed, throughput)
	})

	t.Run("BidirectionalTransfer", func(t *testing.T) {
		// Both clients sending simultaneously (tests concurrency + contention)
		numFiles := 20
		start := time.Now()

		// Alice sends to Bob
		for i := 0; i < numFiles; i++ {
			content := GenerateRandomFile(10 * 1024) // 10KB
			md5Hash := CalculateMD5(content)
			filename := fmt.Sprintf("alice-to-bob-%d.request", i)

			if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
				t.Fatalf("alice upload %d failed: %v", i, err)
			}

			timeout := 5 * time.Second
			if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hash, timeout); err != nil {
				t.Fatalf("bob didn't receive alice's file %d: %v", i, err)
			}
		}

		// Bob sends to Alice
		for i := 0; i < numFiles; i++ {
			content := GenerateRandomFile(10 * 1024) // 10KB
			md5Hash := CalculateMD5(content)
			filename := fmt.Sprintf("bob-to-alice-%d.request", i)

			if err := h.bob.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
				t.Fatalf("bob upload %d failed: %v", i, err)
			}

			timeout := 5 * time.Second
			if err := h.alice.WaitForRPCRequest(h.bob.email, appName, endpoint, filename, md5Hash, timeout); err != nil {
				t.Fatalf("alice didn't receive bob's file %d: %v", i, err)
			}
		}

		elapsed := time.Since(start)
		totalFiles := numFiles * 2
		avgPerFile := elapsed / time.Duration(totalFiles)
		t.Logf("📊 Bidirectional: %d files total in %v (avg %v per file)", totalFiles, elapsed, avgPerFile)
	})

	t.Run("LargeFileSingle", func(t *testing.T) {
		// Single large file transfer (tests throughput limits)
		size := 10 * 1024 * 1024 // 10MB
		content := GenerateRandomFile(size)
		md5Hash := CalculateMD5(content)
		filename := "large-single.request"

		start := time.Now()

		if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
			t.Fatalf("alice upload failed: %v", err)
		}

		timeout := 30 * time.Second
		if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hash, timeout); err != nil {
			t.Fatalf("bob didn't receive file: %v", err)
		}

		elapsed := time.Since(start)
		throughput := float64(size) / elapsed.Seconds() / (1024 * 1024)
		t.Logf("📊 Large file: %d MB in %v (%.2f MB/s)", size/(1024*1024), elapsed, throughput)
	})

	// Generate report
	report := h.metrics.GenerateReport()
	report.Log(t)

	t.Logf("\n🔥 Profile data saved to: cmd/devstack/profiles/performance_profile/")
	t.Logf("Generate flame graph with: just sbdev-flamegraph")
}
