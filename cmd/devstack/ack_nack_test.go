//go:build integration
// +build integration

package main

import (
	"fmt"
	"testing"
	"time"
)

// TestACKNACKMechanism tests that ACK/NACK messages are properly sent and received
func TestACKNACKMechanism(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping ACK/NACK test in short mode")
	}

	h := NewDevstackHarness(t)

	// Create default ACLs for both clients
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	// Setup RPC endpoint for both Alice and Bob (like WebSocket latency test)
	appName := "acktest"
	endpoint := "msg"

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

	t.Run("SuccessfulACK", func(t *testing.T) {
		// Test that ACK is received for successful file write
		content := []byte("test message for ACK")
		filename := "test-ack.request"
		md5Hash := CalculateMD5(content)

		start := time.Now()

		// Alice uploads RPC request - this should wait for ACK before returning
		if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
			t.Fatalf("alice upload failed: %v", err)
		}

		uploadTime := time.Since(start)
		t.Logf("✅ Upload with ACK completed in %v", uploadTime)

		// Verify file was written (Bob should receive it via WebSocket sync)
		timeout := 3 * time.Second
		if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hash, timeout); err != nil {
			t.Fatalf("bob didn't receive file: %v", err)
		}

		// ACK should be faster than the old 1-second sleep hack
		if uploadTime > 2*time.Second {
			t.Errorf("Upload took too long (%v), ACK mechanism may not be working", uploadTime)
		}
	})

	t.Run("MultipleFilesWithACK", func(t *testing.T) {
		// Test that ACK works correctly for multiple files in quick succession
		numFiles := 10
		start := time.Now()

		for i := 0; i < numFiles; i++ {
			content := GenerateRandomFile(1024) // 1KB files
			md5Hash := CalculateMD5(content)
			filename := fmt.Sprintf("multi-ack-%d.request", i)

			if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
				t.Fatalf("alice upload %d failed: %v", i, err)
			}

			// Verify Bob receives it
			timeout := 3 * time.Second
			if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hash, timeout); err != nil {
				t.Fatalf("bob didn't receive file %d: %v", i, err)
			}
		}

		totalTime := time.Since(start)
		avgPerFile := totalTime / time.Duration(numFiles)

		t.Logf("✅ %d files with ACK completed in %v (avg %v per file)", numFiles, totalTime, avgPerFile)

		// With old 1-second sleep, this would take >10 seconds
		// With ACK, should be much faster
		if totalTime > 10*time.Second {
			t.Errorf("Multiple files took too long (%v), ACK mechanism may not be working", totalTime)
		}
	})

	// Generate report
	report := h.metrics.GenerateReport()
	report.Log(t)
}
