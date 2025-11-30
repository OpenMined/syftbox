//go:build integration
// +build integration

package main

import (
	"fmt"
	"testing"
	"time"
)

// TestLargeFileTransfer tests uploading and syncing files of various sizes
// Covers TC1.1 from performance testing plan
func TestLargeFileTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	h := NewDevstackHarness(t)
	if err := h.StartProfiling("TestLargeFileTransfer"); err != nil {
		t.Fatalf("start profiling: %v", err)
	}

	testCases := []struct {
		name string
		size int
	}{
		{"1MB", 1 * 1024 * 1024},
		{"4MB", 4 * 1024 * 1024},
		{"10MB", 10 * 1024 * 1024},
		{"50MB", 50 * 1024 * 1024},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()

			// Generate random file
			content := GenerateRandomFile(tc.size)
			md5Hash := CalculateMD5(content)
			// Large files use .bin (periodic sync), could also use .request for files <=4MB
			filename := fmt.Sprintf("test-%s.bin", tc.name)

			// Alice uploads
			if err := h.alice.UploadFile(filename, content); err != nil {
				t.Fatalf("alice upload failed: %v", err)
			}

			// Bob waits to receive
			timeout := 2 * time.Minute
			if err := h.bob.WaitForFile(h.alice.email, filename, md5Hash, timeout); err != nil {
				t.Fatalf("bob sync failed: %v", err)
			}

			elapsed := time.Since(start)
			h.metrics.RecordLatency(elapsed)

			throughputMBps := float64(tc.size) / elapsed.Seconds() / (1024 * 1024)
			h.metrics.RecordThroughput(throughputMBps)

			t.Logf("✅ %s: %v (%.2f MB/s)", tc.name, elapsed, throughputMBps)

			// Validate thresholds
			if elapsed > timeout {
				t.Errorf("Transfer took too long: %v > %v", elapsed, timeout)
			}
		})
	}

	// Generate report
	report := h.metrics.GenerateReport()
	report.Log(t)
}

// TestWebSocketLatency tests small file transfers via WebSocket priority sync
// Covers TC2.1 from performance testing plan
func TestWebSocketLatency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	h := NewDevstackHarness(t)
	if err := h.StartProfiling("TestWebSocketLatency"); err != nil {
		t.Fatalf("start profiling: %v", err)
	}

	// Create default root and public ACLs (like real client bootstrap)
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	// Setup RPC endpoint for both clients
	appName := "perftest"
	endpoint := "latency"

	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}

	// Wait for initialization in fresh environment:
	// 1. WebSocket connection (~100ms)
	// 2. Peer discovery via adaptive periodic sync (startup phase: 100ms * ~3-5 cycles = 300-500ms)
	// 3. Datasite subscription (~50-100ms)
	// 4. ACL file sync via WebSocket (~70-200ms)
	// Total: ~520-900ms for fresh environment with adaptive sync
	time.Sleep(1 * time.Second)

	testCases := []struct {
		name          string
		size          int
		maxLatency    time.Duration
	}{
		{"1KB", 1 * 1024, 100 * time.Millisecond},
		{"10KB", 10 * 1024, 150 * time.Millisecond},
		{"100KB", 100 * 1024, 300 * time.Millisecond},
		{"1MB", 1 * 1024 * 1024, 1 * time.Second},
		{"3MB", 3 * 1024 * 1024, 3 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			content := GenerateRandomFile(tc.size)
			md5Hash := CalculateMD5(content)
			filename := fmt.Sprintf("test-%s.request", tc.name)

			start := time.Now()

			// Alice uploads RPC request
			if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
				t.Fatalf("upload failed: %v", err)
			}

			// Bob waits for RPC request via WebSocket
			// Note: handlePriorityUpload has 1s sleep, so timeout must be >1s
			timeout := 3 * time.Second
			if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hash, timeout); err != nil {
				t.Fatalf("sync failed: %v", err)
			}

			latency := time.Since(start)
			h.metrics.RecordLatency(latency)

			t.Logf("✅ %s: %v (WebSocket priority sync)", tc.name, latency)

			if latency > tc.maxLatency {
				t.Logf("⚠️  Latency exceeded expected: %v > %v", latency, tc.maxLatency)
			}
		})
	}

	report := h.metrics.GenerateReport()
	report.Log(t)

	// Validate P50 latency for small files
	if report.P50Latency > 500*time.Millisecond {
		t.Errorf("P50 latency too high: %v", report.P50Latency)
	}
}

// TestConcurrentUploads tests multiple clients uploading simultaneously
// Covers TC3.2 from performance testing plan
func TestConcurrentUploads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	h := NewDevstackHarness(t)
	if err := h.StartProfiling("TestConcurrentUploads"); err != nil {
		t.Fatalf("start profiling: %v", err)
	}

	numFiles := 10
	fileSize := 1 * 1024 * 1024 // 1MB each

	t.Logf("Starting %d concurrent uploads from both clients...", numFiles*2)

	start := time.Now()

	// Upload from both clients concurrently
	done := make(chan error, numFiles*2)

	// Alice uploads
	for i := 0; i < numFiles; i++ {
		go func(idx int) {
			content := GenerateRandomFile(fileSize)
			filename := fmt.Sprintf("alice-concurrent-%d.bin", idx)
			done <- h.alice.UploadFile(filename, content)
		}(i)
	}

	// Bob uploads
	for i := 0; i < numFiles; i++ {
		go func(idx int) {
			content := GenerateRandomFile(fileSize)
			filename := fmt.Sprintf("bob-concurrent-%d.bin", idx)
			done <- h.bob.UploadFile(filename, content)
		}(i)
	}

	// Wait for all uploads
	errors := 0
	for i := 0; i < numFiles*2; i++ {
		if err := <-done; err != nil {
			t.Logf("Upload error: %v", err)
			errors++
			h.metrics.RecordError(err)
		}
	}

	elapsed := time.Since(start)
	h.metrics.RecordLatency(elapsed)

	totalMB := float64(numFiles*2*fileSize) / (1024 * 1024)
	throughputMBps := totalMB / elapsed.Seconds()
	h.metrics.RecordThroughput(throughputMBps)

	t.Logf("✅ Concurrent uploads complete: %v (%.2f MB/s, %d errors)",
		elapsed, throughputMBps, errors)

	// Wait for all syncs to complete
	time.Sleep(5 * time.Second)

	report := h.metrics.GenerateReport()
	report.Log(t)

	if errors > numFiles/10 {
		t.Errorf("Too many upload errors: %d/%d", errors, numFiles*2)
	}
}

// TestFileModificationDuringSync tests file corruption detection
// Covers TC4.1 from performance testing plan
func TestFileModificationDuringSync(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	h := NewDevstackHarness(t)
	if err := h.StartProfiling("TestFileModificationDuringSync"); err != nil {
		t.Fatalf("start profiling: %v", err)
	}

	content := GenerateRandomFile(1 * 1024 * 1024) // 1MB
	filename := "modify-test.bin"
	md5Hash := CalculateMD5(content)

	// Alice uploads
	if err := h.alice.UploadFile(filename, content); err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	// Bob waits to receive
	timeout := 30 * time.Second
	if err := h.bob.WaitForFile(h.alice.email, filename, md5Hash, timeout); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	t.Logf("✅ Initial sync successful")

	// Now Alice modifies the file
	modifiedContent := GenerateRandomFile(1 * 1024 * 1024)
	modifiedMD5 := CalculateMD5(modifiedContent)

	if err := h.alice.UploadFile(filename, modifiedContent); err != nil {
		t.Fatalf("modification upload failed: %v", err)
	}

	// Bob should receive the updated version
	if err := h.bob.WaitForFile(h.alice.email, filename, modifiedMD5, timeout); err != nil {
		t.Fatalf("sync after modification failed: %v", err)
	}

	t.Logf("✅ File modification sync successful")
}

// TestManySmallFiles tests sync performance with many small files
// Covers TC3.1 from performance testing plan
func TestManySmallFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	h := NewDevstackHarness(t)
	if err := h.StartProfiling("TestManySmallFiles"); err != nil {
		t.Fatalf("start profiling: %v", err)
	}

	// Create default root and public ACLs (like real client bootstrap)
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	// Setup RPC endpoint
	appName := "perftest"
	endpoint := "batch"

	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}

	// Wait for initialization in fresh environment:
	// 1. WebSocket connection (~100ms)
	// 2. Peer discovery via adaptive periodic sync (startup phase: 100ms * ~3-5 cycles = 300-500ms)
	// 3. Datasite subscription (~50-100ms)
	// 4. ACL file sync via WebSocket (~70-200ms)
	// Total: ~520-900ms for fresh environment with adaptive sync
	time.Sleep(1 * time.Second)

	// Test batch sizes: progressive scaling to find limits
	batchSizes := []int{1, 5, 10, 20, 50, 100}
	fileSize := 1 * 1024 // 1KB each

	for _, numFiles := range batchSizes {
		t.Logf("\n=== Testing with %d files ===", numFiles)

		start := time.Now()

		// Alice creates many small files
		filenames := make([]string, numFiles)
		md5Hashes := make([]string, numFiles)

		for i := 0; i < numFiles; i++ {
			content := GenerateRandomFile(fileSize)
			filename := fmt.Sprintf("batch%d-file%d.request", numFiles, i)
			filenames[i] = filename
			md5Hashes[i] = CalculateMD5(content)

			if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
				t.Fatalf("upload %d failed: %v", i, err)
			}
		}

		uploadTime := time.Since(start)
		h.metrics.RecordCustomMetric("upload_time", uploadTime.Seconds())

		t.Logf("✅ Uploads complete: %v", uploadTime)

		// Give Alice's periodic sync time to upload all files to server, and Bob's
		// periodic sync time to discover them. With 100ms adaptive sync intervals
		// during burst activity, files need ~10-20 sync cycles = 1-2 seconds
		sleepTime := time.Duration(numFiles*100) * time.Millisecond
		if sleepTime < 1*time.Second {
			sleepTime = 1 * time.Second
		}
		if sleepTime > 5*time.Second {
			sleepTime = 5 * time.Second
		}
		t.Logf("Waiting %v for batch upload/download to complete...", sleepTime)
		time.Sleep(sleepTime)

		// Wait for Bob to sync all files
		syncStart := time.Now()
		syncErrors := 0

		for i, filename := range filenames {
			timeout := 5 * time.Second
			if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hashes[i], timeout); err != nil {
				t.Logf("Sync error for %s: %v", filename, err)
				syncErrors++
				h.metrics.RecordError(err)
			}
		}

		syncTime := time.Since(syncStart)
		totalTime := time.Since(start)

		h.metrics.RecordLatency(totalTime)
		h.metrics.RecordCustomMetric("sync_time", syncTime.Seconds())

		t.Logf("✅ Batch %d files: upload=%v, sleep=%v, verify=%v, total=%v, errors=%d/%d",
			numFiles, uploadTime, sleepTime, syncTime, totalTime, syncErrors, numFiles)

		if syncErrors > 0 {
			t.Errorf("❌ Batch %d files: %d/%d files failed to sync", numFiles, syncErrors, numFiles)
			break // Stop testing larger batches if this one failed
		}

		// Validate reasonable performance
		avgTimePerFile := totalTime / time.Duration(numFiles)
		t.Logf("📊 Average time per file: %v", avgTimePerFile)
	}

	// Final report
	report := h.metrics.GenerateReport()
	report.Log(t)
}
