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
	fileSize := int64(1 << 30) // 1GB

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
		PartSize:          32 * 1024 * 1024, // 32MB parts keep progress visible
		PartUploadTimeout: time.Second,
	}

	// First attempt should time out but leave a resumable session on disk.
	shortCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := sdk.Blob.Upload(shortCtx, params); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected timeout on first upload attempt, got %v", err)
	}

	sessionFile := findSessionFile(t, resumeDir)
	if sessionFile == "" {
		t.Fatalf("expected resumable session file after timeout")
	}
	if completed := completedPartsCount(t, sessionFile); completed == 0 {
		t.Fatalf("expected some parts to finish before timeout")
	}

	// Second attempt should resume and finish.
	longCtx, cancelLong := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancelLong()
	resp, err := sdk.Blob.Upload(longCtx, params)
	if err != nil {
		t.Fatalf("resumed upload failed: %v", err)
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
