//go:build integration
// +build integration

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestLargeUploadViaDaemonResumeSmoke is a smaller, CI-friendly resume test.
// It kills the client mid-upload and verifies the upload resumes and reaches bob.
func TestLargeUploadViaDaemonResumeSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping resume smoke test in short mode")
	}

	t.Setenv("SBDEV_PART_SIZE", "8MB")
	t.Setenv("SBDEV_PART_UPLOAD_TIMEOUT", "")
	t.Setenv("SYFTBOX_UPLOAD_PART_SLEEP_MS", "150")
	t.Setenv("SBDEV_HTTP_WRITE_TIMEOUT", "8s")

	h := NewDevstackHarness(t)
	if err := h.alice.CreateDefaultACLs(); err != nil {
		t.Fatalf("create alice default ACLs: %v", err)
	}
	if err := h.bob.CreateDefaultACLs(); err != nil {
		t.Fatalf("create bob default ACLs: %v", err)
	}
	if err := h.bob.SetSubscriptionsAllow(h.alice.email); err != nil {
		t.Fatalf("set bob subscriptions: %v", err)
	}
	time.Sleep(2 * time.Second)

	serverURL := fmt.Sprintf("http://127.0.0.1:%d", h.state.Server.Port)
	aliceClientURL := fmt.Sprintf("http://127.0.0.1:%d", h.alice.state.Port)
	authToken := extractAuthToken(t, h.alice.state.LogPath)
	maybeWriteWatchEnv(t, aliceClientURL, authToken)

	// 64MB file keeps the test fast while still exercising multipart resume.
	fileSize := int64(64 * 1024 * 1024)
	relName := "daemon-large-upload-resume-smoke.bin"
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

	demoTriggerSync(t, aliceClientURL, authToken)
	waitForUploadParts(t, aliceClientURL, authToken, relName, 1, 2*time.Minute)

	// Kill alice mid-upload.
	if err := killProcess(h.alice.state.PID); err != nil {
		t.Fatalf("kill alice: %v", err)
	}

	// Restart alice on same port.
	newState, err := startClient(h.alice.state.BinPath, h.root, h.alice.email, serverURL, h.alice.state.Port)
	if err != nil {
		t.Fatalf("restart alice: %v", err)
	}
	h.alice.state = newState
	h.state.Clients[0] = newState
	h.alice.dataDir = newState.DataPath
	h.alice.publicDir = filepath.Join(newState.DataPath, "datasites", h.alice.email, "public")
	aliceClientURL = fmt.Sprintf("http://127.0.0.1:%d", newState.Port)

	startupWait := 2 * time.Second
	if runtime.GOOS == "windows" {
		startupWait = 5 * time.Second
	}
	time.Sleep(startupWait)
	authToken = extractAuthToken(t, newState.LogPath)
	maybeWriteWatchEnv(t, aliceClientURL, authToken)

	deadline := time.Now().Add(3 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	loopIteration := 0
	for time.Now().Before(deadline) {
		loopIteration++
		demoTriggerSync(t, aliceClientURL, authToken)
		if loopIteration%3 == 1 {
			logAliceUploadState(t, aliceClientURL, authToken, relName)
		}
		if err := h.bob.WaitForFile(h.alice.email, relName, "", 2*time.Second); err == nil {
			return
		}
		select {
		case <-ticker.C:
			t.Logf("Still waiting for bob to receive file (elapsed=%v, iteration=%d)...",
				time.Since(deadline.Add(-3*time.Minute)).Round(time.Second), loopIteration)
		default:
		}
		time.Sleep(2 * time.Second)
	}

	logAliceUploadState(t, aliceClientURL, authToken, relName)
	logAliceSyncStatus(t, aliceClientURL, authToken)
	t.Fatalf("bob did not receive resume smoke upload within timeout")
}
