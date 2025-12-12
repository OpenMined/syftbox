//go:build integration
// +build integration

package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestLargeUploadViaDaemon ensures large uploads flow through the syftbox client daemon
// (so control plane /v1/status reports the real tx/rx).
func TestLargeUploadViaDaemon(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large upload via daemon test in short mode")
	}

	// Force short server write timeouts to exercise resumable uploads.
	t.Setenv("SBDEV_HTTP_WRITE_TIMEOUT", "1s")

	h := NewDevstackHarness(t)
	time.Sleep(2 * time.Second)

	aliceClientURL := fmt.Sprintf("http://127.0.0.1:%d", h.alice.state.Port)
	authToken := extractAuthToken(t, h.alice.state.LogPath)
	maybeWriteWatchEnv(t, aliceClientURL, authToken)

	// Create large file inside Alice's datasite so daemon sync uploads it.
	fileSize := int64(256 * 1024 * 1024) // 256MB
	relName := "daemon-large-upload.bin"
	testFile := filepath.Join(h.alice.dataDir, "datasites", h.alice.email, "public", relName)

	if err := os.MkdirAll(filepath.Dir(testFile), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	f, err := os.Create(testFile)
	if err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := f.Truncate(fileSize); err != nil {
		f.Close()
		t.Fatalf("truncate file: %v", err)
	}
	f.Close()

	// Trigger sync through control plane and retry periodically to allow resumable resume after timeouts.
	deadline := time.Now().Add(10 * time.Minute)
	pausedOnce := false
	for time.Now().Before(deadline) {
		demoTriggerSync(t, aliceClientURL, authToken)
		// Wait a bit for progress; doesn't fail on timeout.
		waitForUploadComplete(t, aliceClientURL, authToken, relName, 10*time.Second)

		// Demonstrate pause/resume once when upload appears.
		if !pausedOnce {
			uploads := demoGetUploads(t, aliceClientURL, authToken)
			for _, u := range uploads.Uploads {
				if filepath.Base(u.Key) == relName {
					// Pause
					req, _ := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/v1/uploads/%s/pause", aliceClientURL, u.ID), nil)
					req.Header.Set("Authorization", "Bearer "+authToken)
					if resp, err := http.DefaultClient.Do(req); err == nil {
						resp.Body.Close()
					}
					time.Sleep(1 * time.Second)
					// Resume
					req, _ = http.NewRequest(http.MethodPost, fmt.Sprintf("%s/v1/uploads/%s/resume", aliceClientURL, u.ID), nil)
					req.Header.Set("Authorization", "Bearer "+authToken)
					if resp, err := http.DefaultClient.Do(req); err == nil {
						resp.Body.Close()
					}
					pausedOnce = true
					break
				}
			}
		}

		if err := h.bob.WaitForFile(h.alice.email, relName, "", 2*time.Second); err == nil {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("bob did not receive upload within timeout")
}
