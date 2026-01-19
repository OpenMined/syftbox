//go:build integration
// +build integration

package main

import (
	"testing"
	"time"
)

// TestACLEnablesDownload verifies that uploading an ACL file allows other users
// to download files from the owner's datasite via periodic sync (non-priority files).
//
// This test catches the bug where the server doesn't load ACL rules when ACL files
// are uploaded via multipart upload, causing Bob's GetView() to return empty.
func TestACLEnablesDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := NewDevstackHarness(t)

	// Step 1: Create ACLs for both alice and bob (with public read)
	t.Log("Step 1: Creating default ACLs...")
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	// Step 2: Wait for ACL files to sync to server (periodic sync runs every 100ms-5s)
	t.Log("Step 2: Waiting for ACL files to sync to server...")
	time.Sleep(3 * time.Second)

	// Step 3: Alice uploads a small .bin file (non-priority file, periodic sync only)
	t.Log("Step 3: Alice uploading test file...")
	testContent := []byte("test-content-for-bob")
	md5Hash := CalculateMD5(testContent)
	filename := "test-download.bin"

	if err := h.alice.UploadFile(filename, testContent); err != nil {
		t.Fatalf("alice upload failed: %v", err)
	}

	// Step 4: Bob should receive the file via periodic sync
	// This verifies the server loaded Alice's ACL rules correctly
	t.Log("Step 4: Bob waiting for file (15s timeout)...")
	if err := h.bob.WaitForFile(h.alice.email, filename, md5Hash, windowsTimeout(15*time.Second)); err != nil {
		t.Fatalf("bob sync failed: %v\n\nThis likely means the server didn't load Alice's ACL rules when they were uploaded.", err)
	}

	t.Log("✅ SUCCESS: ACL enabled download - Bob received Alice's file")
}
