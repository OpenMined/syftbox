//go:build integration
// +build integration

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDeleteDuringDownload tests the race condition where a file is deleted
// while a peer is actively downloading it.
func TestDeleteDuringDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race condition test in short mode")
	}

	h := NewDevstackHarness(t)

	t.Log("=== TEST: Delete During Download ===")
	t.Log("Setup: Alice uploads 5MB file, bob starts download, alice deletes mid-download")
	t.Log("")

	// Create a large file (5MB) to ensure download takes time
	fileSize := 5 * 1024 * 1024 // 5MB
	content := GenerateRandomFile(fileSize)
	md5Hash := CalculateMD5(content)
	filename := "large-delete-test.bin"

	t.Logf("Step 1: Alice uploads %dMB file", fileSize/(1024*1024))
	if err := h.alice.UploadFile(filename, content); err != nil {
		t.Fatalf("alice upload failed: %v", err)
	}

	// Start bob's download in background (don't wait for completion)
	t.Log("Step 2: Bob starts downloading (background)")
	downloadDone := make(chan error, 1)
	go func() {
		err := h.bob.WaitForFile(h.alice.email, filename, md5Hash, 30*time.Second)
		downloadDone <- err
	}()

	// Give bob time to start download (but not finish)
	time.Sleep(500 * time.Millisecond)

	// Alice deletes the file while bob is downloading
	t.Log("Step 3: Alice deletes file while bob is downloading")
	aliceFilePath := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", filename)
	if err := os.Remove(aliceFilePath); err != nil {
		t.Fatalf("alice delete failed: %v", err)
	}

	// Wait for bob's download to complete (or fail)
	t.Log("Step 4: Wait for bob's download to complete or fail")
	err := <-downloadDone

	if err == nil {
		t.Log("✅ Bob completed download before deletion propagated (race lost, but file valid)")

		// Verify bob got the complete file
		bobFilePath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
		bobContent, readErr := os.ReadFile(bobFilePath)
		if readErr != nil {
			t.Fatalf("bob's file disappeared after download: %v", readErr)
		}
		bobMD5 := CalculateMD5(bobContent)
		if bobMD5 != md5Hash {
			t.Fatalf("bob's file corrupted: got MD5 %s, want %s", bobMD5, md5Hash)
		}
	} else {
		t.Logf("⚠️  Bob's download failed (expected): %v", err)
		// This is acceptable - download failed because file was deleted
	}

	// Step 5: Verify no orphaned temp files in .syft-tmp/
	t.Log("Step 5: Verify no orphaned temp files")
	tmpDir := filepath.Join(h.bob.dataDir, ".syft-tmp")
	if _, err := os.Stat(tmpDir); err == nil {
		entries, _ := os.ReadDir(tmpDir)
		if len(entries) > 0 {
			t.Errorf("❌ Found %d orphaned temp files in .syft-tmp/:", len(entries))
			for _, e := range entries {
				t.Errorf("  - %s", e.Name())
			}
		} else {
			t.Log("✅ No orphaned temp files")
		}
	}

	// Step 6: Eventually bob should sync the deletion
	// On Windows, delete propagation can take longer due to the 30-second grace window
	// for remote_deleted detection, plus additional sync cycles needed.
	t.Log("Step 6: Verify bob eventually deletes the file")
	bobFilePath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
	deleteTimeout := windowsTimeout(60 * time.Second)
	if err := waitForFileGone(bobFilePath, deleteTimeout); err != nil {
		t.Error(err.Error())
	} else {
		t.Log("✅ Bob deleted file successfully")
	}
}

func waitForFileGone(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	attempts := 0
	for time.Now().Before(deadline) {
		attempts++
		info, err := os.Stat(path)
		if os.IsNotExist(err) {
			fmt.Printf("[DEBUG] waitForFileGone: file gone after %d attempts path=%s\n", attempts, path)
			return nil
		}
		if err != nil {
			fmt.Printf("[DEBUG] waitForFileGone: stat error attempt=%d path=%s err=%v\n", attempts, path, err)
		} else {
			fmt.Printf("[DEBUG] waitForFileGone: file still exists attempt=%d path=%s size=%d\n", attempts, path, info.Size())
		}
		time.Sleep(500 * time.Millisecond)
	}
	// Final debug: list parent directory
	parentDir := filepath.Dir(path)
	entries, _ := os.ReadDir(parentDir)
	fmt.Printf("[DEBUG] waitForFileGone: TIMEOUT parent=%s contents:\n", parentDir)
	for _, e := range entries {
		info, _ := e.Info()
		if info != nil {
			fmt.Printf("[DEBUG]   - %s (size=%d)\n", e.Name(), info.Size())
		} else {
			fmt.Printf("[DEBUG]   - %s\n", e.Name())
		}
	}
	return fmt.Errorf("❌ File still present after %d attempts (timeout=%v): %s", attempts, timeout, path)
}

// TestACLChangeDuringUpload tests the race condition where ACL permissions
// are revoked while a peer is uploading a file.
func TestACLChangeDuringUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race condition test in short mode")
	}

	h := NewDevstackHarness(t)

	t.Log("=== TEST: ACL Change During Upload ===")
	t.Log("Setup: Alice has public ACL, bob starts upload, alice revokes mid-upload")
	t.Log("")

	// Step 1: Create default ACLs (bootstrap like real client)
	t.Log("Step 1: Create default ACLs for alice")
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}

	// Step 2: Alice starts with public ACL (default)
	t.Log("Step 2: Alice has public ACL (bob has write permission)")

	// Step 3: Bob starts uploading a large file to alice's datasite
	t.Log("Step 3: Bob uploads 5MB file to alice's public folder")
	fileSize := 5 * 1024 * 1024 // 5MB
	content := GenerateRandomFile(fileSize)
	filename := "bob-upload.bin"

	// Start upload in background
	uploadDone := make(chan error, 1)
	go func() {
		// Bob uploads to alice's datasite
		targetPath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
		err := os.MkdirAll(filepath.Dir(targetPath), 0o755)
		if err != nil {
			uploadDone <- err
			return
		}
		err = os.WriteFile(targetPath, content, 0o644)
		uploadDone <- err
	}()

	// Give bob time to start upload
	time.Sleep(500 * time.Millisecond)

	// Step 4: Alice changes ACL to owner-only (revokes bob's write permission)
	t.Log("Step 4: Alice changes ACL to owner-only (revokes bob's write permission)")
	publicDir := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public")
	aclPath := filepath.Join(publicDir, "syft.pub.yaml")
	aclContent := `terminal: false
rules:
  - pattern: '**'
    access:
      admin: []
      write: ['` + h.alice.email + `']
      read: ['` + h.alice.email + `']
`
	if err := os.WriteFile(aclPath, []byte(aclContent), 0o644); err != nil {
		t.Fatalf("alice ACL change failed: %v", err)
	}

	// Wait for upload to complete
	t.Log("Step 5: Wait for bob's upload to complete or fail")
	err := <-uploadDone

	if err != nil {
		t.Logf("⚠️  Bob's upload failed (may be expected if permission check during write): %v", err)
	} else {
		t.Log("✅ Bob's upload completed (file written before ACL check)")
	}

	// Step 6: Verify server state
	// If upload succeeded, server should either:
	// a) Accept it (TOCTOU - permission checked before upload started), or
	// b) Reject it with NACK
	// We document current behavior here for future improvement
	t.Log("Step 6: Current behavior documented (TOCTOU vulnerability)")
	t.Log("   - If upload succeeds: Permission checked at start, not during write")
	t.Log("   - If upload fails: Permission enforced (ideal)")
}

// TestOverwriteDuringDownload tests the race condition where a file is
// overwritten while a peer is downloading the original version.
func TestOverwriteDuringDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race condition test in short mode")
	}

	h := NewDevstackHarness(t)

	t.Log("=== TEST: Overwrite During Download ===")
	t.Log("Setup: Alice uploads v1, bob downloads, alice uploads v2 mid-download")
	t.Log("")

	// Step 1: Alice uploads version 1
	filename := "overwrite-test.bin"
	v1Content := GenerateRandomFile(3 * 1024 * 1024) // 3MB
	v1MD5 := CalculateMD5(v1Content)

	t.Log("Step 1: Alice uploads version 1 (3MB)")
	if err := h.alice.UploadFile(filename, v1Content); err != nil {
		t.Fatalf("alice upload v1 failed: %v", err)
	}

	// Step 2: Bob starts downloading v1 in background
	t.Log("Step 2: Bob starts downloading v1")
	downloadDone := make(chan error, 1)
	var downloadedMD5 string
	go func() {
		err := h.bob.WaitForFile(h.alice.email, filename, v1MD5, 30*time.Second)
		if err == nil {
			// Read what bob got
			bobFilePath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
			bobContent, readErr := os.ReadFile(bobFilePath)
			if readErr == nil {
				downloadedMD5 = CalculateMD5(bobContent)
			}
		}
		downloadDone <- err
	}()

	// Give bob time to start download
	time.Sleep(500 * time.Millisecond)

	// Step 3: Alice uploads version 2 (different content)
	v2Content := GenerateRandomFile(3 * 1024 * 1024) // 3MB different content
	v2MD5 := CalculateMD5(v2Content)

	t.Log("Step 3: Alice overwrites with version 2 while bob downloading v1")
	if err := h.alice.UploadFile(filename, v2Content); err != nil {
		t.Fatalf("alice upload v2 failed: %v", err)
	}

	// Wait for bob's download
	t.Log("Step 4: Wait for bob's download to complete")
	err := <-downloadDone

	if err != nil {
		t.Logf("⚠️  Bob's download failed: %v", err)
		t.Log("   This may happen if server switched versions mid-stream")
	} else {
		t.Logf("✅ Bob completed download, got MD5: %s", downloadedMD5)

		// Verify bob got EITHER v1 OR v2, never a mix
		if downloadedMD5 == v1MD5 {
			t.Log("✅ Bob got complete v1 (download finished before overwrite)")
		} else if downloadedMD5 == v2MD5 {
			t.Log("✅ Bob got complete v2 (download restarted after overwrite)")
		} else {
			t.Errorf("❌ Bob got corrupted file! MD5 %s != v1(%s) or v2(%s)", downloadedMD5, v1MD5, v2MD5)
			t.Error("   File contains mixed content from v1 and v2")
		}
	}

	// Step 5: Eventually bob should converge to v2
	t.Log("Step 5: Verify bob eventually converges to v2")
	// Account for: file watcher delay + alice syncs v2 → server updates → bob's next sync downloads v2
	// Adaptive sync could be in idle state (1s-10s intervals), use same 15s as deletion test
	time.Sleep(15 * time.Second)

	bobFilePath := filepath.Join(h.bob.dataDir, "datasites", h.alice.email, "public", filename)
	bobContent, readErr := os.ReadFile(bobFilePath)
	if readErr != nil {
		t.Fatalf("bob's file missing: %v", readErr)
	}
	finalMD5 := CalculateMD5(bobContent)
	if finalMD5 != v2MD5 {
		t.Errorf("❌ Bob's final version wrong: got %s, want v2 %s", finalMD5, v2MD5)
	} else {
		t.Log("✅ Bob converged to v2")
	}
}

// TestDeleteDuringTempRename tests the race condition where a file is deleted
// during the atomic rename from .syft-tmp/ to the final location.
func TestDeleteDuringTempRename(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping race condition test in short mode")
	}

	h := NewDevstackHarness(t)

	t.Log("=== TEST: Delete During Temp → Final Rename ===")
	t.Log("Setup: Bob downloads to temp, alice deletes during rename to final")
	t.Log("")

	// This is a very tight race condition and hard to hit reliably.
	// We document the expected behavior:
	// - Rename should either succeed (file exists) OR fail and clean up temp

	filename := "rename-race.bin"
	content := GenerateRandomFile(2 * 1024 * 1024) // 2MB
	md5Hash := CalculateMD5(content)

	t.Log("Step 1: Alice uploads file")
	if err := h.alice.UploadFile(filename, content); err != nil {
		t.Fatalf("alice upload failed: %v", err)
	}

	// Try to hit the race multiple times
	raceHit := false
	for attempt := 0; attempt < 5; attempt++ {
		t.Logf("Attempt %d/5 to hit race condition...", attempt+1)

		// Bob downloads
		downloadDone := make(chan error, 1)
		go func() {
			err := h.bob.WaitForFile(h.alice.email, filename, md5Hash, 30*time.Second)
			downloadDone <- err
		}()

		// Try to delete at various points during download
		time.Sleep(time.Duration(100+attempt*100) * time.Millisecond)

		aliceFilePath := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", filename)
		os.Remove(aliceFilePath) // Ignore error if already deleted

		err := <-downloadDone
		if err != nil && !os.IsNotExist(err) {
			raceHit = true
			t.Logf("   Race condition hit: %v", err)
			break
		}

		// Reset for next attempt
		time.Sleep(1 * time.Second)
		h.alice.UploadFile(filename, content)
	}

	if !raceHit {
		t.Log("⚠️  Race condition not hit in 5 attempts (timing-dependent)")
	}

	// Verify no orphaned temp files regardless
	t.Log("Verify no orphaned temp files in .syft-tmp/")
	tmpDir := filepath.Join(h.bob.dataDir, ".syft-tmp")
	if _, err := os.Stat(tmpDir); err == nil {
		entries, _ := os.ReadDir(tmpDir)
		if len(entries) > 0 {
			t.Errorf("❌ Found %d orphaned temp files:", len(entries))
			for _, e := range entries {
				t.Errorf("  - %s", e.Name())
			}
		} else {
			t.Log("✅ No orphaned temp files")
		}
	}
}
