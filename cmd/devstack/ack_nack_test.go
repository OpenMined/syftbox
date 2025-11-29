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

	// Setup RPC endpoint for alice
	appName := "acktest"
	endpoint := "msg"

	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}

	t.Run("SuccessfulACK", func(t *testing.T) {
		// Test that ACK is received for successful file write
		content := []byte("test message for ACK")
		filename := "test-ack.request"
		md5Hash := CalculateMD5(content)

		start := time.Now()

		// Alice uploads - this should wait for ACK before returning
		if err := h.alice.UploadFile(filename, content); err != nil {
			t.Fatalf("alice upload failed: %v", err)
		}

		uploadTime := time.Since(start)
		t.Logf("✅ Upload with ACK completed in %v", uploadTime)

		// Verify file was written (Bob should receive it via WebSocket sync)
		timeout := 10 * time.Second
		if err := h.bob.WaitForFile(h.alice.email, filename, md5Hash, timeout); err != nil {
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

			if err := h.alice.UploadFile(filename, content); err != nil {
				t.Fatalf("alice upload %d failed: %v", i, err)
			}

			// Verify Bob receives it
			timeout := 5 * time.Second
			if err := h.bob.WaitForFile(h.alice.email, filename, md5Hash, timeout); err != nil {
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
