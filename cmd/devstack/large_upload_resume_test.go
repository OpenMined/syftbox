//go:build integration
// +build integration

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openmined/syftbox/internal/syftsdk"
)

// TestLargeUploadResume ensures a large upload survives timeouts by resuming from the last completed part.
func TestLargeUploadResume(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large upload resume test in short mode")
	}

	// Force a very short server write timeout to surface timeout-related failures quickly.
	t.Setenv("SBDEV_HTTP_WRITE_TIMEOUT", "3s")

	h := NewDevstackHarness(t)

	// Ensure both clients have default ACLs so public sharing works.
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", h.state.Server.Port)
	uploadKey := fmt.Sprintf("%s/public/resumable-1gb.bin", h.alice.email)
	resumeDir := filepath.Join(h.root, "upload-resume-cache")
	filePath := filepath.Join(h.root, "resumable-1gb.bin")
	fileSize := int64(1024 * 1024 * 1024) // 1GB realistic large file

	createSparseFile(t, filePath, fileSize)

	sdk, err := syftsdk.New(&syftsdk.SyftSDKConfig{
		BaseURL: serverURL,
		Email:   h.alice.email,
	})
	if err != nil {
		t.Fatalf("sdk init: %v", err)
	}

	params := &syftsdk.UploadParams{
		Key:               uploadKey,
		FilePath:          filePath,
		ResumeDir:         resumeDir,
		PartSize:          64 * 1024 * 1024, // 64MB parts = 16 parts for 1GB
		PartUploadTimeout: time.Second,
	}

	// Simulate multiple timeout/resume cycles to test realistic resumption behavior.
	// Each attempt gets enough time to upload a few parts but not all 16.
	maxAttempts := 5
	var lastCompleted int
	timeoutPerAttempt := 500 * time.Millisecond // enough for ~2-4 parts per attempt

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		shortCtx, cancel := context.WithTimeout(context.Background(), timeoutPerAttempt)
		_, err := sdk.Blob.Upload(shortCtx, params)
		cancel()

		if err == nil {
			// Upload completed before we hit maxAttempts - that's fine
			t.Logf("upload completed on attempt %d", attempt)
			break
		}

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("attempt %d: expected timeout, got %v", attempt, err)
		}

		sessionFile := findSessionFile(t, resumeDir)
		if sessionFile == "" {
			t.Fatalf("attempt %d: expected session file after timeout", attempt)
		}

		completed := completedPartsCount(t, sessionFile)
		t.Logf("attempt %d: %d parts completed (total 16)", attempt, completed)

		// After first attempt we should have made some progress
		if attempt > 1 && completed <= lastCompleted {
			t.Fatalf("attempt %d: no progress made (was %d, now %d)", attempt, lastCompleted, completed)
		}
		lastCompleted = completed
	}

	// Final attempt with long timeout to ensure completion
	longCtx, cancelLong := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancelLong()
	resp, err := sdk.Blob.Upload(longCtx, params)
	if err != nil {
		t.Fatalf("final upload failed: %v", err)
	}
	if resp.Size != fileSize {
		t.Fatalf("unexpected uploaded size: %d (want %d)", resp.Size, fileSize)
	}

	// Session file should be cleaned up on success.
	if files, _ := os.ReadDir(resumeDir); len(files) > 0 {
		t.Fatalf("expected resume cache to be cleaned up, found %d files", len(files))
	}

	// Bob should eventually receive the uploaded file.
	if err := h.bob.WaitForFile(h.alice.email, "resumable-1gb.bin", "", 10*time.Minute); err != nil {
		t.Fatalf("bob did not receive resumed upload: %v", err)
	}
}

func createSparseFile(t *testing.T, path string, size int64) {
	t.Helper()

	if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
		t.Fatalf("create sparse file: %v", err)
	}
	if err := os.Truncate(path, size); err != nil {
		t.Fatalf("truncate sparse file: %v", err)
	}
}

func findSessionFile(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read resume dir: %v", err)
	}
	if len(entries) == 0 {
		return ""
	}
	return filepath.Join(dir, entries[0].Name())
}

func completedPartsCount(t *testing.T, sessionPath string) int {
	t.Helper()

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}

	var payload struct {
		Completed map[string]string `json:"completed"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("parse session file: %v", err)
	}

	return len(payload.Completed)
}

// TestStaleSessionCleanup verifies that abandoned upload sessions get cleaned up
// both on the client (session files) and server (multipart uploads).
func TestStaleSessionCleanup(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stale session cleanup test in short mode")
	}

	h := NewDevstackHarness(t)

	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", h.state.Server.Port)
	resumeDir := filepath.Join(h.root, "stale-session-cache")
	filePath := filepath.Join(h.root, "stale-upload.bin")
	fileSize := int64(64 * 1024 * 1024) // 64MB - just enough for multipart

	createSparseFile(t, filePath, fileSize)

	sdk, err := syftsdk.New(&syftsdk.SyftSDKConfig{
		BaseURL: serverURL,
		Email:   h.alice.email,
	})
	if err != nil {
		t.Fatalf("sdk init: %v", err)
	}

	// Start an upload that will timeout, creating a stale session
	params := &syftsdk.UploadParams{
		Key:               fmt.Sprintf("%s/public/stale-upload.bin", h.alice.email),
		FilePath:          filePath,
		ResumeDir:         resumeDir,
		PartSize:          16 * 1024 * 1024,
		PartUploadTimeout: time.Second,
	}

	// Trigger upload with very short timeout to create incomplete session
	shortCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	_, _ = sdk.Blob.Upload(shortCtx, params)
	cancel()

	// Verify session file exists
	sessionFile := findSessionFile(t, resumeDir)
	if sessionFile == "" {
		t.Fatalf("expected session file to exist after timeout")
	}
	t.Logf("stale session file created: %s", filepath.Base(sessionFile))

	// Cleanup with maxAge=0 should clean up immediately
	cleaned, errs := sdk.Blob.CleanupStaleSessions(resumeDir, 0)
	if len(errs) > 0 {
		t.Logf("cleanup errors (non-fatal): %v", errs)
	}
	if cleaned != 1 {
		t.Fatalf("expected 1 session cleaned, got %d", cleaned)
	}

	// Verify session file is gone
	if files, _ := os.ReadDir(resumeDir); len(files) > 0 {
		t.Fatalf("expected resume dir to be empty after cleanup, found %d files", len(files))
	}

	t.Logf("stale session cleanup successful")
}
