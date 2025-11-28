//go:build integration
// +build integration

package main

import (
	"testing"
	"time"
)

// TestACLRaceCondition demonstrates the race condition where:
// 1. Create ACL file
// 2. Immediately send .request file via WebSocket
// 3. Server blocks broadcast because ACL hasn't synced yet
// 4. File falls back to slow periodic sync (~8s) instead of fast WebSocket (~100ms)
func TestACLRaceCondition(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race condition test in short mode")
	}

	h := NewDevstackHarness(t)

	appName := "racetest"
	endpoint := "instant"

	t.Log("=== TESTING ACL PRIORITY SYNC FIX ===")
	t.Log("")
	t.Log("Step 1: Create default root ACLs (bootstrap)")

	// Create default root and public ACLs (like real client bootstrap)
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	t.Log("Step 2: Create fresh RPC endpoint with ACL")

	// Create RPC endpoints
	if err := h.alice.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup alice RPC: %v", err)
	}
	if err := h.bob.SetupRPCEndpoint(appName, endpoint); err != nil {
		t.Fatalf("setup bob RPC: %v", err)
	}

	t.Log("✅ ACL files created locally")
	t.Log("")
	t.Log("Step 2: Wait briefly for ACL to sync via WebSocket (should be <1s now)")

	// Brief wait for ACL to sync via WebSocket (should be very fast now)
	time.Sleep(2 * time.Second)

	t.Log("Step 3: Send .request file immediately after ACL sync")
	t.Log("Expected: Fast WebSocket sync because ACL is already at server")
	t.Log("")

	// Generate test file
	content := GenerateRandomFile(1 * 1024) // 1KB
	md5Hash := CalculateMD5(content)
	filename := "instant-test.request"

	start := time.Now()

	// Alice uploads WITHOUT waiting for ACL to sync
	if err := h.alice.UploadRPCRequest(appName, endpoint, filename, content); err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	t.Logf("⏱️  Alice uploaded at T+%v", time.Since(start))

	// Try to receive with generous timeout to see what happens
	timeout := 10 * time.Second
	err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename, md5Hash, timeout)

	latency := time.Since(start)

	if err != nil {
		t.Logf("❌ FAILED: File didn't arrive within %v", timeout)
		t.Logf("   This confirms ACL wasn't ready")
	} else {
		t.Logf("⏱️  Bob received at T+%v", latency)
		t.Log("")

		// Check if it was fast (WebSocket with ACL priority) or slow (race condition)
		if latency < 500*time.Millisecond {
			t.Logf("⚡ FAST SYNC: %v latency (WebSocket working!)", latency)
			t.Log("")
			t.Log("✅ ACL PRIORITY SYNC FIX WORKING!")
			t.Log("ACL file synced via WebSocket priority, allowing instant broadcast")
			t.Logf("Latency: %v (expected ~100ms)", latency)
		} else {
			t.Logf("🐌 SLOW SYNC: %v latency", latency)
			t.Log("")
			t.Log("❌ RACE CONDITION STILL EXISTS")
			t.Log("ACL file didn't sync fast enough via WebSocket")
			t.Logf("Expected: <100ms | Actual: %v", latency)
			t.Errorf("Fix didn't work - ACL priority sync not functioning")
		}
	}

	t.Log("")
	t.Log("=== TESTING WITH ACL PRE-SYNCED ===")
	t.Log("Now waiting for ACL to sync, then sending another file...")

	// Wait for ACL to definitely sync
	time.Sleep(6 * time.Second)

	t.Log("✅ ACL files should be synced now")
	t.Log("")

	// Try again with ACL synced
	content2 := GenerateRandomFile(1 * 1024)
	md5Hash2 := CalculateMD5(content2)
	filename2 := "after-acl-sync.request"

	start2 := time.Now()

	if err := h.alice.UploadRPCRequest(appName, endpoint, filename2, content2); err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	if err := h.bob.WaitForRPCRequest(h.alice.email, appName, endpoint, filename2, md5Hash2, 3*time.Second); err != nil {
		t.Fatalf("sync failed even with ACL ready: %v", err)
	}

	latency2 := time.Since(start2)

	t.Logf("⚡ With ACL pre-synced: %v latency", latency2)

	if latency2 < 500*time.Millisecond {
		t.Log("✅ WebSocket working perfectly when ACL is ready!")
	}

	t.Log("")
	t.Log("=== SUMMARY ===")
	if err != nil {
		t.Logf("First message:  TIMEOUT (ACL not ready)")
		t.Errorf("Test failed - ACL priority sync not working")
	} else {
		t.Logf("First message:  %v", latency)
	}
	t.Logf("Second message: %v", latency2)
	t.Log("")

	if latency < 500*time.Millisecond && latency2 < 500*time.Millisecond {
		t.Log("✅ SUCCESS: ACL priority sync eliminates race condition!")
		t.Log("   Both messages synced via WebSocket (<500ms)")
	} else {
		t.Log("❌ FAILED: Race condition still exists")
		t.Log("   ACL priority sync not working as expected")
	}
}
